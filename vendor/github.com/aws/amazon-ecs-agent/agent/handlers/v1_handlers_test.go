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

package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/aws/amazon-ecs-agent/agent/api"
	"github.com/aws/amazon-ecs-agent/agent/config"
	"github.com/aws/amazon-ecs-agent/agent/engine/dockerstate"
	"github.com/aws/amazon-ecs-agent/agent/handlers/mocks"
	"github.com/aws/amazon-ecs-agent/agent/handlers/mocks/http"
	"github.com/aws/amazon-ecs-agent/agent/utils"
	"github.com/aws/amazon-ecs-agent/agent/utils/mocks"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testContainerInstanceArn = "test_container_instance_arn"
const testClusterArn = "test_cluster_arn"

func TestMetadataHandler(t *testing.T) {
	metadataHandler := metadataV1RequestHandlerMaker(utils.Strptr(testContainerInstanceArn), &config.Config{Cluster: testClusterArn})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "http://localhost:"+strconv.Itoa(config.AgentIntrospectionPort), nil)
	metadataHandler(w, req)

	var resp MetadataResponse
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp.Cluster != testClusterArn {
		t.Error("Metadata returned the wrong cluster arn")
	}
	if *resp.ContainerInstanceArn != testContainerInstanceArn {
		t.Error("Metadata returned the wrong cluster arn")
	}
}

func TestListMultipleTasks(t *testing.T) {
	recorder := performMockRequest(t, "/v1/tasks")

	var tasksResponse TasksResponse
	err := json.Unmarshal(recorder.Body.Bytes(), &tasksResponse)
	if err != nil {
		t.Fatal(err)
	}

	taskDiffHelper(t, testTasks, tasksResponse)
}

func TestGetTaskByDockerID(t *testing.T) {
	// stateSetupHelper uses the convention of dockerid-$arn-$containerName; the
	// second task has a container named foo
	recorder := performMockRequest(t, "/v1/tasks?dockerid=dockerid-task2-foo")

	var taskResponse TaskResponse
	err := json.Unmarshal(recorder.Body.Bytes(), &taskResponse)
	if err != nil {
		t.Fatal(err)
	}

	taskDiffHelper(t, []*api.Task{testTasks[1]}, TasksResponse{Tasks: []*TaskResponse{&taskResponse}})
}

func TestGetTaskByShortDockerIDMultiple(t *testing.T) {
	recorder := performMockRequest(t, "/v1/tasks?dockerid=dockerid-tas")

	assert.Equal(t, http.StatusBadRequest, recorder.Code, "Expected http 400 for dockerid with multiple matches")
}

func TestGetTaskShortByDockerID404(t *testing.T) {
	recorder := performMockRequest(t, "/v1/tasks?dockerid=notfound")

	assert.Equal(t, http.StatusNotFound, recorder.Code, "API did not return 404 for bad dockerid")
}

func TestGetTaskByShortDockerID(t *testing.T) {
	// stateSetupHelper uses the convention of dockerid-$arn-$containerName; the
	// first task has a container name prefix of dockerid-tas
	recorder := performMockRequest(t, "/v1/tasks?dockerid=dockerid-by")

	var taskResponse TaskResponse
	err := json.Unmarshal(recorder.Body.Bytes(), &taskResponse)
	require.NoError(t, err, "unmarshal failed for get task by short docker id")

	taskDiffHelper(t, []*api.Task{testTasks[2]}, TasksResponse{Tasks: []*TaskResponse{&taskResponse}})
}

func TestGetTaskByDockerID404(t *testing.T) {
	recorder := performMockRequest(t, "/v1/tasks?dockerid=does-not-exist")

	if recorder.Code != 404 {
		t.Error("API did not return 404 for bad dockerid")
	}
}

func TestGetTaskByTaskArn(t *testing.T) {
	recorder := performMockRequest(t, "/v1/tasks?taskarn=task1")

	var taskResponse TaskResponse
	err := json.Unmarshal(recorder.Body.Bytes(), &taskResponse)
	if err != nil {
		t.Fatal(err)
	}

	taskDiffHelper(t, []*api.Task{testTasks[0]}, TasksResponse{Tasks: []*TaskResponse{&taskResponse}})
}

func TestGetTaskByTaskArnNotFound(t *testing.T) {
	recorder := performMockRequest(t, "/v1/tasks?taskarn=doesnotexist")

	if recorder.Code != http.StatusNotFound {
		t.Errorf("Expected %d for bad taskarn, but was %d", http.StatusNotFound, recorder.Code)
	}
}

func TestGetTaskByTaskArnAndDockerIDBadRequest(t *testing.T) {
	recorder := performMockRequest(t, "/v1/tasks?taskarn=task2&dockerid=foo")

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("Expected %d for both arn and dockerid, but was %d", http.StatusBadRequest, recorder.Code)
	}
}

