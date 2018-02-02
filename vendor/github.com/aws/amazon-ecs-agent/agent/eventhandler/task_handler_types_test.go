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

package eventhandler

import (
	"fmt"
	"testing"
	"time"

	"github.com/aws/amazon-ecs-agent/agent/api"
	"github.com/stretchr/testify/assert"
)

func TestShouldContainerEventBeSent(t *testing.T) {
	event := newSendableContainerEvent(api.ContainerStateChange{
		Status: api.ContainerStopped,
	})
	assert.Equal(t, true, event.containerShouldBeSent())
	assert.Equal(t, false, event.taskShouldBeSent())
}

func TestShouldTaskEventBeSent(t *testing.T) {
	for _, tc := range []struct {
		event        *sendableEvent
		shouldBeSent bool
	}{
		{
			// We don't send a task event to backend if task status == NONE
			event: newSendableTaskEvent(api.TaskStateChange{
				Status: api.TaskStatusNone,
				Task: &api.Task{
					SentStatusUnsafe: api.TaskStatusNone,
				},
			}),
			shouldBeSent: false,
		},
		{
			// task status == RUNNING should be sent to backend
			event: newSendableTaskEvent(api.TaskStateChange{
				Status: api.TaskRunning,
				Task:   &api.Task{},
			}),
			shouldBeSent: true,
		},
		{
			// task event will not be sent if sent status >= task status
			event: newSendableTaskEvent(api.TaskStateChange{
				Status: api.TaskRunning,
				Task: &api.Task{
					SentStatusUnsafe: api.TaskRunning,
				},
			}),
			shouldBeSent: false,
		},
		{
			// this is a valid event as task status >= sent status
			event: newSendableTaskEvent(api.TaskStateChange{
				Status: api.TaskStopped,
				Task: &api.Task{
					SentStatusUnsafe: api.TaskRunning,
				},
			}),
			shouldBeSent: true,
		},
		{
			// Even though the task has been sent, there's a container
			// state change that needs to be sent
			event: newSendableTaskEvent(api.TaskStateChange{
				Status: api.TaskRunning,
				Task: &api.Task{
					SentStatusUnsafe: api.TaskRunning,
				},
				Containers: []api.ContainerStateChange{
					{
						Container: &api.Container{
							SentStatusUnsafe:  api.ContainerRunning,
							KnownStatusUnsafe: api.ContainerRunning,
						},
					},
					{
						Container: &api.Container{
							SentStatusUnsafe:  api.ContainerRunning,
							KnownStatusUnsafe: api.ContainerStopped,
						},
					},
				},
			}),
			shouldBeSent: true,
		},
		{
			// Container state change should be sent regardless of task
			// status.
			event: newSendableTaskEvent(api.TaskStateChange{
				Status: api.TaskStatusNone,
				Task: &api.Task{
					SentStatusUnsafe: api.TaskStatusNone,
				},
				Containers: []api.ContainerStateChange{
					{
						Container: &api.Container{
							SentStatusUnsafe:  api.ContainerStatusNone,
							KnownStatusUnsafe: api.ContainerRunning,
						},
					},
				},
			}),
			shouldBeSent: true,
		},
		{
			// All states sent, nothing to send
			event: newSendableTaskEvent(api.TaskStateChange{
				Status: api.TaskRunning,
				Task: &api.Task{
					SentStatusUnsafe: api.TaskRunning,
				},
				Containers: []api.ContainerStateChange{
					{
						Container: &api.Container{
							SentStatusUnsafe:  api.ContainerRunning,
							KnownStatusUnsafe: api.ContainerRunning,
						},
					},
					{
						Container: &api.Container{
							SentStatusUnsafe:  api.ContainerStopped,
							KnownStatusUnsafe: api.ContainerStopped,
						},
					},
				},
			}),
			shouldBeSent: false,
		},
	} {
		t.Run(fmt.Sprintf("Event[%s] should be sent[%t]", tc.event.toString(), tc.shouldBeSent), func(t *testing.T) {
			assert.Equal(t, tc.shouldBeSent, tc.event.taskShouldBeSent())
			assert.Equal(t, false, tc.event.containerShouldBeSent())
			assert.Equal(t, false, tc.event.taskAttachmentShouldBeSent())
		})
	}
}

func TestShouldTaskAttachmentEventBeSent(t *testing.T) {
	for _, tc := range []struct {
		event                  *sendableEvent
		attachmentShouldBeSent bool
		taskShouldBeSent       bool
	}{
		{
			// ENI Attachment is only sent if task status == NONE
			event: newSendableTaskEvent(api.TaskStateChange{
				Status: api.TaskStopped,
				Task:   &api.Task{},
			}),
			attachmentShouldBeSent: false,
			taskShouldBeSent:       true,
		},
		{
			// ENI Attachment is only sent if task status == NONE and if
			// the event has a non nil attachment object
			event: newSendableTaskEvent(api.TaskStateChange{
				Status: api.TaskStatusNone,
			}),
			attachmentShouldBeSent: false,
			taskShouldBeSent:       false,
		},
		{
			// ENI Attachment is only sent if task status == NONE and if
			// the event has a non nil attachment object and if expiration
			// ack timeout is set for future
			event: newSendableTaskEvent(api.TaskStateChange{
				Status: api.TaskStatusNone,
				Attachment: &api.ENIAttachment{
					ExpiresAt:        time.Unix(time.Now().Unix()-1, 0),
					AttachStatusSent: false,
				},
			}),
			attachmentShouldBeSent: false,
			taskShouldBeSent:       false,
		},
		{
			// ENI Attachment is only sent if task status == NONE and if
			// the event has a non nil attachment object and if expiration
			// ack timeout is set for future and if attachment status hasn't
			// already been sent
			event: newSendableTaskEvent(api.TaskStateChange{
				Status: api.TaskStatusNone,
				Attachment: &api.ENIAttachment{
					ExpiresAt:        time.Unix(time.Now().Unix()+10, 0),
					AttachStatusSent: true,
				},
			}),
			attachmentShouldBeSent: false,
			taskShouldBeSent:       false,
		},
		{
			// Valid attachment event, ensure that its sent
			event: newSendableTaskEvent(api.TaskStateChange{
				Status: api.TaskStatusNone,
				Attachment: &api.ENIAttachment{
					ExpiresAt:        time.Unix(time.Now().Unix()+10, 0),
					AttachStatusSent: false,
				},
			}),
			attachmentShouldBeSent: true,
			taskShouldBeSent:       false,
		},
	} {
		t.Run(fmt.Sprintf("Event[%s] should be sent[attachment=%t;task=%t]",
			tc.event.toString(), tc.attachmentShouldBeSent, tc.taskShouldBeSent), func(t *testing.T) {
			assert.Equal(t, tc.attachmentShouldBeSent, tc.event.taskAttachmentShouldBeSent())
			assert.Equal(t, tc.taskShouldBeSent, tc.event.taskShouldBeSent())
			assert.Equal(t, false, tc.event.containerShouldBeSent())
		})
	}
}
