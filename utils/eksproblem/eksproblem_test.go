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

package eksproblem

import (
	"fmt"
	"strings"
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
	// cooldown is the product default; hold-down is therefore 60s.
	cooldown = 30 * time.Second
)

var holdDown = HoldDownFor(cooldown)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newTestRegistry() (*Registry, *fakeClock) {
	clk := &fakeClock{t: time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)}
	r := NewRegistry(defaultMaxSubnets, clk.now)
	Dropped.Reset() // package-level counter shared by every test in the process
	return r, clk
}

// fail simulates one failed pod ADD at the current time.
func fail(r *Registry, subnets ...string) {
	r.RecordPodIPAllocationFailure(subnets, holdDown)
}

// keepFailing simulates kubelet retrying a failed sandbox once a second for d, which is
// what a real outage looks like. Tests must use this rather than record-once-then-advance,
// because a problem only counts as current while failures keep arriving.
func keepFailing(r *Registry, clk *fakeClock, d time.Duration, subnets ...string) {
	for elapsed := time.Duration(0); elapsed <= d; elapsed += time.Second {
		r.RecordPodIPAllocationFailure(subnets, holdDown)
		clk.advance(time.Second)
	}
}

// series counts the eks_problem series a scrape would see right now.
func series(r *Registry) int { return testutil.CollectAndCount(r, MetricName) }

// TestExpositionMatchesNodeTelemetryContract is the contract test. The Node Telemetry
// design fixes the exact shape the scraper matches on; a change to the metric name, a
// label name, or the value breaks the pipeline silently, because the scraper just stops
// matching. This test is what makes that loud.
func TestExpositionMatchesNodeTelemetryContract(t *testing.T) {
	r, clk := newTestRegistry()
	keepFailing(r, clk, holdDown+time.Second, subnetA)

	reg := prometheus.NewPedanticRegistry()
	require.NoError(t, reg.Register(r))

	expected := `
# HELP eks_problem Active problem reported by this component. Present while active.
# TYPE eks_problem gauge
eks_problem{reason="InsufficientFreeAddresses",resource_subnet_id="subnet-0abcc2fe41543d2ea",type="PodIPAllocationFailed"} 1
`
	require.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(expected), MetricName))
}

// Ordinary pod churn produces brief failures: a released address is unassignable for one
// cooldown period and then comes back by itself, with kubelet's retry picking it up. The
// hold-down exists so that case is never reported, and it is derived from the cooldown
// rather than chosen so it cannot be out of step with it.
func TestTransientFailureInsideTheHoldDownIsNotReported(t *testing.T) {
	r, clk := newTestRegistry()
	fail(r, subnetA)
	assert.Equal(t, 0, series(r), "a single failure must publish nothing")

	clk.advance(cooldown) // one cooldown later: the churn case would have recovered here
	fail(r, subnetA)
	assert.Equal(t, 0, series(r), "still inside the hold-down")

	// Keep failing, as kubelet's retries would, until past the hold-down.
	for elapsed := cooldown; elapsed <= holdDown+time.Second; elapsed += 5 * time.Second {
		fail(r, subnetA)
		clk.advance(5 * time.Second)
	}
	assert.Equal(t, 1, series(r), "past the hold-down, a persistent failure is reported")
}

func TestHoldDownIsDerivedFromTheCooldownAndClamped(t *testing.T) {
	assert.Equal(t, 60*time.Second, HoldDownFor(30*time.Second), "default: 2x cooldown")
	assert.Equal(t, 120*time.Second, HoldDownFor(60*time.Second), "tracks a tuned cooldown")
	// IP_COOLDOWN_PERIOD=0 is legal and is the knob customers under address pressure turn
	// down; it must still get a persistence gate.
	assert.Equal(t, minHoldDown, HoldDownFor(0))
	assert.Equal(t, minHoldDown, HoldDownFor(time.Second))
	// And a very large cooldown must not delay detection of a real outage indefinitely.
	assert.Equal(t, maxHoldDown, HoldDownFor(10*time.Minute))
}

