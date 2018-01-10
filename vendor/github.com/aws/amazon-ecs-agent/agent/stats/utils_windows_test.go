// +build windows,!integration

// Copyright 2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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
	"testing"

	docker "github.com/fsouza/go-dockerclient"
)

func TestDockerStatsToContainerStatsZeroCoresGeneratesError(t *testing.T) {
	numCores = uint64(0)
	jsonStat := fmt.Sprintf(`
		{
			"cpu_stats":{
				"cpu_usage":{
					"total_usage":%d
				}
			}
		}`, 100)
	dockerStat := &docker.Stats{}
	json.Unmarshal([]byte(jsonStat), dockerStat)
	_, err := dockerStatsToContainerStats(dockerStat)
	if err == nil {
		t.Error("Expected error converting container stats with zero cpu cores")
	}
}
