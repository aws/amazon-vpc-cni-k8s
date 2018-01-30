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

package api

import (
	"encoding/json"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/aws/amazon-ecs-agent/agent/acs/model/ecsacs"
	"github.com/aws/amazon-ecs-agent/agent/config"
	"github.com/aws/amazon-ecs-agent/agent/credentials"
	"github.com/aws/amazon-ecs-agent/agent/credentials/mocks"
	"github.com/aws/amazon-ecs-agent/agent/engine/dockerclient"
	"github.com/aws/amazon-ecs-agent/agent/utils/ttime"
	docker "github.com/fsouza/go-dockerclient"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
)

const dockerIDPrefix = "dockerid-"

var defaultDockerClientAPIVersion = dockerclient.Version_1_17

func strptr(s string) *string { return &s }

func dockerMap(task *Task) map[string]*DockerContainer {
	m := make(map[string]*DockerContainer)
	for _, c := range task.Containers {
		m[c.Name] = &DockerContainer{DockerID: dockerIDPrefix + c.Name, DockerName: "dockername-" + c.Name, Container: c}
	}
	return m
}

func TestDockerConfigPortBinding(t *testing.T) {
	testTask := &Task{
		Containers: []*Container{
			{
				Name:  "c1",
				Ports: []PortBinding{{10, 10, "", TransportProtocolTCP}, {20, 20, "", TransportProtocolUDP}},
			},
		},
	}

	config, err := testTask.DockerConfig(testTask.Containers[0], defaultDockerClientAPIVersion)
	if err != nil {
		t.Error(err)
	}

	_, ok := config.ExposedPorts["10/tcp"]
	if !ok {
		t.Fatal("Could not get exposed ports 10/tcp")
	}
	_, ok = config.ExposedPorts["20/udp"]
	if !ok {
		t.Fatal("Could not get exposed ports 20/udp")
	}
}

func TestDockerConfigCPUShareZero(t *testing.T) {
	testTask := &Task{
		Containers: []*Container{
			{
				Name: "c1",
				CPU:  0,
			},
		},
	}

	config, err := testTask.DockerConfig(testTask.Containers[0], defaultDockerClientAPIVersion)
	if err != nil {
		t.Error(err)
	}

	if config.CPUShares != 2 {
		t.Error("CPU shares of 0 did not get changed to 2")
	}
}

func TestDockerConfigCPUShareMinimum(t *testing.T) {
	testTask := &Task{
		Containers: []*Container{
			{
				Name: "c1",
				CPU:  1,
			},
		},
	}

	config, err := testTask.DockerConfig(testTask.Containers[0], defaultDockerClientAPIVersion)
	if err != nil {
		t.Error(err)
	}

	if config.CPUShares != 2 {
		t.Error("CPU shares of 1 did not get changed to 2")
	}
}

func TestDockerConfigCPUShareUnchanged(t *testing.T) {
	testTask := &Task{
		Containers: []*Container{
			{
				Name: "c1",
				CPU:  100,
			},
		},
	}

	config, err := testTask.DockerConfig(testTask.Containers[0], defaultDockerClientAPIVersion)
	if err != nil {
		t.Error(err)
	}

	if config.CPUShares != 100 {
		t.Error("CPU shares unexpectedly changed")
	}
}

func TestDockerHostConfigPortBinding(t *testing.T) {
	testTask := &Task{
		Containers: []*Container{
			{
				Name:  "c1",
				Ports: []PortBinding{{10, 10, "", TransportProtocolTCP}, {20, 20, "", TransportProtocolUDP}},
			},
		},
	}

	config, err := testTask.DockerHostConfig(testTask.Containers[0], dockerMap(testTask), defaultDockerClientAPIVersion)
	assert.Nil(t, err)

	bindings, ok := config.PortBindings["10/tcp"]
	assert.True(t, ok, "Could not get port bindings")
	assert.Equal(t, 1, len(bindings), "Wrong number of bindings")
	assert.Equal(t, "10", bindings[0].HostPort, "Wrong hostport")

	bindings, ok = config.PortBindings["20/udp"]
	assert.True(t, ok, "Could not get port bindings")
	assert.Equal(t, 1, len(bindings), "Wrong number of bindings")
	assert.Equal(t, "20", bindings[0].HostPort, "Wrong hostport")
}

func TestDockerHostConfigVolumesFrom(t *testing.T) {
	testTask := &Task{
		Containers: []*Container{
			{
				Name: "c1",
			},
			{
				Name:        "c2",
				VolumesFrom: []VolumeFrom{{SourceContainer: "c1"}},
			},
		},
	}

	config, err := testTask.DockerHostConfig(testTask.Containers[1], dockerMap(testTask), defaultDockerClientAPIVersion)
	assert.Nil(t, err)

	if !reflect.DeepEqual(config.VolumesFrom, []string{"dockername-c1"}) {
		t.Error("Expected volumesFrom to be resolved, was: ", config.VolumesFrom)
	}
}

func TestDockerHostConfigRawConfig(t *testing.T) {
	rawHostConfigInput := docker.HostConfig{
		Privileged:     true,
		ReadonlyRootfs: true,
		DNS:            []string{"dns1, dns2"},
		DNSSearch:      []string{"dns.search"},
		ExtraHosts:     []string{"extra:hosts"},
		SecurityOpt:    []string{"foo", "bar"},
		CPUShares:      2,
		LogConfig: docker.LogConfig{
			Type:   "foo",
			Config: map[string]string{"foo": "bar"},
		},
		Ulimits:          []docker.ULimit{{Name: "ulimit name", Soft: 10, Hard: 100}},
		MemorySwappiness: memorySwappinessDefault,
	}

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
	assert.Nil(t, configErr)

	expectedOutput := rawHostConfigInput
	expectedOutput.CPUPercent = minimumCPUPercent
	if runtime.GOOS == "windows" {
		// CPUShares will always be 0 on windows
		expectedOutput.CPUShares = 0
	}
	assertSetStructFieldsEqual(t, expectedOutput, *config)
}