// ClearPodIPAllocationFailure is for the hard-capacity case, not for a successful ADD.
func TestExplicitClearEndsTheRun(t *testing.T) {
	r, clk := newTestRegistry()
	keepFailing(r, clk, holdDown+time.Second, subnetA, subnetB)
	require.Equal(t, 2, series(r))

	r.ClearPodIPAllocationFailure()
	assert.Equal(t, 0, series(r), "a success must clear every subnet at once")

	since, subnets := r.pending()
	assert.True(t, since.IsZero())
	assert.Empty(t, subnets)
}

// THE clear path: failures stop arriving. That covers both recoveries - the addresses came
// back, or the pending pods were deleted - and is deliberately the only one. A success on
// some other pod is not evidence that this pod's subnet recovered.
func TestSeriesClearsWhenFailuresStopArriving(t *testing.T) {
	r, clk := newTestRegistry()
	keepFailing(r, clk, holdDown+time.Second, subnetA)
	require.Equal(t, 1, series(r))

	clk.advance(stillFailingWindow(holdDown) + time.Second) // nothing failing any more
	assert.Equal(t, 0, series(r), "failures stopped, so the problem is over")

	since, _ := r.pending()
	assert.True(t, since.IsZero(), "clearing must reset the run, not just hide it")
}

// A single failed ADD followed by silence must NEVER publish, at any cooldown setting.
// This was a real defect: when the "still failing" window was longer than the hold-down
// there was an interval in which one momentary shortfall on an idle node was reported.
// The fix is the ordering invariant in stillFailingWindow, and this test pins it.
func TestAnIsolatedFailureNeverPublishes(t *testing.T) {
	for _, cooldown := range []time.Duration{0, time.Second, 10 * time.Second, 20 * time.Second, 30 * time.Second} {
		r, clk := newTestRegistry()
		r.RecordPodIPAllocationFailure([]string{subnetA}, HoldDownFor(cooldown))
		for elapsed := time.Duration(0); elapsed < 10*time.Minute; elapsed += 5 * time.Second {
			require.Equal(t, 0, series(r),
				"cooldown=%s: a lone failure must not publish at t=+%s", cooldown, elapsed)
			clk.advance(5 * time.Second)
		}
	}
}

// An ongoing outage must be visible to a scraper that only looks every 5 minutes, so the
// series must stay up for as long as failures keep arriving - not expire on a timer.
func TestAnOngoingOutageStaysPublishedAcrossScrapeIntervals(t *testing.T) {
	r, clk := newTestRegistry()
	for minute := 0; minute < 30; minute++ {
		for i := 0; i < 60; i++ { // failing once a second, all through
			fail(r, subnetA)
			clk.advance(time.Second)
		}
		if minute >= 1 { // after the first minute the hold-down is served
			require.Equal(t, 1, series(r), "still failing at minute %d, must still be published", minute)
		}
	}
}

// The subnet list only changes when the node's ENIs do, and an ENI change that relieves the
// shortage also stops the failures - which ends the run through the usual path. So a changed
// list must NOT restart the run: doing so delays, or with repeated changes suppresses,
// reporting of a problem that is still happening.
func TestAChangedSubnetSetKeepsReporting(t *testing.T) {
	r, clk := newTestRegistry()
	keepFailing(r, clk, holdDown+time.Second, subnetA)
	require.Equal(t, 1, series(r))

	fail(r, subnetA, subnetB) // an ENI attached in another subnet, still no addresses
	assert.Equal(t, 2, series(r), "the run continues; both subnets are reported")
}

// A recovery followed by a fresh problem must start a new hold-down, not inherit the old
// run's age - otherwise the next failure would be reported instantly.
func TestRunRestartsAfterAClear(t *testing.T) {
	r, clk := newTestRegistry()
	keepFailing(r, clk, holdDown+time.Second, subnetA)
	require.Equal(t, 1, series(r))

	r.ClearPodIPAllocationFailure()
	fail(r, subnetA)
	assert.Equal(t, 0, series(r), "the new run must serve its own hold-down")
	keepFailing(r, clk, holdDown+time.Second, subnetA)
	assert.Equal(t, 1, series(r))
}

