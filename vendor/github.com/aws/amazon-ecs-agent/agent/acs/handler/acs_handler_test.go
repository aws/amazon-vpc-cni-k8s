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
package handler

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"runtime"
	"runtime/pprof"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/context"

	"github.com/aws/amazon-ecs-agent/agent/api"
	"github.com/aws/amazon-ecs-agent/agent/api/mocks"
	"github.com/aws/amazon-ecs-agent/agent/config"
	rolecredentials "github.com/aws/amazon-ecs-agent/agent/credentials"
	"github.com/aws/amazon-ecs-agent/agent/credentials/mocks"
	"github.com/aws/amazon-ecs-agent/agent/engine"
	"github.com/aws/amazon-ecs-agent/agent/engine/dockerstate"
	"github.com/aws/amazon-ecs-agent/agent/eventhandler"
	"github.com/aws/amazon-ecs-agent/agent/eventstream"
	"github.com/aws/amazon-ecs-agent/agent/statemanager"
	"github.com/aws/amazon-ecs-agent/agent/utils"
	"github.com/aws/amazon-ecs-agent/agent/utils/mocks"
	"github.com/aws/amazon-ecs-agent/agent/version"
	"github.com/aws/amazon-ecs-agent/agent/wsclient"
	"github.com/aws/amazon-ecs-agent/agent/wsclient/mock"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"

	"github.com/golang/mock/gomock"
)

const (
	samplePayloadMessage = `
{
  "type": "PayloadMessage",
  "message": {
    "messageId": "123",
    "tasks": [
      {
        "taskDefinitionAccountId": "123",
        "containers": [
          {
            "environment": {},
            "name": "name",
            "cpu": 1,
            "essential": true,
            "memory": 1,
            "portMappings": [],
            "overrides": "{}",
            "image": "i",
            "mountPoints": [],
            "volumesFrom": []
          }
        ],
        "elasticNetworkInterfaces":[{
                "attachmentArn": "eni_attach_arn",
                "ec2Id": "eni_id",
                "ipv4Addresses":[{
                    "primary": true,
                    "privateAddress": "ipv4"
                }],
                "ipv6Addresses": [{
                    "address": "ipv6"
                }],
                "macAddress": "mac"
        }],
        "roleCredentials": {
          "credentialsId": "credsId",
          "accessKeyId": "accessKeyId",
          "expiration": "2016-03-25T06:17:19.318+0000",
          "roleArn": "r1",
          "secretAccessKey": "secretAccessKey",
          "sessionToken": "token"
        },
        "version": "3",
        "volumes": [],
        "family": "f",
        "arn": "arn",
        "desiredStatus": "RUNNING"
      }
    ],
    "generatedAt": 1,
    "clusterArn": "1",
    "containerInstanceArn": "1",
    "seqNum": 1
  }
}
`
	sampleRefreshCredentialsMessage = `
{
  "type": "IAMRoleCredentialsMessage",
  "message": {
    "messageId": "123",
    "clusterArn": "default",
    "taskArn": "t1",
    "roleType": "TaskApplication",
    "roleCredentials": {
      "credentialsId": "credsId",
      "accessKeyId": "newakid",
      "expiration": "later",
      "roleArn": "r1",
      "secretAccessKey": "newskid",
      "sessionToken": "newstkn"
    }
  }
}
`
	acsURL = "http://endpoint.tld"
)

var testConfig = &config.Config{
	Cluster:            "someCluster",
	AcceptInsecureCert: true,
}

type mockSessionResources struct {
	client wsclient.ClientServer
}

func (m *mockSessionResources) createACSClient(url string, cfg *config.Config) wsclient.ClientServer {
	return m.client
}

func (m *mockSessionResources) connectedToACS() {
}

func (m *mockSessionResources) getSendCredentialsURLParameter() string {
	return "true"
}

// TestACSWSURL tests if the URL is constructed correctly when connecting to ACS
func TestACSWSURL(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	taskEngine := engine.NewMockTaskEngine(ctrl)

	taskEngine.EXPECT().Version().Return("Docker version result", nil)

	wsurl := acsWsURL(acsURL, "myCluster", "myContainerInstance", taskEngine, &mockSessionResources{})

	parsed, err := url.Parse(wsurl)
	if err != nil {
		t.Fatal("Should be able to parse url")
	}

	if parsed.Path != "/ws" {
		t.Fatal("Wrong path")
	}

	if parsed.Query().Get("clusterArn") != "myCluster" {
		t.Fatal("Wrong cluster")
	}
	if parsed.Query().Get("containerInstanceArn") != "myContainerInstance" {
		t.Fatal("Wrong cluster")
	}
	if parsed.Query().Get("agentVersion") != version.Version {
		t.Fatal("Wrong cluster")
	}
	if parsed.Query().Get("agentHash") != version.GitHashString() {
		t.Fatal("Wrong cluster")
	}
	if parsed.Query().Get("dockerVersion") != "DockerVersion: Docker version result" {
		t.Fatal("Wrong docker version")
	}
	if parsed.Query().Get(sendCredentialsURLParameterName) != "true" {
		t.Fatalf("Wrong value set for: %s", sendCredentialsURLParameterName)
	}
	if parsed.Query().Get("seqNum") != "1" {
		t.Fatal("Wrong seqNum")
	}
}

