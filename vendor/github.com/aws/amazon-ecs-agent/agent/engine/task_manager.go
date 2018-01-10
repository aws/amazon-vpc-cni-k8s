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

package engine

import (
	"sync"
	"time"

	"github.com/aws/amazon-ecs-agent/agent/api"
	"github.com/aws/amazon-ecs-agent/agent/config"
	"github.com/aws/amazon-ecs-agent/agent/engine/dependencygraph"
	"github.com/aws/amazon-ecs-agent/agent/resources"
	"github.com/aws/amazon-ecs-agent/agent/utils/ttime"
	"github.com/cihub/seelog"
)

const (
	// waitForPullCredentialsTimeout is the timeout agent trying to wait for pull
	// credentials from acs, after the timeout it will check the credentials manager
	// and start processing the task or start another round of waiting
	waitForPullCredentialsTimeout         = 1 * time.Minute
	steadyStateTaskVerifyInterval         = 5 * time.Minute
	stoppedSentWaitInterval               = 30 * time.Second
	maxStoppedWaitTimes                   = 72 * time.Hour / stoppedSentWaitInterval
	taskUnableToTransitionToStoppedReason = "TaskStateError: Agent could not progress task's state to stopped"
	taskUnableToCreatePlatformResources   = "TaskStateError: Agent could not create task's platform resources"
)

type acsTaskUpdate struct {
	api.TaskStatus
}

type dockerContainerChange struct {
	container *api.Container
	event     DockerContainerChangeEvent
}

type acsTransition struct {
	seqnum        int64
	desiredStatus api.TaskStatus
}

// containerTransition defines the struct for a container to transition
type containerTransition struct {
	nextState      api.ContainerStatus
	actionRequired bool
	reason         error
}

// managedTask is a type that is meant to manage the lifecycle of a task.
// There should be only one managed task construct for a given task arn and the
// managed task should be the only thing to modify the task's known or desired statuses.
//
// The managedTask should run serially in a single goroutine in which it reads
// messages from the two given channels and acts upon them.
// This design is chosen to allow a safe level if isolation and avoid any race
// conditions around the state of a task.
// The data sources (e.g. docker, acs) that write to the task's channels may
// block and it is expected that the managedTask listen to those channels
// almost constantly.
// The general operation should be:
//  1) Listen to the channels
//  2) On an event, update the status of the task and containers (known/desired)
//  3) Figure out if any action needs to be done. If so, do it
//  4) GOTO 1
// Item '3' obviously might lead to some duration where you are not listening
// to the channels. However, this can be solved by kicking off '3' as a
// goroutine and then only communicating the result back via the channels
// (obviously once you kick off a goroutine you give up the right to write the
// task's statuses yourself)
type managedTask struct {
	*api.Task
	engine *DockerTaskEngine

	acsMessages    chan acsTransition
	dockerMessages chan dockerContainerChange

	// unexpectedStart is a once that controls stopping a container that
	// unexpectedly started one time.
	// This exists because a 'start' after a container is meant to be stopped is
	// possible under some circumstances (e.g. a timeout). However, if it
	// continues to 'start' when we aren't asking it to, let it go through in
	// case it's a user trying to debug it or in case we're fighting with another
	// thing managing the container.
	unexpectedStart sync.Once

	_time     ttime.Time
	_timeOnce sync.Once

	resource resources.Resource
}

// newManagedTask is a method on DockerTaskEngine to create a new managedTask.
// This method must only be called when the engine.processTasks write lock is
// already held.
func (engine *DockerTaskEngine) newManagedTask(task *api.Task) *managedTask {
	t := &managedTask{
		Task:           task,
		acsMessages:    make(chan acsTransition),
		dockerMessages: make(chan dockerContainerChange),
		engine:         engine,
		resource:       resources.New(),
	}
	engine.managedTasks[task.Arn] = t
	return t
}

