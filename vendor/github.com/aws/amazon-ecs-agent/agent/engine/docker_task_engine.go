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

// Package engine contains the core logic for managing tasks
package engine

import (
	"strconv"
	"sync"
	"time"

	"github.com/aws/amazon-ecs-agent/agent/api"
	"github.com/aws/amazon-ecs-agent/agent/config"
	"github.com/aws/amazon-ecs-agent/agent/containermetadata"
	"github.com/aws/amazon-ecs-agent/agent/credentials"
	"github.com/aws/amazon-ecs-agent/agent/ecscni"
	"github.com/aws/amazon-ecs-agent/agent/engine/dependencygraph"
	"github.com/aws/amazon-ecs-agent/agent/engine/dockerclient"
	"github.com/aws/amazon-ecs-agent/agent/engine/dockerstate"
	"github.com/aws/amazon-ecs-agent/agent/engine/emptyvolume"
	"github.com/aws/amazon-ecs-agent/agent/eventstream"
	"github.com/aws/amazon-ecs-agent/agent/statechange"
	"github.com/aws/amazon-ecs-agent/agent/statemanager"
	"github.com/aws/amazon-ecs-agent/agent/utils"
	utilsync "github.com/aws/amazon-ecs-agent/agent/utils/sync"
	"github.com/aws/amazon-ecs-agent/agent/utils/ttime"

	"github.com/cihub/seelog"
	"github.com/pkg/errors"
	"golang.org/x/net/context"
)

const (
	//DockerEndpointEnvVariable is the environment variable that can override the Docker endpoint
	DockerEndpointEnvVariable = "DOCKER_HOST"
	// DockerDefaultEndpoint is the default value for the Docker endpoint
	DockerDefaultEndpoint        = "unix:///var/run/docker.sock"
	capabilityPrefix             = "com.amazonaws.ecs.capability."
	capabilityTaskIAMRole        = "task-iam-role"
	capabilityTaskIAMRoleNetHost = "task-iam-role-network-host"
	capabilityTaskCPUMemLimit    = "task-cpu-mem-limit"
	labelPrefix                  = "com.amazonaws.ecs."
	attributePrefix              = "ecs.capability."
)

// DockerTaskEngine is a state machine for managing a task and its containers
// in ECS.
//
// DockerTaskEngine implements an abstraction over the DockerGoClient so that
// it does not have to know about tasks, only containers
// The DockerTaskEngine interacts with Docker to implement a TaskEngine
type DockerTaskEngine struct {
	// implements TaskEngine

	cfg *config.Config

	initialized  bool
	mustInitLock sync.Mutex

	// state stores all tasks this task engine is aware of, including their
	// current state and mappings to/from dockerId and name.
	// This is used to checkpoint state to disk so tasks may survive agent
	// failures or updates
	state        dockerstate.TaskEngineState
	managedTasks map[string]*managedTask

	taskStopGroup *utilsync.SequentialWaitGroup

	events            <-chan DockerContainerChangeEvent
	stateChangeEvents chan statechange.Event
	saver             statemanager.Saver

	client     DockerClient
	clientLock sync.Mutex
	cniClient  ecscni.CNIClient

	containerChangeEventStream *eventstream.EventStream

	stopEngine context.CancelFunc

	// processTasks is a mutex that the task engine must aquire before changing
	// any task's state which it manages. Since this is a lock that encompasses
	// all tasks, it must not aquire it for any significant duration
	// The write mutex should be taken when adding and removing tasks from managedTasks.
	processTasks sync.RWMutex

	enableConcurrentPull                bool
	credentialsManager                  credentials.Manager
	_time                               ttime.Time
	_timeOnce                           sync.Once
	imageManager                        ImageManager
	containerStatusToTransitionFunction map[api.ContainerStatus]transitionApplyFunc
	metadataManager                     containermetadata.Manager
}

// NewDockerTaskEngine returns a created, but uninitialized, DockerTaskEngine.
// The distinction between created and initialized is that when created it may
// be serialized/deserialized, but it will not communicate with docker until it
// is also initialized.
func NewDockerTaskEngine(cfg *config.Config, client DockerClient,
	credentialsManager credentials.Manager, containerChangeEventStream *eventstream.EventStream,
	imageManager ImageManager, state dockerstate.TaskEngineState,
	metadataManager containermetadata.Manager) *DockerTaskEngine {
	dockerTaskEngine := &DockerTaskEngine{
		cfg:    cfg,
		client: client,
		saver:  statemanager.NewNoopStateManager(),

		state:         state,
		managedTasks:  make(map[string]*managedTask),
		taskStopGroup: utilsync.NewSequentialWaitGroup(),

		stateChangeEvents: make(chan statechange.Event),

		enableConcurrentPull: false,
		credentialsManager:   credentialsManager,

		containerChangeEventStream: containerChangeEventStream,
		imageManager:               imageManager,
		cniClient: ecscni.NewClient(&ecscni.Config{
			PluginsPath:            cfg.CNIPluginsPath,
			MinSupportedCNIVersion: config.DefaultMinSupportedCNIVersion,
		}),

		metadataManager: metadataManager,
	}

	dockerTaskEngine.initializeContainerStatusToTransitionFunction()

	return dockerTaskEngine
}

