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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTaskStatus(t *testing.T) {
	// Effectively set containerStatus := ContainerStatusNone, we expect the task state
	// to be TaskStatusNone
	var containerStatus ContainerStatus
	assert.Equal(t, containerStatus.TaskStatus(ContainerRunning), TaskStatusNone)
	assert.Equal(t, containerStatus.TaskStatus(ContainerResourcesProvisioned), TaskStatusNone)

	// When container state is PULLED, Task state is still NONE
	containerStatus = ContainerPulled
	assert.Equal(t, containerStatus.TaskStatus(ContainerRunning), TaskStatusNone)
	assert.Equal(t, containerStatus.TaskStatus(ContainerResourcesProvisioned), TaskStatusNone)

	// When container state is CREATED, Task state is CREATED as well
	containerStatus = ContainerCreated
	assert.Equal(t, containerStatus.TaskStatus(ContainerRunning), TaskCreated)
	assert.Equal(t, containerStatus.TaskStatus(ContainerResourcesProvisioned), TaskCreated)

	containerStatus = ContainerRunning
	// When container state is RUNNING and steadyState is RUNNING, Task state is RUNNING as well
	assert.Equal(t, containerStatus.TaskStatus(ContainerRunning), TaskRunning)
	// When container state is RUNNING and steadyState is RESOURCES_PROVISIONED, Task state
	// still CREATED
	assert.Equal(t, containerStatus.TaskStatus(ContainerResourcesProvisioned), TaskCreated)

	containerStatus = ContainerResourcesProvisioned
	// When container state is RESOURCES_PROVISIONED and steadyState is RESOURCES_PROVISIONED,
	// Task state is RUNNING
	assert.Equal(t, containerStatus.TaskStatus(ContainerResourcesProvisioned), TaskRunning)

	// When container state is STOPPED, Task state is STOPPED as well
	containerStatus = ContainerStopped
	assert.Equal(t, containerStatus.TaskStatus(ContainerRunning), TaskStopped)
	assert.Equal(t, containerStatus.TaskStatus(ContainerResourcesProvisioned), TaskStopped)
}

func TestShouldReportToBackend(t *testing.T) {
	// ContainerStatusNone is not reported to backend
	var containerStatus ContainerStatus
	assert.False(t, containerStatus.ShouldReportToBackend(ContainerRunning))
	assert.False(t, containerStatus.ShouldReportToBackend(ContainerResourcesProvisioned))

	// ContainerPulled is not reported to backend
	containerStatus = ContainerPulled
	assert.False(t, containerStatus.ShouldReportToBackend(ContainerRunning))
	assert.False(t, containerStatus.ShouldReportToBackend(ContainerResourcesProvisioned))

	// ContainerCreated is not reported to backend
	containerStatus = ContainerCreated
	assert.False(t, containerStatus.ShouldReportToBackend(ContainerRunning))
	assert.False(t, containerStatus.ShouldReportToBackend(ContainerResourcesProvisioned))

	containerStatus = ContainerRunning
	// ContainerRunning is reported to backend if the steady state is RUNNING as well
	assert.True(t, containerStatus.ShouldReportToBackend(ContainerRunning))
	// ContainerRunning is not reported to backend if the steady state is RESOURCES_PROVISIONED
	assert.False(t, containerStatus.ShouldReportToBackend(ContainerResourcesProvisioned))

	containerStatus = ContainerResourcesProvisioned
	// ContainerResourcesProvisioned is reported to backend if the steady state
	// is RESOURCES_PROVISIONED
	assert.True(t, containerStatus.ShouldReportToBackend(ContainerResourcesProvisioned))

	// ContainerStopped is not reported to backend
	containerStatus = ContainerStopped
	assert.True(t, containerStatus.ShouldReportToBackend(ContainerRunning))
	assert.True(t, containerStatus.ShouldReportToBackend(ContainerResourcesProvisioned))

}

func TestBackendStatus(t *testing.T) {
	// BackendStatus is ContainerStatusNone when container status is ContainerStatusNone
	var containerStatus ContainerStatus
	assert.Equal(t, containerStatus.BackendStatus(ContainerRunning), ContainerStatusNone)
	assert.Equal(t, containerStatus.BackendStatus(ContainerResourcesProvisioned), ContainerStatusNone)

	// BackendStatus is still ContainerStatusNone when container status is ContainerPulled
	containerStatus = ContainerPulled
	assert.Equal(t, containerStatus.BackendStatus(ContainerRunning), ContainerStatusNone)
	assert.Equal(t, containerStatus.BackendStatus(ContainerResourcesProvisioned), ContainerStatusNone)

	// BackendStatus is still ContainerStatusNone when container status is ContainerCreated
	containerStatus = ContainerCreated
	assert.Equal(t, containerStatus.BackendStatus(ContainerRunning), ContainerStatusNone)
	assert.Equal(t, containerStatus.BackendStatus(ContainerResourcesProvisioned), ContainerStatusNone)

	containerStatus = ContainerRunning
	// BackendStatus is ContainerRunning when container status is ContainerRunning
	// and steady state is ContainerRunning
	assert.Equal(t, containerStatus.BackendStatus(ContainerRunning), ContainerRunning)
	// BackendStatus is still ContainerStatusNone when container status is ContainerRunning
	// and steady state is ContainerResourcesProvisioned
	assert.Equal(t, containerStatus.BackendStatus(ContainerResourcesProvisioned), ContainerStatusNone)

	containerStatus = ContainerResourcesProvisioned
	// BackendStatus is still ContainerRunning when container status is ContainerResourcesProvisioned
	// and steady state is ContainerResourcesProvisioned
	assert.Equal(t, containerStatus.BackendStatus(ContainerResourcesProvisioned), ContainerRunning)

	// BackendStatus is ContainerStopped when container status is ContainerStopped
	containerStatus = ContainerStopped
	assert.Equal(t, containerStatus.BackendStatus(ContainerRunning), ContainerStopped)
	assert.Equal(t, containerStatus.BackendStatus(ContainerResourcesProvisioned), ContainerStopped)
}

type testContainerStatus struct {
	SomeStatus ContainerStatus `json:"status"`
}

func TestUnmarshalContainerStatus(t *testing.T) {
	status := ContainerStatusNone

	err := json.Unmarshal([]byte(`"RUNNING"`), &status)
	if err != nil {
		t.Error(err)
	}
	if status != ContainerRunning {
		t.Error("RUNNING should unmarshal to RUNNING, not " + status.String())
	}

	var test testContainerStatus
	err = json.Unmarshal([]byte(`{"status":"STOPPED"}`), &test)
	if err != nil {
		t.Error(err)
	}
	if test.SomeStatus != ContainerStopped {
		t.Error("STOPPED should unmarshal to STOPPED, not " + test.SomeStatus.String())
	}
}
