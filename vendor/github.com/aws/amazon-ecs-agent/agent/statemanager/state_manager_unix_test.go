// +build !windows

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
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/aws/amazon-ecs-agent/agent/api"
	"github.com/aws/amazon-ecs-agent/agent/config"
	"github.com/aws/amazon-ecs-agent/agent/engine"
	"github.com/aws/amazon-ecs-agent/agent/engine/dockerstate"
	engine_testutils "github.com/aws/amazon-ecs-agent/agent/engine/testutils"
	"github.com/aws/amazon-ecs-agent/agent/statemanager"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStateManager(t *testing.T) {
	tmpDir, err := ioutil.TempDir("/tmp", "ecs_statemanager_test")
	assert.Nil(t, err)
	defer os.RemoveAll(tmpDir)
	cfg := &config.Config{DataDir: tmpDir}
	manager, err := statemanager.NewStateManager(cfg)
	assert.Nil(t, err, "Error loading manager")

	err = manager.Load()
	assert.Nil(t, err, "Expected loading a non-existant file to not be an error")

	// Now let's make some state to save
	containerInstanceArn := ""
	taskEngine := engine.NewTaskEngine(&config.Config{}, nil, nil, nil, nil, dockerstate.NewTaskEngineState(), nil)

	manager, err = statemanager.NewStateManager(cfg, statemanager.AddSaveable("TaskEngine", taskEngine), statemanager.AddSaveable("ContainerInstanceArn", &containerInstanceArn))
	require.Nil(t, err)

	containerInstanceArn = "containerInstanceArn"

	testTask := &api.Task{Arn: "test-arn"}
	taskEngine.(*engine.DockerTaskEngine).State().AddTask(testTask)

	err = manager.Save()
	require.Nil(t, err, "Error saving state")

	assertFileMode(t, filepath.Join(tmpDir, "ecs_agent_data.json"))

	// Now make sure we can load that state sanely
	loadedTaskEngine := engine.NewTaskEngine(&config.Config{}, nil, nil, nil, nil, dockerstate.NewTaskEngineState(), nil)
	var loadedContainerInstanceArn string

	manager, err = statemanager.NewStateManager(cfg, statemanager.AddSaveable("TaskEngine", &loadedTaskEngine), statemanager.AddSaveable("ContainerInstanceArn", &loadedContainerInstanceArn))
	require.Nil(t, err)

	err = manager.Load()
	require.Nil(t, err, "Error loading state")

	assert.Equal(t, containerInstanceArn, loadedContainerInstanceArn, "Did not load containerInstanceArn correctly")

	if !engine_testutils.DockerTaskEnginesEqual(loadedTaskEngine.(*engine.DockerTaskEngine), (taskEngine.(*engine.DockerTaskEngine))) {
		t.Error("Did not load taskEngine correctly")
	}

	// I'd rather double check .Equal there; let's make sure ListTasks agrees.
	tasks, err := loadedTaskEngine.ListTasks()
	assert.Nil(t, err, "Error listing tasks")
	require.Equal(t, 1, len(tasks), "Should have a task!")
	assert.Equal(t, "test-arn", tasks[0].Arn, "Wrong arn")
}

func assertFileMode(t *testing.T, path string) {
	info, err := os.Stat(path)
	assert.Nil(t, err)

	mode := info.Mode()
	assert.Equal(t, os.FileMode(0600), mode, "Wrong file mode")
}
