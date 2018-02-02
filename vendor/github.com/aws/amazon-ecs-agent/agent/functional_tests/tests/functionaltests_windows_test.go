// +build windows,functional

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

package functional_tests

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	ecsapi "github.com/aws/amazon-ecs-agent/agent/ecs_client/model/ecs"
	. "github.com/aws/amazon-ecs-agent/agent/functional_tests/util"
	"github.com/aws/amazon-ecs-agent/agent/utils"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudwatch"
	"github.com/aws/aws-sdk-go/service/cloudwatchlogs"
	"github.com/pborman/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	savedStateTaskDefinition        = "savedstate-windows"
	portResContentionTaskDefinition = "port-80-windows"
	labelsTaskDefinition            = "labels-windows"
	logDriverTaskDefinition         = "logdriver-jsonfile-windows"
	cleanupTaskDefinition           = "cleanup-windows"
	networkModeTaskDefinition       = "network-mode-windows"
	cpuSharesPerCore                = 1024
)

// TestAWSLogsDriver verifies that container logs are sent to Amazon CloudWatch Logs with awslogs as the log driver
func TestAWSLogsDriver(t *testing.T) {
	RequireDockerVersion(t, ">=1.9.0") // awslogs drivers available from docker 1.9.0
	cwlClient := cloudwatchlogs.New(session.New(), aws.NewConfig().WithRegion(*ECS.Config.Region))
	// Test whether the log group existed or not
	respDescribeLogGroups, err := cwlClient.DescribeLogGroups(&cloudwatchlogs.DescribeLogGroupsInput{
		LogGroupNamePrefix: aws.String(awslogsLogGroupName),
	})
	require.NoError(t, err, "CloudWatchLogs describe log groups failed")
	logGroupExists := false
	for i := 0; i < len(respDescribeLogGroups.LogGroups); i++ {
		if *respDescribeLogGroups.LogGroups[i].LogGroupName == awslogsLogGroupName {
			logGroupExists = true
			break
		}
	}

	if !logGroupExists {
		_, err := cwlClient.CreateLogGroup(&cloudwatchlogs.CreateLogGroupInput{
			LogGroupName: aws.String(awslogsLogGroupName),
		})
		require.NoError(t, err, "Failed to create log group %s", awslogsLogGroupName)
	}

	agentOptions := AgentOptions{
		ExtraEnvironment: map[string]string{
			"ECS_AVAILABLE_LOGGING_DRIVERS": `["awslogs"]`,
		},
	}
	agent := RunAgent(t, &agentOptions)
	defer agent.Cleanup()
	agent.RequireVersion(">=1.9.0") //Required for awslogs driver

	tdOverrides := make(map[string]string)
	tdOverrides["$$$TEST_REGION$$$"] = *ECS.Config.Region

	testTask, err := agent.StartTaskWithTaskDefinitionOverrides(t, "awslogs-windows", tdOverrides)
	require.NoError(t, err, "Expected to start task using awslogs driver failed")

	// Wait for the container to start
	testTask.WaitRunning(waitTaskStateChangeDuration)
	strs := strings.Split(*testTask.TaskArn, "/")
	taskId := strs[len(strs)-1]

	// Delete the log stream after the test
	defer func() {
		cwlClient.DeleteLogStream(&cloudwatchlogs.DeleteLogStreamInput{
			LogGroupName:  aws.String(awslogsLogGroupName),
			LogStreamName: aws.String(fmt.Sprintf("ecs-functional-tests/awslogs/%s", taskId)),
		})
	}()

	// Added a delay of 1 minute to allow the task to be stopped - Windows only.
	testTask.WaitStopped(1 * time.Minute)
	params := &cloudwatchlogs.GetLogEventsInput{
		LogGroupName:  aws.String(awslogsLogGroupName),
		LogStreamName: aws.String(fmt.Sprintf("ecs-functional-tests/awslogs/%s", taskId)),
	}

	resp, err := waitCloudwatchLogs(cwlClient, params)
	require.NoError(t, err, "CloudWatchLogs get log failed")
	assert.Len(t, resp.Events, 1, fmt.Sprintf("Get unexpected number of log events: %d", len(resp.Events)))
}

func TestTaskIamRolesDefaultNetworkMode(t *testing.T) {
	// The test runs only when the environment TEST_IAM_ROLE was set
	if os.Getenv("TEST_TASK_IAM_ROLE") != "true" {
		t.Skip("Skipping test TaskIamRole in default network mode, as TEST_TASK_IAM_ROLE isn't set true")
	}

	agentOptions := &AgentOptions{
		ExtraEnvironment: map[string]string{
			"ECS_ENABLE_TASK_IAM_ROLE": "true",
		},
	}
	agent := RunAgent(t, agentOptions)
	defer agent.Cleanup()

	taskIamRolesTest("", agent, t)
}

