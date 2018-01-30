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
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/aws/amazon-ecs-agent/agent/api"
	"github.com/aws/amazon-ecs-agent/agent/engine/dockerstate"
	"github.com/aws/amazon-ecs-agent/agent/statechange"
	"github.com/aws/amazon-ecs-agent/agent/statemanager"
	"github.com/aws/amazon-ecs-agent/agent/utils"
	"github.com/cihub/seelog"
)

const (
	// concurrentEventCalls is the maximum number of tasks that may be handled at
	// once by the TaskHandler
	concurrentEventCalls = 3

	// drainEventsFrequency is the frequency at the which unsent events batched
	// by the task handler are sent to the backend
	drainEventsFrequency = 20 * time.Second

	submitStateBackoffMin            = time.Second
	submitStateBackoffMax            = 30 * time.Second
	submitStateBackoffJitterMultiple = 0.20
	submitStateBackoffMultiple       = 1.3
)

// TaskHandler encapsulates the the map of a task arn to task and container events
// associated with said task
type TaskHandler struct {
	// submitSemaphore for the number of tasks that may be handled at once
	submitSemaphore utils.Semaphore
	// taskToEvents is arn:*eventList map so events may be serialized per task
	tasksToEvents map[string]*taskSendableEvents
	// tasksToContainerStates is used to collect container events
	// between task transitions
	tasksToContainerStates map[string][]api.ContainerStateChange

	//  taskHandlerLock is used to safely access the following maps:
	// * taskToEvents
	// * tasksToContainerStates
	lock sync.RWMutex

	// stateSaver is a statemanager which may be used to save any
	// changes to a task or container's SentStatus
	stateSaver statemanager.Saver

	drainEventsFrequency time.Duration
	state                dockerstate.TaskEngineState
	client               api.ECSClient
	ctx                  context.Context
}

// taskSendableEvents is used to group all events for a task
type taskSendableEvents struct {
	// events is a list of *sendableEvents. We treat this as queue, where
	// new events are added to the back of the queue and old events are
	// drained from the front. `sendChange` pushes an event to the back of
	// the queue. An event is removed from the queue in `submitFirstEvent`
	events *list.List
	// sending will check whether the list is already being handled
	sending bool
	//eventsListLock locks both the list and sending bool
	lock sync.Mutex
	// createdAt is a timestamp for when the event list was created
	createdAt time.Time
	// taskARN is the task arn that the event list is associated with
	taskARN string
}

// NewTaskHandler returns a pointer to TaskHandler
func NewTaskHandler(ctx context.Context,
	stateManager statemanager.Saver,
	state dockerstate.TaskEngineState,
	client api.ECSClient) *TaskHandler {
	// Create a handler and start the periodic event drain loop
	taskHandler := &TaskHandler{
		ctx:                    ctx,
		tasksToEvents:          make(map[string]*taskSendableEvents),
		submitSemaphore:        utils.NewSemaphore(concurrentEventCalls),
		tasksToContainerStates: make(map[string][]api.ContainerStateChange),
		stateSaver:             stateManager,
		state:                  state,
		client:                 client,
		drainEventsFrequency:   drainEventsFrequency,
	}
	go taskHandler.startDrainEventsTicker()

	return taskHandler
}

// AddStateChangeEvent queues up the state change event to be sent to ECS.
// If the event is for a container state change, it just gets added to the
// handler.tasksToContainerStates map.
// If the event is for task state change, it triggers the non-blocking
// handler.submitTaskEvents method to submit the batched container state
// changes and the task state change to ECS
func (handler *TaskHandler) AddStateChangeEvent(change statechange.Event, client api.ECSClient) error {
	handler.lock.Lock()
	defer handler.lock.Unlock()

	switch change.GetEventType() {
	case statechange.TaskEvent:
		event, ok := change.(api.TaskStateChange)
		if !ok {
			return errors.New("eventhandler: unable to get task event from state change event")
		}
		// Task event: gather all the container events and send them
		// to ECS by invoking the async submitTaskEvents method from
		// the sendable event list object
		handler.flushBatchUnsafe(&event, client)
		return nil

	case statechange.ContainerEvent:
		event, ok := change.(api.ContainerStateChange)
		if !ok {
			return errors.New("eventhandler: unable to get container event from state change event")
		}
		handler.batchContainerEventUnsafe(event)
		return nil

	default:
		return errors.New("eventhandler: unable to determine event type from state change event")
	}
}

