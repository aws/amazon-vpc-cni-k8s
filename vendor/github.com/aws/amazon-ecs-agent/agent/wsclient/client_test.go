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

package wsclient

import (
	"io"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/aws/amazon-ecs-agent/agent/acs/model/ecsacs"
	"github.com/aws/amazon-ecs-agent/agent/config"
	"github.com/aws/amazon-ecs-agent/agent/wsclient/mock/utils"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"

	"github.com/gorilla/websocket"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const dockerEndpoint = "/var/run/docker.sock"

// TestConcurrentWritesDontPanic will force a panic in the websocket library if
// the implemented methods don't handle concurrency correctly
// See https://godoc.org/github.com/gorilla/websocket#hdr-Concurrency
func TestConcurrentWritesDontPanic(t *testing.T) {
	closeWS := make(chan []byte)
	defer close(closeWS)

	mockServer, _, requests, _, _ := utils.GetMockServer(closeWS)
	mockServer.StartTLS()
	defer mockServer.Close()

	var waitForRequests sync.WaitGroup
	waitForRequests.Add(1)

	go func() {
		for i := 0; i < 20; i++ {
			<-requests
		}
		waitForRequests.Done()
	}()
	req := ecsacs.AckRequest{Cluster: aws.String("test"), ContainerInstance: aws.String("test"), MessageId: aws.String("test")}

	cs := getClientServer(mockServer.URL)
	require.NoError(t, cs.Connect())

	executeTenRequests := func() {
		for i := 0; i < 10; i++ {
			assert.NoError(t, cs.MakeRequest(&req))
		}
	}

	// Make requests from two separate routines to try and force a
	// concurrent write
	go executeTenRequests()
	go executeTenRequests()

	t.Log("Waiting for all 20 requests to succeed")
	waitForRequests.Wait()
}

// TestProxyVariableCustomValue ensures that a user is able to override the
// proxy variable by setting an environment variable
func TestProxyVariableCustomValue(t *testing.T) {
	closeWS := make(chan []byte)
	defer close(closeWS)

	mockServer, _, _, _, _ := utils.GetMockServer(closeWS)
	mockServer.StartTLS()
	defer mockServer.Close()

	testString := "Custom no proxy string"
	os.Setenv("NO_PROXY", testString)
	require.NoError(t, getClientServer(mockServer.URL).Connect())

	assert.Equal(t, os.Getenv("NO_PROXY"), testString, "NO_PROXY should match user-supplied variable")
}

// TestProxyVariableDefaultValue verifies that NO_PROXY gets overridden if it
// isn't already set
func TestProxyVariableDefaultValue(t *testing.T) {
	closeWS := make(chan []byte)
	defer close(closeWS)

	mockServer, _, _, _, _ := utils.GetMockServer(closeWS)
	mockServer.StartTLS()
	defer mockServer.Close()

	os.Unsetenv("NO_PROXY")
	getClientServer(mockServer.URL).Connect()

	expectedEnvVar := "169.254.169.254,169.254.170.2," + dockerEndpoint

	assert.Equal(t, os.Getenv("NO_PROXY"), expectedEnvVar, "Variable NO_PROXY expected to be overwritten when no default value supplied")
}

// TestHandleMessagePermissibleCloseCode ensures that permissible close codes
// are wrapped in io.EOF
func TestHandleMessagePermissibleCloseCode(t *testing.T) {
	closeWS := make(chan []byte)
	defer close(closeWS)

	messageError := make(chan error)
	mockServer, _, _, _, _ := utils.GetMockServer(closeWS)
	mockServer.StartTLS()
	cs := getClientServer(mockServer.URL)
	require.NoError(t, cs.Connect())

	go func() {
		messageError <- cs.ConsumeMessages()
	}()

	closeWS <- websocket.FormatCloseMessage(websocket.CloseNormalClosure, ":)")
	assert.EqualError(t, <-messageError, io.EOF.Error(), "expected EOF for normal close code")
}

// TestHandleMessageUnexpectedCloseCode checks that unexpected close codes will
// be returned as is (not wrapped in io.EOF)
func TestHandleMessageUnexpectedCloseCode(t *testing.T) {
	closeWS := make(chan []byte)
	defer close(closeWS)

	messageError := make(chan error)
	mockServer, _, _, _, _ := utils.GetMockServer(closeWS)
	mockServer.StartTLS()
	cs := getClientServer(mockServer.URL)
	require.NoError(t, cs.Connect())

	go func() {
		messageError <- cs.ConsumeMessages()
	}()

	closeWS <- websocket.FormatCloseMessage(websocket.CloseTryAgainLater, ":(")
	assert.True(t, websocket.IsCloseError(<-messageError, websocket.CloseTryAgainLater), "Expected error from websocket library")
}

// TestHandlNonHTTPSEndpoint verifies that the wsclient can handle communication over
// an HTTP (so WS) connection
func TestHandleNonHTTPSEndpoint(t *testing.T) {
	closeWS := make(chan []byte)
	defer close(closeWS)

	mockServer, _, requests, _, _ := utils.GetMockServer(closeWS)
	mockServer.Start()
	defer mockServer.Close()

	cs := getClientServer(mockServer.URL)
	require.NoError(t, cs.Connect())

	req := ecsacs.AckRequest{Cluster: aws.String("test"), ContainerInstance: aws.String("test"), MessageId: aws.String("test")}
	cs.MakeRequest(&req)

	t.Log("Waiting for single request to be visible server-side")
	<-requests
}

// TestHandleIncorrectHttpScheme checks that an incorrect URL scheme results in
// an error
func TestHandleIncorrectURLScheme(t *testing.T) {
	closeWS := make(chan []byte)
	defer close(closeWS)

	mockServer, _, _, _, _ := utils.GetMockServer(closeWS)
	mockServer.StartTLS()
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	mockServerURL.Scheme = "notaparticularlyrealscheme"

	cs := getClientServer(mockServerURL.String())
	err := cs.Connect()

	assert.Error(t, err, "Expected error for incorrect URL scheme")
}

// TestWebsocketScheme checks that websocketScheme handles valid and invalid mappings
// correctly
func TestWebsocketScheme(t *testing.T) {
	// test valid schemes
	validMappings := map[string]string{
		"http":  "ws",
		"https": "wss",
	}

	for input, expectedOutput := range validMappings {
		actualOutput, err := websocketScheme(input)

		assert.NoError(t, err, "Unexpected error for valid http scheme")
		assert.Equal(t, actualOutput, expectedOutput, "Valid http schemes should map to a websocket scheme")
	}

	// test an invalid mapping
	_, err := websocketScheme("highly-likely-to-be-junk")
	assert.Error(t, err, "Expected error for invalid http scheme")
}

func getClientServer(url string) *ClientServerImpl {
	types := []interface{}{ecsacs.AckRequest{}}

	return &ClientServerImpl{
		URL: url,
		AgentConfig: &config.Config{
			AcceptInsecureCert: true,
			AWSRegion:          "us-east-1",
			DockerEndpoint:     "unix://" + dockerEndpoint,
		},
		CredentialProvider: credentials.AnonymousCredentials,
		TypeDecoder:        BuildTypeDecoder(types),
		RWTimeout:          time.Second,
	}
}