func TestDockerHostConfigRawConfigMerging(t *testing.T) {
	// Use a struct that will marshal to the actual message we expect; not
	// docker.HostConfig which will include a lot of zero values.
	rawHostConfigInput := struct {
		Privileged  bool     `json:"Privileged,omitempty" yaml:"Privileged,omitempty"`
		SecurityOpt []string `json:"SecurityOpt,omitempty" yaml:"SecurityOpt,omitempty"`
	}{
		Privileged:  true,
		SecurityOpt: []string{"foo", "bar"},
	}

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
				Name:        "c1",
				Image:       "image",
				CPU:         50,
				Memory:      100,
				VolumesFrom: []VolumeFrom{{SourceContainer: "c2"}},
				DockerConfig: DockerConfig{
					HostConfig: strptr(string(rawHostConfig)),
				},
			},
			{
				Name: "c2",
			},
		},
	}

	hostConfig, configErr := testTask.DockerHostConfig(testTask.Containers[0], dockerMap(testTask), defaultDockerClientAPIVersion)
	assert.Nil(t, configErr)

	expected := docker.HostConfig{
		Privileged:       true,
		SecurityOpt:      []string{"foo", "bar"},
		VolumesFrom:      []string{"dockername-c2"},
		MemorySwappiness: memorySwappinessDefault,
		CPUPercent:       minimumCPUPercent,
	}

	assertSetStructFieldsEqual(t, expected, *hostConfig)
}

func TestDockerHostConfigPauseContainer(t *testing.T) {
	testTask := &Task{
		ENI: &ENI{
			ID: "eniID",
		},
		Containers: []*Container{
			&Container{
				Name: "c1",
			},
			&Container{
				Name: emptyHostVolumeName,
				Type: ContainerEmptyHostVolume,
			},
			&Container{
				Name: PauseContainerName,
				Type: ContainerCNIPause,
			},
		},
	}

	// Verify that the network mode is set to "container:<pause-container-docker-id>"
	// for a non empty volume, non pause container
	config, err := testTask.DockerHostConfig(testTask.Containers[0], dockerMap(testTask), defaultDockerClientAPIVersion)
	assert.Nil(t, err)
	assert.Equal(t, "container:"+dockerIDPrefix+PauseContainerName, config.NetworkMode)

	// Verify that the network mode is not set to "none"  for the
	// empty volume container
	config, err = testTask.DockerHostConfig(testTask.Containers[1], dockerMap(testTask), defaultDockerClientAPIVersion)
	assert.Nil(t, err)
	assert.Equal(t, networkModeNone, config.NetworkMode)

	// Verify that the network mode is set to "none" for the pause container
	config, err = testTask.DockerHostConfig(testTask.Containers[2], dockerMap(testTask), defaultDockerClientAPIVersion)
	assert.Nil(t, err)
	assert.Equal(t, networkModeNone, config.NetworkMode)

	// Verify that overridden DNS settings are set for the "pause" container
	// and not set for non "pause" containers
	testTask.ENI.DomainNameServers = []string{"169.254.169.253"}
	testTask.ENI.DomainNameSearchList = []string{"us-west-2.compute.internal"}

	// DNS overrides are only applied to the pause container. Verify that the non-pause
	// container contains no overrides
	config, err = testTask.DockerHostConfig(testTask.Containers[0], dockerMap(testTask), defaultDockerClientAPIVersion)
	assert.Nil(t, err)
	assert.Equal(t, 0, len(config.DNS))
	assert.Equal(t, 0, len(config.DNSSearch))

	// Verify DNS settings are overridden for the pause container
	config, err = testTask.DockerHostConfig(testTask.Containers[2], dockerMap(testTask), defaultDockerClientAPIVersion)
	assert.Nil(t, err)
	assert.Equal(t, []string{"169.254.169.253"}, config.DNS)
	assert.Equal(t, []string{"us-west-2.compute.internal"}, config.DNSSearch)
}

func TestBadDockerHostConfigRawConfig(t *testing.T) {
	for _, badHostConfig := range []string{"malformed", `{"Privileged": "wrongType"}`} {
		testTask := Task{
			Arn:     "arn:aws:ecs:us-east-1:012345678910:task/c09f0188-7f87-4b0f-bfc3-16296622b6fe",
			Family:  "myFamily",
			Version: "1",
			Containers: []*Container{
				{
					Name: "c1",
					DockerConfig: DockerConfig{
						HostConfig: strptr(badHostConfig),
					},
				},
			},
		}
		_, err := testTask.DockerHostConfig(testTask.Containers[0], dockerMap(&testTask), defaultDockerClientAPIVersion)
		assert.Error(t, err)
	}
}

func TestDockerConfigRawConfig(t *testing.T) {
	rawConfigInput := docker.Config{
		Hostname:        "hostname",
		Domainname:      "domainname",
		NetworkDisabled: true,
		DNS:             []string{"dnsfoo", "dnsbar"},
		WorkingDir:      "workdir",
		User:            "user",
	}

	rawConfig, err := json.Marshal(&rawConfigInput)
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
					Config: strptr(string(rawConfig)),
				},
			},
		},
	}

	config, configErr := testTask.DockerConfig(testTask.Containers[0], defaultDockerClientAPIVersion)
	if configErr != nil {
		t.Fatal(configErr)
	}

	expectedOutput := rawConfigInput
	expectedOutput.CPUShares = 2

	assertSetStructFieldsEqual(t, expectedOutput, *config)
}