// TestHandlerReconnectsOnConnectErrors tests if handler reconnects retries
// to establish the session with ACS when ClientServer.Connect() returns errors
func TestHandlerReconnectsOnConnectErrors(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	taskEngine := engine.NewMockTaskEngine(ctrl)
	taskEngine.EXPECT().Version().Return("Docker: 1.5.0", nil).AnyTimes()

	ecsClient := mock_api.NewMockECSClient(ctrl)
	ecsClient.EXPECT().DiscoverPollEndpoint(gomock.Any()).Return(acsURL, nil).AnyTimes()

	stateManager := statemanager.NewNoopStateManager()
	ctx, cancel := context.WithCancel(context.Background())
	taskHandler := eventhandler.NewTaskHandler(ctx, stateManager, nil, nil)

	mockWsClient := mock_wsclient.NewMockClientServer(ctrl)
	mockWsClient.EXPECT().SetAnyRequestHandler(gomock.Any()).AnyTimes()
	mockWsClient.EXPECT().AddRequestHandler(gomock.Any()).AnyTimes()
	mockWsClient.EXPECT().Close().Return(nil).AnyTimes()
	gomock.InOrder(
		// Connect fails 10 times
		mockWsClient.EXPECT().Connect().Return(io.EOF).Times(10),
		// Cancel trying to connect to ACS on the 11th attempt
		// Failure to retry on Connect() errors should cause the
		// test  to time out as the context is never cancelled
		mockWsClient.EXPECT().Connect().Do(func() {
			cancel()
		}).Return(io.EOF),
	)
	acsSession := session{
		containerInstanceARN: "myArn",
		credentialsProvider:  credentials.AnonymousCredentials,
		agentConfig:          testConfig,
		taskEngine:           taskEngine,
		ecsClient:            ecsClient,
		stateManager:         stateManager,
		taskHandler:          taskHandler,
		backoff:              utils.NewSimpleBackoff(connectionBackoffMin, connectionBackoffMax, connectionBackoffJitter, connectionBackoffMultiplier),
		ctx:                  ctx,
		cancel:               cancel,
		resources:            &mockSessionResources{mockWsClient},
		_heartbeatTimeout:    20 * time.Millisecond,
		_heartbeatJitter:     10 * time.Millisecond,
	}
	go func() {
		acsSession.Start()
	}()

	// Wait for context to be cancelled
	select {
	case <-ctx.Done():
	}
}

// TestIsInactiveInstanceErrorReturnsTrueForInactiveInstance tests if the 'InactiveInstance'
// exception is identified correctly by the handler
func TestIsInactiveInstanceErrorReturnsTrueForInactiveInstance(t *testing.T) {
	assert.True(t, isInactiveInstanceError(fmt.Errorf("InactiveInstanceException: ")),
		"inactive instance exception message parsed incorrectly")
}

// TestIsInactiveInstanceErrorReturnsFalseForActiveInstance tests if non 'InactiveInstance'
// exceptions are identified correctly by the handler
func TestIsInactiveInstanceErrorReturnsFalseForActiveInstance(t *testing.T) {
	assert.False(t, isInactiveInstanceError(io.EOF),
		"inactive instance exception message parsed incorrectly")
}

// TestComputeReconnectDelayForInactiveInstance tests if the reconnect delay is computed
// correctly for an inactive instance
func TestComputeReconnectDelayForInactiveInstance(t *testing.T) {
	acsSession := session{_inactiveInstanceReconnectDelay: inactiveInstanceReconnectDelay}
	assert.Equal(t, inactiveInstanceReconnectDelay, acsSession.computeReconnectDelay(true),
		"Reconnect delay doesn't match expected value for inactive instance")
}

// TestComputeReconnectDelayForActiveInstance tests if the reconnect delay is computed
// correctly for an active instance
func TestComputeReconnectDelayForActiveInstance(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockBackoff := mock_utils.NewMockBackoff(ctrl)
	mockBackoff.EXPECT().Duration().Return(connectionBackoffMax)

	acsSession := session{backoff: mockBackoff}
	assert.Equal(t, connectionBackoffMax, acsSession.computeReconnectDelay(false),
		"Reconnect delay doesn't match expected value for active instance")
}

// TestWaitForDurationReturnsTrueWhenContextNotCancelled tests if the
// waitForDurationOrCancelledSession method behaves correctly when the session context
// is not cancelled
func TestWaitForDurationReturnsTrueWhenContextNotCancelled(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	acsSession := session{
		ctx:    ctx,
		cancel: cancel,
	}

	assert.True(t, acsSession.waitForDuration(time.Millisecond),
		"WaitForDuration should return true when uninterrupted")
}

// TestWaitForDurationReturnsFalseWhenContextCancelled tests if the
// waitForDurationOrCancelledSession method behaves correctly when the session contexnt
// is cancelled
func TestWaitForDurationReturnsFalseWhenContextCancelled(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ctx, cancel := context.WithCancel(context.Background())
	acsSession := session{
		ctx:    ctx,
		cancel: cancel,
	}
	cancel()

	assert.False(t, acsSession.waitForDuration(time.Millisecond),
		"WaitForDuration should return false when interrupted")
}

func TestShouldReconnectWithoutBackoffReturnsTrueForEOF(t *testing.T) {
	assert.True(t, shouldReconnectWithoutBackoff(io.EOF),
		"Reconnect without backoff should return true when connection is closed")
}

func TestShouldReconnectWithoutBackoffReturnsFalseForNonEOF(t *testing.T) {
	assert.False(t, shouldReconnectWithoutBackoff(fmt.Errorf("not EOF")),
		"Reconnect without backoff should return false for non io.EOF error")
}