// startDrainEventsTicker starts a ticker that periodically drains the events queue
// by submitting state change events to the ECS backend
func (handler *TaskHandler) startDrainEventsTicker() {
	ticker := time.NewTicker(handler.drainEventsFrequency)
	for {
		select {
		case <-handler.ctx.Done():
			seelog.Infof("TaskHandler: Stopping periodic container state change submission ticker")
			ticker.Stop()
			return
		case <-ticker.C:
			// Gather a list of task state changes to send. This list is
			// constructed from the tasksToEvents map based on the task
			// arns of containers that haven't been sent to ECS yet.
			for _, taskEvent := range handler.taskStateChangesToSend() {
				seelog.Infof(
					"TaskHandler: Adding a state change event to send batched container events: %s",
					taskEvent.String())
				// Force start the the task state change submission
				// workflow by calling AddStateChangeEvent method.
				handler.AddStateChangeEvent(taskEvent, handler.client)
			}
		}
	}
}

// taskStateChangesToSend gets a list task state changes for container events that
// have been batched and not sent beyond the drainEventsFrequency threshold
func (handler *TaskHandler) taskStateChangesToSend() []api.TaskStateChange {
	handler.lock.RLock()
	defer handler.lock.RUnlock()

	var events []api.TaskStateChange
	for taskARN := range handler.tasksToContainerStates {
		// An entry for the task in tasksToContainerStates means that there
		// is at least 1 container event for that task that hasn't been sent
		// to ECS (has been batched).
		// Make sure that the engine's task state knows about this task (as a
		// safety mechanism) and add it to the list of task state changes
		// that need to be sent to ECS
		if task, ok := handler.state.TaskByArn(taskARN); ok {
			event := api.TaskStateChange{
				TaskARN: taskARN,
				Status:  task.GetKnownStatus(),
				Task:    task,
			}
			event.SetTaskTimestamps()

			events = append(events, event)
		}
	}
	return events
}

// batchContainerEventUnsafe collects container state change events for a given task arn
func (handler *TaskHandler) batchContainerEventUnsafe(event api.ContainerStateChange) {
	seelog.Infof("TaskHandler: batching container event: %s", event.String())
	handler.tasksToContainerStates[event.TaskArn] = append(handler.tasksToContainerStates[event.TaskArn], event)
}

// flushBatchUnsafe attaches the task arn's container events to TaskStateChange event
// by creating the sendable event list. It then submits this event to ECS asynchronously
func (handler *TaskHandler) flushBatchUnsafe(taskStateChange *api.TaskStateChange, client api.ECSClient) {
	taskStateChange.Containers = append(taskStateChange.Containers,
		handler.tasksToContainerStates[taskStateChange.TaskARN]...)
	// All container events for the task have now been copied to the
	// task state change object. Remove them from the map
	delete(handler.tasksToContainerStates, taskStateChange.TaskARN)

	// Prepare a given event to be sent by adding it to the handler's
	// eventList
	event := newSendableTaskEvent(*taskStateChange)
	taskEvents := handler.getTaskEventsUnsafe(event)

	// Add the event to the sendable events queue for the task and
	// start sending it asynchronously if possible
	taskEvents.sendChange(event, client, handler)
}

// getTaskEventsUnsafe gets the event list for the task arn in the sendableEvent
// from taskToEvent map
func (handler *TaskHandler) getTaskEventsUnsafe(event *sendableEvent) *taskSendableEvents {
	taskARN := event.taskArn()
	taskEvents, ok := handler.tasksToEvents[taskARN]

	if !ok {
		// We are not tracking this task arn in the tasksToEvents map. Create
		// a new entry
		taskEvents = &taskSendableEvents{
			events:    list.New(),
			sending:   false,
			createdAt: time.Now(),
			taskARN:   taskARN,
		}
		handler.tasksToEvents[taskARN] = taskEvents
		seelog.Debugf("TaskHandler: collecting events for new task; event: %s; events: %s ",
			event.toString(), taskEvents.toStringUnsafe())
	}

	return taskEvents
}

