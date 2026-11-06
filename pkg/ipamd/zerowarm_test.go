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
	"net"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/networkutils"
)

// TestExplicitZeroWarmIPTargetDoesNotFallThroughToWarmENITarget is the headline defect, issue
// #3585. An operator asking for the smallest warm pool received the largest one: the explicit zero
// was indistinguishable from unset, so the pool manager used WARM_ENI_TARGET=1 and reserved a whole
// spare ENI's worth of addresses.
func TestExplicitZeroWarmIPTargetDoesNotFallThroughToWarmENITarget(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	newCtx := func(warmIPTargetSet bool) *IPAMContext {
		return &IPAMContext{
			awsClient:       m.awsutils,
			networkClient:   m.network,
			primaryIP:       make(map[string]string),
			dataStoreAccess: testDatastore(),
			maxIPsPerENI:    14,
			maxENI:          4,
			warmENITarget:   defaultWarmENITarget,
			warmIPTarget:    0,
			warmIPTargetSet: warmIPTargetSet,
			maxPods:         110,
			numNetworkCards: 1,
		}
	}

	// Unset: rank 2 applies, WARM_ENI_TARGET=1, so the node wants a whole ENI's worth.
	unset := newCtx(false)
	assert.False(t, unset.warmIPTargetsDefined())
	assert.True(t, unset.isDatastorePoolTooLow()[defaultNetworkCard].IsLow)
	assert.Equal(t, 14, unset.GetENIResourcesToAllocate(defaultNetworkCard),
		"with no target set, allocation is unclamped at full ENI capacity")

	// Explicitly zero: rank 1 applies at a target of zero, so the node wants nothing.
	zero := newCtx(true)
	assert.True(t, zero.warmIPTargetsDefined())
	assert.False(t, zero.isDatastorePoolTooLow()[defaultNetworkCard].IsLow)
	assert.Equal(t, 0, zero.GetENIResourcesToAllocate(defaultNetworkCard),
		"asking for zero must not yield a spare ENI's worth of addresses")

	short, over, enabled := zero.datastoreTargetState(nil, defaultNetworkCard)
	assert.True(t, enabled)
	assert.Equal(t, 0, short)
	assert.Equal(t, 0, over)
}

// TestZeroWarmIPTargetWithMinimumHoldsExactlyTheMinimum covers the configuration README.md note 2
// already documents. The pool fills to MINIMUM_IP_TARGET and stops. The change makes that reachable
// by asking for it directly rather than only by omitting WARM_IP_TARGET.
func TestZeroWarmIPTargetWithMinimumHoldsExactlyTheMinimum(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	c := &IPAMContext{
		awsClient:          m.awsutils,
		networkClient:      m.network,
		primaryIP:          make(map[string]string),
		dataStoreAccess:    testDatastore(),
		maxIPsPerENI:       14,
		maxENI:             4,
		warmENITarget:      defaultWarmENITarget,
		warmIPTarget:       0,
		warmIPTargetSet:    true,
		minimumIPTarget:    3,
		minimumIPTargetSet: true,
		maxPods:            110,
		numNetworkCards:    1,
	}

	short, over, enabled := c.datastoreTargetState(nil, defaultNetworkCard)
	assert.True(t, enabled)
	assert.Equal(t, 3, short, "empty pool is short of the minimum")
	assert.Equal(t, 0, over)

	ds := c.dataStoreAccess.GetDataStore(defaultNetworkCard)
	_ = ds.AddENI(primaryENIid, 0, true, false, false, networkutils.CalculateRouteTableId(0, 0), "")
	for _, a := range []string{ipaddr01, ipaddr02, ipaddr03} {
		_ = ds.AddIPv4CidrToStore(primaryENIid, net.IPNet{IP: net.ParseIP(a), Mask: net.IPv4Mask(255, 255, 255, 255)}, false)
	}

	short, over, _ = c.datastoreTargetState(nil, defaultNetworkCard)
	assert.Equal(t, 0, short, "the minimum is met")
	assert.Equal(t, 0, over, "and the minimum floors the release, so nothing is surplus")
	assert.False(t, c.isDatastorePoolTooLow()[defaultNetworkCard].IsLow)
}

