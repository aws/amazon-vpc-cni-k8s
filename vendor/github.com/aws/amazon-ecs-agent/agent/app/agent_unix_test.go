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
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"sync"
	"testing"

	app_mocks "github.com/aws/amazon-ecs-agent/agent/app/mocks"
	"github.com/aws/amazon-ecs-agent/agent/app/oswrapper/mocks"
	"github.com/aws/amazon-ecs-agent/agent/config"
	"github.com/aws/amazon-ecs-agent/agent/ec2"
	"github.com/aws/amazon-ecs-agent/agent/ec2/mocks"
	"github.com/aws/amazon-ecs-agent/agent/ecs_client/model/ecs"
	"github.com/aws/amazon-ecs-agent/agent/ecscni"
	"github.com/aws/amazon-ecs-agent/agent/ecscni/mocks"
	"github.com/aws/amazon-ecs-agent/agent/engine"
	"github.com/aws/amazon-ecs-agent/agent/engine/dockerstate/mocks"
	"github.com/aws/amazon-ecs-agent/agent/eni/pause"
	"github.com/aws/amazon-ecs-agent/agent/eni/pause/mocks"
	"github.com/aws/amazon-ecs-agent/agent/eventstream"
	"github.com/aws/amazon-ecs-agent/agent/resources/mock_resources"
	"github.com/aws/amazon-ecs-agent/agent/sighandlers/exitcodes"
	"github.com/aws/amazon-ecs-agent/agent/statemanager"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
)

const (
	mac      = "01:23:45:67:89:ab"
	vpcID    = "vpc-1234"
	subnetID = "subnet-1234"
)

func TestDoStartHappyPath(t *testing.T) {
	ctrl, credentialsManager, state, imageManager, client,
		dockerClient, _, _ := setup(t)
	defer ctrl.Finish()

	mockCredentialsProvider := app_mocks.NewMockProvider(ctrl)
	var discoverEndpointsInvoked sync.WaitGroup
	discoverEndpointsInvoked.Add(2)
	containerChangeEvents := make(chan engine.DockerContainerChangeEvent)

	// These calls are expected to happen, but cannot be ordered as they are
	// invoked via go routines, which will lead to occasional test failues
	dockerClient.EXPECT().Version().AnyTimes()
	imageManager.EXPECT().StartImageCleanupProcess(gomock.Any()).MaxTimes(1)
	mockCredentialsProvider.EXPECT().IsExpired().Return(false).AnyTimes()
	dockerClient.EXPECT().ListContainers(gomock.Any(), gomock.Any()).Return(
		engine.ListContainersResponse{}).AnyTimes()
	client.EXPECT().DiscoverPollEndpoint(gomock.Any()).Do(func(x interface{}) {
		// Ensures that the test waits until acs session has bee started
		discoverEndpointsInvoked.Done()
	}).Return("poll-endpoint", nil)
	client.EXPECT().DiscoverPollEndpoint(gomock.Any()).Return("acs-endpoint", nil).AnyTimes()
	client.EXPECT().DiscoverTelemetryEndpoint(gomock.Any()).Do(func(x interface{}) {
		// Ensures that the test waits until telemetry session has bee started
		discoverEndpointsInvoked.Done()
	}).Return("telemetry-endpoint", nil)
	client.EXPECT().DiscoverTelemetryEndpoint(gomock.Any()).Return(
		"tele-endpoint", nil).AnyTimes()

	gomock.InOrder(
		mockCredentialsProvider.EXPECT().Retrieve().Return(credentials.Value{}, nil),
		dockerClient.EXPECT().SupportedVersions().Return(nil),
		dockerClient.EXPECT().KnownVersions().Return(nil),
		client.EXPECT().RegisterContainerInstance(gomock.Any(), gomock.Any()).Return("arn", nil),
		imageManager.EXPECT().SetSaver(gomock.Any()),
		dockerClient.EXPECT().ContainerEvents(gomock.Any()).Return(containerChangeEvents, nil),
		state.EXPECT().AllImageStates().Return(nil),
		state.EXPECT().AllTasks().Return(nil),
	)

	cfg := getTestConfig()
	ctx, cancel := context.WithCancel(context.TODO())
	// Cancel the context to cancel async routines
	defer cancel()
	agent := &ecsAgent{
		ctx:                ctx,
		cfg:                &cfg,
		credentialProvider: credentials.NewCredentials(mockCredentialsProvider),
		dockerClient:       dockerClient,
		terminationHandler: func(saver statemanager.Saver, taskEngine engine.TaskEngine) {},
	}

	go agent.doStart(eventstream.NewEventStream("events", ctx),
		credentialsManager, state, imageManager, client)

	// Wait for both DiscoverPollEndpointInput and DiscoverTelemetryEndpoint to be
	// invoked. These are used as proxies to indicate that acs and tcs handlers'
	// NewSession call has been invoked
	discoverEndpointsInvoked.Wait()
}

