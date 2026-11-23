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

package ipamd

import (
	"net"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/aws/smithy-go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/ipamd/datastore"
	"github.com/aws/amazon-vpc-cni-k8s/utils/eksinsight"
)

const (
	subnetA = "subnet-0aaa000000000001"
	subnetB = "subnet-0bbb000000000002"
	subnetC = "subnet-0ccc000000000003"
)

// testClock is a hand-wound clock, so a test can serve a 60 second hold-down without
// sleeping for one.
type testClock struct{ t time.Time }

func (c *testClock) now() time.Time          { return c.t }
func (c *testClock) advance(d time.Duration) { c.t = c.t.Add(d) }

// withInsights gives ctx its own registry and refusal record on a hand-wound clock.
func withInsights(ctx *IPAMContext) *testClock {
	clk := &testClock{t: time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)}
	ctx.eksInsights = eksinsight.NewRegistry(20, clk.now)
	ctx.eksRefusals = eksinsight.NewRefusals(clk.now)
	return clk
}

// refuse records that EC2 could not supply addresses from these subnets while creating an
// ENI, which is a fact about the node rather than one card.
func refuse(ctx *IPAMContext, subnets ...string) {
	err := &smithy.GenericAPIError{Code: "InsufficientFreeAddressesInSubnet"}
	for _, subnet := range subnets {
		ctx.eksRefusals.RefusedIf(err, subnet)
	}
}

// refuseCard records that EC2 could not supply addresses while topping up an ENI on one
// card, which no other card may be blamed for.
func refuseCard(ctx *IPAMContext, card int, subnets ...string) {
	err := &smithy.GenericAPIError{Code: "InsufficientFreeAddressesInSubnet"}
	for _, subnet := range subnets {
		ctx.eksRefusals.RefusedCardIf(err, card, subnet)
	}
}

// keepFailing drives the hook the way kubelet does, until the hold-down has been served.
func keepFailing(s *server, clk *testClock, starvedCards ...int) {
	for i := 0; i < 7; i++ {
		s.recordPodIPAllocationFailure(starvedCards)
		clk.advance(10 * time.Second)
	}
	s.recordPodIPAllocationFailure(starvedCards)
}

// published returns the subnets a scrape would see, sorted.
func published(t *testing.T, ctx *IPAMContext) []string {
	t.Helper()
	reg := prometheus.NewPedanticRegistry()
	require.NoError(t, reg.Register(ctx.eksInsights))
	families, err := reg.Gather()
	require.NoError(t, err)

	var subnets []string
	for _, family := range families {
		if family.GetName() != eksinsight.MetricName {
			continue
		}
		for _, metric := range family.GetMetric() {
			assert.Equal(t, float64(1), metric.GetGauge().GetValue(), "the contract fixes the value at 1")
			for _, label := range metric.GetLabel() {
				if label.GetName() == "resource_subnet_id" {
					subnets = append(subnets, label.GetValue())
				}
			}
		}
	}
	sort.Strings(subnets)
	return subnets
}

func cidr28(t *testing.T, s string) net.IPNet {
	t.Helper()
	_, n, err := net.ParseCIDR(s)
	require.NoError(t, err)
	return *n
}

func cidr32(t *testing.T, s string) net.IPNet {
	t.Helper()
	_, n, err := net.ParseCIDR(s)
	require.NoError(t, err)
	return *n
}

// dsWith builds a datastore for one card holding one secondary ENI in subnet, with n
// addresses.
func dsWith(t *testing.T, networkCard int, subnet string, n int) *datastore.DataStore {
	t.Helper()
	return dsWithENI(t, networkCard, subnet, n, false)
}

// dsWithPrimary builds the same thing with the ENI marked as the node's primary.
func dsWithPrimary(t *testing.T, networkCard int, subnet string, n int) *datastore.DataStore {
	t.Helper()
	return dsWithENI(t, networkCard, subnet, n, true)
}

