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

// Package handler deals with appropriately reacting to all ACS messages as well
// as maintaining the connection to ACS.
package eventstream

import (
	"context"
	"fmt"
	"sync"

	"github.com/cihub/seelog"
)

type eventHandler func(...interface{}) error

// EventStream waiting for events and notifying the listeners by invoking
// the handler that listeners registered
type EventStream struct {
	name         string
	open         bool
	event        chan interface{}
	handlers     map[string]eventHandler
	ctx          context.Context
	handlersLock sync.RWMutex
	statusLock   sync.RWMutex
}

func NewEventStream(name string, ctx context.Context) *EventStream {
	return &EventStream{
		name:     name,
		event:    make(chan interface{}, 1),
		ctx:      ctx,
		open:     false,
		handlers: make(map[string]eventHandler),
	}
}

// Subscribe adds the handler to be called into EventStream
func (eventStream *EventStream) Subscribe(name string, handler eventHandler) error {
	eventStream.handlersLock.Lock()
	defer eventStream.handlersLock.Unlock()

	if _, ok := eventStream.handlers[name]; ok {
		return fmt.Errorf("handler %s already exists", name)
	}

	eventStream.handlers[name] = handler
	return nil
}

// broadcast calls all handler's handler function
func (eventStream *EventStream) broadcast(event interface{}) {
	eventStream.handlersLock.RLock()
	defer eventStream.handlersLock.RUnlock()

	seelog.Debugf("Event stream %s received events, broadcasting to listeners...", eventStream.name)

	for _, handlerFunc := range eventStream.handlers {
		go handlerFunc(event)
	}
}

// Unsubscribe deletes the handler from the EventStream
func (eventStream *EventStream) Unsubscribe(name string) {
	eventStream.handlersLock.Lock()
	defer eventStream.handlersLock.Unlock()

	for handler := range eventStream.handlers {
		if handler == name {
			seelog.Debugf("Unsubscribing event handler %s from event stream %s", handler, eventStream.name)
			delete(eventStream.handlers, handler)
			return
		}
	}
}

// WriteToEventStream writes event to the event stream
func (eventStream *EventStream) WriteToEventStream(event interface{}) error {
	eventStream.statusLock.RLock()
	defer eventStream.statusLock.RUnlock()

	if !eventStream.open {
		return fmt.Errorf("Event stream is closed")
	}
	eventStream.event <- event
	return nil
}

// Context returns the context of event stream
func (eventStream *EventStream) Context() context.Context {
	return eventStream.ctx
}

// listen listens to the event channel
func (eventStream *EventStream) listen() {
	seelog.Infof("Event stream %s start listening...", eventStream.name)
	for {
		select {
		case event := <-eventStream.event:
			eventStream.broadcast(event)
		case <-eventStream.ctx.Done():
			seelog.Infof("Event stream %s stopped listening...", eventStream.name)

			eventStream.statusLock.Lock()
			eventStream.open = false
			close(eventStream.event)
			eventStream.statusLock.Unlock()
			return
		}
	}
}

// StartListening mark the event stream as open and start listening
func (eventStream *EventStream) StartListening() {
	eventStream.statusLock.Lock()
	defer eventStream.statusLock.Unlock()
	eventStream.open = true

	go eventStream.listen()
}