func TestDoStartTaskENIHappyPath(t *testing.T) {
	ctrl, credentialsManager, state, imageManager, client,
		dockerClient, _, _ := setup(t)
	defer ctrl.Finish()

	cniCapabilities := []string{ecscni.CapabilityAWSVPCNetworkingMode}
	containerChangeEvents := make(chan engine.DockerContainerChangeEvent)

	cniClient := mock_ecscni.NewMockCNIClient(ctrl)
	mockCredentialsProvider := app_mocks.NewMockProvider(ctrl)
	mockPauseLoader := mock_pause.NewMockLoader(ctrl)
	mockOS := mock_oswrapper.NewMockOS(ctrl)
	mockMetadata := mock_ec2.NewMockEC2MetadataClient(ctrl)

	var discoverEndpointsInvoked sync.WaitGroup
	discoverEndpointsInvoked.Add(2)

	// These calls are expected to happen, but cannot be ordered as they are
	// invoked via go routines, which will lead to occasional test failues
	mockCredentialsProvider.EXPECT().IsExpired().Return(false).AnyTimes()
	dockerClient.EXPECT().Version().AnyTimes()
	dockerClient.EXPECT().ListContainers(gomock.Any(), gomock.Any()).Return(
		engine.ListContainersResponse{}).AnyTimes()
	imageManager.EXPECT().StartImageCleanupProcess(gomock.Any()).MaxTimes(1)
	client.EXPECT().DiscoverPollEndpoint(gomock.Any()).Do(func(x interface{}) {
		// Ensures that the test waits until acs session has bee started
		discoverEndpointsInvoked.Done()
	}).Return("poll-endpoint", nil)
	client.EXPECT().DiscoverPollEndpoint(gomock.Any()).Return("acs-endpoint", nil).AnyTimes()
	client.EXPECT().DiscoverTelemetryEndpoint(gomock.Any()).Do(func(x interface{}) {
		// Ensures that the test waits until telemetry session has bee started
		discoverEndpointsInvoked.Done()
	}).Return("telemetry-endpoint", nil)
	client.EXPECT().DiscoverTelemetryEndpoint(gomock.Any()).Return(
		"tele-endpoint", nil).AnyTimes()

	gomock.InOrder(
		mockOS.EXPECT().Getpid().Return(10),
		mockMetadata.EXPECT().PrimaryENIMAC().Return(mac, nil),
		mockMetadata.EXPECT().VPCID(mac).Return(vpcID, nil),
		mockMetadata.EXPECT().SubnetID(mac).Return(subnetID, nil),
		cniClient.EXPECT().Capabilities(ecscni.ECSENIPluginName).Return(cniCapabilities, nil),
		cniClient.EXPECT().Capabilities(ecscni.ECSBridgePluginName).Return(cniCapabilities, nil),
		cniClient.EXPECT().Capabilities(ecscni.ECSIPAMPluginName).Return(cniCapabilities, nil),
		mockPauseLoader.EXPECT().LoadImage(gomock.Any(), gomock.Any()).Return(nil, nil),
		state.EXPECT().ENIByMac(gomock.Any()).Return(nil, false).AnyTimes(),
		mockCredentialsProvider.EXPECT().Retrieve().Return(credentials.Value{}, nil),
		dockerClient.EXPECT().SupportedVersions().Return(nil),
		dockerClient.EXPECT().KnownVersions().Return(nil),
		cniClient.EXPECT().Version(ecscni.ECSENIPluginName).Return("v1", nil),
		client.EXPECT().RegisterContainerInstance(gomock.Any(), gomock.Any()).Do(
			func(x interface{}, attributes []*ecs.Attribute) {
				vpcFound := false
				subnetFound := false
				for _, attribute := range attributes {
					if aws.StringValue(attribute.Name) == vpcIDAttributeName &&
						aws.StringValue(attribute.Value) == vpcID {
						vpcFound = true
					}
					if aws.StringValue(attribute.Name) == subnetIDAttributeName &&
						aws.StringValue(attribute.Value) == subnetID {
						subnetFound = true
					}
				}
				assert.True(t, vpcFound)
				assert.True(t, subnetFound)
			}).Return("arn", nil),
		imageManager.EXPECT().SetSaver(gomock.Any()),
		dockerClient.EXPECT().ContainerEvents(gomock.Any()).Return(containerChangeEvents, nil),
		state.EXPECT().AllImageStates().Return(nil),
		state.EXPECT().AllTasks().Return(nil),
	)

	cfg := getTestConfig()
	cfg.TaskENIEnabled = true
	ctx, cancel := context.WithCancel(context.TODO())
	// Cancel the context to cancel async routines
	defer cancel()
	agent := &ecsAgent{
		ctx:                ctx,
		cfg:                &cfg,
		credentialProvider: credentials.NewCredentials(mockCredentialsProvider),
		dockerClient:       dockerClient,
		pauseLoader:        mockPauseLoader,
		cniClient:          cniClient,
		os:                 mockOS,
		ec2MetadataClient:  mockMetadata,
		terminationHandler: func(saver statemanager.Saver, taskEngine engine.TaskEngine) {},
	}

	go agent.doStart(eventstream.NewEventStream("events", ctx),
		credentialsManager, state, imageManager, client)

	// Wait for both DiscoverPollEndpointInput and DiscoverTelemetryEndpoint to be
	// invoked. These are used as proxies to indicate that acs and tcs handlers'
	// NewSession call has been invoked
	discoverEndpointsInvoked.Wait()
}

