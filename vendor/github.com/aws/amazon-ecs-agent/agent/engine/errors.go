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

package engine

import (
	"time"

	"github.com/aws/amazon-ecs-agent/agent/api"
	"github.com/aws/amazon-ecs-agent/agent/engine/dockerclient"
	docker "github.com/fsouza/go-dockerclient"
)

const dockerTimeoutErrorName = "DockerTimeoutError"

// engineError wraps the error interface with an identifier method that
// is used to classify the error type
type engineError interface {
	error
	ErrorName() string
}

type cannotStopContainerError interface {
	engineError
	IsRetriableError() bool
}

// impossibleTransitionError is an error that occurs when an event causes a
// container to try and transition to a state that it cannot be moved to
type impossibleTransitionError struct {
	state api.ContainerStatus
}

func (err *impossibleTransitionError) Error() string {
	return "Cannot transition to " + err.state.String()
}
func (err *impossibleTransitionError) ErrorName() string { return "ImpossibleStateTransitionError" }

// DockerTimeoutError is an error type for describing timeouts
type DockerTimeoutError struct {
	duration   time.Duration
	transition string
}

func (err *DockerTimeoutError) Error() string {
	return "Could not transition to " + err.transition + "; timed out after waiting " + err.duration.String()
}

// ErrorName returns the name of the error
func (err *DockerTimeoutError) ErrorName() string { return dockerTimeoutErrorName }

// ContainerVanishedError is a type for describing a container that does not exist
type ContainerVanishedError struct{}

func (err ContainerVanishedError) Error() string { return "No container matching saved ID found" }

// ErrorName returns the name of the error
func (err ContainerVanishedError) ErrorName() string { return "ContainerVanishedError" }

// OutOfMemoryError is a type for errors caused by running out of memory
type OutOfMemoryError struct{}

func (err OutOfMemoryError) Error() string { return "Container killed due to memory usage" }

// ErrorName returns the name of the error
func (err OutOfMemoryError) ErrorName() string { return "OutOfMemoryError" }

// DockerStateError is a wrapper around the error docker puts in the '.State.Error' field of its inspect output.
type DockerStateError struct {
	dockerError string
	name        string
}

// NewDockerStateError creates a DockerStateError
func NewDockerStateError(err string) DockerStateError {
	// Add stringmatching logic as needed to provide better output than docker
	return DockerStateError{
		dockerError: err,
		name:        "DockerStateError",
	}
}

func (err DockerStateError) Error() string {
	return err.dockerError
}

// ErrorName returns the name of the error
func (err DockerStateError) ErrorName() string {
	return err.name
}

// CannotGetDockerClientError is a type for failing to get a specific Docker client
type CannotGetDockerClientError struct {
	version dockerclient.DockerVersion
	err     error
}

func (c CannotGetDockerClientError) Error() string {
	if c.version != "" {
		return "(v" + string(c.version) + ") - " + c.err.Error()
	}
	return c.err.Error()
}

// ErrorName returns the name of the error
func (CannotGetDockerClientError) ErrorName() string {
	return "CannotGetDockerclientError"
}

// TaskDependencyError is the error for task that dependencies can't
// be resolved
type TaskDependencyError struct {
	taskArn string
}

func (err TaskDependencyError) Error() string {
	return "Task dependencies cannot be resolved, taskArn: " + err.taskArn
}

// ErrorName is the name of the error
func (err TaskDependencyError) ErrorName() string {
	return "TaskDependencyError"
}

// TaskStoppedBeforePullBeginError is a type for task errors involving pull
type TaskStoppedBeforePullBeginError struct {
	taskArn string
}

func (err TaskStoppedBeforePullBeginError) Error() string {
	return "Task stopped before image pull could begin for task: " + err.taskArn
}

// ErrorName returns the name of the error
func (TaskStoppedBeforePullBeginError) ErrorName() string {
	return "TaskStoppedBeforePullBeginError"
}

// CannotStopContainerError indicates any error when trying to stop a container
type CannotStopContainerError struct {
	fromError error
}

func (err CannotStopContainerError) Error() string {
	return err.fromError.Error()
}

func (err CannotStopContainerError) ErrorName() string {
	return "CannotStopContainerError"
}

