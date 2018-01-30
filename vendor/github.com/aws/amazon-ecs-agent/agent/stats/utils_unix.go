// +build !windows
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

	"github.com/cihub/seelog"
	docker "github.com/fsouza/go-dockerclient"
)

// dockerStatsToContainerStats returns a new object of the ContainerStats object from docker stats.
func dockerStatsToContainerStats(dockerStats *docker.Stats) (*ContainerStats, error) {
	// The length of PercpuUsage represents the number of cores in an instance.
	if len(dockerStats.CPUStats.CPUUsage.PercpuUsage) == 0 || numCores == uint64(0) {
		seelog.Debug("Invalid container statistics reported, no cpu core usage reported")
		return nil, fmt.Errorf("Invalid container statistics reported, no cpu core usage reported")
	}

	cpuUsage := dockerStats.CPUStats.CPUUsage.TotalUsage / numCores
	memoryUsage := dockerStats.MemoryStats.Usage - dockerStats.MemoryStats.Stats.Cache
	return &ContainerStats{
		cpuUsage:    cpuUsage,
		memoryUsage: memoryUsage,
		timestamp:   dockerStats.Read,
	}, nil
}