func TestSetVPCSubnetHappyPath(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockMetadata := mock_ec2.NewMockEC2MetadataClient(ctrl)
	gomock.InOrder(
		mockMetadata.EXPECT().PrimaryENIMAC().Return(mac, nil),
		mockMetadata.EXPECT().VPCID(mac).Return(vpcID, nil),
		mockMetadata.EXPECT().SubnetID(mac).Return(subnetID, nil),
	)

	agent := &ecsAgent{ec2MetadataClient: mockMetadata}
	err, ok := agent.setVPCSubnet()
	assert.NoError(t, err)
	assert.False(t, ok)
	assert.Equal(t, vpcID, agent.vpc)
	assert.Equal(t, subnetID, agent.subnet)
}

func TestSetVPCSubnetClassicEC2(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockMetadata := mock_ec2.NewMockEC2MetadataClient(ctrl)
	gomock.InOrder(
		mockMetadata.EXPECT().PrimaryENIMAC().Return(mac, nil),
		mockMetadata.EXPECT().VPCID(mac).Return("", ec2.NewMetadataError(http.StatusNotFound)),
	)
	agent := &ecsAgent{ec2MetadataClient: mockMetadata}
	err, ok := agent.setVPCSubnet()
	assert.Error(t, err)
	assert.Equal(t, instanceNotLaunchedInVPCError, err)
	assert.False(t, ok)
	assert.Equal(t, "", agent.vpc)
	assert.Equal(t, "", agent.subnet)
}

func TestSetVPCSubnetPrimaryENIMACError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockMetadata := mock_ec2.NewMockEC2MetadataClient(ctrl)
	mockMetadata.EXPECT().PrimaryENIMAC().Return("", errors.New("error"))
	agent := &ecsAgent{ec2MetadataClient: mockMetadata}
	err, ok := agent.setVPCSubnet()
	assert.Error(t, err)
	assert.False(t, ok)
	assert.Equal(t, "", agent.vpc)
	assert.Equal(t, "", agent.subnet)
}

func TestSetVPCSubnetVPCIDError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockMetadata := mock_ec2.NewMockEC2MetadataClient(ctrl)
	gomock.InOrder(
		mockMetadata.EXPECT().PrimaryENIMAC().Return(mac, nil),
		mockMetadata.EXPECT().VPCID(mac).Return("", errors.New("error")),
	)
	agent := &ecsAgent{ec2MetadataClient: mockMetadata}
	err, ok := agent.setVPCSubnet()
	assert.Error(t, err)
	assert.True(t, ok)
	assert.Equal(t, "", agent.vpc)
	assert.Equal(t, "", agent.subnet)
}

