// Copyright 2014-2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//	http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package stats

//go:generate go run ../../scripts/generate/mockgen.go github.com/aws/amazon-ecs-agent/agent/stats Engine mock/$GOFILE

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/cihub/seelog"
	"github.com/pborman/uuid"

	"github.com/aws/amazon-ecs-agent/agent/api"
	"github.com/aws/amazon-ecs-agent/agent/config"
	ecsengine "github.com/aws/amazon-ecs-agent/agent/engine"
	"github.com/aws/amazon-ecs-agent/agent/eventstream"
	"github.com/aws/amazon-ecs-agent/agent/stats/resolver"
	"github.com/aws/amazon-ecs-agent/agent/tcs/model/ecstcs"
	"github.com/aws/aws-sdk-go/aws"
)

const (
	containerChangeHandler = "DockerStatsEngineDockerEventsHandler"
	listContainersTimeout  = 10 * time.Minute
	queueResetThreshold    = 2 * ecsengine.StatsInactivityTimeout
)

// DockerContainerMetadataResolver implements ContainerMetadataResolver for
// DockerTaskEngine.
type DockerContainerMetadataResolver struct {
	dockerTaskEngine *ecsengine.DockerTaskEngine
}

// Engine defines methods to be implemented by the engine struct. It is
// defined to make testing easier.
type Engine interface {
	GetInstanceMetrics() (*ecstcs.MetricsMetadata, []*ecstcs.TaskMetric, error)
}

// DockerStatsEngine is used to monitor docker container events and to report
// utlization metrics of the same.
type DockerStatsEngine struct {
	client                     ecsengine.DockerClient
	cluster                    string
	containerInstanceArn       string
	containersLock             sync.RWMutex
	containerChangeEventStream *eventstream.EventStream
	resolver                   resolver.ContainerMetadataResolver
	// tasksToContainers maps task arns to a map of container ids to StatsContainer objects.
	tasksToContainers map[string]map[string]*StatsContainer
	// tasksToDefinitions maps task arns to task definiton name and family metadata objects.
	tasksToDefinitions map[string]*taskDefinition
}

var EmptyMetricsError = errors.New("No task metrics to report")

// ResolveTask resolves the api task object, given container id.
func (resolver *DockerContainerMetadataResolver) ResolveTask(dockerID string) (*api.Task, error) {
	if resolver.dockerTaskEngine == nil {
		return nil, fmt.Errorf("Docker task engine uninitialized")
	}
	task, found := resolver.dockerTaskEngine.State().TaskByID(dockerID)
	if !found {
		return nil, fmt.Errorf("Could not map docker id to task: %s", dockerID)
	}

	return task, nil
}

// ResolveContainer resolves the api container object, given container id.
func (resolver *DockerContainerMetadataResolver) ResolveContainer(dockerID string) (*api.DockerContainer, error) {
	if resolver.dockerTaskEngine == nil {
		return nil, fmt.Errorf("Docker task engine uninitialized")
	}
	container, found := resolver.dockerTaskEngine.State().ContainerByID(dockerID)
	if !found {
		return nil, fmt.Errorf("Could not map docker id to container: %s", dockerID)
	}

	return container, nil
}

// NewDockerStatsEngine creates a new instance of the DockerStatsEngine object.
// MustInit() must be called to initialize the fields of the new event listener.
func NewDockerStatsEngine(cfg *config.Config, client ecsengine.DockerClient, containerChangeEventStream *eventstream.EventStream) *DockerStatsEngine {
	return &DockerStatsEngine{
		client:                     client,
		resolver:                   nil,
		tasksToContainers:          make(map[string]map[string]*StatsContainer),
		tasksToDefinitions:         make(map[string]*taskDefinition),
		containerChangeEventStream: containerChangeEventStream,
	}
}

// MustInit initializes fields of the DockerStatsEngine object.
func (engine *DockerStatsEngine) MustInit(taskEngine ecsengine.TaskEngine, cluster string, containerInstanceArn string) error {
	seelog.Info("Initializing stats engine")
	engine.cluster = cluster
	engine.containerInstanceArn = containerInstanceArn

	var err error
	engine.resolver, err = newDockerContainerMetadataResolver(taskEngine)
	if err != nil {
		return err
	}

	return engine.Init()
}

