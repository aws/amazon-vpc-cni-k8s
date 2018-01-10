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

	"github.com/aws/amazon-ecs-agent/agent/engine/dockerclient"
)

const (
	// AgentCredentialsAddress is used to serve the credentials for tasks.
	AgentCredentialsAddress = "" // this is left blank right now for net=bridge
	// defaultAuditLogFile specifies the default audit log filename
	defaultCredentialsAuditLogFile = "/log/audit.log"
	// Default cgroup prefix for ECS tasks
	DefaultTaskCgroupPrefix = "/ecs"
)

// DefaultConfig returns the default configuration for Linux
func DefaultConfig() Config {
	return Config{
		DockerEndpoint:              "unix:///var/run/docker.sock",
		ReservedPorts:               []uint16{SSHPort, DockerReservedPort, DockerReservedSSLPort, AgentIntrospectionPort, AgentCredentialsPort},
		ReservedPortsUDP:            []uint16{},
		DataDir:                     "/data/",
		DataDirOnHost:               "/var/lib/ecs",
		DisableMetrics:              false,
		ReservedMemory:              0,
		AvailableLoggingDrivers:     []dockerclient.LoggingDriver{dockerclient.JSONFileDriver, dockerclient.NoneDriver},
		TaskCleanupWaitDuration:     DefaultTaskCleanupWaitDuration,
		DockerStopTimeout:           DefaultDockerStopTimeout,
		CredentialsAuditLogFile:     defaultCredentialsAuditLogFile,
		CredentialsAuditLogDisabled: false,
		ImageCleanupDisabled:        false,
		MinimumImageDeletionAge:     DefaultImageDeletionAge,
		ImageCleanupInterval:        DefaultImageCleanupTimeInterval,
		NumImagesToDeletePerCycle:   DefaultNumImagesToDeletePerCycle,
		CNIPluginsPath:              defaultCNIPluginsPath,
		PauseContainerTarballPath:   pauseContainerTarballPath,
		PauseContainerImageName:     DefaultPauseContainerImageName,
		PauseContainerTag:           DefaultPauseContainerTag,
		AWSVPCBlockInstanceMetdata:  false,
		ContainerMetadataEnabled:    false,
		TaskCPUMemLimit:             DefaultEnabled,
	}
}

func (cfg *Config) platformOverrides() {}

// ShouldLoadPauseContainerTarball determines whether the pause container
// tarball should be loaded into Docker or not.  This function will return
// false is the default image name/tag have been overridden, because we do not
// expect the tarball to match the overridden name/tag.
func (cfg *Config) ShouldLoadPauseContainerTarball() bool {
	return cfg.PauseContainerImageName == DefaultPauseContainerImageName &&
		cfg.PauseContainerTag == DefaultPauseContainerTag
}

// platformString returns platform-specific config data that can be serialized
// to string for debugging
func (cfg *Config) platformString() string {
	if cfg.ShouldLoadPauseContainerTarball() {
		return fmt.Sprintf(", PauseContainerImageName: %s, PauseContainerTag: %s",
			cfg.PauseContainerImageName, cfg.PauseContainerTag)
	}
	return ""
}
