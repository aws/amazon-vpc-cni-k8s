// Copyright 2014-2015 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

// Package sighandlers handle signals and behave appropriately.
// SIGTERM:
//   Flush state to disk and exit
// SIGUSR1:
//   Print a dump of goroutines to the logger and DON'T exit
package sighandlers

import (
	"errors"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aws/amazon-ecs-agent/agent/engine"
	"github.com/aws/amazon-ecs-agent/agent/sighandlers/exitcodes"
	"github.com/aws/amazon-ecs-agent/agent/statemanager"
	"github.com/aws/amazon-ecs-agent/agent/utils"

	"github.com/cihub/seelog"
)

const (
	engineDisableTimeout = 5 * time.Second
	finalSaveTimeout     = 3 * time.Second
)

// TerminationHandler defines a handler used for terminating the agent
type TerminationHandler func(saver statemanager.Saver, taskEngine engine.TaskEngine)

// StartDefaultTerminationHandler defines a default termination handler suitable for running in a process
func StartDefaultTerminationHandler(saver statemanager.Saver, taskEngine engine.TaskEngine) {
	signalChannel := make(chan os.Signal, 2)
	signal.Notify(signalChannel, os.Interrupt, syscall.SIGTERM)

	sig := <-signalChannel
	seelog.Debugf("Termination handler received termination signal: %s", sig.String())

	err := FinalSave(saver, taskEngine)
	if err != nil {
		seelog.Criticalf("Error saving state before final shutdown: %v", err)
		// Terminal because it's a sigterm; the user doesn't want it to restart
		os.Exit(exitcodes.ExitTerminal)
	}
	os.Exit(exitcodes.ExitSuccess)
}

// FinalSave should be called immediately before exiting, and only before
// exiting, in order to flush tasks to disk. It waits a short timeout for state
// to settle if necessary. If unable to reach a steady-state and save within
// this short timeout, it returns an error
func FinalSave(saver statemanager.Saver, taskEngine engine.TaskEngine) error {
	engineDisabled := make(chan error)

	disableTimer := time.AfterFunc(engineDisableTimeout, func() {
		engineDisabled <- errors.New("final save: timed out waiting for TaskEngine to settle")
	})

	go func() {
		seelog.Debug("Shutting down task engine for final save")
		taskEngine.Disable()
		disableTimer.Stop()
		engineDisabled <- nil
	}()

	disableErr := <-engineDisabled

	stateSaved := make(chan error)
	saveTimer := time.AfterFunc(finalSaveTimeout, func() {
		stateSaved <- errors.New("final save: timed out trying to save to disk")
	})
	go func() {
		seelog.Debug("Saving state before shutting down")
		stateSaved <- saver.ForceSave()
		saveTimer.Stop()
	}()

	saveErr := <-stateSaved

	if disableErr != nil || saveErr != nil {
		return utils.NewMultiError(disableErr, saveErr)
	}
	return nil
}