// Init subscribes to the container change event stream.
func (engine *DockerStatsEngine) Init() error {
	// Subscribe to the container change event stream
	err := engine.containerChangeEventStream.Subscribe(containerChangeHandler, engine.handleDockerEvents)
	if err != nil {
		return fmt.Errorf("Failed to subscribe to container change event stream, err %v", err)
	}

	go engine.listContainersAndStartEventHandler()
	go engine.waitToStop()

	return nil
}

// waitToStop waits for the container change event stream close ans stop collection metrics
func (engine *DockerStatsEngine) waitToStop() {
	// Waiting for the event stream to close
	ctx := engine.containerChangeEventStream.Context()
	select {
	case <-ctx.Done():
		seelog.Debug("Event stream closed, stop listening to the event stream")
		engine.containerChangeEventStream.Unsubscribe(containerChangeHandler)
		engine.removeAll()
	}
}

// removeAll stops the periodic usage data collection for all containers
func (engine *DockerStatsEngine) removeAll() {
	for task, containers := range engine.tasksToContainers {
		for _, statsContainer := range containers {
			statsContainer.StopStatsCollection()
		}
		delete(engine.tasksToContainers, task)
	}
}

// listContainersAndStartEventHandler adds existing containers to the watch-list
// and starts the docker event handler.
func (engine *DockerStatsEngine) listContainersAndStartEventHandler() {
	// List and add existing containers to the list of containers to watch.
	err := engine.addExistingContainers()
	if err != nil {
		seelog.Warnf("Error listing existing containers, err: %v", err)
		return
	}
}

// addExistingContainers lists existing containers and adds them to the engine.
func (engine *DockerStatsEngine) addExistingContainers() error {
	listContainersResponse := engine.client.ListContainers(false, ecsengine.ListContainersTimeout)
	if listContainersResponse.Error != nil {
		return listContainersResponse.Error
	}

	for _, containerID := range listContainersResponse.DockerIDs {
		engine.addContainer(containerID)
	}

	return nil
}

// GetInstanceMetrics gets all task metrics and instance metadata from stats engine.
func (engine *DockerStatsEngine) GetInstanceMetrics() (*ecstcs.MetricsMetadata, []*ecstcs.TaskMetric, error) {
	var taskMetrics []*ecstcs.TaskMetric
	idle := engine.isIdle()
	metricsMetadata := &ecstcs.MetricsMetadata{
		Cluster:           aws.String(engine.cluster),
		ContainerInstance: aws.String(engine.containerInstanceArn),
		Idle:              aws.Bool(idle),
		MessageId:         aws.String(uuid.NewRandom().String()),
	}

	if idle {
		seelog.Debug("Instance is idle. No task metrics to report")
		fin := true
		metricsMetadata.Fin = &fin
		return metricsMetadata, taskMetrics, nil
	}

	for taskArn := range engine.tasksToContainers {
		containerMetrics, err := engine.getContainerMetricsForTask(taskArn)
		if err != nil {
			seelog.Debugf("Error getting container metrics for task: %s, err: %v", taskArn, err)
			continue
		}

		if len(containerMetrics) == 0 {
			seelog.Debugf("Empty containerMetrics for task, ignoring, task: %s", taskArn)
			continue
		}

		taskDef, exists := engine.tasksToDefinitions[taskArn]
		if !exists {
			seelog.Debugf("Could not map task to definition, task: %s", taskArn)
			continue
		}

		metricTaskArn := taskArn
		taskMetric := &ecstcs.TaskMetric{
			TaskArn:               &metricTaskArn,
			TaskDefinitionFamily:  &taskDef.family,
			TaskDefinitionVersion: &taskDef.version,
			ContainerMetrics:      containerMetrics,
		}
		taskMetrics = append(taskMetrics, taskMetric)
	}

	if len(taskMetrics) == 0 {
		// Not idle. Expect taskMetrics to be there.
		return nil, nil, EmptyMetricsError
	}

	// Reset current stats. Retaining older stats results in incorrect utilization stats
	// until they are removed from the queue.
	engine.resetStats()
	return metricsMetadata, taskMetrics, nil
}

