// +build linux

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

package app

import (
	"context"
	"testing"

	"github.com/aws/amazon-ecs-agent/agent/api"
	"github.com/aws/amazon-ecs-agent/agent/config"
	"github.com/aws/amazon-ecs-agent/agent/ec2"
	"github.com/aws/amazon-ecs-agent/agent/eventstream"
	"github.com/aws/amazon-ecs-agent/agent/statemanager/mocks"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
)

func TestCompatibilityEnabledSuccess(t *testing.T) {
	ctrl, creds, state, images, _, _, stateManagerFactory, saveableOptionFactory := setup(t)
	defer ctrl.Finish()
	stateManager := mock_statemanager.NewMockStateManager(ctrl)

	cfg := getTestConfig()
	cfg.Checkpoint = true
	cfg.TaskCPUMemLimit = config.DefaultEnabled

	agent := &ecsAgent{
		cfg:                   &cfg,
		stateManagerFactory:   stateManagerFactory,
		saveableOptionFactory: saveableOptionFactory,
		ec2MetadataClient:     ec2.NewBlackholeEC2MetadataClient(),
	}

	gomock.InOrder(
		saveableOptionFactory.EXPECT().AddSaveable(gomock.Any(), gomock.Any()).AnyTimes(),
		stateManagerFactory.EXPECT().NewStateManager(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(stateManager, nil),
		stateManager.EXPECT().Load().AnyTimes(),
		state.EXPECT().AllTasks().Return([]*api.Task{}),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	containerChangeEventStream := eventstream.NewEventStream("events", ctx)
	_, _, err := agent.newTaskEngine(containerChangeEventStream, creds, state, images)

	assert.NoError(t, err)
	assert.True(t, cfg.TaskCPUMemLimit.Enabled())
}

func TestCompatibilityDefaultEnabledFail(t *testing.T) {
	ctrl, creds, state, images, _, _, stateManagerFactory, saveableOptionFactory := setup(t)
	defer ctrl.Finish()
	stateManager := mock_statemanager.NewMockStateManager(ctrl)

	cfg := getTestConfig()
	cfg.Checkpoint = true
	cfg.TaskCPUMemLimit = config.DefaultEnabled

	agent := &ecsAgent{
		cfg:                   &cfg,
		stateManagerFactory:   stateManagerFactory,
		saveableOptionFactory: saveableOptionFactory,
		ec2MetadataClient:     ec2.NewBlackholeEC2MetadataClient(),
	}
	gomock.InOrder(
		saveableOptionFactory.EXPECT().AddSaveable(gomock.Any(), gomock.Any()).AnyTimes(),
		stateManagerFactory.EXPECT().NewStateManager(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(stateManager, nil),
		stateManager.EXPECT().Load().AnyTimes(),
		state.EXPECT().AllTasks().Return(getTaskListWithOneBadTask()),
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	containerChangeEventStream := eventstream.NewEventStream("events", ctx)
	_, _, err := agent.newTaskEngine(containerChangeEventStream, creds, state, images)

	assert.NoError(t, err)
	assert.False(t, cfg.TaskCPUMemLimit.Enabled())
}

func TestCompatibilityExplicitlyEnabledFail(t *testing.T) {
	ctrl, creds, state, images, _, _, stateManagerFactory, saveableOptionFactory := setup(t)
	defer ctrl.Finish()
	stateManager := mock_statemanager.NewMockStateManager(ctrl)

	cfg := getTestConfig()
	cfg.Checkpoint = true
	cfg.TaskCPUMemLimit = config.ExplicitlyEnabled

	agent := &ecsAgent{
		cfg:                   &cfg,
		stateManagerFactory:   stateManagerFactory,
		saveableOptionFactory: saveableOptionFactory,
		ec2MetadataClient:     ec2.NewBlackholeEC2MetadataClient(),
	}
	gomock.InOrder(
		saveableOptionFactory.EXPECT().AddSaveable(gomock.Any(), gomock.Any()).AnyTimes(),
		stateManagerFactory.EXPECT().NewStateManager(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(stateManager, nil),
		stateManager.EXPECT().Load().AnyTimes(),
		state.EXPECT().AllTasks().Return(getTaskListWithOneBadTask()),
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	containerChangeEventStream := eventstream.NewEventStream("events", ctx)
	_, _, err := agent.newTaskEngine(containerChangeEventStream, creds, state, images)

	assert.Error(t, err)
}

func getTaskListWithOneBadTask() []*api.Task {
	oldtask := &api.Task{}
	newtask := &api.Task{
		MemoryCPULimitsEnabled: true,
	}
	return []*api.Task{oldtask, newtask}
}
