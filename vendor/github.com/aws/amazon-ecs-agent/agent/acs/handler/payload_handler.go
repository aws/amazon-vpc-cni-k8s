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
	"github.com/aws/amazon-ecs-agent/agent/api"
	"github.com/aws/amazon-ecs-agent/agent/credentials"
	"github.com/aws/amazon-ecs-agent/agent/engine"
	"github.com/aws/amazon-ecs-agent/agent/eventhandler"
	"github.com/aws/amazon-ecs-agent/agent/statemanager"
	"github.com/aws/amazon-ecs-agent/agent/wsclient"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/cihub/seelog"
	"golang.org/x/net/context"
)

// payloadRequestHandler represents the payload operation for the ACS client
type payloadRequestHandler struct {
	// messageBuffer is used to process PayloadMessages received from the server
	messageBuffer chan *ecsacs.PayloadMessage
	// ackRequest is used to send acks to the backend
	ackRequest  chan string
	ctx         context.Context
	taskEngine  engine.TaskEngine
	ecsClient   api.ECSClient
	saver       statemanager.Saver
	taskHandler *eventhandler.TaskHandler
	// cancel is used to stop go routines started by start() method
	cancel               context.CancelFunc
	cluster              string
	containerInstanceArn string
	acsClient            wsclient.ClientServer
	refreshHandler       refreshCredentialsHandler
	credentialsManager   credentials.Manager
}

// newPayloadRequestHandler returns a new payloadRequestHandler object
func newPayloadRequestHandler(
	ctx context.Context,
	taskEngine engine.TaskEngine,
	ecsClient api.ECSClient,
	cluster string,
	containerInstanceArn string,
	acsClient wsclient.ClientServer,
	saver statemanager.Saver,
	refreshHandler refreshCredentialsHandler,
	credentialsManager credentials.Manager,
	taskHandler *eventhandler.TaskHandler) payloadRequestHandler {
	// Create a cancelable context from the parent context
	derivedContext, cancel := context.WithCancel(ctx)
	return payloadRequestHandler{
		messageBuffer:        make(chan *ecsacs.PayloadMessage, payloadMessageBufferSize),
		ackRequest:           make(chan string, payloadMessageBufferSize),
		taskEngine:           taskEngine,
		ecsClient:            ecsClient,
		saver:                saver,
		taskHandler:          taskHandler,
		ctx:                  derivedContext,
		cancel:               cancel,
		cluster:              cluster,
		containerInstanceArn: containerInstanceArn,
		acsClient:            acsClient,
		refreshHandler:       refreshHandler,
		credentialsManager:   credentialsManager,
	}
}

// handlerFunc returns the request handler function for the ecsacs.PayloadMessage type
func (payloadHandler *payloadRequestHandler) handlerFunc() func(payload *ecsacs.PayloadMessage) {
	// return a function that just enqueues PayloadMessages into the message buffer
	return func(payload *ecsacs.PayloadMessage) {
		payloadHandler.messageBuffer <- payload
	}
}

// start invokes go routines to:
// 1. handle messages in the payload message buffer
// 2. handle ack requests to be sent to ACS
func (payloadHandler *payloadRequestHandler) start() {
	go payloadHandler.handleMessages()
	go payloadHandler.sendAcks()
}

// stop cancels the context being used by the payload handler. This is used
// to stop the go routines started by 'start()'
func (payloadHandler *payloadRequestHandler) stop() {
	payloadHandler.cancel()
}

// sendAcks sends ack requests to ACS
func (payloadHandler *payloadRequestHandler) sendAcks() {
	for {
		select {
		case mid := <-payloadHandler.ackRequest:
			payloadHandler.ackMessageId(mid)
		case <-payloadHandler.ctx.Done():
			return
		}
	}
}

// ackMessageId sends an AckRequest for a message id
func (payloadHandler *payloadRequestHandler) ackMessageId(messageID string) {
	seelog.Debugf("Acking payload message id: %s", messageID)
	err := payloadHandler.acsClient.MakeRequest(&ecsacs.AckRequest{
		Cluster:           aws.String(payloadHandler.cluster),
		ContainerInstance: aws.String(payloadHandler.containerInstanceArn),
		MessageId:         aws.String(messageID),
	})
	if err != nil {
		seelog.Warnf("Error 'ack'ing request with messageID: %s, error: %v", messageID, err)
	}
}

// handleMessages processes payload messages in the payload message buffer in-order
func (payloadHandler *payloadRequestHandler) handleMessages() {
	for {
		select {
		case payload := <-payloadHandler.messageBuffer:
			payloadHandler.handleSingleMessage(payload)
		case <-payloadHandler.ctx.Done():
			return
		}
	}
}

