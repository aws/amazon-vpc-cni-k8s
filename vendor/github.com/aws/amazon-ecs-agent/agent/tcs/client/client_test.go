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

// Package tcsclient wraps the generated aws-sdk-go client to provide marshalling
// and unmarshalling of data over a websocket connection in the format expected
// by TCS. It allows for bidirectional communication and acts as both a
// client-and-server in terms of requests, but only as a client in terms of
// connecting.

package tcsclient

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/aws/amazon-ecs-agent/agent/config"
	"github.com/aws/amazon-ecs-agent/agent/tcs/model/ecstcs"
	"github.com/aws/amazon-ecs-agent/agent/wsclient"
	"github.com/aws/amazon-ecs-agent/agent/wsclient/mock"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
)

const (
	testPublishMetricsInterval = 1 * time.Second
	testMessageId              = "testMessageId"
	testCluster                = "default"
	testContainerInstance      = "containerInstance"
	rwTimeout                  = time.Second
)

type mockStatsEngine struct{}

func (engine *mockStatsEngine) GetInstanceMetrics() (*ecstcs.MetricsMetadata, []*ecstcs.TaskMetric, error) {
	return nil, nil, fmt.Errorf("uninitialized")
}

type emptyStatsEngine struct{}

func (engine *emptyStatsEngine) GetInstanceMetrics() (*ecstcs.MetricsMetadata, []*ecstcs.TaskMetric, error) {
	return nil, nil, fmt.Errorf("empty stats")
}

type idleStatsEngine struct{}

func (engine *idleStatsEngine) GetInstanceMetrics() (*ecstcs.MetricsMetadata, []*ecstcs.TaskMetric, error) {
	metadata := &ecstcs.MetricsMetadata{
		Cluster:           aws.String(testCluster),
		ContainerInstance: aws.String(testContainerInstance),
		Idle:              aws.Bool(true),
		MessageId:         aws.String(testMessageId),
	}
	return metadata, []*ecstcs.TaskMetric{}, nil
}

type nonIdleStatsEngine struct {
	numTasks int
}

func (engine *nonIdleStatsEngine) GetInstanceMetrics() (*ecstcs.MetricsMetadata, []*ecstcs.TaskMetric, error) {
	metadata := &ecstcs.MetricsMetadata{
		Cluster:           aws.String(testCluster),
		ContainerInstance: aws.String(testContainerInstance),
		Idle:              aws.Bool(false),
		MessageId:         aws.String(testMessageId),
	}
	var taskMetrics []*ecstcs.TaskMetric
	var i int64
	for i = 0; int(i) < engine.numTasks; i++ {
		taskArn := "task/" + strconv.FormatInt(i, 10)
		taskMetrics = append(taskMetrics, &ecstcs.TaskMetric{TaskArn: &taskArn})
	}
	return metadata, taskMetrics, nil
}

func newNonIdleStatsEngine(numTasks int) *nonIdleStatsEngine {
	return &nonIdleStatsEngine{numTasks: numTasks}
}

func TestPayloadHandlerCalled(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	conn := mock_wsclient.NewMockWebsocketConn(ctrl)
	cs := testCS(conn)

	// Messages should be read from the connection at least once
	conn.EXPECT().SetReadDeadline(gomock.Any()).Return(nil).MinTimes(1)
	conn.EXPECT().ReadMessage().Return(1,
		[]byte(`{"type":"AckPublishMetric","message":{}}`), nil).MinTimes(1)
	// Invoked when closing the connection
	conn.EXPECT().SetWriteDeadline(gomock.Any()).Return(nil)
	conn.EXPECT().Close()

	handledPayload := make(chan *ecstcs.AckPublishMetric)

	reqHandler := func(payload *ecstcs.AckPublishMetric) {
		handledPayload <- payload
	}
	cs.AddRequestHandler(reqHandler)

	go cs.Serve()
	defer cs.Close()

	t.Log("Waiting for handler to return payload.")
	<-handledPayload
}