func TestSetVPCSubnetSubnetIDError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockMetadata := mock_ec2.NewMockEC2MetadataClient(ctrl)
	gomock.InOrder(
		mockMetadata.EXPECT().PrimaryENIMAC().Return(mac, nil),
		mockMetadata.EXPECT().VPCID(mac).Return(vpcID, nil),
		mockMetadata.EXPECT().SubnetID(mac).Return("", errors.New("error")),
	)
	agent := &ecsAgent{ec2MetadataClient: mockMetadata}
	err, ok := agent.setVPCSubnet()
	assert.Error(t, err)
	assert.False(t, ok)
	assert.Equal(t, "", agent.vpc)
	assert.Equal(t, "", agent.subnet)
}

func TestQueryCNIPluginsCapabilitiesHappyPath(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	cniCapabilities := []string{ecscni.CapabilityAWSVPCNetworkingMode}
	cniClient := mock_ecscni.NewMockCNIClient(ctrl)
	gomock.InOrder(
		cniClient.EXPECT().Capabilities(ecscni.ECSENIPluginName).Return(cniCapabilities, nil),
		cniClient.EXPECT().Capabilities(ecscni.ECSBridgePluginName).Return(cniCapabilities, nil),
		cniClient.EXPECT().Capabilities(ecscni.ECSIPAMPluginName).Return(cniCapabilities, nil),
	)
	agent := &ecsAgent{
		cniClient: cniClient,
	}
	assert.NoError(t, agent.verifyCNIPluginsCapabilities())
}

func TestQueryCNIPluginsCapabilitiesEmptyCapabilityListFromPlugin(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	cniClient := mock_ecscni.NewMockCNIClient(ctrl)
	cniClient.EXPECT().Capabilities(ecscni.ECSENIPluginName).Return([]string{}, nil)

	agent := &ecsAgent{
		cniClient: cniClient,
	}

	assert.Error(t, agent.verifyCNIPluginsCapabilities())
}

func TestQueryCNIPluginsCapabilitiesErrorGettingCapabilitiesFromPlugin(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	cniClient := mock_ecscni.NewMockCNIClient(ctrl)
	cniClient.EXPECT().Capabilities(ecscni.ECSENIPluginName).Return(nil, errors.New("error"))

	agent := &ecsAgent{
		cniClient: cniClient,
	}

	assert.Error(t, agent.verifyCNIPluginsCapabilities())
}

func setupMocksForInitializeTaskENIDependencies(t *testing.T) (*gomock.Controller,
	*mock_dockerstate.MockTaskEngineState,
	*engine.MockTaskEngine,
	*mock_oswrapper.MockOS) {
	ctrl := gomock.NewController(t)

	return ctrl,
		mock_dockerstate.NewMockTaskEngineState(ctrl),
		engine.NewMockTaskEngine(ctrl),
		mock_oswrapper.NewMockOS(ctrl)
}

func TestInitializeTaskENIDependenciesNoInit(t *testing.T) {
	ctrl, state, taskEngine, mockOS := setupMocksForInitializeTaskENIDependencies(t)
	defer ctrl.Finish()

	mockOS.EXPECT().Getpid().Return(1)
	agent := &ecsAgent{
		os: mockOS,
	}

	err, ok := agent.initializeTaskENIDependencies(state, taskEngine)
	assert.Error(t, err)
	assert.True(t, ok)
}

func TestInitializeTaskENIDependenciesSetVPCSubnetError(t *testing.T) {
	ctrl, state, taskEngine, mockOS := setupMocksForInitializeTaskENIDependencies(t)
	defer ctrl.Finish()

	mockMetadata := mock_ec2.NewMockEC2MetadataClient(ctrl)
	gomock.InOrder(
		mockOS.EXPECT().Getpid().Return(10),
		mockMetadata.EXPECT().PrimaryENIMAC().Return("", errors.New("error")),
	)
	agent := &ecsAgent{
		os:                mockOS,
		ec2MetadataClient: mockMetadata,
	}
	err, ok := agent.initializeTaskENIDependencies(state, taskEngine)
	assert.Error(t, err)
	assert.False(t, ok)
}