func TestDockerConfigRawConfigNilLabel(t *testing.T) {
	rawConfig, err := json.Marshal(&struct{ Labels map[string]string }{nil})
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
					Config: strptr(string(rawConfig)),
				},
			},
		},
	}

	_, configErr := testTask.DockerConfig(testTask.Containers[0], defaultDockerClientAPIVersion)
	if configErr != nil {
		t.Fatal(configErr)
	}
}

func TestDockerConfigRawConfigMerging(t *testing.T) {
	// Use a struct that will marshal to the actual message we expect; not
	// docker.Config which will include a lot of zero values.
	rawConfigInput := struct {
		User string `json:"User,omitempty" yaml:"User,omitempty"`
	}{
		User: "user",
	}

	rawConfig, err := json.Marshal(&rawConfigInput)
	if err != nil {
		t.Fatal(err)
	}

	testTask := &Task{
		Arn:     "arn:aws:ecs:us-east-1:012345678910:task/c09f0188-7f87-4b0f-bfc3-16296622b6fe",
		Family:  "myFamily",
		Version: "1",
		Containers: []*Container{
			{
				Name:   "c1",
				Image:  "image",
				CPU:    50,
				Memory: 1000,
				DockerConfig: DockerConfig{
					Config: strptr(string(rawConfig)),
				},
			},
		},
	}

	config, configErr := testTask.DockerConfig(testTask.Containers[0], defaultDockerClientAPIVersion)
	if configErr != nil {
		t.Fatal(configErr)
	}

	expected := docker.Config{
		Memory:    1000 * 1024 * 1024,
		CPUShares: 50,
		Image:     "image",
		User:      "user",
	}

	assertSetStructFieldsEqual(t, expected, *config)
}

func TestBadDockerConfigRawConfig(t *testing.T) {
	for _, badConfig := range []string{"malformed", `{"Labels": "wrongType"}`} {
		testTask := Task{
			Arn:     "arn:aws:ecs:us-east-1:012345678910:task/c09f0188-7f87-4b0f-bfc3-16296622b6fe",
			Family:  "myFamily",
			Version: "1",
			Containers: []*Container{
				{
					Name: "c1",
					DockerConfig: DockerConfig{
						Config: strptr(badConfig),
					},
				},
			},
		}
		_, err := testTask.DockerConfig(testTask.Containers[0], defaultDockerClientAPIVersion)
		if err == nil {
			t.Fatal("Expected error, was none for: " + badConfig)
		}
	}
}

func TestGetCredentialsEndpointWhenCredentialsAreSet(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	credentialsManager := mock_credentials.NewMockManager(ctrl)

	credentialsIDInTask := "credsid"
	task := Task{
		Containers: []*Container{
			{
				Name:        "c1",
				Environment: make(map[string]string),
			},
			{
				Name:        "c2",
				Environment: make(map[string]string),
			}},
		credentialsID: credentialsIDInTask,
	}

	taskCredentials := credentials.TaskIAMRoleCredentials{
		IAMRoleCredentials: credentials.IAMRoleCredentials{CredentialsID: "credsid"},
	}
	credentialsManager.EXPECT().GetTaskCredentials(credentialsIDInTask).Return(taskCredentials, true)
	task.initializeCredentialsEndpoint(credentialsManager)

	// Test if all containers in the task have the environment variable for
	// credentials endpoint set correctly.
	for _, container := range task.Containers {
		env := container.Environment
		_, exists := env[awsSDKCredentialsRelativeURIPathEnvironmentVariableName]
		if !exists {
			t.Errorf("'%s' environment variable not set for container '%s', env: %v", awsSDKCredentialsRelativeURIPathEnvironmentVariableName, container.Name, env)
		}
	}
}

func TestGetCredentialsEndpointWhenCredentialsAreNotSet(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	credentialsManager := mock_credentials.NewMockManager(ctrl)

	task := Task{
		Containers: []*Container{
			{
				Name:        "c1",
				Environment: make(map[string]string),
			},
			{
				Name:        "c2",
				Environment: make(map[string]string),
			}},
	}

	task.initializeCredentialsEndpoint(credentialsManager)

	for _, container := range task.Containers {
		env := container.Environment
		_, exists := env[awsSDKCredentialsRelativeURIPathEnvironmentVariableName]
		if exists {
			t.Errorf("'%s' environment variable should not be set for container '%s'", awsSDKCredentialsRelativeURIPathEnvironmentVariableName, container.Name)
		}
	}
}

// TODO: UT for PostUnmarshalTask, etc

