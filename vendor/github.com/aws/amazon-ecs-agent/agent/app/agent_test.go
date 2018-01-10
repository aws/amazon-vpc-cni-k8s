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
	"errors"
	"fmt"
	"testing"

	"golang.org/x/net/context"

	"github.com/aws/amazon-ecs-agent/agent/api"
	"github.com/aws/amazon-ecs-agent/agent/api/mocks"
	"github.com/aws/amazon-ecs-agent/agent/app/factory/mocks"
	app_mocks "github.com/aws/amazon-ecs-agent/agent/app/mocks"
	"github.com/aws/amazon-ecs-agent/agent/config"
	"github.com/aws/amazon-ecs-agent/agent/credentials/mocks"
	"github.com/aws/amazon-ecs-agent/agent/ec2"
	"github.com/aws/amazon-ecs-agent/agent/ec2/mocks"
	"github.com/aws/amazon-ecs-agent/agent/ecs_client/model/ecs"
	"github.com/aws/amazon-ecs-agent/agent/engine"
	"github.com/aws/amazon-ecs-agent/agent/engine/dockerstate/mocks"
	"github.com/aws/amazon-ecs-agent/agent/eventstream"
	"github.com/aws/amazon-ecs-agent/agent/resources/mock_resources"
	"github.com/aws/amazon-ecs-agent/agent/sighandlers/exitcodes"
	"github.com/aws/amazon-ecs-agent/agent/statemanager"
	"github.com/aws/amazon-ecs-agent/agent/statemanager/mocks"
	"github.com/aws/amazon-ecs-agent/agent/utils"

	"github.com/aws/aws-sdk-go/aws/awserr"
	aws_credentials "github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
)

const (
	clusterName          = "some-cluster"
	containerInstanceARN = "container-instance1"
)

var capabilities []*ecs.Attribute

func setup(t *testing.T) (*gomock.Controller,
	*mock_credentials.MockManager,
	*mock_dockerstate.MockTaskEngineState,
	*engine.MockImageManager,
	*mock_api.MockECSClient,
	*engine.MockDockerClient,
	*mock_factory.MockStateManager,
	*mock_factory.MockSaveableOption) {

	ctrl := gomock.NewController(t)

	return ctrl,
		mock_credentials.NewMockManager(ctrl),
		mock_dockerstate.NewMockTaskEngineState(ctrl),
		engine.NewMockImageManager(ctrl),
		mock_api.NewMockECSClient(ctrl),
		engine.NewMockDockerClient(ctrl),
		mock_factory.NewMockStateManager(ctrl),
		mock_factory.NewMockSaveableOption(ctrl)
}

func TestDoStartNewTaskEngineError(t *testing.T) {
	ctrl, credentialsManager, state, imageManager, client,
		dockerClient, stateManagerFactory, saveableOptionFactory := setup(t)
	defer ctrl.Finish()

	gomock.InOrder(
		saveableOptionFactory.EXPECT().AddSaveable("ContainerInstanceArn", gomock.Any()).Return(nil),
		saveableOptionFactory.EXPECT().AddSaveable("Cluster", gomock.Any()).Return(nil),
		saveableOptionFactory.EXPECT().AddSaveable("EC2InstanceID", gomock.Any()).Return(nil),
		// An error in creating the state manager should result in an
		// error from newTaskEngine as well
		stateManagerFactory.EXPECT().NewStateManager(gomock.Any(),
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
		).Return(
			nil, errors.New("error")),
	)

	cfg := getTestConfig()
	cfg.Checkpoint = true
	ctx, cancel := context.WithCancel(context.TODO())
	// Cancel the context to cancel async routines
	defer cancel()
	agent := &ecsAgent{
		ctx:                   ctx,
		cfg:                   &cfg,
		dockerClient:          dockerClient,
		stateManagerFactory:   stateManagerFactory,
		saveableOptionFactory: saveableOptionFactory,
	}

	exitCode := agent.doStart(eventstream.NewEventStream("events", ctx),
		credentialsManager, state, imageManager, client)
	assert.Equal(t, exitcodes.ExitTerminal, exitCode)
}

