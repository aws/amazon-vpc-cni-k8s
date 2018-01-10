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

package config

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/aws/amazon-ecs-agent/agent/ec2"
	"github.com/aws/amazon-ecs-agent/agent/ec2/mocks"
	"github.com/aws/amazon-ecs-agent/agent/engine/dockerclient"

	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
)

func TestMerge(t *testing.T) {
	conf1 := &Config{Cluster: "Foo"}
	conf2 := Config{Cluster: "ignored", APIEndpoint: "Bar"}
	conf3 := Config{AWSRegion: "us-west-2"}

	conf1.Merge(conf2).Merge(conf3)

	assert.Equal(t, conf1.Cluster, "Foo", "The cluster should not have been overridden")
	assert.Equal(t, conf1.APIEndpoint, "Bar", "The APIEndpoint should have been merged in")
	assert.Equal(t, conf1.AWSRegion, "us-west-2", "Incorrect region")
}

func TestBrokenEC2Metadata(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockEc2Metadata := mock_ec2.NewMockEC2MetadataClient(ctrl)
	mockEc2Metadata.EXPECT().InstanceIdentityDocument().Return(ec2metadata.EC2InstanceIdentityDocument{}, errors.New("err"))

	_, err := NewConfig(mockEc2Metadata)
	assert.Error(t, err, "Expected error when region isn't set and metadata doesn't work")
}

func TestBrokenEC2MetadataEndpoint(t *testing.T) {
	defer setTestRegion()()
	ctrl := gomock.NewController(t)
	mockEc2Metadata := mock_ec2.NewMockEC2MetadataClient(ctrl)

	mockEc2Metadata.EXPECT().InstanceIdentityDocument().Return(ec2metadata.EC2InstanceIdentityDocument{}, errors.New("err"))

	config, err := NewConfig(mockEc2Metadata)
	assert.NoError(t, err)
	assert.Equal(t, config.AWSRegion, "us-west-2", "Wrong region")
	assert.Zero(t, config.APIEndpoint, "Endpoint env variable not set; endpoint should be blank")
}

func TestEnvironmentConfig(t *testing.T) {
	defer setTestRegion()()
	defer setTestEnv("ECS_CLUSTER", "myCluster")()
	defer setTestEnv("ECS_RESERVED_PORTS_UDP", "[42,99]")()
	defer setTestEnv("ECS_RESERVED_MEMORY", "20")()
	defer setTestEnv("ECS_CONTAINER_STOP_TIMEOUT", "60s")()
	defer setTestEnv("ECS_AVAILABLE_LOGGING_DRIVERS", "[\""+string(dockerclient.SyslogDriver)+"\"]")()
	defer setTestEnv("ECS_SELINUX_CAPABLE", "true")()
	defer setTestEnv("ECS_APPARMOR_CAPABLE", "true")()
	defer setTestEnv("ECS_DISABLE_PRIVILEGED", "true")()
	defer setTestEnv("ECS_ENGINE_TASK_CLEANUP_WAIT_DURATION", "90s")()
	defer setTestEnv("ECS_ENABLE_TASK_IAM_ROLE", "true")()
	defer setTestEnv("ECS_ENABLE_TASK_IAM_ROLE_NETWORK_HOST", "true")()
	defer setTestEnv("ECS_DISABLE_IMAGE_CLEANUP", "true")()
	defer setTestEnv("ECS_IMAGE_CLEANUP_INTERVAL", "2h")()
	defer setTestEnv("ECS_IMAGE_MINIMUM_CLEANUP_AGE", "30m")()
	defer setTestEnv("ECS_NUM_IMAGES_DELETE_PER_CYCLE", "2")()
	defer setTestEnv("ECS_INSTANCE_ATTRIBUTES", "{\"my_attribute\": \"testing\"}")()
	defer setTestEnv("ECS_ENABLE_TASK_ENI", "true")()
	additionalLocalRoutesJSON := `["1.2.3.4/22","5.6.7.8/32"]`
	setTestEnv("ECS_AWSVPC_ADDITIONAL_LOCAL_ROUTES", additionalLocalRoutesJSON)
	setTestEnv("ECS_ENABLE_CONTAINER_METADATA", "true")
	setTestEnv("ECS_HOST_DATA_DIR", "/etc/ecs/")

	conf, err := environmentConfig()
	assert.NoError(t, err)
	assert.Equal(t, "myCluster", conf.Cluster)
	assert.Equal(t, 2, len(conf.ReservedPortsUDP))
	assert.Contains(t, conf.ReservedPortsUDP, uint16(42))
	assert.Contains(t, conf.ReservedPortsUDP, uint16(99))
	assert.Equal(t, uint16(20), conf.ReservedMemory)
	expectedDuration, _ := time.ParseDuration("60s")
	assert.Equal(t, expectedDuration, conf.DockerStopTimeout)
	assert.Equal(t, []dockerclient.LoggingDriver{dockerclient.SyslogDriver}, conf.AvailableLoggingDrivers)
	assert.True(t, conf.PrivilegedDisabled)
	assert.True(t, conf.SELinuxCapable, "Wrong value for SELinuxCapable")
	assert.True(t, conf.AppArmorCapable, "Wrong value for AppArmorCapable")
	assert.True(t, conf.TaskIAMRoleEnabled, "Wrong value for TaskIAMRoleEnabled")
	assert.True(t, conf.TaskIAMRoleEnabledForNetworkHost, "Wrong value for TaskIAMRoleEnabledForNetworkHost")
	assert.True(t, conf.ImageCleanupDisabled, "Wrong value for ImageCleanupDisabled")

	assert.True(t, conf.TaskENIEnabled, "Wrong value for TaskNetwork")
	assert.Equal(t, (30 * time.Minute), conf.MinimumImageDeletionAge)
	assert.Equal(t, (2 * time.Hour), conf.ImageCleanupInterval)
	assert.Equal(t, 2, conf.NumImagesToDeletePerCycle)
	assert.Equal(t, "testing", conf.InstanceAttributes["my_attribute"])
	assert.Equal(t, (90 * time.Second), conf.TaskCleanupWaitDuration)
	serializedAdditionalLocalRoutesJSON, err := json.Marshal(conf.AWSVPCAdditionalLocalRoutes)
	assert.NoError(t, err, "should marshal additional local routes")
	assert.Equal(t, additionalLocalRoutesJSON, string(serializedAdditionalLocalRoutesJSON))
	assert.Equal(t, "/etc/ecs/", conf.DataDirOnHost, "Wrong value for DataDirOnHost")
	assert.True(t, conf.ContainerMetadataEnabled, "Wrong value for ContainerMetadataEnabled")
}

