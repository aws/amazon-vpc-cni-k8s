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

// Package eksproblem publishes ipamd's active problems as the reserved `eks_problem`
// gauge, in the shape the EKS node telemetry pipeline scrapes:
//
//	# TYPE eks_problem gauge
//	eks_problem{type="PodIPAllocationFailed",reason="InsufficientFreeAddresses",resource_subnet_id="subnet-0abc..."} 1
//
// A series present with value 1 means the problem is active; an absent series means it has
// cleared. The scraper forwards only series whose value is 1, renaming `type`/`reason` to
// the OTLP attributes `problem.type`/`problem.reason` and `resource_subnet_id` to
// `resource.subnetId`.
//
// # What it reports, and what it deliberately does not
//
// The one statement this package makes is: *from this node's point of view, a pod could
// not be given an IP address, and it has stayed that way*. That is the only condition a
// customer can act on, and it is what the consuming insight describes.
//
// It is driven entirely by the pod-ADD path, never by ipamd's pool-growth decisions. That
// matters because the two are independent:
//
//   - ipamd keeps a warm buffer, so EC2 refusing a *buffer top-up* is routine and costs no
//     pod an address. Reporting that would claim a pod IP allocation failed when none had.
//   - conversely ipamd can judge its pool adequate and stop attempting growth while pod
//     ADDs fail, in which case no EC2 call is made and there is no error to observe.
//
// Being downstream of every configuration knob also makes this independent of them: it
// behaves the same with or without prefix delegation, with or without subnet discovery,
// and whatever WARM_IP_TARGET / WARM_ENI_TARGET / WARM_PREFIX_TARGET are set to.
//
// # Why a hold-down, and why it is derived rather than chosen
//
// Not every failed ADD is worth reporting. An address released by a deleted pod is
// unassignable for IP_COOLDOWN_PERIOD, so ordinary pod churn produces brief, self-healing
// failures - kubelet retries the sandbox roughly once a second and succeeds as soon as an
// address leaves cooldown. A problem that outlives twice that period cannot be explained
// by cooldown, so the hold-down is derived from the same setting rather than picked. A
// customer who tunes IP_COOLDOWN_PERIOD therefore keeps a correct gate for free.
//
// # Why a custom Collector rather than a GaugeVec
//
// Whether a series exists depends on how long the failure has persisted, which can only be
// evaluated at scrape time. `eks_problem` is also a shared metric name whose resource label
// differs per problem, and a GaugeVec fixes its label set at construction. Because a custom
// Collector gives up client_golang's label-set guarantees, every label value is validated
// here before it is emitted.
package eksproblem

import (
	"fmt"
	"regexp"
	"sort"
	"sync"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
	"github.com/prometheus/client_golang/prometheus"
)

// log is resolved lazily so that importing this package does not build a logger.
var log = logger.Lazy()

// MetricName is fixed by the node telemetry contract; the scraper matches it exactly.
const MetricName = "eks_problem"

// Label names are fixed by the same contract. `resource_` is the prefix the scraper turns
// into the OTLP `resource.*` attribute namespace.
const (
	labelType     = "type"
	labelReason   = "reason"
	labelSubnetID = "resource_subnet_id"
)

// Problem type and reason. These are tokens from a closed catalogue, not free text: the
// receiver requires ^[A-Z][A-Za-z0-9]{2,63}$ and the consumer keys its behaviour on the
// (type, reason) pair, so renaming either is a breaking contract change.
//
// There is exactly one reason, deliberately. EC2's error codes cannot distinguish subnet
// conditions: which of InsufficientFreeAddressesInSubnet and InsufficientCidrBlocks comes
// back is decided by the shape of the request (/32s versus /28 prefixes), not by what is
// wrong with the subnet - a completely full subnet returns either one depending on what
// was asked of it. Keying the reason on that would also silently exclude every
// prefix-delegation node, since in that mode every request ipamd makes is a prefix
// request. So the reason states what this package actually knows.
const (
	TypePodIPAllocationFailed       = "PodIPAllocationFailed"
	ReasonInsufficientFreeAddresses = "InsufficientFreeAddresses"
)

