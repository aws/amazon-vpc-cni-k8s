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
	"context"
	"fmt"
	"net"
	"sort"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/apis/crd/v1alpha1"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/ipamd/datastore"
	"github.com/aws/amazon-vpc-cni-k8s/utils/eksproblem"
)

// These cover the integration points the eksproblem unit suite cannot see: the handler hook
// that decides whether to record and which subnets to name, and the capacity check that
// decides whether the subnets can be at fault at all. Two review rounds found bugs in them.
//
// Every assertion here is on the series a scrape would see, through the real Collector, so
// deleting the call site or the label makes these fail.

// testClock is a hand-wound clock, so that a test can serve a 60-second hold-down without
// sleeping for one.
type testClock struct{ t time.Time }

func (c *testClock) now() time.Time          { return c.t }
func (c *testClock) advance(d time.Duration) { c.t = c.t.Add(d) }

// newProblemRegistry gives ctx its own problem registry on a hand-wound clock, so tests
// neither share process-wide state nor wait out a real hold-down.
func newProblemRegistry(ctx *IPAMContext) *testClock {
	clk := &testClock{t: time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)}
	ctx.eksProblems = eksproblem.NewRegistry(20, clk.now)
	return clk
}

// keepFailing drives the hook the way kubelet does - retrying a stuck pod over and over -
// until the hold-down has been served, so that whatever was recorded is now published.
func keepFailing(s *server, clk *testClock, starvedCards ...int) {
	for i := 0; i < 5; i++ {
		s.recordPodIPAllocationFailure(context.Background(), starvedCards)
		clk.advance(20 * time.Second)
	}
	s.recordPodIPAllocationFailure(context.Background(), starvedCards)
}