func TestTrimWhitespaceWhenCreating(t *testing.T) {
	defer setTestRegion()()
	defer setTestEnv("ECS_CLUSTER", "default \r")()
	defer setTestEnv("ECS_ENGINE_AUTH_TYPE", "dockercfg\r")()

	cfg, err := NewConfig(ec2.NewBlackholeEC2MetadataClient())
	assert.NoError(t, err)
	assert.Equal(t, cfg.Cluster, "default", "Wrong cluster")
	assert.Equal(t, cfg.EngineAuthType, "dockercfg", "Wrong auth type")
}

func TestTrimWhitespace(t *testing.T) {
	cfg := &Config{
		Cluster:   " asdf ",
		AWSRegion: " us-east-1\r\t",
		DataDir:   "/trailing/space/directory ",
	}

	cfg.trimWhitespace()
	assert.Equal(t, cfg, &Config{Cluster: "asdf", AWSRegion: "us-east-1", DataDir: "/trailing/space/directory "})
}

func TestConfigBoolean(t *testing.T) {
	defer setTestRegion()()
	defer setTestEnv("ECS_DISABLE_METRICS", "true")()
	cfg, err := NewConfig(ec2.NewBlackholeEC2MetadataClient())
	assert.NoError(t, err)
	assert.True(t, cfg.DisableMetrics)
}

func TestBadLoggingDriverSerialization(t *testing.T) {
	defer setTestEnv("ECS_AVAILABLE_LOGGING_DRIVERS", "[\"malformed]")
	defer setTestRegion()()
	conf, err := environmentConfig()
	assert.NoError(t, err)
	assert.Zero(t, len(conf.AvailableLoggingDrivers), "Wrong value for AvailableLoggingDrivers")
}

func TestBadAttributesSerialization(t *testing.T) {
	defer setTestRegion()()
	defer setTestEnv("ECS_INSTANCE_ATTRIBUTES", "This is not valid JSON")()
	_, err := environmentConfig()
	assert.Error(t, err)
}

func TestInvalidLoggingDriver(t *testing.T) {
	conf := DefaultConfig()
	conf.AWSRegion = "us-west-2"
	conf.AvailableLoggingDrivers = []dockerclient.LoggingDriver{"invalid-logging-driver"}
	assert.Error(t, conf.validateAndOverrideBounds(), "Should be error with invalid-logging-driver")
}

