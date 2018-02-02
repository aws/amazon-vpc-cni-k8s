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

// Package handler deals with appropriately reacting to all ACS messages as well
// as maintaining the connection to ACS.
package handler

import (
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/context"

	acsclient "github.com/aws/amazon-ecs-agent/agent/acs/client"
	"github.com/aws/amazon-ecs-agent/agent/acs/model/ecsacs"
	"github.com/aws/amazon-ecs-agent/agent/acs/update_handler"
	"github.com/aws/amazon-ecs-agent/agent/api"
	"github.com/aws/amazon-ecs-agent/agent/config"
	rolecredentials "github.com/aws/amazon-ecs-agent/agent/credentials"
	"github.com/aws/amazon-ecs-agent/agent/engine"
	"github.com/aws/amazon-ecs-agent/agent/engine/dockerstate"
	"github.com/aws/amazon-ecs-agent/agent/eventhandler"
	"github.com/aws/amazon-ecs-agent/agent/eventstream"
	"github.com/aws/amazon-ecs-agent/agent/statemanager"
	"github.com/aws/amazon-ecs-agent/agent/utils"
	"github.com/aws/amazon-ecs-agent/agent/utils/ttime"
	"github.com/aws/amazon-ecs-agent/agent/version"
	"github.com/aws/amazon-ecs-agent/agent/wsclient"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/cihub/seelog"
)

const (
	// heartbeatTimeout is the maximum time to wait between heartbeats
	// without disconnecting
	heartbeatTimeout = 1 * time.Minute
	heartbeatJitter  = 1 * time.Minute
	// wsRWTimeout is the duration of read and write deadline for the
	// websocket connection
	wsRWTimeout = 2*heartbeatTimeout + heartbeatJitter

	inactiveInstanceReconnectDelay = 1 * time.Hour

	connectionBackoffMin        = 250 * time.Millisecond
	connectionBackoffMax        = 2 * time.Minute
	connectionBackoffJitter     = 0.2
	connectionBackoffMultiplier = 1.5
	// payloadMessageBufferSize is the maximum number of payload messages
	// to queue up without having handled previous ones.
	payloadMessageBufferSize = 10
	// sendCredentialsURLParameterName is the name of the URL parameter
	// in the ACS URL that is used to indicate if ACS should send
	// credentials for all tasks on establishing the connection
	sendCredentialsURLParameterName = "sendCredentials"
	inactiveInstanceExceptionPrefix = "InactiveInstanceException:"
)

// Session defines an interface for handler's long-lived connection with ACS.
type Session interface {
	Start() error
}

// session encapsulates all arguments needed by the handler to connect to ACS
// and to handle messages recieved by ACS. The Session.Start() method can be used
// to start processing messages from ACS.
type session struct {
	containerInstanceARN            string
	credentialsProvider             *credentials.Credentials
	agentConfig                     *config.Config
	deregisterInstanceEventStream   *eventstream.EventStream
	taskEngine                      engine.TaskEngine
	ecsClient                       api.ECSClient
	state                           dockerstate.TaskEngineState
	stateManager                    statemanager.StateManager
	credentialsManager              rolecredentials.Manager
	taskHandler                     *eventhandler.TaskHandler
	ctx                             context.Context
	cancel                          context.CancelFunc
	backoff                         utils.Backoff
	resources                       sessionResources
	_heartbeatTimeout               time.Duration
	_heartbeatJitter                time.Duration
	_inactiveInstanceReconnectDelay time.Duration
}

// sessionResources defines the resource creator interface for starting
// a session with ACS. This interface is intended to define methods
// that create resources used to establish the connection to ACS
// It is confined to just the createACSClient() method for now. It can be
// extended to include the acsWsURL() and newDisconnectionTimer() methods
// when needed
// The goal is to make it easier to test and inject dependencies
type sessionResources interface {
	// createACSClient creates a new websocket client
	createACSClient(url string, cfg *config.Config) wsclient.ClientServer
	sessionState
}

// acsSessionResources implements resource creator and session state interfaces
// to create resources needed to connect to ACS and to record session state
// for the same
type acsSessionResources struct {
	credentialsProvider *credentials.Credentials
	// sendCredentials is used to set the 'sendCredentials' URL parameter
	// used to connect to ACS
	// It is set to 'true' for the very first successful connection on
	// agent start. It is set to false for all successive connections
	sendCredentials bool
}