const (
	// holdDownMultiple is applied to IP_COOLDOWN_PERIOD to get the hold-down. Two cooldown
	// periods cannot be survived by a cooldown-induced failure, because the address that
	// caused it becomes assignable after one.
	holdDownMultiple = 2

	// minStillFailing floors how recently a failure must have been seen for the problem to
	// count as current. kubelet retries a failed sandbox about once a second, so even a
	// few seconds of silence means the failures have stopped; this is generous.
	minStillFailing = 10 * time.Second

	// minHoldDown floors the hold-down so that IP_COOLDOWN_PERIOD=0 - legal, and the knob
	// customers under address pressure actually turn down - still gets a persistence gate.
	minHoldDown = 60 * time.Second

	// maxHoldDown caps it, so a large IP_COOLDOWN_PERIOD cannot delay detection of a real
	// outage indefinitely. At 5 minutes a problem is reported by the second scrape.
	maxHoldDown = 5 * time.Minute

	// defaultMaxSubnets caps how many subnets one node reports. The scraper tracks a
	// bounded number of source series per metric and drops the whole metric for an
	// interval once a target exceeds it, so an uncapped metric would fail hardest exactly
	// when an outage is widest. A node draws pod addresses from the subnets its own ENIs
	// sit in, which is normally one or two, so this is a backstop rather than a limit
	// expected to bind.
	defaultMaxSubnets = 20
)

const (
	dropReasonCap           = "cap"
	dropReasonInvalidSubnet = "invalid_subnet"
)

var (
	// subnetIDPattern guards the one label value that does not come from a constant.
	subnetIDPattern = regexp.MustCompile(`^subnet-[0-9a-f]{8,17}$`)

	problemDesc = prometheus.NewDesc(
		MetricName,
		"Active problem reported by this component. Present while active.",
		[]string{labelType, labelReason, labelSubnetID},
		nil,
	)

	// Dropped counts what this package refused to report and why. It is ipamd's own
	// operational metric, not part of the telemetry contract.
	Dropped = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "awscni_eks_problem_dropped_total",
			Help: "The number of eks_problem observations dropped, by reason",
		},
		[]string{"reason"},
	)

	// Subnets is the process-wide registry: the pod-ADD path records into it and the
	// metrics endpoint collects from it.
	Subnets = NewRegistry(defaultMaxSubnets, time.Now)
)

// Registry holds the current pod-IP-allocation failure, if any, and renders it as the
// gauge. It is safe for concurrent use: CNI ADD handlers and the metrics endpoint touch it
// from different goroutines.
type Registry struct {
	mu         sync.Mutex
	maxSubnets int
	now        func() time.Time

	// pendingSince is when the current unbroken run of failures began. Zero means no
	// failure is outstanding.
	pendingSince time.Time
	// lastFailure is the most recent failure in that run, used to notice that the
	// failures have stopped arriving without a success ever being seen - which happens
	// when the pending pods are deleted rather than satisfied.
	lastFailure time.Time
	// holdDown is how long a run must persist before it is reported. Supplied by the
	// caller from IP_COOLDOWN_PERIOD so this package holds no configuration of its own.
	holdDown time.Duration
	// subnets are the subnets the failing allocation attempt actually drew from.
	subnets []string
	// droppedThisRun is the high-water mark of subnets refused in the current run, by
	// reason, so that a run whose subnet list grows keeps the Dropped counter current
	// without counting the same refusal once per kubelet retry.
	droppedThisRun map[string]int
}

