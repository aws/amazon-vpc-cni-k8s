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
	"encoding/json"
	"log"
	"net/http"

	"github.com/aws/amazon-vpc-cni-k8s/test/agent/pkg/input"
)

var connectivityMetric []input.TestStatus

// metric server stores metrics from test client and returns the aggregated metrics to the
// automation test
func main() {
	http.HandleFunc("/submit/metric/connectivity", submitConnectivityMetric)
	http.HandleFunc("/get/metric/connectivity", getConnectivityMetric)
	log.Fatal(http.ListenAndServe(":8080", nil))
}

// adds the metric to list of metrics
func submitConnectivityMetric(_ http.ResponseWriter, r *http.Request) {
	decoder := json.NewDecoder(r.Body)

	var status input.TestStatus
	err := decoder.Decode(&status)

	if err != nil {
		log.Printf("failed to decode the request body: %v", err)
		return
	}

	log.Printf("received metric %+v", status)
	connectivityMetric = append(connectivityMetric, status)
}

// returns the list of metrics
func getConnectivityMetric(w http.ResponseWriter, r *http.Request) {
	metricByte, err := json.Marshal(connectivityMetric)
	if err != nil {
		log.Printf("failed to marshall: %v", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(metricByte)
}
