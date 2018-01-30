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

package eventhandler

import (
	"container/list"
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/aws/amazon-ecs-agent/agent/api"
	"github.com/aws/amazon-ecs-agent/agent/api/mocks"
	"github.com/aws/amazon-ecs-agent/agent/engine/dockerstate/mocks"
	"github.com/aws/amazon-ecs-agent/agent/statechange"
	"github.com/aws/amazon-ecs-agent/agent/statemanager"
	"github.com/aws/amazon-ecs-agent/agent/utils"
	"github.com/aws/amazon-ecs-agent/agent/utils/mocks"
	"github.com/golang/mock/gomock"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
)

const taskARN = "taskarn"

func TestSendsEventsOneContainer(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mock_api.NewMockECSClient(ctrl)
	stateManager := statemanager.NewNoopStateManager()

	ctx, cancel := context.WithCancel(context.Background())
	handler := NewTaskHandler(ctx, stateManager, nil, client)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)

	// Trivial: one container, no errors
	contEvent1 := containerEvent(taskARN)
	contEvent2 := containerEvent(taskARN)
	taskEvent2 := taskEvent(taskARN)

	client.EXPECT().SubmitTaskStateChange(gomock.Any()).Do(func(change api.TaskStateChange) {
		assert.Equal(t, 2, len(change.Containers))
		assert.Equal(t, taskARN, change.Containers[0].TaskArn)
		assert.Equal(t, taskARN, change.Containers[1].TaskArn)
		wg.Done()
	})

	handler.AddStateChangeEvent(contEvent1, client)
	handler.AddStateChangeEvent(contEvent2, client)
	handler.AddStateChangeEvent(taskEvent2, client)

	wg.Wait()
}

func TestSendsEventsOneEventRetries(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mock_api.NewMockECSClient(ctrl)
	stateManager := statemanager.NewNoopStateManager()

	ctx, cancel := context.WithCancel(context.Background())
	handler := NewTaskHandler(ctx, stateManager, nil, client)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(2)

	retriable := utils.NewRetriableError(utils.NewRetriable(true), errors.New("test"))
	taskEvent := taskEvent(taskARN)

	gomock.InOrder(
		client.EXPECT().SubmitTaskStateChange(gomock.Any()).Return(retriable).Do(func(interface{}) { wg.Done() }),
		client.EXPECT().SubmitTaskStateChange(gomock.Any()).Return(nil).Do(func(interface{}) { wg.Done() }),
	)

	handler.AddStateChangeEvent(taskEvent, client)

	wg.Wait()
}

func TestSendsEventsConcurrentLimit(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mock_api.NewMockECSClient(ctrl)
	stateManager := statemanager.NewNoopStateManager()

	ctx, cancel := context.WithCancel(context.Background())
	handler := NewTaskHandler(ctx, stateManager, nil, client)
	defer cancel()

	completeStateChange := make(chan bool, concurrentEventCalls+1)
	var wg sync.WaitGroup

	client.EXPECT().SubmitTaskStateChange(gomock.Any()).Times(concurrentEventCalls + 1).Do(func(interface{}) {
		wg.Done()
		<-completeStateChange
	})

	// Test concurrency; ensure it doesn't attempt to send more than
	// concurrentEventCalls at once
	wg.Add(concurrentEventCalls)

	// Put on N+1 events
	for i := 0; i < concurrentEventCalls+1; i++ {
		handler.AddStateChangeEvent(taskEvent("concurrent_"+strconv.Itoa(i)), client)
	}
	wg.Wait()

	// accept a single change event
	wg.Add(1)
	completeStateChange <- true
	wg.Wait()

	// ensure the remaining requests are completed
	for i := 0; i < concurrentEventCalls; i++ {
		completeStateChange <- true
	}
}

