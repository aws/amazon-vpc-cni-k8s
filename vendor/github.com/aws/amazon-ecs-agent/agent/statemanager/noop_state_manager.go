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

package statemanager

// NoopStateManager is a state manager that succeeds for all reads/writes without
// even trying; it allows disabling of state serialization by being a drop-in
// replacement so no other code need be concerned with it.
type NoopStateManager struct{}

// NewNoopStateManager constructs a state manager
func NewNoopStateManager() StateManager {
	return &NoopStateManager{}
}

// Save does nothing, successfully
func (nsm *NoopStateManager) Save() error {
	return nil
}

// ForceSave does nothing, successfully
func (nsm *NoopStateManager) ForceSave() error {
	return nil
}

// Load does nothing, successfully
func (nsm *NoopStateManager) Load() error {
	return nil
}
