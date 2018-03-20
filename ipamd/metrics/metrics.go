// Copyright 2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

package metrics

import (
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type Metrics struct {
	awsLatency    *prometheus.SummaryVec
	addEniRetries prometheus.Counter

	ipCount  *prometheus.GaugeVec
	eniCount *prometheus.GaugeVec
}

func New() (*Metrics, error) {
	m := &Metrics{
		awsLatency: prometheus.NewSummaryVec(
			prometheus.SummaryOpts{
				Name: "k8s_eni_aws_latency_ms",
				Help: "aws latency in ms",
			},
			[]string{"fn", "error"},
		),
		addEniRetries: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "k8s_eni_addeni_retries",
				Help: "addEni retries",
			},
		),
		eniCount: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "k8s_eni_enipool_enis",
				Help: "enis in enipool",
			},
			[]string{"status"},
		),
		ipCount: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "k8s_eni_ippool_ips",
				Help: "ips in ippool",
			},
			[]string{"status"},
		),
	}

	return m, m.Register()
}

func (m *Metrics) TimeAWSCall(start time.Time, fn string, isErr bool) {
	m.awsLatency.WithLabelValues(fn, fmt.Sprint(isErr)).Observe(msSince(start))
}

func (m *Metrics) AddENIRetry() {
	m.addEniRetries.Inc()
}

func (m *Metrics) AddENI() {
	m.eniCount.WithLabelValues("assigned").Add(1)
}

func (m *Metrics) FreeENI(ipCount int) {
	m.ipCount.WithLabelValues("total").Add(-1 * float64(ipCount))
	m.eniCount.WithLabelValues("assigned").Add(-1)
}

func (m *Metrics) SetMaxENI(eniCount int64) {
	m.eniCount.WithLabelValues("max").Set(float64(eniCount))
}

func (m *Metrics) AddIP() {
	m.ipCount.WithLabelValues("total").Add(1)
}

func (m *Metrics) AssignPodIP() {
	m.ipCount.WithLabelValues("assigned").Add(1)
}

func (m *Metrics) UnassignPodIP() {
	m.ipCount.WithLabelValues("assigned").Add(-1)
}

func (m *Metrics) Reset() {
	m.ipCount.WithLabelValues("assigned").Set(0)
	m.ipCount.WithLabelValues("total").Set(0)
	m.eniCount.WithLabelValues("assigned").Set(0)
}

// msSince returns milliseconds since start.
func msSince(start time.Time) float64 {
	return float64(time.Since(start) / time.Millisecond)
}