func dsWithENI(t *testing.T, networkCard int, subnet string, n int, isPrimary bool) *datastore.DataStore {
	t.Helper()
	ds := datastore.NewDataStore(log, datastore.NullCheckpoint{}, false, networkCard)
	eniID := "eni-" + strconv.Itoa(networkCard)
	require.NoError(t, ds.AddENI(eniID, networkCard+1, isPrimary, false, false, 0, subnet))
	for i := 0; i < n; i++ {
		require.NoError(t, ds.AddIPv4CidrToStore(eniID,
			cidr32(t, "10."+strconv.Itoa(networkCard)+".0."+strconv.Itoa(i+1)+"/32"), false))
	}
	return ds
}

// dsWithPrefixes builds a prefix delegation datastore for one card holding one ENI in subnet
// with n prefixes.
func dsWithPrefixes(t *testing.T, networkCard int, subnet string, n int) *datastore.DataStore {
	t.Helper()
	ds := datastore.NewDataStore(log, datastore.NullCheckpoint{}, true, networkCard)
	eniID := "eni-pd-" + strconv.Itoa(networkCard)
	require.NoError(t, ds.AddENI(eniID, networkCard+1, false, false, false, 0, subnet))
	for i := 0; i < n; i++ {
		require.NoError(t, ds.AddIPv4CidrToStore(eniID,
			cidr28(t, "10."+strconv.Itoa(networkCard)+"."+strconv.Itoa(i)+".0/28"), true))
	}
	return ds
}

// dsEmpty builds a datastore for a card with no ENIs at all.
func dsEmpty(t *testing.T, networkCard int) *datastore.DataStore {
	t.Helper()
	return datastore.NewDataStore(log, datastore.NullCheckpoint{}, false, networkCard)
}

func ctxWith(m *testMocks, stores ...*datastore.DataStore) *IPAMContext {
	return &IPAMContext{
		awsClient:    m.awsutils,
		maxIPsPerENI: 9,
		maxENI:       3,
		// IP_COOLDOWN_PERIOD's default, so the hold-down is 60s as in production.
		ipCooldown:      30 * time.Second,
		dataStoreAccess: &datastore.DataStoreAccess{DataStores: stores},
	}
}

// Nothing a pod ADD records can reach /metrics unless the registry the endpoint serves is
// the very object this context writes to. Comparing what each reports would not catch a
// mismatch, because a registry nobody writes to reports nothing either way.
func TestTheRegisteredRegistryIsTheOneWeRecordInto(t *testing.T) {
	var handed *eksinsight.Registry
	original := registerEksInsight
	registerEksInsight = func(r *eksinsight.Registry) { handed = r }
	t.Cleanup(func() { registerEksInsight = original })

	c := &IPAMContext{}
	c.eksInsights = eksinsight.NewRegistry(eksinsight.DefaultMaxSubnets, time.Now)
	prometheusRegister(c.eksInsights)

	assert.Same(t, c.eksInsights, handed)
}

func TestTheRefusedSubnetIsReported(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	ctx := ctxWith(m, dsWith(t, 0, subnetA, 0))
	clk := withInsights(ctx)
	refuse(ctx, subnetA)
	s := &server{ipamContext: ctx}

	keepFailing(s, clk, 0)

	assert.Equal(t, []string{subnetA}, published(t, ctx))
}

// Without a refusal there is no evidence any subnet is at fault, so nothing is reported.
// This covers max-pods over-commit, addresses in cooldown, a cordoned node, throttling and
// a missing IAM permission, none of which leave a refusal behind.
func TestNothingIsReportedWithoutARefusal(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	ctx := ctxWith(m, dsWith(t, 0, subnetA, 0))
	clk := withInsights(ctx)
	s := &server{ipamContext: ctx}

	keepFailing(s, clk, 0)

	assert.Empty(t, published(t, ctx))
}

