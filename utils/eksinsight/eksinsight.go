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

// Package eksinsight publishes the problems ipamd detects as the eks_insight gauge on
// ipamd's metrics endpoint:
//
//	eks_insight{type="PodIPAllocationFailed",reason="InsufficientSubnetAddressCapacity",resource_subnet_id="subnet-0abc..."} 1
//
// A series is present while the problem is active and absent once it clears.
package eksinsight

import (
	"regexp"
	"sort"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// MetricName is the exported metric name and is part of this package's public contract.
const MetricName = "eks_insight"

const (
	labelType     = "type"
	labelReason   = "reason"
	labelSubnetID = "resource_subnet_id"
)

// The type and reason label values. Both are part of the public contract, so renaming
// either is a breaking change.
const (
	TypePodIPAllocationFailed = "PodIPAllocationFailed"
	// ReasonInsufficientSubnetAddressCapacity covers both EC2 refusals: no free addresses in
	// non-PD mode, and no allocatable prefix in PD mode. A subnet can have free addresses and
	// still have no free prefix, so the reason does not claim the subnet is empty.
	ReasonInsufficientSubnetAddressCapacity = "InsufficientSubnetAddressCapacity"
)

const (
	// holdDownMultiple is applied to IP_COOLDOWN_PERIOD to get the hold-down.
	holdDownMultiple = 2

	// minHoldDown and maxHoldDown clamp the hold-down. IP_COOLDOWN_PERIOD=0 is legal, so a
	// floor is needed, and the ceiling keeps a real problem from going unreported for long.
	minHoldDown = 60 * time.Second
	maxHoldDown = 5 * time.Minute

	// defaultMaxSubnets caps how many subnets one node reports, to bound the number of
	// series this metric can add to the endpoint.
	defaultMaxSubnets = 20
)

const (
	dropReasonCap           = "cap"
	dropReasonInvalidSubnet = "invalid_subnet"
)

var (
	// subnetIDPattern guards the one label value that is not a constant.
	subnetIDPattern = regexp.MustCompile(`^subnet-[0-9a-f]{8,17}$`)

	insightDesc = prometheus.NewDesc(
		MetricName,
		"Active problem reported by this component. Present while active.",
		[]string{labelType, labelReason, labelSubnetID},
		nil,
	)

	// Dropped counts the observations this package refused to report, by reason.
	Dropped = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "awscni_eks_insight_dropped_total",
			Help: "The number of eks_insight observations dropped, by reason",
		},
		[]string{"reason"},
	)

	// Subnets is the process-wide registry. The pod ADD path records into it and the metrics
	// endpoint collects from it.
	Subnets = NewRegistry(defaultMaxSubnets, time.Now)
)

// subnetState is when a subnet's problem was first and most recently seen.
type subnetState struct {
	firstSeen time.Time
	lastSeen  time.Time
}

// Registry holds the subnets that cannot currently supply a pod address and renders them as
// the gauge. It is safe for concurrent use: CNI ADD handlers and the metrics endpoint touch
// it from different goroutines.
type Registry struct {
	mu         sync.Mutex
	maxSubnets int
	now        func() time.Time

	// holdDown is how long a subnet's problem must persist before it is reported.
	holdDown time.Duration
	// subnets holds one entry per subnet, each expiring on its own last-seen time.
	subnets map[string]subnetState

	// countedInvalid and countedOverCap are what Dropped has already counted, so repeated
	// reports of the same thing do not inflate it.
	countedInvalid map[string]struct{}
	countedOverCap int
}

