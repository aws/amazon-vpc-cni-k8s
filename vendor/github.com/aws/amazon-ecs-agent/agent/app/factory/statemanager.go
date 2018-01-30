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

package factory

import (
	"github.com/aws/amazon-ecs-agent/agent/config"
	"github.com/aws/amazon-ecs-agent/agent/statemanager"
)

// StateManager factory wraps the global statemenager.NewStateManager method,
// which creates a new state manager object
type StateManager interface {
	// NewStateManager creates a new state manager
	NewStateManager(cfg *config.Config, options ...statemanager.Option) (statemanager.StateManager, error)
}

// SaveableOption factory wraps the global statemanager.AddSaveable method,
// which creates a new saveable state manager option
type SaveableOption interface {
	// AddSaveable is an option that adds a given saveable as one that should be saved
	// under the given name
	AddSaveable(name string, saveable statemanager.Saveable) statemanager.Option
}

type stateManager struct{}

// NewStateManager creates a new StateManager factory
func NewStateManager() StateManager {
	return &stateManager{}
}

func (*stateManager) NewStateManager(cfg *config.Config, options ...statemanager.Option) (statemanager.StateManager, error) {
	return statemanager.NewStateManager(cfg, options...)
}

type saveableOption struct{}

// NewSaveableOption creates a new SaveableOption factory
func NewSaveableOption() SaveableOption {
	return &saveableOption{}
}

func (*saveableOption) AddSaveable(name string, saveable statemanager.Saveable) statemanager.Option {
	return statemanager.AddSaveable(name, saveable)
}