func (engine *DockerTaskEngine) initializeContainerStatusToTransitionFunction() {
	containerStatusToTransitionFunction := map[api.ContainerStatus]transitionApplyFunc{
		api.ContainerPulled:               engine.pullContainer,
		api.ContainerCreated:              engine.createContainer,
		api.ContainerRunning:              engine.startContainer,
		api.ContainerResourcesProvisioned: engine.provisionContainerResources,
		api.ContainerStopped:              engine.stopContainer,
	}
	engine.containerStatusToTransitionFunction = containerStatusToTransitionFunction
}

// ImagePullDeleteLock ensures that pulls and deletes do not run at the same time and pulls can be run at the same time for docker >= 1.11.1
// Pulls are serialized as a temporary workaround for a devicemapper issue. (see https://github.com/docker/docker/issues/9718)
// Deletes must not run at the same time as pulls to prevent deletion of images that are being used to launch new tasks.
var ImagePullDeleteLock sync.RWMutex

// UnmarshalJSON restores a previously marshaled task-engine state from json
func (engine *DockerTaskEngine) UnmarshalJSON(data []byte) error {
	return engine.state.UnmarshalJSON(data)
}

// MarshalJSON marshals into state directly
func (engine *DockerTaskEngine) MarshalJSON() ([]byte, error) {
	return engine.state.MarshalJSON()
}

// Init initializes a DockerTaskEngine such that it may communicate with docker
// and operate normally.
// This function must be called before any other function, except serializing and deserializing, can succeed without error.
func (engine *DockerTaskEngine) Init(ctx context.Context) error {
	// TODO, pass in a a context from main from background so that other things can stop us, not just the tests
	derivedCtx, cancel := context.WithCancel(ctx)
	engine.stopEngine = cancel

	// Determine whether the engine can perform concurrent "docker pull" based on docker version
	engine.enableConcurrentPull = engine.isParallelPullCompatible()

	// Open the event stream before we sync state so that e.g. if a container
	// goes from running to stopped after we sync with it as "running" we still
	// have the "went to stopped" event pending so we can be up to date.
	err := engine.openEventstream(derivedCtx)
	if err != nil {
		return err
	}
	engine.synchronizeState()
	// Now catch up and start processing new events per normal
	go engine.handleDockerEvents(derivedCtx)
	engine.initialized = true
	return nil
}

// SetDockerClient provides a way to override the client used for communication with docker as a testing hook.
func (engine *DockerTaskEngine) SetDockerClient(client DockerClient) {
	engine.clientLock.Lock()
	engine.clientLock.Unlock()
	engine.client = client
}

// MustInit blocks and retries until an engine can be initialized.
func (engine *DockerTaskEngine) MustInit(ctx context.Context) {
	if engine.initialized {
		return
	}
	engine.mustInitLock.Lock()
	defer engine.mustInitLock.Unlock()

	errorOnce := sync.Once{}
	taskEngineConnectBackoff := utils.NewSimpleBackoff(200*time.Millisecond, 2*time.Second, 0.20, 1.5)
	utils.RetryWithBackoff(taskEngineConnectBackoff, func() error {
		if engine.initialized {
			return nil
		}
		err := engine.Init(ctx)
		if err != nil {
			errorOnce.Do(func() {
				log.Error("Could not connect to docker daemon", "err", err)
			})
		}
		return err
	})
}

// SetSaver sets the saver that is used by the DockerTaskEngine
func (engine *DockerTaskEngine) SetSaver(saver statemanager.Saver) {
	engine.saver = saver
}

// Shutdown makes a best-effort attempt to cleanup after the task engine.
// This should not be relied on for anything more complicated than testing.
func (engine *DockerTaskEngine) Shutdown() {
	engine.stopEngine()
	engine.Disable()
}

// Disable prevents this engine from managing any additional tasks.
func (engine *DockerTaskEngine) Disable() {
	engine.processTasks.Lock()
}

// synchronizeState explicitly goes through each docker container stored in
// "state" and updates its KnownStatus appropriately, as well as queueing up
// events to push upstream.
func (engine *DockerTaskEngine) synchronizeState() {
	engine.processTasks.Lock()
	defer engine.processTasks.Unlock()
	imageStates := engine.state.AllImageStates()
	if len(imageStates) != 0 {
		engine.imageManager.AddAllImageStates(imageStates)
	}

	tasks := engine.state.AllTasks()
	var tasksToStart []*api.Task
	for _, task := range tasks {
		conts, ok := engine.state.ContainerMapByArn(task.Arn)
		if !ok {
			// task hasn't started processing, no need to check container status
			tasksToStart = append(tasksToStart, task)
			continue
		}

		for _, cont := range conts {
			engine.synchronizeContainerStatus(cont, task)
		}

		tasksToStart = append(tasksToStart, task)

		// Put tasks that are stopped by acs but hasn't been stopped in wait group
		if task.GetDesiredStatus().Terminal() && task.GetStopSequenceNumber() != 0 {
			engine.taskStopGroup.Add(task.GetStopSequenceNumber(), 1)
		}
	}

	for _, task := range tasksToStart {
		engine.startTask(task)
	}

	engine.saver.Save()
}

