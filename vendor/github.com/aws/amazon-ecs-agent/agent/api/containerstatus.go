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
	// ContainerStatusNone is the zero state of a container; this container has not completed pull
	ContainerStatusNone ContainerStatus = iota
	// ContainerPulled represents a container which has had the image pulled
	ContainerPulled
	// ContainerCreated represents a container that has been created
	ContainerCreated
	// ContainerRunning represents a container that has started
	ContainerRunning
	// ContainerResourcesProvisioned represents a container that has completed provisioning all of its
	// resources. Non-internal containers (containers present in the task definition) transition to
	// this state without doing any additional work. However, containers that are added to a task
	// by the ECS Agent would possibly need to perform additional actions before they can be
	// considered "ready" and contribute to the progress of a task. For example, the "pause" container
	// would be provisioned by invoking CNI plugins
	ContainerResourcesProvisioned
	// ContainerStopped represents a container that has stopped
	ContainerStopped
	// ContainerZombie is an "impossible" state that is used as the maximum
	ContainerZombie
)

// ContainerStatus is an enumeration of valid states in the container lifecycle
type ContainerStatus int32

var containerStatusMap = map[string]ContainerStatus{
	"NONE":                  ContainerStatusNone,
	"PULLED":                ContainerPulled,
	"CREATED":               ContainerCreated,
	"RUNNING":               ContainerRunning,
	"RESOURCES_PROVISIONED": ContainerResourcesProvisioned,
	"STOPPED":               ContainerStopped,
}

// String returns a human readable string representation of this object
func (cs ContainerStatus) String() string {
	for k, v := range containerStatusMap {
		if v == cs {
			return k
		}
	}
	return "NONE"
}

// TaskStatus maps the container status to the corresponding task status. The
// transition map is illustrated below.
//
// Container: None -> Pulled -> Created -> Running -> Provisioned -> Stopped -> Zombie
//
// Task     : None ->     Created       ->         Running        -> Stopped
func (cs *ContainerStatus) TaskStatus(steadyStateStatus ContainerStatus) TaskStatus {
	switch *cs {
	case ContainerStatusNone:
		return TaskStatusNone
	case steadyStateStatus:
		return TaskRunning
	case ContainerCreated:
		return TaskCreated
	case ContainerStopped:
		return TaskStopped
	}

	if *cs == ContainerRunning && steadyStateStatus == ContainerResourcesProvisioned {
		return TaskCreated
	}

	return TaskStatusNone
}

// ShouldReportToBackend returns true if the container status is recognized as a
// valid state by ECS. Note that not all container statuses are recognized by ECS
// or map to ECS states
func (cs *ContainerStatus) ShouldReportToBackend(steadyStateStatus ContainerStatus) bool {
	return *cs == steadyStateStatus || *cs == ContainerStopped
}

// BackendStatus maps the internal container status in the agent to that in the
// backend
func (cs *ContainerStatus) BackendStatus(steadyStateStatus ContainerStatus) ContainerStatus {
	if *cs == steadyStateStatus {
		return ContainerRunning
	}

	if *cs == ContainerStopped {
		return ContainerStopped
	}

	return ContainerStatusNone
}

// Terminal returns true if the container status is STOPPED
func (cs ContainerStatus) Terminal() bool {
	return cs == ContainerStopped
}

// UnmarshalJSON overrides the logic for parsing the JSON-encoded TaskStatus data
func (cs *ContainerStatus) UnmarshalJSON(b []byte) error {
	if strings.ToLower(string(b)) == "null" {
		*cs = ContainerStatusNone
		return nil
	}
	if b[0] != '"' || b[len(b)-1] != '"' {
		*cs = ContainerStatusNone
		return errors.New("ContainerStatus must be a string or null; Got " + string(b))
	}
	strStatus := string(b[1 : len(b)-1])
	// 'UNKNOWN' and 'DEAD' for Compatibility with v1.0.0 state files
	if strStatus == "UNKNOWN" {
		*cs = ContainerStatusNone
		return nil
	}
	if strStatus == "DEAD" {
		*cs = ContainerStopped
		return nil
	}

	stat, ok := containerStatusMap[strStatus]
	if !ok {
		*cs = ContainerStatusNone
		return errors.New("Unrecognized ContainerStatus")
	}
	*cs = stat
	return nil
}

// MarshalJSON overrides the logic for JSON-encoding the ContainerStatus type
func (cs *ContainerStatus) MarshalJSON() ([]byte, error) {
	if cs == nil {
		return nil, nil
	}
	return []byte(`"` + cs.String() + `"`), nil
}

// IsRunning returns true if the container status is either RUNNING or RESOURCES_PROVISIONED
func (cs ContainerStatus) IsRunning() bool {
	return cs == ContainerRunning || cs == ContainerResourcesProvisioned
}
