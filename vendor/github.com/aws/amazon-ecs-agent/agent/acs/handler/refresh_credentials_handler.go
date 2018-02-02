// Copyright 2014-2016 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

	"github.com/aws/amazon-ecs-agent/agent/acs/model/ecsacs"
	"github.com/aws/amazon-ecs-agent/agent/credentials"
	"github.com/aws/amazon-ecs-agent/agent/engine"
	"github.com/aws/amazon-ecs-agent/agent/wsclient"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/cihub/seelog"
	"golang.org/x/net/context"
)

// refreshCredentialsHandler represents the refresh credentials operation for the ACS client
type refreshCredentialsHandler struct {
	// messageBuffer is used to process IAMRoleCredentialsMessages received from the server
	messageBuffer chan *ecsacs.IAMRoleCredentialsMessage
	// ackRequest is used to send acks to the backend
	ackRequest chan *ecsacs.IAMRoleCredentialsAckRequest
	ctx        context.Context
	// cancel is used to stop go routines started by start() method
	cancel             context.CancelFunc
	cluster            *string
	containerInstance  *string
	acsClient          wsclient.ClientServer
	credentialsManager credentials.Manager
	taskEngine         engine.TaskEngine
}

// newRefreshCredentialsHandler returns a new refreshCredentialsHandler object
func newRefreshCredentialsHandler(ctx context.Context, cluster string, containerInstanceArn string, acsClient wsclient.ClientServer, credentialsManager credentials.Manager, taskEngine engine.TaskEngine) refreshCredentialsHandler {
	// Create a cancelable context from the parent context
	derivedContext, cancel := context.WithCancel(ctx)
	return refreshCredentialsHandler{
		messageBuffer:      make(chan *ecsacs.IAMRoleCredentialsMessage),
		ackRequest:         make(chan *ecsacs.IAMRoleCredentialsAckRequest),
		ctx:                derivedContext,
		cancel:             cancel,
		cluster:            aws.String(cluster),
		containerInstance:  aws.String(containerInstanceArn),
		acsClient:          acsClient,
		credentialsManager: credentialsManager,
		taskEngine:         taskEngine,
	}
}

// handlerFunc returns the request handler function for the ecsacs.IAMRoleCredentialsMessage
func (refreshHandler *refreshCredentialsHandler) handlerFunc() func(message *ecsacs.IAMRoleCredentialsMessage) {
	// return a function that just enqueues IAMRoleCredentials messages into the message buffer
	return func(message *ecsacs.IAMRoleCredentialsMessage) {
		refreshHandler.messageBuffer <- message
	}
}

// start invokes go routines to:
// 1. handle messages in the refresh credentials message buffer
// 2. handle ack requests to be sent to ACS
func (refreshHandler *refreshCredentialsHandler) start() {
	go refreshHandler.handleMessages()
	go refreshHandler.sendAcks()
}

// stop cancels the context being used by the refresh credentials handler. This is used
// to stop the go routines started by 'start()'
func (refreshHandler *refreshCredentialsHandler) stop() {
	refreshHandler.cancel()
}

// sendAcks sends ack requests to ACS
func (refreshHandler *refreshCredentialsHandler) sendAcks() {
	for {
		select {
		case ack := <-refreshHandler.ackRequest:
			refreshHandler.ackMessage(ack)
		case <-refreshHandler.ctx.Done():
			return
		}
	}
}

// ackMessageId sends an IAMRoleCredentialsAckRequest to the backend
func (refreshHandler *refreshCredentialsHandler) ackMessage(ack *ecsacs.IAMRoleCredentialsAckRequest) {
	err := refreshHandler.acsClient.MakeRequest(ack)
	if err != nil {
		seelog.Warnf("Error 'ack'ing request with messageID: %s, error: %v", aws.StringValue(ack.MessageId), err)
	}
	seelog.Debugf("Acking credentials message: %s", ack.String())
}