// handleSingleMessage processes a single payload message. It adds tasks in the message to the task engine
// An error is returned if the message was not handled correctly. The error is being used only for testing
// today. In the future, it could be used for doing more interesting things.
func (payloadHandler *payloadRequestHandler) handleSingleMessage(payload *ecsacs.PayloadMessage) error {
	if aws.StringValue(payload.MessageId) == "" {
		seelog.Criticalf("Received a payload with no message id")
		return fmt.Errorf("received a payload with no message id")
	}
	seelog.Debugf("Received payload message, message id: %s", aws.StringValue(payload.MessageId))
	credentialsAcks, allTasksHandled := payloadHandler.addPayloadTasks(payload)
	// save the state of tasks we know about after passing them to the task engine
	err := payloadHandler.saver.Save()
	if err != nil {
		seelog.Errorf("Error saving state for payload message! err: %v, messageId: %s", err, aws.StringValue(payload.MessageId))
		// Don't ack; maybe we can save it in the future.
		return fmt.Errorf("error saving state for payload message, with messageId: %s", aws.StringValue(payload.MessageId))
	}
	if !allTasksHandled {
		return fmt.Errorf("did not handle all tasks")
	}

	go func() {
		// Throw the ack in async; it doesn't really matter all that much and this is blocking handling more tasks.
		for _, credentialsAck := range credentialsAcks {
			payloadHandler.refreshHandler.ackMessage(credentialsAck)
		}
		payloadHandler.ackRequest <- *payload.MessageId
	}()

	return nil
}

// addPayloadTasks does validation on each task and, for all valid ones, adds
// it to the task engine. It returns a bool indicating if it could add every
// task to the taskEngine and a slice of credential ack requests
func (payloadHandler *payloadRequestHandler) addPayloadTasks(payload *ecsacs.PayloadMessage) ([]*ecsacs.IAMRoleCredentialsAckRequest, bool) {
	// verify thatwe were able to work with all tasks in this payload so we know whether to ack the whole thing or not
	allTasksOK := true

	validTasks := make([]*api.Task, 0, len(payload.Tasks))
	for _, task := range payload.Tasks {
		if task == nil {
			seelog.Criticalf("Recieved nil task for messageId: %s", *payload.MessageId)
			allTasksOK = false
			continue
		}
		apiTask, err := api.TaskFromACS(task, payload)
		if err != nil {
			payloadHandler.handleUnrecognizedTask(task, err, payload)
			allTasksOK = false
			continue
		}
		if task.RoleCredentials != nil {
			// The payload from ACS for the task has credentials for the
			// task. Add those to the credentials manager and set the
			// credentials id for the task as well
			taskCredentials := credentials.TaskIAMRoleCredentials{
				ARN:                aws.StringValue(task.Arn),
				IAMRoleCredentials: credentials.IAMRoleCredentialsFromACS(task.RoleCredentials, credentials.ApplicationRoleType),
			}
			err = payloadHandler.credentialsManager.SetTaskCredentials(taskCredentials)
			if err != nil {
				payloadHandler.handleUnrecognizedTask(task, err, payload)
				allTasksOK = false
				continue
			}
			apiTask.SetCredentialsID(taskCredentials.IAMRoleCredentials.CredentialsID)
		}

		// Adding the eni information to the task struct
		if len(task.ElasticNetworkInterfaces) != 0 {
			eni, err := api.ENIFromACS(task.ElasticNetworkInterfaces)
			if err != nil {
				payloadHandler.handleUnrecognizedTask(task, err, payload)
				allTasksOK = false
				continue
			}

			apiTask.SetTaskENI(eni)
		}
		if task.ExecutionRoleCredentials != nil {
			// The payload message contains execution credentials for the task.
			// Add the credentials to the credentials manager and set the
			// task executionCredentials id.
			taskExecutionCredentials := credentials.TaskIAMRoleCredentials{
				ARN:                aws.StringValue(task.Arn),
				IAMRoleCredentials: credentials.IAMRoleCredentialsFromACS(task.ExecutionRoleCredentials, credentials.ExecutionRoleType),
			}
			err = payloadHandler.credentialsManager.SetTaskCredentials(taskExecutionCredentials)
			if err != nil {
				payloadHandler.handleUnrecognizedTask(task, err, payload)
				allTasksOK = false
				continue
			}
			apiTask.SetExecutionRoleCredentialsID(taskExecutionCredentials.IAMRoleCredentials.CredentialsID)
		}

		validTasks = append(validTasks, apiTask)
	}
	// Add 'stop' transitions first to allow seqnum ordering to work out
	// Because a 'start' sequence number should only be proceeded if all 'stop's
	// of the same sequence number have completed, the 'start' events need to be
	// added after the 'stop' events are there to block them.
	stoppedTasksCredentialsAcks, stoppedTasksAddedOK := payloadHandler.addTasks(payload, validTasks, isTaskStatusNotStopped)
	newTasksCredentialsAcks, newTasksAddedOK := payloadHandler.addTasks(payload, validTasks, isTaskStatusStopped)
	if !stoppedTasksAddedOK || !newTasksAddedOK {
		allTasksOK = false
	}

	// Construct a slice with credentials acks from all tasks
	var credentialsAcks []*ecsacs.IAMRoleCredentialsAckRequest
	credentialsAcks = append(stoppedTasksCredentialsAcks, newTasksCredentialsAcks...)
	return credentialsAcks, allTasksOK
}