func TestDoStartNewStateManagerError(t *testing.T) {
	ctrl, credentialsManager, state, imageManager, client,
		dockerClient, stateManagerFactory, saveableOptionFactory := setup(t)
	defer ctrl.Finish()

	ec2MetadataClient := mock_ec2.NewMockEC2MetadataClient(ctrl)
	expectedInstanceID := "inst-1"
	iid := ec2metadata.EC2InstanceIdentityDocument{
		InstanceID: expectedInstanceID,
		Region:     "us-west-2",
	}
	gomock.InOrder(
		saveableOptionFactory.EXPECT().AddSaveable("ContainerInstanceArn", gomock.Any()).Return(nil),
		saveableOptionFactory.EXPECT().AddSaveable("Cluster", gomock.Any()).Return(nil),
		saveableOptionFactory.EXPECT().AddSaveable("EC2InstanceID", gomock.Any()).Return(nil),
		stateManagerFactory.EXPECT().NewStateManager(gomock.Any(), gomock.Any(),
			gomock.Any(), gomock.Any(), gomock.Any()).Return(
			statemanager.NewNoopStateManager(), nil),
		state.EXPECT().AllTasks().AnyTimes(),
		ec2MetadataClient.EXPECT().InstanceIdentityDocument().Return(iid, nil),
		saveableOptionFactory.EXPECT().AddSaveable("ContainerInstanceArn", gomock.Any()).Return(nil),
		saveableOptionFactory.EXPECT().AddSaveable("Cluster", gomock.Any()).Return(nil),
		saveableOptionFactory.EXPECT().AddSaveable("EC2InstanceID", gomock.Any()).Return(nil),
		stateManagerFactory.EXPECT().NewStateManager(gomock.Any(), gomock.Any(),
			gomock.Any(), gomock.Any(), gomock.Any()).Return(
			nil, errors.New("error")),
	)

	cfg := getTestConfig()
	cfg.Checkpoint = true
	ctx, cancel := context.WithCancel(context.TODO())
	// Cancel the context to cancel async routines
	defer cancel()
	agent := &ecsAgent{
		ctx:                   ctx,
		cfg:                   &cfg,
		dockerClient:          dockerClient,
		ec2MetadataClient:     ec2MetadataClient,
		stateManagerFactory:   stateManagerFactory,
		saveableOptionFactory: saveableOptionFactory,
	}

	exitCode := agent.doStart(eventstream.NewEventStream("events", ctx),
		credentialsManager, state, imageManager, client)
	assert.Equal(t, exitcodes.ExitTerminal, exitCode)
}

func TestDoStartTaskLimitsFail(t *testing.T) {
	ctrl, credentialsManager, state, imageManager, client,
		dockerClient, stateManagerFactory, saveableOptionFactory := setup(t)
	defer ctrl.Finish()

	resource := mock_resources.NewMockResource(ctrl)

	gomock.InOrder(
		saveableOptionFactory.EXPECT().AddSaveable(gomock.Any(), gomock.Any()).AnyTimes(),
		stateManagerFactory.EXPECT().NewStateManager(gomock.Any(), gomock.Any(), gomock.Any(),
			gomock.Any(), gomock.Any()).Return(statemanager.NewNoopStateManager(), nil),
		state.EXPECT().AllTasks().AnyTimes(),
		saveableOptionFactory.EXPECT().AddSaveable(gomock.Any(), gomock.Any()).AnyTimes(),
		stateManagerFactory.EXPECT().NewStateManager(gomock.Any(), gomock.Any(), gomock.Any(),
			gomock.Any(), gomock.Any()).Return(statemanager.NewNoopStateManager(), nil),
		resource.EXPECT().Init().Return(errors.New("test error")),
	)

	cfg := getTestConfig()
	cfg.Checkpoint = true
	cfg.TaskCPUMemLimit = config.ExplicitlyEnabled

	ctx, cancel := context.WithCancel(context.TODO())
	// Cancel the context to cancel async routines
	defer cancel()
	agent := &ecsAgent{
		ctx:                   ctx,
		cfg:                   &cfg,
		dockerClient:          dockerClient,
		stateManagerFactory:   stateManagerFactory,
		saveableOptionFactory: saveableOptionFactory,
		ec2MetadataClient:     ec2.NewBlackholeEC2MetadataClient(),
		resource:              resource,
	}

	exitCode := agent.doStart(eventstream.NewEventStream("events", ctx),
		credentialsManager, state, imageManager, client)
	assert.Equal(t, exitcodes.ExitTerminal, exitCode)
}

func TestDoStartRegisterContainerInstanceErrorTerminal(t *testing.T) {
	ctrl, credentialsManager, state, imageManager, client,
		dockerClient, _, _ := setup(t)
	defer ctrl.Finish()

	mockCredentialsProvider := app_mocks.NewMockProvider(ctrl)

	gomock.InOrder(
		mockCredentialsProvider.EXPECT().Retrieve().Return(aws_credentials.Value{}, nil),
		dockerClient.EXPECT().SupportedVersions().Return(nil),
		dockerClient.EXPECT().KnownVersions().Return(nil),
		client.EXPECT().RegisterContainerInstance(gomock.Any(), gomock.Any()).Return(
			"", utils.NewAttributeError("error")),
	)

	cfg := getTestConfig()
	cfg.TaskCPUMemLimit = config.ExplicitlyDisabled
	ctx, cancel := context.WithCancel(context.TODO())
	// Cancel the context to cancel async routines
	defer cancel()
	agent := &ecsAgent{
		ctx:                ctx,
		cfg:                &cfg,
		credentialProvider: aws_credentials.NewCredentials(mockCredentialsProvider),
		dockerClient:       dockerClient,
	}

	exitCode := agent.doStart(eventstream.NewEventStream("events", ctx),
		credentialsManager, state, imageManager, client)
	assert.Equal(t, exitcodes.ExitTerminal, exitCode)
}