func (engine *DockerStatsEngine) isIdle() bool {
	engine.containersLock.RLock()
	defer engine.containersLock.RUnlock()

	return len(engine.tasksToContainers) == 0
}

// handleDockerEvents must be called after openEventstream; it processes each
// event that it reads from the docker event stream.
func (engine *DockerStatsEngine) handleDockerEvents(events ...interface{}) error {
	for _, event := range events {
		dockerContainerChangeEvent, ok := event.(ecsengine.DockerContainerChangeEvent)
		if !ok {
			return fmt.Errorf("Unexpected event received, expected docker container change event")
		}

		switch dockerContainerChangeEvent.Status {
		case api.ContainerRunning:
			engine.addContainer(dockerContainerChangeEvent.DockerID)
		case api.ContainerStopped:
			engine.removeContainer(dockerContainerChangeEvent.DockerID)
		default:
			seelog.Debugf("Ignoring event for container, id: %s, status: %d", dockerContainerChangeEvent.DockerID, dockerContainerChangeEvent.Status)
		}
	}

	return nil
}

// addContainer adds a container to the map of containers being watched.
// It also starts the periodic usage data collection for the container.
func (engine *DockerStatsEngine) addContainer(dockerID string) {
	engine.containersLock.Lock()
	defer engine.containersLock.Unlock()

	// Make sure that this container belongs to a task and that the task
	// is not terminal.
	task, err := engine.resolver.ResolveTask(dockerID)
	if err != nil {
		seelog.Debugf("Could not map container to task, ignoring, err: %v, id: %s", err, dockerID)
		return
	}

	if len(task.Arn) == 0 || len(task.Family) == 0 {
		seelog.Debugf("Task has invalid fields, id: %s", dockerID)
		return
	}

	if task.GetKnownStatus().Terminal() {
		seelog.Debugf("Task is terminal, ignoring, id: %s", dockerID)
		return
	}

	// Check if this container is already being watched.
	_, taskExists := engine.tasksToContainers[task.Arn]
	if taskExists {
		// task arn exists in map.
		_, containerExists := engine.tasksToContainers[task.Arn][dockerID]
		if containerExists {
			// container arn exists in map.
			seelog.Debugf("Container already being watched, ignoring, id: %s", dockerID)
			return
		}
	} else {
		// Create a map for the task arn if it doesn't exist yet.
		engine.tasksToContainers[task.Arn] = make(map[string]*StatsContainer)
	}

	seelog.Debugf("Adding container to stats watch list, id: %s, task: %s", dockerID, task.Arn)
	container := newStatsContainer(dockerID, engine.client, engine.resolver)
	engine.tasksToContainers[task.Arn][dockerID] = container
	engine.tasksToDefinitions[task.Arn] = &taskDefinition{family: task.Family, version: task.Version}
	container.StartStatsCollection()
}

// removeContainer deletes the container from the map of containers being watched.
// It also stops the periodic usage data collection for the container.
func (engine *DockerStatsEngine) removeContainer(dockerID string) {
	engine.containersLock.Lock()
	defer engine.containersLock.Unlock()

	// Make sure that this container belongs to a task.
	task, err := engine.resolver.ResolveTask(dockerID)
	if err != nil {
		seelog.Debugf("Could not map container to task, ignoring, err: %v, id: %s", err, dockerID)
		return
	}

	_, taskExists := engine.tasksToContainers[task.Arn]
	if !taskExists {
		seelog.Debugf("Container not being watched, id: %s", dockerID)
		return
	}

	// task arn exists in map.
	container, containerExists := engine.tasksToContainers[task.Arn][dockerID]
	if !containerExists {
		// container arn does not exist in map.
		seelog.Debugf("Container not being watched, id: %s", dockerID)
		return
	}

	engine.doRemoveContainer(container, task.Arn)
}

// newDockerContainerMetadataResolver returns a new instance of DockerContainerMetadataResolver.
func newDockerContainerMetadataResolver(taskEngine ecsengine.TaskEngine) (*DockerContainerMetadataResolver, error) {
	dockerTaskEngine, ok := taskEngine.(*ecsengine.DockerTaskEngine)
	if !ok {
		// Error type casting docker task engine.
		return nil, fmt.Errorf("Could not load docker task engine")
	}

	resolver := &DockerContainerMetadataResolver{
		dockerTaskEngine: dockerTaskEngine,
	}

	return resolver, nil
}

