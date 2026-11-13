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

package ipamd

import (
	"fmt"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/ipamd/datastore"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/networkutils"
)

const osc1451MaxIPsPerENI = 14

// osc1451Context builds the configuration from upstream issue #1451: custom networking, so the
// primary ENI contributes no pod addresses, WARM_ENI_TARGET as given, and an idle node holding
// `secondaryENIs` secondary ENIs at full address capacity.
func osc1451Context(t *testing.T, m *testMocks, warmENITarget, secondaryENIs int) (*IPAMContext, *datastore.DataStore) {
	t.Helper()
	dsa := testDatastore()
	ds := dsa.GetDataStore(defaultNetworkCard)
	assert.NoError(t, ds.AddENI(primaryENIid, 0, true, false, false, networkutils.CalculateRouteTableId(0, 0), ""))
	for e := 1; e <= secondaryENIs; e++ {
		id := fmt.Sprintf("eni-sec-%d", e)
		assert.NoError(t, ds.AddENI(id, e, false, false, false, networkutils.CalculateRouteTableId(e, 0), ""))
		for i := 1; i <= osc1451MaxIPsPerENI; i++ {
			assert.NoError(t, ds.AddIPv4CidrToStore(id, net.IPNet{
				IP: net.ParseIP(fmt.Sprintf("10.%d.0.%d", e, i)), Mask: net.CIDRMask(32, 32),
			}, false))
		}
	}
	return &IPAMContext{
		awsClient:           m.awsutils,
		networkClient:       m.network,
		primaryIP:           make(map[string]string),
		dataStoreAccess:     dsa,
		useCustomNetworking: true,
		maxIPsPerENI:        osc1451MaxIPsPerENI,
		maxENI:              4,
		warmENITarget:       warmENITarget,
		maxPods:             110,
		numNetworkCards:     1,
		unmanagedENI:        []int{0},
	}, ds
}

// TestOscillation1451GrowAndShrinkWindowsTouchAtZeroWarmENITarget locates upstream issue #1451 in
// the decision functions, and records that neither the warm-target sentinel work nor the cooldown
// work in this change fixes it.
//
// At WARM_ENI_TARGET=0 the grow rule and the shrink rule are satisfied at adjacent pool sizes with
// no gap between them:
//
//	grow:   isDatastorePoolTooLow      available == 0                            ipamd.go:2722
//	shrink: shouldRemoveExtraENIs      available >= (WARM_ENI_TARGET+1) * 14     ipamd.go:1561
//
// A node with one full secondary ENI sits at available == 14, which satisfies the shrink rule. Free
// that ENI and available is 0, which satisfies the grow rule. At WARM_ENI_TARGET=1 the shrink rule
// needs 28, so one ENI is stable and there is a gap.
func TestOscillation1451GrowAndShrinkWindowsTouchAtZeroWarmENITarget(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	// WARM_ENI_TARGET=0, one full secondary ENI. This is the state the loop runs in.
	c, ds := osc1451Context(t, m, 0, 1)
	stats := ds.GetIPStats(ipV4AddrFamily)
	assert.Equal(t, 14, stats.AvailableAddresses())
	assert.False(t, c.isDatastorePoolTooLow()[defaultNetworkCard].IsLow, "14 available is not too low")
	assert.False(t, c.shouldRemoveExtraENIs(stats, defaultNetworkCard),
		"the last interface's worth of addresses must not be offered for removal, or the next tick re-attaches it")

	// The loop would close here: once that ENI is gone the pool is too low again.
	assert.NoError(t, ds.RemoveENIFromDataStore("eni-sec-1", true))
	assert.True(t, c.isDatastorePoolTooLow()[defaultNetworkCard].IsLow,
		"0 available is too low, which is the other half of the loop")

	// Two full secondary ENIs at a zero target: one of them is genuinely spare, so removal is
	// still offered. The fix holds the last interface's worth, not every interface.
	c, ds = osc1451Context(t, m, 0, 2)
	stats = ds.GetIPStats(ipV4AddrFamily)
	assert.Equal(t, 28, stats.AvailableAddresses())
	assert.True(t, c.shouldRemoveExtraENIs(stats, defaultNetworkCard),
		"28 available is more than the last interface's worth, so an ENI is spare")

	// WARM_ENI_TARGET=1, the default. The shrink rule needs two ENIs' worth, so one is stable, and
	// the new condition is gated off entirely because the target is not zero.
	c, ds = osc1451Context(t, m, 1, 1)
	stats = ds.GetIPStats(ipV4AddrFamily)
	assert.False(t, c.isDatastorePoolTooLow()[defaultNetworkCard].IsLow)
	assert.False(t, c.shouldRemoveExtraENIs(stats, defaultNetworkCard),
		"14 available is below the (1+1)*14 shrink threshold, so there is a gap between the windows")

	// WARM_ENI_TARGET=1 with two ENIs: 28 >= 28, removal offered, exactly as before this change.
	c, ds = osc1451Context(t, m, 1, 2)
	stats = ds.GetIPStats(ipV4AddrFamily)
	assert.True(t, c.shouldRemoveExtraENIs(stats, defaultNetworkCard),
		"the default path is unchanged: at WARM_ENI_TARGET=1 removal is still offered at two ENIs' worth")
}

// TestOscillation1451IsRateLimitedByMinENILifeTime records the one brake that already exists. A
// freshly attached ENI is not deletable for minENILifeTime, so the loop cannot run faster than
// roughly once per minute per node even unpatched. The pre-registered falsification threshold in
// argument.md section 8 F3 is 2 cycles per minute, so F3 does not discriminate: master already
// passes it.
func TestOscillation1451IsRateLimitedByMinENILifeTime(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	c, ds := osc1451Context(t, m, 0, 2)
	stats := ds.GetIPStats(ipV4AddrFamily)
	assert.True(t, c.shouldRemoveExtraENIs(stats, defaultNetworkCard))
	assert.Equal(t, "", ds.RemoveUnusedENIFromStore(c.warmIPTarget, c.minimumIPTarget, c.warmPrefixTarget),
		"an ENI younger than minENILifeTime is never deletable, which caps the loop at about 1 cycle per minute")
}