// synchronizeContainerStatus checks and updates the container status with docker
func (engine *DockerTaskEngine) synchronizeContainerStatus(container *api.DockerContainer, task *api.Task) {
	if container.DockerID == "" {
		log.Debug("Found container potentially created while we were down", "name", container.DockerName)
		// Figure out the dockerid
		describedContainer, err := engine.client.InspectContainer(container.DockerName, inspectContainerTimeout)
		if err != nil {
			log.Warn("Could not find matching container for expected", "name", container.DockerName)
		} else {
			container.DockerID = describedContainer.ID
			container.Container.SetKnownStatus(dockerStateToState(describedContainer.State))
			// update mappings that need dockerid
			engine.state.AddContainer(container, task)
			engine.imageManager.RecordContainerReference(container.Container)
		}
		return
	}

	if container.DockerID != "" {
		currentState, metadata := engine.client.DescribeContainer(container.DockerID)
		if metadata.Error != nil {
			currentState = api.ContainerStopped
			if !container.Container.KnownTerminal() {
				container.Container.ApplyingError = api.NewNamedError(&ContainerVanishedError{})
				log.Warn("Could not describe previously known container; assuming dead", "err", metadata.Error, "id", container.DockerID, "name", container.DockerName)
				engine.imageManager.RemoveContainerReferenceFromImageState(container.Container)
			}
		} else {
			engine.imageManager.RecordContainerReference(container.Container)
			if engine.cfg.ContainerMetadataEnabled && !container.Container.IsMetadataFileUpdated() {
				go engine.updateMetadataFile(task, container)
			}
		}
		if currentState > container.Container.GetKnownStatus() {
			// update the container known status
			container.Container.SetKnownStatus(currentState)
		}
	}
}

// CheckTaskState inspects the state of all containers within a task and writes
// their state to the managed task's container channel.
func (engine *DockerTaskEngine) CheckTaskState(task *api.Task) {
	taskContainers, ok := engine.state.ContainerMapByArn(task.Arn)
	if !ok {
		log.Warn("Could not check task state for task; no task in state", "task", task)
		return
	}
	for _, container := range task.Containers {
		dockerContainer, ok := taskContainers[container.Name]
		if !ok {
			continue
		}
		status, metadata := engine.client.DescribeContainer(dockerContainer.DockerID)
		engine.processTasks.RLock()
		managedTask, ok := engine.managedTasks[task.Arn]
		engine.processTasks.RUnlock()

		if ok {
			managedTask.dockerMessages <- dockerContainerChange{
				container: container,
				event: DockerContainerChangeEvent{
					Status:                  status,
					DockerContainerMetadata: metadata,
				},
			}
		}
	}
}

// sweepTask deletes all the containers associated with a task
func (engine *DockerTaskEngine) sweepTask(task *api.Task) {
	for _, cont := range task.Containers {
		err := engine.removeContainer(task, cont)
		if err != nil {
			log.Debug("Unable to remove old container", "err", err, "task", task, "cont", cont)
		}
		// Internal container(created by ecs-agent) state isn't recorded
		if cont.IsInternal() {
			continue
		}
		err = engine.imageManager.RemoveContainerReferenceFromImageState(cont)
		if err != nil {
			seelog.Errorf("Error removing container reference from image state: %v", err)
		}
	}

	// Clean metadata directory for task
	if engine.cfg.ContainerMetadataEnabled {
		err := engine.metadataManager.Clean(task.Arn)
		if err != nil {
			seelog.Warnf("Clean task metadata failed for task %s: %v", task.Arn, err)
		}
	}
	engine.saver.Save()
}

func (engine *DockerTaskEngine) emitTaskEvent(task *api.Task, reason string) {
	taskKnownStatus := task.GetKnownStatus()
	if !taskKnownStatus.BackendRecognized() {
		return
	}
	if task.GetSentStatus() >= taskKnownStatus {
		log.Debug("Already sent task event; no need to re-send", "task", task.Arn, "event", taskKnownStatus.String())
		return
	}

	event := api.TaskStateChange{
		TaskARN: task.Arn,
		Status:  taskKnownStatus,
		Reason:  reason,
		Task:    task,
	}

	event.SetTaskTimestamps()

	seelog.Infof("Task change event: %s", event.String())
	engine.stateChangeEvents <- event
}

// startTask creates a managedTask construct to track the task and then begins
// pushing it towards its desired state when allowed startTask is protected by
// the processTasks lock of 'AddTask'. It should not be called from anywhere
// else and should exit quickly to allow AddTask to do more work.
func (engine *DockerTaskEngine) startTask(task *api.Task) {
	// Create a channel that may be used to communicate with this task, survey
	// what tasks need to be waited for for this one to start, and then spin off
	// a goroutine to oversee this task

	thisTask := engine.newManagedTask(task)
	thisTask._time = engine.time()

	go thisTask.overseeTask()
}

func (engine *DockerTaskEngine) time() ttime.Time {
	engine._timeOnce.Do(func() {
		if engine._time == nil {
			engine._time = &ttime.DefaultTime{}
		}
	})
	return engine._time
}

