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

package utils

import (
	"fmt"
	"strings"
)

// Retriable defines an interface for retriable methods
type Retriable interface {
	// Retry returns true if the operation can be retried
	Retry() bool
}

// DefaultRetriable implements the Retriable interface with a boolean to
// indicate if retry should occur
type DefaultRetriable struct {
	retry bool
}

// Retry returns true if the operation can be retried
func (dr DefaultRetriable) Retry() bool {
	return dr.retry
}

// NewRetriable creates a new DefaultRetriable object
func NewRetriable(retry bool) Retriable {
	return DefaultRetriable{
		retry: retry,
	}
}

// RetriableError defines an interface for a retriable error
type RetriableError interface {
	Retriable
	error
}

// DefaultRetriableError is used to wrap a retriable error
type DefaultRetriableError struct {
	Retriable
	error
}

// NewRetriableError creates a new DefaultRetriableError object
func NewRetriableError(retriable Retriable, err error) RetriableError {
	return &DefaultRetriableError{
		retriable,
		err,
	}
}

// AttributeError defines an error type to indicate an error with an ECS
// attribute
type AttributeError struct {
	err string
}

// Error returns the error string for AttributeError
func (e AttributeError) Error() string {
	return e.err
}

// NewAttributeError creates a new AttributeError object
func NewAttributeError(err string) AttributeError {
	return AttributeError{err}
}

// MultiErr wraps multiple errors
type MultiErr struct {
	errors []error
}

// Error returns the error string for MultiErr
func (me MultiErr) Error() string {
	ret := make([]string, len(me.errors)+1)
	ret[0] = "Multiple error:"
	for ndx, err := range me.errors {
		ret[ndx+1] = fmt.Sprintf("\t%d: %s", ndx, err.Error())
	}
	return strings.Join(ret, "\n")
}

// NewMultiError creates a new MultErr object
func NewMultiError(errs ...error) error {
	errors := make([]error, 0, len(errs))
	for _, err := range errs {
		if err != nil {
			errors = append(errors, err)
		}
	}
	return MultiErr{errors}
}
