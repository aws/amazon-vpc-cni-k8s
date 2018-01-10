//+build !integration
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
	"encoding/json"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/aws/amazon-ecs-agent/agent/api"
	ecsengine "github.com/aws/amazon-ecs-agent/agent/engine"
	mock_resolver "github.com/aws/amazon-ecs-agent/agent/stats/resolver/mock"
	docker "github.com/fsouza/go-dockerclient"
	"github.com/golang/mock/gomock"
	"golang.org/x/net/context"
)

type StatTestData struct {
	timestamp time.Time
	cpuTime   uint64
	memBytes  uint64
}

var statsData = []*StatTestData{
	{parseNanoTime("2015-02-12T21:22:05.131117533Z"), 22400432, 1839104},
	{parseNanoTime("2015-02-12T21:22:05.232291187Z"), 116499979, 3649536},
	{parseNanoTime("2015-02-12T21:22:05.333776335Z"), 248503503, 3649536},
	{parseNanoTime("2015-02-12T21:22:05.434753595Z"), 372167097, 3649536},
	{parseNanoTime("2015-02-12T21:22:05.535746779Z"), 502862518, 3649536},
	{parseNanoTime("2015-02-12T21:22:05.638709495Z"), 638485801, 3649536},
	{parseNanoTime("2015-02-12T21:22:05.739985398Z"), 780707806, 3649536},
	{parseNanoTime("2015-02-12T21:22:05.840941705Z"), 911624529, 3649536},
}

func TestContainerStatsCollection(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockDockerClient := ecsengine.NewMockDockerClient(ctrl)

	dockerID := "container1"
	ctx, cancel := context.WithCancel(context.TODO())
	statChan := make(chan *docker.Stats)
	mockDockerClient.EXPECT().Stats(dockerID, ctx).Return(statChan, nil)
	go func() {
		for _, stat := range statsData {
			// doing this with json makes me sad, but is the easiest way to
			// deal with the docker.Stats.MemoryStats inner struct
			jsonStat := fmt.Sprintf(`
				{
					"memory_stats": {"usage":%d, "privateworkingset":%d},
					"cpu_stats":{
						"cpu_usage":{
							"percpu_usage":[%d],
							"total_usage":%d
						}
					}
				}`, stat.memBytes, stat.memBytes, stat.cpuTime, stat.cpuTime)
			dockerStat := &docker.Stats{}
			json.Unmarshal([]byte(jsonStat), dockerStat)
			dockerStat.Read = stat.timestamp
			statChan <- dockerStat
		}
	}()

	container := &StatsContainer{
		containerMetadata: &ContainerMetadata{
			DockerID: dockerID,
		},
		ctx:    ctx,
		cancel: cancel,
		client: mockDockerClient,
	}
	container.StartStatsCollection()
	time.Sleep(checkPointSleep)
	container.StopStatsCollection()
	cpuStatsSet, err := container.statsQueue.GetCPUStatsSet()
	if err != nil {
		t.Fatal("Error gettting cpu stats set:", err)
	}
	if *cpuStatsSet.Min == math.MaxFloat64 || math.IsNaN(*cpuStatsSet.Min) {
		t.Error("Min value incorrectly set: ", *cpuStatsSet.Min)
	}
	if *cpuStatsSet.Max == -math.MaxFloat64 || math.IsNaN(*cpuStatsSet.Max) {
		t.Error("Max value incorrectly set: ", *cpuStatsSet.Max)
	}
	if *cpuStatsSet.SampleCount == 0 {
		t.Error("Samplecount is 0")
	}
	if *cpuStatsSet.Sum == 0 {
		t.Error("Sum value incorrectly set: ", *cpuStatsSet.Sum)
	}

	memStatsSet, err := container.statsQueue.GetMemoryStatsSet()
	if err != nil {
		t.Error("Error gettting cpu stats set:", err)
	}
	if *memStatsSet.Min == math.MaxFloat64 {
		t.Error("Min value incorrectly set: ", *memStatsSet.Min)
	}
	if *memStatsSet.Max == 0 {
		t.Error("Max value incorrectly set: ", *memStatsSet.Max)
	}
	if *memStatsSet.SampleCount == 0 {
		t.Error("Samplecount is 0")
	}
	if *memStatsSet.Sum == 0 {
		t.Error("Sum value incorrectly set: ", *memStatsSet.Sum)
	}
}

func TestContainerStatsCollectionReconnection(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockDockerClient := ecsengine.NewMockDockerClient(ctrl)
	resolver := mock_resolver.NewMockContainerMetadataResolver(ctrl)

	dockerID := "container1"
	ctx, cancel := context.WithCancel(context.TODO())

	statChan := make(chan *docker.Stats)
	statErr := fmt.Errorf("test error")
	closedChan := make(chan *docker.Stats)
	close(closedChan)

	mockContainer := &api.DockerContainer{
		DockerID: dockerID,
		Container: &api.Container{
			KnownStatusUnsafe: api.ContainerRunning,
		},
	}
	gomock.InOrder(
		mockDockerClient.EXPECT().Stats(dockerID, ctx).Return(nil, statErr),
		resolver.EXPECT().ResolveContainer(dockerID).Return(mockContainer, nil),
		mockDockerClient.EXPECT().Stats(dockerID, ctx).Return(closedChan, nil),
		resolver.EXPECT().ResolveContainer(dockerID).Return(mockContainer, nil),
		mockDockerClient.EXPECT().Stats(dockerID, ctx).Return(statChan, nil),
	)

	container := &StatsContainer{
		containerMetadata: &ContainerMetadata{
			DockerID: dockerID,
		},
		ctx:      ctx,
		cancel:   cancel,
		client:   mockDockerClient,
		resolver: resolver,
	}
	container.StartStatsCollection()
	time.Sleep(checkPointSleep)
	container.StopStatsCollection()
}

func TestContainerStatsCollectionStopsIfContainerIsTerminal(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockDockerClient := ecsengine.NewMockDockerClient(ctrl)
	resolver := mock_resolver.NewMockContainerMetadataResolver(ctrl)

	dockerID := "container1"
	ctx, cancel := context.WithCancel(context.TODO())

	closedChan := make(chan *docker.Stats)
	close(closedChan)

	statsErr := fmt.Errorf("test error")
	mockContainer := &api.DockerContainer{
		DockerID: dockerID,
		Container: &api.Container{
			KnownStatusUnsafe: api.ContainerStopped,
		},
	}
	gomock.InOrder(
		mockDockerClient.EXPECT().Stats(dockerID, ctx).Return(closedChan, nil),
		resolver.EXPECT().ResolveContainer(dockerID).Return(mockContainer, statsErr),
	)

	container := &StatsContainer{
		containerMetadata: &ContainerMetadata{
			DockerID: dockerID,
		},
		ctx:      ctx,
		cancel:   cancel,
		client:   mockDockerClient,
		resolver: resolver,
	}
	container.StartStatsCollection()
	select {
	case <-ctx.Done():
	}
}