func taskIamRolesTest(networkMode string, agent *TestAgent, t *testing.T) {
	RequireDockerVersion(t, ">=1.11.0") // TaskIamRole is available from agent 1.11.0
	roleArn := os.Getenv("TASK_IAM_ROLE_ARN")
	if utils.ZeroOrNil(roleArn) {
		t.Logf("TASK_IAM_ROLE_ARN not set, will try to use the role attached to instance profile")
		role, err := GetInstanceIAMRole()
		if err != nil {
			t.Fatalf("Error getting IAM Roles from instance profile, err: %v", err)
		}
		roleArn = *role.Arn
	}

	tdOverride := make(map[string]string)
	tdOverride["$$$TASK_ROLE$$$"] = roleArn
	tdOverride["$$$TEST_REGION$$$"] = *ECS.Config.Region
	tdOverride["$$$NETWORK_MODE$$$"] = networkMode

	task, err := agent.StartTaskWithTaskDefinitionOverrides(t, "iam-roles-windows", tdOverride)
	if err != nil {
		t.Fatalf("Error start iam-roles task: %v", err)
	}
	err = task.WaitRunning(waitTaskStateChangeDuration)
	if err != nil {
		t.Fatalf("Error waiting for task to run: %v", err)
	}
	containerId, err := agent.ResolveTaskDockerID(task, "container-with-iamrole-windows")
	if err != nil {
		t.Fatalf("Error resolving docker id for container in task: %v", err)
	}

	// TaskIAMRoles enabled contaienr should have the ExtraEnvironment variable AWS_CONTAINER_CREDENTIALS_RELATIVE_URI
	containerMetaData, err := agent.DockerClient.InspectContainer(containerId)
	if err != nil {
		t.Fatalf("Could not inspect container for task: %v", err)
	}
	iamRoleEnabled := false
	if containerMetaData.Config != nil {
		for _, env := range containerMetaData.Config.Env {
			if strings.HasPrefix(env, "AWS_CONTAINER_CREDENTIALS_RELATIVE_URI=") {
				iamRoleEnabled = true
				break
			}
		}
	}
	if !iamRoleEnabled {
		task.Stop()
		t.Fatalf("Could not found AWS_CONTAINER_CREDENTIALS_RELATIVE_URI in the container envrionment variable")
	}

	// Task will only run one command "aws ec2 describe-regions"
	err = task.WaitStopped(waitTaskStateChangeDuration)
	if err != nil {
		t.Fatalf("Waiting task to stop error : %v", err)
	}

	containerMetaData, err = agent.DockerClient.InspectContainer(containerId)
	if err != nil {
		t.Fatalf("Could not inspect container for task: %v", err)
	}

	if containerMetaData.State.ExitCode != 42 {
		t.Fatalf("Container exit code non-zero: %v", containerMetaData.State.ExitCode)
	}

}

// TestMetadataServiceValidator Tests that the metadata file can be accessed from the
// container using the ECS_CONTAINER_METADATA_FILE environment variables
func TestMetadataServiceValidator(t *testing.T) {
	agentOptions := &AgentOptions{
		ExtraEnvironment: map[string]string{
			"ECS_ENABLE_CONTAINER_METADATA": "true",
		},
	}

	agent := RunAgent(t, agentOptions)
	defer agent.Cleanup()

	tdOverride := make(map[string]string)
	tdOverride["$$$TEST_REGION$$$"] = *ECS.Config.Region
	tdOverride["$$$NETWORK_MODE$$$"] = ""

	task, err := agent.StartTaskWithTaskDefinitionOverrides(t, "mdservice-validator-windows", tdOverride)
	if err != nil {
		t.Fatalf("Error starting mdservice-validator-windows: %v", err)
	}

	// clean up
	err = task.WaitStopped(waitTaskStateChangeDuration)
	require.NoError(t, err, "Error waiting for task to transition to STOPPED")

	containerID, err := agent.ResolveTaskDockerID(task, "mdservice-validator-windows")
	if err != nil {
		t.Fatalf("Error resolving docker id for container in task: %v", err)
	}

	containerMetaData, err := agent.DockerClient.InspectContainer(containerID)
	require.NoError(t, err, "Could not inspect container for task")

	exitCode := containerMetaData.State.ExitCode
	assert.Equal(t, 42, exitCode, fmt.Sprintf("Expected exit code of 42; got %d", exitCode))
}

