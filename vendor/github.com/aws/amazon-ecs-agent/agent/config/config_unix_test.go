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

package config

import (
	"fmt"
	"io/ioutil"
	"os"
	"testing"
	"time"

	"github.com/aws/amazon-ecs-agent/agent/ec2"
	"github.com/aws/amazon-ecs-agent/agent/engine/dockerclient"
	cnitypes "github.com/containernetworking/cni/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigDefault(t *testing.T) {
	defer setTestRegion()()
	os.Unsetenv("ECS_HOST_DATA_DIR")

	cfg, err := NewConfig(ec2.NewBlackholeEC2MetadataClient())
	require.NoError(t, err)

	assert.Equal(t, "unix:///var/run/docker.sock", cfg.DockerEndpoint, "Default docker endpoint set incorrectly")
	assert.Equal(t, "/data/", cfg.DataDir, "Default datadir set incorrectly")
	assert.False(t, cfg.DisableMetrics, "Default disablemetrics set incorrectly")
	assert.Equal(t, 5, len(cfg.ReservedPorts), "Default reserved ports set incorrectly")
	assert.Equal(t, uint16(0), cfg.ReservedMemory, "Default reserved memory set incorrectly")
	assert.Equal(t, 30*time.Second, cfg.DockerStopTimeout, "Default docker stop container timeout set incorrectly")
	assert.False(t, cfg.PrivilegedDisabled, "Default PrivilegedDisabled set incorrectly")
	assert.Equal(t, []dockerclient.LoggingDriver{dockerclient.JSONFileDriver, dockerclient.NoneDriver},
		cfg.AvailableLoggingDrivers, "Default logging drivers set incorrectly")
	assert.Equal(t, 3*time.Hour, cfg.TaskCleanupWaitDuration, "Default task cleanup wait duration set incorrectly")
	assert.False(t, cfg.TaskENIEnabled, "TaskENIEnabled set incorrectly")
	assert.False(t, cfg.TaskIAMRoleEnabled, "TaskIAMRoleEnabled set incorrectly")
	assert.False(t, cfg.TaskIAMRoleEnabledForNetworkHost, "TaskIAMRoleEnabledForNetworkHost set incorrectly")
	assert.Equal(t, DefaultEnabled, cfg.TaskCPUMemLimit, "TaskCPUMemLimit should be DefaultEnabled")
	assert.False(t, cfg.CredentialsAuditLogDisabled, "CredentialsAuditLogDisabled set incorrectly")
	assert.Equal(t, defaultCredentialsAuditLogFile, cfg.CredentialsAuditLogFile, "CredentialsAuditLogFile is set incorrectly")
	assert.False(t, cfg.ImageCleanupDisabled, "ImageCleanupDisabled default is set incorrectly")
	assert.Equal(t, DefaultImageDeletionAge, cfg.MinimumImageDeletionAge, "MinimumImageDeletionAge default is set incorrectly")
	assert.Equal(t, DefaultImageCleanupTimeInterval, cfg.ImageCleanupInterval, "ImageCleanupInterval default is set incorrectly")
	assert.Equal(t, DefaultNumImagesToDeletePerCycle, cfg.NumImagesToDeletePerCycle, "NumImagesToDeletePerCycle default is set incorrectly")
	assert.Equal(t, defaultCNIPluginsPath, cfg.CNIPluginsPath, "CNIPluginsPath default is set incorrectly")
	assert.False(t, cfg.AWSVPCBlockInstanceMetdata, "AWSVPCBlockInstanceMetdata default is incorrectly set")
	assert.Equal(t, "/var/lib/ecs", cfg.DataDirOnHost, "Default DataDirOnHost set incorrectly")
}

