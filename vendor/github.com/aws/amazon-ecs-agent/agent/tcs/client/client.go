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

package tcsclient

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/aws/amazon-ecs-agent/agent/config"
	"github.com/aws/amazon-ecs-agent/agent/stats"
	"github.com/aws/amazon-ecs-agent/agent/tcs/model/ecstcs"
	"github.com/aws/amazon-ecs-agent/agent/utils"
	"github.com/aws/amazon-ecs-agent/agent/wsclient"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/cihub/seelog"
)

// tasksInMessage is the maximum number of tasks that can be sent in a message to the backend
// This is a very conservative estimate assuming max allowed string lengths for all fields.
const tasksInMessage = 10

// clientServer implements wsclient.ClientServer interface for metrics backend.
type clientServer struct {
	statsEngine            stats.Engine
	publishTicker          *time.Ticker
	endPublish             chan struct{}
	publishMetricsInterval time.Duration
	wsclient.ClientServerImpl
}

// New returns a client/server to bidirectionally communicate with the backend.
// The returned struct should have both 'Connect' and 'Serve' called upon it
// before being used.
func New(url string,
	cfg *config.Config,
	credentialProvider *credentials.Credentials,
	statsEngine stats.Engine,
	publishMetricsInterval time.Duration,
	rwTimeout time.Duration) wsclient.ClientServer {
	cs := &clientServer{
		statsEngine:            statsEngine,
		publishTicker:          nil,
		publishMetricsInterval: publishMetricsInterval,
	}
	cs.URL = url
	cs.AgentConfig = cfg
	cs.CredentialProvider = credentialProvider
	cs.ServiceError = &tcsError{}
	cs.RequestHandlers = make(map[string]wsclient.RequestHandler)
	cs.TypeDecoder = NewTCSDecoder()
	cs.RWTimeout = rwTimeout
	return cs
}

// Serve begins serving requests using previously registered handlers (see
// AddRequestHandler). All request handlers should be added prior to making this
// call as unhandled requests will be discarded.
func (cs *clientServer) Serve() error {
	seelog.Debug("TCS client starting websocket poll loop")
	if !cs.IsReady() {
		return fmt.Errorf("tcs client: websocket not ready for connections")
	}

	if cs.statsEngine == nil {
		return fmt.Errorf("tcs client: uninitialized stats engine")
	}

	// Start the timer function to publish metrics to the backend.
	cs.publishTicker = time.NewTicker(cs.publishMetricsInterval)
	cs.endPublish = make(chan struct{})
	go cs.publishMetrics()

	return cs.ConsumeMessages()
}

// MakeRequest makes a request using the given input. Note, the input *MUST* be
// a pointer to a valid backend type that this client recognises
func (cs *clientServer) MakeRequest(input interface{}) error {
	payload, err := cs.CreateRequestMessage(input)
	if err != nil {
		return err
	}

	seelog.Debugf("TCS client sending payload: %s", string(payload))
	data := cs.signRequest(payload)

	// Over the wire we send something like
	// {"type":"AckRequest","message":{"messageId":"xyz"}}
	return cs.WriteMessage(data)
}

func (cs *clientServer) signRequest(payload []byte) []byte {
	reqBody := bytes.NewBuffer(payload)
	// NewRequest never returns an error if the url parses and we just verified
	// it did above
	request, _ := http.NewRequest("GET", cs.URL, reqBody)
	utils.SignHTTPRequest(request, cs.AgentConfig.AWSRegion, "ecs", cs.CredentialProvider, aws.ReadSeekCloser(reqBody))

	request.Header.Add("Host", request.Host)
	var dataBuffer bytes.Buffer
	request.Header.Write(&dataBuffer)
	io.WriteString(&dataBuffer, "\r\n")

	data := dataBuffer.Bytes()
	data = append(data, payload...)

	return data
}

// Close closes the underlying connection.
func (cs *clientServer) Close() error {
	if cs.publishTicker != nil {
		cs.publishTicker.Stop()
		cs.endPublish <- struct{}{}
	}

	return cs.Disconnect()
}