// Continuously retries sending an event until it succeeds, sleeping between each
// attempt
func (handler *TaskHandler) submitTaskEvents(taskEvents *taskSendableEvents, client api.ECSClient, taskARN string) {
	defer handler.removeTaskEvents(taskARN)

	backoff := utils.NewSimpleBackoff(submitStateBackoffMin, submitStateBackoffMax,
		submitStateBackoffJitterMultiple, submitStateBackoffMultiple)

	// Mirror events.sending, but without the need to lock since this is local
	// to our goroutine
	done := false
	// TODO: wire in the context here. Else, we have go routine leaks in tests
	for !done {
		// If we looped back up here, we successfully submitted an event, but
		// we haven't emptied the list so we should keep submitting
		backoff.Reset()
		utils.RetryWithBackoff(backoff, func() error {
			// Lock and unlock within this function, allowing the list to be added
			// to while we're not actively sending an event
			seelog.Debug("TaskHandler: Waiting on semaphore to send events...")
			handler.submitSemaphore.Wait()
			defer handler.submitSemaphore.Post()

			var err error
			done, err = taskEvents.submitFirstEvent(handler, backoff)
			return err
		})
	}
}

func (handler *TaskHandler) removeTaskEvents(taskARN string) {
	handler.lock.Lock()
	defer handler.lock.Unlock()

	delete(handler.tasksToEvents, taskARN)
}

// sendChange adds the change to the sendable events queue. It triggers
// the handler's submitTaskEvents async method to submit this change if
// there's no go routines already sending changes for this event list
func (taskEvents *taskSendableEvents) sendChange(change *sendableEvent,
	client api.ECSClient,
	handler *TaskHandler) {

	taskEvents.lock.Lock()
	defer taskEvents.lock.Unlock()

	// Add event to the queue
	seelog.Infof("TaskHandler: Adding event: %s", change.toString())
	taskEvents.events.PushBack(change)

	if !taskEvents.sending {
		// If a send event is not already in progress, trigger the
		// submitTaskEvents to start sending changes to ECS
		taskEvents.sending = true
		go handler.submitTaskEvents(taskEvents, client, change.taskArn())
	} else {
		seelog.Debugf(
			"TaskHandler: Not submitting change as the task is already being sent: %s",
			change.toString())
	}
}

// submitFirstEvent submits the first event for the task from the event list. It
// returns true if the list became empty after submitting the event. Else, it returns
// false. An error is returned if there was an error with submitting the state change
// to ECS. The error is used by the backoff handler to backoff before retrying the
// state change submission for the first event
func (taskEvents *taskSendableEvents) submitFirstEvent(handler *TaskHandler, backoff utils.Backoff) (bool, error) {
	seelog.Debug("TaskHandler: Aquiring lock for sending event...")
	taskEvents.lock.Lock()
	defer taskEvents.lock.Unlock()

	seelog.Debugf("TaskHandler: Aquired lock, processing event list: : %s", taskEvents.toStringUnsafe())

	if taskEvents.events.Len() == 0 {
		seelog.Debug("TaskHandler: No events left; not retrying more")
		taskEvents.sending = false
		return true, nil
	}

	eventToSubmit := taskEvents.events.Front()
	// Extract the wrapped event from the list element
	event := eventToSubmit.Value.(*sendableEvent)

	if event.containerShouldBeSent() {
		if err := event.send(sendContainerStatusToECS, setContainerChangeSent, "container",
			handler.client, eventToSubmit, handler.stateSaver, backoff, taskEvents); err != nil {
			return false, err
		}
	} else if event.taskShouldBeSent() {
		if err := event.send(sendTaskStatusToECS, setTaskChangeSent, "task",
			handler.client, eventToSubmit, handler.stateSaver, backoff, taskEvents); err != nil {
			return false, err
		}
	} else if event.taskAttachmentShouldBeSent() {
		if err := event.send(sendTaskStatusToECS, setTaskAttachmentSent, "task attachment",
			handler.client, eventToSubmit, handler.stateSaver, backoff, taskEvents); err != nil {
			return false, err
		}
	} else {
		// Shouldn't be sent as either a task or container change event; must have been already sent
		seelog.Infof("TaskHandler: Not submitting redundant event; just removing: %s", event.toString())
		taskEvents.events.Remove(eventToSubmit)
	}

	if taskEvents.events.Len() == 0 {
		seelog.Debug("TaskHandler: Removed the last element, no longer sending")
		taskEvents.sending = false
		return true, nil
	}

	return false, nil
}

func (taskEvents *taskSendableEvents) toStringUnsafe() string {
	return fmt.Sprintf("Task event list [taskARN: %s, sending: %t, createdAt: %s]",
		taskEvents.taskARN, taskEvents.sending, taskEvents.createdAt.String())
}

// getTasksToEventsLen returns the length of the tasksToEvents map. It is
// used only in the test code to ascertain that map has been cleaned up
func (handler *TaskHandler) getTasksToEventsLen() int {
	handler.lock.RLock()
	defer handler.lock.RUnlock()

	return len(handler.tasksToEvents)
}