func TestDoStartRegisterContainerInstanceErrorNonTerminal(t *testing.T) {
	ctrl, credentialsManager, state, imageManager, client,
		dockerClient, _, _ := setup(t)
	defer ctrl.Finish()

	mockCredentialsProvider := app_mocks.NewMockProvider(ctrl)
	gomock.InOrder(
		mockCredentialsProvider.EXPECT().Retrieve().Return(aws_credentials.Value{}, nil),
		dockerClient.EXPECT().SupportedVersions().Return(nil),
		dockerClient.EXPECT().KnownVersions().Return(nil),
		client.EXPECT().RegisterContainerInstance(gomock.Any(), gomock.Any()).Return(
			"", errors.New("error")),
	)

	cfg := getTestConfig()
	ctx, cancel := context.WithCancel(context.TODO())
	// Cancel the context to cancel async routines
	defer cancel()
	agent := &ecsAgent{
		ctx:                ctx,
		cfg:                &cfg,
		dockerClient:       dockerClient,
		credentialProvider: aws_credentials.NewCredentials(mockCredentialsProvider),
	}

	exitCode := agent.doStart(eventstream.NewEventStream("events", ctx),
		credentialsManager, state, imageManager, client)
	assert.Equal(t, exitcodes.ExitError, exitCode)
}

func TestNewTaskEngineRestoreFromCheckpointNoEC2InstanceIDToLoadHappyPath(t *testing.T) {
	ctrl, credentialsManager, state, imageManager, _,
		dockerClient, stateManagerFactory, saveableOptionFactory := setup(t)
	defer ctrl.Finish()

	ec2MetadataClient := mock_ec2.NewMockEC2MetadataClient(ctrl)
	cfg := getTestConfig()
	cfg.Checkpoint = true
	expectedInstanceID := "inst-1"
	iid := ec2metadata.EC2InstanceIdentityDocument{
		InstanceID: expectedInstanceID,
		Region:     "us-west-2",
	}
	gomock.InOrder(
		saveableOptionFactory.EXPECT().AddSaveable("ContainerInstanceArn", gomock.Any()).Do(
			func(name string, saveable statemanager.Saveable) {
				previousContainerInstanceARN, ok := saveable.(*string)
				assert.True(t, ok)
				*previousContainerInstanceARN = "prev-container-inst"
			}).Return(nil),
		saveableOptionFactory.EXPECT().AddSaveable("Cluster", gomock.Any()).Return(nil),
		saveableOptionFactory.EXPECT().AddSaveable("EC2InstanceID", gomock.Any()).Return(nil),
		stateManagerFactory.EXPECT().NewStateManager(gomock.Any(), gomock.Any(),
			gomock.Any(), gomock.Any(), gomock.Any()).Return(
			statemanager.NewNoopStateManager(), nil),
		state.EXPECT().AllTasks().AnyTimes(),
		ec2MetadataClient.EXPECT().InstanceIdentityDocument().Return(iid, nil),
	)

	ctx, cancel := context.WithCancel(context.TODO())
	// Cancel the context to cancel async routines
	defer cancel()
	agent := &ecsAgent{
		ctx:                   ctx,
		cfg:                   &cfg,
		dockerClient:          dockerClient,
		stateManagerFactory:   stateManagerFactory,
		ec2MetadataClient:     ec2MetadataClient,
		saveableOptionFactory: saveableOptionFactory,
	}

	_, instanceID, err := agent.newTaskEngine(eventstream.NewEventStream("events", ctx),
		credentialsManager, state, imageManager)
	assert.NoError(t, err)
	assert.Equal(t, expectedInstanceID, instanceID)
	assert.Equal(t, "prev-container-inst", agent.containerInstanceARN)
}