func TestSendsEventsContainerDifferences(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mock_api.NewMockECSClient(ctrl)
	stateManager := statemanager.NewNoopStateManager()

	ctx, cancel := context.WithCancel(context.Background())
	handler := NewTaskHandler(ctx, stateManager, nil, client)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)

	// Test container event replacement doesn't happen
	contEvent1 := containerEvent(taskARN)
	contEvent2 := containerEventStopped(taskARN)
	taskEvent := taskEvent(taskARN)

	client.EXPECT().SubmitTaskStateChange(gomock.Any()).Do(func(change api.TaskStateChange) {
		assert.Equal(t, 2, len(change.Containers))
		assert.Equal(t, taskARN, change.Containers[0].TaskArn)
		assert.Equal(t, api.ContainerRunning, change.Containers[0].Status)
		assert.Equal(t, taskARN, change.Containers[1].TaskArn)
		assert.Equal(t, api.ContainerStopped, change.Containers[1].Status)
		wg.Done()
	})

	handler.AddStateChangeEvent(contEvent1, client)
	handler.AddStateChangeEvent(contEvent2, client)
	handler.AddStateChangeEvent(taskEvent, client)

	wg.Wait()
}

func TestSendsEventsTaskDifferences(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mock_api.NewMockECSClient(ctrl)
	stateManager := statemanager.NewNoopStateManager()

	ctx, cancel := context.WithCancel(context.Background())
	handler := NewTaskHandler(ctx, stateManager, nil, client)
	defer cancel()

	taskARNA := "taskarnA"
	taskARNB := "taskarnB"

	var wg sync.WaitGroup
	wg.Add(2)

	var wgAddEvent sync.WaitGroup
	wgAddEvent.Add(1)

	// Test task event replacement doesn't happen
	taskEventA := taskEvent(taskARNA)
	contEventA1 := containerEvent(taskARNA)

	contEventB1 := containerEvent(taskARNB)
	contEventB2 := containerEventStopped(taskARNB)
	taskEventB := taskEventStopped(taskARNB)

	client.EXPECT().SubmitTaskStateChange(gomock.Any()).Do(func(change api.TaskStateChange) {
		assert.Equal(t, taskARNA, change.TaskARN)
		wgAddEvent.Done()
		wg.Done()
	})

	client.EXPECT().SubmitTaskStateChange(gomock.Any()).Do(func(change api.TaskStateChange) {
		assert.Equal(t, taskARNB, change.TaskARN)
		wg.Done()
	})

	handler.AddStateChangeEvent(contEventB1, client)
	handler.AddStateChangeEvent(contEventA1, client)
	handler.AddStateChangeEvent(contEventB2, client)

	handler.AddStateChangeEvent(taskEventA, client)
	wgAddEvent.Wait()

	handler.AddStateChangeEvent(taskEventB, client)

	wg.Wait()
}

func TestSendsEventsDedupe(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mock_api.NewMockECSClient(ctrl)
	stateManager := statemanager.NewNoopStateManager()

	ctx, cancel := context.WithCancel(context.Background())
	handler := NewTaskHandler(ctx, stateManager, nil, client)
	defer cancel()

	taskARNA := "taskarnA"
	taskARNB := "taskarnB"

	var wg sync.WaitGroup
	wg.Add(1)

	// Verify that a task doesn't get sent if we already have 'sent' it
	task1 := taskEvent(taskARNA)
	task1.(api.TaskStateChange).Task.SetSentStatus(api.TaskRunning)
	cont1 := containerEvent(taskARNA)
	cont1.(api.ContainerStateChange).Container.SetSentStatus(api.ContainerRunning)

	handler.AddStateChangeEvent(cont1, client)
	handler.AddStateChangeEvent(task1, client)

	task2 := taskEvent(taskARNB)
	task2.(api.TaskStateChange).Task.SetSentStatus(api.TaskStatusNone)
	cont2 := containerEvent(taskARNB)
	cont2.(api.ContainerStateChange).Container.SetSentStatus(api.ContainerRunning)

	// Expect to send a task status but not a container status
	client.EXPECT().SubmitTaskStateChange(gomock.Any()).Do(func(change api.TaskStateChange) {
		assert.Equal(t, 1, len(change.Containers))
		assert.Equal(t, taskARNB, change.Containers[0].TaskArn)
		assert.Equal(t, taskARNB, change.TaskARN)
		wg.Done()
	})

	handler.AddStateChangeEvent(cont2, client)
	handler.AddStateChangeEvent(task2, client)

	wg.Wait()
}