// TestTelemetry tests whether agent can send metrics to TACS
func TestTelemetry(t *testing.T) {
	// Try to use a new cluster for this test, ensure no other task metrics for this cluster
	newClusterName := "ecstest-telemetry-" + uuid.New()
	_, err := ECS.CreateCluster(&ecsapi.CreateClusterInput{
		ClusterName: aws.String(newClusterName),
	})
	require.NoError(t, err, "Failed to create cluster")
	defer DeleteCluster(t, newClusterName)

	agentOptions := AgentOptions{
		ExtraEnvironment: map[string]string{
			"ECS_CLUSTER": newClusterName,
		},
	}
	agent := RunAgent(t, &agentOptions)
	defer agent.Cleanup()

	params := &cloudwatch.GetMetricStatisticsInput{
		MetricName: aws.String("CPUUtilization"),
		Namespace:  aws.String("AWS/ECS"),
		Period:     aws.Int64(60),
		Statistics: []*string{
			aws.String("Average"),
			aws.String("SampleCount"),
		},
		Dimensions: []*cloudwatch.Dimension{
			{
				Name:  aws.String("ClusterName"),
				Value: aws.String(newClusterName),
			},
		},
	}
	params.StartTime = aws.Time(RoundTimeUp(time.Now(), time.Minute).UTC())
	params.EndTime = aws.Time((*params.StartTime).Add(waitMetricsInCloudwatchDuration).UTC())
	// wait for the agent start and ensure no task is running
	time.Sleep(waitMetricsInCloudwatchDuration)

	cwclient := cloudwatch.New(session.New(), aws.NewConfig().WithRegion(*ECS.Config.Region))
	_, err = VerifyMetrics(cwclient, params, true)
	assert.NoError(t, err, "Before task running, verify metrics for CPU utilization failed")

	params.MetricName = aws.String("MemoryUtilization")
	_, err = VerifyMetrics(cwclient, params, true)
	assert.NoError(t, err, "Before task running, verify metrics for memory utilization failed")

	cpuNum := runtime.NumCPU()

	tdOverrides := make(map[string]string)
	// Set the container cpu percentage 25%
	tdOverrides["$$$$CPUSHARE$$$$"] = strconv.Itoa(int(float64(cpuNum*cpuSharesPerCore) * 0.25))

	testTask, err := agent.StartTaskWithTaskDefinitionOverrides(t, "telemetry-windows", tdOverrides)
	require.NoError(t, err, "Failed to start telemetry task")
	// Wait for the task to run and the agent to send back metrics
	err = testTask.WaitRunning(waitTaskStateChangeDuration)
	require.NoError(t, err, "Error wait telemetry task running")

	time.Sleep(waitMetricsInCloudwatchDuration)
	params.EndTime = aws.Time(RoundTimeUp(time.Now(), time.Minute).UTC())
	params.StartTime = aws.Time((*params.EndTime).Add(-waitMetricsInCloudwatchDuration).UTC())
	params.MetricName = aws.String("CPUUtilization")
	metrics, err := VerifyMetrics(cwclient, params, false)
	assert.NoError(t, err, "Task is running, verify metrics for CPU utilization failed")
	// Also verify the cpu usage is around 25%
	assert.InDelta(t, 0.25, *metrics.Average, 0.05)

	params.MetricName = aws.String("MemoryUtilization")
	_, err = VerifyMetrics(cwclient, params, false)
	assert.NoError(t, err, "Task is running, verify metrics for memory utilization failed")

	err = testTask.Stop()
	require.NoError(t, err, "Failed to stop the telemetry task")

	err = testTask.WaitStopped(waitTaskStateChangeDuration)
	require.NoError(t, err, "Waiting for task stop failed")

	time.Sleep(waitMetricsInCloudwatchDuration)
	params.EndTime = aws.Time(RoundTimeUp(time.Now(), time.Minute).UTC())
	params.StartTime = aws.Time((*params.EndTime).Add(-waitMetricsInCloudwatchDuration).UTC())
	params.MetricName = aws.String("CPUUtilization")
	_, err = VerifyMetrics(cwclient, params, true)
	assert.NoError(t, err, "Task stopped: verify metrics for CPU utilization failed")

	params.MetricName = aws.String("MemoryUtilization")
	_, err = VerifyMetrics(cwclient, params, true)
	assert.NoError(t, err, "Task stopped, verify metrics for memory utilization failed")
}

// TestOOMContainer verifies that an OOM container returns an error
func TestOOMContainer(t *testing.T) {
	agent := RunAgent(t, nil)
	defer agent.Cleanup()

	testTask, err := agent.StartTask(t, "oom-windows")
	require.NoError(t, err, "Expected to start invalid-image task")
	err = testTask.WaitRunning(waitTaskStateChangeDuration)
	assert.NoError(t, err, "Expect task to be running")
	err = testTask.WaitStopped(waitTaskStateChangeDuration)
	assert.NoError(t, err, "Expect task to be stopped")
	assert.NotEqual(t, 0, testTask.Containers[0].ExitCode, "container should fail with memory error")
}