// TestConfigFromFile tests the configuration can be read from file
func TestConfigFromFile(t *testing.T) {
	cluster := "TestCluster"
	dockerAuthType := "dockercfg"
	dockerAuth := `{
  "https://index.docker.io/v1/":{
    "auth":"admin",
    "email":"email"
  }
}`
	testPauseImageName := "pause-image-name"
	testPauseTag := "pause-image-tag"
	content := fmt.Sprintf(`{
  "AWSRegion": "not-real-1",
  "Cluster": "%s",
  "EngineAuthType": "%s",
  "EngineAuthData": %s,
  "DataDir": "/var/run/ecs_agent",
  "TaskIAMRoleEnabled": true,
  "TaskCPUMemLimit": true,
  "InstanceAttributes": {
    "attribute1": "value1"
  },
  "PauseContainerImageName":"%s",
  "PauseContainerTag":"%s",
  "AWSVPCAdditionalLocalRoutes":["169.254.172.1/32"]
}`, cluster, dockerAuthType, dockerAuth, testPauseImageName, testPauseTag)

	filePath := setupFileConfiguration(t, content)
	defer os.Remove(filePath)

	defer setTestEnv("ECS_AGENT_CONFIG_FILE_PATH", filePath)()
	defer setTestEnv("AWS_DEFAULT_REGION", "us-west-2")()

	cfg, err := fileConfig()
	assert.NoError(t, err, "reading configuration from file failed")

	assert.Equal(t, cluster, cfg.Cluster, "cluster name not as expected from file")
	assert.Equal(t, dockerAuthType, cfg.EngineAuthType, "docker auth type not as expected from file")
	assert.Equal(t, dockerAuth, string(cfg.EngineAuthData.Contents()), "docker auth data not as expected from file")
	assert.Equal(t, map[string]string{"attribute1": "value1"}, cfg.InstanceAttributes)
	assert.Equal(t, testPauseImageName, cfg.PauseContainerImageName, "should read PauseContainerImageName")
	assert.Equal(t, testPauseTag, cfg.PauseContainerTag, "should read PauseContainerTag")
	assert.Equal(t, 1, len(cfg.AWSVPCAdditionalLocalRoutes), "should have one additional local route")
	expectedLocalRoute, err := cnitypes.ParseCIDR("169.254.172.1/32")
	assert.NoError(t, err)
	assert.Equal(t, expectedLocalRoute.IP, cfg.AWSVPCAdditionalLocalRoutes[0].IP, "should match expected route IP")
	assert.Equal(t, expectedLocalRoute.Mask, cfg.AWSVPCAdditionalLocalRoutes[0].Mask, "should match expected route Mask")
	assert.Equal(t, ExplicitlyEnabled, cfg.TaskCPUMemLimit, "TaskCPUMemLimit should be explicitly enabled")
}

// TestDockerAuthMergeFromFile tests docker auth read from file correctly after merge
func TestDockerAuthMergeFromFile(t *testing.T) {
	cluster := "myCluster"
	dockerAuthType := "dockercfg"
	dockerAuth := `{
  "https://index.docker.io/v1/":{
    "auth":"admin",
    "email":"email"
  }
}`
	content := fmt.Sprintf(`{
  "AWSRegion": "not-real-1",
  "Cluster": "TestCluster",
  "EngineAuthType": "%s",
  "EngineAuthData": %s,
  "DataDir": "/var/run/ecs_agent",
  "TaskIAMRoleEnabled": true,
  "InstanceAttributes": {
    "attribute1": "value1"
  }
}`, dockerAuthType, dockerAuth)

	filePath := setupFileConfiguration(t, content)
	defer os.Remove(filePath)

	defer setTestEnv("ECS_CLUSTER", cluster)()
	defer setTestEnv("ECS_AGENT_CONFIG_FILE_PATH", filePath)()
	defer setTestEnv("AWS_DEFAULT_REGION", "us-west-2")()

	cfg, err := NewConfig(ec2.NewBlackholeEC2MetadataClient())
	assert.NoError(t, err, "create configuration failed")

	assert.Equal(t, cluster, cfg.Cluster, "cluster name not as expected from environment variable")
	assert.Equal(t, dockerAuthType, cfg.EngineAuthType, "docker auth type not as expected from file")
	assert.Equal(t, dockerAuth, string(cfg.EngineAuthData.Contents()), "docker auth data not as expected from file")
	assert.Equal(t, map[string]string{"attribute1": "value1"}, cfg.InstanceAttributes)
}

func TestBadFileContent(t *testing.T) {
	content := `{
	"AWSRegion": "not-real-1",
	"AWSVPCAdditionalLocalRoutes":["169.254.172.1/32", "300.300.300.300/32", "foo"]
	}`

	filePath := setupFileConfiguration(t, content)
	defer os.Remove(filePath)

	os.Setenv("ECS_AGENT_CONFIG_FILE_PATH", filePath)
	defer os.Unsetenv("ECS_AGENT_CONFIG_FILE_PATH")

	_, err := NewConfig(ec2.NewBlackholeEC2MetadataClient())
	assert.Error(t, err, "create configuration should fail")
}

func TestShouldLoadPauseContainerTarball(t *testing.T) {
	cfg := DefaultConfig()
	assert.True(t, cfg.ShouldLoadPauseContainerTarball(), "should load tarball by default")
	cfg.PauseContainerTag = "foo!"
	assert.False(t, cfg.ShouldLoadPauseContainerTarball(), "should not load tarball if tag differs")
	cfg = DefaultConfig()
	cfg.PauseContainerImageName = "foo!"
	assert.False(t, cfg.ShouldLoadPauseContainerTarball(), "should not load tarball if image name differs")
}

// setupFileConfiguration create a temp file store the configuration
func setupFileConfiguration(t *testing.T, configContent string) string {
	file, err := ioutil.TempFile("", "ecs-test")
	require.NoError(t, err, "creating temp file for configuration failed")

	_, err = file.Write([]byte(configContent))
	require.NoError(t, err, "writing configuration to file failed")

	return file.Name()
}
