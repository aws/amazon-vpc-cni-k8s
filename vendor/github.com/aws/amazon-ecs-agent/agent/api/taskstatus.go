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
	"errors"
	"strings"
)

const (
	// TaskStatusNone is the zero state of a task; this task has been received but no further progress has completed
	TaskStatusNone TaskStatus = iota
	// TaskPulled represents a task which has had all its container images pulled, but not all have yet progressed passed pull
	TaskPulled
	// TaskCreated represents a task which has had all its containers created
	TaskCreated
	// TaskRunning represents a task which has had all its containers started
	TaskRunning
	// TaskStopped represents a task in which all containers are stopped
	TaskStopped
	// TaskZombie is an "impossible" state that is used as the maximum
	TaskZombie
)

// TaskStatus is an enumeration of valid states in the task lifecycle
type TaskStatus int32

var taskStatusMap = map[string]TaskStatus{
	"NONE":    TaskStatusNone,
	"CREATED": TaskCreated,
	"RUNNING": TaskRunning,
	"STOPPED": TaskStopped,
}

// String returns a human readable string representation of this object
func (ts TaskStatus) String() string {
	for k, v := range taskStatusMap {
		if v == ts {
			return k
		}
	}
	return "NONE"
}

// BackendStatus maps the internal task status in the agent to that in the backend
func (ts *TaskStatus) BackendStatus() string {
	switch *ts {
	case TaskRunning:
		fallthrough
	case TaskStopped:
		return ts.String()
	}
	return "PENDING"
}

// BackendRecognized returns true if the task status is recognized as a valid state
// by ECS. Note that not all task statuses are recognized by ECS or map to ECS
// states
func (ts *TaskStatus) BackendRecognized() bool {
	return *ts == TaskRunning || *ts == TaskStopped
}

// ContainerStatus maps the task status to the corresponding container status
func (ts *TaskStatus) ContainerStatus(steadyState ContainerStatus) ContainerStatus {
	switch *ts {
	case TaskStatusNone:
		return ContainerStatusNone
	case TaskCreated:
		return ContainerCreated
	case TaskRunning:
		return steadyState
	case TaskStopped:
		return ContainerStopped
	}
	return ContainerStatusNone
}

// Terminal returns true if the Task status is STOPPED
func (ts TaskStatus) Terminal() bool {
	return ts == TaskStopped
}

// UnmarshalJSON overrides the logic for parsing the JSON-encoded TaskStatus data
func (ts *TaskStatus) UnmarshalJSON(b []byte) error {
	if strings.ToLower(string(b)) == "null" {
		*ts = TaskStatusNone
		return nil
	}
	if b[0] != '"' || b[len(b)-1] != '"' {
		*ts = TaskStatusNone
		return errors.New("TaskStatus must be a string or null")
	}
	strStatus := string(b[1 : len(b)-1])
	// 'UNKNOWN' and 'DEAD' for Compatibility with v1.0.0 state files
	if strStatus == "UNKNOWN" {
		*ts = TaskStatusNone
		return nil
	}
	if strStatus == "DEAD" {
		*ts = TaskStopped
		return nil
	}

	stat, ok := taskStatusMap[strStatus]
	if !ok {
		*ts = TaskStatusNone
		return errors.New("Unrecognized TaskStatus")
	}
	*ts = stat
	return nil
}

// MarshalJSON overrides the logic for JSON-encoding the TaskStatus type
func (ts *TaskStatus) MarshalJSON() ([]byte, error) {
	if ts == nil {
		return nil, nil
	}
	return []byte(`"` + ts.String() + `"`), nil
}