func TestBackendMismatchMapping(t *testing.T) {
	// Test that a KnownStatus past a DesiredStatus suppresses the DesiredStatus output
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockStateResolver := mock_handlers.NewMockDockerStateResolver(ctrl)

	containers := []*api.Container{
		{
			Name: "c1",
		},
	}
	testTask := &api.Task{
		Arn:                 "task1",
		DesiredStatusUnsafe: api.TaskRunning,
		KnownStatusUnsafe:   api.TaskStopped,
		Family:              "test",
		Version:             "1",
		Containers:          containers,
	}

	state := dockerstate.NewTaskEngineState()
	stateSetupHelper(state, []*api.Task{testTask})

	mockStateResolver.EXPECT().State().Return(state)
	requestHandler := tasksV1RequestHandlerMaker(mockStateResolver)

	recorder := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/v1/tasks", nil)
	requestHandler(recorder, req)

	var tasksResponse TasksResponse
	err := json.Unmarshal(recorder.Body.Bytes(), &tasksResponse)
	if err != nil {
		t.Fatal(err)
	}
	if tasksResponse.Tasks[0].DesiredStatus != "" {
		t.Error("Expected '', was ", tasksResponse.Tasks[0].DesiredStatus)
	}
	if tasksResponse.Tasks[0].KnownStatus != "STOPPED" {
		t.Error("Expected STOPPED, was ", tasksResponse.Tasks[0].KnownStatus)
	}
}

func TestLicenseHandler(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockResponseWriter := mock_http.NewMockResponseWriter(mockCtrl)
	mockLicenseProvider := mock_utils.NewMockLicenseProvider(mockCtrl)

	licenseProvider = mockLicenseProvider

	text := "text here"
	mockLicenseProvider.EXPECT().GetText().Return(text, nil)
	mockResponseWriter.EXPECT().Write([]byte(text))

	licenseHandler(mockResponseWriter, nil)
}

func TestLicenseHandlerError(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockResponseWriter := mock_http.NewMockResponseWriter(mockCtrl)
	mockLicenseProvider := mock_utils.NewMockLicenseProvider(mockCtrl)

	licenseProvider = mockLicenseProvider

	mockLicenseProvider.EXPECT().GetText().Return("", errors.New("test error"))
	mockResponseWriter.EXPECT().WriteHeader(http.StatusInternalServerError)

	licenseHandler(mockResponseWriter, nil)
}

func taskDiffHelper(t *testing.T, expected []*api.Task, actual TasksResponse) {
	if len(expected) != len(actual.Tasks) {
		t.Errorf("Expected %v tasks, had %v tasks", len(expected), len(actual.Tasks))
	}

	for _, task := range expected {
		// Find related actual task
		var respTask *TaskResponse
		for _, actualTask := range actual.Tasks {
			if actualTask.Arn == task.Arn {
				respTask = actualTask
			}
		}

		if respTask == nil {
			t.Errorf("Could not find matching task for arn: %v", task.Arn)
			continue
		}

		if respTask.DesiredStatus != task.GetDesiredStatus().String() {
			t.Errorf("DesiredStatus mismatch: %v != %v", respTask.DesiredStatus, task.GetDesiredStatus())
		}
		taskKnownStatus := task.GetKnownStatus().String()
		if respTask.KnownStatus != taskKnownStatus {
			t.Errorf("KnownStatus mismatch: %v != %v", respTask.KnownStatus, taskKnownStatus)
		}

		if respTask.Family != task.Family || respTask.Version != task.Version {
			t.Errorf("Family mismatch: %v:%v != %v:%v", respTask.Family, respTask.Version, task.Family, task.Version)
		}

		if len(respTask.Containers) != len(task.Containers) {
			t.Errorf("Expected %v containers in %v, was %v", len(task.Containers), task.Arn, len(respTask.Containers))
			continue
		}
		for _, respCont := range respTask.Containers {
			_, ok := task.ContainerByName(respCont.Name)
			if !ok {
				t.Errorf("Could not find container %v", respCont.Name)
			}
			if respCont.DockerId == "" {
				t.Error("blank dockerid")
			}
		}
	}
}

var testTasks = []*api.Task{
	{
		Arn:                 "task1",
		DesiredStatusUnsafe: api.TaskRunning,
		KnownStatusUnsafe:   api.TaskRunning,
		Family:              "test",
		Version:             "1",
		Containers: []*api.Container{
			{
				Name: "one",
			},
			{
				Name: "two",
			},
		},
	},
	{
		Arn:                 "task2",
		DesiredStatusUnsafe: api.TaskRunning,
		KnownStatusUnsafe:   api.TaskRunning,
		Family:              "test",
		Version:             "2",
		Containers: []*api.Container{
			{
				Name: "foo",
			},
		},
	},
	{
		Arn:                 "byShortId",
		DesiredStatusUnsafe: api.TaskRunning,
		KnownStatusUnsafe:   api.TaskRunning,
		Family:              "test",
		Version:             "2",
		Containers: []*api.Container{
			{
				Name: "shortId",
			},
		},
	},
}

func stateSetupHelper(state dockerstate.TaskEngineState, tasks []*api.Task) {
	for _, task := range tasks {
		state.AddTask(task)
		for _, container := range task.Containers {
			state.AddContainer(&api.DockerContainer{
				Container:  container,
				DockerID:   "dockerid-" + task.Arn + "-" + container.Name,
				DockerName: "dockername-" + task.Arn + "-" + container.Name,
			}, task)
		}
	}
}

func performMockRequest(t *testing.T, path string) *httptest.ResponseRecorder {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockStateResolver := mock_handlers.NewMockDockerStateResolver(ctrl)

	state := dockerstate.NewTaskEngineState()
	stateSetupHelper(state, testTasks)

	mockStateResolver.EXPECT().State().Return(state)
	requestHandler := setupServer(utils.Strptr(testContainerInstanceArn), mockStateResolver, &config.Config{Cluster: testClusterArn})

	recorder := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", path, nil)
	requestHandler.Handler.ServeHTTP(recorder, req)

	return recorder
}