// Repeated failures extend the run; they must not keep resetting its start, or a problem
// that keeps failing every second would never reach the hold-down.
func TestRepeatedFailuresDoNotResetTheRun(t *testing.T) {
	r, clk := newTestRegistry()
	for i := 0; i < 59; i++ { // kubelet retries about once a second
		fail(r, subnetA)
		clk.advance(time.Second)
	}
	assert.Equal(t, 0, series(r), "59s of failures is still inside the 60s hold-down")
	clk.advance(2 * time.Second)
	fail(r, subnetA)
	assert.Equal(t, 1, series(r))
}

// The subnets reported are whatever the failing allocation scanned, so the count follows
// the node's ENIs: one subnet without subnet discovery, several with it.
func TestReportsEverySubnetTheAttemptDrewFrom(t *testing.T) {
	r, clk := newTestRegistry()
	keepFailing(r, clk, holdDown+time.Second, subnetB, subnetA, subnetA) // duplicates collapsed
	assert.Equal(t, 2, series(r))
}

// The set is sorted so successive scrapes agree, which the consumer needs in order to show
// a stable list rather than one that reshuffles every five minutes.
func TestReportedSubnetsAreStableAcrossScrapes(t *testing.T) {
	r, clk := newTestRegistry()
	keepFailing(r, clk, holdDown+time.Second, subnetB, subnetA)
	_, first := r.pending()
	fail(r, subnetA, subnetB) // same set, recorded in the other order
	_, second := r.pending()

	require.Len(t, first, 2)
	assert.Equal(t, []string{subnetA, subnetB}, first, "must be sorted, not arrival-ordered")
	assert.Equal(t, first, second, "a re-record of the same set must not reorder it")
	assert.Equal(t, 2, series(r))
}

// A custom Collector gives up client_golang's label-set checking, and ENIConfig.Subnet is
// a free-form string a cluster admin controls, so nothing malformed may reach a label.
func TestMalformedSubnetIDsAreRefused(t *testing.T) {
	r, clk := newTestRegistry()
	bad := []string{
		"", "please fix my subnet", "subnet-", "subnet-NOTHEX", "vpc-0abcc2fe41543d2ea",
		"subnet-0abcc2fe41543d2ea extra", "subnet-0abcc2fe41543d2ea\"},x{y=\"1",
		"subnet-0abcc2fe41543d2eaffffffffffffffff",
	}
	keepFailing(r, clk, holdDown+time.Second, bad...)
	assert.Equal(t, 0, series(r))
	// Counted once for the run, not once per kubelet retry.
	assert.Equal(t, float64(len(bad)), testutil.ToFloat64(Dropped.WithLabelValues(dropReasonInvalidSubnet)))

	// A good subnet alongside bad ones is still reported.
	r2, clk2 := newTestRegistry()
	keepFailing(r2, clk2, holdDown+time.Second, append(bad, subnetA)...)
	assert.Equal(t, 1, series(r2))
}

func TestSubnetCountIsCappedAndDropsAreCounted(t *testing.T) {
	r, clk := newTestRegistry()
	many := make([]string, 0, defaultMaxSubnets+3)
	for i := 0; i < defaultMaxSubnets+3; i++ {
		many = append(many, fmt.Sprintf("subnet-%016x", i))
	}
	keepFailing(r, clk, holdDown+time.Second, many...)
	assert.Equal(t, defaultMaxSubnets, series(r))
	// Counted once for the run, not once per kubelet retry.
	assert.Equal(t, 3.0, testutil.ToFloat64(Dropped.WithLabelValues(dropReasonCap)))
}

