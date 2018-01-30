// +build windows

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

package api

import (
	"encoding/json"
	"testing"

	"github.com/aws/amazon-ecs-agent/agent/acs/model/ecsacs"
	"github.com/aws/amazon-ecs-agent/agent/config"
	"github.com/fsouza/go-dockerclient"
	"github.com/stretchr/testify/assert"
)

const (
	emptyVolumeName1                  = "Empty-Volume-1"
	emptyVolumeContainerPath1         = `C:\my\empty-volume-1`
	expectedEmptyVolumeGeneratedPath1 = `c:\ecs-empty-volume\empty-volume-1`

	emptyVolumeName2                  = "empty-volume-2"
	emptyVolumeContainerPath2         = `C:\my\empty-volume-2`
	expectedEmptyVolumeGeneratedPath2 = `c:\ecs-empty-volume\` + emptyVolumeName2

	expectedEmptyVolumeContainerImage = "microsoft/nanoserver"
	expectedEmptyVolumeContainerTag   = "latest"
	expectedEmptyVolumeContainerCmd   = "not-applicable"

	expectedMemorySwappinessDefault = memorySwappinessDefault
)

func TestPostUnmarshalWindowsCanonicalPaths(t *testing.T) {
	// Testing type conversions, bleh. At least the type conversion itself
	// doesn't look this messy.
	taskFromAcs := ecsacs.Task{
		Arn:           strptr("myArn"),
		DesiredStatus: strptr("RUNNING"),
		Family:        strptr("myFamily"),
		Version:       strptr("1"),
		Containers: []*ecsacs.Container{
			{
				Name: strptr("myName"),
				MountPoints: []*ecsacs.MountPoint{
					{
						ContainerPath: strptr(`C:/Container/Path`),
						SourceVolume:  strptr("sourceVolume"),
					},
				},
			},
		},
		Volumes: []*ecsacs.Volume{
			{
				Name: strptr("sourceVolume"),
				Host: &ecsacs.HostVolumeProperties{
					SourcePath: strptr(`C:/Host/path`),
				},
			},
		},
	}
	expectedTask := &Task{
		Arn:                 "myArn",
		DesiredStatusUnsafe: TaskRunning,
		Family:              "myFamily",
		Version:             "1",
		Containers: []*Container{
			{
				Name: "myName",
				MountPoints: []MountPoint{
					{
						ContainerPath: `c:\container\path`,
						SourceVolume:  "sourceVolume",
					},
				},
			},
		},
		Volumes: []TaskVolume{
			{
				Name: "sourceVolume",
				Volume: &FSHostVolume{
					FSSourcePath: `c:\host\path`,
				},
			},
		},
		StartSequenceNumber: 42,
	}

	seqNum := int64(42)
	task, err := TaskFromACS(&taskFromAcs, &ecsacs.PayloadMessage{SeqNum: &seqNum})
	assert.Nil(t, err, "Should be able to handle acs task")
	cfg := config.Config{TaskCPUMemLimit: config.ExplicitlyDisabled}
	task.PostUnmarshalTask(&cfg, nil)

	assert.Equal(t, expectedTask.Containers, task.Containers, "Containers should be equal")
	assert.Equal(t, expectedTask.Volumes, task.Volumes, "Volumes should be equal")
}

func TestWindowsPlatformHostConfigOverride(t *testing.T) {
	// Testing Windows platform override for HostConfig.
	// Expects MemorySwappiness option to be set to -1

	task := &Task{}

	hostConfig := &docker.HostConfig{CPUShares: int64(1 * cpuSharesPerCore)}

	task.platformHostConfigOverride(hostConfig)
	assert.Equal(t, int64(1*cpuSharesPerCore*percentageFactor)/int64(cpuShareScaleFactor), hostConfig.CPUPercent)
	assert.Equal(t, int64(0), hostConfig.CPUShares)
	assert.EqualValues(t, expectedMemorySwappinessDefault, hostConfig.MemorySwappiness)

	hostConfig = &docker.HostConfig{CPUShares: 10}
	task.platformHostConfigOverride(hostConfig)
	assert.Equal(t, int64(minimumCPUPercent), hostConfig.CPUPercent)
	assert.Empty(t, hostConfig.CPUShares)
}

func TestWindowsMemorySwappinessOption(t *testing.T) {
	// Testing sending a task to windows overriding MemorySwappiness value
	rawHostConfigInput := docker.HostConfig{}

	rawHostConfig, err := json.Marshal(&rawHostConfigInput)
	if err != nil {
		t.Fatal(err)
	}

	testTask := &Task{
		Arn:     "arn:aws:ecs:us-east-1:012345678910:task/c09f0188-7f87-4b0f-bfc3-16296622b6fe",
		Family:  "myFamily",
		Version: "1",
		Containers: []*Container{
			{
				Name: "c1",
				DockerConfig: DockerConfig{
					HostConfig: strptr(string(rawHostConfig)),
				},
			},
		},
	}

	config, configErr := testTask.DockerHostConfig(testTask.Containers[0], dockerMap(testTask), defaultDockerClientAPIVersion)
	if configErr != nil {
		t.Fatal(configErr)
	}

	assert.EqualValues(t, expectedMemorySwappinessDefault, config.MemorySwappiness)
}
