// +build windows,integration

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

package engine

import "github.com/aws/amazon-ecs-agent/agent/api"

const (
	dockerEndpoint  = "npipe:////./pipe/docker_engine"
	testVolumeImage = "amazon/amazon-ecs-volumes-test:make"
)

// TODO implement this
func isDockerRunning() bool { return true }

func createTestContainer() *api.Container {
	return &api.Container{
		Name:                "windows",
		Image:               "microsoft/windowsservercore:latest",
		Essential:           true,
		DesiredStatusUnsafe: api.ContainerRunning,
		CPU:                 512,
		Memory:              256,
	}
}

func createTestHostVolumeMountTask(tmpPath string) *api.Task {
	testTask := createTestTask("testHostVolumeMount")
	testTask.Volumes = []api.TaskVolume{{Name: "test-tmp", Volume: &api.FSHostVolume{FSSourcePath: tmpPath}}}
	testTask.Containers[0].Image = testVolumeImage
	testTask.Containers[0].MountPoints = []api.MountPoint{{ContainerPath: "C:/host/tmp", SourceVolume: "test-tmp"}}
	testTask.Containers[0].Command = []string{
		`echo "hi" | Out-File -FilePath C:\host\tmp\hello-from-container -Encoding ascii ; $exists = Test-Path C:\host\tmp\test-file ; if (!$exists) { exit 2 } ;$contents = [IO.File]::ReadAllText("C:\host\tmp\test-file") ; if (!$contents -match "test-data") { $contents ; exit 4 } ; exit 42`,
	}
	return testTask
}

func createTestEmptyHostVolumeMountTask() *api.Task {
	testTask := createTestTask("testEmptyHostVolumeMount")
	testTask.Volumes = []api.TaskVolume{{Name: "test-tmp", Volume: &api.EmptyHostVolume{}}}
	testTask.Containers[0].Image = testVolumeImage
	testTask.Containers[0].MountPoints = []api.MountPoint{{ContainerPath: "C:/empty", SourceVolume: "test-tmp"}}
	testTask.Containers[0].Command = []string{`While($true){ if (Test-Path C:\empty\file) { exit 42 } }`}
	testTask.Containers = append(testTask.Containers, createTestContainer())
	testTask.Containers[1].Name = "test2"
	testTask.Containers[1].Image = testVolumeImage
	testTask.Containers[1].MountPoints = []api.MountPoint{{ContainerPath: "C:/alsoempty/", SourceVolume: "test-tmp"}}
	testTask.Containers[1].Command = []string{`New-Item -Path C:\alsoempty\file`}
	testTask.Containers[1].Essential = false
	return testTask
}