// overseeTask is the main goroutine of the managedTask. It runs an infinite
// loop of receiving messages and attempting to take action based on those
// messages.
func (mtask *managedTask) overseeTask() {
	llog := log.New("task", mtask)

	// Do a single updatestatus at the beginning to create the container
	// `desiredstatus`es which are a construct of the engine used only here,
	// not present on the backend
	mtask.UpdateStatus()
	// If this was a 'state restore', send all unsent statuses
	mtask.emitCurrentStatus()

	// Wait for host resources required by this task to become available
	mtask.waitForHostResources()

	// Main infinite loop. This is where we receive messages and dispatch work.
	for {
		// If it's steadyState, just spin until we need to do work
		for mtask.steadyState() {
			mtask.waitSteady()
		}

		if !mtask.GetKnownStatus().Terminal() {
			// If we aren't terminal and we aren't steady state, we should be
			// able to move some containers along.
			llog.Debug("Task not steady state or terminal; progressing it")

			// TODO: Add new resource provisioned state ?
			if mtask.engine.cfg.TaskCPUMemLimit.Enabled() {
				err := mtask.resource.Setup(mtask.Task)
				if err != nil {
					seelog.Criticalf("Unable to setup platform resources for task %s: %v", mtask.Task.Arn, err)
					mtask.SetDesiredStatus(api.TaskStopped)
					mtask.engine.emitTaskEvent(mtask.Task, taskUnableToCreatePlatformResources)
				}
				// TODO: Add log to indicate successful setup of platform resources
			}
			mtask.progressContainers()
		}

		// If we reach this point, we've changed the task in some way.
		// Conversely, for it to spin in steady state it will have to have been
		// loaded in steady state or progressed through here, so saving here should
		// be sufficient to capture state changes.
		err := mtask.engine.saver.Save()
		if err != nil {
			llog.Warn("Error checkpointing task's states to disk", "err", err)
		}

		if mtask.GetKnownStatus().Terminal() {
			break
		}
	}
	// We only break out of the above if this task is known to be stopped. Do
	// onetime cleanup here, including removing the task after a timeout
	llog.Debug("Task has reached stopped. We're just waiting and removing containers now")
	mtask.cleanupCredentials()
	if mtask.StopSequenceNumber != 0 {
		llog.Debug("Marking done for this sequence", "seqnum", mtask.StopSequenceNumber)
		mtask.engine.taskStopGroup.Done(mtask.StopSequenceNumber)
	}
	// TODO: make this idempotent on agent restart
	go mtask.releaseIPInIPAM()
	mtask.cleanupTask(mtask.engine.cfg.TaskCleanupWaitDuration)
}

// emitCurrentStatus emits a container event for every container and a task
// event for the task
func (mtask *managedTask) emitCurrentStatus() {
	for _, container := range mtask.Containers {
		mtask.engine.emitContainerEvent(mtask.Task, container, "")
	}
	mtask.engine.emitTaskEvent(mtask.Task, "")
}

// waitForHostResources waits for host resources to become available to start
// the task. This involves waiting for previous stops to complete so the
// resources become free.
func (mtask *managedTask) waitForHostResources() {
	llog := log.New("task", mtask.Task)
	if mtask.StartSequenceNumber != 0 && !mtask.GetDesiredStatus().Terminal() {
		llog.Info("Waiting for any previous stops to complete", "seqnum", mtask.StartSequenceNumber)
		othersStopped := make(chan bool, 1)
		go func() {
			mtask.engine.taskStopGroup.Wait(mtask.StartSequenceNumber)
			othersStopped <- true
		}()
		for !mtask.waitEvent(othersStopped) {
			if mtask.GetDesiredStatus().Terminal() {
				// If we end up here, that means we received a start then stop for this
				// task before a task that was expected to stop before it could
				// actually stop
				break
			}
		}
		llog.Info("Wait over; ready to move towards status: " + mtask.GetDesiredStatus().String())
	}
}