// A subnet refused while creating an ENI is only named if this node draws addresses from it.
// Otherwise the customer is sent to a subnet the node never used, which may have addresses
// to spare.
func TestARefusedSubnetTheNodeDoesNotUseIsNotReported(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	ctx := ctxWith(m, dsWith(t, 0, subnetA, 0))
	clk := withInsights(ctx)
	refuse(ctx, subnetA, subnetC)
	s := &server{ipamContext: ctx}

	keepFailing(s, clk, 0)

	assert.Equal(t, []string{subnetA}, published(t, ctx), "subnet C holds no ENI on this node")
}

// A full ENI is never asked for addresses, so its subnet has no refusal and is not named.
func TestAFullENIsSubnetIsNotReported(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	ds := dsWith(t, 0, subnetA, 9)
	require.NoError(t, ds.AddENI("eni-roomy", 2, false, false, false, 0, subnetB))
	ctx := ctxWith(m, ds)
	clk := withInsights(ctx)
	refuse(ctx, subnetB)
	s := &server{ipamContext: ctx}

	keepFailing(s, clk, 0)

	assert.Equal(t, []string{subnetB}, published(t, ctx))
}

// A pod needing an address from every card fails if one card is short. The cards that did
// supply an address are not named.
func TestOnlyTheStarvedCardsSubnetIsReported(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	ctx := ctxWith(m, dsWith(t, 0, subnetA, 5), dsWith(t, 1, subnetB, 0))
	clk := withInsights(ctx)
	refuse(ctx, subnetA, subnetB)
	s := &server{ipamContext: ctx}

	keepFailing(s, clk, 1)

	assert.Equal(t, []string{subnetB}, published(t, ctx))
}

// A card whose ENIs are all full could only have grown with a new ENI, so a subnet that
// refused one is named even though no ENI sits in it yet.
func TestAFullCardNamesTheSubnetItCouldNotGrowInto(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	ctx := ctxWith(m, dsWith(t, 0, subnetA, 9))
	clk := withInsights(ctx)
	refuse(ctx, subnetB)
	s := &server{ipamContext: ctx}

	keepFailing(s, clk, 0)

	assert.Equal(t, []string{subnetB}, published(t, ctx),
		"subnet A is full rather than short, so B is the subnet to act on")
}

// In prefix delegation mode room is counted in prefixes, so the prefix limit is what decides
// whether the card could still have grown.
func TestAFullCardInPrefixDelegationModeNamesTheRefusedSubnet(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	ctx := ctxWith(m, dsWithPrefixes(t, 0, subnetA, 1))
	ctx.enablePrefixDelegation = true
	ctx.maxPrefixesPerENI = 1
	clk := withInsights(ctx)
	refuse(ctx, subnetB)
	s := &server{ipamContext: ctx}

	keepFailing(s, clk, 0)

	assert.Equal(t, []string{subnetB}, published(t, ctx),
		"one prefix fills the ENI, so the card could only have grown with a new ENI")
}

// While an ENI can still take more addresses, a pod failure is not evidence that the subnet
// a new ENI would have gone to is at fault.
func TestACardWithRoomNamesNothingItDoesNotUse(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	ctx := ctxWith(m, dsWith(t, 0, subnetA, 5))
	clk := withInsights(ctx)
	refuse(ctx, subnetB)
	s := &server{ipamContext: ctx}

	keepFailing(s, clk, 0)

	assert.Empty(t, published(t, ctx))
}

// Without custom networking the primary ENI serves pod IPs, so its remaining room counts
// and a card holding only a roomy primary has not run out of ways to grow.
func TestARoomyPrimaryENICountsAsRoom(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	ctx := ctxWith(m, dsWithPrimary(t, 0, subnetA, 5))
	clk := withInsights(ctx)
	refuse(ctx, subnetB)
	s := &server{ipamContext: ctx}

	keepFailing(s, clk, 0)

	assert.Empty(t, published(t, ctx))
}