// NewRegistry returns a Registry capped at maxSubnets reported subnets, reading the clock
// through now. Production uses the process-wide Subnets registry; callers that need their
// own instance - tests driving the real call sites, which must neither share state nor
// wait out a real hold-down - construct one here. A nil now means time.Now.
func NewRegistry(maxSubnets int, now func() time.Time) *Registry {
	if now == nil {
		now = time.Now
	}
	return &Registry{maxSubnets: maxSubnets, now: now}
}

// HoldDownFor converts an IP cooldown period into the hold-down this package applies.
func HoldDownFor(ipCooldownPeriod time.Duration) time.Duration {
	holdDown := holdDownMultiple * ipCooldownPeriod
	if holdDown < minHoldDown {
		return minHoldDown
	}
	if holdDown > maxHoldDown {
		return maxHoldDown
	}
	return holdDown
}

// RecordPodIPAllocationFailure notes that a pod could not be given an address, and which
// subnets the attempt drew from. subnets is the set of subnets holding the ENIs the
// allocator actually scanned, so when the attempt fails every one of them failed to supply
// this node. holdDown comes from IP_COOLDOWN_PERIOD via HoldDownFor.
//
// Repeated calls extend the current run rather than restarting it; the run's age is what
// the hold-down is measured against. A changed subnet set does not restart it either: the
// list only changes when the node's ENIs do, and an ENI change that relieves the shortage
// also stops the failures, which ends the run through the usual path.
//
// It reports whether this call started a new run, so the caller can log a failure once per
// run instead of once per kubelet retry.
func (r *Registry) RecordPodIPAllocationFailure(subnets []string, holdDown time.Duration) bool {
	kept := make([]string, 0, len(subnets))
	seen := make(map[string]struct{}, len(subnets))
	invalid := 0
	for _, s := range subnets {
		if _, dup := seen[s]; dup {
			continue
		}
		if !subnetIDPattern.MatchString(s) {
			invalid++
			continue
		}
		seen[s] = struct{}{}
		kept = append(kept, s)
	}
	// Sorted so a scrape is deterministic, and so the cap keeps the same subset every
	// time rather than an arrival-order one the consumer would see flapping.
	sort.Strings(kept)
	overCap := 0
	if len(kept) > r.maxSubnets {
		overCap = len(kept) - r.maxSubnets
		kept = kept[:r.maxSubnets]
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	newRun := r.pendingSince.IsZero()
	if newRun {
		r.pendingSince = now
		r.droppedThisRun = nil
	}
	// Counted as a per-run high-water mark: the delta is added only when this call refused
	// more than any earlier call in the run did. Counting every call would measure
	// kubelet's retry rate instead; counting only the first call would miss the case this
	// counter exists for, a run whose subnet list grows past the cap while it is published.
	r.countDroppedLocked(dropReasonCap, overCap)
	r.countDroppedLocked(dropReasonInvalidSubnet, invalid)
	r.lastFailure = now
	r.holdDown = holdDown
	r.subnets = kept
	return newRun
}

// countDroppedLocked adds to Dropped only what this run has not already counted. The
// caller must hold r.mu.
func (r *Registry) countDroppedLocked(reason string, dropped int) {
	if dropped <= r.droppedThisRun[reason] {
		return
	}
	Dropped.WithLabelValues(reason).Add(float64(dropped - r.droppedThisRun[reason]))
	if r.droppedThisRun == nil {
		r.droppedThisRun = make(map[string]int, 2)
	}
	r.droppedThisRun[reason] = dropped
}

// ClearPodIPAllocationFailure ends the current run, so the series stops being reported at
// the next scrape. It is for the case where a failure is known not to be attributable to
// the subnets, so an earlier run does not keep being reported on the node's behalf.
//
// It is deliberately NOT called on a successful pod ADD. The run is per node but the
// evidence is per pod, so one unrelated pod getting an address says nothing about the pod
// that cannot - and clearing on it would let ordinary churn, or a multi-network-card pod
// whose first card succeeds, reset the run on every retry and make the hold-down
// unreachable. Recovery is instead detected by failures ceasing to arrive.
func (r *Registry) ClearPodIPAllocationFailure() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pendingSince = time.Time{}
	r.lastFailure = time.Time{}
	r.subnets = nil
	r.droppedThisRun = nil
}

