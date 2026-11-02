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
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/publisher"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
)

// These tests exercise counter aggregation across more than one scrape target,
// which the existing single-target tests never do. They are the regression
// coverage for issue #3844: on a multi-node cluster the per-poll delta that
// cni-metrics-helper publishes for Prometheus counters must be the sum of each
// node's own increase, independent of the order pods are scraped in, and robust
// to node restarts, nodes joining or leaving, and transient scrape failures.

const testCounterMetric = "awscni_add_ip_req_count"

// newCounterMetrics returns a fresh, isolated conversion definition for a single
// counter metric so each test keeps its own per-target state instead of sharing
// the global InterestingCNIMetrics map.
func newCounterMetrics() map[string]metricsConvert {
	return map[string]metricsConvert{
		testCounterMetric: {actions: []metricsAction{{
			cwMetricName: "addReqCount",
			matchFunc:    matchAny,
			actionFunc:   metricsAdd,
			data:         &dataPoints{},
		}}},
	}
}

// counterExposition renders a Prometheus text exposition for the test counter.
// Passing more than one value emits several label series for the same metric on
// one target, which must be summed within that target.
func counterExposition(values ...float64) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# HELP %s test counter\n# TYPE %s counter\n", testCounterMetric, testCounterMetric)
	if len(values) == 1 {
		fmt.Fprintf(&b, "%s %g\n", testCounterMetric, values[0])
		return b.String()
	}
	for i, v := range values {
		fmt.Fprintf(&b, "%s{series=\"%d\"} %g\n", testCounterMetric, i, v)
	}
	return b.String()
}

// multiNodeTarget is a metricsTarget backed by an in-memory map of scrape
// targets to their exposition text. A target that is listed in order but absent
// from raw simulates a target that exists in the cluster but fails to scrape
// this poll.
type multiNodeTarget struct {
	interesting map[string]metricsConvert
	order       []string
	raw         map[string]string
}

func (m *multiNodeTarget) grabMetricsFromTarget(_ context.Context, target string) ([]byte, error) {
	text, ok := m.raw[target]
	if !ok {
		return nil, fmt.Errorf("scrape failed for target %q", target)
	}
	return []byte(text), nil
}

func (m *multiNodeTarget) getInterestingMetrics() map[string]metricsConvert { return m.interesting }
func (m *multiNodeTarget) getCWMetricsPublisher() publisher.Publisher       { return nil }
func (m *multiNodeTarget) getTargetList(context.Context) ([]string, error)  { return m.order, nil }
func (m *multiNodeTarget) submitCloudWatch() bool                           { return false }
func (m *multiNodeTarget) submitPrometheus() bool                           { return false }
func (m *multiNodeTarget) getLogger() logger.Logger                         { return logger.DefaultLogger() }

// poll runs one aggregation pass and returns whether it was flagged as a
// baseline/reset poll (which Handler skips publishing).
func poll(t *testing.T, target *multiNodeTarget) bool {
	t.Helper()
	_, _, resetDetected, err := metricsListGrabAggregateConvert(context.Background(), target)
	assert.NoError(t, err)
	return resetDetected
}

// published is the value that would be sent to CloudWatch for the test counter.
func published(target *multiNodeTarget) float64 {
	return target.interesting[testCounterMetric].actions[0].data.curSingleDataPoint
}

// TestCounterIdleClusterPublishesZeroRegardlessOfScrapeOrder is the headline
// regression for #3844. Three nodes sit at 10/20/30 lifetime requests and serve
// no new traffic. The helper must publish 0 on every poll after the first,
// whatever order the nodes are scraped in. The buggy code published c-a (or
// b+c) -- a non-zero, scrape-order-dependent value -- forever.
func TestCounterIdleClusterPublishesZeroRegardlessOfScrapeOrder(t *testing.T) {
	for _, order := range [][]string{{"a", "b", "c"}, {"c", "b", "a"}, {"b", "a", "c"}} {
		order := order
		t.Run(strings.Join(order, ""), func(t *testing.T) {
			target := &multiNodeTarget{
				interesting: newCounterMetrics(),
				order:       order,
				raw: map[string]string{
					"a": counterExposition(10),
					"b": counterExposition(20),
					"c": counterExposition(30),
				},
			}

			assert.True(t, poll(t, target), "first poll establishes the baseline and is skipped")

			assert.False(t, poll(t, target), "idle cluster should not be flagged as reset")
			assert.Equal(t, 0.0, published(target), "idle cluster must publish 0, not a scrape-order artifact")

			assert.False(t, poll(t, target))
			assert.Equal(t, 0.0, published(target), "repeated idle polls must stay at 0")
		})
	}
}

