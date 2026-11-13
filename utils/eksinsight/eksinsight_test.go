// Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//      http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package eksinsight

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	subnetA = "subnet-0abcc2fe41543d2ea"
	subnetB = "subnet-0bbbc2fe41543d2eb"
	// cooldown is the product default, so the hold-down is 60s and the window 30s.
	cooldown = 30 * time.Second
)

var (
	holdDown = HoldDownFor(cooldown)
	window   = stillFailingWindow(holdDown)
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newTestRegistry() (*Registry, *fakeClock) {
	clk := &fakeClock{t: time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)}
	return NewRegistry(DefaultMaxSubnets, clk.now), clk
}

// record notes one failed pod ADD naming the given subnets.
func record(r *Registry, subnets ...string) {
	r.Record(subnets, holdDown)
}

// keepFailing records a failure every 10s for d, which is what kubelet retrying a stuck pod
// looks like.
func keepFailing(r *Registry, clk *fakeClock, d time.Duration, subnets ...string) {
	for elapsed := time.Duration(0); elapsed <= d; elapsed += 10 * time.Second {
		record(r, subnets...)
		clk.advance(10 * time.Second)
	}
}

// series counts the eks_insight series a scrape would see now.
func series(r *Registry) int { return testutil.CollectAndCount(r, MetricName) }

// published returns the subnets a scrape would see now.
func published(t *testing.T, r *Registry) []string {
	t.Helper()
	reg := prometheus.NewPedanticRegistry()
	require.NoError(t, reg.Register(r))
	families, err := reg.Gather()
	require.NoError(t, err)

	var subnets []string
	for _, family := range families {
		if family.GetName() != MetricName {
			continue
		}
		for _, metric := range family.GetMetric() {
			for _, label := range metric.GetLabel() {
				if label.GetName() == labelSubnetID {
					subnets = append(subnets, label.GetValue())
				}
			}
		}
	}
	return subnets
}

// An unchecked collector, one that describes nothing, still renders but gives up the
// registry's duplicate and consistency checking at registration.
func TestDescribeAdvertisesTheMetric(t *testing.T) {
	r, _ := newTestRegistry()
	ch := make(chan *prometheus.Desc, 8)
	r.Describe(ch)
	close(ch)

	var descs []*prometheus.Desc
	for d := range ch {
		descs = append(descs, d)
	}
	require.Len(t, descs, 1)
	assert.Contains(t, descs[0].String(), MetricName)
}

// The exposition is a public contract, so a change to the metric name, a label name or the
// value is a breaking change and must be deliberate.
func TestExpositionMatchesTheContract(t *testing.T) {
	r, clk := newTestRegistry()
	keepFailing(r, clk, holdDown, subnetA)

	reg := prometheus.NewPedanticRegistry()
	require.NoError(t, reg.Register(r))

	expected := `
# HELP eks_insight Active problem reported by this component. Present while active.
# TYPE eks_insight gauge
eks_insight{reason="InsufficientSubnetAddressCapacity",resource_subnet_id="subnet-0abcc2fe41543d2ea",type="PodIPAllocationFailed"} 1
`
	require.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(expected), MetricName))
}

func TestHoldDownIsDerivedFromTheCooldownAndClamped(t *testing.T) {
	assert.Equal(t, 60*time.Second, HoldDownFor(30*time.Second))
	assert.Equal(t, 120*time.Second, HoldDownFor(60*time.Second))
	assert.Equal(t, minHoldDown, HoldDownFor(0))
	assert.Equal(t, minHoldDown, HoldDownFor(time.Second))
	assert.Equal(t, maxHoldDown, HoldDownFor(10*time.Minute))
}

// The window must stay shorter than the hold-down at every cooldown setting. Reverse them
// and a single isolated failure becomes publishable.
func TestWindowIsAlwaysShorterThanTheHoldDown(t *testing.T) {
	for _, c := range []time.Duration{0, time.Second, 10 * time.Second, 30 * time.Second, 10 * time.Minute} {
		hd := HoldDownFor(c)
		assert.Less(t, stillFailingWindow(hd), hd, "cooldown=%s", c)
		assert.GreaterOrEqual(t, stillFailingWindow(hd), 30*time.Second, "cooldown=%s", c)
	}
}

func TestATransientFailureIsNotReported(t *testing.T) {
	r, clk := newTestRegistry()
	record(r, subnetA)
	assert.Equal(t, 0, series(r))

	clk.advance(cooldown)
	record(r, subnetA)
	assert.Equal(t, 0, series(r), "still inside the hold-down")

	keepFailing(r, clk, holdDown, subnetA)
	assert.Equal(t, 1, series(r), "past the hold-down")
}