// publishMetrics invokes the PublishMetricsRequest on the clientserver object.
func (cs *clientServer) publishMetrics() {
	if cs.publishTicker == nil {
		seelog.Debug("Skipping publishing metrics. Publish ticker is uninitialized")
		return
	}

	// Publish metrics immediately after we connect and wait for ticks. This makes
	// sure that there is no data loss when a scheduled metrics publishing fails
	// due to a connection reset.
	err := cs.publishMetricsOnce()
	if err != nil && err != stats.EmptyMetricsError {
		seelog.Warnf("Error publishing metrics: %v", err)
	}
	// don't simply range over the ticker since its channel doesn't ever get closed
	for {
		select {
		case <-cs.publishTicker.C:
			err := cs.publishMetricsOnce()
			if err != nil {
				seelog.Warnf("Error publishing metrics: %v", err)
			}
		case <-cs.endPublish:
			return
		}
	}
}

// publishMetricsOnce is invoked by the ticker to periodically publish metrics to backend.
func (cs *clientServer) publishMetricsOnce() error {
	// Get the list of objects to send to backend.
	requests, err := cs.metricsToPublishMetricRequests()
	if err != nil {
		return err
	}

	// Make the publish metrics request to the backend.
	for _, request := range requests {
		err = cs.MakeRequest(request)
		if err != nil {
			return err
		}
	}
	return nil
}

// metricsToPublishMetricRequests gets task metrics and converts them to a list of PublishMetricRequest
// objects.
func (cs *clientServer) metricsToPublishMetricRequests() ([]*ecstcs.PublishMetricsRequest, error) {
	metadata, taskMetrics, err := cs.statsEngine.GetInstanceMetrics()
	if err != nil {
		return nil, err
	}

	var requests []*ecstcs.PublishMetricsRequest
	if *metadata.Idle {
		metadata.Fin = aws.Bool(true)
		// Idle instance, we have only one request to send to backend.
		requests = append(requests, ecstcs.NewPublishMetricsRequest(metadata, taskMetrics))
		return requests, nil
	}
	var messageTaskMetrics []*ecstcs.TaskMetric
	numTasks := len(taskMetrics)

	for i, taskMetric := range taskMetrics {
		messageTaskMetrics = append(messageTaskMetrics, taskMetric)
		var requestMetadata *ecstcs.MetricsMetadata
		if (i + 1) == numTasks {
			// If this is the last task to send, set fin to true
			requestMetadata = copyMetricsMetadata(metadata, true)
		} else {
			requestMetadata = copyMetricsMetadata(metadata, false)
		}
		if (i+1)%tasksInMessage == 0 {
			// Construct payload with tasksInMessage number of task metrics and send to backend.
			requests = append(requests, ecstcs.NewPublishMetricsRequest(requestMetadata, copyTaskMetrics(messageTaskMetrics)))
			messageTaskMetrics = messageTaskMetrics[:0]
		}
	}

	if len(messageTaskMetrics) > 0 {
		// Create the new metadata object and set fin to true as this is the last message in the payload.
		requestMetadata := copyMetricsMetadata(metadata, true)
		// Create a request with remaining task metrics.
		requests = append(requests, ecstcs.NewPublishMetricsRequest(requestMetadata, messageTaskMetrics))
	}
	return requests, nil
}

// copyMetricsMetadata creates a new MetricsMetadata object from a given MetricsMetadata object.
// It copies all the fields from the source object to the new object and sets the 'Fin' field
// as specified by the argument.
func copyMetricsMetadata(metadata *ecstcs.MetricsMetadata, fin bool) *ecstcs.MetricsMetadata {
	return &ecstcs.MetricsMetadata{
		Cluster:           aws.String(*metadata.Cluster),
		ContainerInstance: aws.String(*metadata.ContainerInstance),
		Idle:              aws.Bool(*metadata.Idle),
		MessageId:         aws.String(*metadata.MessageId),
		Fin:               aws.Bool(fin),
	}
}

// copyTaskMetrics copies a slice of TaskMetric objects to another slice. This is needed as we
// reset the source slice after creating a new PublishMetricsRequest object.
func copyTaskMetrics(from []*ecstcs.TaskMetric) []*ecstcs.TaskMetric {
	to := make([]*ecstcs.TaskMetric, len(from))
	copy(to, from)
	return to
}