// emitContainerEvent passes a given event up through the containerEvents channel if necessary.
// It will omit events the backend would not process and will perform best-effort deduplication of events.
func (engine *DockerTaskEngine) emitContainerEvent(task *api.Task, cont *api.Container, reason string) {
	contKnownStatus := cont.GetKnownStatus()
	if !contKnownStatus.ShouldReportToBackend(cont.GetSteadyStateStatus()) {
		return
	}
	if cont.IsInternal() {
		return
	}
	if cont.GetSentStatus() >= contKnownStatus {
		log.Debug("Already sent container event; no need to re-send", "task", task.Arn, "container", cont.Name, "event", contKnownStatus.String())
		return
	}

	if reason == "" && cont.ApplyingError != nil {
		reason = cont.ApplyingError.Error()
	}
	event := api.ContainerStateChange{
		TaskArn:       task.Arn,
		ContainerName: cont.Name,
		Status:        contKnownStatus.BackendStatus(cont.GetSteadyStateStatus()),
		ExitCode:      cont.GetKnownExitCode(),
		PortBindings:  cont.KnownPortBindings,
		Reason:        reason,
		Container:     cont,
	}
	log.Debug("Container change event", "event", event)
	engine.stateChangeEvents <- event
	log.Debug("Container change event passed on", "event", event)
}

// openEventstream opens, but does not consume, the docker event stream
func (engine *DockerTaskEngine) openEventstream(ctx context.Context) error {
	events, err := engine.client.ContainerEvents(ctx)
	if err != nil {
		return err
	}
	engine.events = events
	return nil
}

// handleDockerEvents must be called after openEventstream; it processes each
// event that it reads from the docker eventstream
func (engine *DockerTaskEngine) handleDockerEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-engine.events:
			engine.handleDockerEvent(event)
		}
	}
}

// handleDockerEvent is the entrypoint for task modifications originating with
// events occurring through Docker, outside the task engine itself.
// handleDockerEvent is responsible for taking an event that correlates to a
// container and placing it in the context of the task to which that container
// belongs.
func (engine *DockerTaskEngine) handleDockerEvent(event DockerContainerChangeEvent) bool {
	log.Debug("Handling a docker event", "event", event)

	task, taskFound := engine.state.TaskByID(event.DockerID)
	cont, containerFound := engine.state.ContainerByID(event.DockerID)
	if !taskFound || !containerFound {
		log.Debug("Event for container not managed", "dockerId", event.DockerID)
		return false
	}
	engine.processTasks.RLock()
	managedTask, ok := engine.managedTasks[task.Arn]
	// hold the lock until the message is sent so we don't send on a closed channel
	defer engine.processTasks.RUnlock()
	if !ok {
		log.Crit("Could not find managed task corresponding to a docker event", "event", event, "task", task)
		return true
	}
	log.Debug("Writing docker event to the associated task", "task", task, "event", event)

	managedTask.dockerMessages <- dockerContainerChange{container: cont.Container, event: event}
	log.Debug("Wrote docker event to the associated task", "task", task, "event", event)
	return true
}

// StateChangeEvents returns channels to read task and container state changes. These
// changes should be read as soon as possible as them not being read will block
// processing the task referenced by the event.
func (engine *DockerTaskEngine) StateChangeEvents() chan statechange.Event {
	return engine.stateChangeEvents
}

// AddTask starts tracking a task
func (engine *DockerTaskEngine) AddTask(task *api.Task) error {
	task.PostUnmarshalTask(engine.cfg, engine.credentialsManager)

	engine.processTasks.Lock()
	defer engine.processTasks.Unlock()

	existingTask, exists := engine.state.TaskByArn(task.Arn)
	if !exists {
		// This will update the container desired status
		task.UpdateDesiredStatus()

		engine.state.AddTask(task)
		if dependencygraph.ValidDependencies(task) {
			engine.startTask(task)
		} else {
			seelog.Errorf("Unable to progress task with circular dependencies, task: %s", task.String())
			task.SetKnownStatus(api.TaskStopped)
			task.SetDesiredStatus(api.TaskStopped)
			err := TaskDependencyError{task.Arn}
			engine.emitTaskEvent(task, err.Error())
		}
		return nil
	}

	// Update task
	engine.updateTask(existingTask, task)

	return nil
}

// ListTasks returns the tasks currently managed by the DockerTaskEngine
func (engine *DockerTaskEngine) ListTasks() ([]*api.Task, error) {
	return engine.state.AllTasks(), nil
}

// GetTaskByArn returns the task identified by that ARN
func (engine *DockerTaskEngine) GetTaskByArn(arn string) (*api.Task, bool) {
	return engine.state.TaskByArn(arn)
}

