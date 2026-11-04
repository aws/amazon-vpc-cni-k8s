// Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
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

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/test/agent/pkg/input"
)

var success, failure int

const (
	ClientRequestData = "message from the client!"
	TcpTimeout        = time.Second * 5
	UdpReadTimeout    = time.Second * 2
	UdpMaxAttempts    = 3
)

// Queries the list of server address provided and posts the success and failure rate in stdout
// If a metric aggregator address is provided then the response is posted to aggregator
func main() {
	//  List of server separated by comma
	var serverListCsv string
	// Port on which all the server are listening
	var serverPort string
	// Mode on which the server is listening
	var connectionMode string
	// Aggregates the metrics from all clients into a single place
	var metricAggregatorAddr string

	flag.StringVar(&connectionMode, "server-listen-mode", "tcp", "supported server mode - tcp,udp")
	flag.StringVar(&serverListCsv, "server-list-csv", "", "server list address with comma separated value")
	flag.StringVar(&serverPort, "server-port", "2273", "port on which the servers are listening")
	flag.StringVar(&metricAggregatorAddr, "metric-aggregator-addr", "", "optional aggregator where each client can post their metrics")

	flag.Parse()

	serverList := strings.Split(serverListCsv, ",")

	log.Printf("will test connection %s to the list of server %v on port %s",
		connectionMode, serverList, serverPort)

	hostName, err := os.Hostname()
	if err != nil {
		log.Fatalf("failed to get the host name: %v", err)
	}

	var failureList []input.Failure
	for _, server := range serverList {
		// Server IPs may be passed pre-bracketed for IPv6, strip them so JoinHostPort can re-add
		serverAddr := net.JoinHostPort(strings.Trim(server, "[]"), serverPort)

		// Get connection based on the server mode - tcp/udp
		conn, failure := getConnection(serverAddr, connectionMode)
		if failure != nil {
			failureList = append(failureList, *failure)
			continue
		}

		// Send data to the server and wait for response from the server (this sequence is
		// expected from the server)
		failure = sendAndReceiveResponse(conn, serverAddr, connectionMode)
		if failure != nil {
			failureList = append(failureList, *failure)
			continue
		}

		// Connection successfully tested, mark as success and test next server
		success++
	}

	failure = len(failureList)

	fmt.Printf("Success: %d, Failure: %d, Failure Reason: %v", success, failure, failureList)

	// Report metrics to the aggregator address from all the clients if provided
	// Otherwise the results can be read from stdout
	if metricAggregatorAddr != "" {
		body, _ := json.Marshal(input.TestStatus{
			SuccessCount: success,
			FailureCount: failure,
			SourcePod:    hostName,
			Failures:     failureList,
		})
		resp, err := http.Post(metricAggregatorAddr, "application/json", bytes.NewBuffer(body))
		if err != nil {
			log.Fatalf("failed to post data to metric aggregator %s: %v",
				metricAggregatorAddr, err)
		}
		if resp.StatusCode != http.StatusOK {
			log.Fatalf("received non OK status code from the server%s: %d",
				metricAggregatorAddr, resp.StatusCode)
		}
		log.Printf("successfully posted data to metric aggregator %v", resp)
	}
}

func getConnection(serverAddr string, serverMode string) (net.Conn, *input.Failure) {
	failure := &input.Failure{
		DestinationIP: serverAddr,
	}

	if serverMode == "tcp" {
		conn, err := net.DialTimeout("tcp", serverAddr, TcpTimeout)
		if err != nil {
			failure.FailureReason = fmt.Sprintf("failed to connect to server %v", err)
			return nil, failure
		}
		return conn, nil
	}
	if serverMode == "udp" {
		udpAddr, err := net.ResolveUDPAddr("udp", serverAddr)
		if err != nil {
			failure.FailureReason = fmt.Sprintf("failed to resolve the server address: %v", err)
			return nil, failure
		}

		conn, err := net.DialUDP("udp", nil, udpAddr)
		if err != nil {
			failure.FailureReason = fmt.Sprintf("failed to connect to udp server: %v", err)
			return nil, failure
		}
		return conn, nil
	}

	failure.FailureReason = fmt.Sprintf("invalid server mode provided %s", serverMode)
	return nil, failure
}

func sendAndReceiveResponse(conn net.Conn, serverAddr string, serverMode string) *input.Failure {
	defer conn.Close()

	failure := &input.Failure{
		DestinationIP: serverAddr,
	}

	attempts, readTimeout := 1, TcpTimeout
	if serverMode == "udp" {
		attempts, readTimeout = UdpMaxAttempts, UdpReadTimeout
	}

	data := []byte(ClientRequestData)
	buffer := make([]byte, 1024)

	var respLen int
	var err error
	for attempt := 1; attempt <= attempts; attempt++ {
		if _, err = conn.Write(data); err != nil {
			failure.FailureReason = fmt.Sprintf("failed to write to server: %v", err)
			return failure
		}

		log.Printf("successfully sent data to the server %s (attempt %d/%d)", serverAddr, attempt, attempts)

		if err = conn.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
			failure.FailureReason = fmt.Sprintf("failed to set read deadline: %v", err)
			return failure
		}

		respLen, err = conn.Read(buffer)
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			log.Printf("timed out waiting for response from server %s (attempt %d/%d)", serverAddr, attempt, attempts)
			continue
		}
		break
	}

	if err != nil || respLen == 0 {
		failure.FailureReason = fmt.Sprintf("failed to read from the server after %d attempt(s), resp length %d: %v", attempts, respLen, err)
		return failure
	}

	log.Printf("successfully received response from server %s: %s", serverAddr, string(buffer[:respLen]))

	return nil
}
