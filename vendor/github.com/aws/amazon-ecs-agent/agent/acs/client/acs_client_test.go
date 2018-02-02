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

package acsclient

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aws/amazon-ecs-agent/agent/acs/model/ecsacs"
	"github.com/aws/amazon-ecs-agent/agent/config"
	"github.com/aws/amazon-ecs-agent/agent/wsclient"
	"github.com/aws/amazon-ecs-agent/agent/wsclient/mock"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/golang/mock/gomock"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
)

const (
	sampleCredentialsMessage = `
{
  "type": "IAMRoleCredentialsMessage",
  "message": {
    "messageId": "123",
    "clusterArn": "default",
    "taskArn": "t1",
    "roleCredentials": {
      "credentialsId": "credsId",
      "accessKeyId": "accessKeyId",
      "expiration": "2016-03-25T06:17:19.318+0000",
      "roleArn": "roleArn",
      "secretAccessKey": "secretAccessKey",
      "sessionToken": "token"
    }
  }
}
`
	sampleAttachENIMessage = `
{
  "type": "AttachTaskNetworkInterfacesMessage",
  "message": {
    "messageId": "123",
    "clusterArn": "default",
    "taskArn": "task",
    "elasticNetworkInterfaces":[{
      "attachmentArn": "attach_arn",
      "ec2Id": "eni_id",
      "ipv4Addresses":[{
        "primary": true,
        "privateAddress": "ipv4"
      }],
      "ipv6Addresses":[{
        "address": "ipv6"
      }],
      "macAddress": "mac"
    }]
  }
}
`
)

const (
	TestClusterArn  = "arn:aws:ec2:123:container/cluster:123456"
	TestInstanceArn = "arn:aws:ec2:123:container/containerInstance/12345678"
	rwTimeout       = time.Second
)

var testCfg = &config.Config{
	AcceptInsecureCert: true,
	AWSRegion:          "us-east-1",
}

func TestMakeUnrecognizedRequest(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	conn := mock_wsclient.NewMockWebsocketConn(ctrl)
	conn.EXPECT().SetWriteDeadline(gomock.Any()).Return(nil)
	conn.EXPECT().Close()

	cs := testCS(conn)
	defer cs.Close()
	// 'testing.T' should not be a known type ;)
	err := cs.MakeRequest(t)
	if _, ok := err.(*wsclient.UnrecognizedWSRequestType); !ok {
		t.Fatal("Expected unrecognized request type")
	}
}

func TestWriteAckRequest(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	conn := mock_wsclient.NewMockWebsocketConn(ctrl)
	conn.EXPECT().SetWriteDeadline(gomock.Any()).Return(nil).Times(2)
	conn.EXPECT().Close()
	cs := testCS(conn)
	defer cs.Close()

	// capture bytes written
	var writes []byte
	conn.EXPECT().WriteMessage(gomock.Any(), gomock.Any()).Do(func(_ int, data []byte) {
		writes = data
	})

	// send request
	err := cs.MakeRequest(&ecsacs.AckRequest{})
	assert.NoError(t, err)

	// unmarshal bytes written to the socket
	msg := &wsclient.RequestMessage{}
	err = json.Unmarshal(writes, msg)
	assert.NoError(t, err)
	assert.Equal(t, "AckRequest", msg.Type)
}

func TestPayloadHandlerCalled(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	conn := mock_wsclient.NewMockWebsocketConn(ctrl)
	// Messages should be read from the connection at least once
	conn.EXPECT().SetReadDeadline(gomock.Any()).Return(nil).MinTimes(1)
	conn.EXPECT().ReadMessage().Return(websocket.TextMessage,
		[]byte(`{"type":"PayloadMessage","message":{"tasks":[{"arn":"arn"}]}}`),
		nil).MinTimes(1)
	// Invoked when closing the connection
	conn.EXPECT().SetWriteDeadline(gomock.Any()).Return(nil)
	conn.EXPECT().Close()
	cs := testCS(conn)
	defer cs.Close()

	messageChannel := make(chan *ecsacs.PayloadMessage)
	reqHandler := func(payload *ecsacs.PayloadMessage) {
		messageChannel <- payload
	}
	cs.AddRequestHandler(reqHandler)
	go cs.Serve()

	expectedMessage := &ecsacs.PayloadMessage{
		Tasks: []*ecsacs.Task{{
			Arn: aws.String("arn"),
		}},
	}

	assert.Equal(t, expectedMessage, <-messageChannel)
}