func (engine *DockerTaskEngine) pullContainer(task *api.Task, container *api.Container) DockerContainerMetadata {
	switch container.Type {
	case api.ContainerCNIPause:
		// ContainerCNIPause image are managed at startup
		return DockerContainerMetadata{}
	case api.ContainerEmptyHostVolume:
		// ContainerEmptyHostVolume image is either local (must be imported) or remote (must be pulled)
		if emptyvolume.LocalImage {
			return engine.client.ImportLocalEmptyVolumeImage()
		}
	}

	// Record the pullStoppedAt timestamp
	defer func() {
		timestamp := engine.time().Now()
		task.SetPullStoppedAt(timestamp)
	}()

	if engine.enableConcurrentPull {
		seelog.Infof("Pulling container %v concurrently. Task: %v", container, task)
		return engine.concurrentPull(task, container)
	}
	seelog.Infof("Pulling container %v serially. Task: %v", container, task)
	return engine.serialPull(task, container)
}

func (engine *DockerTaskEngine) concurrentPull(task *api.Task, container *api.Container) DockerContainerMetadata {
	seelog.Debugf("Attempting to obtain ImagePullDeleteLock to pull image - %s. Task: %v", container.Image, task)
	ImagePullDeleteLock.RLock()
	seelog.Debugf("Acquired ImagePullDeleteLock, start pulling image - %s. Task: %v", container.Image, task)
	defer seelog.Debugf("Released ImagePullDeleteLock after pulling image - %s. Task: %v", container.Image, task)
	defer ImagePullDeleteLock.RUnlock()

	// Record the task pull_started_at timestamp
	pullStart := engine.time().Now()
	defer func(startTime time.Time) {
		seelog.Infof("Finished pulling container %v in %s. Task: %v", container.Image, time.Since(startTime).String(), task)
	}(pullStart)
	ok := task.SetPullStartedAt(pullStart)
	if ok {
		seelog.Infof("Recording timestamp for starting image pull, task %s, time: %s", task.String(), pullStart)
	}

	return engine.pullAndUpdateContainerReference(task, container)
}

func (engine *DockerTaskEngine) serialPull(task *api.Task, container *api.Container) DockerContainerMetadata {
	seelog.Debugf("Attempting to obtain ImagePullDeleteLock to pull image - %s. Task: %v", container.Image, task)
	ImagePullDeleteLock.Lock()
	seelog.Debugf("Acquired ImagePullDeleteLock, start pulling image - %s. Task: %v", container.Image, task)
	defer seelog.Debugf("Released ImagePullDeleteLock after pulling image - %s. Task: %v", container.Image, task)
	defer ImagePullDeleteLock.Unlock()

	pullStart := engine.time().Now()
	defer func(startTime time.Time) {
		seelog.Infof("Finished pulling container %v in %s. Task: %v", container.Image, time.Since(startTime).String(), task)
	}(pullStart)
	ok := task.SetPullStartedAt(pullStart)
	if ok {
		seelog.Infof("Recording timestamp for starting image pull, task %s, time: %s", task.String(), pullStart)
	}

	return engine.pullAndUpdateContainerReference(task, container)
}

func (engine *DockerTaskEngine) pullAndUpdateContainerReference(task *api.Task, container *api.Container) DockerContainerMetadata {
	// If a task is blocked here for some time, and before it starts pulling image,
	// the task's desired status is set to stopped, then don't pull the image
	if task.GetDesiredStatus() == api.TaskStopped {
		seelog.Infof("Task desired status is stopped, skip pull container: %v, task %v", container, task)
		container.SetDesiredStatus(api.ContainerStopped)
		return DockerContainerMetadata{Error: TaskStoppedBeforePullBeginError{task.Arn}}
	}

	// Set the credentials for pull from ECR if necessary
	if container.ShouldPullWithExecutionRole() {
		executionCredentials, ok := engine.credentialsManager.GetTaskCredentials(task.GetExecutionCredentialsID())
		if !ok {
			seelog.Infof("Acquiring ecr credentials from credential manager failed, container: %s, task: %s", container.String(), task.String())
			return DockerContainerMetadata{Error: CannotPullECRContainerError{errors.New("engine ecr credentials acquisition: not found in credential manager")}}
		}

		iamCredentials := executionCredentials.GetIAMRoleCredentials()
		container.SetRegistryAuthCredentials(iamCredentials)
		// Clean up the ECR pull credentials after pulling
		defer container.SetRegistryAuthCredentials(credentials.IAMRoleCredentials{})
	}

	metadata := engine.client.PullImage(container.Image, container.RegistryAuthentication)

	// Don't add internal images(created by ecs-agent) into imagemanger state
	if container.IsInternal() {
		return metadata
	}

	err := engine.imageManager.RecordContainerReference(container)
	if err != nil {
		seelog.Errorf("Error adding container reference to image state: %v", err)
	}
	imageState := engine.imageManager.GetImageStateFromImageName(container.Image)
	engine.state.AddImageState(imageState)
	engine.saver.Save()
	return metadata
}

