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

package handler

import (
	"time"

	"github.com/aws/amazon-ecs-agent/agent/acs/model/ecsacs"
	"github.com/aws/amazon-ecs-agent/agent/api"
	"github.com/aws/amazon-ecs-agent/agent/engine/dockerstate"
	"github.com/aws/amazon-ecs-agent/agent/statemanager"
	"github.com/aws/amazon-ecs-agent/agent/wsclient"
	"github.com/aws/aws-sdk-go/aws"

	"github.com/cihub/seelog"
	"github.com/pkg/errors"

	"golang.org/x/net/context"
)

// attachENIHandler represents the ENI attach operation for the ACS client
type attachENIHandler struct {
	messageBuffer     chan *ecsacs.AttachTaskNetworkInterfacesMessage
	ctx               context.Context
	cancel            context.CancelFunc
	saver             statemanager.Saver
	cluster           *string
	containerInstance *string
	acsClient         wsclient.ClientServer
	state             dockerstate.TaskEngineState
}

// newAttachENIHandler returns an instance of the attachENIHandler struct
func newAttachENIHandler(ctx context.Context,
	cluster string,
	containerInstanceArn string,
	acsClient wsclient.ClientServer,
	taskEngineState dockerstate.TaskEngineState,
	saver statemanager.Saver) attachENIHandler {

	// Create a cancelable context from the parent context
	derivedContext, cancel := context.WithCancel(ctx)
	return attachENIHandler{
		messageBuffer:     make(chan *ecsacs.AttachTaskNetworkInterfacesMessage),
		ctx:               derivedContext,
		cancel:            cancel,
		cluster:           aws.String(cluster),
		containerInstance: aws.String(containerInstanceArn),
		acsClient:         acsClient,
		state:             taskEngineState,
		saver:             saver,
	}
}

// handlerFunc returns a function to enqueue requests onto attachENIHandler buffer
func (attachENIHandler *attachENIHandler) handlerFunc() func(message *ecsacs.AttachTaskNetworkInterfacesMessage) {
	return func(message *ecsacs.AttachTaskNetworkInterfacesMessage) {
		attachENIHandler.messageBuffer <- message
	}
}

// start invokes handleMessages to ack each enqueued request
func (attachENIHandler *attachENIHandler) start() {
	go attachENIHandler.handleMessages()
}

// stop is used to invoke a cancellation function
func (attachENIHandler *attachENIHandler) stop() {
	attachENIHandler.cancel()
}

// handleMessages handles each message one at a time
func (attachENIHandler *attachENIHandler) handleMessages() {
	for {
		select {
		case message := <-attachENIHandler.messageBuffer:
			if err := attachENIHandler.handleSingleMessage(message); err != nil {
				seelog.Warnf("Unable to handle ENI Attachment message [%s]: %v", message.String(), err)
			}
		case <-attachENIHandler.ctx.Done():
			return
		}
	}
}

// handleSingleMessage acks the message received
func (handler *attachENIHandler) handleSingleMessage(message *ecsacs.AttachTaskNetworkInterfacesMessage) error {
	receivedAt := time.Now()
	// Validate fields in the message
	if err := validateAttachTaskNetworkInterfacesMessage(message); err != nil {
		return errors.Wrapf(err,
			"attach eni message handler: error validating AttachTaskNetworkInterface message received from ECS")
	}

	// Send ACK
	go func(clusterArn *string, containerInstanceArn *string, messageID *string) {
		if err := handler.acsClient.MakeRequest(&ecsacs.AckRequest{
			Cluster:           clusterArn,
			ContainerInstance: containerInstanceArn,
			MessageId:         messageID,
		}); err != nil {
			seelog.Warnf("Failed to ack request with messageId: %s, error: %v", aws.StringValue(messageID), err)
		}
	}(message.ClusterArn, message.ContainerInstanceArn, message.MessageId)

	// Check if this is a duplicate message
	mac := aws.StringValue(message.ElasticNetworkInterfaces[0].MacAddress)
	if eniAttachment, ok := handler.state.ENIByMac(mac); ok {
		seelog.Infof("Duplicate ENI attachment message for ENI with MAC address: %s", mac)
		eniAckTimeoutHandler := ackTimeoutHandler{mac: mac, state: handler.state}
		return eniAttachment.StartTimer(eniAckTimeoutHandler.handle)
	}
	if err := handler.addENIAttachmentToState(message, receivedAt); err != nil {
		return errors.Wrapf(err, "attach eni message handler: unable to add eni attachment to engine state")
	}
	if err := handler.saver.Save(); err != nil {
		return errors.Wrapf(err, "attach eni message handler: unable to save agent state")
	}
	return nil
}

