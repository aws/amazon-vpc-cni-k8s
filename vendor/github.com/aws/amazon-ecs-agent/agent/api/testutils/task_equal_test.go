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

package testutils

import (
	"fmt"
	"testing"

	. "github.com/aws/amazon-ecs-agent/agent/api"
	"github.com/stretchr/testify/assert"
)

func TestTaskEqual(t *testing.T) {

	testCases := []struct {
		rhs           Task
		lhs           Task
		shouldBeEqual bool
	}{
		// Equal Pairs
		{Task{Arn: "a"}, Task{Arn: "a"}, true},
		{Task{Family: "a"}, Task{Family: "a"}, true},
		{Task{Version: "a"}, Task{Version: "a"}, true},
		{Task{Containers: []*Container{{Name: "a"}}}, Task{Containers: []*Container{{Name: "a"}}}, true},
		{Task{DesiredStatusUnsafe: TaskRunning}, Task{DesiredStatusUnsafe: TaskRunning}, true},
		{Task{KnownStatusUnsafe: TaskRunning}, Task{KnownStatusUnsafe: TaskRunning}, true},

		// Unequal Pairs
		{Task{Arn: "a"}, Task{Arn: "あ"}, false},
		{Task{Family: "a"}, Task{Family: "あ"}, false},
		{Task{Version: "a"}, Task{Version: "あ"}, false},
		{Task{Containers: []*Container{{Name: "a"}}}, Task{Containers: []*Container{{Name: "あ"}}}, false},
		{Task{DesiredStatusUnsafe: TaskRunning}, Task{DesiredStatusUnsafe: TaskStopped}, false},
		{Task{KnownStatusUnsafe: TaskRunning}, Task{KnownStatusUnsafe: TaskStopped}, false},
	}

	for index, tc := range testCases {
		t.Run(fmt.Sprintf("index %d expected %t", index, tc.shouldBeEqual), func(t *testing.T) {
			assert.Equal(t, TasksEqual(&tc.lhs, &tc.rhs), tc.shouldBeEqual, "TasksEqual not working as expected. Check index failure.")
			// Symetric
			assert.Equal(t, TasksEqual(&tc.rhs, &tc.lhs), tc.shouldBeEqual, "Symetric equality check failed. Check index failure.")
		})
	}
}