func TestPostUnmarshalTaskWithEmptyVolumes(t *testing.T) {
	// Constants used here are defined in task_unix_test.go and task_windows_test.go
	taskFromACS := ecsacs.Task{
		Arn:           strptr("myArn"),
		DesiredStatus: strptr("RUNNING"),
		Family:        strptr("myFamily"),
		Version:       strptr("1"),
		Containers: []*ecsacs.Container{
			{
				Name: strptr("myName1"),
				MountPoints: []*ecsacs.MountPoint{
					{
						ContainerPath: strptr(emptyVolumeContainerPath1),
						SourceVolume:  strptr(emptyVolumeName1),
					},
				},
			},
			{
				Name: strptr("myName2"),
				MountPoints: []*ecsacs.MountPoint{
					{
						ContainerPath: strptr(emptyVolumeContainerPath2),
						SourceVolume:  strptr(emptyVolumeName2),
					},
				},
			},
		},
		Volumes: []*ecsacs.Volume{
			{
				Name: strptr(emptyVolumeName1),
				Host: &ecsacs.HostVolumeProperties{},
			},
			{
				Name: strptr(emptyVolumeName2),
				Host: &ecsacs.HostVolumeProperties{},
			},
		},
	}
	seqNum := int64(42)
	task, err := TaskFromACS(&taskFromACS, &ecsacs.PayloadMessage{SeqNum: &seqNum})
	assert.Nil(t, err, "Should be able to handle acs task")
	assert.Equal(t, 2, len(task.Containers)) // before PostUnmarshalTask
	cfg := config.Config{}
	task.PostUnmarshalTask(&cfg, nil)

	assert.Equal(t, 3, len(task.Containers), "Should include new container for volumes")
	emptyContainer, ok := task.ContainerByName(emptyHostVolumeName)
	assert.True(t, ok, "Should find empty volume container")
	assert.Equal(t, 2, len(emptyContainer.MountPoints), "Should have two mount points")
	assert.Equal(t, []MountPoint{
		{
			SourceVolume:  emptyVolumeName1,
			ContainerPath: expectedEmptyVolumeGeneratedPath1,
		}, {
			SourceVolume:  emptyVolumeName2,
			ContainerPath: expectedEmptyVolumeGeneratedPath2,
		},
	}, emptyContainer.MountPoints)
	assert.Equal(t, expectedEmptyVolumeContainerImage+":"+expectedEmptyVolumeContainerTag, emptyContainer.Image, "Should have expected image")
	assert.Equal(t, []string{expectedEmptyVolumeContainerCmd}, emptyContainer.Command, "Should have expected command")
}

func TestTaskFromACS(t *testing.T) {
	testTime := ttime.Now().Truncate(1 * time.Second).Format(time.RFC3339)

	intptr := func(i int64) *int64 {
		return &i
	}
	boolptr := func(b bool) *bool {
		return &b
	}
	floatptr := func(f float64) *float64 {
		return &f
	}
	// Testing type conversions, bleh. At least the type conversion itself
	// doesn't look this messy.
	taskFromAcs := ecsacs.Task{
		Arn:           strptr("myArn"),
		DesiredStatus: strptr("RUNNING"),
		Family:        strptr("myFamily"),
		Version:       strptr("1"),
		Containers: []*ecsacs.Container{
			{
				Name:        strptr("myName"),
				Cpu:         intptr(10),
				Command:     []*string{strptr("command"), strptr("command2")},
				EntryPoint:  []*string{strptr("sh"), strptr("-c")},
				Environment: map[string]*string{"key": strptr("value")},
				Essential:   boolptr(true),
				Image:       strptr("image:tag"),
				Links:       []*string{strptr("link1"), strptr("link2")},
				Memory:      intptr(100),
				MountPoints: []*ecsacs.MountPoint{
					{
						ContainerPath: strptr("/container/path"),
						ReadOnly:      boolptr(true),
						SourceVolume:  strptr("sourceVolume"),
					},
				},
				Overrides: strptr(`{"command":["a","b","c"]}`),
				PortMappings: []*ecsacs.PortMapping{
					{
						HostPort:      intptr(800),
						ContainerPort: intptr(900),
						Protocol:      strptr("udp"),
					},
				},
				VolumesFrom: []*ecsacs.VolumeFrom{
					{
						ReadOnly:        boolptr(true),
						SourceContainer: strptr("volumeLink"),
					},
				},
				DockerConfig: &ecsacs.DockerConfig{
					Config:     strptr("config json"),
					HostConfig: strptr("hostconfig json"),
					Version:    strptr("version string"),
				},
			},
		},
		Volumes: []*ecsacs.Volume{
			{
				Name: strptr("volName"),
				Host: &ecsacs.HostVolumeProperties{
					SourcePath: strptr("/host/path"),
				},
			},
		},
		RoleCredentials: &ecsacs.IAMRoleCredentials{
			CredentialsId:   strptr("credsId"),
			AccessKeyId:     strptr("keyId"),
			Expiration:      strptr(testTime),
			RoleArn:         strptr("roleArn"),
			SecretAccessKey: strptr("OhhSecret"),
			SessionToken:    strptr("sessionToken"),
		},
		Cpu:    floatptr(2.0),
		Memory: intptr(512),
	}
	expectedTask := &Task{
		Arn:                 "myArn",
		DesiredStatusUnsafe: TaskRunning,
		Family:              "myFamily",
		Version:             "1",
		Containers: []*Container{
			{
				Name:        "myName",
				Image:       "image:tag",
				Command:     []string{"a", "b", "c"},
				Links:       []string{"link1", "link2"},
				EntryPoint:  &[]string{"sh", "-c"},
				Essential:   true,
				Environment: map[string]string{"key": "value"},
				CPU:         10,
				Memory:      100,
				MountPoints: []MountPoint{
					{
						ContainerPath: "/container/path",
						ReadOnly:      true,
						SourceVolume:  "sourceVolume",
					},
				},
				Overrides: ContainerOverrides{
					Command: &[]string{"a", "b", "c"},
				},
				Ports: []PortBinding{
					{
						HostPort:      800,
						ContainerPort: 900,
						Protocol:      TransportProtocolUDP,
					},
				},
				VolumesFrom: []VolumeFrom{
					{
						ReadOnly:        true,
						SourceContainer: "volumeLink",
					},
				},
				DockerConfig: DockerConfig{
					Config:     strptr("config json"),
					HostConfig: strptr("hostconfig json"),
					Version:    strptr("version string"),
				},
			},
		},
		Volumes: []TaskVolume{
			{
				Name: "volName",
				Volume: &FSHostVolume{
					FSSourcePath: "/host/path",
				},
			},
		},
		StartSequenceNumber: 42,
		CPU:                 2.0,
		Memory:              512,
	}

	seqNum := int64(42)
	task, err := TaskFromACS(&taskFromAcs, &ecsacs.PayloadMessage{SeqNum: &seqNum})

	assert.NoError(t, err)
	assert.EqualValues(t, expectedTask, task)
}

