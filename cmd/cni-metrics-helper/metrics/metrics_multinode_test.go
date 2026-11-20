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

package metrics

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	cloudwatchtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/stretchr/testify/assert"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/publisher"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
)

type recordingMetricsPublisher struct {
	datums []cloudwatchtypes.MetricDatum
}

func (p *recordingMetricsPublisher) Publish(datums ...cloudwatchtypes.MetricDatum) {
	p.datums = append(p.datums, datums...)
}

func (*recordingMetricsPublisher) Start(int) {}
func (*recordingMetricsPublisher) Stop()     {}

type multiNodeTarget struct {
	metricsPollState
	interesting map[string]metricsConvert
	targets     []string
	scrapes     map[string]string
	listError   bool
	cw          recordingMetricsPublisher
}

func (m *multiNodeTarget) grabMetricsFromTarget(_ context.Context, target string) ([]byte, error) {
	raw, ok := m.scrapes[target]
	if !ok {
		return nil, fmt.Errorf("scrape failed for target %q", target)
	}
	return []byte(raw), nil
}

func (m *multiNodeTarget) getTargetList(context.Context) ([]string, error) {
	if m.listError {
		return m.targets, errors.New("target list failed")
	}
	return m.targets, nil
}

func (m *multiNodeTarget) getInterestingMetrics() map[string]metricsConvert { return m.interesting }
func (m *multiNodeTarget) getCWMetricsPublisher() publisher.Publisher       { return &m.cw }
func (*multiNodeTarget) submitCloudWatch() bool                             { return true }
func (*multiNodeTarget) submitPrometheus() bool                             { return false }
func (*multiNodeTarget) getLogger() logger.Logger                           { return logger.DefaultLogger() }