// waitSteady waits for a task to leave steady-state by waiting for a new
// event, or a timeout.
func (mtask *managedTask) waitSteady() {
	llog := log.New("task", mtask.Task)
	llog.Debug("Task at steady state", "state", mtask.GetKnownStatus().String())

	maxWait := make(chan bool, 1)
	timer := mtask.time().After(steadyStateTaskVerifyInterval)
	go func() {
		<-timer
		maxWait <- true
	}()
	timedOut := mtask.waitEvent(maxWait)

	if timedOut {
		llog.Debug("Checking task to make sure it's still at steadystate")
		go mtask.engine.CheckTaskState(mtask.Task)
	}
}

// cleanupCredentials removes credentials for a stopped task
func (mtask *managedTask) cleanupCredentials() {
	taskCredentialsID := mtask.GetCredentialsID()
	if taskCredentialsID != "" {
		mtask.engine.credentialsManager.RemoveCredentials(taskCredentialsID)
	}
}

// waitEvent waits for any event to occur. If an event occurs, the appropriate
// handler is called. If the event is the passed in channel, it will return the
// value written to the channel, otherwise it will return false.
func (mtask *managedTask) waitEvent(stopWaiting <-chan bool) bool {
	log.Debug("Waiting for event for task", "task", mtask.Task)
	select {
	case acsTransition := <-mtask.acsMessages:
		log.Debug("Got acs event for task", "task", mtask.Task)
		mtask.handleDesiredStatusChange(acsTransition.desiredStatus, acsTransition.seqnum)
		return false
	case dockerChange := <-mtask.dockerMessages:
		log.Debug("Got container event for task", "task", mtask.Task)
		mtask.handleContainerChange(dockerChange)
		return false
	case b := <-stopWaiting:
		log.Debug("No longer waiting", "task", mtask.Task)
		return b
	}
}

// handleDesiredStatusChange updates the desired status on the task. Updates
// only occur if the new desired status is "compatible" (farther along than the
// current desired state); "redundant" (less-than or equal desired states) are
// ignored and dropped.
func (mtask *managedTask) handleDesiredStatusChange(desiredStatus api.TaskStatus, seqnum int64) {
	llog := log.New("task", mtask.Task)
	// Handle acs message changes this task's desired status to whatever
	// acs says it should be if it is compatible
	llog.Debug("New acs transition", "status", desiredStatus.String(), "seqnum", seqnum, "taskSeqnum", mtask.StopSequenceNumber)
	if desiredStatus <= mtask.GetDesiredStatus() {
		llog.Debug("Redundant task transition; ignoring", "old", mtask.GetDesiredStatus().String(), "new", desiredStatus.String())
		return
	}
	if desiredStatus == api.TaskStopped && seqnum != 0 && mtask.GetStopSequenceNumber() == 0 {
		llog.Debug("Task moving to stopped, adding to stopgroup", "seqnum", seqnum)
		mtask.SetStopSequenceNumber(seqnum)
		mtask.engine.taskStopGroup.Add(seqnum, 1)
	}
	mtask.SetDesiredStatus(desiredStatus)
	mtask.UpdateDesiredStatus()
}