// handleMessages processes refresh credentials messages in the buffer in-order
func (refreshHandler *refreshCredentialsHandler) handleMessages() {
	for {
		select {
		case message := <-refreshHandler.messageBuffer:
			refreshHandler.handleSingleMessage(message)
		case <-refreshHandler.ctx.Done():
			return
		}
	}
}

// handleSingleMessage processes a single refresh credentials message.
func (refreshHandler *refreshCredentialsHandler) handleSingleMessage(message *ecsacs.IAMRoleCredentialsMessage) error {
	// Validate fields in the message
	err := validateIAMRoleCredentialsMessage(message)
	if err != nil {
		seelog.Errorf("Error validating credentials message: %v", err)
		return err
	}
	taskArn := aws.StringValue(message.TaskArn)
	messageId := aws.StringValue(message.MessageId)
	task, ok := refreshHandler.taskEngine.GetTaskByArn(taskArn)
	if !ok {
		seelog.Errorf("Task not found in the engine for the arn in credentials message, arn: %s, messageId: %s", taskArn, messageId)
		return fmt.Errorf("task not found in the engine for the arn in credentials message, arn: %s", taskArn)
	}

	roleType := aws.StringValue(message.RoleType)
	if !validRoleType(roleType) {
		seelog.Errorf("Unknown RoleType for task in credentials message, roleType: %s arn: %s, messageId: %s", roleType, taskArn, messageId)
	} else {

		taskCredentials := credentials.TaskIAMRoleCredentials{
			ARN:                taskArn,
			IAMRoleCredentials: credentials.IAMRoleCredentialsFromACS(message.RoleCredentials, roleType),
		}
		err = refreshHandler.credentialsManager.SetTaskCredentials(taskCredentials)
		if err != nil {
			seelog.Errorf("Unable to update credentials for task, err: %v messageId: %s", err, messageId)
			return fmt.Errorf("unable to update credentials %v", err)
		}

		if roleType == credentials.ApplicationRoleType {
			task.SetCredentialsID(aws.StringValue(message.RoleCredentials.CredentialsId))
		}
		if roleType == credentials.ExecutionRoleType {
			task.SetExecutionRoleCredentialsID(aws.StringValue(message.RoleCredentials.CredentialsId))
		}
	}

	go func() {
		response := &ecsacs.IAMRoleCredentialsAckRequest{
			Expiration:    message.RoleCredentials.Expiration,
			MessageId:     message.MessageId,
			CredentialsId: message.RoleCredentials.CredentialsId,
		}
		refreshHandler.ackRequest <- response
	}()
	return nil
}

// validateIAMRoleCredentialsMessage validates fields in the IAMRoleCredentialsMessage
// It returns an error if any of the following fields are not set in the message:
// messageId, taskArn, roleCredentials
func validateIAMRoleCredentialsMessage(message *ecsacs.IAMRoleCredentialsMessage) error {
	if message == nil {
		return fmt.Errorf("empty credentials message")
	}

	messageId := aws.StringValue(message.MessageId)
	if messageId == "" {
		return fmt.Errorf("message id not set in credentials message")
	}

	if aws.StringValue(message.TaskArn) == "" {
		return fmt.Errorf("task Arn not set in credentials message")
	}

	if message.RoleCredentials == nil {
		return fmt.Errorf("role Credentials not set in credentials message: messageId: %s", messageId)
	}

	if aws.StringValue(message.RoleCredentials.CredentialsId) == "" {
		return fmt.Errorf("role Credentials ID not set in credentials message: messageId: %s", messageId)
	}

	return nil
}

// clearAcks drains the ack request channel
func (refreshHandler *refreshCredentialsHandler) clearAcks() {
	for {
		select {
		case <-refreshHandler.ackRequest:
		default:
			return
		}
	}
}

// validRoleType returns false if the RoleType in the acs refresh payload is not
// one of the expected types. TaskApplication, TaskExecution
func validRoleType(roleType string) bool {
	switch roleType {
	case credentials.ApplicationRoleType:
		return true
	case credentials.ExecutionRoleType:
		return true
	default:
		return false
	}
}