func TestHandlerPublishesCounterDeltas(t *testing.T) {
	const (
		addCounter = "awscni_add_ip_req_count"
		delCounter = "awscni_del_ip_req_count"
		ipGauge    = "awscni_assigned_ip_addresses"
		addCW      = "addReqCount"
		delCW      = "delReqCount"
		ipCW       = "assignIPAddresses"
	)

	scalar := func(name, kind string, value int) string {
		return fmt.Sprintf("# TYPE %s %s\n%s %d\n", name, kind, name, value)
	}
	add := func(value int) string { return scalar(addCounter, "counter", value) }
	del := func(value int) string { return scalar(delCounter, "counter", value) }
	ips := func(value int) string { return scalar(ipGauge, "gauge", value) }
	series := func(samples ...string) string {
		var b strings.Builder
		fmt.Fprintf(&b, "# TYPE %s counter\n", addCounter)
		for _, sample := range samples {
			fmt.Fprintf(&b, "%s%s\n", addCounter, sample)
		}
		return b.String()
	}
	malformed := "# TYPE " + addCounter + " counter\n" + addCounter + " invalid\n"

	type scrapes map[string]string
	type values map[string]float64
	type poll struct {
		name      string
		order     string // space-separated scrape targets
		scrapes   scrapes
		want      values
		listError bool
	}

	tests := []struct {
		name  string
		polls []poll
	}{
		{
			name: "startup waits for a successfully parsed poll",
			polls: []poll{
				{name: "empty target list"},
				{name: "all scrapes fail", order: "a b"},
				{name: "empty exposition", order: "a", scrapes: scrapes{"a": ""}},
				{name: "untracked metric only", order: "a", scrapes: scrapes{"a": scalar("untracked", "gauge", 1)}},
				{name: "partial parse abort", order: "a b", scrapes: scrapes{"a": add(7), "b": malformed}},
				{name: "target list error", order: "a", scrapes: scrapes{"a": add(8)}, listError: true},
				{
					name:  "first valid scrape emits zero and current gauge",
					order: "a b", scrapes: scrapes{"a": add(10) + ips(4)},
					want: values{addCW: 0, ipCW: 4},
				},
				{
					name:  "previously unobserved target counts from zero",
					order: "b a", scrapes: scrapes{"a": add(12) + ips(5), "b": add(20)},
					want: values{addCW: 22, ipCW: 5},
				},
			},
		},
		{
			name: "gauge-only startup",
			polls: []poll{
				{name: "gauge establishes startup", order: "a", scrapes: scrapes{"a": ips(4)}, want: values{ipCW: 4}},
				{name: "new counter family counts from zero", order: "a", scrapes: scrapes{"a": add(10)}, want: values{addCW: 10}},
			},
		},
		{
			name: "multiple targets accumulate increases and resets independently",
			polls: []poll{
				{name: "baseline", order: "a b c", scrapes: scrapes{"a": add(10), "b": add(20), "c": add(30)}, want: values{addCW: 0}},
				{name: "idle after reorder", order: "c b a", scrapes: scrapes{"a": add(10), "b": add(20), "c": add(30)}, want: values{addCW: 0}},
				{name: "two targets grow", order: "b a c", scrapes: scrapes{"a": add(15), "b": add(20), "c": add(38)}, want: values{addCW: 13}},
				{name: "reset and growth", order: "c a b", scrapes: scrapes{"a": add(2), "b": add(23), "c": add(38)}, want: values{addCW: 5}},
				{name: "idle after reset", order: "a b c", scrapes: scrapes{"a": add(2), "b": add(23), "c": add(38)}, want: values{addCW: 0}},
			},
		},
		{
			name: "target and family lifecycles",
			polls: []poll{
				{
					name:  "publish families from every target",
					order: "a b", scrapes: scrapes{"a": add(10) + del(7) + ips(4), "b": add(20)},
					want: values{addCW: 0, delCW: 0, ipCW: 4},
				},
				{
					name:  "new target contributes its full counters",
					order: "a b c", scrapes: scrapes{"a": add(10) + del(7) + ips(4), "b": add(21), "c": add(30) + del(4)},
					want: values{addCW: 31, delCW: 4, ipCW: 4},
				},
				{
					name:  "target leaves and family disappears from one target",
					order: "c a", scrapes: scrapes{"a": add(12) + ips(4), "c": add(32) + del(7)},
					want: values{addCW: 4, delCW: 3, ipCW: 4},
				},
				{
					name:  "departed target and missing family return",
					order: "b a c", scrapes: scrapes{"a": add(12) + del(9), "b": add(22), "c": add(32) + del(7)},
					want: values{addCW: 22, delCW: 2},
				},
				{
					name:  "family absent across all targets",
					order: "a b c", scrapes: scrapes{"a": add(13), "b": add(22), "c": add(32)},
					want: values{addCW: 1},
				},
				{
					name:  "family reappears after absence across all targets",
					order: "a b c", scrapes: scrapes{"a": add(13), "b": add(22), "c": add(32) + del(10)},
					want: values{addCW: 0, delCW: 3},
				},
				{name: "empty successful target list retires all targets"},
				{name: "target reappears after empty list", order: "b", scrapes: scrapes{"b": add(24)}, want: values{addCW: 24}},
			},
		},
		{
			name: "departed target retires even when its counter family is absent",
			polls: []poll{
				{name: "baseline", order: "a b", scrapes: scrapes{"a": add(10), "b": add(20)}, want: values{addCW: 0}},
				{name: "target leaves during a gauge-only poll", order: "a", scrapes: scrapes{"a": ips(4)}, want: values{ipCW: 4}},
				{name: "returning target counts from zero", order: "a b", scrapes: scrapes{"a": add(11), "b": add(25)}, want: values{addCW: 26}},
			},
		},
		{
			name: "counter label series form one target total",
			polls: []poll{
				{
					name: "baseline", order: "a",
					scrapes: scrapes{"a": series(`{api="alloc", status="ok"} 20`, `{api="free", status="ok"} 50`)},
					want:    values{addCW: 0},
				},
				{
					name: "series order and label order change", order: "a",
					scrapes: scrapes{"a": series(`{status="ok", api="free"} 50`, `{status="ok", api="alloc"} 20`)},
					want:    values{addCW: 0},
				},
				{
					name: "new series and growth increase the total", order: "a",
					scrapes: scrapes{"a": series(`{api="alloc", status="ok"} 20`, `{api="free", status="ok"} 55`, `{api="alloc", status="error"} 8`)},
					want:    values{addCW: 13},
				},
				{
					name: "decreased target total counts from zero", order: "a",
					scrapes: scrapes{"a": series(`{api="alloc", status="ok"} 2`, `{api="free", status="ok"} 60`, `{api="alloc", status="error"} 1`)},
					want:    values{addCW: 63},
				},
			},
		},
		{
			name: "errors preserve baselines",
			polls: []poll{
				{name: "baseline", order: "a b c", scrapes: scrapes{"a": add(10), "b": add(20), "c": add(40)}, want: values{addCW: 0}},
				{name: "one scrape fails", order: "a b c", scrapes: scrapes{"a": add(15), "c": add(40)}, want: values{addCW: 5}},
				{name: "listing fails", order: "a", scrapes: scrapes{"a": add(90)}, listError: true},
				{name: "parse error aborts after partial read", order: "a b", scrapes: scrapes{"a": add(16), "b": malformed}},
				{name: "all scrapes fail", order: "a b c"},
				{
					name:  "recovery bridges failed polls",
					order: "a b c", scrapes: scrapes{"a": add(16), "b": add(26), "c": add(44)},
					want: values{addCW: 11},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := &multiNodeTarget{interesting: newCNIMetrics()}

			for _, p := range tt.polls {
				t.Run(p.name, func(t *testing.T) {
					target.targets = strings.Fields(p.order)
					target.scrapes = p.scrapes
					target.listError = p.listError
					target.cw.datums = nil

					Handler(context.Background(), target)

					var want []cloudwatchtypes.MetricDatum
					for name, value := range p.want {
						want = append(want, cloudwatchtypes.MetricDatum{
							MetricName: aws.String(name),
							Unit:       cloudwatchtypes.StandardUnitCount,
							Value:      aws.Float64(value),
						})
					}
					assert.ElementsMatch(t, want, target.cw.datums)
				})
			}
		})
	}
}