// handleContainerChange updates a container's known status. If the message
// contains any interesting information (like exit codes or ports), they are
// propagated.
func (mtask *managedTask) handleContainerChange(containerChange dockerContainerChange) {
	llog := log.New("task", mtask.Task)

	// locate the container
	container := containerChange.container
	found := false
	for _, c := range mtask.Containers {
		if container == c {
			found = true
		}
	}
	if !found {
		llog.Crit("State error; task manager called with another task's container!", "container", container)
		return
	}

	event := containerChange.event
	llog.Debug("Handling container change", "change", containerChange)

	// If this is a backwards transition stopped->running, the first time set it
	// to be known running so it will be stopped. Subsequently ignore these backward transitions
	containerKnownStatus := container.GetKnownStatus()
	mtask.handleStoppedToRunningContainerTransition(event.Status, container)
	if event.Status <= containerKnownStatus {
		seelog.Infof("Redundant container state change for task %s: %s to %s, but already %s", mtask.Task, container, event.Status, containerKnownStatus)
		return
	}

	// Update the container to be known
	currentKnownStatus := containerKnownStatus
	container.SetKnownStatus(event.Status)

	if event.Error != nil {
		proceedAnyway := mtask.handleEventError(containerChange, currentKnownStatus)
		if !proceedAnyway {
			return
		}
	}

	// If the essential container is stopped, set the ExecutionStoppedAt timestamp
	if container.GetKnownStatus() == api.ContainerStopped && container.IsEssential() {
		now := mtask.time().Now()
		ok := mtask.Task.SetExecutionStoppedAt(now)
		if ok {
			seelog.Infof("Recording execution stopped time for a task, essential container in task stopped, task %s, time: %s", mtask.Task.String(), now.String())
		}
	}

	seelog.Debugf("Sending container change event to tcs, container: %s, status: %s", event.DockerID, event.Status)
	err := mtask.engine.containerChangeEventStream.WriteToEventStream(event)
	if err != nil {
		seelog.Warnf("Failed to write container change event to event stream, err %v", err)
	}

	if event.ExitCode != nil && event.ExitCode != container.GetKnownExitCode() {
		container.SetKnownExitCode(event.ExitCode)
	}
	if event.PortBindings != nil {
		container.KnownPortBindings = event.PortBindings
	}
	if event.Volumes != nil {
		mtask.UpdateMountPoints(container, event.Volumes)
	}

	mtask.engine.emitContainerEvent(mtask.Task, container, "")
	if mtask.UpdateStatus() {
		llog.Debug("Container change also resulted in task change")
		// If knownStatus changed, let it be known
		mtask.engine.emitTaskEvent(mtask.Task, "")
	}
}

// releaseIPInIPAM releases the ip used by the task for awsvpc
func (mtask *managedTask) releaseIPInIPAM() {
	if mtask.ENI == nil {
		return
	}
	seelog.Infof("Releasing ip in IPAM for task used eni, task: %s", mtask.Task.Arn)

	err := mtask.engine.releaseIPInIPAM(mtask.Task)
	if err != nil {
		seelog.Warnf("Releasing the ip in IPAM failed, task: [%s], err: %v", mtask.Task.Arn, err)
	}
}

// handleStoppedToRunningContainerTransition detects a "backwards" container
// transition where a known-stopped container is found to be running again and
// handles it.
func (mtask *managedTask) handleStoppedToRunningContainerTransition(status api.ContainerStatus, container *api.Container) {
	llog := log.New("task", mtask.Task)
	containerKnownStatus := container.GetKnownStatus()
	if status <= containerKnownStatus && containerKnownStatus == api.ContainerStopped {
		if status.IsRunning() {
			// If the container becomes running after we've stopped it (possibly
			// because we got an error running it and it ran anyways), the first time
			// update it to 'known running' so that it will be driven back to stopped
			mtask.unexpectedStart.Do(func() {
				llog.Warn("Container that we thought was stopped came back; re-stopping it once")
				go mtask.engine.transitionContainer(mtask.Task, container, api.ContainerStopped)
				// This will not proceed afterwards because status <= knownstatus below
			})
		}
	}
}