func TestTaskUpdateKnownStatusHappyPath(t *testing.T) {
	testTask := &Task{
		KnownStatusUnsafe: TaskStatusNone,
		Containers: []*Container{
			{
				KnownStatusUnsafe: ContainerCreated,
			},
			{
				KnownStatusUnsafe: ContainerStopped,
				Essential:         true,
			},
			{
				KnownStatusUnsafe: ContainerRunning,
			},
		},
	}

	newStatus := testTask.updateTaskKnownStatus()
	assert.Equal(t, TaskCreated, newStatus, "task status should depend on the earlist container status")
	assert.Equal(t, TaskCreated, testTask.GetKnownStatus(), "task status should depend on the earlist container status")
}

// TestTaskUpdateKnownStatusNotChangeToRunningWithEssentialContainerStopped tests when there is one essential
// container is stopped while the other containers are running, the task status shouldn't be changed to running
func TestTaskUpdateKnownStatusNotChangeToRunningWithEssentialContainerStopped(t *testing.T) {
	testTask := &Task{
		KnownStatusUnsafe: TaskCreated,
		Containers: []*Container{
			{
				KnownStatusUnsafe: ContainerRunning,
				Essential:         true,
			},
			{
				KnownStatusUnsafe: ContainerStopped,
				Essential:         true,
			},
			{
				KnownStatusUnsafe: ContainerRunning,
			},
		},
	}

	newStatus := testTask.updateTaskKnownStatus()
	assert.Equal(t, TaskStatusNone, newStatus, "task status should not move to running if essential container is stopped")
	assert.Equal(t, TaskCreated, testTask.GetKnownStatus(), "task status should not move to running if essential container is stopped")
}

// TestTaskUpdateKnownStatusToPendingWithEssentialContainerStopped tests when there is one essential container
// is stopped while other container status are prior to Running, the task status should be updated.
func TestTaskUpdateKnownStatusToPendingWithEssentialContainerStopped(t *testing.T) {
	testTask := &Task{
		KnownStatusUnsafe: TaskStatusNone,
		Containers: []*Container{
			{
				KnownStatusUnsafe: ContainerCreated,
				Essential:         true,
			},
			{
				KnownStatusUnsafe: ContainerStopped,
				Essential:         true,
			},
			{
				KnownStatusUnsafe: ContainerCreated,
			},
		},
	}

	newStatus := testTask.updateTaskKnownStatus()
	assert.Equal(t, TaskCreated, newStatus)
	assert.Equal(t, TaskCreated, testTask.GetKnownStatus())
}

// TestTaskUpdateKnownStatusToPendingWithEssentialContainerStoppedWhenSteadyStateIsResourcesProvisioned
// tests when there is one essential container is stopped while other container status are prior to
// ResourcesProvisioned, the task status should be updated.
func TestTaskUpdateKnownStatusToPendingWithEssentialContainerStoppedWhenSteadyStateIsResourcesProvisioned(t *testing.T) {
	resourcesProvisioned := ContainerResourcesProvisioned
	testTask := &Task{
		KnownStatusUnsafe: TaskStatusNone,
		Containers: []*Container{
			&Container{
				KnownStatusUnsafe: ContainerCreated,
				Essential:         true,
			},
			&Container{
				KnownStatusUnsafe: ContainerStopped,
				Essential:         true,
			},
			&Container{
				KnownStatusUnsafe:       ContainerCreated,
				Essential:               true,
				SteadyStateStatusUnsafe: &resourcesProvisioned,
			},
		},
	}

	newStatus := testTask.updateTaskKnownStatus()
	assert.Equal(t, TaskCreated, newStatus)
	assert.Equal(t, TaskCreated, testTask.GetKnownStatus())
}

// TestGetEarliestTaskStatusForContainersEmptyTask verifies that
// `getEarliestKnownTaskStatusForContainers` returns TaskStatusNone when invoked on
// a task with no contianers
func TestGetEarliestTaskStatusForContainersEmptyTask(t *testing.T) {
	testTask := &Task{}
	assert.Equal(t, testTask.getEarliestKnownTaskStatusForContainers(), TaskStatusNone)
}

// TestGetEarliestTaskStatusForContainersWhenKnownStatusIsNotSetForContainers verifies that
// `getEarliestKnownTaskStatusForContainers` returns TaskStatusNone when invoked on
// a task with contianers that do not have the `KnownStatusUnsafe` field set
func TestGetEarliestTaskStatusForContainersWhenKnownStatusIsNotSetForContainers(t *testing.T) {
	testTask := &Task{
		KnownStatusUnsafe: TaskStatusNone,
		Containers: []*Container{
			&Container{},
			&Container{},
		},
	}
	assert.Equal(t, testTask.getEarliestKnownTaskStatusForContainers(), TaskStatusNone)
}

func TestGetEarliestTaskStatusForContainersWhenSteadyStateIsRunning(t *testing.T) {
	testTask := &Task{
		KnownStatusUnsafe: TaskStatusNone,
		Containers: []*Container{
			&Container{
				KnownStatusUnsafe: ContainerCreated,
			},
			&Container{
				KnownStatusUnsafe: ContainerRunning,
			},
		},
	}

	// Since a container is still in CREATED state, the earliest known status
	// for the task based on its container statuses must be `TaskCreated`
	assert.Equal(t, testTask.getEarliestKnownTaskStatusForContainers(), TaskCreated)
	// Ensure that both containers are RUNNING, which means that the earliest known status
	// for the task based on its container statuses must be `TaskRunning`
	testTask.Containers[0].SetKnownStatus(ContainerRunning)
	assert.Equal(t, testTask.getEarliestKnownTaskStatusForContainers(), TaskRunning)
}