// sessionState defines state recorder interface for the
// session established with ACS. It can be used to record and
// retrieve data shared across multiple connections to ACS
type sessionState interface {
	// connectedToACS callback indicates that the client has
	// connected to ACS
	connectedToACS()
	// getSendCredentialsURLParameter retrieves the value for
	// the 'sendCredentials' URL parameter
	getSendCredentialsURLParameter() string
}

// NewSession creates a new Session object
func NewSession(ctx context.Context,
	config *config.Config,
	deregisterInstanceEventStream *eventstream.EventStream,
	containerInstanceArn string,
	credentialsProvider *credentials.Credentials,
	ecsClient api.ECSClient,
	taskEngineState dockerstate.TaskEngineState,
	stateManager statemanager.StateManager,
	taskEngine engine.TaskEngine,
	credentialsManager rolecredentials.Manager,
	taskHandler *eventhandler.TaskHandler) Session {
	resources := newSessionResources(credentialsProvider)
	backoff := utils.NewSimpleBackoff(connectionBackoffMin, connectionBackoffMax,
		connectionBackoffJitter, connectionBackoffMultiplier)
	derivedContext, cancel := context.WithCancel(ctx)

	return &session{
		agentConfig:                     config,
		deregisterInstanceEventStream:   deregisterInstanceEventStream,
		containerInstanceARN:            containerInstanceArn,
		credentialsProvider:             credentialsProvider,
		ecsClient:                       ecsClient,
		state:                           taskEngineState,
		stateManager:                    stateManager,
		taskEngine:                      taskEngine,
		credentialsManager:              credentialsManager,
		taskHandler:                     taskHandler,
		ctx:                             derivedContext,
		cancel:                          cancel,
		backoff:                         backoff,
		resources:                       resources,
		_heartbeatTimeout:               heartbeatTimeout,
		_heartbeatJitter:                heartbeatJitter,
		_inactiveInstanceReconnectDelay: inactiveInstanceReconnectDelay,
	}
}

// Start starts the session. It'll forever keep trying to connect to ACS unless
// the context is cancelled.
//
// If the context is cancelled, Start() would return with the error code returned
// by the context.
// If the instance is deregistered, Start() would emit an event to the
// deregister-instance event stream and sets the connection backoff time to 1 hour.
func (acsSession *session) Start() error {
	// connectToACS channel is used to inidcate the intent to connect to ACS
	// It's processed by the select loop to connect to ACS
	connectToACS := make(chan struct{})
	// This is required to trigger the first connection to ACS. Subsequent
	// connections are triggered by the handleACSError() method
	go func() {
		connectToACS <- struct{}{}
	}()
	for {
		select {
		case <-connectToACS:
			seelog.Debugf("Received connect to ACS message")
			// Start a session with ACS
			acsError := acsSession.startSessionOnce()
			// Session with ACS was stopped with some error, start processing the error
			isInactiveInstance := isInactiveInstanceError(acsError)
			if isInactiveInstance {
				// If the instance was deregistered, send an event to the event stream
				// for the same
				seelog.Debug("Container instance is deregistered, notifying listeners")
				err := acsSession.deregisterInstanceEventStream.WriteToEventStream(struct{}{})
				if err != nil {
					seelog.Debugf("Failed to write to deregister container instance event stream, err: %v", err)
				}
			}
			if shouldReconnectWithoutBackoff(acsError) {
				// If ACS closed the connection, there's no need to backoff,
				// reconnect immediately
				seelog.Info("ACS Websocket connection closed for a valid reason")
				acsSession.backoff.Reset()
				sendEmptyMessageOnChannel(connectToACS)
			} else {
				// Disconnected unexpectedly from ACS, compute backoff duration to
				// reconnect
				reconnectDelay := acsSession.computeReconnectDelay(isInactiveInstance)
				seelog.Infof("Reconnecting to ACS in: %s", reconnectDelay.String())
				waitComplete := acsSession.waitForDuration(reconnectDelay)
				if waitComplete {
					// If the context was not cancelled and we've waited for the
					// wait duration without any errors, send the message to the channel
					// to reconnect to ACS
					sendEmptyMessageOnChannel(connectToACS)
				} else {
					// Wait was interrupted. We expect the session to close as canelling
					// the session context is the only way to end up here. Print a message
					// to indicate the same
					seelog.Info("Interrupted waiting for reconnect delay to elapse; Expect session to close")
				}
			}
		case <-acsSession.ctx.Done():
			seelog.Debugf("ACS session context cancelled")
			return acsSession.ctx.Err()
		}

	}
}