// handleEventError handles a container change event error and decides whether
// we should proceed to transition the container
func (mtask *managedTask) handleEventError(containerChange dockerContainerChange, currentKnownStatus api.ContainerStatus) bool {
	container := containerChange.container
	event := containerChange.event
	if container.ApplyingError == nil {
		container.ApplyingError = api.NewNamedError(event.Error)
	}
	if event.Status == api.ContainerStopped {
		// If we were trying to transition to stopped and had a timeout error
		// from docker, reset the known status to the current status and return
		// This ensures that we don't emit a containerstopped event; a
		// terminal container event from docker event stream will instead be
		// responsible for the transition. Alternatively, the steadyState check
		// could also trigger the progress and have another go at stopping the
		// container
		if event.Error.ErrorName() == dockerTimeoutErrorName {
			seelog.Infof("%s for 'docker stop' of container; ignoring state change;  task: %v, container: %v, error: %v",
				dockerTimeoutErrorName, mtask.Task, container, event.Error.Error())
			container.SetKnownStatus(currentKnownStatus)
			return false
		}
		// If docker returned a transient error while trying to stop a container,
		// reset the known status to the current status and return
		cannotStopContainerError, ok := event.Error.(cannotStopContainerError)
		if ok && cannotStopContainerError.IsRetriableError() {
			seelog.Infof("Error stopping the container, ignoring state change; error: %s, task: %v",
				cannotStopContainerError.Error(), mtask.Task)
			container.SetKnownStatus(currentKnownStatus)
			return false
		}

		// If we were trying to transition to stopped and had an error, we
		// clearly can't just continue trying to transition it to stopped
		// again and again... In this case, assume it's stopped (or close
		// enough) and get on with it
		// This can happen in cases where the container we tried to stop
		// was already stopped or did not exist at all.
		seelog.Warnf("'docker stop' returned %s: %s", event.Error.ErrorName(), event.Error.Error())
		container.SetKnownStatus(api.ContainerStopped)
		container.SetDesiredStatus(api.ContainerStopped)
	} else if event.Status == api.ContainerPulled {
		// Another special case; a failure to pull might not be fatal if e.g. the image already exists.
		seelog.Errorf("Error while pulling container %v for task %v, will try to run anyway: %v", container, mtask, event.Error)
	} else {
		seelog.Warnf("Error with docker; stopping container %v for task %v: %v", container, mtask, event.Error)
		// Leave the container known status as it is when encountered transition error,
		// as we are not sure if the container status changed or not, we will get the actual
		// status change from the docker event stream
		container.SetKnownStatus(currentKnownStatus)
		container.SetDesiredStatus(api.ContainerStopped)
		// Container known status not changed, no need for further processing
		return false
	}
	return true
}

func (mtask *managedTask) steadyState() bool {
	taskKnownStatus := mtask.GetKnownStatus()
	return taskKnownStatus == api.TaskRunning && taskKnownStatus >= mtask.GetDesiredStatus()
}

// progressContainers tries to step forwards all containers that are able to be
// transitioned in the task's current state.
// It will continue listening to events from all channels while it does so, but
// none of those changes will be acted upon until this set of requests to
// docker completes.
// Container changes may also prompt the task status to change as well.
func (mtask *managedTask) progressContainers() {
	seelog.Debugf("Progressing task: %s", mtask.Task.String())
	// max number of transitions length to ensure writes will never block on
	// these and if we exit early transitions can exit the goroutine and it'll
	// get GC'd eventually
	transitionChange := make(chan bool, len(mtask.Containers))
	transitionChangeContainer := make(chan string, len(mtask.Containers))

	anyCanTransition, transitions, reasons := mtask.startContainerTransitions(
		func(container *api.Container, nextStatus api.ContainerStatus) {
			mtask.engine.transitionContainer(mtask.Task, container, nextStatus)
			transitionChange <- true
			transitionChangeContainer <- container.Name
		})

	if !anyCanTransition {
		if !mtask.waitForExecutionCredentialsFromACS(reasons) {
			mtask.onContainersUnableToTransitionState()
		}
		return
	}

	// We've kicked off one or more transitions, wait for them to
	// complete, but keep reading events as we do.. in fact, we have to for
	// transitions to complete
	mtask.waitForContainerTransitions(transitions, transitionChange, transitionChangeContainer)
	seelog.Debugf("Done transitioning all containers for task %v", mtask.Task)

	// update the task status
	changed := mtask.UpdateStatus()
	if changed {
		seelog.Debug("Container change also resulted in task change for task %v", mtask.Task)
		// If knownStatus changed, let it be known
		mtask.engine.emitTaskEvent(mtask.Task, "")
	}
}