func TestNewTaskEngineRestoreFromCheckpointPreviousEC2InstanceIDLoadedHappyPath(t *testing.T) {
	ctrl, credentialsManager, state, imageManager, _,
		dockerClient, stateManagerFactory, saveableOptionFactory := setup(t)
	defer ctrl.Finish()

	ec2MetadataClient := mock_ec2.NewMockEC2MetadataClient(ctrl)
	cfg := getTestConfig()
	cfg.Checkpoint = true
	expectedInstanceID := "inst-1"
	iid := ec2metadata.EC2InstanceIdentityDocument{
		InstanceID: expectedInstanceID,
		Region:     "us-west-2",
	}

	gomock.InOrder(
		saveableOptionFactory.EXPECT().AddSaveable("ContainerInstanceArn", gomock.Any()).Do(
			func(name string, saveable statemanager.Saveable) {
				previousContainerInstanceARN, ok := saveable.(*string)
				assert.True(t, ok)
				*previousContainerInstanceARN = "prev-container-inst"
			}).Return(nil),
		saveableOptionFactory.EXPECT().AddSaveable("Cluster", gomock.Any()).Return(nil),
		saveableOptionFactory.EXPECT().AddSaveable("EC2InstanceID", gomock.Any()).Do(
			func(name string, saveable statemanager.Saveable) {
				previousEC2InstanceID, ok := saveable.(*string)
				assert.True(t, ok)
				*previousEC2InstanceID = "inst-2"
			}).Return(nil),
		stateManagerFactory.EXPECT().NewStateManager(gomock.Any(), gomock.Any(),
			gomock.Any(), gomock.Any(), gomock.Any()).Return(
			statemanager.NewNoopStateManager(), nil),
		state.EXPECT().AllTasks().AnyTimes(),
		ec2MetadataClient.EXPECT().InstanceIdentityDocument().Return(iid, nil),
		state.EXPECT().Reset(),
	)

	ctx, cancel := context.WithCancel(context.TODO())
	// Cancel the context to cancel async routines
	defer cancel()
	agent := &ecsAgent{
		ctx:                   ctx,
		cfg:                   &cfg,
		dockerClient:          dockerClient,
		stateManagerFactory:   stateManagerFactory,
		ec2MetadataClient:     ec2MetadataClient,
		saveableOptionFactory: saveableOptionFactory,
	}

	_, instanceID, err := agent.newTaskEngine(eventstream.NewEventStream("events", ctx),
		credentialsManager, state, imageManager)
	assert.NoError(t, err)
	assert.Equal(t, expectedInstanceID, instanceID)
	assert.NotEqual(t, "prev-container-inst", agent.containerInstanceARN)
}

func TestNewTaskEngineRestoreFromCheckpointClusterIDMismatch(t *testing.T) {
	ctrl, credentialsManager, state, imageManager, _,
		dockerClient, stateManagerFactory, saveableOptionFactory := setup(t)
	defer ctrl.Finish()

	ec2MetadataClient := mock_ec2.NewMockEC2MetadataClient(ctrl)
	cfg := getTestConfig()
	cfg.Checkpoint = true
	cfg.Cluster = "default"
	ec2InstanceID := "inst-1"
	iid := ec2metadata.EC2InstanceIdentityDocument{
		InstanceID: ec2InstanceID,
		Region:     "us-west-2",
	}

	gomock.InOrder(
		saveableOptionFactory.EXPECT().AddSaveable("ContainerInstanceArn", gomock.Any()).Do(
			func(name string, saveable statemanager.Saveable) {
				previousContainerInstanceARN, ok := saveable.(*string)
				assert.True(t, ok)
				*previousContainerInstanceARN = ec2InstanceID
			}).Return(nil),
		saveableOptionFactory.EXPECT().AddSaveable("Cluster", gomock.Any()).Do(
			func(name string, saveable statemanager.Saveable) {
				previousCluster, ok := saveable.(*string)
				assert.True(t, ok)
				*previousCluster = clusterName
			}).Return(nil),
		saveableOptionFactory.EXPECT().AddSaveable("EC2InstanceID", gomock.Any()).Return(nil),
		stateManagerFactory.EXPECT().NewStateManager(gomock.Any(), gomock.Any(),
			gomock.Any(), gomock.Any(), gomock.Any()).Return(
			statemanager.NewNoopStateManager(), nil),
		state.EXPECT().AllTasks().AnyTimes(),
		ec2MetadataClient.EXPECT().InstanceIdentityDocument().Return(iid, nil),
	)

	ctx, cancel := context.WithCancel(context.TODO())
	// Cancel the context to cancel async routines
	defer cancel()
	agent := &ecsAgent{
		ctx:                   ctx,
		cfg:                   &cfg,
		dockerClient:          dockerClient,
		stateManagerFactory:   stateManagerFactory,
		ec2MetadataClient:     ec2MetadataClient,
		saveableOptionFactory: saveableOptionFactory,
	}

	_, _, err := agent.newTaskEngine(eventstream.NewEventStream("events", ctx),
		credentialsManager, state, imageManager)
	assert.Error(t, err)
	assert.True(t, isClusterMismatch(err))
}

