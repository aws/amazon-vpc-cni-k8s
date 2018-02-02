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
	"time"

	ecsengine "github.com/aws/amazon-ecs-agent/agent/engine"
	"github.com/aws/amazon-ecs-agent/agent/stats/resolver"
	"golang.org/x/net/context"
)

// ContainerStats encapsulates the raw CPU and memory utilization from cgroup fs.
type ContainerStats struct {
	cpuUsage    uint64
	memoryUsage uint64
	timestamp   time.Time
}

// UsageStats abstracts the format in which the queue stores data.
type UsageStats struct {
	CPUUsagePerc      float32   `json:"cpuUsagePerc"`
	MemoryUsageInMegs uint32    `json:"memoryUsageInMegs"`
	Timestamp         time.Time `json:"timestamp"`
	cpuUsage          uint64
}

// ContainerMetadata contains meta-data information for a container.
type ContainerMetadata struct {
	DockerID string `json:"-"`
}

// StatsContainer abstracts methods to gather and aggregate utilization data for a container.
type StatsContainer struct {
	containerMetadata *ContainerMetadata
	ctx               context.Context
	cancel            context.CancelFunc
	client            ecsengine.DockerClient
	statsQueue        *Queue
	resolver          resolver.ContainerMetadataResolver
}

// taskDefinition encapsulates family and version strings for a task definition
type taskDefinition struct {
	family  string
	version string
}
