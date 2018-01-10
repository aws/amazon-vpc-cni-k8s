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

import (
	"fmt"
	"os"
	"time"

	"github.com/aws/amazon-ecs-agent/agent/api"
	"github.com/aws/amazon-ecs-agent/agent/config"
	"github.com/aws/amazon-ecs-agent/agent/ecs_client/model/ecs"
	ecsengine "github.com/aws/amazon-ecs-agent/agent/engine"
	"github.com/aws/amazon-ecs-agent/agent/engine/dockerclient"
	"github.com/aws/amazon-ecs-agent/agent/eventstream"
	"github.com/aws/amazon-ecs-agent/agent/statechange"
	"github.com/aws/amazon-ecs-agent/agent/statemanager"
	"github.com/aws/amazon-ecs-agent/agent/tcs/model/ecstcs"
	"github.com/aws/amazon-ecs-agent/agent/utils"
	docker "github.com/fsouza/go-dockerclient"

	"golang.org/x/net/context"
)

const (
	// checkPointSleep is the sleep duration in milliseconds between
	// starting/stopping containers in the test code.
	checkPointSleep = 5 * SleepBetweenUsageDataCollection
	testImageName   = "amazon/amazon-ecs-gremlin:make"

	// defaultDockerTimeoutSeconds is the timeout for dialing the docker remote API.
	defaultDockerTimeoutSeconds uint = 10

	// waitForCleanupSleep is the sleep duration in milliseconds
	// for the waiting after container cleanup before checking the state of the manager.
	waitForCleanupSleep = 10 * time.Millisecond

	taskArn               = "gremlin"
	taskDefinitionFamily  = "docker-gremlin"
	taskDefinitionVersion = "1"
	containerName         = "gremlin-container"
)

var endpoint = utils.DefaultIfBlank(os.Getenv(ecsengine.DockerEndpointEnvVariable), ecsengine.DockerDefaultEndpoint)

var client, _ = docker.NewClient(endpoint)
var clientFactory = dockerclient.NewFactory(endpoint)
var cfg = config.DefaultConfig()

var defaultCluster = "default"
var defaultContainerInstance = "ci"

func init() {
	cfg.EngineAuthData = config.NewSensitiveRawMessage([]byte{})
}

// eventStream returns the event stream used to receive container change events
func eventStream(name string) *eventstream.EventStream {
	eventStream := eventstream.NewEventStream(name, context.Background())
	eventStream.StartListening()
	return eventStream
}

// createGremlin creates the gremlin container using the docker client.
// It is used only in the test code.
func createGremlin(client *docker.Client) (*docker.Container, error) {
	container, err := client.CreateContainer(docker.CreateContainerOptions{
		Config: &docker.Config{
			Image: testImageName,
		},
	})

	return container, err
}

type IntegContainerMetadataResolver struct {
	containerIDToTask            map[string]*api.Task
	containerIDToDockerContainer map[string]*api.DockerContainer
}

func newIntegContainerMetadataResolver() *IntegContainerMetadataResolver {
	resolver := IntegContainerMetadataResolver{
		containerIDToTask:            make(map[string]*api.Task),
		containerIDToDockerContainer: make(map[string]*api.DockerContainer),
	}

	return &resolver
}

func (resolver *IntegContainerMetadataResolver) ResolveTask(containerID string) (*api.Task, error) {
	task, exists := resolver.containerIDToTask[containerID]
	if !exists {
		return nil, fmt.Errorf("unmapped container")
	}

	return task, nil
}

func (resolver *IntegContainerMetadataResolver) ResolveContainer(containerID string) (*api.DockerContainer, error) {
	container, exists := resolver.containerIDToDockerContainer[containerID]
	if !exists {
		return nil, fmt.Errorf("unmapped container")
	}

	return container, nil
}

func validateContainerMetrics(containerMetrics []*ecstcs.ContainerMetric, expected int) error {
	if len(containerMetrics) != expected {
		return fmt.Errorf("Mismatch in number of ContainerStatsSet elements. Expected: %d, Got: %d", expected, len(containerMetrics))
	}
	for _, containerMetric := range containerMetrics {
		if containerMetric.CpuStatsSet == nil {
			return fmt.Errorf("CPUStatsSet is nil")
		}
		if containerMetric.MemoryStatsSet == nil {
			return fmt.Errorf("MemoryStatsSet is nil")
		}
	}
	return nil
}

func validateIdleContainerMetrics(engine *DockerStatsEngine) error {
	metadata, taskMetrics, err := engine.GetInstanceMetrics()
	if err != nil {
		return err
	}
	err = validateMetricsMetadata(metadata)
	if err != nil {
		return err
	}
	if !*metadata.Idle {
		return fmt.Errorf("Expected idle metadata to be true")
	}
	if !*metadata.Fin {
		return fmt.Errorf("Fin not set to true when idle")
	}
	if len(taskMetrics) != 0 {
		return fmt.Errorf("Expected empty task metrics, got a list of length: %d", len(taskMetrics))
	}

	return nil
}

func validateMetricsMetadata(metadata *ecstcs.MetricsMetadata) error {
	if metadata == nil {
		return fmt.Errorf("Metadata is nil")
	}
	if *metadata.Cluster != defaultCluster {
		return fmt.Errorf("Expected cluster in metadata to be: %s, got %s", defaultCluster, *metadata.Cluster)
	}
	if *metadata.ContainerInstance != defaultContainerInstance {
		return fmt.Errorf("Expected container instance in metadata to be %s, got %s", defaultContainerInstance, *metadata.ContainerInstance)
	}
	if len(*metadata.MessageId) == 0 {
		return fmt.Errorf("Empty MessageId")
	}

	return nil
}

func createFakeContainerStats() []*ContainerStats {
	return []*ContainerStats{
		{22400432, 1839104, parseNanoTime("2015-02-12T21:22:05.131117533Z")},
		{116499979, 3649536, parseNanoTime("2015-02-12T21:22:05.232291187Z")},
	}
}

type MockTaskEngine struct {
}

func (engine *MockTaskEngine) GetAdditionalAttributes() []*ecs.Attribute {
	return nil
}

func (engine *MockTaskEngine) Init(ctx context.Context) error {
	return nil
}
func (engine *MockTaskEngine) MustInit(ctx context.Context) {
}

func (engine *MockTaskEngine) StateChangeEvents() chan statechange.Event {
	return make(chan statechange.Event)
}

func (engine *MockTaskEngine) SetSaver(statemanager.Saver) {
}

func (engine *MockTaskEngine) AddTask(*api.Task) error {
	return nil
}

func (engine *MockTaskEngine) ListTasks() ([]*api.Task, error) {
	return nil, nil
}

func (engine *MockTaskEngine) GetTaskByArn(arn string) (*api.Task, bool) {
	return nil, false
}

func (engine *MockTaskEngine) UnmarshalJSON([]byte) error {
	return nil
}

func (engine *MockTaskEngine) MarshalJSON() ([]byte, error) {
	return make([]byte, 0), nil
}

func (engine *MockTaskEngine) Version() (string, error) {
	return "", nil
}

func (engine *MockTaskEngine) Capabilities() []*ecs.Attribute {
	return nil
}

func (engine *MockTaskEngine) Disable() {
}