// getContainerMetricsForTask gets all container metrics for a task arn.
func (engine *DockerStatsEngine) getContainerMetricsForTask(taskArn string) ([]*ecstcs.ContainerMetric, error) {
	engine.containersLock.Lock()
	defer engine.containersLock.Unlock()

	containerMap, taskExists := engine.tasksToContainers[taskArn]
	if !taskExists {
		return nil, fmt.Errorf("Task not found")
	}

	var containerMetrics []*ecstcs.ContainerMetric
	for _, container := range containerMap {
		dockerID := container.containerMetadata.DockerID
		// Check if the container is terminal. If it is, make sure that it is
		// cleaned up properly. We might sometimes miss events from docker task
		// engine and this helps in reconciling the state. The tcs client's
		// GetInstanceMetrics probe is used as the trigger for this.
		terminal, err := container.terminal()
		if err != nil {
			// Error determining if the container is terminal. This means that the container
			// id could not be resolved to a container that is being tracked by the
			// docker task engine. If the docker task engine has already removed
			// the container from its state, there's no point in stats engine tracking the
			// container. So, clean-up anyway.
			seelog.Warnf("Error determining if the container %s is terminal, cleaning up and skipping", dockerID, err)
			engine.doRemoveContainer(container, taskArn)
			continue
		} else if terminal {
			// Container is in knonwn terminal state. Stop collection metrics.
			seelog.Infof("Container %s is terminal, cleaning up and skipping", dockerID)
			engine.doRemoveContainer(container, taskArn)
			continue
		}

		if !container.statsQueue.enoughDatapointsInBuffer() &&
			!container.statsQueue.resetThresholdElapsed(queueResetThreshold) {
			seelog.Debugf("Stats not ready for container %s", dockerID)
			continue
		}

		// Container is not terminal. Get CPU stats set.
		cpuStatsSet, err := container.statsQueue.GetCPUStatsSet()
		if err != nil {
			seelog.Warnf("Error getting cpu stats, err: %v, container: %v", err, dockerID)
			continue
		}

		// Get memory stats set.
		memoryStatsSet, err := container.statsQueue.GetMemoryStatsSet()
		if err != nil {
			seelog.Warnf("Error getting memory stats, err: %v, container: %v", err, dockerID)
			continue
		}

		containerMetrics = append(containerMetrics, &ecstcs.ContainerMetric{
			CpuStatsSet:    cpuStatsSet,
			MemoryStatsSet: memoryStatsSet,
		})

	}

	return containerMetrics, nil
}

func (engine *DockerStatsEngine) doRemoveContainer(container *StatsContainer, taskArn string) {
	container.StopStatsCollection()
	dockerID := container.containerMetadata.DockerID
	delete(engine.tasksToContainers[taskArn], dockerID)
	seelog.Debugf("Deleted container from tasks, id: %s", dockerID)

	if len(engine.tasksToContainers[taskArn]) == 0 {
		// No containers in task, delete task arn from map.
		delete(engine.tasksToContainers, taskArn)
		// No need to verify if the key exists in tasksToDefinitions.
		// Delete will do nothing if the specified key doesn't exist.
		delete(engine.tasksToDefinitions, taskArn)
		seelog.Debugf("Deleted task from tasks, arn: %s", taskArn)
	}
}

// resetStats resets stats for all watched containers.
func (engine *DockerStatsEngine) resetStats() {
	engine.containersLock.Lock()
	defer engine.containersLock.Unlock()
	for _, containerMap := range engine.tasksToContainers {
		for _, container := range containerMap {
			container.statsQueue.Reset()
		}
	}
}

// newMetricsMetadata creates the singleton metadata object.
func newMetricsMetadata(cluster *string, containerInstance *string) *ecstcs.MetricsMetadata {
	return &ecstcs.MetricsMetadata{
		Cluster:           cluster,
		ContainerInstance: containerInstance,
	}
}