func TestNewTaskEngineRestoreFromCheckpointNewStateManagerError(t *testing.T) {
	ctrl, credentialsManager, state, imageManager, _,
		dockerClient, stateManagerFactory, saveableOptionFactory := setup(t)
	defer ctrl.Finish()

	cfg := getTestConfig()
	cfg.Checkpoint = true
	gomock.InOrder(
		saveableOptionFactory.EXPECT().AddSaveable("ContainerInstanceArn", gomock.Any()).Return(nil),
		saveableOptionFactory.EXPECT().AddSaveable("Cluster", gomock.Any()).Return(nil),
		saveableOptionFactory.EXPECT().AddSaveable("EC2InstanceID", gomock.Any()).Return(nil),
		stateManagerFactory.EXPECT().NewStateManager(gomock.Any(), gomock.Any(),
			gomock.Any(), gomock.Any(), gomock.Any()).Return(
			nil, errors.New("error")),
	)

	ctx, cancel := context.WithCancel(context.TODO())
	// Cancel the context to cancel async routines
	defer cancel()
	agent := &ecsAgent{
		ctx:                   ctx,
		cfg:                   &cfg,
		dockerClient:          dockerClient,
		stateManagerFactory:   stateManagerFactory,
		saveableOptionFactory: saveableOptionFactory,
	}

	_, _, err := agent.newTaskEngine(eventstream.NewEventStream("events", ctx),
		credentialsManager, state, imageManager)
	assert.Error(t, err)
	assert.False(t, isTransient(err))
}

func TestNewTaskEngineRestoreFromCheckpointStateLoadError(t *testing.T) {
	ctrl, credentialsManager, state, imageManager, _,
		dockerClient, stateManagerFactory, saveableOptionFactory := setup(t)
	defer ctrl.Finish()

	stateManager := mock_statemanager.NewMockStateManager(ctrl)
	cfg := getTestConfig()
	cfg.Checkpoint = true
	gomock.InOrder(
		saveableOptionFactory.EXPECT().AddSaveable("ContainerInstanceArn", gomock.Any()).Return(nil),
		saveableOptionFactory.EXPECT().AddSaveable("Cluster", gomock.Any()).Return(nil),
		saveableOptionFactory.EXPECT().AddSaveable("EC2InstanceID", gomock.Any()).Return(nil),
		stateManagerFactory.EXPECT().NewStateManager(gomock.Any(),
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
		).Return(stateManager, nil),
		stateManager.EXPECT().Load().Return(errors.New("error")),
	)

	ctx, cancel := context.WithCancel(context.TODO())
	// Cancel the context to cancel async routines
	defer cancel()
	agent := &ecsAgent{
		ctx:                   ctx,
		cfg:                   &cfg,
		dockerClient:          dockerClient,
		stateManagerFactory:   stateManagerFactory,
		saveableOptionFactory: saveableOptionFactory,
	}

	_, _, err := agent.newTaskEngine(eventstream.NewEventStream("events", ctx),
		credentialsManager, state, imageManager)
	assert.Error(t, err)
	assert.False(t, isTransient(err))
}

func TestNewTaskEngineRestoreFromCheckpoint(t *testing.T) {
	ctrl, credentialsManager, state, imageManager, _,
		dockerClient, stateManagerFactory, saveableOptionFactory := setup(t)
	defer ctrl.Finish()

	ec2MetadataClient := mock_ec2.NewMockEC2MetadataClient(ctrl)
	cfg := getTestConfig()
	cfg.Checkpoint = true
	expectedInstanceID := "inst-1"
	iid := ec2metadata.EC2InstanceIdentityDocument{
		InstanceID: expectedInstanceID,
		Region:     "us-west-2",
	}
	gomock.InOrder(
		saveableOptionFactory.EXPECT().AddSaveable("ContainerInstanceArn", gomock.Any()).Return(nil),
		saveableOptionFactory.EXPECT().AddSaveable("Cluster", gomock.Any()).Return(nil),
		saveableOptionFactory.EXPECT().AddSaveable("EC2InstanceID", gomock.Any()).Return(nil),
		stateManagerFactory.EXPECT().NewStateManager(gomock.Any(),
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
		).Return(statemanager.NewNoopStateManager(), nil),
		state.EXPECT().AllTasks().AnyTimes(),
		ec2MetadataClient.EXPECT().InstanceIdentityDocument().Return(iid, nil),
	)

	ctx, cancel := context.WithCancel(context.TODO())
	// Cancel the context to cancel async routines
	defer cancel()
	agent := &ecsAgent{
		ctx:                   ctx,
		cfg:                   &cfg,
		dockerClient:          dockerClient,
		stateManagerFactory:   stateManagerFactory,
		ec2MetadataClient:     ec2MetadataClient,
		saveableOptionFactory: saveableOptionFactory,
	}

	_, instanceID, err := agent.newTaskEngine(eventstream.NewEventStream("events", ctx),
		credentialsManager, state, imageManager)
	assert.NoError(t, err)
	assert.Equal(t, expectedInstanceID, instanceID)
}