// NewRegistry returns a Registry capped at maxSubnets, reading the clock through now. A nil
// now means time.Now.
func NewRegistry(maxSubnets int, now func() time.Time) *Registry {
	if now == nil {
		now = time.Now
	}
	return &Registry{
		maxSubnets:     maxSubnets,
		now:            now,
		subnets:        map[string]subnetState{},
		countedInvalid: map[string]struct{}{},
	}
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

// stillFailingWindow is how long a subnet stays current after it was last reported. Half the
// hold-down, so always at least 30s, which spans more than two kubelet sandbox retries. It
// must stay strictly shorter than the hold-down, or a single isolated failure becomes
// publishable.
func stillFailingWindow(holdDown time.Duration) time.Duration {
	return holdDown / 2
}

// Record notes that the given subnets cannot supply a pod address. A subnet already recorded
// has its last-seen time refreshed, and one absent from this call is left to expire on its
// own. holdDown comes from IP_COOLDOWN_PERIOD via HoldDownFor.
func (r *Registry) Record(subnets []string, holdDown time.Duration) {
	kept, invalid := validate(subnets)

	r.mu.Lock()
	defer r.mu.Unlock()

	r.holdDown = holdDown
	now := r.now()
	window := stillFailingWindow(holdDown)

	for _, subnet := range kept {
		state, known := r.subnets[subnet]
		// A gap longer than the window means the earlier failures stopped, so this subnet
		// starts its hold-down again.
		if !known || now.Sub(state.lastSeen) > window {
			state.firstSeen = now
		}
		state.lastSeen = now
		r.subnets[subnet] = state
	}

	for _, subnet := range invalid {
		if _, counted := r.countedInvalid[subnet]; counted {
			continue
		}
		// Counting a value we cannot remember would count it again on every retry, so the
		// counter stops at maxSubnets distinct values rather than inflating.
		if len(r.countedInvalid) >= r.maxSubnets {
			continue
		}
		r.countedInvalid[subnet] = struct{}{}
		Dropped.WithLabelValues(dropReasonInvalidSubnet).Inc()
	}
}

// Reset drops all recorded state. For tests.
func (r *Registry) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.subnets = map[string]subnetState{}
	r.countedInvalid = map[string]struct{}{}
	r.countedOverCap = 0
}

// Describe implements prometheus.Collector.
func (r *Registry) Describe(ch chan<- *prometheus.Desc) {
	ch <- insightDesc
}

// Collect implements prometheus.Collector.
func (r *Registry) Collect(ch chan<- prometheus.Metric) {
	for _, metric := range r.snapshot() {
		ch <- metric
	}
}

// snapshot forgets expired subnets, then returns one metric per subnet whose problem has
// lasted the hold-down. The metrics are built under the lock so Collect never holds the lock
// while sending on a channel it does not own.
func (r *Registry) snapshot() []prometheus.Metric {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now()
	window := stillFailingWindow(r.holdDown)

	publishable := make([]string, 0, len(r.subnets))
	for subnet, state := range r.subnets {
		if now.Sub(state.lastSeen) > window {
			delete(r.subnets, subnet)
			continue
		}
		if now.Sub(state.firstSeen) >= r.holdDown {
			publishable = append(publishable, subnet)
		}
	}

	// Sorted so successive scrapes agree and the cap keeps the same subset each time.
	sort.Strings(publishable)
	if over := len(publishable) - r.maxSubnets; over > 0 {
		publishable = publishable[:r.maxSubnets]
		if over > r.countedOverCap {
			Dropped.WithLabelValues(dropReasonCap).Add(float64(over - r.countedOverCap))
			r.countedOverCap = over
		}
	}

	metrics := make([]prometheus.Metric, 0, len(publishable))
	for _, subnet := range publishable {
		metrics = append(metrics, prometheus.MustNewConstMetric(
			insightDesc, prometheus.GaugeValue, 1,
			TypePodIPAllocationFailed, ReasonInsufficientSubnetAddressCapacity, subnet,
		))
	}
	return metrics
}

// validate splits subnets into those that may become a label and those that may not.
func validate(subnets []string) (kept, invalid []string) {
	seen := make(map[string]struct{}, len(subnets))
	for _, subnet := range subnets {
		if _, dup := seen[subnet]; dup {
			continue
		}
		seen[subnet] = struct{}{}
		if subnetIDPattern.MatchString(subnet) {
			kept = append(kept, subnet)
		} else {
			invalid = append(invalid, subnet)
		}
	}
	return kept, invalid
}