func TestGetEarliestTaskStatusForContainersWhenSteadyStateIsResourceProvisioned(t *testing.T) {
	resourcesProvisioned := ContainerResourcesProvisioned
	testTask := &Task{
		KnownStatusUnsafe: TaskStatusNone,
		Containers: []*Container{
			&Container{
				KnownStatusUnsafe: ContainerCreated,
			},
			&Container{
				KnownStatusUnsafe: ContainerRunning,
			},
			&Container{
				KnownStatusUnsafe:       ContainerRunning,
				SteadyStateStatusUnsafe: &resourcesProvisioned,
			},
		},
	}

	// Since a container is still in CREATED state, the earliest known status
	// for the task based on its container statuses must be `TaskCreated`
	assert.Equal(t, testTask.getEarliestKnownTaskStatusForContainers(), TaskCreated)
	testTask.Containers[0].SetKnownStatus(ContainerRunning)
	// Even if all containers transition to RUNNING, the earliest known status
	// for the task based on its container statuses would still be `TaskCreated`
	// as one of the containers has RESOURCES_PROVISIONED as its steady state
	assert.Equal(t, testTask.getEarliestKnownTaskStatusForContainers(), TaskCreated)
	// All of the containers in the task have reached their steady state. Ensure
	// that the earliest known status for the task based on its container states
	// is now `TaskRunning`
	testTask.Containers[2].SetKnownStatus(ContainerResourcesProvisioned)
	assert.Equal(t, testTask.getEarliestKnownTaskStatusForContainers(), TaskRunning)
}

func TestTaskUpdateKnownStatusChecksSteadyStateWhenSetToRunning(t *testing.T) {
	testTask := &Task{
		KnownStatusUnsafe: TaskStatusNone,
		Containers: []*Container{
			&Container{
				KnownStatusUnsafe: ContainerCreated,
			},
			&Container{
				KnownStatusUnsafe: ContainerRunning,
			},
			&Container{
				KnownStatusUnsafe: ContainerRunning,
			},
		},
	}

	// One of the containers is in CREATED state, expect task to be updated
	// to TaskCreated
	newStatus := testTask.updateTaskKnownStatus()
	assert.Equal(t, TaskCreated, newStatus, "Incorrect status returned: %s", newStatus.String())
	assert.Equal(t, TaskCreated, testTask.GetKnownStatus())

	// All of the containers are in RUNNING state, expect task to be updated
	// to TaskRunning
	testTask.Containers[0].SetKnownStatus(ContainerRunning)
	newStatus = testTask.updateTaskKnownStatus()
	assert.Equal(t, TaskRunning, newStatus, "Incorrect status returned: %s", newStatus.String())
	assert.Equal(t, TaskRunning, testTask.GetKnownStatus())
}

func TestTaskUpdateKnownStatusChecksSteadyStateWhenSetToResourceProvisioned(t *testing.T) {
	resourcesProvisioned := ContainerResourcesProvisioned
	testTask := &Task{
		KnownStatusUnsafe: TaskStatusNone,
		Containers: []*Container{
			&Container{
				KnownStatusUnsafe: ContainerCreated,
				Essential:         true,
			},
			&Container{
				KnownStatusUnsafe: ContainerRunning,
				Essential:         true,
			},
			&Container{
				KnownStatusUnsafe:       ContainerRunning,
				Essential:               true,
				SteadyStateStatusUnsafe: &resourcesProvisioned,
			},
		},
	}

	// One of the containers is in CREATED state, expect task to be updated
	// to TaskCreated
	newStatus := testTask.updateTaskKnownStatus()
	assert.Equal(t, TaskCreated, newStatus, "Incorrect status returned: %s", newStatus.String())
	assert.Equal(t, TaskCreated, testTask.GetKnownStatus())

	// All of the containers are in RUNNING state, but one of the containers
	// has its steady state set to RESOURCES_PROVISIONED, doexpect task to be
	// updated to TaskRunning
	testTask.Containers[0].SetKnownStatus(ContainerRunning)
	newStatus = testTask.updateTaskKnownStatus()
	assert.Equal(t, TaskStatusNone, newStatus, "Incorrect status returned: %s", newStatus.String())
	assert.Equal(t, TaskCreated, testTask.GetKnownStatus())

	// All of the containers have reached their steady states, expect the task
	// to be updated to `TaskRunning`
	testTask.Containers[2].SetKnownStatus(ContainerResourcesProvisioned)
	newStatus = testTask.updateTaskKnownStatus()
	assert.Equal(t, TaskRunning, newStatus, "Incorrect status returned: %s", newStatus.String())
	assert.Equal(t, TaskRunning, testTask.GetKnownStatus())
}

func assertSetStructFieldsEqual(t *testing.T, expected, actual interface{}) {
	for i := 0; i < reflect.TypeOf(expected).NumField(); i++ {
		expectedValue := reflect.ValueOf(expected).Field(i)
		// All the values we actaully expect to see are valid and non-nil
		if !expectedValue.IsValid() || ((expectedValue.Kind() == reflect.Map || expectedValue.Kind() == reflect.Slice) && expectedValue.IsNil()) {
			continue
		}
		expectedVal := expectedValue.Interface()
		actualVal := reflect.ValueOf(actual).Field(i).Interface()
		if !reflect.DeepEqual(expectedVal, actualVal) {
			t.Fatalf("Field %v did not match: %v != %v", reflect.TypeOf(expected).Field(i).Name, expectedVal, actualVal)
		}
	}
}