// addENIAttachmentToState adds the eni info to the state
func (handler *attachENIHandler) addENIAttachmentToState(message *ecsacs.AttachTaskNetworkInterfacesMessage, receivedAt time.Time) error {
	attachmentARN := aws.StringValue(message.ElasticNetworkInterfaces[0].AttachmentArn)
	mac := aws.StringValue(message.ElasticNetworkInterfaces[0].MacAddress)
	taskARN := aws.StringValue(message.TaskArn)
	eniAttachment := &api.ENIAttachment{
		TaskARN:          taskARN,
		AttachmentARN:    attachmentARN,
		AttachStatusSent: false,
		MACAddress:       mac,
		// Stop tracking the eni attachment after timeout
		ExpiresAt: receivedAt.Add(time.Duration(aws.Int64Value(message.WaitTimeoutMs)) * time.Millisecond),
	}
	eniAckTimeoutHandler := ackTimeoutHandler{mac: mac, state: handler.state}
	if err := eniAttachment.StartTimer(eniAckTimeoutHandler.handle); err != nil {
		return err
	}
	seelog.Infof("Adding eni info for task '%s' to state, attachment=%s mac=%s",
		taskARN, attachmentARN, mac)
	handler.state.AddENIAttachment(eniAttachment)
	return nil
}

// ackTimeoutHandler remove ENI attachment from agent state after the ENI ack timeout
type ackTimeoutHandler struct {
	mac   string
	state dockerstate.TaskEngineState
}

func (handler *ackTimeoutHandler) handle() {
	eniAttachment, ok := handler.state.ENIByMac(handler.mac)
	if !ok {
		seelog.Warnf("Timed out waiting for ENI ack; ignoring unmanaged ENI attachment with MAC address: %s", handler.mac)
		return
	}
	if !eniAttachment.IsSent() {
		seelog.Infof("Timed out waiting for ENI ack; removing ENI attachment record with MAC address: %s", handler.mac)
		handler.state.RemoveENIAttachment(handler.mac)
	}
}

// validateAttachTaskNetworkInterfacesMessage performs validation checks on the
// AttachTaskNetworkInterfacesMessage
func validateAttachTaskNetworkInterfacesMessage(message *ecsacs.AttachTaskNetworkInterfacesMessage) error {
	if message == nil {
		return errors.Errorf("attach eni handler validation: empty AttachTaskNetworkInterface message received from ECS")
	}

	messageId := aws.StringValue(message.MessageId)
	if messageId == "" {
		return errors.Errorf("attach eni handler validation: message id not set in AttachTaskNetworkInterface message received from ECS")
	}

	clusterArn := aws.StringValue(message.ClusterArn)
	if clusterArn == "" {
		return errors.Errorf("attach eni handler validation: clusterArn not set in AttachTaskNetworkInterface message received from ECS")
	}

	containerInstanceArn := aws.StringValue(message.ContainerInstanceArn)
	if containerInstanceArn == "" {
		return errors.Errorf("attach eni handler validation: containerInstanceArn not set in AttachTaskNetworkInterface message received from ECS")
	}

	enis := message.ElasticNetworkInterfaces
	if len(enis) != 1 {
		return errors.Errorf("attach eni handler validation: incorrect number of ENIs in AttachTaskNetworkInterface message received from ECS. Obtained %d", len(enis))
	}

	eni := enis[0]
	if aws.StringValue(eni.MacAddress) == "" {
		return errors.Errorf("attach eni handler validation: MACAddress not listed in AttachTaskNetworkInterface message received from ECS")
	}

	taskArn := aws.StringValue(message.TaskArn)
	if taskArn == "" {
		return errors.Errorf("attach eni handler validation: taskArn not set in AttachTaskNetworkInterface message received from ECS")
	}

	timeout := aws.Int64Value(message.WaitTimeoutMs)
	if timeout <= 0 {
		return errors.Errorf("attach eni handler validation: invalid timeout listed in AttachTaskNetworkInterface message received from ECS")

	}

	return nil
}
