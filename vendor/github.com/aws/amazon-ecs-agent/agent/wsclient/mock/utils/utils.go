// Copyright 2015-2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//     http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package utils

import (
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/gorilla/websocket"
)

// GetMockServer retuns a mock websocket server that can be started up as TLS or not.
// TODO replace with gomock
func GetMockServer(closeWS <-chan []byte) (*httptest.Server, chan<- string, <-chan string, <-chan error, error) {
	serverChan := make(chan string)
	requestsChan := make(chan string)
	errChan := make(chan error)
	stopListen := make(chan bool)

	upgrader := websocket.Upgrader{ReadBufferSize: 1024, WriteBufferSize: 1024}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		go func() {
			ws.WriteControl(websocket.CloseMessage, <-closeWS, time.Now().Add(time.Second))
			close(stopListen)
		}()
		if err != nil {
			errChan <- err
		}
		go func() {
			for {
				select {
				case <-stopListen:
					return
				default:
					_, msg, err := ws.ReadMessage()
					if err != nil {
						errChan <- err
					} else {
						requestsChan <- string(msg)
					}
				}
			}
		}()
		for str := range serverChan {
			err := ws.WriteMessage(websocket.TextMessage, []byte(str))
			if err != nil {
				errChan <- err
			}
		}
	})

	server := httptest.NewUnstartedServer(handler)
	return server, serverChan, requestsChan, errChan, nil
}