// addTasks adds the tasks to the task engine based on the skipAddTask condition
// This is used to add non-stopped tasks before adding stopped tasks
func (payloadHandler *payloadRequestHandler) addTasks(payload *ecsacs.PayloadMessage, tasks []*api.Task, skipAddTask skipAddTaskComparatorFunc) ([]*ecsacs.IAMRoleCredentialsAckRequest, bool) {
	allTasksOK := true
	var credentialsAcks []*ecsacs.IAMRoleCredentialsAckRequest
	for _, task := range tasks {
		if skipAddTask(task.GetDesiredStatus()) {
			continue
		}
		err := payloadHandler.taskEngine.AddTask(task)
		if err != nil {
			seelog.Warnf("Could not add task; taskengine probably disabled, err: %v", err)
			// Don't ack
			allTasksOK = false
		}

		ackCredentials := func(id string, description string) {
			ack, err := payloadHandler.ackCredentials(payload.MessageId, id)
			if err != nil {
				allTasksOK = false
				seelog.Errorf("Failed to acknowledge %s credentials for task: %s, err: %v", description, task.String(), err)
				return
			}
			credentialsAcks = append(credentialsAcks, ack)
		}

		// Generate an ack request for the credentials in the task, if the
		// task is associated with an IAM role or the exectuion role
		taskCredentialsID := task.GetCredentialsID()
		if taskCredentialsID != "" {
			ackCredentials(taskCredentialsID, "task iam role")
		}

		taskExecutionCredentialsID := task.GetExecutionCredentialsID()
		if taskExecutionCredentialsID != "" {
			ackCredentials(taskExecutionCredentialsID, "task execution role")
		}
	}
	return credentialsAcks, allTasksOK
}

func (payloadHandler *payloadRequestHandler) ackCredentials(messageID *string, credentialsID string) (*ecsacs.IAMRoleCredentialsAckRequest, error) {
	creds, ok := payloadHandler.credentialsManager.GetTaskCredentials(credentialsID)
	if !ok {
		return nil, fmt.Errorf("credentials could not be retrieved")
	} else {
		return &ecsacs.IAMRoleCredentialsAckRequest{
			MessageId:     messageID,
			Expiration:    aws.String(creds.IAMRoleCredentials.Expiration),
			CredentialsId: aws.String(creds.IAMRoleCredentials.CredentialsID),
		}, nil
	}
}

// skipAddTaskComparatorFunc defines the function pointer that accepts task status
// and returns the boolean comparison result
type skipAddTaskComparatorFunc func(api.TaskStatus) bool

// isTaskStatusStopped returns true if the task status == STOPPTED
func isTaskStatusStopped(status api.TaskStatus) bool {
	return status == api.TaskStopped
}

// isTaskStatusNotStopped returns true if the task status != STOPPTED
func isTaskStatusNotStopped(status api.TaskStatus) bool {
	return status != api.TaskStopped
}

// handleUnrecognizedTask handles unrecognized tasks by sending 'stopped' with
// a suitable reason to the backend
func (payloadHandler *payloadRequestHandler) handleUnrecognizedTask(task *ecsacs.Task, err error, payload *ecsacs.PayloadMessage) {
	if task.Arn == nil {
		seelog.Criticalf("Recieved task with no arn, messageId: %s, task: %v", *payload.MessageId, task)
		return
	}

	// Only need to stop the task; it brings down the containers too.
	taskEvent := api.TaskStateChange{
		TaskARN: *task.Arn,
		Status:  api.TaskStopped,
		Reason:  UnrecognizedTaskError{err}.Error(),
	}

	payloadHandler.taskHandler.AddStateChangeEvent(taskEvent, payloadHandler.ecsClient)
}

// clearAcks drains the ack request channel
func (payloadHandler *payloadRequestHandler) clearAcks() {
	for {
		select {
		case <-payloadHandler.ackRequest:
		default:
			return
		}
	}
}
