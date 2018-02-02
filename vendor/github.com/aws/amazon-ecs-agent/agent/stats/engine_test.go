//+build !integration
// Copyright 2014-2016 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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
	"testing"

	"github.com/aws/amazon-ecs-agent/agent/api"
	ecsengine "github.com/aws/amazon-ecs-agent/agent/engine"
	mock_resolver "github.com/aws/amazon-ecs-agent/agent/stats/resolver/mock"
	docker "github.com/fsouza/go-dockerclient"
	"github.com/golang/mock/gomock"
)

func TestStatsEngineAddRemoveContainers(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	resolver := mock_resolver.NewMockContainerMetadataResolver(ctrl)
	mockDockerClient := ecsengine.NewMockDockerClient(ctrl)
	t1 := &api.Task{Arn: "t1", Family: "f1"}
	t2 := &api.Task{Arn: "t2", Family: "f2"}
	t3 := &api.Task{Arn: "t3"}
	resolver.EXPECT().ResolveTask("c1").AnyTimes().Return(t1, nil)
	resolver.EXPECT().ResolveTask("c2").AnyTimes().Return(t1, nil)
	resolver.EXPECT().ResolveTask("c3").AnyTimes().Return(t2, nil)
	resolver.EXPECT().ResolveTask("c4").AnyTimes().Return(nil, fmt.Errorf("unmapped container"))
	resolver.EXPECT().ResolveTask("c5").AnyTimes().Return(t2, nil)
	resolver.EXPECT().ResolveTask("c6").AnyTimes().Return(t3, nil)
	resolver.EXPECT().ResolveContainer(gomock.Any()).AnyTimes().Return(&api.DockerContainer{
		Container: &api.Container{},
	}, nil)
	mockStatsChannel := make(chan *docker.Stats)
	defer close(mockStatsChannel)
	mockDockerClient.EXPECT().Stats(gomock.Any(), gomock.Any()).Return(mockStatsChannel, nil).AnyTimes()

	engine := NewDockerStatsEngine(&cfg, nil, eventStream("TestStatsEngineAddRemoveContainers"))
	engine.resolver = resolver
	engine.client = mockDockerClient
	engine.cluster = defaultCluster
	engine.containerInstanceArn = defaultContainerInstance
	defer engine.removeAll()

	engine.addContainer("c1")
	engine.addContainer("c1")

	if len(engine.tasksToContainers) != 1 {
		t.Errorf("Adding containers failed. Expected num tasks = 1, got: %d", len(engine.tasksToContainers))
	}

	containers, _ := engine.tasksToContainers["t1"]
	if len(containers) != 1 {
		t.Error("Adding duplicate containers failed.")
	}
	_, exists := containers["c1"]
	if !exists {
		t.Error("Container c1 not found in engine")
	}

	engine.addContainer("c2")
	containers, _ = engine.tasksToContainers["t1"]
	_, exists = containers["c2"]
	if !exists {
		t.Error("Container c2 not found in engine")
	}

	for _, statsContainer := range containers {
		for _, fakeContainerStats := range createFakeContainerStats() {
			statsContainer.statsQueue.Add(fakeContainerStats)
		}
	}

	// Ensure task shows up in metrics.
	containerMetrics, err := engine.getContainerMetricsForTask("t1")
	if err != nil {
		t.Errorf("Error getting container metrics: %v", err)
	}
	err = validateContainerMetrics(containerMetrics, 2)
	if err != nil {
		t.Errorf("Error validating container metrics: %v", err)
	}

	metadata, taskMetrics, err := engine.GetInstanceMetrics()
	if err != nil {
		t.Errorf("Error gettting instance metrics: %v", err)
	}

	err = validateMetricsMetadata(metadata)
	if err != nil {
		t.Errorf("Error validating metadata: %v", err)
	}
	if len(taskMetrics) != 1 {
		t.Errorf("Incorrect number of tasks. Expected: 1, got: %d", len(taskMetrics))
	}
	err = validateContainerMetrics(taskMetrics[0].ContainerMetrics, 2)
	if err != nil {
		t.Errorf("Error validating container metrics: %v", err)
	}
	if *taskMetrics[0].TaskArn != "t1" {
		t.Errorf("Incorrect task arn. Expected: t1, got: %s", *taskMetrics[0].TaskArn)
	}

	// Ensure that only valid task shows up in metrics.
	_, err = engine.getContainerMetricsForTask("t2")
	if err == nil {
		t.Error("Expected non-empty error for non existent task")
	}

	engine.removeContainer("c1")
	containers, _ = engine.tasksToContainers["t1"]
	_, exists = containers["c1"]
	if exists {
		t.Error("Container c1 not removed from engine")
	}
	engine.removeContainer("c2")
	containers, _ = engine.tasksToContainers["t1"]
	_, exists = containers["c2"]
	if exists {
		t.Error("Container c2 not removed from engine")
	}
	engine.addContainer("c3")
	containers, _ = engine.tasksToContainers["t2"]
	_, exists = containers["c3"]
	if !exists {
		t.Error("Container c3 not found in engine")
	}

	_, _, err = engine.GetInstanceMetrics()
	if err == nil {
		t.Error("Expected non-empty error for empty stats.")
	}
	engine.removeContainer("c3")

	// Should get an error while adding this container due to unmapped
	// container to task.
	engine.addContainer("c4")
	err = validateIdleContainerMetrics(engine)
	if err != nil {
		t.Fatalf("Error validating metadata: %v", err)
	}

	// Should get an error while adding this container due to unmapped
	// task arn to task definition family.
	engine.addContainer("c6")
	err = validateIdleContainerMetrics(engine)
	if err != nil {
		t.Fatalf("Error validating metadata: %v", err)
	}
}

