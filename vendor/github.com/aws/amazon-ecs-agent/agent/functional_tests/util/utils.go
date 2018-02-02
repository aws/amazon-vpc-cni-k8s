// +build functional

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

package util

import (
	"crypto/md5"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/aws/amazon-ecs-agent/agent/ecs_client/model/ecs"
	"github.com/aws/amazon-ecs-agent/agent/handlers"
	"github.com/aws/amazon-ecs-agent/agent/utils"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudwatch"
	"github.com/aws/aws-sdk-go/service/iam"
	docker "github.com/fsouza/go-dockerclient"
	"golang.org/x/net/context"
)

// GetTaskDefinition is a helper that provies the family:revision for the named
// task definition where the name matches the folder in which the task
// definition is present. In order to avoid re-registering a task definition
// when it has already been regestered in the past, this registers a task
// definition of the pattern 'family-md5sum' with md5sum being the input task
// definition json's md5. This special family name is checked for existence
// before a new one is registered and it is assumed that if it exists, the task
// definition currently represented by the file was registered as such already.
func GetTaskDefinition(name string) (string, error) {
	return GetTaskDefinitionWithOverrides(name, make(map[string]string))
}

func GetTaskDefinitionWithOverrides(name string, overrides map[string]string) (string, error) {
	_, filename, _, _ := runtime.Caller(0)
	tdDataFromFile, err := ioutil.ReadFile(filepath.Join(path.Dir(filename), "..", "testdata", "taskdefinitions", name, "task-definition.json"))
	if err != nil {
		return "", err
	}

	tdStr := string(tdDataFromFile)
	for key, value := range overrides {
		tdStr = strings.Replace(tdStr, key, value, -1)
	}
	tdData := []byte(tdStr)

	registerRequest := &ecs.RegisterTaskDefinitionInput{}
	err = json.Unmarshal(tdData, registerRequest)
	if err != nil {
		return "", err
	}

	tdHash := fmt.Sprintf("%x", md5.Sum(tdData))
	idempotentFamily := *registerRequest.Family + "-" + tdHash

	existing, err := ECS.DescribeTaskDefinition(&ecs.DescribeTaskDefinitionInput{
		TaskDefinition: &idempotentFamily,
	})
	if err == nil {
		return fmt.Sprintf("%s:%d", *existing.TaskDefinition.Family, *existing.TaskDefinition.Revision), nil
	}

	registerRequest.Family = &idempotentFamily

	registered, err := ECS.RegisterTaskDefinition(registerRequest)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s:%d", *registered.TaskDefinition.Family, *registered.TaskDefinition.Revision), nil
}

type TestAgent struct {
	Image                string
	DockerID             string
	IntrospectionURL     string
	Version              string
	ContainerInstanceArn string
	Cluster              string
	TestDir              string
	Logdir               string
	Options              *AgentOptions
	Process              *os.Process

	DockerClient *docker.Client
	t            *testing.T
}

type AgentOptions struct {
	ExtraEnvironment map[string]string
	ContainerLinks   []string
	PortBindings     map[docker.Port]map[string]string
}

// verifyIntrospectionAPI verifies that we can talk to the agent's introspection http endpoint.
// This is a platform-independent piece of Agent Startup.
func (agent *TestAgent) verifyIntrospectionAPI() error {
	// Wait up to 10s for it to register
	var localMetadata handlers.MetadataResponse
	for i := 0; i < 10; i++ {
		func() {
			agentMetadataResp, err := http.Get(agent.IntrospectionURL + "/v1/metadata")
			if err != nil {
				return
			}
			metadata, err := ioutil.ReadAll(agentMetadataResp.Body)
			if err != nil {
				return
			}

			json.Unmarshal(metadata, &localMetadata)
		}()
		if localMetadata.ContainerInstanceArn != nil && *localMetadata.ContainerInstanceArn != "" {
			break
		}
		time.Sleep(1 * time.Second)
	}
	if localMetadata.ContainerInstanceArn == nil {
		agent.DockerClient.StopContainer(agent.DockerID, 1)
		return errors.New("Could not get agent metadata after launching it")
	}

	agent.ContainerInstanceArn = *localMetadata.ContainerInstanceArn
	fmt.Println("Container InstanceArn:", agent.ContainerInstanceArn)
	agent.Cluster = localMetadata.Cluster
	agent.Version = utils.ExtractVersion(localMetadata.Version)
	agent.t.Logf("Found agent metadata: %+v", localMetadata)
	return nil
}