// TestCounterSumsPerNodeIncreasesOrderIndependently verifies that a real
// increase is the sum of each node's own delta and does not depend on order.
func TestCounterSumsPerNodeIncreasesOrderIndependently(t *testing.T) {
	for _, order := range [][]string{{"a", "b", "c"}, {"c", "a", "b"}} {
		order := order
		t.Run(strings.Join(order, ""), func(t *testing.T) {
			target := &multiNodeTarget{
				interesting: newCounterMetrics(),
				order:       order,
				raw: map[string]string{
					"a": counterExposition(10),
					"b": counterExposition(20),
					"c": counterExposition(30),
				},
			}
			assert.True(t, poll(t, target))

			// a:+5, b:+0, c:+8  => cluster delta 13
			target.raw["a"] = counterExposition(15)
			target.raw["c"] = counterExposition(38)
			assert.False(t, poll(t, target))
			assert.Equal(t, 13.0, published(target))
		})
	}
}

// TestCounterHandlesNodeRestart checks that a node whose counter goes backwards
// (ipamd restarted) contributes its post-reset total rather than a negative
// delta, and that this does not discard the rest of the cluster's poll.
func TestCounterHandlesNodeRestart(t *testing.T) {
	target := &multiNodeTarget{
		interesting: newCounterMetrics(),
		order:       []string{"a", "b"},
		raw: map[string]string{
			"a": counterExposition(100),
			"b": counterExposition(50),
		},
	}
	assert.True(t, poll(t, target))

	// a restarts: 100 -> 5. b unchanged. Expected delta = 5 (a's post-reset total) + 0.
	target.raw["a"] = counterExposition(5)
	assert.False(t, poll(t, target), "a single node restart must not skip the whole poll")
	assert.Equal(t, 5.0, published(target))

	// Next poll, both steady at 5 and 50 => 0.
	assert.False(t, poll(t, target))
	assert.Equal(t, 0.0, published(target))
}

// TestCounterDropsDepartedNode checks that a node leaving the cluster is removed
// rather than counted as a decrease.
func TestCounterDropsDepartedNode(t *testing.T) {
	target := &multiNodeTarget{
		interesting: newCounterMetrics(),
		order:       []string{"a", "b"},
		raw: map[string]string{
			"a": counterExposition(10),
			"b": counterExposition(20),
		},
	}
	assert.True(t, poll(t, target))

	// b leaves the cluster entirely; a advances by 2.
	target.order = []string{"a"}
	delete(target.raw, "b")
	target.raw["a"] = counterExposition(12)
	assert.False(t, poll(t, target))
	assert.Equal(t, 2.0, published(target), "a departed node must not subtract its last value")

	// b's baseline must be gone: a steady => 0.
	assert.False(t, poll(t, target))
	assert.Equal(t, 0.0, published(target))
}

// TestCounterBaselinesNewlyDiscoveredNode checks that a node joining an
// established cluster does not spike the published delta by its whole lifetime
// counter; its first observation only sets a baseline.
func TestCounterBaselinesNewlyDiscoveredNode(t *testing.T) {
	target := &multiNodeTarget{
		interesting: newCounterMetrics(),
		order:       []string{"a"},
		raw:         map[string]string{"a": counterExposition(10)},
	}
	assert.True(t, poll(t, target))

	// b joins with a large lifetime counter; a is unchanged.
	target.order = []string{"a", "b"}
	target.raw["b"] = counterExposition(50)
	assert.False(t, poll(t, target))
	assert.Equal(t, 0.0, published(target), "a newly discovered node must not spike by its lifetime total")

	// Now b does real work: +5. a unchanged.
	target.raw["b"] = counterExposition(55)
	assert.False(t, poll(t, target))
	assert.Equal(t, 5.0, published(target))
}