// waitForExecutionCredentialsFromACS checks if the container that can't be transitioned
// was caused by waiting for credentials and start waiting
func (mtask *managedTask) waitForExecutionCredentialsFromACS(reasons []error) bool {
	for _, reason := range reasons {
		if reason == dependencygraph.UnableTransitionExecutionCredentialsNotResolved {
			seelog.Debugf("Waiting for credentials to pull from ECR, task: %s", mtask.Task.String())

			maxWait := make(chan bool, 1)
			timer := mtask.time().AfterFunc(waitForPullCredentialsTimeout, func() {
				maxWait <- true
			})
			// Have a timeout in case we missed the acs message but the credentials
			// were already in the credentials manager
			if !mtask.waitEvent(maxWait) {
				timer.Stop()
			}
			return true
		}
	}
	return false
}

// startContainerTransitions steps through each container in the task and calls
// the passed transition function when a transition should occur.
func (mtask *managedTask) startContainerTransitions(transitionFunc containerTransitionFunc) (bool, map[string]api.ContainerStatus, []error) {
	anyCanTransition := false
	var reasons []error
	transitions := make(map[string]api.ContainerStatus)
	for _, cont := range mtask.Containers {
		transition := mtask.containerNextState(cont)
		if transition.reason != nil {
			// container can't be transitioned
			reasons = append(reasons, transition.reason)
			continue
		}
		// At least one container is able to be moved forwards, so we're not deadlocked
		anyCanTransition = true

		if !transition.actionRequired {
			mtask.handleContainerChange(dockerContainerChange{cont, DockerContainerChangeEvent{Status: transition.nextState}})
			continue
		}
		transitions[cont.Name] = transition.nextState
		go transitionFunc(cont, transition.nextState)
	}

	return anyCanTransition, transitions, reasons
}

type containerTransitionFunc func(container *api.Container, nextStatus api.ContainerStatus)

// containerNextState determines the next state a container should go to.
// It returns a transition struct including the information:
// * container state it should transition to,
// * a bool indicating whether any action is required
// * an error indicate why this transition can't happend
//
// 'Stopped, false, ""' -> "You can move it to known stopped, but you don't have to call a transition function"
// 'Running, true, ""' -> "You can move it to running and you need to call the transition function"
// 'None, false, containerDependencyNotResolved' -> "This should not be moved; it has unresolved dependencies;"
//
// Next status is determined by whether the known and desired statuses are
// equal, the next numerically greater (iota-wise) status, and whether
// dependencies are fully resolved.
func (mtask *managedTask) containerNextState(container *api.Container) *containerTransition {
	containerKnownStatus := container.GetKnownStatus()
	containerDesiredStatus := container.GetDesiredStatus()

	if containerKnownStatus == containerDesiredStatus {
		seelog.Debugf("Container at desired status, desired status: %s, container: %s", containerDesiredStatus, container.String())
		return &containerTransition{
			nextState:      api.ContainerStatusNone,
			actionRequired: false,
			reason:         dependencygraph.UnableTransitionContainerPassedDesiredStatus,
		}
	}

	if containerKnownStatus > containerDesiredStatus {
		seelog.Debugf("Container transitioned, desired status: %s, container: %s", containerDesiredStatus, container.String())
		return &containerTransition{
			nextState:      api.ContainerStatusNone,
			actionRequired: false,
			reason:         dependencygraph.UnableTransitionContainerPassedDesiredStatus,
		}
	}
	if err := dependencygraph.DependenciesAreResolved(container, mtask.Containers, mtask.Task.GetExecutionCredentialsID(), mtask.engine.credentialsManager); err != nil {
		seelog.Debug("Can not apply state to container yet; dependencies unresolved, container: %s, task: %s, err: %v", container.String(), mtask.Task.Arn, err)
		return &containerTransition{
			nextState:      api.ContainerStatusNone,
			actionRequired: false,
			reason:         err,
		}
	}

	var nextState api.ContainerStatus
	if container.DesiredTerminal() {
		nextState = api.ContainerStopped
		// It's not enough to just check if container is in steady state here
		// we should really check if >= RUNNING <= STOPPED
		if !container.IsRunning() {
			// If it's not currently running we do not need to do anything to make it become stopped.
			return &containerTransition{
				nextState:      nextState,
				actionRequired: false,
			}
		}
	} else {
		nextState = container.GetNextKnownStateProgression()
	}
	return &containerTransition{
		nextState:      nextState,
		actionRequired: true,
	}
}