func TestSetClusterInConfigMismatch(t *testing.T) {
	clusterNamesInConfig := []string{"", "foo"}
	for _, clusterNameInConfig := range clusterNamesInConfig {
		t.Run(fmt.Sprintf("cluster in config is '%s'", clusterNameInConfig), func(t *testing.T) {
			cfg := getTestConfig()
			cfg.Cluster = ""
			agent := &ecsAgent{cfg: &cfg}
			err := agent.setClusterInConfig("bar")
			assert.Error(t, err)
		})
	}
}

func TestSetClusterInConfig(t *testing.T) {
	cfg := getTestConfig()
	cfg.Cluster = clusterName
	agent := &ecsAgent{cfg: &cfg}
	err := agent.setClusterInConfig(clusterName)
	assert.NoError(t, err)
}

func TestGetEC2InstanceIDIIDError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ec2MetadataClient := mock_ec2.NewMockEC2MetadataClient(ctrl)
	agent := &ecsAgent{ec2MetadataClient: ec2MetadataClient}

	ec2MetadataClient.EXPECT().InstanceIdentityDocument().Return(ec2metadata.EC2InstanceIdentityDocument{}, errors.New("error"))
	assert.Equal(t, "", agent.getEC2InstanceID())
}

func TestReregisterContainerInstanceHappyPath(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDockerClient := engine.NewMockDockerClient(ctrl)
	stateManager := mock_statemanager.NewMockStateManager(ctrl)
	client := mock_api.NewMockECSClient(ctrl)
	mockCredentialsProvider := app_mocks.NewMockProvider(ctrl)

	gomock.InOrder(
		mockCredentialsProvider.EXPECT().Retrieve().Return(aws_credentials.Value{}, nil),
		mockDockerClient.EXPECT().SupportedVersions().Return(nil),
		mockDockerClient.EXPECT().KnownVersions().Return(nil),
		client.EXPECT().RegisterContainerInstance(containerInstanceARN, gomock.Any()).Return(containerInstanceARN, nil),
	)
	cfg := getTestConfig()
	cfg.Cluster = clusterName
	ctx, cancel := context.WithCancel(context.TODO())
	// Cancel the context to cancel async routines
	defer cancel()
	agent := &ecsAgent{
		ctx:                ctx,
		cfg:                &cfg,
		dockerClient:       mockDockerClient,
		credentialProvider: aws_credentials.NewCredentials(mockCredentialsProvider),
	}
	agent.containerInstanceARN = containerInstanceARN

	err := agent.registerContainerInstance(stateManager, client, nil)
	assert.NoError(t, err)
}

func TestReregisterContainerInstanceInstanceTypeChanged(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDockerClient := engine.NewMockDockerClient(ctrl)
	stateManager := mock_statemanager.NewMockStateManager(ctrl)
	client := mock_api.NewMockECSClient(ctrl)
	mockCredentialsProvider := app_mocks.NewMockProvider(ctrl)

	gomock.InOrder(
		mockCredentialsProvider.EXPECT().Retrieve().Return(aws_credentials.Value{}, nil),
		mockDockerClient.EXPECT().SupportedVersions().Return(nil),
		mockDockerClient.EXPECT().KnownVersions().Return(nil),
		client.EXPECT().RegisterContainerInstance(containerInstanceARN, gomock.Any()).Return(
			"", awserr.New("", api.InstanceTypeChangedErrorMessage, errors.New(""))),
	)

	cfg := getTestConfig()
	cfg.Cluster = clusterName
	ctx, cancel := context.WithCancel(context.TODO())
	// Cancel the context to cancel async routines
	defer cancel()
	agent := &ecsAgent{
		ctx:                ctx,
		cfg:                &cfg,
		dockerClient:       mockDockerClient,
		credentialProvider: aws_credentials.NewCredentials(mockCredentialsProvider),
	}
	agent.containerInstanceARN = containerInstanceARN

	err := agent.registerContainerInstance(stateManager, client, nil)
	assert.Error(t, err)
	assert.False(t, isTransient(err))
}

