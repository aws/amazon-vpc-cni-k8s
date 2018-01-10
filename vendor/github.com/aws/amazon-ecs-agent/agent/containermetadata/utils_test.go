// +build !integration
// Copyright 2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

package containermetadata

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"testing"
)

const (
	invalidSplitTaskARN = "arn:aws:ecs:region:account-id:task"
)

// TestGetTaskIDFailDueToInvalidID checks the case where task ARN has 6 parts but last section is invalid
func TestGetTaskIDFailDueToInvalidID(t *testing.T) {
	mockTaskARN := invalidSplitTaskARN

	_, err := getTaskIDfromARN(mockTaskARN)
	expectErrorMessage := fmt.Sprintf("get task ARN: cannot find TaskID for TaskARN %s", mockTaskARN)

	assert.Equal(t, expectErrorMessage, err.Error())
}