// TestWarmPrefixTargetUnsetInPrefixModeIsNotDefined covers the tautology. The old predicate was
// `warmPrefixTarget >= defaultWarmPrefixTarget`, and the default is 0, so it reduced to
// `enablePrefixDelegation` and switched the prefix scale-down regime on with a target of zero.
func TestWarmPrefixTargetUnsetInPrefixModeIsNotDefined(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	newCtx := func(set bool) *IPAMContext {
		c := &IPAMContext{
			awsClient:              m.awsutils,
			networkClient:          m.network,
			primaryIP:              make(map[string]string),
			dataStoreAccess:        testDatastorewithPrefix(),
			enablePrefixDelegation: true,
			maxIPsPerENI:           16,
			maxPrefixesPerENI:      1,
			maxENI:                 4,
			warmPrefixTarget:       0,
			warmPrefixTargetSet:    set,
			maxPods:                110,
			numNetworkCards:        1,
		}
		ds := c.dataStoreAccess.GetDataStore(defaultNetworkCard)
		_ = ds.AddENI(primaryENIid, 0, true, false, false, networkutils.CalculateRouteTableId(0, 0), "")
		_, prefix, _ := net.ParseCIDR("10.0.0.0/28")
		_ = ds.AddIPv4CidrToStore(primaryENIid, *prefix, true)
		return c
	}

	unset := newCtx(false)
	assert.False(t, unset.warmPrefixTargetDefined())
	stats := unset.dataStoreAccess.GetDataStore(defaultNetworkCard).GetIPStats(ipV4AddrFamily)
	assert.False(t, unset.isDatastorePoolTooHigh(stats, defaultNetworkCard),
		"an unset WARM_PREFIX_TARGET must not read as a target of zero and release the launch prefix")
	assert.Equal(t, 0, unset.computeExtraPrefixesOverWarmTarget(defaultNetworkCard))

	// An explicit zero keeps the old behaviour: the operator asked for no spare prefix.
	zero := newCtx(true)
	assert.True(t, zero.warmPrefixTargetDefined())
	stats = zero.dataStoreAccess.GetDataStore(defaultNetworkCard).GetIPStats(ipV4AddrFamily)
	assert.True(t, zero.isDatastorePoolTooHigh(stats, defaultNetworkCard))
	assert.Equal(t, 1, zero.computeExtraPrefixesOverWarmTarget(defaultNetworkCard))
}

// TestDefaultPathUnchangedByWarmTargetSentinel is the regression guard. With no warm env set, every
// decision the pool manager makes must be what it was before the sentinel became tri-state.
func TestDefaultPathUnchangedByWarmTargetSentinel(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	for _, pd := range []bool{false, true} {
		c := &IPAMContext{
			awsClient:              m.awsutils,
			networkClient:          m.network,
			primaryIP:              make(map[string]string),
			enablePrefixDelegation: pd,
			maxIPsPerENI:           14,
			maxPrefixesPerENI:      1,
			maxENI:                 4,
			// Exactly what New() produces with nothing set.
			warmENITarget:       defaultWarmENITarget,
			warmIPTarget:        noWarmIPTarget,
			warmIPTargetSet:     false,
			minimumIPTarget:     noMinimumIPTarget,
			minimumIPTargetSet:  false,
			warmPrefixTarget:    defaultWarmPrefixTarget,
			warmPrefixTargetSet: false,
			maxPods:             110,
			numNetworkCards:     1,
		}
		if pd {
			c.dataStoreAccess = testDatastorewithPrefix()
			c.maxIPsPerENI = 16
		} else {
			c.dataStoreAccess = testDatastore()
		}

		assert.False(t, c.warmIPTargetsDefined(), "pd=%v", pd)
		_, _, enabled := c.datastoreTargetState(nil, defaultNetworkCard)
		assert.False(t, enabled, "rank 1 must stay inactive with nothing set, pd=%v", pd)

		// Rank 2 reads the unmodified AvailableAddresses, so an empty pool is still too low.
		assert.True(t, c.isDatastorePoolTooLow()[defaultNetworkCard].IsLow, "pd=%v", pd)
		expected := 14
		if pd {
			expected = 1
		}
		assert.Equal(t, expected, c.GetENIResourcesToAllocate(defaultNetworkCard), "pd=%v", pd)
	}
}
