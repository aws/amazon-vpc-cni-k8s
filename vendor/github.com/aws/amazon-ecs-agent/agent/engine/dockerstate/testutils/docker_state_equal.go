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

// Package testutils contains files that are used in tests but not elsewhere and thus can
// be excluded from the final executable. Including them in a different package
// allows this to happen
package testutils

import "github.com/aws/amazon-ecs-agent/agent/engine/dockerstate"
import api_testutils "github.com/aws/amazon-ecs-agent/agent/api/testutils"

// DockerStatesEqual determines if the two given dockerstates are equal, for
// equal meaning they have the same tasks and their tasks are equal
func DockerStatesEqual(lhs, rhs dockerstate.TaskEngineState) bool {
	// Simple equality check; just verify that all tasks are equal
	lhsTasks := lhs.AllTasks()
	rhsTasks := rhs.AllTasks()
	if len(lhsTasks) != len(rhsTasks) {
		return false
	}

	for _, left := range lhsTasks {
		right, ok := rhs.TaskByArn(left.Arn)
		if !ok {
			return false
		}
		if !api_testutils.TasksEqual(left, right) {
			return false
		}
	}
	return true
}