// A lone failure followed by silence must never publish, whatever the cooldown setting and
// whether or not anything scrapes in between.
func TestALoneFailureNeverPublishes(t *testing.T) {
	for _, c := range []time.Duration{0, 10 * time.Second, 30 * time.Second} {
		r, clk := newTestRegistry()
		r.Record([]string{subnetA}, HoldDownFor(c))
		for elapsed := time.Duration(0); elapsed < 10*time.Minute; elapsed += 5 * time.Second {
			require.Equal(t, 0, series(r), "cooldown=%s at t+%s", c, elapsed)
			clk.advance(5 * time.Second)
		}
	}
}

// Two failures far apart are two lone failures, not one lasting problem. Nothing scrapes
// during the gap, which is what production looks like between 5-minute scrapes.
func TestAGapRestartsTheHoldDown(t *testing.T) {
	r, clk := newTestRegistry()
	record(r, subnetA)
	clk.advance(window + 20*time.Second)
	record(r, subnetA)
	clk.advance(10 * time.Second)
	assert.Equal(t, 0, series(r), "the gap means the first failure does not count")

	keepFailing(r, clk, holdDown, subnetA)
	assert.Equal(t, 1, series(r))
}

func TestAnOngoingProblemStaysPublished(t *testing.T) {
	r, clk := newTestRegistry()
	for minute := 0; minute < 30; minute++ {
		keepFailing(r, clk, time.Minute, subnetA)
		if minute >= 1 {
			require.Equal(t, 1, series(r), "minute %d", minute)
		}
	}
}

func TestSeriesClearsWhenFailuresStop(t *testing.T) {
	r, clk := newTestRegistry()
	keepFailing(r, clk, holdDown, subnetA)
	require.Equal(t, 1, series(r))

	clk.advance(window + time.Second)
	assert.Equal(t, 0, series(r))
}

// Each subnet keeps its own clock, so a record naming fewer subnets does not drop the rest.
// A plain pod names every starved card's subnets; a multi-NIC pod stops at the first starved
// card and names fewer.
func TestEachSubnetExpiresOnItsOwn(t *testing.T) {
	r, clk := newTestRegistry()
	keepFailing(r, clk, holdDown, subnetA, subnetB)
	require.ElementsMatch(t, []string{subnetA, subnetB}, published(t, r))

	record(r, subnetA)
	clk.advance(10 * time.Second)
	assert.ElementsMatch(t, []string{subnetA, subnetB}, published(t, r),
		"B is still inside its window")

	// Keep naming A for longer than the window, without naming B.
	keepFailing(r, clk, window+10*time.Second, subnetA)
	assert.Equal(t, []string{subnetA}, published(t, r), "B has expired, A has not")
}

// A subnet joining an ongoing problem serves its own hold-down.
func TestANewSubnetServesItsOwnHoldDown(t *testing.T) {
	r, clk := newTestRegistry()
	keepFailing(r, clk, holdDown, subnetA)
	require.Equal(t, []string{subnetA}, published(t, r))

	record(r, subnetA, subnetB)
	assert.Equal(t, []string{subnetA}, published(t, r), "B has only just appeared")

	keepFailing(r, clk, holdDown, subnetA, subnetB)
	assert.ElementsMatch(t, []string{subnetA, subnetB}, published(t, r))
}

func TestReportedSubnetsAreSorted(t *testing.T) {
	r, clk := newTestRegistry()
	keepFailing(r, clk, holdDown, subnetB, subnetA, subnetA)
	assert.Equal(t, []string{subnetA, subnetB}, published(t, r))
}

// A custom collector gives up the library's label checking, and the ENIConfig subnet is a
// free-form string a cluster admin controls.
func TestMalformedSubnetIDsAreRefused(t *testing.T) {
	r, clk := newTestRegistry()
	bad := []string{
		"", "please fix my subnet", "subnet-", "subnet-NOTHEX", "vpc-0abcc2fe41543d2ea",
		"subnet-0abcc2fe41543d2ea extra", "subnet-0abcc2fe41543d2ea\"},x{y=\"1",
		"subnet-0abcc2fe41543d2eaffffffffffffffff",
	}
	keepFailing(r, clk, holdDown, bad...)
	assert.Equal(t, 0, series(r))
	// Reported once per distinct value, not once per kubelet retry.
	assert.Equal(t, float64(len(bad)), testutil.ToFloat64(r.Dropped().WithLabelValues(dropReasonInvalidSubnet)))

	r2, clk2 := newTestRegistry()
	keepFailing(r2, clk2, holdDown, append(bad, subnetA)...)
	assert.Equal(t, []string{subnetA}, published(t, r2), "a good subnet alongside bad ones is reported")
}