// publishedSubnets scrapes the registry and returns the resource_subnet_id of every
// eks_problem series, sorted.
func publishedSubnets(t *testing.T, ctx *IPAMContext) []string {
	t.Helper()
	reg := prometheus.NewPedanticRegistry()
	require.NoError(t, reg.Register(ctx.eksProblemRegistry()))
	families, err := reg.Gather()
	require.NoError(t, err)

	var subnets []string
	for _, family := range families {
		if family.GetName() != eksproblem.MetricName {
			continue
		}
		for _, metric := range family.GetMetric() {
			assert.Equal(t, float64(1), metric.GetGauge().GetValue(),
				"the contract fixes the value at 1")
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

func cidr32(t *testing.T, s string) net.IPNet {
	t.Helper()
	_, n, err := net.ParseCIDR(s)
	require.NoError(t, err)
	return *n
}

// dsWith builds a datastore for one network card holding one ENI in subnet, with n /32s.
func dsWith(t *testing.T, networkCard int, subnet string, isPrimary bool, n int) *datastore.DataStore {
	t.Helper()
	ds := datastore.NewDataStore(log, datastore.NullCheckpoint{}, false, networkCard)
	require.NoError(t, ds.AddENI("eni-"+itoa(networkCard), networkCard+1, isPrimary, false, false, 0, subnet))
	for i := 0; i < n; i++ {
		require.NoError(t, ds.AddIPv4CidrToStore("eni-"+itoa(networkCard),
			cidr32(t, "10."+itoa(networkCard)+".0."+itoa(i+1)+"/32"), false))
	}
	return ds
}

func itoa(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return string(rune('0'+i/10)) + string(rune('0'+i%10))
}

func TestRecordPodIPAllocationFailureRecordsTheSubnet(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()
	m.awsutils.EXPECT().IsTrunkingCompatible().Return(false).AnyTimes()

	ctx := &IPAMContext{
		awsClient:       m.awsutils,
		maxIPsPerENI:    9,
		maxENI:          3,
		unmanagedENI:    []int{0},
		dataStoreAccess: &datastore.DataStoreAccess{DataStores: []*datastore.DataStore{dsWith(t, 0, "subnet-0abcc2fe41543d2ea", false, 2)}},
	}
	clk := newProblemRegistry(ctx)
	s := &server{ipamContext: ctx}

	keepFailing(s, clk, 0)

	assert.Equal(t, []string{"subnet-0abcc2fe41543d2ea"}, publishedSubnets(t, ctx),
		"the failing allocation's subnet must be reported")
}

// A multi-NIC pod needs an address from every network card, so it fails if one card is
// short while the others have addresses to spare. Naming all of them sent the customer to
// widen a subnet that was not the problem.
func TestOnlyTheStarvedNetworkCardsSubnetIsReported(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()
	m.awsutils.EXPECT().IsTrunkingCompatible().Return(false).AnyTimes()

	ctx := &IPAMContext{
		awsClient:    m.awsutils,
		maxIPsPerENI: 9,
		maxENI:       3,
		unmanagedENI: []int{0, 0},
		dataStoreAccess: &datastore.DataStoreAccess{DataStores: []*datastore.DataStore{
			dsWith(t, 0, "subnet-0aaa000000000001", false, 5), // healthy
			dsWith(t, 1, "subnet-0bbb000000000002", false, 0), // out of addresses
		}},
	}
	clk := newProblemRegistry(ctx)
	s := &server{ipamContext: ctx}

	keepFailing(s, clk, 1) // only network card 1 ran out

	assert.Equal(t, []string{"subnet-0bbb000000000002"}, publishedSubnets(t, ctx),
		"the healthy card's subnet must not be named")
}

// dsFullAtEniLimit builds a datastore for a card that cannot grow: maxENI ENIs, each
// holding its full maxIPsPerENI addresses.
func dsFullAtEniLimit(t *testing.T, networkCard, maxENI, perENI int, subnet string) *datastore.DataStore {
	t.Helper()
	ds := datastore.NewDataStore(log, datastore.NullCheckpoint{}, false, networkCard)
	for e := 0; e < maxENI; e++ {
		id := fmt.Sprintf("eni-%d-%d", networkCard, e)
		require.NoError(t, ds.AddENI(id, e+1, false, false, false, 0, subnet))
		for i := 0; i < perENI; i++ {
			require.NoError(t, ds.AddIPv4CidrToStore(id,
				cidr32(t, fmt.Sprintf("10.%d.%d.%d/32", networkCard, e, i+1)), false))
		}
	}
	return ds
}

// A plain pod needs one address and the handler walks every network card looking for it, so
// several cards can starve on one pod ADD. A card that is itself at its ENI/address limit
// must not contribute its subnet: that subnet has addresses to spare and could not have
// helped the card anyway. Judging capacity across the node while taking subnets per card
// published the maxed-out card's healthy subnet next to the genuinely exhausted one.
func TestAMaxedOutCardDoesNotDonateItsHealthySubnet(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()
	m.awsutils.EXPECT().IsTrunkingCompatible().Return(false).AnyTimes()

	ctx := &IPAMContext{
		awsClient:    m.awsutils,
		maxIPsPerENI: 9,
		maxENI:       3,
		unmanagedENI: []int{0, 0},
		dataStoreAccess: &datastore.DataStoreAccess{DataStores: []*datastore.DataStore{
			// card 0: 3 of 3 ENIs, all full - cannot grow, so its subnet is not at fault
			dsFullAtEniLimit(t, 0, 3, 9, "subnet-0aaa000000000001"),
			// card 1: room to grow, but its subnet cannot supply
			dsWith(t, 1, "subnet-0bbb000000000002", false, 0),
		}},
	}
	clk := newProblemRegistry(ctx)
	s := &server{ipamContext: ctx}

	keepFailing(s, clk, 0, 1) // both cards starved on the same pod ADD

	assert.Equal(t, []string{"subnet-0bbb000000000002"}, publishedSubnets(t, ctx),
		"only the card that could still have grown names its subnet")
}

// Ending a run is a node-wide act, so it must need a node-wide verdict. One card being
// over-committed says nothing about a run another card established, and clearing on it
// would resolve a real problem and then re-raise it a hold-down later.
func TestAnOverCommittedCardDoesNotClearAnotherCardsRun(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()
	m.awsutils.EXPECT().IsTrunkingCompatible().Return(false).AnyTimes()

	ctx := &IPAMContext{
		awsClient:    m.awsutils,
		maxIPsPerENI: 9,
		maxENI:       3,
		unmanagedENI: []int{0, 0},
		dataStoreAccess: &datastore.DataStoreAccess{DataStores: []*datastore.DataStore{
			dsFullAtEniLimit(t, 0, 3, 9, "subnet-0aaa000000000001"), // at capacity
			dsWith(t, 1, "subnet-0bbb000000000002", false, 0),       // exhausted subnet
		}},
	}
	clk := newProblemRegistry(ctx)
	s := &server{ipamContext: ctx}

	keepFailing(s, clk, 1)
	require.Equal(t, []string{"subnet-0bbb000000000002"}, publishedSubnets(t, ctx))

	// A pod that starves on the over-committed card arrives while card 1 is still broken.
	s.recordPodIPAllocationFailure(context.Background(), []int{0})

	assert.Equal(t, []string{"subnet-0bbb000000000002"}, publishedSubnets(t, ctx),
		"card 1's run is still correct and must survive a card 0 failure")
}

// The converse: when the whole node is at capacity, an outstanding run must end rather than
// keep a subnet published that cannot be the cause.
func TestARunEndsOnceTheWholeNodeIsAtCapacity(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()
	m.awsutils.EXPECT().IsTrunkingCompatible().Return(false).AnyTimes()

	ctx := &IPAMContext{
		awsClient:       m.awsutils,
		maxIPsPerENI:    9,
		maxENI:          3,
		unmanagedENI:    []int{0},
		dataStoreAccess: &datastore.DataStoreAccess{DataStores: []*datastore.DataStore{dsWith(t, 0, "subnet-0abcc2fe41543d2ea", false, 0)}},
	}
	clk := newProblemRegistry(ctx)
	s := &server{ipamContext: ctx}

	keepFailing(s, clk, 0)
	require.Len(t, publishedSubnets(t, ctx), 1)

	// The node can no longer grow at all, so nothing it reports can be the subnet's fault.
	ctx.disableENIProvisioning = true
	s.recordPodIPAllocationFailure(context.Background(), []int{0})

	assert.Empty(t, publishedSubnets(t, ctx), "the run must end, not keep being published")
}

// With ENI provisioning disabled the pool never grows, so every pod fails for good once it
// is used up - with healthy subnets. Reporting it would be an unclearable false alarm.
func TestNothingIsReportedWhenENIProvisioningIsDisabled(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()
	m.awsutils.EXPECT().IsTrunkingCompatible().Return(false).AnyTimes()

	ctx := &IPAMContext{
		awsClient:              m.awsutils,
		maxIPsPerENI:           9,
		maxENI:                 3,
		unmanagedENI:           []int{0},
		disableENIProvisioning: true,
		dataStoreAccess:        &datastore.DataStoreAccess{DataStores: []*datastore.DataStore{dsWith(t, 0, "subnet-0abcc2fe41543d2ea", false, 0)}},
	}
	clk := newProblemRegistry(ctx)
	s := &server{ipamContext: ctx}

	keepFailing(s, clk, 0)

	assert.Empty(t, publishedSubnets(t, ctx),
		"the node cannot grow by design, so the subnet is not at fault")
}

// Under custom networking, pod addresses come from the ENIConfig subnet, which may hold no
// ENI yet - and that is exactly the subnet at fault. The node's own subnet, on the primary
// ENI, is not where addresses are drawn from, which is why the growth path skips it too.
// Reporting it named a healthy resource; this was observed on a live cluster.
func TestCustomNetworkingNamesTheENIConfigSubnetOnly(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()
	m.awsutils.EXPECT().IsTrunkingCompatible().Return(false).AnyTimes()

	t.Setenv("MY_NODE_NAME", "test-node")
	bg := context.Background()
	require.NoError(t, m.k8sClient.Create(bg, &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "test-node"},
	}))
	eniConfig := &v1alpha1.ENIConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "default"},
		Spec:       v1alpha1.ENIConfigSpec{Subnet: "subnet-0bbb000000000002"},
	}
	require.NoError(t, m.k8sClient.Create(bg, eniConfig))

	ctx := &IPAMContext{
		awsClient:           m.awsutils,
		k8sClient:           m.k8sClient,
		maxIPsPerENI:        9,
		maxENI:              3,
		unmanagedENI:        []int{0},
		useCustomNetworking: true,
		// The primary ENI sits in the node's own subnet, which custom networking does not
		// draw pod addresses from.
		dataStoreAccess: &datastore.DataStoreAccess{DataStores: []*datastore.DataStore{dsWith(t, 0, "subnet-0aaa000000000001", true, 0)}},
	}
	clk := newProblemRegistry(ctx)
	s := &server{ipamContext: ctx}

	keepFailing(s, clk, 0)

	assert.Equal(t, []string{"subnet-0bbb000000000002"}, publishedSubnets(t, ctx),
		"only the ENIConfig subnet supplies pod addresses here")

	// Pointing the ENIConfig at a fresh subnet is the usual remedy. The reported subnet must
	// follow the live config rather than whatever ipamd saw when it last attached an ENI,
	// which on a node that has stopped attaching ENIs would be stale indefinitely.
	eniConfig.Spec.Subnet = "subnet-0ccc000000000003"
	require.NoError(t, m.k8sClient.Update(bg, eniConfig))

	keepFailing(s, clk, 0)

	assert.Equal(t, []string{"subnet-0ccc000000000003"}, publishedSubnets(t, ctx),
		"an edited ENIConfig must be picked up without waiting for an ENI attach")
}