func (mtask *managedTask) onContainersUnableToTransitionState() {
	log.Crit("Task in a bad state; it's not steadystate but no containers want to transition", "task", mtask.Task)
	if mtask.GetDesiredStatus().Terminal() {
		// Ack, really bad. We want it to stop but the containers don't think
		// that's possible... let's just break out and hope for the best!
		log.Crit("The state is so bad that we're just giving up on it")
		mtask.SetKnownStatus(api.TaskStopped)
		mtask.engine.emitTaskEvent(mtask.Task, taskUnableToTransitionToStoppedReason)
		// TODO we should probably panic here
	} else {
		log.Crit("Moving task to stopped due to bad state", "task", mtask.Task)
		mtask.handleDesiredStatusChange(api.TaskStopped, 0)
	}
}

func (mtask *managedTask) waitForContainerTransitions(transitions map[string]api.ContainerStatus, transitionChange <-chan bool, transitionChangeContainer <-chan string) {
	for len(transitions) > 0 {
		if mtask.waitEvent(transitionChange) {
			changedContainer := <-transitionChangeContainer
			log.Debug("Transition for container finished", "task", mtask.Task, "container", changedContainer)
			delete(transitions, changedContainer)
			log.Debug("Still waiting for", "map", transitions)
		}
		if mtask.GetDesiredStatus().Terminal() || mtask.GetKnownStatus().Terminal() {
			allWaitingOnPulled := true
			for _, desired := range transitions {
				if desired != api.ContainerPulled {
					allWaitingOnPulled = false
				}
			}
			if allWaitingOnPulled {
				// We don't actually care to wait for 'pull' transitions to finish if
				// we're just heading to stopped since those resources aren't
				// inherently linked to this task anyways for e.g. gc and so on.
				log.Debug("All waiting is for pulled transition; exiting early",
					"map", transitions, "task", mtask.Task)
				break
			}
		}
	}
}

func (mtask *managedTask) time() ttime.Time {
	mtask._timeOnce.Do(func() {
		if mtask._time == nil {
			mtask._time = &ttime.DefaultTime{}
		}
	})
	return mtask._time
}