func (engine *DockerTaskEngine) createContainer(task *api.Task, container *api.Container) DockerContainerMetadata {
	log.Info("Creating container", "task", task, "container", container)
	client := engine.client
	if container.DockerConfig.Version != nil {
		client = client.WithVersion(dockerclient.DockerVersion(*container.DockerConfig.Version))
	}

	dockerContainerName := ""
	containerMap, ok := engine.state.ContainerMapByArn(task.Arn)
	if !ok {
		containerMap = make(map[string]*api.DockerContainer)
	} else {
		// looking for container that has docker name but not created
		for _, v := range containerMap {
			if v.Container.Name == container.Name {
				dockerContainerName = v.DockerName
				break
			}
		}
	}

	// Resolve HostConfig
	// we have to do this in create, not start, because docker no longer handles
	// merging create config with start hostconfig the same; e.g. memory limits
	// get lost
	dockerClientVersion, versionErr := client.APIVersion()
	if versionErr != nil {
		return DockerContainerMetadata{Error: CannotGetDockerClientVersionError{versionErr}}
	}

	hostConfig, hcerr := task.DockerHostConfig(container, containerMap, dockerClientVersion)
	if hcerr != nil {
		return DockerContainerMetadata{Error: api.NamedError(hcerr)}
	}

	if container.AWSLogAuthExecutionRole() {
		hcerr = task.ApplyExecutionRoleLogsAuth(hostConfig, engine.credentialsManager)
		if hcerr != nil {
			return DockerContainerMetadata{Error: api.NamedError(hcerr)}
		}
	}

	config, err := task.DockerConfig(container, dockerClientVersion)
	if err != nil {
		return DockerContainerMetadata{Error: api.NamedError(err)}
	}

	// Augment labels with some metadata from the agent. Explicitly do this last
	// such that it will always override duplicates in the provided raw config
	// data.
	config.Labels[labelPrefix+"task-arn"] = task.Arn
	config.Labels[labelPrefix+"container-name"] = container.Name
	config.Labels[labelPrefix+"task-definition-family"] = task.Family
	config.Labels[labelPrefix+"task-definition-version"] = task.Version
	config.Labels[labelPrefix+"cluster"] = engine.cfg.Cluster

	if dockerContainerName == "" {
		name := ""
		for i := 0; i < len(container.Name); i++ {
			c := container.Name[i]
			if !((c <= '9' && c >= '0') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c == '-')) {
				continue
			}
			name += string(c)
		}

		dockerContainerName = "ecs-" + task.Family + "-" + task.Version + "-" + name + "-" + utils.RandHex()

		// Pre-add the container in case we stop before the next, more useful,
		// AddContainer call. This ensures we have a way to get the container if
		// we die before 'createContainer' returns because we can inspect by
		// name
		engine.state.AddContainer(&api.DockerContainer{DockerName: dockerContainerName, Container: container}, task)
		seelog.Infof("Created container name mapping for task %s - %s -> %s", task.Arn, container.Name, dockerContainerName)
		engine.saver.ForceSave()
	}

	// Create metadata directory and file then populate it with common metadata of all containers of this task
	// Afterwards add this directory to the container's mounts if file creation was successful
	if engine.cfg.ContainerMetadataEnabled && !container.IsInternal() {
		mderr := engine.metadataManager.Create(config, hostConfig, task.Arn, container.Name)
		if mderr != nil {
			seelog.Warnf("Create metadata failed for container %s of task %s: %v", container.Name, task.Arn, mderr)
		}
	}

	metadata := client.CreateContainer(config, hostConfig, dockerContainerName, createContainerTimeout)
	if metadata.DockerID != "" {
		engine.state.AddContainer(&api.DockerContainer{DockerID: metadata.DockerID, DockerName: dockerContainerName, Container: container}, task)
	}
	seelog.Infof("Created docker container for task %s: %s -> %s", task.Arn, container.Name, metadata.DockerID)
	return metadata
}

func (engine *DockerTaskEngine) startContainer(task *api.Task, container *api.Container) DockerContainerMetadata {
	log.Info("Starting container", "task", task, "container", container)
	client := engine.client
	if container.DockerConfig.Version != nil {
		client = client.WithVersion(dockerclient.DockerVersion(*container.DockerConfig.Version))
	}

	containerMap, ok := engine.state.ContainerMapByArn(task.Arn)
	if !ok {
		return DockerContainerMetadata{
			Error: CannotStartContainerError{errors.Errorf("Container belongs to unrecognized task %s", task.Arn)},
		}
	}

	dockerContainer, ok := containerMap[container.Name]
	if !ok {
		return DockerContainerMetadata{
			Error: CannotStartContainerError{errors.Errorf("Container not recorded as created")},
		}
	}
	dockerContainerMD := client.StartContainer(dockerContainer.DockerID, startContainerTimeout)

	// Get metadata through container inspection and available task information then write this to the metadata file
	// Performs this in the background to avoid delaying container start
	// TODO: Add a state to the api.Container for the status of the metadata file (Whether it needs update) and
	// add logic to engine state restoration to do a metadata update for containers that are running after the agent was restarted
	if dockerContainerMD.Error == nil && engine.cfg.ContainerMetadataEnabled && !container.IsInternal() {
		go func() {
			err := engine.metadataManager.Update(dockerContainer.DockerID, task.Arn, container.Name)
			if err != nil {
				seelog.Warnf("Update metadata file failed for container %s of task %s: %v", container.Name, task.Arn, err)
				return
			}
			container.SetMetadataFileUpdated()
			seelog.Debugf("Updated metadata file for container %s of task %s", container.Name, task.Arn)
		}()
	}
	return dockerContainerMD
}

