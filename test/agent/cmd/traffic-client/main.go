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
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/test/agent/pkg/input"
)

var success, failure int

const (
	ClientRequestData = "message from the client!"
	TcpTimeout        = time.Second * 2
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

	for _, server := range serverList {
		serverAddr := fmt.Sprintf("%s:%s", server, serverPort)

		// Get connection based on the server mode - tcp/udp
		conn, err := getConnection(serverAddr, connectionMode)
		if err != nil {
			log.Printf("failed to get connection to server %s: %v", serverAddr, err)
			failure++
			continue
		}

		// Send data to the server and wait for response from the server (this sequence is
		// expected from the server)
		err = sendAndReceiveResponse(conn, serverAddr)
		if err != nil {
			log.Printf("failed to send/receive response from server %s: %v", serverAddr, err)
			failure++
			continue
		}

		// Connection successfully tested, mark as success and test next server
		success++

	}

	fmt.Printf("Success: %d, Failure: %d", success, failure)

	// Report metrics to the aggregator address from all the clients if provided
	// Otherwise the results can be read from stdout
	if metricAggregatorAddr != "" {
		body, _ := json.Marshal(input.TestStatus{
			SuccessCount: success,
			FailureCount: failure,
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

func getConnection(serverAddr string, serverMode string) (net.Conn, error) {
	if serverMode == "tcp" {
		conn, err := net.DialTimeout("tcp", serverAddr, TcpTimeout)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to server %s: %v", serverAddr, err)
		}
		return conn, nil
	}
	if serverMode == "udp" {
		udpAddr, err := net.ResolveUDPAddr("udp", serverAddr)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve the server address: %v", err)
		}

		conn, err := net.DialUDP("udp", nil, udpAddr)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to udp server: %v", err)
		}
		return conn, nil
	}

	return nil, fmt.Errorf("invalid server mode provided %s", serverMode)
}

func sendAndReceiveResponse(conn net.Conn, serverAddr string) error {
	defer conn.Close()

	data := []byte(ClientRequestData)

	_, err := conn.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write to server: %v", err)
	}

	log.Printf("successfully sent data to the server %s", serverAddr)

	buffer := make([]byte, 1024)
	respLen, err := conn.Read(buffer)
	if err != nil || respLen == 0 {
		return fmt.Errorf("failed to read from the server, resp length %d: %v", respLen, err)
	}

	log.Printf("successfully recieved response from server %s: %s", serverAddr, string(buffer))

	return nil
}