// This counter exists to say "the published list is incomplete", so it has to stay current
// while a run is live. An outage that spreads - more subnets going over the cap while the
// same run keeps failing - is exactly when it matters, and counting only at the start of a
// run missed it.
func TestDropsAreRecountedWhenARunGetsWorse(t *testing.T) {
	r, clk := newTestRegistry()
	subnet := func(i int) string { return fmt.Sprintf("subnet-%016x", i) }
	list := func(n int) []string {
		out := make([]string, 0, n)
		for i := 0; i < n; i++ {
			out = append(out, subnet(i))
		}
		return out
	}

	keepFailing(r, clk, holdDown+time.Second, list(defaultMaxSubnets+3)...)
	require.Equal(t, 3.0, testutil.ToFloat64(Dropped.WithLabelValues(dropReasonCap)))

	// The same run, still failing, now drawing from five more subnets.
	fail(r, list(defaultMaxSubnets+8)...)
	assert.Equal(t, 8.0, testutil.ToFloat64(Dropped.WithLabelValues(dropReasonCap)),
		"the extra five must be counted, not swallowed because the run already existed")

	// Retries at the same width must not keep adding.
	fail(r, list(defaultMaxSubnets+8)...)
	fail(r, list(defaultMaxSubnets+8)...)
	assert.Equal(t, 8.0, testutil.ToFloat64(Dropped.WithLabelValues(dropReasonCap)),
		"counting per call would measure kubelet's retry rate instead")

	// A new run starts its own high-water mark.
	r.ClearPodIPAllocationFailure()
	fail(r, list(defaultMaxSubnets+1)...)
	assert.Equal(t, 9.0, testutil.ToFloat64(Dropped.WithLabelValues(dropReasonCap)))
}

// Every label value must satisfy the receiver's patterns, asserted on the constants
// themselves so a future rename cannot quietly start emitting points the server rejects.
func TestLabelValuesSatisfyTheReceiverPatterns(t *testing.T) {
	for _, v := range []string{TypePodIPAllocationFailed, ReasonInsufficientFreeAddresses} {
		assert.Regexp(t, `^[A-Z][A-Za-z0-9]{2,63}$`, v)
	}
	// resource_subnet_id must camelCase to a key the receiver accepts, and the label name
	// itself is what gets transformed - so assert on the constant, not on a literal.
	require.True(t, strings.HasPrefix(labelSubnetID, "resource_"))
	key := strings.TrimPrefix(labelSubnetID, "resource_")
	parts := strings.Split(key, "_")
	camel := parts[0]
	for _, p := range parts[1:] {
		camel += strings.ToUpper(p[:1]) + p[1:]
	}
	assert.Regexp(t, `^[a-z][a-zA-Z0-9]{1,31}$`, camel)
	assert.Equal(t, "subnetId", camel)
	// And anything subnetIDPattern accepts must satisfy the receiver's value pattern.
	for _, ok := range []string{"subnet-0abcc2fe", "subnet-0abcc2fe41543d2ea"} {
		require.Regexp(t, subnetIDPattern, ok)
		assert.Regexp(t, `^[A-Za-z0-9._:/-]{1,64}$`, ok)
	}
}

func TestConcurrentRecordAndCollectDoNotRace(t *testing.T) {
	r, _ := newTestRegistry()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 300; i++ {
			fail(r, subnetA, subnetB)
			if i%7 == 0 {
				r.ClearPodIPAllocationFailure()
			}
		}
	}()
	for i := 0; i < 300; i++ {
		series(r)
	}
	<-done
}

// The ordering invariant that makes an isolated failure unpublishable.
func TestStillFailingWindowIsAlwaysShorterThanTheHoldDown(t *testing.T) {
	for _, cooldown := range []time.Duration{0, time.Second, 15 * time.Second, 30 * time.Second,
		60 * time.Second, 5 * time.Minute, time.Hour} {
		hd := HoldDownFor(cooldown)
		assert.Less(t, stillFailingWindow(hd), hd, "cooldown=%s hold-down=%s", cooldown, hd)
	}
}