// atAddressCapacity decides whether the subnets can be at fault at all. Its prefix-
// delegation bug - passing maxIPsPerENI where the growth path passes maxPrefixesPerENI -
// made it inert on every PD node and was invisible to the suite.
func TestAtAddressCapacity(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()
	m.awsutils.EXPECT().IsTrunkingCompatible().Return(false).AnyTimes()

	newCtx := func(pd bool, eniCount, maxENI int, cidrsPerENI int) *IPAMContext {
		ds := datastore.NewDataStore(log, datastore.NullCheckpoint{}, pd, 0)
		for e := 0; e < eniCount; e++ {
			id := "eni-" + itoa(e)
			require.NoError(t, ds.AddENI(id, e+1, false, false, false, 0, "subnet-0ccc000000000003"))
			for i := 0; i < cidrsPerENI; i++ {
				if pd {
					require.NoError(t, ds.AddIPv4CidrToStore(id, cidr28(t, i), true))
				} else {
					require.NoError(t, ds.AddIPv4CidrToStore(id, cidr32(t, "10.0."+itoa(e)+"."+itoa(i+1)+"/32"), false))
				}
			}
		}
		return &IPAMContext{
			awsClient:              m.awsutils,
			enablePrefixDelegation: pd,
			maxIPsPerENI:           9,
			maxPrefixesPerENI:      1,
			maxENI:                 maxENI,
			unmanagedENI:           []int{0},
			dataStoreAccess:        &datastore.DataStoreAccess{DataStores: []*datastore.DataStore{ds}},
		}
	}

	t.Run("an ENI with room means not at capacity", func(t *testing.T) {
		// 1 ENI holding 1 of its 9 possible addresses: room to add more.
		assert.False(t, newCtx(false, 1, 3, 1).atAddressCapacity(0))
	})

	t.Run("ENIs full but a slot free means not at capacity", func(t *testing.T) {
		// 1 full ENI of a possible 3: ipamd can still attach another.
		assert.False(t, newCtx(false, 1, 3, 9).atAddressCapacity(0))
	})

	t.Run("ENIs full and no slot free means AT capacity", func(t *testing.T) {
		assert.True(t, newCtx(false, 3, 3, 9).atAddressCapacity(0),
			"this is max-pods over-commit, not a subnet problem")
	})

	// The bug: in PD mode the per-ENI limit is counted in prefixes, not addresses. Passing
	// maxIPsPerENI (16x larger) made every ENI look like it had room, so the guard never
	// fired and a healthy subnet was published for the life of the node.
	t.Run("prefix delegation uses the PREFIX limit, so a full node is AT capacity", func(t *testing.T) {
		assert.True(t, newCtx(true, 3, 3, 1).atAddressCapacity(0),
			"maxPrefixesPerENI=1 and 1 prefix attached means this ENI is full")
	})

	t.Run("ENI provisioning disabled means AT capacity whatever the pool looks like", func(t *testing.T) {
		ctx := newCtx(false, 1, 3, 1) // plenty of room, but it will never be used
		ctx.disableENIProvisioning = true
		assert.True(t, ctx.atAddressCapacity(0))
	})

	// On an IPv6 node the check is deliberately inert: a pod subnet is a /64 and one /80
	// prefix holds about 10^14 addresses, so an IPv6 pod ADD fails only when no prefix
	// could be attached at all - which is a subnet problem, and should be reported.
	t.Run("IPv6 is never treated as at capacity", func(t *testing.T) {
		ctx := newCtx(false, 3, 3, 9) // would be at capacity on IPv4
		ctx.enableIPv6 = true
		assert.False(t, ctx.atAddressCapacity(0))
	})

	// The two flags answer different questions and must not shadow each other: "will this
	// node ever grow" is a property of the configuration and holds in either address
	// family, while the IPv6 branch is only an argument about how big a prefix is. An IPv6
	// node with provisioning disabled - which IMDS-only mode turns on - would otherwise
	// blame a healthy subnet forever, with no way to clear it.
	t.Run("ENI provisioning disabled beats the IPv6 carve-out", func(t *testing.T) {
		ctx := newCtx(false, 1, 3, 1) // plenty of room, but it will never be used
		ctx.enableIPv6 = true
		ctx.disableENIProvisioning = true
		assert.True(t, ctx.atAddressCapacity(0))
	})

	t.Run("at capacity on every card requires every card", func(t *testing.T) {
		ctx := newCtx(false, 3, 3, 9)
		assert.True(t, ctx.atAddressCapacityOnEveryCard(), "one card, and it is maxed out")

		ctx.dataStoreAccess.DataStores = append(ctx.dataStoreAccess.DataStores,
			dsWith(t, 1, "subnet-0bbb000000000002", false, 0))
		ctx.unmanagedENI = []int{0, 0}
		assert.False(t, ctx.atAddressCapacityOnEveryCard(), "card 1 can still grow")
	})

	t.Run("an unpopulated network card index does not panic and does not suppress", func(t *testing.T) {
		ctx := newCtx(false, 3, 3, 9)
		ctx.unmanagedENI = nil // hasRoomForEni would index out of range
		assert.False(t, ctx.atAddressCapacity(0),
			"must err towards publishing rather than panic on the CNI ADD path")
	})
}

func cidr28(t *testing.T, i int) net.IPNet {
	t.Helper()
	_, n, err := net.ParseCIDR("10.1." + itoa(i) + ".0/28")
	require.NoError(t, err)
	return *n
}