func TestInitializeTaskENIDependenciesQueryCNICapabilitiesError(t *testing.T) {
	ctrl, state, taskEngine, mockOS := setupMocksForInitializeTaskENIDependencies(t)
	defer ctrl.Finish()

	mockMetadata := mock_ec2.NewMockEC2MetadataClient(ctrl)
	cniClient := mock_ecscni.NewMockCNIClient(ctrl)
	gomock.InOrder(
		mockOS.EXPECT().Getpid().Return(10),
		mockMetadata.EXPECT().PrimaryENIMAC().Return(mac, nil),
		mockMetadata.EXPECT().VPCID(mac).Return(vpcID, nil),
		mockMetadata.EXPECT().SubnetID(mac).Return(subnetID, nil),
		cniClient.EXPECT().Capabilities(ecscni.ECSENIPluginName).Return([]string{}, nil),
	)
	agent := &ecsAgent{
		os:                mockOS,
		ec2MetadataClient: mockMetadata,
		cniClient:         cniClient,
	}
	err, ok := agent.initializeTaskENIDependencies(state, taskEngine)
	assert.Error(t, err)
	assert.True(t, ok)
}

func TestInitializeTaskENIDependenciesPauseLoaderError(t *testing.T) {
	errorsToIsTerminal := map[error]bool{
		errors.New("error"):                                    false,
		pause.NewNoSuchFileError(errors.New("error")):          true,
		pause.NewUnsupportedPlatformError(errors.New("error")): true,
	}
	for loadErr, expectedIsTerminal := range errorsToIsTerminal {
		errType := reflect.TypeOf(loadErr)
		t.Run(fmt.Sprintf("error type %s->expected exit code %t", errType, expectedIsTerminal), func(t *testing.T) {
			ctrl, state, taskEngine, mockOS := setupMocksForInitializeTaskENIDependencies(t)
			defer ctrl.Finish()

			mockMetadata := mock_ec2.NewMockEC2MetadataClient(ctrl)
			cniClient := mock_ecscni.NewMockCNIClient(ctrl)
			mockPauseLoader := mock_pause.NewMockLoader(ctrl)
			cniCapabilities := []string{ecscni.CapabilityAWSVPCNetworkingMode}
			gomock.InOrder(
				mockOS.EXPECT().Getpid().Return(10),
				mockMetadata.EXPECT().PrimaryENIMAC().Return(mac, nil),
				mockMetadata.EXPECT().VPCID(mac).Return(vpcID, nil),
				mockMetadata.EXPECT().SubnetID(mac).Return(subnetID, nil),
				cniClient.EXPECT().Capabilities(ecscni.ECSENIPluginName).Return(cniCapabilities, nil),
				cniClient.EXPECT().Capabilities(ecscni.ECSBridgePluginName).Return(cniCapabilities, nil),
				cniClient.EXPECT().Capabilities(ecscni.ECSIPAMPluginName).Return(cniCapabilities, nil),
				mockPauseLoader.EXPECT().LoadImage(gomock.Any(), gomock.Any()).Return(nil, loadErr),
			)
			cfg := getTestConfig()
			agent := &ecsAgent{
				os:                mockOS,
				ec2MetadataClient: mockMetadata,
				cniClient:         cniClient,
				pauseLoader:       mockPauseLoader,
				cfg:               &cfg,
			}
			err, ok := agent.initializeTaskENIDependencies(state, taskEngine)
			assert.Error(t, err)
			assert.Equal(t, expectedIsTerminal, ok)
		})
	}
}

// TODO: At some point in the future, enisetup.New() will be refactored to be
// platform independent and we would be able to wrap it in a factory interface
// so that we can mock the factory and test the initialization code path for
// cases where udev monitor initialization fails as well

