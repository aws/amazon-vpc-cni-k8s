// !build windows
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
	"os"
	"testing"
	"time"

	"github.com/aws/amazon-ecs-agent/agent/ec2"
	"github.com/aws/amazon-ecs-agent/agent/engine/dockerclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigDefault(t *testing.T) {
	defer setTestRegion()()
	os.Unsetenv("ECS_HOST_DATA_DIR")

	cfg, err := NewConfig(ec2.NewBlackholeEC2MetadataClient())
	require.NoError(t, err)

	assert.Equal(t, "npipe:////./pipe/docker_engine", cfg.DockerEndpoint, "Default docker endpoint set incorrectly")
	assert.Equal(t, `C:\ProgramData\Amazon\ECS\data`, cfg.DataDir, "Default datadir set incorrectly")
	assert.False(t, cfg.DisableMetrics, "Default disablemetrics set incorrectly")
	assert.Equal(t, 10, len(cfg.ReservedPorts), "Default reserved ports set incorrectly")
	assert.Equal(t, uint16(0), cfg.ReservedMemory, "Default reserved memory set incorrectly")
	assert.Equal(t, 30*time.Second, cfg.DockerStopTimeout, "Default docker stop container timeout set incorrectly")
	assert.False(t, cfg.PrivilegedDisabled, "Default PrivilegedDisabled set incorrectly")
	assert.Equal(t, []dockerclient.LoggingDriver{dockerclient.JSONFileDriver, dockerclient.NoneDriver},
		cfg.AvailableLoggingDrivers, "Default logging drivers set incorrectly")
	assert.Equal(t, 3*time.Hour, cfg.TaskCleanupWaitDuration, "Default task cleanup wait duration set incorrectly")
	assert.False(t, cfg.TaskIAMRoleEnabled, "TaskIAMRoleEnabled set incorrectly")
	assert.False(t, cfg.TaskIAMRoleEnabledForNetworkHost, "TaskIAMRoleEnabledForNetworkHost set incorrectly")
	assert.False(t, cfg.CredentialsAuditLogDisabled, "CredentialsAuditLogDisabled set incorrectly")
	assert.Equal(t, `C:\ProgramData\Amazon\ECS\log\audit.log`, cfg.CredentialsAuditLogFile, "CredentialsAuditLogFile is set incorrectly")
	assert.False(t, cfg.ImageCleanupDisabled, "ImageCleanupDisabled default is set incorrectly")
	assert.Equal(t, DefaultImageDeletionAge, cfg.MinimumImageDeletionAge, "MinimumImageDeletionAge default is set incorrectly")
	assert.Equal(t, DefaultImageCleanupTimeInterval, cfg.ImageCleanupInterval, "ImageCleanupInterval default is set incorrectly")
	assert.Equal(t, DefaultNumImagesToDeletePerCycle, cfg.NumImagesToDeletePerCycle, "NumImagesToDeletePerCycle default is set incorrectly")
	assert.Equal(t, `C:\ProgramData\Amazon\ECS\data`, cfg.DataDirOnHost, "Default DataDirOnHost set incorrectly")
}

func TestConfigIAMTaskRolesReserves80(t *testing.T) {
	defer setTestRegion()()
	defer setTestEnv("ECS_ENABLE_TASK_IAM_ROLE", "true")()

	cfg, err := NewConfig(ec2.NewBlackholeEC2MetadataClient())
	assert.NoError(t, err)
	assert.Equal(t, []uint16{
		DockerReservedPort,
		DockerReservedSSLPort,
		AgentIntrospectionPort,
		AgentCredentialsPort,
		rdpPort,
		rpcPort,
		smbPort,
		winRMPort,
		dnsPort,
		netBIOSPort,
		httpPort,
	}, cfg.ReservedPorts)

	defer setTestEnv("ECS_RESERVED_PORTS", "[1]")()
	cfg, err = NewConfig(ec2.NewBlackholeEC2MetadataClient())
	assert.NoError(t, err)
	assert.Equal(t, []uint16{1, httpPort}, cfg.ReservedPorts)
}

func TestTaskResourceLimitPlatformOverrideDisabled(t *testing.T) {
	defer setTestRegion()()
	defer setTestEnv("ECS_ENABLE_TASK_CPU_MEM_LIMIT", "true")()
	cfg, err := NewConfig(ec2.NewBlackholeEC2MetadataClient())
	cfg.platformOverrides()
	assert.NoError(t, err)
	assert.False(t, cfg.TaskCPUMemLimit.Enabled())
}