// Platform Independent piece of Agent Cleanup. Gets executed on both linux and Windows.
func (agent *TestAgent) platformIndependentCleanup() {
	agent.StopAgent()
	if agent.t.Failed() {
		agent.t.Logf("Preserving test dir for failed test %s", agent.TestDir)
	} else {
		agent.t.Logf("Removing test dir for passed test %s", agent.TestDir)
		os.RemoveAll(agent.TestDir)
	}
	ECS.DeregisterContainerInstance(&ecs.DeregisterContainerInstanceInput{
		Cluster:           &agent.Cluster,
		ContainerInstance: &agent.ContainerInstanceArn,
		Force:             aws.Bool(true),
	})
}

func (agent *TestAgent) StartMultipleTasks(t *testing.T, taskDefinition string, num int) ([]*TestTask, error) {
	t.Logf("Task definition: %s", taskDefinition)
	cis := make([]*string, num)
	for i := 0; i < num; i++ {
		cis[i] = &agent.ContainerInstanceArn
	}

	resp, err := ECS.StartTask(&ecs.StartTaskInput{
		Cluster:            &agent.Cluster,
		ContainerInstances: cis,
		TaskDefinition:     &taskDefinition,
	})
	if err != nil {
		return nil, err
	}
	if len(resp.Failures) != 0 || len(resp.Tasks) == 0 {
		return nil, errors.New("Failure starting task: " + *resp.Failures[0].Reason)
	}

	testTasks := make([]*TestTask, num)
	for i, task := range resp.Tasks {
		agent.t.Logf("Started task: %s\n", *task.TaskArn)
		testTasks[i] = &TestTask{task}
	}
	return testTasks, nil
}

func (agent *TestAgent) StartTask(t *testing.T, task string) (*TestTask, error) {
	td, err := GetTaskDefinition(task)
	if err != nil {
		return nil, err
	}

	tasks, err := agent.StartMultipleTasks(t, td, 1)
	if err != nil {
		return nil, err
	}
	return tasks[0], nil
}

func (agent *TestAgent) StartTaskWithTaskDefinitionOverrides(t *testing.T, task string, overrides map[string]string) (*TestTask, error) {
	td, err := GetTaskDefinitionWithOverrides(task, overrides)
	if err != nil {
		return nil, err
	}

	tasks, err := agent.StartMultipleTasks(t, td, 1)
	if err != nil {
		return nil, err
	}

	return tasks[0], nil
}

func (agent *TestAgent) StartTaskWithOverrides(t *testing.T, task string, overrides []*ecs.ContainerOverride) (*TestTask, error) {
	td, err := GetTaskDefinition(task)
	if err != nil {
		return nil, err
	}
	t.Logf("Task definition: %s", td)

	resp, err := ECS.StartTask(&ecs.StartTaskInput{
		Cluster:            &agent.Cluster,
		ContainerInstances: []*string{&agent.ContainerInstanceArn},
		TaskDefinition:     &td,
		Overrides: &ecs.TaskOverride{
			ContainerOverrides: overrides,
		},
	})
	if err != nil {
		return nil, err
	}
	if len(resp.Failures) != 0 || len(resp.Tasks) == 0 {
		return nil, errors.New("Failure starting task: " + *resp.Failures[0].Reason)
	}

	agent.t.Logf("Started task: %s\n", *resp.Tasks[0].TaskArn)
	return &TestTask{resp.Tasks[0]}, nil
}

// RoundTimeUp rounds the time to the next second/minute/hours depending on the duration
func RoundTimeUp(realTime time.Time, duration time.Duration) time.Time {
	tmpTime := realTime.Round(duration)
	if tmpTime.Before(realTime) {
		return tmpTime.Add(duration)
	}
	return tmpTime
}

func DeleteCluster(t *testing.T, clusterName string) {
	_, err := ECS.DeleteCluster(&ecs.DeleteClusterInput{
		Cluster: aws.String(clusterName),
	})
	if err != nil {
		t.Fatalf("Failed to delete the cluster: %s: %v", clusterName, err)
	}
}