func TestDoStartCgroupInitHappyPath(t *testing.T) {
	ctrl, credentialsManager, state, imageManager, client,
		dockerClient, _, _ := setup(t)
	defer ctrl.Finish()
	mockCredentialsProvider := app_mocks.NewMockProvider(ctrl)
	mockResource := mock_resources.NewMockResource(ctrl)
	var discoverEndpointsInvoked sync.WaitGroup
	discoverEndpointsInvoked.Add(2)
	containerChangeEvents := make(chan engine.DockerContainerChangeEvent)

	dockerClient.EXPECT().Version().AnyTimes()
	imageManager.EXPECT().StartImageCleanupProcess(gomock.Any()).MaxTimes(1)
	mockCredentialsProvider.EXPECT().IsExpired().Return(false).AnyTimes()

	gomock.InOrder(
		mockResource.EXPECT().Init().Return(nil),
		mockCredentialsProvider.EXPECT().Retrieve().Return(credentials.Value{}, nil),
		dockerClient.EXPECT().SupportedVersions().Return(nil),
		dockerClient.EXPECT().KnownVersions().Return(nil),
		client.EXPECT().RegisterContainerInstance(gomock.Any(), gomock.Any()).Return("arn", nil),
		imageManager.EXPECT().SetSaver(gomock.Any()),
		dockerClient.EXPECT().ContainerEvents(gomock.Any()).Return(containerChangeEvents, nil),
		state.EXPECT().AllImageStates().Return(nil),
		state.EXPECT().AllTasks().Return(nil),
		client.EXPECT().DiscoverPollEndpoint(gomock.Any()).Do(func(x interface{}) {
			// Ensures that the test waits until acs session has bee started
			discoverEndpointsInvoked.Done()
		}).Return("poll-endpoint", nil),
		client.EXPECT().DiscoverPollEndpoint(gomock.Any()).Return("acs-endpoint", nil).AnyTimes(),
		client.EXPECT().DiscoverTelemetryEndpoint(gomock.Any()).Do(func(x interface{}) {
			// Ensures that the test waits until telemetry session has bee started
			discoverEndpointsInvoked.Done()
		}).Return("telemetry-endpoint", nil),
		client.EXPECT().DiscoverTelemetryEndpoint(gomock.Any()).Return(
			"tele-endpoint", nil).AnyTimes(),
		dockerClient.EXPECT().ListContainers(gomock.Any(), gomock.Any()).Return(
			engine.ListContainersResponse{}).AnyTimes(),
	)

	cfg := config.DefaultConfig()
	ctx, cancel := context.WithCancel(context.TODO())
	// Cancel the context to cancel async routines
	defer cancel()
	agent := &ecsAgent{
		ctx:                ctx,
		cfg:                &cfg,
		credentialProvider: credentials.NewCredentials(mockCredentialsProvider),
		dockerClient:       dockerClient,
		resource:           mockResource,
		terminationHandler: func(saver statemanager.Saver, taskEngine engine.TaskEngine) {},
	}

	go agent.doStart(eventstream.NewEventStream("events", ctx),
		credentialsManager, state, imageManager, client)

	// Wait for both DiscoverPollEndpointInput and DiscoverTelemetryEndpoint to be
	// invoked. These are used as proxies to indicate that acs and tcs handlers'
	// NewSession call has been invoked

	discoverEndpointsInvoked.Wait()
}

func TestDoStartCgroupInitErrorPath(t *testing.T) {
	ctrl, credentialsManager, state, imageManager, client,
		dockerClient, _, _ := setup(t)
	defer ctrl.Finish()

	mockCredentialsProvider := app_mocks.NewMockProvider(ctrl)
	mockResource := mock_resources.NewMockResource(ctrl)
	var discoverEndpointsInvoked sync.WaitGroup
	discoverEndpointsInvoked.Add(2)

	dockerClient.EXPECT().Version().AnyTimes()
	imageManager.EXPECT().StartImageCleanupProcess(gomock.Any()).MaxTimes(1)
	mockCredentialsProvider.EXPECT().IsExpired().Return(false).AnyTimes()

	mockResource.EXPECT().Init().Return(errors.New("cgroup init error"))

	cfg := getTestConfig()
	cfg.TaskCPUMemLimit = config.ExplicitlyEnabled

	ctx, cancel := context.WithCancel(context.TODO())
	// Cancel the context to cancel async routines
	defer cancel()
	agent := &ecsAgent{
		ctx:                ctx,
		cfg:                &cfg,
		credentialProvider: credentials.NewCredentials(mockCredentialsProvider),
		dockerClient:       dockerClient,
		resource:           mockResource,
		terminationHandler: func(saver statemanager.Saver, taskEngine engine.TaskEngine) {},
	}

	status := agent.doStart(eventstream.NewEventStream("events", ctx),
		credentialsManager, state, imageManager, client)

	assert.Equal(t, exitcodes.ExitTerminal, status)
}
