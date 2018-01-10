// Copyright 2014-2016 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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
package eventstream

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestSubscribe tests the listener subscribed to the
// event stream will be notified
func TestSubscribe(t *testing.T) {
	waiter, listener := setupWaitGroupAndListener()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	waiter.Add(1)
	eventStream := NewEventStream("TestSubscribe", ctx)
	eventStream.Subscribe("listener", listener)
	eventStream.StartListening()

	err := eventStream.WriteToEventStream(struct{}{})
	assert.NoError(t, err)
	waiter.Wait()
}

// TestUnsubscribe tests the listener unsubscribed from the
// event steam will not be notified
func TestUnsubscribe(t *testing.T) {
	waiter1, listener1 := setupWaitGroupAndListener()
	waiter2, listener2 := setupWaitGroupAndListener()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	eventStream := NewEventStream("TestUnsubscribe", ctx)
	eventStream.Subscribe("listener1", listener1)
	eventStream.Subscribe("listener2", listener2)
	eventStream.StartListening()

	waiter1.Add(1)
	waiter2.Add(1)
	err := eventStream.WriteToEventStream(struct{}{})
	assert.NoError(t, err)
	waiter1.Wait()
	waiter2.Wait()

	eventStream.Unsubscribe("listener1")

	waiter2.Add(1)
	err = eventStream.WriteToEventStream(struct{}{})
	assert.NoError(t, err)

	waiter1.Wait()
	waiter2.Wait()
}

// TestCancelEventStream tests the event stream can
// be closed by context
func TestCancelEventStream(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	eventStream := NewEventStream("TestCancelEventStream", ctx)
	_, listener := setupWaitGroupAndListener()

	eventStream.Subscribe("listener", listener)
	eventStream.StartListening()
	cancel()

	time.Sleep(1 * time.Second)

	err := eventStream.WriteToEventStream(struct{}{})
	assert.Error(t, err)
}

// setupWaitGroupAndListener creates a waitgroup and a function
// that decrements a WaitGroup when called
func setupWaitGroupAndListener() (*sync.WaitGroup, func(...interface{}) error) {
	waiter := &sync.WaitGroup{}
	listener := func(...interface{}) error {
		waiter.Done()
		return nil
	}
	return waiter, listener
}
