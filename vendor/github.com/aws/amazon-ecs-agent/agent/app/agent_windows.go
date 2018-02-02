// +build windows

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

package app

import (
	"context"
	"sync"
	"time"

	"github.com/aws/amazon-ecs-agent/agent/engine"
	"github.com/aws/amazon-ecs-agent/agent/sighandlers"
	"github.com/aws/amazon-ecs-agent/agent/sighandlers/exitcodes"
	"github.com/aws/amazon-ecs-agent/agent/statemanager"
	"github.com/cihub/seelog"
	"golang.org/x/sys/windows/svc"
)

const (
	//EcsSvcName is the name of the service
	EcsSvcName = "AmazonECS"
)

// startWindowsService runs the ECS agent as a Windows Service
func (agent *ecsAgent) startWindowsService() int {
	svc.Run(EcsSvcName, newHandler(agent))
	return 0
}

// handler implements https://godoc.org/golang.org/x/sys/windows/svc#Handler
type handler struct {
	ecsAgent agent
}

func newHandler(agent agent) *handler {
	return &handler{agent}
}

// Execute implements https://godoc.org/golang.org/x/sys/windows/svc#Handler
// The basic way that this implementation works is through two channels (representing the requests from Windows and the
// responses we're sending to Windows) and two goroutines (one for message processing with Windows and the other to
// actually run the agent).  Once we've set everything up and started both goroutines, we wait for either one to exit
// (the Windows goroutine will exit based on messages from Windows while the agent goroutine exits if the agent exits)
// and then cancel the other.  Once everything has stopped running, this function returns and the process exits.
func (h *handler) Execute(args []string, requests <-chan svc.ChangeRequest, responses chan<- svc.Status) (bool, uint32) {
	defer seelog.Flush()
	// channels for communication between goroutines
	ctx, cancel := context.WithCancel(context.Background())
	agentDone := make(chan struct{})
	windowsDone := make(chan struct{})
	wg := sync.WaitGroup{}
	wg.Add(2)

	go func() {
		defer close(windowsDone)
		defer wg.Done()
		h.handleWindowsRequests(ctx, requests, responses)
	}()

	var agentExitCode uint32
	go func() {
		defer close(agentDone)
		defer wg.Done()
		agentExitCode = h.runAgent(ctx)
	}()

	// Wait until one of the goroutines is either told to stop or fails spectacularly.  Under normal conditions we will
	// be waiting here for a long time.
	select {
	case <-windowsDone:
		// Service was told to stop by the Windows API.  This happens either through manual intervention (i.e.,
		// "Stop-Service AmazonECS") or through system shutdown.  Regardless, this is a normal exit event and not an
		// error.
		seelog.Info("Received normal signal from Windows to exit")
	case <-agentDone:
		// This means that the agent stopped on its own.  This is where it's appropriate to light the event log on fire
		// and set off all the alarms.
		seelog.Errorf("Exiting %d", agentExitCode)
	}
	cancel()
	wg.Wait()
	seelog.Infof("Bye Bye! Exiting with %d", agentExitCode)
	return true, agentExitCode
}

// handleWindowsRequests is a loop intended to run in a goroutine.  It handles bidirectional communication with the
// Windows service manager.  This function works by pretty much immediately moving to running and then waiting for a
// stop or shut down message from Windows or to be canceled (which could happen if the agent exits by itself and the
// calling function cancels the context).
func (h *handler) handleWindowsRequests(ctx context.Context, requests <-chan svc.ChangeRequest, responses chan<- svc.Status) {
	// Immediately tell Windows that we are pending service start.
	responses <- svc.Status{State: svc.StartPending}
	seelog.Info("Starting Windows service")

	// TODO: Pre-start hooks go here (unclear if we need any yet)

	// https://msdn.microsoft.com/en-us/library/windows/desktop/ms682108(v=vs.85).aspx
	// Not sure if a better link exists to describe what these values mean
	accepts := svc.AcceptStop | svc.AcceptShutdown

	// Announce that we are running and we accept the above-mentioned commands
	responses <- svc.Status{State: svc.Running, Accepts: accepts}

	defer func() {
		// Announce that we are stopping
		seelog.Info("Stopping Windows service")
		responses <- svc.Status{State: svc.StopPending}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case r := <-requests:
			switch r.Cmd {
			case svc.Interrogate:
				// Our status doesn't change unless we are told to stop or shutdown
				responses <- r.CurrentStatus
			case svc.Stop, svc.Shutdown:
				return
			default:
				continue
			}
		}
	}
}

// runAgent runs the ECS agent inside a goroutine and waits to be told to exit.
func (h *handler) runAgent(ctx context.Context) uint32 {
	agentCtx, cancel := context.WithCancel(ctx)
	indicator := newTermHandlerIndicator()

	terminationHandler := func(saver statemanager.Saver, taskEngine engine.TaskEngine) {
		// We're using a custom indicator to record that the handler is scheduled to be executed (has been invoked) and
		// to determine whether it should run (we skip when the agent engine has already exited).  After recording to
		// the indicator that the handler has been invoked, we wait on the context.  When we wake up, we determine
		// whether to execute or not based on whether the agent is still running.
		defer indicator.finish()
		indicator.setInvoked()
		<-agentCtx.Done()
		if !indicator.isAgentRunning() {
			return
		}

		seelog.Info("Termination handler received signal to stop")
		err := sighandlers.FinalSave(saver, taskEngine)
		if err != nil {
			seelog.Criticalf("Error saving state before final shutdown: %v", err)
		}
	}
	h.ecsAgent.setTerminationHandler(terminationHandler)

	go func() {
		defer cancel()
		exitCode := h.ecsAgent.start() // should block forever, unless there is an error

		if exitCode == exitcodes.ExitTerminal {
			seelog.Critical("Terminal exit code received from agent.  Windows SCM will not restart the AmazonECS service.")
			// We override the exit code to 0 here so that Windows does not treat this as a restartable failure even
			// when "sc.exe failureflag" is set.
			exitCode = 0
		}

		indicator.agentStopped(exitCode)
	}()

	sleepCtx(agentCtx, time.Minute) // give the agent a minute to start and invoke terminationHandler

	// wait for the termination handler to run.  Once the termination handler runs, we can safely exit.  If the agent
	// exits by itself, the termination handler doesn't need to do anything and skips.  If the agent exits before the
	// termination handler is invoked, we can exit immediately.
	return indicator.wait()
}

// sleepCtx provides a cancelable sleep
func sleepCtx(ctx context.Context, duration time.Duration) {
	derivedCtx, _ := context.WithDeadline(ctx, time.Now().Add(duration))
	<-derivedCtx.Done()
}

type termHandlerIndicator struct {
	mu             sync.Mutex
	agentRunning   bool
	exitCode       uint32
	handlerInvoked bool
	handlerDone    chan struct{}
}

func newTermHandlerIndicator() *termHandlerIndicator {
	return &termHandlerIndicator{
		agentRunning:   true,
		handlerInvoked: false,
		handlerDone:    make(chan struct{}),
	}
}

func (t *termHandlerIndicator) isAgentRunning() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.agentRunning
}

func (t *termHandlerIndicator) agentStopped(exitCode int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.agentRunning = false
	t.exitCode = uint32(exitCode)
}

func (t *termHandlerIndicator) finish() {
	close(t.handlerDone)
}

func (t *termHandlerIndicator) setInvoked() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.handlerInvoked = true
}

func (t *termHandlerIndicator) wait() uint32 {
	t.mu.Lock()
	invoked := t.handlerInvoked
	t.mu.Unlock()
	if invoked {
		<-t.handlerDone
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.exitCode
}