// TestHandlerReconnectsWithoutBackoffOnEOFError tests if the session handler reconnects
// to ACS without any delay when the connection is closed with the io.EOF error
func TestHandlerReconnectsWithoutBackoffOnEOFError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	taskEngine := engine.NewMockTaskEngine(ctrl)
	taskEngine.EXPECT().Version().Return("Docker: 1.5.0", nil).AnyTimes()

	ecsClient := mock_api.NewMockECSClient(ctrl)
	ecsClient.EXPECT().DiscoverPollEndpoint(gomock.Any()).Return(acsURL, nil).AnyTimes()

	stateManager := statemanager.NewNoopStateManager()
	ctx, cancel := context.WithCancel(context.Background())
	taskHandler := eventhandler.NewTaskHandler(ctx, stateManager, nil, nil)

	deregisterInstanceEventStream := eventstream.NewEventStream("DeregisterContainerInstance", ctx)
	deregisterInstanceEventStream.StartListening()

	mockBackoff := mock_utils.NewMockBackoff(ctrl)
	mockWsClient := mock_wsclient.NewMockClientServer(ctrl)
	mockWsClient.EXPECT().SetAnyRequestHandler(gomock.Any()).AnyTimes()
	mockWsClient.EXPECT().AddRequestHandler(gomock.Any()).AnyTimes()
	mockWsClient.EXPECT().Close().Return(nil).AnyTimes()
	gomock.InOrder(
		mockWsClient.EXPECT().Connect().Return(io.EOF),
		// The backoff.Reset() method is expected to be invoked when the connection
		// is closed with io.EOF
		mockBackoff.EXPECT().Reset(),
		mockWsClient.EXPECT().Connect().Do(func() {
			// cancel the context on the 2nd connect attempt, which should stop
			// the test
			cancel()
		}).Return(io.EOF),
		mockBackoff.EXPECT().Reset().AnyTimes(),
	)
	acsSession := session{
		containerInstanceARN:            "myArn",
		credentialsProvider:             credentials.AnonymousCredentials,
		agentConfig:                     testConfig,
		taskEngine:                      taskEngine,
		ecsClient:                       ecsClient,
		deregisterInstanceEventStream:   deregisterInstanceEventStream,
		stateManager:                    stateManager,
		taskHandler:                     taskHandler,
		backoff:                         mockBackoff,
		ctx:                             ctx,
		cancel:                          cancel,
		resources:                       &mockSessionResources{mockWsClient},
		_heartbeatTimeout:               20 * time.Millisecond,
		_heartbeatJitter:                10 * time.Millisecond,
		_inactiveInstanceReconnectDelay: inactiveInstanceReconnectDelay,
	}
	go func() {
		acsSession.Start()
	}()

	// Wait for context to be cancelled
	select {
	case <-ctx.Done():
	}
}

// TestHandlerReconnectsWithoutBackoffOnEOFError tests if the session handler reconnects
// to ACS after a backoff duration when the connection is closed with non io.EOF error
func TestHandlerReconnectsWithBackoffOnNonEOFError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	taskEngine := engine.NewMockTaskEngine(ctrl)
	taskEngine.EXPECT().Version().Return("Docker: 1.5.0", nil).AnyTimes()

	ecsClient := mock_api.NewMockECSClient(ctrl)
	ecsClient.EXPECT().DiscoverPollEndpoint(gomock.Any()).Return(acsURL, nil).AnyTimes()

	stateManager := statemanager.NewNoopStateManager()
	ctx, cancel := context.WithCancel(context.Background())
	taskHandler := eventhandler.NewTaskHandler(ctx, stateManager, nil, nil)

	deregisterInstanceEventStream := eventstream.NewEventStream("DeregisterContainerInstance", ctx)
	deregisterInstanceEventStream.StartListening()

	mockBackoff := mock_utils.NewMockBackoff(ctrl)
	mockWsClient := mock_wsclient.NewMockClientServer(ctrl)
	mockWsClient.EXPECT().SetAnyRequestHandler(gomock.Any()).AnyTimes()
	mockWsClient.EXPECT().AddRequestHandler(gomock.Any()).AnyTimes()
	mockWsClient.EXPECT().Close().Return(nil).AnyTimes()
	gomock.InOrder(
		mockWsClient.EXPECT().Connect().Return(fmt.Errorf("not EOF")),
		// The backoff.Duration() method is expected to be invoked when
		// the connection is closed with a non-EOF error code to compute
		// the backoff. Also, no calls to backoff.Reset() are expected
		// in this code path.
		mockBackoff.EXPECT().Duration().Return(time.Millisecond),
		mockWsClient.EXPECT().Connect().Do(func() {
			cancel()
		}).Return(io.EOF),
		mockBackoff.EXPECT().Reset().AnyTimes(),
	)
	acsSession := session{
		containerInstanceARN:          "myArn",
		credentialsProvider:           credentials.AnonymousCredentials,
		agentConfig:                   testConfig,
		taskEngine:                    taskEngine,
		ecsClient:                     ecsClient,
		deregisterInstanceEventStream: deregisterInstanceEventStream,
		stateManager:                  stateManager,
		taskHandler:                   taskHandler,
		backoff:                       mockBackoff,
		ctx:                           ctx,
		cancel:                        cancel,
		resources:                     &mockSessionResources{mockWsClient},
		_heartbeatTimeout:             20 * time.Millisecond,
		_heartbeatJitter:              10 * time.Millisecond,
	}
	go func() {
		acsSession.Start()
	}()

	// Wait for context to be cancelled
	select {
	case <-ctx.Done():
	}
}