// Describe implements prometheus.Collector.
func (r *Registry) Describe(ch chan<- *prometheus.Desc) {
	ch <- problemDesc
}

// Collect implements prometheus.Collector. Logging happens here rather than in snapshot so
// that the lock is never held across a write to the log file, which would make a pod ADD
// wait on disk I/O. Nothing is logged on the healthy path, which is every scrape forever.
//
// Note that collecting WRITES: a run whose failures have stopped arriving is ended here
// rather than by a timer, because "has it stopped" can only be answered against the time
// the question is asked. Scrape traffic therefore drives the reset, and on a node nothing
// ever scrapes a stale run simply sits there - harmlessly, since nothing reads it either.
// Ending it here rather than merely hiding it is what lets the next real outage start a
// fresh run, and log one line when it does.
func (r *Registry) Collect(ch chan<- prometheus.Metric) {
	metrics, note := r.snapshot()
	if note != "" {
		log.Debugf("eks_problem: %s", note)
	}
	for _, m := range metrics {
		ch <- m
	}
}

// snapshot builds the metrics under the lock and returns them, so that Collect never holds
// the lock while sending on a channel it does not control. The second return value is a
// one-line account of the decision, empty when there is nothing to say.
func (r *Registry) snapshot() ([]prometheus.Metric, string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.pendingSince.IsZero() || len(r.subnets) == 0 {
		return nil, ""
	}
	now := r.now()

	// The failures have stopped arriving, so the problem is over: either the addresses
	// came back, or the pending pods were deleted. Either way nothing is wrong now.
	if now.Sub(r.lastFailure) > stillFailingWindow(r.holdDown) {
		note := fmt.Sprintf("failures stopped %s ago (window %s), clearing",
			now.Sub(r.lastFailure), stillFailingWindow(r.holdDown))
		r.pendingSince = time.Time{}
		r.lastFailure = time.Time{}
		r.subnets = nil
		return nil, note
	}

	// Still inside the hold-down, so this could be ordinary churn clearing by itself.
	if now.Sub(r.pendingSince) < r.holdDown {
		return nil, fmt.Sprintf("run is %s old, inside the %s hold-down",
			now.Sub(r.pendingSince), r.holdDown)
	}

	out := make([]prometheus.Metric, 0, len(r.subnets))
	for _, id := range r.subnets {
		out = append(out, prometheus.MustNewConstMetric(
			problemDesc, prometheus.GaugeValue, 1,
			TypePodIPAllocationFailed, ReasonInsufficientFreeAddresses, id,
		))
	}
	return out, fmt.Sprintf("publishing %d subnet(s); run %s old, last failure %s ago",
		len(r.subnets), now.Sub(r.pendingSince), now.Sub(r.lastFailure))
}

// stillFailingWindow is how long the problem stays current after the last failure. It is
// deliberately HALF the hold-down and so always strictly shorter than it. That ordering is
// what guarantees a single isolated failure can never be published: by the time the
// persistence gate would be satisfied, a lone failure has already gone stale. Reverse the
// two and there is a window in which one momentary shortfall on an idle node is reported.
//
// Being shorter costs nothing in detection: during a real outage kubelet retries about once
// a second, so failures keep arriving far faster than this. It also means a recovery clears
// within half the hold-down, well inside one scrape interval.
func stillFailingWindow(holdDown time.Duration) time.Duration {
	if half := holdDown / 2; half > minStillFailing {
		return half
	}
	return minStillFailing
}

// pending reports the current run's start time and subnets, for tests in this package.
// Tests elsewhere assert on the published series instead, through their own Registry.
func (r *Registry) pending() (time.Time, []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.pendingSince, r.subnets
}