func TestRefreshCredentialsHandlerCalled(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	conn := mock_wsclient.NewMockWebsocketConn(ctrl)
	// Messages should be read from the connection at least once
	conn.EXPECT().SetReadDeadline(gomock.Any()).Return(nil).MinTimes(1)
	conn.EXPECT().ReadMessage().Return(websocket.TextMessage,
		[]byte(sampleCredentialsMessage), nil).MinTimes(1)
	// Invoked when closing the connection
	conn.EXPECT().SetWriteDeadline(gomock.Any()).Return(nil)
	conn.EXPECT().Close()
	cs := testCS(conn)
	defer cs.Close()

	messageChannel := make(chan *ecsacs.IAMRoleCredentialsMessage)
	reqHandler := func(message *ecsacs.IAMRoleCredentialsMessage) {
		messageChannel <- message
	}
	cs.AddRequestHandler(reqHandler)

	go cs.Serve()

	expectedMessage := &ecsacs.IAMRoleCredentialsMessage{
		MessageId: aws.String("123"),
		TaskArn:   aws.String("t1"),
		RoleCredentials: &ecsacs.IAMRoleCredentials{
			CredentialsId:   aws.String("credsId"),
			AccessKeyId:     aws.String("accessKeyId"),
			Expiration:      aws.String("2016-03-25T06:17:19.318+0000"),
			RoleArn:         aws.String("roleArn"),
			SecretAccessKey: aws.String("secretAccessKey"),
			SessionToken:    aws.String("token"),
		},
	}
	assert.Equal(t, <-messageChannel, expectedMessage)
}

func TestClosingConnection(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Returning EOF tells the ClientServer that the connection is closed
	conn := mock_wsclient.NewMockWebsocketConn(ctrl)
	conn.EXPECT().SetReadDeadline(gomock.Any()).Return(nil)
	conn.EXPECT().ReadMessage().Return(0, nil, io.EOF)
	// SetWriteDeadline will be invoked once for WriteMessage() and
	// once for Close()
	conn.EXPECT().SetWriteDeadline(gomock.Any()).Return(nil).Times(2)
	conn.EXPECT().WriteMessage(gomock.Any(), gomock.Any()).Return(io.EOF)
	conn.EXPECT().Close()
	cs := testCS(conn)
	defer cs.Close()

	serveErr := cs.Serve()
	assert.Error(t, serveErr)

	err := cs.MakeRequest(&ecsacs.AckRequest{})
	assert.Error(t, err)
}

func TestConnect(t *testing.T) {
	closeWS := make(chan bool)
	server, serverChan, requestChan, serverErr, err := startMockAcsServer(t, closeWS)
	defer server.Close()
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		t.Fatal(<-serverErr)
	}()

	cs := New(server.URL, testCfg, credentials.AnonymousCredentials, rwTimeout)
	// Wait for up to a second for the mock server to launch
	for i := 0; i < 100; i++ {
		err = cs.Connect()
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}

	errs := make(chan error)
	cs.AddRequestHandler(func(msg *ecsacs.PayloadMessage) {
		if *msg.MessageId != "messageId" || len(msg.Tasks) != 1 || *msg.Tasks[0].Arn != "arn1" {
			errs <- errors.New("incorrect payloadMessage arguments")
		} else {
			errs <- nil
		}
	})

	go func() {
		_ = cs.Serve()
	}()

	go func() {
		serverChan <- `{"type":"PayloadMessage","message":{"tasks":[{"arn":"arn1","desiredStatus":"RUNNING","overrides":"{}","family":"test","version":"v1","containers":[{"name":"c1","image":"redis","command":["arg1","arg2"],"cpu":10,"memory":20,"links":["db"],"portMappings":[{"containerPort":22,"hostPort":22}],"essential":true,"entryPoint":["bash"],"environment":{"key":"val"},"overrides":"{}","desiredStatus":"RUNNING"}]}],"messageId":"messageId"}}` + "\n"
	}()
	// Error for handling a 'PayloadMessage' request
	err = <-errs
	if err != nil {
		t.Fatal(err)
	}

	mid := "messageId"
	cluster := TestClusterArn
	ci := TestInstanceArn
	go func() {
		cs.MakeRequest(&ecsacs.AckRequest{
			MessageId:         &mid,
			Cluster:           &cluster,
			ContainerInstance: &ci,
		})
	}()

	request := <-requestChan

	// A request should have a 'type' and a 'message'
	intermediate := struct {
		Type    string             `json:"type"`
		Message *ecsacs.AckRequest `json:"message"`
	}{}
	err = json.Unmarshal([]byte(request), &intermediate)
	if err != nil {
		t.Fatal(err)
	}
	if intermediate.Type != "AckRequest" || *intermediate.Message.MessageId != mid || *intermediate.Message.ContainerInstance != ci || *intermediate.Message.Cluster != cluster {
		t.Fatal("Unexpected request")
	}
	closeWS <- true
	close(serverChan)
}