// TestHandlerGeneratesDeregisteredInstanceEvent tests if the session handler generates
// an event into the deregister instance event stream when the acs connection is closed
// with inactive instance error
func TestHandlerGeneratesDeregisteredInstanceEvent(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	taskEngine := engine.NewMockTaskEngine(ctrl)
	taskEngine.EXPECT().Version().Return("Docker: 1.5.0", nil).AnyTimes()

	ecsClient := mock_api.NewMockECSClient(ctrl)
	ecsClient.EXPECT().DiscoverPollEndpoint(gomock.Any()).Return(acsURL, nil).AnyTimes()

	stateManager := statemanager.NewNoopStateManager()
	ctx, cancel := context.WithCancel(context.Background())
	taskHandler := eventhandler.NewTaskHandler(ctx, stateManager, nil, nil)

	deregisterInstanceEventStream := eventstream.NewEventStream("DeregisterContainerInstance", ctx)

	// receiverFunc cancels the context when invoked. Any event on the deregister
	// instance even stream would trigger this.
	receiverFunc := func(...interface{}) error {
		cancel()
		return nil
	}
	err := deregisterInstanceEventStream.Subscribe("DeregisterContainerInstance", receiverFunc)
	assert.NoError(t, err, "Error adding deregister instance event stream subscriber")
	deregisterInstanceEventStream.StartListening()
	mockWsClient := mock_wsclient.NewMockClientServer(ctrl)
	mockWsClient.EXPECT().SetAnyRequestHandler(gomock.Any()).AnyTimes()
	mockWsClient.EXPECT().AddRequestHandler(gomock.Any()).AnyTimes()
	mockWsClient.EXPECT().Close().Return(nil).AnyTimes()
	mockWsClient.EXPECT().Connect().Return(fmt.Errorf("InactiveInstanceException:"))
	inactiveInstanceReconnectDelay := 200 * time.Millisecond
	acsSession := session{
		containerInstanceARN:            "myArn",
		credentialsProvider:             credentials.AnonymousCredentials,
		agentConfig:                     testConfig,
		taskEngine:                      taskEngine,
		ecsClient:                       ecsClient,
		deregisterInstanceEventStream:   deregisterInstanceEventStream,
		stateManager:                    stateManager,
		taskHandler:                     taskHandler,
		backoff:                         utils.NewSimpleBackoff(connectionBackoffMin, connectionBackoffMax, connectionBackoffJitter, connectionBackoffMultiplier),
		ctx:                             ctx,
		cancel:                          cancel,
		resources:                       &mockSessionResources{mockWsClient},
		_heartbeatTimeout:               20 * time.Millisecond,
		_heartbeatJitter:                10 * time.Millisecond,
		_inactiveInstanceReconnectDelay: inactiveInstanceReconnectDelay,
	}
	go func() {
		acsSession.Start()
	}()

	// Wait for context to be cancelled
	select {
	case <-ctx.Done():
	}
}

// TestHandlerReconnectDelayForInactiveInstanceError tests if the session handler applies
// the proper reconnect delay with ACS when ClientServer.Connect() returns the
// InstanceInactive error
func TestHandlerReconnectDelayForInactiveInstanceError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	taskEngine := engine.NewMockTaskEngine(ctrl)
	taskEngine.EXPECT().Version().Return("Docker: 1.5.0", nil).AnyTimes()

	ecsClient := mock_api.NewMockECSClient(ctrl)
	ecsClient.EXPECT().DiscoverPollEndpoint(gomock.Any()).Return(acsURL, nil).AnyTimes()

	stateManager := statemanager.NewNoopStateManager()
	ctx, cancel := context.WithCancel(context.Background())
	taskHandler := eventhandler.NewTaskHandler(ctx, stateManager, nil, nil)

	deregisterInstanceEventStream := eventstream.NewEventStream("DeregisterContainerInstance", ctx)
	deregisterInstanceEventStream.StartListening()

	mockWsClient := mock_wsclient.NewMockClientServer(ctrl)
	mockWsClient.EXPECT().SetAnyRequestHandler(gomock.Any()).AnyTimes()
	mockWsClient.EXPECT().AddRequestHandler(gomock.Any()).AnyTimes()
	mockWsClient.EXPECT().Close().Return(nil).AnyTimes()
	var firstConnectionAttemptTime time.Time
	inactiveInstanceReconnectDelay := 200 * time.Millisecond
	gomock.InOrder(
		mockWsClient.EXPECT().Connect().Do(func() {
			firstConnectionAttemptTime = time.Now()
		}).Return(fmt.Errorf("InactiveInstanceException:")),
		mockWsClient.EXPECT().Connect().Do(func() {
			reconnectDelay := time.Now().Sub(firstConnectionAttemptTime)
			reconnectDelayTime := time.Now()
			t.Logf("Delay between successive connections: %v", reconnectDelay)
			timeSubFuncSlopAllowed := 2 * time.Millisecond
			if reconnectDelay < inactiveInstanceReconnectDelay {
				// On windows platform, we found issue with time.Now().Sub(...) reporting 199.9989 even
				// after the code has already waited for time.NewTimer(200)ms.
				assert.WithinDuration(t, reconnectDelayTime, firstConnectionAttemptTime.Add(inactiveInstanceReconnectDelay), timeSubFuncSlopAllowed)
			}
			cancel()
		}).Return(io.EOF),
	)
	acsSession := session{
		containerInstanceARN:            "myArn",
		credentialsProvider:             credentials.AnonymousCredentials,
		agentConfig:                     testConfig,
		taskEngine:                      taskEngine,
		ecsClient:                       ecsClient,
		deregisterInstanceEventStream:   deregisterInstanceEventStream,
		stateManager:                    stateManager,
		taskHandler:                     taskHandler,
		backoff:                         utils.NewSimpleBackoff(connectionBackoffMin, connectionBackoffMax, connectionBackoffJitter, connectionBackoffMultiplier),
		ctx:                             ctx,
		cancel:                          cancel,
		resources:                       &mockSessionResources{mockWsClient},
		_heartbeatTimeout:               20 * time.Millisecond,
		_heartbeatJitter:                10 * time.Millisecond,
		_inactiveInstanceReconnectDelay: inactiveInstanceReconnectDelay,
	}
	go func() {
		acsSession.Start()
	}()

	// Wait for context to be cancelled
	select {
	case <-ctx.Done():
	}
}