func TestInvalidFormatDockerStopTimeout(t *testing.T) {
	defer setTestRegion()()
	defer setTestEnv("ECS_CONTAINER_STOP_TIMEOUT", "invalid")()
	conf, err := environmentConfig()
	assert.NoError(t, err)
	assert.Zero(t, conf.DockerStopTimeout, "Wrong value for DockerStopTimeout")
}

func TestInvalideValueDockerStopTimeout(t *testing.T) {
	defer setTestRegion()()
	defer setTestEnv("ECS_CONTAINER_STOP_TIMEOUT", "-10s")()
	conf, err := environmentConfig()
	assert.NoError(t, err)
	assert.Zero(t, conf.DockerStopTimeout)
}

func TestInvalidDockerStopTimeout(t *testing.T) {
	conf := DefaultConfig()
	conf.DockerStopTimeout = -1 * time.Second
	assert.Error(t, conf.validateAndOverrideBounds(), "Should be error with negative DockerStopTimeout")
}

func TestInvalidFormatParseEnvVariableUint16(t *testing.T) {
	defer setTestRegion()()
	setTestEnv("FOO", "foo")
	var16 := parseEnvVariableUint16("FOO")
	assert.Zero(t, var16, "Expected 0 from parseEnvVariableUint16 for invalid Uint16 format")
}

func TestValidFormatParseEnvVariableUint16(t *testing.T) {
	defer setTestRegion()()
	setTestEnv("FOO", "1")
	var16 := parseEnvVariableUint16("FOO")
	assert.Equal(t, var16, uint16(1), "Unexpected value parsed in parseEnvVariableUint16.")
}

func TestInvalidFormatParseEnvVariableDuration(t *testing.T) {
	defer setTestRegion()()
	setTestEnv("FOO", "foo")
	duration := parseEnvVariableDuration("FOO")
	assert.Zero(t, duration, "Expected 0 from parseEnvVariableDuration for invalid format")
}

func TestValidFormatParseEnvVariableDuration(t *testing.T) {
	defer setTestRegion()()
	setTestEnv("FOO", "1s")
	duration := parseEnvVariableDuration("FOO")
	assert.Equal(t, duration, 1*time.Second, "Unexpected value parsed in parseEnvVariableDuration.")
}

func TestInvalidTaskCleanupTimeoutOverridesToThreeHours(t *testing.T) {
	defer setTestRegion()()
	setTestEnv("ECS_ENGINE_TASK_CLEANUP_WAIT_DURATION", "1s")
	cfg, err := NewConfig(ec2.NewBlackholeEC2MetadataClient())
	assert.NoError(t, err)

	// If an invalid value is set, the config should pick up the default value for
	// cleaning up the task.
	assert.Equal(t, cfg.TaskCleanupWaitDuration, 3*time.Hour, "Defualt task cleanup wait duration set incorrectly")
}

func TestTaskCleanupTimeout(t *testing.T) {
	defer setTestRegion()()
	defer setTestEnv("ECS_ENGINE_TASK_CLEANUP_WAIT_DURATION", "10m")()
	cfg, err := NewConfig(ec2.NewBlackholeEC2MetadataClient())
	assert.NoError(t, err)
	assert.Equal(t, cfg.TaskCleanupWaitDuration, 10*time.Minute, "Task cleanup wait duration set incorrectly")
}

func TestInvalidReservedMemoryOverridesToZero(t *testing.T) {
	defer setTestRegion()()
	defer setTestEnv("ECS_RESERVED_MEMORY", "-1")()
	cfg, err := NewConfig(ec2.NewBlackholeEC2MetadataClient())
	assert.NoError(t, err)
	// If an invalid value is set, the config should pick up the default value for
	// reserved memory, which is 0.
	assert.Zero(t, cfg.ReservedMemory, "Wrong value for ReservedMemory")
}

func TestReservedMemory(t *testing.T) {
	defer setTestRegion()()
	defer setTestEnv("ECS_RESERVED_MEMORY", "1")()
	cfg, err := NewConfig(ec2.NewBlackholeEC2MetadataClient())
	assert.NoError(t, err)
	assert.Equal(t, cfg.ReservedMemory, uint16(1), "Wrong value for ReservedMemory.")
}

func TestTaskIAMRoleEnabled(t *testing.T) {
	defer setTestRegion()()
	defer setTestEnv("ECS_ENABLE_TASK_IAM_ROLE", "true")()
	cfg, err := NewConfig(ec2.NewBlackholeEC2MetadataClient())
	assert.NoError(t, err)
	assert.True(t, cfg.TaskIAMRoleEnabled, "Wrong value for TaskIAMRoleEnabled")
}