// startSessionOnce creates a session with ACS and handles requests using the passed
// in arguments
func (acsSession *session) startSessionOnce() error {
	acsEndpoint, err := acsSession.ecsClient.DiscoverPollEndpoint(acsSession.containerInstanceARN)
	if err != nil {
		seelog.Errorf("Unable to discover poll endpoint, err: %v", err)
		return err
	}

	url := acsWsURL(acsEndpoint, acsSession.agentConfig.Cluster, acsSession.containerInstanceARN, acsSession.taskEngine, acsSession.resources)
	client := acsSession.resources.createACSClient(url, acsSession.agentConfig)
	defer client.Close()

	return acsSession.startACSSession(client)
}

// startACSSession starts a session with ACS. It adds request handlers for various
// kinds of messages expected from ACS. It returns on server disconnection or when
// the context is cancelled
func (acsSession *session) startACSSession(client wsclient.ClientServer) error {
	cfg := acsSession.agentConfig

	refreshCredsHandler := newRefreshCredentialsHandler(acsSession.ctx, cfg.Cluster, acsSession.containerInstanceARN,
		client, acsSession.credentialsManager, acsSession.taskEngine)
	defer refreshCredsHandler.clearAcks()
	refreshCredsHandler.start()
	defer refreshCredsHandler.stop()

	client.AddRequestHandler(refreshCredsHandler.handlerFunc())

	// Add handler to ack ENI attach message
	eniAttachHandler := newAttachENIHandler(
		acsSession.ctx,
		cfg.Cluster,
		acsSession.containerInstanceARN,
		client,
		acsSession.state,
		acsSession.stateManager,
	)
	eniAttachHandler.start()
	defer eniAttachHandler.stop()

	client.AddRequestHandler(eniAttachHandler.handlerFunc())

	// Add request handler for handling payload messages from ACS
	payloadHandler := newPayloadRequestHandler(
		acsSession.ctx,
		acsSession.taskEngine,
		acsSession.ecsClient,
		cfg.Cluster,
		acsSession.containerInstanceARN,
		client,
		acsSession.stateManager,
		refreshCredsHandler,
		acsSession.credentialsManager,
		acsSession.taskHandler)
	// Clear the acks channel on return because acks of messageids don't have any value across sessions
	defer payloadHandler.clearAcks()
	payloadHandler.start()
	defer payloadHandler.stop()

	client.AddRequestHandler(payloadHandler.handlerFunc())

	// Ignore heartbeat messages; anyMessageHandler gets 'em
	client.AddRequestHandler(func(*ecsacs.HeartbeatMessage) {})

	updater.AddAgentUpdateHandlers(client, cfg, acsSession.stateManager, acsSession.taskEngine)

	err := client.Connect()
	if err != nil {
		seelog.Errorf("Error connecting to ACS: %v", err)
		return err
	}
	seelog.Info("Connected to ACS endpoint")
	// Start inactivity timer for closing the connection
	timer := newDisconnectionTimer(client, acsSession.heartbeatTimeout(), acsSession.heartbeatJitter())
	// Any message from the server resets the disconnect timeout
	client.SetAnyRequestHandler(anyMessageHandler(timer, client))
	defer timer.Stop()

	acsSession.resources.connectedToACS()

	backoffResetTimer := time.AfterFunc(
		utils.AddJitter(acsSession.heartbeatTimeout(), acsSession.heartbeatJitter()), func() {
			// If we do not have an error connecting and remain connected for at
			// least 1 or so minutes, reset the backoff. This prevents disconnect
			// errors that only happen infrequently from damaging the
			// reconnectability as significantly.
			acsSession.backoff.Reset()
		})
	defer backoffResetTimer.Stop()

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- client.Serve()
	}()

	for {
		select {
		case <-acsSession.ctx.Done():
			// Stop receiving and sending messages from and to ACS when
			// the context received from the main function is canceled
			return acsSession.ctx.Err()
		case err := <-serveErr:
			// Stop receiving and sending messages from and to ACS when
			// client.Serve returns an error. This can happen when the
			// the connection is closed by ACS or the agent
			return err
		}
	}
}