func (engine *DockerTaskEngine) provisionContainerResources(task *api.Task, container *api.Container) DockerContainerMetadata {
	seelog.Infof("Task [%s]: Setting up container resources for container [%s]", task.String(), container.String())
	cniConfig, err := engine.buildCNIConfigFromTaskContainer(task, container)
	if err != nil {
		return DockerContainerMetadata{
			Error: ContainerNetworkingError{errors.Wrap(err, "container resource provisioning: unable to build cni configuration")},
		}
	}
	// Invoke the libcni to config the network namespace for the container
	err = engine.cniClient.SetupNS(cniConfig)
	if err != nil {
		seelog.Errorf("Set up pause container namespace failed, err: %v, task: %s", err, task.String())
		return DockerContainerMetadata{
			DockerID: cniConfig.ContainerID,
			Error:    ContainerNetworkingError{errors.Wrap(err, "container resource provisioning: failed to setup network namespace")},
		}
	}

	return DockerContainerMetadata{
		DockerID: cniConfig.ContainerID,
	}
}

// releaseIPInIPAM marks the ip avaialble in the ipam db
func (engine *DockerTaskEngine) releaseIPInIPAM(task *api.Task) error {
	seelog.Infof("Releasing ip in the ipam, task: %s", task.Arn)
	cfg, err := task.BuildCNIConfig()
	if err != nil {
		return errors.Wrapf(err, "engine: build cni configuration from task failed")
	}

	return engine.cniClient.ReleaseIPResource(cfg)
}

// cleanupPauseContainerNetwork will clean up the network namespace of pause container
func (engine *DockerTaskEngine) cleanupPauseContainerNetwork(task *api.Task, container *api.Container) error {
	seelog.Infof("Task [%s]: Cleaning up the network namespace", task.String())

	cniConfig, err := engine.buildCNIConfigFromTaskContainer(task, container)
	if err != nil {
		return errors.Wrapf(err, "engine: failed cleanup task network namespace, task: %s", task.String())
	}

	return engine.cniClient.CleanupNS(cniConfig)
}

func (engine *DockerTaskEngine) buildCNIConfigFromTaskContainer(task *api.Task, container *api.Container) (*ecscni.Config, error) {
	cfg, err := task.BuildCNIConfig()
	if err != nil {
		return nil, errors.Wrapf(err, "engine: build cni configuration from task failed")
	}

	if engine.cfg.OverrideAWSVPCLocalIPv4Address != nil &&
		len(engine.cfg.OverrideAWSVPCLocalIPv4Address.IP) != 0 &&
		len(engine.cfg.OverrideAWSVPCLocalIPv4Address.Mask) != 0 {
		cfg.IPAMV4Address = engine.cfg.OverrideAWSVPCLocalIPv4Address
	}

	if len(engine.cfg.AWSVPCAdditionalLocalRoutes) != 0 {
		cfg.AdditionalLocalRoutes = engine.cfg.AWSVPCAdditionalLocalRoutes
	}

	// Get the pid of container
	containers, ok := engine.state.ContainerMapByArn(task.Arn)
	if !ok {
		return nil, errors.New("engine: failed to find the pause container, no containers in the task")
	}

	pauseContainer, ok := containers[container.Name]
	if !ok {
		return nil, errors.New("engine: failed to find the pause container")
	}
	containerInspectOutput, err := engine.client.InspectContainer(pauseContainer.DockerName, inspectContainerTimeout)
	if err != nil {
		return nil, err
	}

	cfg.ContainerPID = strconv.Itoa(containerInspectOutput.State.Pid)
	cfg.ContainerID = containerInspectOutput.ID
	cfg.BlockInstanceMetdata = engine.cfg.AWSVPCBlockInstanceMetdata

	return cfg, nil
}

func (engine *DockerTaskEngine) stopContainer(task *api.Task, container *api.Container) DockerContainerMetadata {
	seelog.Infof("Stopping container, container: %s, task: %s", container.String(), task.String())
	containerMap, ok := engine.state.ContainerMapByArn(task.Arn)
	if !ok {
		return DockerContainerMetadata{
			Error: CannotStopContainerError{errors.Errorf("Container belongs to unrecognized task %s", task.Arn)},
		}
	}

	dockerContainer, ok := containerMap[container.Name]
	if !ok {
		return DockerContainerMetadata{
			Error: CannotStopContainerError{errors.Errorf("Container not recorded as created")},
		}
	}

	// Cleanup the pause container network namespace before stop the container
	if container.Type == api.ContainerCNIPause {
		err := engine.cleanupPauseContainerNetwork(task, container)
		if err != nil {
			seelog.Errorf("Engine: cleanup pause container network namespace error, task: %s", task.String())
		}
		seelog.Infof("Cleaned pause container network namespace, task: %s", task.String())
	}

	return engine.client.StopContainer(dockerContainer.DockerID, stopContainerTimeout)
}

