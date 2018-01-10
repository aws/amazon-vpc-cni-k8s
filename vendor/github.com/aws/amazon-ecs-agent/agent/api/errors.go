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
	"strings"

	"github.com/aws/aws-sdk-go/aws/awserr"
)

// InstanceTypeChangedErrorMessage is the error message to print for the
// instance type changed error when registering a container instance
const InstanceTypeChangedErrorMessage = "Container instance type changes are not supported."

// IsInstanceTypeChangedError returns true if the error when
// registering the container instance is because of instance type being
// changed
func IsInstanceTypeChangedError(err error) bool {
	if awserr, ok := err.(awserr.Error); ok {
		return strings.Contains(awserr.Message(), InstanceTypeChangedErrorMessage)
	}
	return false
}

type badVolumeError struct {
	msg string
}

func (err *badVolumeError) Error() string     { return err.msg }
func (err *badVolumeError) ErrorName() string { return "InvalidVolumeError" }
func (err *badVolumeError) Retry() bool       { return false }

type NamedError interface {
	error
	ErrorName() string
}

// DefaultNamedError is a wrapper type for 'error' which adds an optional name and provides a symmetric
// marshal/unmarshal
type DefaultNamedError struct {
	Err  string `json:"error"`
	Name string `json:"name"`
}

// Error implements error
func (err *DefaultNamedError) Error() string {
	if err.Name == "" {
		return "UnknownError: " + err.Err
	}
	return err.Name + ": " + err.Err
}

// ErrorName implements NamedError
func (err *DefaultNamedError) ErrorName() string {
	return err.Name
}

// NewNamedError creates a NamedError.
func NewNamedError(err error) *DefaultNamedError {
	if namedErr, ok := err.(NamedError); ok {
		return &DefaultNamedError{Err: namedErr.Error(), Name: namedErr.ErrorName()}
	}
	return &DefaultNamedError{Err: err.Error()}
}

type HostConfigError struct {
	msg string
}

func (err *HostConfigError) Error() string     { return err.msg }
func (err *HostConfigError) ErrorName() string { return "HostConfigError" }

type DockerClientConfigError struct {
	msg string
}

func (err *DockerClientConfigError) Error() string     { return err.msg }
func (err *DockerClientConfigError) ErrorName() string { return "DockerClientConfigError" }