// TestCounterCarriesBaselineThroughScrapeFailure checks that a target which
// still exists but fails to scrape this poll keeps its baseline, so the delta is
// bridged across the gap when it recovers instead of being lost or double
// counted.
func TestCounterCarriesBaselineThroughScrapeFailure(t *testing.T) {
	target := &multiNodeTarget{
		interesting: newCounterMetrics(),
		order:       []string{"a", "b"},
		raw: map[string]string{
			"a": counterExposition(10),
			"b": counterExposition(20),
		},
	}
	assert.True(t, poll(t, target))

	// b is still a live target but its scrape fails this poll; a advances by 5.
	delete(target.raw, "b") // still in order => still "live", scrape errors
	target.raw["a"] = counterExposition(15)
	assert.False(t, poll(t, target))
	assert.Equal(t, 5.0, published(target), "a transient scrape failure should not fabricate a delta")

	// b recovers at 26 (it advanced by 6 across the gap); a unchanged.
	target.raw["b"] = counterExposition(26)
	assert.False(t, poll(t, target))
	assert.Equal(t, 6.0, published(target), "b's carried baseline must bridge the scrape gap")
}

// TestCounterSumsWithinTargetAndIsStableAcrossPolls checks that several label
// series on one target are summed for that target, and that repeated identical
// polls do not drift.
func TestCounterSumsWithinTargetAndIsStableAcrossPolls(t *testing.T) {
	target := &multiNodeTarget{
		interesting: newCounterMetrics(),
		order:       []string{"a"},
		raw:         map[string]string{"a": counterExposition(3, 4)}, // 3 + 4 = 7
	}
	assert.True(t, poll(t, target))

	assert.False(t, poll(t, target))
	assert.Equal(t, 0.0, published(target))

	// Repeated identical poll must still be 0 (no drift).
	assert.False(t, poll(t, target))
	assert.Equal(t, 0.0, published(target))

	// Series grow 3->3 and 4->9: within-target total 7 -> 12, delta 5.
	target.raw["a"] = counterExposition(3, 9)
	assert.False(t, poll(t, target))
	assert.Equal(t, 5.0, published(target))
}

// TestCounterBaselineStaysBoundedUnderChurn verifies that the per-target
// baseline does not grow without bound as nodes join and leave: departed
// targets must drop out so the retained state tracks only the current cluster.
func TestCounterBaselineStaysBoundedUnderChurn(t *testing.T) {
	const window = 3
	target := &multiNodeTarget{
		interesting: newCounterMetrics(),
		order:       nil,
		raw:         map[string]string{},
	}
	data := target.interesting[testCounterMetric].actions[0].data

	for step := 0; step < 100; step++ {
		// A sliding window of 3 live nodes; each node appears for a few polls
		// then is replaced, so 100 distinct node names churn through.
		target.order = target.order[:0]
		target.raw = map[string]string{}
		for j := 0; j < window; j++ {
			name := fmt.Sprintf("node-%d", step+j)
			target.order = append(target.order, name)
			target.raw[name] = counterExposition(float64(step + j))
		}
		poll(t, target)
		assert.LessOrEqual(t, len(data.lastTargetValues), window,
			"baseline map must not retain departed targets (step %d)", step)
	}
}

// TestCounterRecoversAfterParseError checks that a poll which aborts partway
// through on a malformed scrape does not leave half-accumulated per-target
// state that corrupts the next poll. The target scraped before the error ("a")
// must not be double counted once scraping recovers.
func TestCounterRecoversAfterParseError(t *testing.T) {
	target := &multiNodeTarget{
		interesting: newCounterMetrics(),
		order:       []string{"a", "b"}, // a is scraped before b fails
		raw: map[string]string{
			"a": counterExposition(10),
			"b": counterExposition(20),
		},
	}
	assert.True(t, poll(t, target)) // baseline: a=10, b=20

	// a advances to 12 and is scraped first; b then returns unparseable text,
	// so the poll aborts with an error before aggregation runs.
	target.raw["a"] = counterExposition(12)
	target.raw["b"] = "# TYPE awscni_add_ip_req_count counter\nawscni_add_ip_req_count abc\n"
	_, _, _, err := metricsListGrabAggregateConvert(context.Background(), target)
	assert.Error(t, err, "a malformed scrape should surface a parse error")

	// Scraping recovers. a is unchanged at 12 (delta 2 vs the baseline of 10),
	// b unchanged at 20 (delta 0). The aborted poll's partial read of a=12 must
	// not be carried into this one.
	target.raw["b"] = counterExposition(20)
	assert.False(t, poll(t, target))
	assert.Equal(t, 2.0, published(target), "counter must not double-count after a parse-error poll")
}
