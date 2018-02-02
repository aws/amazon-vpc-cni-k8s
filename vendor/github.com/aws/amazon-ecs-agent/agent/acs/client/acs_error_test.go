// Copyright 2014-2015 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

package acsclient

import (
	"errors"
	"strings"
	"testing"

	"github.com/aws/amazon-ecs-agent/agent/acs/model/ecsacs"
)

var acsErr *acsError

func init() {
	acsErr = &acsError{}
}

func TestInvalidInstanceException(t *testing.T) {
	errMsg := "Invalid instance"
	err := acsErr.NewError(&ecsacs.InvalidInstanceException{Message: &errMsg})

	if err.Retry() {
		t.Fatal("Expected InvalidInstanceException to not be retriable")
	}

	if err.Error() != "InvalidInstanceException: "+errMsg {
		t.Fatal("Error string did not match expected: " + err.Error())
	}
}

func TestInvalidClusterException(t *testing.T) {
	errMsg := "Invalid cluster"
	err := acsErr.NewError(&ecsacs.InvalidClusterException{Message: &errMsg})

	if err.Retry() {
		t.Fatal("Expected to not be retriable")
	}

	if err.Error() != "InvalidClusterException: "+errMsg {
		t.Fatal("Error string did not match expected: " + err.Error())
	}
}

func TestServerException(t *testing.T) {
	err := acsErr.NewError(&ecsacs.ServerException{Message: nil})

	if !err.Retry() {
		t.Fatal("Server exceptions are retriable")
	}

	if err.Error() != "ServerException: null" {
		t.Fatal("Error string did not match expected: " + err.Error())
	}
}

func TestGenericErrorConversion(t *testing.T) {
	err := acsErr.NewError(errors.New("generic error"))

	if !err.Retry() {
		t.Error("Should default to retriable")
	}

	if err.Error() != "ACSError: generic error" {
		t.Error("Did not match expected error: " + err.Error())
	}
}

func TestSomeRandomTypeConversion(t *testing.T) {
	// This is really just an 'it doesn't panic' check.
	err := acsErr.NewError(t)
	if !err.Retry() {
		t.Error("Should default to retriable")
	}
	if !strings.HasPrefix(err.Error(), "ACSError: Unknown error") {
		t.Error("Expected unknown error")
	}
}

func TestBadlyTypedMessage(t *testing.T) {
	// Another 'does not panic' check
	err := acsErr.NewError(struct{ Message int }{1})
	if !err.Retry() {
		t.Error("Should default to retriable")
	}
	_ = err.Error()
}