func TestTaskIAMRoleForHostNetworkEnabled(t *testing.T) {
	defer setTestRegion()()
	defer setTestEnv("ECS_ENABLE_TASK_IAM_ROLE_NETWORK_HOST", "true")()
	cfg, err := NewConfig(ec2.NewBlackholeEC2MetadataClient())
	assert.NoError(t, err)
	assert.True(t, cfg.TaskIAMRoleEnabledForNetworkHost, "Wrong value for TaskIAMRoleEnabledForNetworkHost")
}

func TestCredentialsAuditLogFile(t *testing.T) {
	defer setTestRegion()()
	dummyLocation := "/foo/bar.log"
	defer setTestEnv("ECS_AUDIT_LOGFILE", dummyLocation)()
	cfg, err := NewConfig(ec2.NewBlackholeEC2MetadataClient())
	assert.NoError(t, err)
	assert.Equal(t, cfg.CredentialsAuditLogFile, dummyLocation, "Wrong value for CredentialsAuditLogFile")
}

func TestCredentialsAuditLogDisabled(t *testing.T) {
	defer setTestRegion()()
	defer setTestEnv("ECS_AUDIT_LOGFILE_DISABLED", "true")()
	cfg, err := NewConfig(ec2.NewBlackholeEC2MetadataClient())
	assert.NoError(t, err)
	assert.True(t, cfg.CredentialsAuditLogDisabled, "Wrong value for CredentialsAuditLogDisabled")
}

func TestImageCleanupMinimumInterval(t *testing.T) {
	defer setTestRegion()()
	defer setTestEnv("ECS_IMAGE_CLEANUP_INTERVAL", "1m")()
	cfg, err := NewConfig(ec2.NewBlackholeEC2MetadataClient())
	assert.NoError(t, err)
	assert.Equal(t, cfg.ImageCleanupInterval, DefaultImageCleanupTimeInterval, "Wrong value for ImageCleanupInterval")
}

func TestImageCleanupMinimumNumImagesToDeletePerCycle(t *testing.T) {
	defer setTestRegion()()
	defer setTestEnv("ECS_NUM_IMAGES_DELETE_PER_CYCLE", "-1")()
	cfg, err := NewConfig(ec2.NewBlackholeEC2MetadataClient())
	assert.NoError(t, err)
	assert.Equal(t, cfg.NumImagesToDeletePerCycle, DefaultNumImagesToDeletePerCycle, "Wrong value for NumImagesToDeletePerCycle")
}

func TestTaskResourceLimitsOverride(t *testing.T) {
	defer setTestRegion()()
	defer setTestEnv("ECS_ENABLE_TASK_CPU_MEM_LIMIT", "false")()
	cfg, err := NewConfig(ec2.NewBlackholeEC2MetadataClient())
	assert.NoError(t, err)
	assert.False(t, cfg.TaskCPUMemLimit.Enabled(), "Task cpu and memory limits should be overridden to false")
	assert.Equal(t, ExplicitlyDisabled, cfg.TaskCPUMemLimit, "Task cpu and memory limits should be explicitly set")
}

func TestAWSVPCBlockInstanceMetadata(t *testing.T) {
	defer setTestEnv("ECS_AWSVPC_BLOCK_IMDS", "true")()
	defer setTestRegion()()
	cfg, err := NewConfig(ec2.NewBlackholeEC2MetadataClient())
	assert.NoError(t, err)
	assert.True(t, cfg.AWSVPCBlockInstanceMetdata)
}

func TestInvalidAWSVPCAdditionalLocalRoutes(t *testing.T) {
	os.Setenv("ECS_AWSVPC_ADDITIONAL_LOCAL_ROUTES", `["300.300.300.300/64"]`)
	defer os.Unsetenv("ECS_AWSVPC_ADDITIONAL_LOCAL_ROUTES")
	_, err := environmentConfig()
	assert.Error(t, err)
}

func TestAWSLogsExecutionRole(t *testing.T) {
	setTestEnv("ECS_ENABLE_AWSLOGS_EXECUTIONROLE_OVERRIDE", "true")
	conf, err := environmentConfig()
	assert.NoError(t, err)
	assert.True(t, conf.OverrideAWSLogsExecutionRole)
}

func setTestRegion() func() {
	return setTestEnv("AWS_DEFAULT_REGION", "us-west-2")
}

func setTestEnv(k, v string) func() {
	os.Setenv(k, v)
	return func() {
		os.Unsetenv(k)
	}
}