// TestCleanupTaskEventAfterSubmit tests the map of task event is removed after
// calling submittaskstatechange
func TestCleanupTaskEventAfterSubmit(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	stateManager := statemanager.NewNoopStateManager()
	client := mock_api.NewMockECSClient(ctrl)

	ctx, cancel := context.WithCancel(context.Background())
	handler := NewTaskHandler(ctx, stateManager, nil, client)
	defer cancel()

	taskARN2 := "taskarn2"

	var wg sync.WaitGroup
	wg.Add(3)

	taskEvent1 := taskEvent(taskARN)
	taskEvent2 := taskEvent(taskARN)
	taskEvent3 := taskEvent(taskARN2)

	client.EXPECT().SubmitTaskStateChange(gomock.Any()).Do(
		func(change api.TaskStateChange) {
			wg.Done()
		}).Times(3)

	handler.AddStateChangeEvent(taskEvent1, client)
	handler.AddStateChangeEvent(taskEvent2, client)
	handler.AddStateChangeEvent(taskEvent3, client)

	wg.Wait()

	// Wait for task events to be removed from the tasksToEvents map
	for {
		if handler.getTasksToEventsLen() == 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
}

func containerEvent(arn string) statechange.Event {
	return api.ContainerStateChange{TaskArn: arn, ContainerName: "containerName", Status: api.ContainerRunning, Container: &api.Container{}}
}

func containerEventStopped(arn string) statechange.Event {
	return api.ContainerStateChange{TaskArn: arn, ContainerName: "containerName", Status: api.ContainerStopped, Container: &api.Container{}}
}

func taskEvent(arn string) statechange.Event {
	return api.TaskStateChange{TaskARN: arn, Status: api.TaskRunning, Task: &api.Task{}}
}

func taskEventStopped(arn string) statechange.Event {
	return api.TaskStateChange{TaskARN: arn, Status: api.TaskStopped, Task: &api.Task{}}
}

func TestENISentStatusChange(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	client := mock_api.NewMockECSClient(ctrl)

	task := &api.Task{
		Arn: taskARN,
	}

	eniAttachment := &api.ENIAttachment{
		TaskARN:          taskARN,
		AttachStatusSent: false,
		ExpiresAt:        time.Now().Add(time.Second),
	}
	timeoutFunc := func() {
		eniAttachment.AttachStatusSent = true
	}
	assert.NoError(t, eniAttachment.StartTimer(timeoutFunc))

	sendableTaskEvent := newSendableTaskEvent(api.TaskStateChange{
		Attachment: eniAttachment,
		TaskARN:    taskARN,
		Status:     api.TaskStatusNone,
		Task:       task,
	})

	client.EXPECT().SubmitTaskStateChange(gomock.Any()).Return(nil)

	events := list.New()
	events.PushBack(sendableTaskEvent)
	ctx, cancel := context.WithCancel(context.Background())
	handler := NewTaskHandler(ctx, statemanager.NewNoopStateManager(), nil, client)
	defer cancel()
	handler.submitTaskEvents(&taskSendableEvents{
		events: events,
	}, client, taskARN)

	assert.True(t, eniAttachment.AttachStatusSent)
}

func TestGetBatchedContainerEvents(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	state := mock_dockerstate.NewMockTaskEngineState(ctrl)

	handler := &TaskHandler{
		tasksToContainerStates: map[string][]api.ContainerStateChange{
			"t1": []api.ContainerStateChange{},
			"t2": []api.ContainerStateChange{},
		},
		state: state,
	}

	state.EXPECT().TaskByArn("t1").Return(&api.Task{Arn: "t1", KnownStatusUnsafe: api.TaskRunning}, true)
	state.EXPECT().TaskByArn("t2").Return(nil, false)

	events := handler.taskStateChangesToSend()
	assert.Len(t, events, 1)
	assert.Equal(t, "t1", events[0].TaskARN)
}

func TestSubmitTaskEventsWhenSubmittingTaskRunningAfterStopped(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	state := mock_dockerstate.NewMockTaskEngineState(ctrl)
	client := mock_api.NewMockECSClient(ctrl)
	stateManager := statemanager.NewNoopStateManager()

	handler := &TaskHandler{
		state:                  state,
		submitSemaphore:        utils.NewSemaphore(concurrentEventCalls),
		tasksToEvents:          make(map[string]*taskSendableEvents),
		tasksToContainerStates: make(map[string][]api.ContainerStateChange),
		stateSaver:             stateManager,
		client:                 client,
	}

	taskEvents := &taskSendableEvents{events: list.New(),
		sending:   false,
		createdAt: time.Now(),
		taskARN:   taskARN,
	}

	backoff := mock_utils.NewMockBackoff(ctrl)
	ok, err := taskEvents.submitFirstEvent(handler, backoff)
	assert.True(t, ok)
	assert.NoError(t, err)

	task := &api.Task{}
	taskEvents.events.PushBack(newSendableTaskEvent(api.TaskStateChange{
		TaskARN: taskARN,
		Status:  api.TaskStopped,
		Task:    task,
	}))
	taskEvents.events.PushBack(newSendableTaskEvent(api.TaskStateChange{
		TaskARN: taskARN,
		Status:  api.TaskRunning,
		Task:    task,
	}))
	handler.tasksToEvents[taskARN] = taskEvents

	var wg sync.WaitGroup
	wg.Add(1)
	gomock.InOrder(
		client.EXPECT().SubmitTaskStateChange(gomock.Any()).Do(func(change api.TaskStateChange) {
			assert.Equal(t, api.TaskStopped, change.Status)
		}),
		backoff.EXPECT().Reset().Do(func() {
			wg.Done()
		}),
	)
	ok, err = taskEvents.submitFirstEvent(handler, backoff)
	// We have an unsent event for the TaskRunning transition. Hence, send() returns false
	assert.False(t, ok)
	assert.NoError(t, err)
	wg.Wait()

	// The unsent transition is deleted from the task list. send() returns true as it
	// does not have any more events to process
	ok, err = taskEvents.submitFirstEvent(handler, backoff)
	assert.NoError(t, err)
	assert.True(t, ok)
}

func TestSubmitTaskEventsWhenSubmittingTaskStoppedAfterRunning(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	state := mock_dockerstate.NewMockTaskEngineState(ctrl)
	client := mock_api.NewMockECSClient(ctrl)
	stateManager := statemanager.NewNoopStateManager()

	handler := &TaskHandler{
		state:                  state,
		submitSemaphore:        utils.NewSemaphore(concurrentEventCalls),
		tasksToEvents:          make(map[string]*taskSendableEvents),
		tasksToContainerStates: make(map[string][]api.ContainerStateChange),
		stateSaver:             stateManager,
		client:                 client,
	}

	taskEvents := &taskSendableEvents{events: list.New(),
		sending:   false,
		createdAt: time.Now(),
		taskARN:   taskARN,
	}

	backoff := mock_utils.NewMockBackoff(ctrl)
	ok, err := taskEvents.submitFirstEvent(handler, backoff)
	assert.True(t, ok)
	assert.NoError(t, err)

	task := &api.Task{}
	taskEvents.events.PushBack(newSendableTaskEvent(api.TaskStateChange{
		TaskARN: taskARN,
		Status:  api.TaskRunning,
		Task:    task,
	}))
	taskEvents.events.PushBack(newSendableTaskEvent(api.TaskStateChange{
		TaskARN: taskARN,
		Status:  api.TaskStopped,
		Task:    task,
	}))
	handler.tasksToEvents[taskARN] = taskEvents

	var wg sync.WaitGroup
	wg.Add(1)
	gomock.InOrder(
		client.EXPECT().SubmitTaskStateChange(gomock.Any()).Do(func(change api.TaskStateChange) {
			assert.Equal(t, api.TaskRunning, change.Status)
		}),
		backoff.EXPECT().Reset().Do(func() {
			wg.Done()
		}),
	)
	// We have an unsent event for the TaskStopped transition. Hence, send() returns false
	ok, err = taskEvents.submitFirstEvent(handler, backoff)
	assert.False(t, ok)
	assert.NoError(t, err)
	wg.Wait()

	wg.Add(1)
	gomock.InOrder(
		client.EXPECT().SubmitTaskStateChange(gomock.Any()).Do(func(change api.TaskStateChange) {
			assert.Equal(t, api.TaskStopped, change.Status)
		}),
		backoff.EXPECT().Reset().Do(func() {
			wg.Done()
		}),
	)
	// The unsent transition is send and deleted from the task list. send() returns true as it
	// does not have any more events to process
	ok, err = taskEvents.submitFirstEvent(handler, backoff)
	assert.True(t, ok)
	assert.NoError(t, err)
	wg.Wait()
}