// TestGetIDErrorPaths performs table tests on GetID with erroneous taskARNs
func TestGetIDErrorPaths(t *testing.T) {
	testCases := []struct {
		arn  string
		name string
	}{
		{"", "EmptyString"},
		{"invalidArn", "InvalidARN"},
		{"arn:aws:ecs:region:account-id:task:task-id", "IncorrectSections"},
		{"arn:aws:ecs:region:account-id:task", "IncorrectResouceSections"},
	}

	task := Task{}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			task.Arn = tc.arn
			taskID, err := task.GetID()
			assert.Error(t, err, "GetID should return an error")
			assert.Empty(t, taskID, "ID should be empty")
		})
	}
}

// TestGetIDHappyPath validates the happy path of GetID
func TestGetIDHappyPath(t *testing.T) {
	task := Task{
		Arn: "arn:aws:ecs:region:account-id:task/task-id",
	}
	taskID, err := task.GetID()
	assert.NoError(t, err)
	assert.Equal(t, "task-id", taskID)
}

// TestTaskGetENI tests the eni can be correctly acquired by calling GetTaskENI
func TestTaskGetENI(t *testing.T) {
	enisOfTask := &ENI{
		ID: "id",
	}
	testTask := &Task{
		ENI: enisOfTask,
	}

	eni := testTask.GetTaskENI()
	assert.NotNil(t, eni)
	assert.Equal(t, "id", eni.ID)

	testTask.ENI = nil
	eni = testTask.GetTaskENI()
	assert.Nil(t, eni)
}

// TestTaskFromACSWithOverrides tests the container command is overridden correctly
func TestTaskFromACSWithOverrides(t *testing.T) {
	taskFromACS := ecsacs.Task{
		Arn:           strptr("myArn"),
		DesiredStatus: strptr("RUNNING"),
		Family:        strptr("myFamily"),
		Version:       strptr("1"),
		Containers: []*ecsacs.Container{
			{
				Name: strptr("myName1"),
				MountPoints: []*ecsacs.MountPoint{
					{
						ContainerPath: strptr(emptyVolumeContainerPath1),
						SourceVolume:  strptr(emptyVolumeName1),
					},
				},
				Overrides: strptr(`{"command": ["foo", "bar"]}`),
			},
			{
				Name:    strptr("myName2"),
				Command: []*string{strptr("command")},
				MountPoints: []*ecsacs.MountPoint{
					{
						ContainerPath: strptr(emptyVolumeContainerPath2),
						SourceVolume:  strptr(emptyVolumeName2),
					},
				},
			},
		},
	}

	seqNum := int64(42)
	task, err := TaskFromACS(&taskFromACS, &ecsacs.PayloadMessage{SeqNum: &seqNum})
	assert.Nil(t, err, "Should be able to handle acs task")
	assert.Equal(t, 2, len(task.Containers)) // before PostUnmarshalTask

	assert.Equal(t, task.Containers[0].Command[0], "foo")
	assert.Equal(t, task.Containers[0].Command[1], "bar")
	assert.Equal(t, task.Containers[1].Command[0], "command")
}

// TestSetPullStartedAt tests the task SetPullStartedAt
func TestSetPullStartedAt(t *testing.T) {
	testTask := &Task{}

	t1 := time.Now()
	t2 := t1.Add(1 * time.Second)

	testTask.SetPullStartedAt(t1)
	assert.Equal(t, t1, testTask.GetPullStartedAt(), "first set of pullStartedAt should succeed")

	testTask.SetPullStartedAt(t2)
	assert.Equal(t, t1, testTask.GetPullStartedAt(), "second set of pullStartedAt should have no impact")
}

// TestSetExecutionStoppedAt tests the task SetExecutionStoppedAt
func TestSetExecutionStoppedAt(t *testing.T) {
	testTask := &Task{}

	t1 := time.Now()
	t2 := t1.Add(1 * time.Second)

	testTask.SetExecutionStoppedAt(t1)
	assert.Equal(t, t1, testTask.GetExecutionStoppedAt(), "first set of executionStoppedAt should succeed")

	testTask.SetExecutionStoppedAt(t2)
	assert.Equal(t, t1, testTask.GetExecutionStoppedAt(), "second set of executionStoppedAt should have no impact")
}

func TestApplyExecutionRoleLogsAuthSet(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	credentialsManager := mock_credentials.NewMockManager(ctrl)

	credentialsIDInTask := "credsid"
	expectedEndpoint := "/v2/credentials/" + credentialsIDInTask

	rawHostConfigInput := docker.HostConfig{
		LogConfig: docker.LogConfig{
			Type:   "foo",
			Config: map[string]string{"foo": "bar"},
		},
	}

	rawHostConfig, err := json.Marshal(&rawHostConfigInput)
	if err != nil {
		t.Fatal(err)
	}

	task := &Task{
		Arn:     "arn:aws:ecs:us-east-1:012345678910:task/c09f0188-7f87-4b0f-bfc3-16296622b6fe",
		Family:  "testFamily",
		Version: "1",
		Containers: []*Container{
			{
				Name: "c1",
				DockerConfig: DockerConfig{
					HostConfig: strptr(string(rawHostConfig)),
				},
			},
		},
		ExecutionCredentialsID: credentialsIDInTask,
	}

	taskCredentials := credentials.TaskIAMRoleCredentials{
		IAMRoleCredentials: credentials.IAMRoleCredentials{CredentialsID: "credsid"},
	}
	credentialsManager.EXPECT().GetTaskCredentials(credentialsIDInTask).Return(taskCredentials, true)
	task.initializeCredentialsEndpoint(credentialsManager)

	config, err := task.DockerHostConfig(task.Containers[0], dockerMap(task), defaultDockerClientAPIVersion)
	assert.Nil(t, err)

	err = task.ApplyExecutionRoleLogsAuth(config, credentialsManager)
	assert.Nil(t, err)

	endpoint, ok := config.LogConfig.Config["awslogs-credentials-endpoint"]
	assert.True(t, ok)
	assert.Equal(t, expectedEndpoint, endpoint)
}