// IsRetriableError returns a boolean indicating whether the call that
// generated the error can be retried.
// When stopping a container, most errors that we can get should be
// considered retriable. However, in the case where the container is
// already stopped or doesn't exist at all, there's no sense in
// retrying.
func (err CannotStopContainerError) IsRetriableError() bool {
	if _, ok := err.fromError.(*docker.NoSuchContainer); ok {
		return false
	}
	if _, ok := err.fromError.(*docker.ContainerNotRunning); ok {
		return false
	}

	return true
}

// CannotPullContainerError indicates any error when trying to pull
// a container image
type CannotPullContainerError struct {
	fromError error
}

func (err CannotPullContainerError) Error() string {
	return err.fromError.Error()
}

func (err CannotPullContainerError) ErrorName() string {
	return "CannotPullContainerError"
}

// CannotPullECRContainerError indicates any error when trying to pull
// a container image from ECR
type CannotPullECRContainerError struct {
	fromError error
}

func (err CannotPullECRContainerError) Error() string {
	return err.fromError.Error()
}

func (err CannotPullECRContainerError) ErrorName() string {
	return "CannotPullECRContainerError"
}

// Retry fulfills the utils.Retrier interface and allows retries to be skipped by utils.Retry* functions
func (err CannotPullECRContainerError) Retry() bool {
	return false
}

type CreateEmptyVolumeError struct {
	fromError error
}

func (err CreateEmptyVolumeError) Error() string {
	return err.fromError.Error()
}

func (err CreateEmptyVolumeError) ErrorName() string {
	return "CreateEmptyVolumeError"
}

// Retry fulfills the utils.Retrier interface and allows retries to be skipped by utils.Retry* functions
func (err CreateEmptyVolumeError) Retry() bool {
	return false
}

// CannotCreateContainerError indicates any error when trying to create a container
type CannotCreateContainerError struct {
	fromError error
}

func (err CannotCreateContainerError) Error() string {
	return err.fromError.Error()
}

func (err CannotCreateContainerError) ErrorName() string {
	return "CannotCreateContainerError"
}

// CannotStartContainerError indicates any error when trying to start a container
type CannotStartContainerError struct {
	fromError error
}

func (err CannotStartContainerError) Error() string {
	return err.fromError.Error()
}

func (err CannotStartContainerError) ErrorName() string {
	return "CannotStartContainerError"
}

// CannotInspectContainerError indicates any error when trying to inspect a container
type CannotInspectContainerError struct {
	fromError error
}

func (err CannotInspectContainerError) Error() string {
	return err.fromError.Error()
}

func (err CannotInspectContainerError) ErrorName() string {
	return "CannotInspectContainerError"
}

// CannotRemoveContainerError indicates any error when trying to remove a container
type CannotRemoveContainerError struct {
	fromError error
}

func (err CannotRemoveContainerError) Error() string {
	return err.fromError.Error()
}

func (err CannotRemoveContainerError) ErrorName() string {
	return "CannotRemoveContainerError"
}

// CannotDescribeContainerError indicates any error when trying to describe a container
type CannotDescribeContainerError struct {
	fromError error
}

func (err CannotDescribeContainerError) Error() string {
	return err.fromError.Error()
}

func (err CannotDescribeContainerError) ErrorName() string {
	return "CannotDescribeContainerError"
}

// CannotListContainersError indicates any error when trying to list containers
type CannotListContainersError struct {
	fromError error
}

func (err CannotListContainersError) Error() string {
	return err.fromError.Error()
}

func (err CannotListContainersError) ErrorName() string {
	return "CannotListContainersError"
}

// ContainerNetworkingError indicates any error when dealing with the network
// namespace of container
type ContainerNetworkingError struct {
	fromError error
}

func (err ContainerNetworkingError) Error() string {
	return err.fromError.Error()
}

func (err ContainerNetworkingError) ErrorName() string {
	return "ContainerNetworkingError"
}

// CannotGetDockerClientVersionError indicates error when trying to get docker
// client api version
type CannotGetDockerClientVersionError struct {
	fromError error
}

func (err CannotGetDockerClientVersionError) ErrorName() string {
	return "CannotGetDockerClientVersionError"
}
func (err CannotGetDockerClientVersionError) Error() string {
	return err.fromError.Error()
}
