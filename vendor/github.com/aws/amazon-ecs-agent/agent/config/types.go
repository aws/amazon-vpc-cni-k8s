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
	"time"

	"github.com/aws/amazon-ecs-agent/agent/engine/dockerclient"
	cnitypes "github.com/containernetworking/cni/pkg/types"
)

type Config struct {
	// DEPRECATED
	// ClusterArn is the Name or full ARN of a Cluster to register into. It has
	// been deprecated (and will eventually be removed) in favor of Cluster
	ClusterArn string `deprecated:"Please use Cluster instead"`
	// Cluster can either be the Name or full ARN of a Cluster. This is the
	// cluster the agent should register this ContainerInstance into. If this
	// value is not set, it will default to "default"
	Cluster string `trim:"true"`
	// APIEndpoint is the endpoint, such as "ecs.us-east-1.amazonaws.com", to
	// make calls against. If this value is not set, it will default to the
	// endpoint for your current AWSRegion
	APIEndpoint string `trim:"true"`
	// DockerEndpoint is the address the agent will attempt to connect to the
	// Docker daemon at. This should have the same value as "DOCKER_HOST"
	// normally would to interact with the daemon. It defaults to
	// unix:///var/run/docker.sock
	DockerEndpoint string
	// AWSRegion is the region to run in (such as "us-east-1"). This value will
	// be inferred from the EC2 metadata service, but if it cannot be found this
	// will be fatal.
	AWSRegion string `missing:"fatal" trim:"true"`

	// ReservedPorts is an array of ports which should be registerd as
	// unavailable. If not set, they default to [22,2375,2376,51678].
	ReservedPorts []uint16
	// ReservedPortsUDP is an array of UDP ports which should be registered as
	// unavailable. If not set, it defaults to [].
	ReservedPortsUDP []uint16

	// DataDir is the directory data is saved to in order to preserve state
	// across agent restarts.
	// It is also used to keep the metadata of containers managed by the agent
	DataDir string
	// DataDirOnHost is the directory in the instance from which we mount
	// DataDir to the ecs-agent container and to agent managed containers
	DataDirOnHost string
	// Checkpoint configures whether data should be periodically to a checkpoint
	// file, in DataDir, such that on instance or agent restarts it will resume
	// as the same ContainerInstance. It defaults to false.
	Checkpoint bool

	// EngineAuthType configures what type of data is in EngineAuthData.
	// Supported types, right now, can be found in the dockerauth package: https://godoc.org/github.com/aws/amazon-ecs-agent/agent/engine/dockerauth
	EngineAuthType string `trim:"true"`
	// EngineAuthData contains authentication data. Please see the documentation
	// for EngineAuthType for more information.
	EngineAuthData *SensitiveRawMessage

	// UpdatesEnabled specifies whether updates should be applied to this agent.
	// Default true
	UpdatesEnabled bool
	// UpdateDownloadDir specifies where new agent versions should be placed
	// within the container in order for the external updating process to
	// correctly handle them.
	UpdateDownloadDir string

	// DisableMetrics configures whether task utilization metrics should be
	// sent to the ECS telemetry endpoint
	DisableMetrics bool

	// ReservedMemory specifies the amount of memory (in MB) to reserve for things
	// other than containers managed by ECS
	ReservedMemory uint16

	// DockerStopTimeout specifies the amount time before a SIGKILL is issued to
	// containers managed by ECS
	DockerStopTimeout time.Duration

	// AvailableLoggingDrivers specifies the logging drivers available for use
	// with Docker.  If not set, it defaults to ["json-file","none"].
	AvailableLoggingDrivers []dockerclient.LoggingDriver

	// PrivilegedDisabled specified whether the Agent is capable of launching
	// tasks with privileged containers
	PrivilegedDisabled bool

	// SELinxuCapable specifies whether the Agent is capable of using SELinux
	// security options
	SELinuxCapable bool

	// AppArmorCapable specifies whether the Agent is capable of using AppArmor
	// security options
	AppArmorCapable bool

	// TaskCleanupWaitDuration specifies the time to wait after a task is stopped
	// until cleanup of task resources is started.
	TaskCleanupWaitDuration time.Duration

	// TaskIAMRoleEnabled specifies if the Agent is capable of launching
	// tasks with IAM Roles.
	TaskIAMRoleEnabled bool

	// TaskCPUMemLimit specifies if Agent can launch a task with a hierarchical cgroup
	TaskCPUMemLimit Conditional

	// CredentialsAuditLogFile specifies the path/filename of the audit log.
	CredentialsAuditLogFile string

	// CredentialsAuditLogEnabled specifies whether audit logging is disabled.
	CredentialsAuditLogDisabled bool

	// TaskIAMRoleEnabledForNetworkHost specifies if the Agent is capable of launching
	// tasks with IAM Roles when networkMode is set to 'host'
	TaskIAMRoleEnabledForNetworkHost bool

	// TaskENIEnabled specifies if the Agent is capable of launching task within
	// defined EC2 networks
	TaskENIEnabled bool

	// ImageCleanupDisabled specifies whether the Agent will periodically perform
	// automated image cleanup
	ImageCleanupDisabled bool

	// MinimumImageDeletionAge specifies the minimum time since it was pulled
	// before it can be deleted
	MinimumImageDeletionAge time.Duration

	// ImageCleanupInterval specifies the time to wait before performing the image
	// cleanup since last time it was executed
	ImageCleanupInterval time.Duration

	// NumImagesToDeletePerCycle specifies the num of image to delete every time
	// when Agent performs cleanup
	NumImagesToDeletePerCycle int

	// InstanceAttributes contains key/value pairs representing
	// attributes to be associated with this instance within the
	// ECS service and used to influence behavior such as launch
	// placement.
	InstanceAttributes map[string]string

	// Set if clients validate ssl certificates. Used mainly for testing
	AcceptInsecureCert bool `json:"-"`

	// CNIPluginsPath is the path for the cni plugins
	CNIPluginsPath string

	// PauseContainerTarballPath is the path to the pause container tarball
	PauseContainerTarballPath string

	// PauseContainerImageName is the name for the pause container image.
	// Setting this value to be different from the default will disable loading
	// the image from the tarball; the referenced image must already be loaded.
	PauseContainerImageName string

	// PauseContainerTag is the tag for the pause container image.
	// Setting this value to be different from the default will disable loading
	// the image from the tarball; the referenced image must already be loaded.
	PauseContainerTag string

	// AWSVPCBlockInstanceMetdata specifies if InstanceMetadata endpoint should be blocked
	// for tasks that are launched with network mode "awsvpc" when ECS_AWSVPC_BLOCK_IMDS=true
	AWSVPCBlockInstanceMetdata bool

	// OverrideAWSVPCLocalIPv4Address overrides the local IPv4 address chosen
	// for a task using the `awsvpc` networking mode. Using this configuration
	// will limit you to running one `awsvpc` task at a time. IPv4 addresses
	// must be specified in decimal-octet form and also specify the subnet
	// size (e.g., "169.254.172.42/22").
	OverrideAWSVPCLocalIPv4Address *cnitypes.IPNet

	// AWSVPCAdditionalLocalRoutes allows the specification of routing table
	// entries that will be added in the task's network namespace via the
	// instance bridge interface rather than via the ENI.
	AWSVPCAdditionalLocalRoutes []cnitypes.IPNet

	// ContainerMetadataEnabled specifies if the agent should provide a metadata
	// file for containers.
	ContainerMetadataEnabled bool

	// OverrideAWSLogsExecutionRole is config option used to enable awslogs
	// driver authentication over the task's execution role
	OverrideAWSLogsExecutionRole bool
}