func TestApplyExecutionRoleLogsAuthFailEmptyCredentialsID(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	credentialsManager := mock_credentials.NewMockManager(ctrl)

	rawHostConfigInput := docker.HostConfig{
		LogConfig: docker.LogConfig{
			Type:   "foo",
			Config: map[string]string{"foo": "bar"},
		},
	}

	rawHostConfig, err := json.Marshal(&rawHostConfigInput)
	if err != nil {
		t.Fatal(err)
	}

	task := &Task{
		Arn:     "arn:aws:ecs:us-east-1:012345678910:task/c09f0188-7f87-4b0f-bfc3-16296622b6fe",
		Family:  "testFamily",
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

	task.initializeCredentialsEndpoint(credentialsManager)

	config, err := task.DockerHostConfig(task.Containers[0], dockerMap(task), defaultDockerClientAPIVersion)
	assert.Nil(t, err)

	err = task.ApplyExecutionRoleLogsAuth(config, credentialsManager)
	assert.Error(t, err)
}

func TestApplyExecutionRoleLogsAuthFailNoCredentialsForTask(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	credentialsManager := mock_credentials.NewMockManager(ctrl)

	credentialsIDInTask := "credsid"

	rawHostConfigInput := docker.HostConfig{
		LogConfig: docker.LogConfig{
			Type:   "foo",
			Config: map[string]string{"foo": "bar"},
		},
	}

	rawHostConfig, err := json.Marshal(&rawHostConfigInput)
	if err != nil {
		t.Fatal(err)
	}

	task := &Task{
		Arn:     "arn:aws:ecs:us-east-1:012345678910:task/c09f0188-7f87-4b0f-bfc3-16296622b6fe",
		Family:  "testFamily",
		Version: "1",
		Containers: []*Container{
			{
				Name: "c1",
				DockerConfig: DockerConfig{
					HostConfig: strptr(string(rawHostConfig)),
				},
			},
		},
		ExecutionCredentialsID: credentialsIDInTask,
	}

	credentialsManager.EXPECT().GetTaskCredentials(credentialsIDInTask).Return(credentials.TaskIAMRoleCredentials{}, false)
	task.initializeCredentialsEndpoint(credentialsManager)

	config, err := task.DockerHostConfig(task.Containers[0], dockerMap(task), defaultDockerClientAPIVersion)
	assert.Error(t, err)

	err = task.ApplyExecutionRoleLogsAuth(config, credentialsManager)
	assert.Error(t, err)
}

// TestSetConfigHostconfigBasedOnAPIVersion tests the docker hostconfig was correctly set// based on the docker client version
func TestSetConfigHostconfigBasedOnAPIVersion(t *testing.T) {
	memoryMiB := 500
	testTask := &Task{
		Containers: []*Container{
			{
				Name:   "c1",
				CPU:    uint(10),
				Memory: uint(memoryMiB),
			},
		},
	}

	hostconfig, err := testTask.DockerHostConfig(testTask.Containers[0], dockerMap(testTask), defaultDockerClientAPIVersion)
	assert.Nil(t, err)

	config, cerr := testTask.DockerConfig(testTask.Containers[0], defaultDockerClientAPIVersion)
	assert.Nil(t, cerr)

	assert.Equal(t, int64(memoryMiB*1024*1024), config.Memory)
	if runtime.GOOS == "windows" {
		assert.Equal(t, int64(minimumCPUPercent), hostconfig.CPUPercent)
	} else {
		assert.Equal(t, int64(10), config.CPUShares)
	}
	assert.Empty(t, hostconfig.CPUShares)
	assert.Empty(t, hostconfig.Memory)

	hostconfig, err = testTask.DockerHostConfig(testTask.Containers[0], dockerMap(testTask), dockerclient.Version_1_18)
	assert.Nil(t, err)

	config, cerr = testTask.DockerConfig(testTask.Containers[0], dockerclient.Version_1_18)
	assert.Nil(t, err)
	assert.Equal(t, int64(memoryMiB*1024*1024), hostconfig.Memory)
	if runtime.GOOS == "windows" {
		// cpushares is set to zero on windows
		assert.Empty(t, hostconfig.CPUShares)
		assert.Equal(t, int64(minimumCPUPercent), hostconfig.CPUPercent)
	} else {
		assert.Equal(t, int64(10), hostconfig.CPUShares)
	}

	assert.Empty(t, config.CPUShares)
	assert.Empty(t, config.Memory)
}

// TestSetMinimumMemoryLimit ensures that we set the correct minimum memory limit when the limit is too low
func TestSetMinimumMemoryLimit(t *testing.T) {
	testTask := &Task{
		Containers: []*Container{
			{
				Name:   "c1",
				Memory: uint(1),
			},
		},
	}

	hostconfig, err := testTask.DockerHostConfig(testTask.Containers[0], dockerMap(testTask), defaultDockerClientAPIVersion)
	assert.Nil(t, err)

	config, cerr := testTask.DockerConfig(testTask.Containers[0], defaultDockerClientAPIVersion)
	assert.Nil(t, cerr)

	assert.Equal(t, int64(DockerContainerMinimumMemoryInBytes), config.Memory)
	assert.Empty(t, hostconfig.Memory)

	hostconfig, err = testTask.DockerHostConfig(testTask.Containers[0], dockerMap(testTask), dockerclient.Version_1_18)
	assert.Nil(t, err)

	config, cerr = testTask.DockerConfig(testTask.Containers[0], dockerclient.Version_1_18)
	assert.Nil(t, err)
	assert.Equal(t, int64(DockerContainerMinimumMemoryInBytes), hostconfig.Memory)
	assert.Empty(t, config.Memory)
}