// VerifyMetrics whether the response is as expected
// the expected value can be 0 or positive
func VerifyMetrics(cwclient *cloudwatch.CloudWatch, params *cloudwatch.GetMetricStatisticsInput, idleCluster bool) (*cloudwatch.Datapoint, error) {
	resp, err := cwclient.GetMetricStatistics(params)
	if err != nil {
		return nil, fmt.Errorf("Error getting metrics of cluster: %v", err)
	}

	if resp == nil || resp.Datapoints == nil {
		return nil, fmt.Errorf("Cloudwatch get metrics failed, returned null")
	}
	metricsCount := len(resp.Datapoints)
	if metricsCount == 0 {
		return nil, fmt.Errorf("No datapoints returned")
	}

	datapoint := resp.Datapoints[metricsCount-1]
	// Samplecount is always expected to be "1" for cluster metrics
	if *datapoint.SampleCount != 1.0 {
		return nil, fmt.Errorf("Incorrect SampleCount %f, expected 1", *datapoint.SampleCount)
	}

	if idleCluster {
		if *datapoint.Average != 0.0 {
			return nil, fmt.Errorf("non-zero utilization for idle cluster")
		}
	} else {
		if *datapoint.Average == 0.0 {
			return nil, fmt.Errorf("utilization is zero for non-idle cluster")
		}
	}
	return datapoint, nil
}