// TestHandlerReconnectsOnServeErrors tests if the handler retries to
// to establish the session with ACS when ClientServer.Connect() returns errors
func TestHandlerReconnectsOnServeErrors(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	taskEngine := engine.NewMockTaskEngine(ctrl)
	taskEngine.EXPECT().Version().Return("Docker: 1.5.0", nil).AnyTimes()

	ecsClient := mock_api.NewMockECSClient(ctrl)
	ecsClient.EXPECT().DiscoverPollEndpoint(gomock.Any()).Return(acsURL, nil).AnyTimes()

	stateManager := statemanager.NewNoopStateManager()
	ctx, cancel := context.WithCancel(context.Background())
	taskHandler := eventhandler.NewTaskHandler(ctx, stateManager, nil, nil)

	mockWsClient := mock_wsclient.NewMockClientServer(ctrl)
	mockWsClient.EXPECT().SetAnyRequestHandler(gomock.Any()).AnyTimes()
	mockWsClient.EXPECT().AddRequestHandler(gomock.Any()).AnyTimes()
	mockWsClient.EXPECT().Connect().Return(nil).AnyTimes()
	mockWsClient.EXPECT().Close().Return(nil).AnyTimes()
	gomock.InOrder(
		// Serve fails 10 times
		mockWsClient.EXPECT().Serve().Return(io.EOF).Times(10),
		// Cancel trying to Serve ACS requests on the 11th attempt
		// Failure to retry on Serve() errors should cause the
		// test to time out as the context is never cancelled
		mockWsClient.EXPECT().Serve().Do(func() {
			cancel()
		}).Return(io.EOF),
	)

	acsSession := session{
		containerInstanceARN: "myArn",
		credentialsProvider:  credentials.AnonymousCredentials,
		agentConfig:          testConfig,
		taskEngine:           taskEngine,
		ecsClient:            ecsClient,
		stateManager:         stateManager,
		taskHandler:          taskHandler,
		backoff:              utils.NewSimpleBackoff(connectionBackoffMin, connectionBackoffMax, connectionBackoffJitter, connectionBackoffMultiplier),
		ctx:                  ctx,
		cancel:               cancel,
		resources:            &mockSessionResources{mockWsClient},
		_heartbeatTimeout:    20 * time.Millisecond,
		_heartbeatJitter:     10 * time.Millisecond,
	}
	go func() {
		acsSession.Start()
	}()

	// Wait for context to be cancelled
	select {
	case <-ctx.Done():
	}
}

// TestHandlerStopsWhenContextIsCancelled tests if the session's Start() method returns
// when session context is cancelled
func TestHandlerStopsWhenContextIsCancelled(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	taskEngine := engine.NewMockTaskEngine(ctrl)
	taskEngine.EXPECT().Version().Return("Docker: 1.5.0", nil).AnyTimes()

	ecsClient := mock_api.NewMockECSClient(ctrl)
	ecsClient.EXPECT().DiscoverPollEndpoint(gomock.Any()).Return(acsURL, nil).AnyTimes()

	stateManager := statemanager.NewNoopStateManager()
	ctx, cancel := context.WithCancel(context.Background())
	taskHandler := eventhandler.NewTaskHandler(ctx, stateManager, nil, nil)

	mockWsClient := mock_wsclient.NewMockClientServer(ctrl)
	mockWsClient.EXPECT().SetAnyRequestHandler(gomock.Any()).AnyTimes()
	mockWsClient.EXPECT().AddRequestHandler(gomock.Any()).AnyTimes()
	mockWsClient.EXPECT().Connect().Return(nil).AnyTimes()
	mockWsClient.EXPECT().Close().Return(nil).AnyTimes()
	gomock.InOrder(
		mockWsClient.EXPECT().Serve().Return(io.EOF),
		mockWsClient.EXPECT().Serve().Do(func() {
			cancel()
		}).Return(io.EOF),
	)
	acsSession := session{
		containerInstanceARN: "myArn",
		credentialsProvider:  credentials.AnonymousCredentials,
		agentConfig:          testConfig,
		taskEngine:           taskEngine,
		ecsClient:            ecsClient,
		stateManager:         stateManager,
		taskHandler:          taskHandler,
		backoff:              utils.NewSimpleBackoff(connectionBackoffMin, connectionBackoffMax, connectionBackoffJitter, connectionBackoffMultiplier),
		ctx:                  ctx,
		cancel:               cancel,
		resources:            &mockSessionResources{mockWsClient},
		_heartbeatTimeout:    20 * time.Millisecond,
		_heartbeatJitter:     10 * time.Millisecond,
	}

	// The session error channel would have an event when the Start() method returns
	// Cancelling the context should trigger this
	sessionError := make(chan error)
	go func() {
		sessionError <- acsSession.Start()
	}()
	<-sessionError
}

