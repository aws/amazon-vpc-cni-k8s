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

package api

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestShouldBeReported(t *testing.T) {
	cases := []struct {
		status          TaskStatus
		containerChange []ContainerStateChange
		result          bool
	}{
		{ // Normal task state change to running
			status: TaskRunning,
			result: true,
		},
		{ // Normal task state change to stopped
			status: TaskStopped,
			result: true,
		},
		{ // Container changed while task is not in steady state
			status: TaskCreated,
			containerChange: []ContainerStateChange{
				{TaskArn: "taskarn"},
			},
			result: true,
		},
		{ // No container change and task status not recognized
			status: TaskCreated,
			result: false,
		},
	}

	for _, tc := range cases {
		t.Run(fmt.Sprintf("task change status: %s, container change: %s", tc.status, len(tc.containerChange) > 0),
			func(t *testing.T) {
				taskChange := TaskStateChange{
					Status:     tc.status,
					Containers: tc.containerChange,
				}

				assert.Equal(t, tc.result, taskChange.ShouldBeReported())
			})
	}
}

func TestSetTaskTimestamps(t *testing.T) {
	t1 := time.Now()
	t2 := t1.Add(time.Second)
	t3 := t2.Add(time.Second)

	change := &TaskStateChange{
		Task: &Task{
			PullStartedAt:      t1,
			PullStoppedAt:      t2,
			ExecutionStoppedAt: t3,
		},
	}

	change.SetTaskTimestamps()
	assert.Equal(t, t1.UTC().String(), change.PullStartedAt.String())
	assert.Equal(t, t2.UTC().String(), change.PullStoppedAt.String())
	assert.Equal(t, t3.UTC().String(), change.ExecutionStoppedAt.String())
}