func TestReregisterContainerInstanceAttributeError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDockerClient := engine.NewMockDockerClient(ctrl)
	stateManager := mock_statemanager.NewMockStateManager(ctrl)
	client := mock_api.NewMockECSClient(ctrl)
	mockCredentialsProvider := app_mocks.NewMockProvider(ctrl)

	gomock.InOrder(
		mockCredentialsProvider.EXPECT().Retrieve().Return(aws_credentials.Value{}, nil),
		mockDockerClient.EXPECT().SupportedVersions().Return(nil),
		mockDockerClient.EXPECT().KnownVersions().Return(nil),
		client.EXPECT().RegisterContainerInstance(containerInstanceARN, gomock.Any()).Return(
			"", utils.NewAttributeError("error")),
	)

	cfg := getTestConfig()
	cfg.Cluster = clusterName
	ctx, cancel := context.WithCancel(context.TODO())
	// Cancel the context to cancel async routines
	defer cancel()
	agent := &ecsAgent{
		ctx:                ctx,
		cfg:                &cfg,
		dockerClient:       mockDockerClient,
		credentialProvider: aws_credentials.NewCredentials(mockCredentialsProvider),
	}
	agent.containerInstanceARN = containerInstanceARN

	err := agent.registerContainerInstance(stateManager, client, nil)
	assert.Error(t, err)
	assert.False(t, isTransient(err))
}

func TestReregisterContainerInstanceNonTerminalError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDockerClient := engine.NewMockDockerClient(ctrl)
	stateManager := mock_statemanager.NewMockStateManager(ctrl)
	client := mock_api.NewMockECSClient(ctrl)
	mockCredentialsProvider := app_mocks.NewMockProvider(ctrl)

	gomock.InOrder(
		mockCredentialsProvider.EXPECT().Retrieve().Return(aws_credentials.Value{}, nil),
		mockDockerClient.EXPECT().SupportedVersions().Return(nil),
		mockDockerClient.EXPECT().KnownVersions().Return(nil),
		client.EXPECT().RegisterContainerInstance(containerInstanceARN, gomock.Any()).Return(
			"", errors.New("error")),
	)

	cfg := getTestConfig()
	cfg.Cluster = clusterName
	ctx, cancel := context.WithCancel(context.TODO())
	// Cancel the context to cancel async routines
	defer cancel()
	agent := &ecsAgent{
		ctx:                ctx,
		cfg:                &cfg,
		dockerClient:       mockDockerClient,
		credentialProvider: aws_credentials.NewCredentials(mockCredentialsProvider),
	}
	agent.containerInstanceARN = containerInstanceARN

	err := agent.registerContainerInstance(stateManager, client, nil)
	assert.Error(t, err)
	assert.True(t, isTransient(err))
}

func TestRegisterContainerInstanceWhenContainerInstanceARNIsNotSetHappyPath(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDockerClient := engine.NewMockDockerClient(ctrl)
	stateManager := mock_statemanager.NewMockStateManager(ctrl)
	client := mock_api.NewMockECSClient(ctrl)
	mockCredentialsProvider := app_mocks.NewMockProvider(ctrl)

	gomock.InOrder(
		mockCredentialsProvider.EXPECT().Retrieve().Return(aws_credentials.Value{}, nil),
		mockDockerClient.EXPECT().SupportedVersions().Return(nil),
		mockDockerClient.EXPECT().KnownVersions().Return(nil),
		client.EXPECT().RegisterContainerInstance("", gomock.Any()).Return(containerInstanceARN, nil),
		stateManager.EXPECT().Save(),
	)

	cfg := getTestConfig()
	cfg.Cluster = clusterName
	ctx, cancel := context.WithCancel(context.TODO())
	// Cancel the context to cancel async routines
	defer cancel()
	agent := &ecsAgent{
		ctx:                ctx,
		cfg:                &cfg,
		dockerClient:       mockDockerClient,
		credentialProvider: aws_credentials.NewCredentials(mockCredentialsProvider),
	}

	err := agent.registerContainerInstance(stateManager, client, nil)
	assert.NoError(t, err)
	assert.Equal(t, containerInstanceARN, agent.containerInstanceARN)
}

func TestRegisterContainerInstanceWhenContainerInstanceARNIsNotSetCanRetryError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDockerClient := engine.NewMockDockerClient(ctrl)
	stateManager := mock_statemanager.NewMockStateManager(ctrl)
	client := mock_api.NewMockECSClient(ctrl)
	mockCredentialsProvider := app_mocks.NewMockProvider(ctrl)

	retriableError := utils.NewRetriableError(utils.NewRetriable(true), errors.New("error"))
	gomock.InOrder(
		mockCredentialsProvider.EXPECT().Retrieve().Return(aws_credentials.Value{}, nil),
		mockDockerClient.EXPECT().SupportedVersions().Return(nil),
		mockDockerClient.EXPECT().KnownVersions().Return(nil),
		client.EXPECT().RegisterContainerInstance("", gomock.Any()).Return("", retriableError),
	)

	cfg := getTestConfig()
	cfg.Cluster = clusterName
	ctx, cancel := context.WithCancel(context.TODO())
	// Cancel the context to cancel async routines
	defer cancel()
	agent := &ecsAgent{
		ctx:                ctx,
		cfg:                &cfg,
		dockerClient:       mockDockerClient,
		credentialProvider: aws_credentials.NewCredentials(mockCredentialsProvider),
	}

	err := agent.registerContainerInstance(stateManager, client, nil)
	assert.Error(t, err)
	assert.True(t, isTransient(err))
}