// A refusal earned while topping up one card's ENI must not be pinned on another card. A
// multi-NIC pod takes an address from card 0 and then fails on card 1, so only card 1 is
// starved, and card 1 never asked subnet A for anything.
func TestOneCardsTopUpRefusalIsNotBlamedOnAnother(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	ctx := ctxWith(m, dsWith(t, 0, subnetA, 5), dsWith(t, 1, subnetB, 9))
	clk := withInsights(ctx)
	refuseCard(ctx, 0, subnetA)
	s := &server{ipamContext: ctx}

	keepFailing(s, clk, 1)

	assert.Empty(t, published(t, ctx),
		"card 1 is full with nothing refused of its own, so no subnet is at fault")
}

// A refusal from creating an ENI is a fact about the subnet, so every card may be blamed for
// it. The subnets discovery tries are the same whichever card wanted the interface.
func TestACreationRefusalIsVisibleToEveryCard(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	ctx := ctxWith(m, dsWith(t, 0, subnetA, 5), dsWith(t, 1, subnetB, 9))
	clk := withInsights(ctx)
	refuse(ctx, subnetB)
	s := &server{ipamContext: ctx}

	keepFailing(s, clk, 1)

	assert.Equal(t, []string{subnetB}, published(t, ctx))
}

// One starved card having no evidence must not silence another that does.
func TestACardWithNoEvidenceDoesNotSilenceTheOthers(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	ctx := ctxWith(m, dsWith(t, 0, subnetA, 0), dsWith(t, 1, subnetB, 0))
	clk := withInsights(ctx)
	refuseCard(ctx, 1, subnetB)
	s := &server{ipamContext: ctx}

	keepFailing(s, clk, 0, 1)

	assert.Equal(t, []string{subnetB}, published(t, ctx),
		"card 0 has nothing refused, card 1 does")
}

// The hold-down comes from the cooldown ipamd cached at startup, so a longer cooldown means
// a longer run of failures before anything is published.
func TestTheCachedCooldownDrivesTheHoldDown(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	ctx := ctxWith(m, dsWith(t, 0, subnetA, 0))
	ctx.ipCooldown = 60 * time.Second // so the hold-down is 120s rather than the 60s floor
	clk := withInsights(ctx)
	refuse(ctx, subnetA)
	s := &server{ipamContext: ctx}

	keepFailing(s, clk, 0)
	assert.Empty(t, published(t, ctx), "80s of failures does not serve a 120s hold-down")

	keepFailing(s, clk, 0)
	assert.Equal(t, []string{subnetA}, published(t, ctx))
}

// A card with no ENIs has no subnets to narrow by, so every refused subnet is named. The
// card could not get even its first ENI, which is the most exhausted state.
func TestACardWithNoENIsNamesTheRefusedSubnets(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	ctx := ctxWith(m, dsWith(t, 0, subnetA, 5), dsEmpty(t, 1))
	clk := withInsights(ctx)
	refuse(ctx, subnetB)
	s := &server{ipamContext: ctx}

	keepFailing(s, clk, 1)

	assert.Equal(t, []string{subnetB}, published(t, ctx))
}

// Under custom networking pod addresses come from the ENIConfig subnet, so the node's own
// subnets are not a useful filter.
func TestCustomNetworkingNamesTheRefusedSubnet(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	ctx := ctxWith(m, dsWith(t, 0, subnetA, 0))
	ctx.useCustomNetworking = true
	clk := withInsights(ctx)
	refuse(ctx, subnetB)
	s := &server{ipamContext: ctx}

	keepFailing(s, clk, 0)

	assert.Equal(t, []string{subnetB}, published(t, ctx),
		"the configured subnet is named even though no ENI sits in it")
}

// Supplying addresses from a subnet clears its refusal, so it stops being named.
func TestASuppliedSubnetStopsBeingReported(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	ctx := ctxWith(m, dsWith(t, 0, subnetA, 0))
	clk := withInsights(ctx)
	refuse(ctx, subnetA)
	s := &server{ipamContext: ctx}

	keepFailing(s, clk, 0)
	require.Equal(t, []string{subnetA}, published(t, ctx))

	ctx.eksRefusals.Supplied(subnetA)
	keepFailing(s, clk, 0)

	assert.Empty(t, published(t, ctx))
}