// The drop gauges report what is being dropped now, so maxSubnets bounds the series this
// node publishes and not the numbers these two carry.
func TestTheDropGaugesReportTheTrueCount(t *testing.T) {
	r, clk := newTestRegistry()
	bad := make([]string, 0, DefaultMaxSubnets+3)
	for i := 0; i < DefaultMaxSubnets+3; i++ {
		bad = append(bad, fmt.Sprintf("not a subnet %d", i))
	}

	keepFailing(r, clk, holdDown, bad...)
	assert.Equal(t, 0, series(r))
	assert.Equal(t, float64(DefaultMaxSubnets+3),
		testutil.ToFloat64(r.Dropped().WithLabelValues(dropReasonInvalidSubnet)),
		"the count is one series whatever its value, so it is not capped at maxSubnets")
}

// A misconfiguration that is fixed must stop being reported, which a counter could never do.
// Both gauges are written during a scrape, so each assertion needs one.
func TestTheInvalidGaugeClearsWhenTheValuesStopArriving(t *testing.T) {
	r, clk := newTestRegistry()
	keepFailing(r, clk, holdDown, "not a subnet")
	series(r)
	require.Equal(t, 1.0, testutil.ToFloat64(r.Dropped().WithLabelValues(dropReasonInvalidSubnet)))

	// Nothing names it again, so it ages out on the same window the subnets use.
	clk.advance(window + time.Second)
	series(r)
	assert.Equal(t, 0.0, testutil.ToFloat64(r.Dropped().WithLabelValues(dropReasonInvalidSubnet)))
}

// The same for the cap: once the node is back under it, nothing is being withheld.
func TestTheCapGaugeClearsWhenTheOverflowGoesAway(t *testing.T) {
	r, clk := newTestRegistry()
	many := make([]string, 0, DefaultMaxSubnets+3)
	for i := 0; i < DefaultMaxSubnets+3; i++ {
		many = append(many, fmt.Sprintf("subnet-%016x", i))
	}
	keepFailing(r, clk, holdDown, many...)
	require.Equal(t, DefaultMaxSubnets, series(r))
	require.Equal(t, 3.0, testutil.ToFloat64(r.Dropped().WithLabelValues(dropReasonCap)))

	// Keep naming only the first few, so the rest expire and the overflow is gone.
	keepFailing(r, clk, window+10*time.Second, many[:5]...)
	assert.Equal(t, 5, series(r))
	assert.Equal(t, 0.0, testutil.ToFloat64(r.Dropped().WithLabelValues(dropReasonCap)))
}

func TestSubnetCountIsCappedAndTheOverflowReported(t *testing.T) {
	r, clk := newTestRegistry()
	many := make([]string, 0, DefaultMaxSubnets+3)
	for i := 0; i < DefaultMaxSubnets+3; i++ {
		many = append(many, fmt.Sprintf("subnet-%016x", i))
	}
	keepFailing(r, clk, holdDown, many...)
	assert.Equal(t, DefaultMaxSubnets, series(r))
	assert.Equal(t, 3.0, testutil.ToFloat64(r.Dropped().WithLabelValues(dropReasonCap)))

	// Scraping again reports the same overflow rather than accumulating it.
	require.Equal(t, DefaultMaxSubnets, series(r))
	assert.Equal(t, 3.0, testutil.ToFloat64(r.Dropped().WithLabelValues(dropReasonCap)))
}

// Label values must stay within these patterns, asserted on the constants so a rename
// cannot quietly widen them.
func TestLabelValuesSatisfyTheDocumentedPatterns(t *testing.T) {
	for _, v := range []string{TypePodIPAllocationFailed, ReasonInsufficientSubnetAddressCapacity} {
		assert.Regexp(t, `^[A-Z][A-Za-z0-9]{2,63}$`, v)
	}
	assert.Equal(t, "resource_subnet_id", labelSubnetID)
	assert.Regexp(t, `^[A-Za-z0-9._:/-]{1,64}$`, subnetA)
}

func TestConcurrentRecordAndCollectDoNotRace(t *testing.T) {
	r, _ := newTestRegistry()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); record(r, subnetA, subnetB) }()
		go func() { defer wg.Done(); series(r) }()
	}
	wg.Wait()
}