func (engine *DockerTaskEngine) removeContainer(task *api.Task, container *api.Container) error {
	log.Info("Removing container", "task", task, "container", container)
	containerMap, ok := engine.state.ContainerMapByArn(task.Arn)

	if !ok {
		return errors.New("No such task: " + task.Arn)
	}

	dockerContainer, ok := containerMap[container.Name]
	if !ok {
		return errors.New("No container named '" + container.Name + "' created in " + task.Arn)
	}

	return engine.client.RemoveContainer(dockerContainer.DockerName, removeContainerTimeout)
}

// updateTask determines if a new transition needs to be applied to the
// referenced task, and if needed applies it. It should not be called anywhere
// but from 'AddTask' and is protected by the processTasks lock there.
func (engine *DockerTaskEngine) updateTask(task *api.Task, update *api.Task) {
	managedTask, ok := engine.managedTasks[task.Arn]
	if !ok {
		log.Crit("ACS message for a task we thought we managed, but don't!  Aborting.", "arn", task.Arn)
		return
	}
	// Keep the lock because sequence numbers cannot be correct unless they are
	// also read in the order addtask was called
	// This does block the engine's ability to ingest any new events (including
	// stops for past tasks, ack!), but this is necessary for correctness
	updateDesiredStatus := update.GetDesiredStatus()
	log.Debug("Putting update on the acs channel", "task", task.Arn, "status", updateDesiredStatus, "seqnum", update.StopSequenceNumber)
	transition := acsTransition{desiredStatus: updateDesiredStatus}
	transition.seqnum = update.StopSequenceNumber
	managedTask.acsMessages <- transition
	log.Debug("Update was taken off the acs channel", "task", task.Arn, "status", updateDesiredStatus)
}

// transitionContainer calls applyContainerState, and then notifies the managed
// task of the change.  transitionContainer is called by progressContainers and
// by handleStoppedToRunningContainerTransition.
func (engine *DockerTaskEngine) transitionContainer(task *api.Task, container *api.Container, to api.ContainerStatus) {
	// Let docker events operate async so that we can continue to handle ACS / other requests
	// This is safe because 'applyContainerState' will not mutate the task
	metadata := engine.applyContainerState(task, container, to)

	engine.processTasks.RLock()
	managedTask, ok := engine.managedTasks[task.Arn]
	if ok {
		managedTask.dockerMessages <- dockerContainerChange{
			container: container,
			event: DockerContainerChangeEvent{
				Status:                  to,
				DockerContainerMetadata: metadata,
			},
		}
	}
	engine.processTasks.RUnlock()
}

// applyContainerState moves the container to the given state by calling the
// function defined in the transitionFunctionMap for the state
func (engine *DockerTaskEngine) applyContainerState(task *api.Task, container *api.Container, nextState api.ContainerStatus) DockerContainerMetadata {
	clog := log.New("task", task, "container", container)
	transitionFunction, ok := engine.transitionFunctionMap()[nextState]
	if !ok {
		clog.Crit("Container desired to transition to an unsupported state", "state", nextState.String())
		return DockerContainerMetadata{Error: &impossibleTransitionError{nextState}}
	}
	metadata := transitionFunction(task, container)
	if metadata.Error != nil {
		clog.Info("Error transitioning container", "state", nextState.String(), "error", metadata.Error)
	} else {
		clog.Debug("Transitioned container", "state", nextState.String())
		engine.saver.Save()
	}
	return metadata
}

// transitionFunctionMap provides the logic for the simple state machine of the
// DockerTaskEngine. Each desired state maps to a function that can be called
// to try and move the task to that desired state.
func (engine *DockerTaskEngine) transitionFunctionMap() map[api.ContainerStatus]transitionApplyFunc {
	return engine.containerStatusToTransitionFunction
}

type transitionApplyFunc (func(*api.Task, *api.Container) DockerContainerMetadata)

// State is a function primarily meant for testing usage; it is explicitly not
// part of the TaskEngine interface and should not be relied upon.
// It returns an internal representation of the state of this DockerTaskEngine.
func (engine *DockerTaskEngine) State() dockerstate.TaskEngineState {
	return engine.state
}

// Version returns the underlying docker version.
func (engine *DockerTaskEngine) Version() (string, error) {
	return engine.client.Version()
}

// isParallelPullCompatible checks the docker version and return true if docker version >= 1.11.1
func (engine *DockerTaskEngine) isParallelPullCompatible() bool {
	version, err := engine.Version()
	if err != nil {
		seelog.Warnf("Failed to get docker version, err %v", err)
		return false
	}

	match, err := utils.Version(version).Matches(">=1.11.1")
	if err != nil {
		seelog.Warnf("Could not compare docker version, err %v", err)
		return false
	}

	if match {
		seelog.Debugf("Docker version: %v, enable concurrent pulling", version)
		return true
	}

	return false
}

func (engine *DockerTaskEngine) updateMetadataFile(task *api.Task, cont *api.DockerContainer) {
	err := engine.metadataManager.Update(cont.DockerID, task.Arn, cont.Container.Name)
	if err != nil {
		seelog.Errorf("Update metadata file failed for container %s of task %s: %v", cont.Container.Name, task.Arn, err)
	} else {
		cont.Container.SetMetadataFileUpdated()
		seelog.Debugf("Updated metadata file for container %s of task %s", cont.Container.Name, task.Arn)
	}
}