// TestHandlerReconnectsOnDiscoverPollEndpointError tests if handler retries
// to establish the session with ACS on DiscoverPollEndpoint errors
func TestHandlerReconnectsOnDiscoverPollEndpointError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	taskEngine := engine.NewMockTaskEngine(ctrl)
	taskEngine.EXPECT().Version().Return("Docker: 1.5.0", nil).AnyTimes()

	ecsClient := mock_api.NewMockECSClient(ctrl)
	stateManager := statemanager.NewNoopStateManager()
	ctx, cancel := context.WithCancel(context.Background())
	taskHandler := eventhandler.NewTaskHandler(ctx, stateManager, nil, nil)

	mockWsClient := mock_wsclient.NewMockClientServer(ctrl)
	mockWsClient.EXPECT().SetAnyRequestHandler(gomock.Any()).AnyTimes()
	mockWsClient.EXPECT().AddRequestHandler(gomock.Any()).AnyTimes()
	mockWsClient.EXPECT().Connect().Return(nil).AnyTimes()
	mockWsClient.EXPECT().Close().Return(nil).AnyTimes()
	mockWsClient.EXPECT().Serve().Do(func() {
		// Serve() cancels the context
		cancel()
	}).Return(io.EOF)

	gomock.InOrder(
		// DiscoverPollEndpoint returns an error on its first invocation
		ecsClient.EXPECT().DiscoverPollEndpoint(gomock.Any()).Return("", fmt.Errorf("oops")).Times(1),
		// Second invocation returns a success
		ecsClient.EXPECT().DiscoverPollEndpoint(gomock.Any()).Return(acsURL, nil).Times(1),
	)
	acsSession := session{
		containerInstanceARN: "myArn",
		credentialsProvider:  credentials.AnonymousCredentials,
		agentConfig:          testConfig,
		taskEngine:           taskEngine,
		ecsClient:            ecsClient,
		stateManager:         stateManager,
		taskHandler:          taskHandler,
		backoff:              utils.NewSimpleBackoff(connectionBackoffMin, connectionBackoffMax, connectionBackoffJitter, connectionBackoffMultiplier),
		ctx:                  ctx,
		cancel:               cancel,
		resources:            &mockSessionResources{mockWsClient},
		_heartbeatTimeout:    20 * time.Millisecond,
		_heartbeatJitter:     10 * time.Millisecond,
	}
	go func() {
		acsSession.Start()
	}()
	start := time.Now()

	// Wait for context to be cancelled
	select {
	case <-ctx.Done():
	}

	// Measure the duration between retries
	timeSinceStart := time.Since(start)
	if timeSinceStart < connectionBackoffMin {
		t.Errorf("Duration since start is less than minimum threshold for backoff: %s", timeSinceStart.String())
	}

	// The upper limit here should really be connectionBackoffMin + (connectionBackoffMin * jitter)
	// But, it can be off by a few milliseconds to account for execution of other instructions
	// In any case, it should never be higher than 2*connectionBackoffMin
	if timeSinceStart > 2*connectionBackoffMin {
		t.Errorf("Duration since start is greater than maximum anticipated wait time: %v", timeSinceStart.String())
	}

}

// TestConnectionIsClosedOnIdle tests if the connection to ACS is closed
// when the channel is idle
func TestConnectionIsClosedOnIdle(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	taskEngine := engine.NewMockTaskEngine(ctrl)
	taskEngine.EXPECT().Version().Return("Docker: 1.5.0", nil).AnyTimes()

	ecsClient := mock_api.NewMockECSClient(ctrl)
	stateManager := statemanager.NewNoopStateManager()
	ctx, cancel := context.WithCancel(context.Background())
	taskHandler := eventhandler.NewTaskHandler(ctx, stateManager, nil, nil)
	defer cancel()

	wait := sync.WaitGroup{}
	wait.Add(1)
	mockWsClient := mock_wsclient.NewMockClientServer(ctrl)
	mockWsClient.EXPECT().SetAnyRequestHandler(gomock.Any()).Do(func(v interface{}) {}).AnyTimes()
	mockWsClient.EXPECT().AddRequestHandler(gomock.Any()).Do(func(v interface{}) {}).AnyTimes()
	mockWsClient.EXPECT().Connect().Return(nil)
	mockWsClient.EXPECT().Serve().Do(func() {
		wait.Done()
		// Pretend as if the maximum heartbeatTimeout duration has
		// been breached while Serving requests
		time.Sleep(30 * time.Millisecond)
	}).Return(io.EOF)

	connectionClosed := make(chan bool)
	mockWsClient.EXPECT().Close().Do(func() {
		wait.Wait()
		// Record connection closed
		connectionClosed <- true
	}).Return(nil)
	acsSession := session{
		containerInstanceARN: "myArn",
		credentialsProvider:  credentials.AnonymousCredentials,
		agentConfig:          testConfig,
		taskEngine:           taskEngine,
		ecsClient:            ecsClient,
		stateManager:         stateManager,
		taskHandler:          taskHandler,
		ctx:                  context.Background(),
		backoff:              utils.NewSimpleBackoff(connectionBackoffMin, connectionBackoffMax, connectionBackoffJitter, connectionBackoffMultiplier),
		resources:            &mockSessionResources{},
		_heartbeatTimeout:    20 * time.Millisecond,
		_heartbeatJitter:     10 * time.Millisecond,
	}
	go acsSession.startACSSession(mockWsClient)

	// Wait for connection to be closed. If the connection is not closed
	// due to inactivity, the test will time out
	<-connectionClosed
}

func TestHandlerDoesntLeakGoroutines(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	taskEngine := engine.NewMockTaskEngine(ctrl)
	ecsClient := mock_api.NewMockECSClient(ctrl)
	stateManager := statemanager.NewNoopStateManager()
	ctx, cancel := context.WithCancel(context.Background())
	taskHandler := eventhandler.NewTaskHandler(ctx, stateManager, nil, nil)

	closeWS := make(chan bool)
	server, serverIn, requests, errs, err := startMockAcsServer(t, closeWS)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			select {
			case <-requests:
			case <-errs:
			}
		}
	}()

	timesConnected := 0
	ecsClient.EXPECT().DiscoverPollEndpoint("myArn").Return(server.URL, nil).AnyTimes().Do(func(_ interface{}) {
		timesConnected++
	})
	taskEngine.EXPECT().Version().Return("Docker: 1.5.0", nil).AnyTimes()
	taskEngine.EXPECT().AddTask(gomock.Any()).AnyTimes()

	ended := make(chan bool, 1)
	go func() {
		acsSession := session{
			containerInstanceARN: "myArn",
			credentialsProvider:  credentials.AnonymousCredentials,
			agentConfig:          testConfig,
			taskEngine:           taskEngine,
			ecsClient:            ecsClient,
			stateManager:         stateManager,
			taskHandler:          taskHandler,
			ctx:                  ctx,
			_heartbeatTimeout:    1 * time.Second,
			backoff:              utils.NewSimpleBackoff(connectionBackoffMin, connectionBackoffMax, connectionBackoffJitter, connectionBackoffMultiplier),
			resources:            newSessionResources(credentials.AnonymousCredentials),
			credentialsManager:   rolecredentials.NewManager(),
		}
		acsSession.Start()
		ended <- true
	}()
	// Warm it up
	serverIn <- `{"type":"HeartbeatMessage","message":{"healthy":true}}`
	serverIn <- samplePayloadMessage

	beforeGoroutines := runtime.NumGoroutine()
	for i := 0; i < 100; i++ {
		serverIn <- `{"type":"HeartbeatMessage","message":{"healthy":true}}`
		serverIn <- samplePayloadMessage
		closeWS <- true
	}

	cancel()
	<-ended

	// The number of goroutines finishing in the MockACSServer will affect
	// the result unless we wait here.
	time.Sleep(10 * time.Millisecond)
	afterGoroutines := runtime.NumGoroutine()

	t.Logf("Goroutines after 1 and after %v acs messages: %v and %v", timesConnected, beforeGoroutines, afterGoroutines)

	if timesConnected < 50 {
		t.Fatal("Expected times connected to be a large number, was ", timesConnected)
	}
	if afterGoroutines > beforeGoroutines+5 {
		t.Error("Goroutine leak, oh no!")
		pprof.Lookup("goroutine").WriteTo(os.Stdout, 1)
	}

}