func (mtask *managedTask) cleanupTask(taskStoppedDuration time.Duration) {
	cleanupTimeDuration := mtask.GetKnownStatusTime().Add(taskStoppedDuration).Sub(ttime.Now())
	// There is a potential deadlock here if cleanupTime is negative. Ignore the computed
	// value in this case in favor of the default config value.
	if cleanupTimeDuration < 0 {
		log.Debug("Task Cleanup Duration is too short. Resetting to " + config.DefaultTaskCleanupWaitDuration.String())
		cleanupTimeDuration = config.DefaultTaskCleanupWaitDuration
	}
	cleanupTime := mtask.time().After(cleanupTimeDuration)
	cleanupTimeBool := make(chan bool)
	go func() {
		<-cleanupTime
		cleanupTimeBool <- true
		close(cleanupTimeBool)
	}()
	// wait for the cleanup time to elapse, signalled by cleanupTimeBool
	for !mtask.waitEvent(cleanupTimeBool) {
	}

	// wait for api.TaskStopped to be sent
	ok := mtask.waitForStopReported()
	if !ok {
		seelog.Errorf("Aborting cleanup for task %v as it is not reported stopped.  SentStatus: %v", mtask, mtask.GetSentStatus())
		return
	}

	log.Info("Cleaning up task's containers and data", "task", mtask.Task)

	// For the duration of this, simply discard any task events; this ensures the
	// speedy processing of other events for other tasks
	handleCleanupDone := make(chan struct{})
	// discard events while the task is being removed from engine state
	go mtask.discardEventsUntil(handleCleanupDone)
	mtask.engine.sweepTask(mtask.Task)

	if mtask.engine.cfg.TaskCPUMemLimit.Enabled() {
		err := mtask.resource.Cleanup(mtask.Task)
		if err != nil {
			seelog.Warnf("Unable to cleanup platform resources for task %s: %v", mtask.Task.Arn, err)
		}
	}

	// Now remove ourselves from the global state and cleanup channels
	mtask.engine.processTasks.Lock()
	mtask.engine.state.RemoveTask(mtask.Task)
	eni := mtask.Task.GetTaskENI()
	if eni == nil {
		seelog.Debugf("No eni associated with task: [%s]", mtask.Task.String())
	} else {
		seelog.Debugf("Removing the eni from agent state, task: [%s]", mtask.Task.String())
		mtask.engine.state.RemoveENIAttachment(eni.MacAddress)
	}
	seelog.Debugf("Finished removing task data, removing task from managed tasks: %v", mtask.Task)
	delete(mtask.engine.managedTasks, mtask.Arn)
	handleCleanupDone <- struct{}{}
	mtask.engine.processTasks.Unlock()
	mtask.engine.saver.Save()

	// Cleanup any leftover messages before closing their channels. No new
	// messages possible because we deleted ourselves from managedTasks, so this
	// removes all stale ones
	mtask.discardPendingMessages()

	close(mtask.dockerMessages)
	close(mtask.acsMessages)
}

func (mtask *managedTask) discardEventsUntil(done chan struct{}) {
	for {
		select {
		case <-mtask.dockerMessages:
		case <-mtask.acsMessages:
		case <-done:
			return
		}
	}
}

func (mtask *managedTask) discardPendingMessages() {
	for {
		select {
		case <-mtask.dockerMessages:
		case <-mtask.acsMessages:
		default:
			return
		}
	}
}

var _stoppedSentWaitInterval = stoppedSentWaitInterval
var _maxStoppedWaitTimes = int(maxStoppedWaitTimes)

// waitForStopReported will wait for the task to be reported stopped and return true, or will time-out and return false.
// Messages on the mtask.dockerMessages and mtask.acsMessages channels will be handled while this function is waiting.
func (mtask *managedTask) waitForStopReported() bool {
	stoppedSentBool := make(chan bool)
	taskStopped := false
	go func() {
		for i := 0; i < _maxStoppedWaitTimes; i++ {
			// ensure that we block until api.TaskStopped is actually sent
			sentStatus := mtask.GetSentStatus()
			if sentStatus >= api.TaskStopped {
				taskStopped = true
				break
			}
			seelog.Warnf("Blocking cleanup for task %v until the task has been reported stopped. SentStatus: %v (%d/%d)", mtask, sentStatus, i+1, _maxStoppedWaitTimes)
			mtask._time.Sleep(_stoppedSentWaitInterval)
		}
		stoppedSentBool <- true
		close(stoppedSentBool)
	}()
	// wait for api.TaskStopped to be sent
	for !mtask.waitEvent(stoppedSentBool) {
	}
	return taskStopped
}