// ResolveTaskDockerID determines the Docker ID for a container within a given
// task that has been run by the Agent.
func (agent *TestAgent) ResolveTaskDockerID(task *TestTask, containerName string) (string, error) {
	var err error
	var dockerId string
	for i := 0; i < 5; i++ {
		dockerId, err = agent.resolveTaskDockerID(task, containerName)
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	return dockerId, err
}

func (agent *TestAgent) resolveTaskDockerID(task *TestTask, containerName string) (string, error) {
	bodyData, err := agent.callTaskIntrospectionApi(*task.TaskArn)
	if err != nil {
		return "", err
	}
	var taskResp handlers.TaskResponse
	err = json.Unmarshal(*bodyData, &taskResp)
	if err != nil {
		return "", err
	}
	if len(taskResp.Containers) == 0 {
		return "", errors.New("No containers in task response")
	}
	for _, container := range taskResp.Containers {
		if container.Name == containerName {
			return container.DockerId, nil
		}
	}
	return "", errors.New("No containers matched given name")
}

func (agent *TestAgent) WaitStoppedViaIntrospection(task *TestTask) (bool, error) {
	var err error
	var isStopped bool

	for i := 0; i < 5; i++ {
		isStopped, err = agent.waitStoppedViaIntrospection(task)
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	return isStopped, err
}

func (agent *TestAgent) waitStoppedViaIntrospection(task *TestTask) (bool, error) {
	rawResponse, err := agent.callTaskIntrospectionApi(*task.TaskArn)
	if err != nil {
		return false, err
	}

	var taskResp handlers.TaskResponse
	err = json.Unmarshal(*rawResponse, &taskResp)

	if taskResp.KnownStatus == "STOPPED" {
		return true, nil
	} else {
		return false, errors.New("Task should be STOPPED but is " + taskResp.KnownStatus)
	}
}

func (agent *TestAgent) WaitRunningViaIntrospection(task *TestTask) (bool, error) {
	var err error
	var isRunning bool

	for i := 0; i < 5; i++ {
		isRunning, err = agent.waitRunningViaIntrospection(task)
		if err == nil && isRunning {
			break
		}
		time.Sleep(10000 * time.Millisecond)
	}
	return isRunning, err
}

func (agent *TestAgent) waitRunningViaIntrospection(task *TestTask) (bool, error) {
	rawResponse, err := agent.callTaskIntrospectionApi(*task.TaskArn)
	if err != nil {
		return false, err
	}

	var taskResp handlers.TaskResponse
	err = json.Unmarshal(*rawResponse, &taskResp)

	if taskResp.KnownStatus == "RUNNING" {
		return true, nil
	} else {
		return false, errors.New("Task should be RUNNING but is " + taskResp.KnownStatus)
	}
}

func (agent *TestAgent) CallTaskIntrospectionAPI(task *TestTask) (*handlers.TaskResponse, error) {
	rawResponse, err := agent.callTaskIntrospectionApi(*task.TaskArn)
	if err != nil {
		return nil, err
	}

	var taskResp handlers.TaskResponse
	err = json.Unmarshal(*rawResponse, &taskResp)
	return &taskResp, err
}

func (agent *TestAgent) callTaskIntrospectionApi(taskArn string) (*[]byte, error) {
	fullIntrospectionApiURL := agent.IntrospectionURL + "/v1/tasks"
	if taskArn != "" {
		fullIntrospectionApiURL += "?taskarn=" + taskArn
	}

	agentTasksResp, err := http.Get(fullIntrospectionApiURL)
	if err != nil {
		return nil, err
	}

	bodyData, err := ioutil.ReadAll(agentTasksResp.Body)
	if err != nil {
		return nil, err
	}
	return &bodyData, nil
}

func (agent *TestAgent) RequireVersion(version string) {
	if agent.Version == "UNKNOWN" {
		agent.t.Skipf("Skipping test requiring version %v; agent version unknown", version)
	}

	matches, err := utils.Version(agent.Version).Matches(version)
	if err != nil {
		agent.t.Skipf("Skipping test requiring version %v; could not compare because of error: %v", version, err)
	}
	if !matches {
		agent.t.Skipf("Skipping test requiring version %v; agent version %v", version, agent.Version)
	}
}

type TestTask struct {
	*ecs.Task
}

func (task *TestTask) Redescribe() {
	res, err := ECS.DescribeTasks(&ecs.DescribeTasksInput{
		Cluster: task.ClusterArn,
		Tasks:   []*string{task.TaskArn},
	})
	if err == nil && len(res.Failures) == 0 {
		task.Task = res.Tasks[0]
	}
}

func (task *TestTask) waitStatus(timeout time.Duration, status string) error {
	timer := time.NewTimer(timeout)
	atStatus := make(chan error, 1)

	cancelled := false
	go func() {
		if *task.LastStatus == "STOPPED" && status != "STOPPED" {
			atStatus <- errors.New("Task terminal; will never reach " + status)
			return
		}
		for *task.LastStatus != status && !cancelled {
			task.Redescribe()
			if *task.LastStatus == status {
				break
			}
			if *task.LastStatus == "STOPPED" && status != "STOPPED" {
				atStatus <- errors.New("Task terminal; will never reach " + status)
				return
			}
			time.Sleep(5 * time.Second)
		}
		atStatus <- nil
	}()

	select {
	case err := <-atStatus:
		return err
	case <-timer.C:
		cancelled = true
		return errors.New("Timed out waiting for task to reach " + status + ": " + *task.TaskDefinitionArn + ", " + *task.TaskArn)
	}
}

func (task *TestTask) ContainerExitcode(name string) (int, bool) {
	for _, cont := range task.Containers {
		if cont != nil && cont.Name != nil && cont.ExitCode != nil {
			if *cont.Name == name {
				return int(*cont.ExitCode), true
			}
		}
	}
	return 0, false
}

func (task *TestTask) WaitRunning(timeout time.Duration) error {
	return task.waitStatus(timeout, "RUNNING")
}

func (task *TestTask) WaitStopped(timeout time.Duration) error {
	return task.waitStatus(timeout, "STOPPED")
}

func (task *TestTask) ExpectErrorType(containerName, errType string, timeout time.Duration) error {
	task.WaitStopped(timeout)

	for _, container := range task.Containers {
		if *container.Name != containerName {
			continue
		}
		if container.Reason == nil {
			return errors.New("Expected error reason")
		}
		errParts := strings.SplitN(*container.Reason, ":", 2)
		if len(errParts) != 2 {
			return errors.New("Error did not have a type: " + *container.Reason)
		}
		if errParts[0] != errType {
			return errors.New("Type did not match: " + *container.Reason)
		}
		return nil
	}
	return errors.New("Could not find container " + containerName + " in task " + *task.TaskArn)
}

func (task *TestTask) Stop() error {
	_, err := ECS.StopTask(&ecs.StopTaskInput{
		Cluster: task.ClusterArn,
		Task:    task.TaskArn,
	})
	return err
}

func RequireDockerVersion(t *testing.T, selector string) {
	dockerClient, err := docker.NewClientFromEnv()
	if err != nil {
		t.Fatalf("Could not get docker client to check version: %v", err)
	}
	dockerVersion, err := dockerClient.Version()
	if err != nil {
		t.Fatalf("Could not get docker version: %v", err)
	}

	version := dockerVersion.Get("Version")

	match, err := utils.Version(version).Matches(selector)
	if err != nil {
		t.Fatalf("Could not check docker version to match required: %v", err)
	}

	if !match {
		t.Skipf("Skipping test; requires %v, but version is %v", selector, version)
	}
}

// GetInstanceProfileName gets the instance profile name
func GetInstanceMetadata(path string) (string, error) {
	ec2MetadataClient := ec2metadata.New(session.New())
	return ec2MetadataClient.GetMetadata(path)
}

// GetInstanceIAMRole gets the iam roles attached to the instance profile
func GetInstanceIAMRole() (*iam.Role, error) {
	// This returns the name of the role
	instanceRoleName, err := GetInstanceMetadata("iam/security-credentials")
	if err != nil {
		return nil, fmt.Errorf("Error getting instance role name, err: %v", err)
	}
	if utils.ZeroOrNil(instanceRoleName) {
		return nil, fmt.Errorf("Instance Role name nil")
	}

	iamClient := iam.New(session.New())
	instanceRole, err := iamClient.GetRole(&iam.GetRoleInput{
		RoleName: aws.String(instanceRoleName),
	})
	if err != nil {
		return nil, err
	}

	return instanceRole.Role, nil
}

// SearchStrInDir searches the files in direcotry for specific content
func SearchStrInDir(dir, filePrefix, content string) error {
	logfiles, err := ioutil.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("Error reading the directory, err %v", err)
	}

	var desiredFile string
	found := false

	for _, file := range logfiles {
		if strings.HasPrefix(file.Name(), filePrefix) {
			desiredFile = file.Name()
			if utils.ZeroOrNil(desiredFile) {
				return fmt.Errorf("File with prefix: %v does not exist", filePrefix)
			}

			data, err := ioutil.ReadFile(filepath.Join(dir, desiredFile))
			if err != nil {
				return fmt.Errorf("Failed to read file, err: %v", err)
			}

			if strings.Contains(string(data), content) {
				found = true
				break
			}
		}
	}

	if !found {
		return fmt.Errorf("Could not find the content: %v in the file: %v", content, desiredFile)
	}

	return nil
}

// GetContainerNetworkMode gets the container network mode, given container id
func (agent *TestAgent) GetContainerNetworkMode(containerId string) ([]string, error) {
	containerMetaData, err := agent.DockerClient.InspectContainer(containerId)
	if err != nil {
		return nil, fmt.Errorf("Could not inspect container for task: %v", err)
	}

	if containerMetaData.NetworkSettings == nil {
		return nil, fmt.Errorf("Couldn't find the container network setting info")
	}

	var networks []string
	for key := range containerMetaData.NetworkSettings.Networks {
		networks = append(networks, key)
	}

	return networks, nil
}

// SweepTask removes all the containers belong to a task
func (agent *TestAgent) SweepTask(task *TestTask) error {
	bodyData, err := agent.callTaskIntrospectionApi(*task.TaskArn)
	if err != nil {
		return err
	}

	var taskResponse handlers.TaskResponse
	err = json.Unmarshal(*bodyData, &taskResponse)
	if err != nil {
		return err
	}

	for _, container := range taskResponse.Containers {
		ctx, _ := context.WithTimeout(context.Background(), 1*time.Minute)
		agent.DockerClient.RemoveContainer(docker.RemoveContainerOptions{
			ID:            container.DockerId,
			RemoveVolumes: true,
			Force:         true,
			Context:       ctx,
		})
	}

	return nil
}

// AttributesToMap transforms a list of key, value attributes to return a map
func AttributesToMap(attributes []*ecs.Attribute) map[string]string {
	attributeMap := make(map[string]string)
	for _, attribute := range attributes {
		attributeMap[aws.StringValue(attribute.Name)] = aws.StringValue(attribute.Value)
	}
	return attributeMap
}