// TestStartSessionHandlesRefreshCredentialsMessages tests the agent restart
// scenario where the payload to refresh credentials is processed immediately on
// connection establishment with ACS
func TestStartSessionHandlesRefreshCredentialsMessages(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	taskEngine := engine.NewMockTaskEngine(ctrl)
	ecsClient := mock_api.NewMockECSClient(ctrl)
	stateManager := statemanager.NewNoopStateManager()
	ctx, cancel := context.WithCancel(context.Background())
	taskHandler := eventhandler.NewTaskHandler(ctx, stateManager, nil, nil)
	closeWS := make(chan bool)
	server, serverIn, requestsChan, errChan, err := startMockAcsServer(t, closeWS)
	if err != nil {
		t.Fatal(err)
	}
	defer close(serverIn)

	go func() {
		for {
			select {
			case <-requestsChan:
				// Cancel the context when we get the ack request
				cancel()
			}
		}
	}()

	// DiscoverPollEndpoint returns the URL for the server that we started
	ecsClient.EXPECT().DiscoverPollEndpoint("myArn").Return(server.URL, nil).Times(1)
	taskEngine.EXPECT().Version().Return("Docker: 1.5.0", nil).AnyTimes()

	credentialsManager := mock_credentials.NewMockManager(ctrl)

	ended := make(chan bool, 1)
	go func() {
		acsSession := NewSession(ctx,
			testConfig,
			nil,
			"myArn",
			credentials.AnonymousCredentials,
			ecsClient,
			dockerstate.NewTaskEngineState(),
			stateManager,
			taskEngine,
			credentialsManager,
			taskHandler,
		)
		acsSession.Start()
		// StartSession should never return unless the context is canceled
		ended <- true
	}()

	updatedCredentials := rolecredentials.TaskIAMRoleCredentials{}
	taskFromEngine := &api.Task{}
	credentialsIdInRefreshMessage := "credsId"
	// Ensure that credentials manager interface methods are invoked in the
	// correct order, with expected arguments
	gomock.InOrder(
		// Return a task from the engine for GetTaskByArn
		taskEngine.EXPECT().GetTaskByArn("t1").Return(taskFromEngine, true),
		// The last invocation of SetCredentials is to update
		// credentials when a refresh message is recieved by the handler
		credentialsManager.EXPECT().SetTaskCredentials(gomock.Any()).Do(func(creds rolecredentials.TaskIAMRoleCredentials) {
			updatedCredentials = creds
			// Validate parsed credentials after the update
			expectedCreds := rolecredentials.TaskIAMRoleCredentials{
				ARN: "t1",
				IAMRoleCredentials: rolecredentials.IAMRoleCredentials{
					RoleArn:         "r1",
					AccessKeyID:     "newakid",
					SecretAccessKey: "newskid",
					SessionToken:    "newstkn",
					Expiration:      "later",
					CredentialsID:   credentialsIdInRefreshMessage,
					RoleType:        "TaskApplication",
				},
			}
			if !reflect.DeepEqual(updatedCredentials, expectedCreds) {
				t.Errorf("Mismatch between expected and credentials expected: %v, added: %v", expectedCreds, updatedCredentials)
			}
		}).Return(nil),
	)
	serverIn <- sampleRefreshCredentialsMessage

	select {
	case err := <-errChan:
		t.Fatal("Error should not have been returned from server", err)
	case <-ctx.Done():
		// Context is canceled when requestsChan recieves an ack
	}

	// Validate that the correct credentialsId is set for the task
	credentialsIdFromTask := taskFromEngine.GetCredentialsID()
	if credentialsIdFromTask != credentialsIdInRefreshMessage {
		t.Errorf("Mismatch between expected and added credentials id for task, expected: %s, added: %s", credentialsIdInRefreshMessage, credentialsIdFromTask)
	}

	server.Close()
	// Cancel context should close the session
	<-ended
}

// TestACSSessionResourcesCorrectlySetsSendCredentials tests if acsSessionResources
// struct correctly sets 'sendCredentials'
func TestACSSessionResourcesCorrectlySetsSendCredentials(t *testing.T) {
	acsResources := newSessionResources(nil)
	// Validate that 'sendCredentials' is set to true on create
	sendCredentials := acsResources.getSendCredentialsURLParameter()
	if sendCredentials != "true" {
		t.Errorf("Mismatch in sendCredentials URL parameter value, expected: 'true', got: %s", sendCredentials)
	}
	// Simulate a successful connection to ACS
	acsResources.connectedToACS()
	// On successful connection to ACS, 'sendCredentials' should be set to false
	sendCredentials = acsResources.getSendCredentialsURLParameter()
	if sendCredentials != "false" {
		t.Errorf("Mismatch in sendCredentials URL parameter value, expected: 'false', got: %s", sendCredentials)
	}
}

