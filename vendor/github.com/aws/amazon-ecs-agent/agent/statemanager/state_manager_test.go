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

package statemanager_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/aws/amazon-ecs-agent/agent/api"
	"github.com/aws/amazon-ecs-agent/agent/config"
	"github.com/aws/amazon-ecs-agent/agent/engine"
	"github.com/aws/amazon-ecs-agent/agent/engine/dockerstate"
	"github.com/aws/amazon-ecs-agent/agent/statemanager"
	"github.com/stretchr/testify/assert"
)

func TestStateManagerNonexistantDirectory(t *testing.T) {
	cfg := &config.Config{DataDir: "/path/to/directory/that/doesnt/exist"}
	_, err := statemanager.NewStateManager(cfg)
	assert.Error(t, err)
}

func TestLoadsV1DataCorrectly(t *testing.T) {
	cleanup, err := setupWindowsTest(filepath.Join(".", "testdata", "v1", "1", "ecs_agent_data.json"))
	assert.Nil(t, err, "Failed to set up test")
	defer cleanup()
	cfg := &config.Config{DataDir: filepath.Join(".", "testdata", "v1", "1")}

	taskEngine := engine.NewTaskEngine(&config.Config{}, nil, nil, nil, nil, dockerstate.NewTaskEngineState(), nil)
	var containerInstanceArn, cluster, savedInstanceID string
	var sequenceNumber int64

	stateManager, err := statemanager.NewStateManager(cfg,
		statemanager.AddSaveable("TaskEngine", taskEngine),
		statemanager.AddSaveable("ContainerInstanceArn", &containerInstanceArn),
		statemanager.AddSaveable("Cluster", &cluster),
		statemanager.AddSaveable("EC2InstanceID", &savedInstanceID),
		statemanager.AddSaveable("SeqNum", &sequenceNumber),
	)
	assert.NoError(t, err)
	err = stateManager.Load()
	assert.NoError(t, err)
	assert.Equal(t, cluster, "test")
	assert.True(t, sequenceNumber == 0)
	tasks, err := taskEngine.ListTasks()
	assert.NoError(t, err)
	var deadTask *api.Task
	for _, task := range tasks {
		if task.Arn == "arn:aws:ecs:us-west-2:1234567890:task/f44b4fc9-adb0-4f4f-9dff-871512310588" {
			deadTask = task
		}
	}
	assert.NotNil(t, deadTask)
	assert.Equal(t, deadTask.GetSentStatus(), api.TaskStopped)
	assert.Equal(t, deadTask.Containers[0].SentStatusUnsafe, api.ContainerStopped)
	assert.Equal(t, deadTask.Containers[0].DesiredStatusUnsafe, api.ContainerStopped)
	assert.Equal(t, deadTask.Containers[0].KnownStatusUnsafe, api.ContainerStopped)
	expected, _ := time.Parse(time.RFC3339, "2015-04-28T17:29:48.129140193Z")
	assert.Equal(t, deadTask.GetKnownStatusTime(), expected)
}