func TestRegisterContainerInstanceWhenContainerInstanceARNIsNotSetCannotRetryError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDockerClient := engine.NewMockDockerClient(ctrl)
	stateManager := mock_statemanager.NewMockStateManager(ctrl)
	client := mock_api.NewMockECSClient(ctrl)
	mockCredentialsProvider := app_mocks.NewMockProvider(ctrl)

	cannotRetryError := utils.NewRetriableError(utils.NewRetriable(false), errors.New("error"))
	gomock.InOrder(
		mockCredentialsProvider.EXPECT().Retrieve().Return(aws_credentials.Value{}, nil),
		mockDockerClient.EXPECT().SupportedVersions().Return(nil),
		mockDockerClient.EXPECT().KnownVersions().Return(nil),
		client.EXPECT().RegisterContainerInstance("", gomock.Any()).Return("", cannotRetryError),
	)

	cfg := getTestConfig()
	cfg.Cluster = clusterName
	ctx, cancel := context.WithCancel(context.TODO())
	// Cancel the context to cancel async routines
	defer cancel()
	agent := &ecsAgent{
		ctx:                ctx,
		cfg:                &cfg,
		dockerClient:       mockDockerClient,
		credentialProvider: aws_credentials.NewCredentials(mockCredentialsProvider),
	}

	err := agent.registerContainerInstance(stateManager, client, nil)
	assert.Error(t, err)
	assert.False(t, isTransient(err))
}

func TestRegisterContainerInstanceWhenContainerInstanceARNIsNotSetAttributeError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDockerClient := engine.NewMockDockerClient(ctrl)
	stateManager := mock_statemanager.NewMockStateManager(ctrl)
	client := mock_api.NewMockECSClient(ctrl)
	mockCredentialsProvider := app_mocks.NewMockProvider(ctrl)

	gomock.InOrder(
		mockCredentialsProvider.EXPECT().Retrieve().Return(aws_credentials.Value{}, nil),
		mockDockerClient.EXPECT().SupportedVersions().Return(nil),
		mockDockerClient.EXPECT().KnownVersions().Return(nil),
		client.EXPECT().RegisterContainerInstance("", gomock.Any()).Return(
			"", utils.NewAttributeError("error")),
	)

	cfg := getTestConfig()
	cfg.Cluster = clusterName
	ctx, cancel := context.WithCancel(context.TODO())
	// Cancel the context to cancel async routines
	defer cancel()
	agent := &ecsAgent{
		ctx:                ctx,
		cfg:                &cfg,
		dockerClient:       mockDockerClient,
		credentialProvider: aws_credentials.NewCredentials(mockCredentialsProvider),
	}

	err := agent.registerContainerInstance(stateManager, client, nil)
	assert.Error(t, err)
	assert.False(t, isTransient(err))
}

func TestRegisterContainerInstanceInvalidParameterTerminalError(t *testing.T) {
	ctrl, credentialsManager, state, imageManager, client,
		dockerClient, _, _ := setup(t)
	defer ctrl.Finish()

	mockCredentialsProvider := app_mocks.NewMockProvider(ctrl)

	gomock.InOrder(
		mockCredentialsProvider.EXPECT().Retrieve().Return(aws_credentials.Value{}, nil),
		dockerClient.EXPECT().SupportedVersions().Return(nil),
		dockerClient.EXPECT().KnownVersions().Return(nil),
		client.EXPECT().RegisterContainerInstance(gomock.Any(), gomock.Any()).Return(
			"", awserr.New("InvalidParameterException", "", nil)),
	)

	cfg := getTestConfig()
	ctx, cancel := context.WithCancel(context.TODO())
	// Cancel the context to cancel async routines
	defer cancel()
	agent := &ecsAgent{
		ctx:                ctx,
		cfg:                &cfg,
		credentialProvider: aws_credentials.NewCredentials(mockCredentialsProvider),
		dockerClient:       dockerClient,
	}

	exitCode := agent.doStart(eventstream.NewEventStream("events", ctx),
		credentialsManager, state, imageManager, client)
	assert.Equal(t, exitcodes.ExitTerminal, exitCode)
}

func getTestConfig() config.Config {
	cfg := config.DefaultConfig()
	cfg.TaskCPUMemLimit = config.ExplicitlyDisabled
	return cfg
}