func TestStatsEngineMetadataInStatsSets(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()
	resolver := mock_resolver.NewMockContainerMetadataResolver(mockCtrl)
	mockDockerClient := ecsengine.NewMockDockerClient(mockCtrl)
	t1 := &api.Task{Arn: "t1", Family: "f1"}
	resolver.EXPECT().ResolveTask("c1").AnyTimes().Return(t1, nil)
	resolver.EXPECT().ResolveContainer(gomock.Any()).AnyTimes().Return(&api.DockerContainer{
		Container: &api.Container{},
	}, nil)
	mockDockerClient.EXPECT().Stats(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()

	engine := NewDockerStatsEngine(&cfg, nil, eventStream("TestStatsEngineMetadataInStatsSets"))
	engine.resolver = resolver
	engine.cluster = defaultCluster
	engine.containerInstanceArn = defaultContainerInstance
	engine.client = mockDockerClient
	engine.addContainer("c1")
	containerStats := []*ContainerStats{
		{22400432, 1839104, parseNanoTime("2015-02-12T21:22:05.131117533Z")},
		{116499979, 3649536, parseNanoTime("2015-02-12T21:22:05.232291187Z")},
	}
	containers, _ := engine.tasksToContainers["t1"]
	for _, statsContainer := range containers {
		for i := 0; i < 2; i++ {
			statsContainer.statsQueue.Add(containerStats[i])
		}
	}
	metadata, taskMetrics, err := engine.GetInstanceMetrics()
	if err != nil {
		t.Errorf("Error gettting instance metrics: %v", err)
	}
	if len(taskMetrics) != 1 {
		t.Fatalf("Incorrect number of tasks. Expected: 1, got: %d", len(taskMetrics))
	}
	err = validateContainerMetrics(taskMetrics[0].ContainerMetrics, 1)
	if err != nil {
		t.Errorf("Error validating container metrics: %v", err)
	}
	if *taskMetrics[0].TaskArn != "t1" {
		t.Errorf("Incorrect task arn. Expected: t1, got: %s", *taskMetrics[0].TaskArn)
	}
	err = validateMetricsMetadata(metadata)
	if err != nil {
		t.Errorf("Error validating metadata: %v", err)
	}

	engine.removeContainer("c1")
	err = validateIdleContainerMetrics(engine)
	if err != nil {
		t.Fatalf("Error validating metadata: %v", err)
	}
}

func TestStatsEngineInvalidTaskEngine(t *testing.T) {
	statsEngine := NewDockerStatsEngine(&cfg, nil, eventStream("TestStatsEngineInvalidTaskEngine"))
	taskEngine := &MockTaskEngine{}
	err := statsEngine.MustInit(taskEngine, "", "")
	if err == nil {
		t.Error("Expected error in engine initialization, got nil")
	}
}

func TestStatsEngineUninitialized(t *testing.T) {
	engine := NewDockerStatsEngine(&cfg, nil, eventStream("TestStatsEngineUninitialized"))
	defer engine.removeAll()

	engine.resolver = &DockerContainerMetadataResolver{}
	engine.cluster = defaultCluster
	engine.containerInstanceArn = defaultContainerInstance
	engine.addContainer("c1")
	err := validateIdleContainerMetrics(engine)
	if err != nil {
		t.Fatalf("Error validating metadata: %v", err)
	}
}

func TestStatsEngineTerminalTask(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()
	resolver := mock_resolver.NewMockContainerMetadataResolver(mockCtrl)
	resolver.EXPECT().ResolveTask("c1").Return(&api.Task{
		Arn:               "t1",
		KnownStatusUnsafe: api.TaskStopped,
		Family:            "f1",
	}, nil)
	engine := NewDockerStatsEngine(&cfg, nil, eventStream("TestStatsEngineTerminalTask"))
	defer engine.removeAll()

	engine.cluster = defaultCluster
	engine.containerInstanceArn = defaultContainerInstance
	engine.resolver = resolver

	engine.addContainer("c1")
	err := validateIdleContainerMetrics(engine)
	if err != nil {
		t.Fatalf("Error validating metadata: %v", err)
	}
}
