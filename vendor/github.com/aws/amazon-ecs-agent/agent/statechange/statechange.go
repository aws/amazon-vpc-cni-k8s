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

package statechange

const (
	// ContainerEvent is used to define the container state transition events
	// emitted by the engine
	ContainerEvent EventType = iota

	// TaskEvent is used to define the task state transition events emitted by
	// the engine
	TaskEvent
)

// Event defines the type of state change event
type EventType int32

// Event is used to abstract away the two transition event types
// passed up through a single channel from the the engine
type Event interface {

	// GetEventType implementations should return one the enums defined above to
	// identify the type of event being emitted
	GetEventType() EventType
}