func (acsSession *session) computeReconnectDelay(isInactiveInstance bool) time.Duration {
	if isInactiveInstance {
		return acsSession._inactiveInstanceReconnectDelay
	}

	return acsSession.backoff.Duration()
}

// waitForDuration waits for the specified duration of time. If the wait is interrupted,
// it returns a false value. Else, it returns true, indicating completion of wait time.
func (acsSession *session) waitForDuration(delay time.Duration) bool {
	reconnectTimer := time.NewTimer(delay)
	select {
	case <-reconnectTimer.C:
		return true
	case <-acsSession.ctx.Done():
		reconnectTimer.Stop()
		return false
	}
}

func (acsSession *session) heartbeatTimeout() time.Duration {
	return acsSession._heartbeatTimeout
}

func (acsSession *session) heartbeatJitter() time.Duration {
	return acsSession._heartbeatJitter
}

// createACSClient creates the ACS Client using the specified URL
func (acsResources *acsSessionResources) createACSClient(url string, cfg *config.Config) wsclient.ClientServer {
	return acsclient.New(url, cfg, acsResources.credentialsProvider, wsRWTimeout)
}

// connectedToACS records a successful connection to ACS
// It sets sendCredentials to false on such an event
func (acsResources *acsSessionResources) connectedToACS() {
	acsResources.sendCredentials = false
}

// getSendCredentialsURLParameter gets the value to be set for the
// 'sendCredentials' URL parameter
func (acsResources *acsSessionResources) getSendCredentialsURLParameter() string {
	return strconv.FormatBool(acsResources.sendCredentials)
}

func newSessionResources(credentialsProvider *credentials.Credentials) sessionResources {
	return &acsSessionResources{
		credentialsProvider: credentialsProvider,
		sendCredentials:     true,
	}
}

// acsWsURL returns the websocket url for ACS given the endpoint
func acsWsURL(endpoint, cluster, containerInstanceArn string, taskEngine engine.TaskEngine, acsSessionState sessionState) string {
	acsURL := endpoint
	if endpoint[len(endpoint)-1] != '/' {
		acsURL += "/"
	}
	acsURL += "ws"
	query := url.Values{}
	query.Set("clusterArn", cluster)
	query.Set("containerInstanceArn", containerInstanceArn)
	query.Set("agentHash", version.GitHashString())
	query.Set("agentVersion", version.Version)
	query.Set("seqNum", "1")
	if dockerVersion, err := taskEngine.Version(); err == nil {
		query.Set("dockerVersion", "DockerVersion: "+dockerVersion)
	}
	query.Set(sendCredentialsURLParameterName, acsSessionState.getSendCredentialsURLParameter())
	return acsURL + "?" + query.Encode()
}

// newDisconnectionTimer creates a new time object, with a callback to
// disconnect from ACS on inactivity
func newDisconnectionTimer(client wsclient.ClientServer, timeout time.Duration, jitter time.Duration) ttime.Timer {
	timer := time.AfterFunc(utils.AddJitter(timeout, jitter), func() {
		seelog.Warn("ACS Connection hasn't had any activity for too long; closing connection")
		if err := client.Close(); err != nil {
			seelog.Warnf("Error disconnecting: %v", err)
		}
	})

	return timer
}

// anyMessageHandler handles any server message. Any server message means the
// connection is active and thus the heartbeat disconnect should not occur
func anyMessageHandler(timer ttime.Timer, client wsclient.ClientServer) func(interface{}) {
	return func(interface{}) {
		seelog.Debug("ACS activity occurred")
		// Reset read deadline as there's activity on the channel
		if err := client.SetReadDeadline(time.Now().Add(wsRWTimeout)); err != nil {
			seelog.Warnf("Unable to extend read deadline for ACS connection: %v", err)
		}

		// Reset hearbeat timer
		timer.Reset(utils.AddJitter(heartbeatTimeout, heartbeatJitter))
	}
}

func shouldReconnectWithoutBackoff(acsError error) bool {
	return acsError == nil || acsError == io.EOF
}

func isInactiveInstanceError(acsError error) bool {
	return acsError != nil && strings.HasPrefix(acsError.Error(), inactiveInstanceExceptionPrefix)
}

// sendEmptyMessageOnChannel sends an empty message using a go-routine on the
// sepcified channel
func sendEmptyMessageOnChannel(channel chan<- struct{}) {
	go func() {
		channel <- struct{}{}
	}()
}