func TestPublishMetricsRequest(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	conn := mock_wsclient.NewMockWebsocketConn(ctrl)
	// Invoked when closing the connection
	conn.EXPECT().SetWriteDeadline(gomock.Any()).Return(nil).Times(2)
	conn.EXPECT().Close()
	// TODO: should use explicit values
	conn.EXPECT().WriteMessage(gomock.Any(), gomock.Any())

	cs := testCS(conn)
	defer cs.Close()

	err := cs.MakeRequest(&ecstcs.PublishMetricsRequest{})
	if err != nil {
		t.Fatal(err)
	}

}
func TestPublishMetricsOnceEmptyStatsError(t *testing.T) {
	cs := clientServer{
		statsEngine: &emptyStatsEngine{},
	}
	err := cs.publishMetricsOnce()

	assert.Error(t, err, "Failed: expecting publishMerticOnce return err ")
}

func TestPublishOnceIdleStatsEngine(t *testing.T) {
	cs := clientServer{
		statsEngine: &idleStatsEngine{},
	}
	requests, err := cs.metricsToPublishMetricRequests()
	if err != nil {
		t.Fatal("Error creating publishmetricrequests: ", err)
	}
	if len(requests) != 1 {
		t.Errorf("Expected %d requests, got %d", 1, len(requests))
	}
	lastRequest := requests[0]
	if !*lastRequest.Metadata.Fin {
		t.Error("Fin not set to true in Last request")
	}
}

func TestPublishOnceNonIdleStatsEngine(t *testing.T) {
	expectedRequests := 3
	// Cretes 21 task metrics, which translate to 3 batches,
	// {[Task1, Task2, ...Task10], [Task11, Task12, ...Task20], [Task21]}
	numTasks := (tasksInMessage * (expectedRequests - 1)) + 1
	cs := clientServer{
		statsEngine: newNonIdleStatsEngine(numTasks),
	}
	requests, err := cs.metricsToPublishMetricRequests()
	if err != nil {
		t.Fatal("Error creating publishmetricrequests: ", err)
	}
	taskArns := make(map[string]bool)
	for _, request := range requests {
		for _, taskMetric := range request.TaskMetrics {
			_, exists := taskArns[*taskMetric.TaskArn]
			if exists {
				t.Fatal("Duplicate task arn in requests: ", *taskMetric.TaskArn)
			}
			taskArns[*taskMetric.TaskArn] = true
		}
	}
	if len(requests) != expectedRequests {
		t.Errorf("Expected %d requests, got %d", expectedRequests, len(requests))
	}
	lastRequest := requests[expectedRequests-1]
	if !*lastRequest.Metadata.Fin {
		t.Error("Fin not set to true in last request")
	}
	requests = requests[:(expectedRequests - 1)]
	for i, request := range requests {
		if *request.Metadata.Fin {
			t.Errorf("Fin set to true in request %d/%d", i, (expectedRequests - 1))
		}
	}
}

func testCS(conn *mock_wsclient.MockWebsocketConn) wsclient.ClientServer {
	testCreds := credentials.AnonymousCredentials
	cfg := &config.Config{
		AWSRegion:          "us-east-1",
		AcceptInsecureCert: true,
	}
	cs := New("localhost:443", cfg, testCreds, &mockStatsEngine{},
		testPublishMetricsInterval, rwTimeout).(*clientServer)
	cs.SetConnection(conn)
	return cs
}

// TestCloseClientServer tests the ws connection will be closed by tcs client when
// received the deregisterInstanceStream
func TestCloseClientServer(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	conn := mock_wsclient.NewMockWebsocketConn(ctrl)
	cs := testCS(conn)

	gomock.InOrder(
		conn.EXPECT().SetWriteDeadline(gomock.Any()).Return(nil),
		conn.EXPECT().WriteMessage(gomock.Any(), gomock.Any()),
		conn.EXPECT().SetWriteDeadline(gomock.Any()).Return(nil),
		conn.EXPECT().Close(),
	)

	err := cs.MakeRequest(&ecstcs.PublishMetricsRequest{})
	assert.Nil(t, err)

	err = cs.Disconnect()
	assert.Nil(t, err)
}