// TestHandlerReconnectsCorrectlySetsSendCredentialsURLParameter tests if
// the 'sendCredentials' URL parameter is set correctly for successive
// invocations of startACSSession
func TestHandlerReconnectsCorrectlySetsSendCredentialsURLParameter(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	taskEngine := engine.NewMockTaskEngine(ctrl)
	ecsClient := mock_api.NewMockECSClient(ctrl)
	stateManager := statemanager.NewNoopStateManager()
	ctx, cancel := context.WithCancel(context.Background())
	taskHandler := eventhandler.NewTaskHandler(ctx, stateManager, nil, nil)

	mockWsClient := mock_wsclient.NewMockClientServer(ctrl)

	mockWsClient.EXPECT().SetAnyRequestHandler(gomock.Any()).AnyTimes()
	mockWsClient.EXPECT().AddRequestHandler(gomock.Any()).AnyTimes()
	mockWsClient.EXPECT().Close().Return(nil).AnyTimes()
	mockWsClient.EXPECT().Serve().Return(io.EOF).AnyTimes()
	resources := newSessionResources(credentials.AnonymousCredentials)
	gomock.InOrder(
		// When the websocket client connects to ACS for the first
		// time, 'sendCredentials' should be set to true
		mockWsClient.EXPECT().Connect().Do(func() {
			validateSendCredentialsInSession(t, resources, "true")
		}).Return(nil),
		// For all subsequent connections to ACS, 'sendCredentials'
		// should be set to false
		mockWsClient.EXPECT().Connect().Do(func() {
			validateSendCredentialsInSession(t, resources, "false")
		}).Return(nil).AnyTimes(),
	)

	acsSession := session{
		containerInstanceARN: "myArn",
		credentialsProvider:  credentials.AnonymousCredentials,
		agentConfig:          testConfig,
		taskEngine:           taskEngine,
		ecsClient:            ecsClient,
		stateManager:         stateManager,
		taskHandler:          taskHandler,
		ctx:                  ctx,
		resources:            resources,
		backoff:              utils.NewSimpleBackoff(connectionBackoffMin, connectionBackoffMax, connectionBackoffJitter, connectionBackoffMultiplier),
		_heartbeatTimeout:    20 * time.Millisecond,
		_heartbeatJitter:     10 * time.Millisecond,
	}
	go func() {
		for i := 0; i < 10; i++ {
			acsSession.startACSSession(mockWsClient)
		}
		cancel()
	}()

	// Wait for context to be cancelled
	select {
	case <-ctx.Done():
	}
}

// TODO: replace with gomock
func startMockAcsServer(t *testing.T, closeWS <-chan bool) (*httptest.Server, chan<- string, <-chan string, <-chan error, error) {
	serverChan := make(chan string, 1)
	requestsChan := make(chan string, 1)
	errChan := make(chan error, 1)

	upgrader := websocket.Upgrader{ReadBufferSize: 1024, WriteBufferSize: 1024}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)

		if err != nil {
			errChan <- err
		}

		go func() {
			_, msg, err := ws.ReadMessage()
			if err != nil {
				errChan <- err
			} else {
				requestsChan <- string(msg)
			}
		}()
		for {
			select {
			case str := <-serverChan:
				err := ws.WriteMessage(websocket.TextMessage, []byte(str))
				if err != nil {
					errChan <- err
				}

			case <-closeWS:
				ws.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				ws.Close()
				errChan <- io.EOF
				// Quit listening to serverChan if we've been closed
				return
			}

		}
	})

	server := httptest.NewTLSServer(handler)
	return server, serverChan, requestsChan, errChan, nil
}

// validateAddedTask validates fields in addedTask for expected values
// It returns an error if there's a mismatch
func validateAddedTask(expectedTask api.Task, addedTask api.Task) error {
	// The ecsacs.Task -> api.Task conversion initializes all fields in api.Task
	// with empty objects. So, we create a new object to compare with only those
	// fields that we are intrested in for comparison
	taskToCompareFromAdded := api.Task{
		Arn:                 addedTask.Arn,
		Family:              addedTask.Family,
		Version:             addedTask.Version,
		DesiredStatusUnsafe: addedTask.GetDesiredStatus(),
		StartSequenceNumber: addedTask.StartSequenceNumber,
	}

	if !reflect.DeepEqual(expectedTask, taskToCompareFromAdded) {
		return fmt.Errorf("Mismatch between added and expected task: expected: %v, added: %v", expectedTask, taskToCompareFromAdded)
	}

	return nil
}

// validateAddedContainer validates fields in addedContainer for expected values
// It returns an error if there's a mismatch
func validateAddedContainer(expectedContainer *api.Container, addedContainer *api.Container) error {
	// The ecsacs.Task -> api.Task conversion initializes all fields in api.Container
	// with empty objects. So, we create a new object to compare with only those
	// fields that we are intrested in for comparison
	containerToCompareFromAdded := &api.Container{
		Name:      addedContainer.Name,
		CPU:       addedContainer.CPU,
		Essential: addedContainer.Essential,
		Memory:    addedContainer.Memory,
		Image:     addedContainer.Image,
	}
	if !reflect.DeepEqual(expectedContainer, containerToCompareFromAdded) {
		return fmt.Errorf("Mismatch between added and expected container: expected: %v, added: %v", expectedContainer, containerToCompareFromAdded)
	}
	return nil
}

func validateSendCredentialsInSession(t *testing.T, state sessionState, expected string) {
	sendCredentials := state.getSendCredentialsURLParameter()
	if sendCredentials != expected {
		t.Errorf("Incorrect value set for sendCredentials, expected: %s, got: %s", expected, sendCredentials)
	}
}
