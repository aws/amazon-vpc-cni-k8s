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

package engine

import (
	docker "github.com/fsouza/go-dockerclient"
	"sync"
)

const (
	// TODO  add support for filter in go-dockerclient
	containerTypeEvent = "container"
)

var containerEvents = []string{"create", "start", "stop", "die", "restart", "oom"}

// InfiniteBuffer defines an unlimited buffer, where it reads from
// input channel and write to output channel.
type InfiniteBuffer struct {
	events       []*docker.APIEvents
	empty        bool
	waitForEvent sync.WaitGroup
	count        int
	lock         sync.RWMutex
}

// NewInfiniteBuffer returns an InfiniteBuffer object
func NewInfiniteBuffer() *InfiniteBuffer {
	return &InfiniteBuffer{}
}

// StartListening starts reading from the input channel and writes to the buffer
func (buffer *InfiniteBuffer) StartListening(events chan *docker.APIEvents) {
	for event := range events {
		go buffer.CopyEvents(event)
	}
}

// CopyEvents copies the event into the buffer
func (buffer *InfiniteBuffer) CopyEvents(event *docker.APIEvents) {
	if event.ID == "" || event.Type != containerTypeEvent {
		return
	}

	// Only add the events agent is interested
	for _, containerEvent := range containerEvents {
		if event.Status == containerEvent {
			buffer.lock.Lock()
			defer buffer.lock.Unlock()

			buffer.events = append(buffer.events, event)
			// Check if there is consumer waiting for events
			if buffer.empty {
				buffer.empty = false

				// Unblock the consumer
				buffer.waitForEvent.Done()
			}
			return
		}
	}
}

// Consume reads the buffer and write to a listener channel
func (buffer *InfiniteBuffer) Consume(in chan<- *docker.APIEvents) {
	for {
		buffer.lock.Lock()

		if len(buffer.events) == 0 {
			// Mark the buffer as empty and start waiting for events
			buffer.empty = true
			buffer.waitForEvent.Add(1)
			buffer.lock.Unlock()
			buffer.waitForEvent.Wait()
		} else {
			event := buffer.events[0]
			buffer.events = buffer.events[1:]
			buffer.lock.Unlock()

			// Send event to the buffer listener
			in <- event
		}
	}
}