func TestConnectClientError(t *testing.T) {
	testServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte(`{"InvalidClusterException":"Invalid cluster"}` + "\n"))
	}))
	defer testServer.Close()

	cs := New(testServer.URL, testCfg, credentials.AnonymousCredentials, rwTimeout)
	err := cs.Connect()
	_, ok := err.(*wsclient.WSError)
	assert.True(t, ok)
	assert.EqualError(t, err, "InvalidClusterException: Invalid cluster")
}

func testCS(conn *mock_wsclient.MockWebsocketConn) wsclient.ClientServer {
	testCreds := credentials.AnonymousCredentials
	foo := New("localhost:443", testCfg, testCreds, rwTimeout)
	cs := foo.(*clientServer)
	cs.SetConnection(conn)
	return cs
}

// TODO: replace with gomock
func startMockAcsServer(t *testing.T, closeWS <-chan bool) (*httptest.Server, chan<- string, <-chan string, <-chan error, error) {
	serverChan := make(chan string)
	requestsChan := make(chan string)
	errChan := make(chan error)

	upgrader := websocket.Upgrader{ReadBufferSize: 1024, WriteBufferSize: 1024}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		go func() {
			<-closeWS
			ws.Close()
		}()
		if err != nil {
			errChan <- err
		}
		go func() {
			_, msg, err := ws.ReadMessage()
			if err != nil {
				errChan <- err
			} else {
				requestsChan <- string(msg)
			}
		}()
		for str := range serverChan {
			err := ws.WriteMessage(websocket.TextMessage, []byte(str))
			if err != nil {
				errChan <- err
			}
		}
	})

	server := httptest.NewTLSServer(handler)
	return server, serverChan, requestsChan, errChan, nil
}

func TestAttachENIHandlerCalled(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	conn := mock_wsclient.NewMockWebsocketConn(ctrl)
	cs := testCS(conn)
	defer cs.Close()

	// Messages should be read from the connection at least once
	conn.EXPECT().SetReadDeadline(gomock.Any()).Return(nil).MinTimes(1)
	conn.EXPECT().ReadMessage().Return(websocket.TextMessage,
		[]byte(sampleAttachENIMessage), nil).MinTimes(1)
	// Invoked when closing the connection
	conn.EXPECT().SetWriteDeadline(gomock.Any()).Return(nil)
	conn.EXPECT().Close()

	messageChannel := make(chan *ecsacs.AttachTaskNetworkInterfacesMessage)
	reqHandler := func(message *ecsacs.AttachTaskNetworkInterfacesMessage) {
		messageChannel <- message
	}

	cs.AddRequestHandler(reqHandler)

	go cs.Serve()

	expectedMessage := &ecsacs.AttachTaskNetworkInterfacesMessage{
		MessageId:  aws.String("123"),
		ClusterArn: aws.String("default"),
		TaskArn:    aws.String("task"),
		ElasticNetworkInterfaces: []*ecsacs.ElasticNetworkInterface{
			{AttachmentArn: aws.String("attach_arn"),
				Ec2Id: aws.String("eni_id"),
				Ipv4Addresses: []*ecsacs.IPv4AddressAssignment{
					{
						Primary:        aws.Bool(true),
						PrivateAddress: aws.String("ipv4"),
					},
				},
				Ipv6Addresses: []*ecsacs.IPv6AddressAssignment{
					{
						Address: aws.String("ipv6"),
					},
				},
				MacAddress: aws.String("mac"),
			},
		},
	}

	assert.Equal(t, <-messageChannel, expectedMessage)
}
