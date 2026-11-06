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

package datastore

import (
	"net"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/networkutils"
)

func cidr32(addr string) net.IPNet {
	return net.IPNet{IP: net.ParseIP(addr), Mask: net.IPv4Mask(255, 255, 255, 255)}
}

// TestSurplusAddressesExcludesCooldown pins the arithmetic the release side is sized from.
// AvailableAddresses must keep counting a cooling address, because the allocate side relies on
// that; SurplusAddresses must not, because the release side does.
func TestSurplusAddressesExcludesCooldown(t *testing.T) {
	ds := NewDataStore(Testlog, NullCheckpoint{}, false, defaultNetworkCard)
	_ = ds.AddENI("eni-1", 1, true, false, false, networkutils.CalculateRouteTableId(1, 0), "")
	for _, a := range []string{"1.1.1.1", "1.1.1.2", "1.1.1.3"} {
		_ = ds.AddIPv4CidrToStore("eni-1", cidr32(a), false)
	}

	keys := []IPAMKey{{"net0", "sandbox-1", "eth0"}, {"net0", "sandbox-2", "eth0"}}
	for _, k := range keys {
		_, _, _, err := ds.AssignPodIPv4Address(k, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: k.ContainerID})
		assert.NoError(t, err)
	}

	stats := ds.GetIPStats("4")
	assert.Equal(t, 3, stats.TotalIPs)
	assert.Equal(t, 2, stats.AssignedIPs)
	assert.Equal(t, 0, stats.CooldownIPs)
	assert.Equal(t, 1, stats.AvailableAddresses())
	assert.Equal(t, 1, stats.SurplusAddresses())

	// One pod departs. Its address is now unassigned but cooling.
	_, _, _, _, _, err := ds.UnassignPodIPAddress(keys[0])
	assert.NoError(t, err)

	stats = ds.GetIPStats("4")
	assert.Equal(t, 1, stats.AssignedIPs)
	assert.Equal(t, 1, stats.CooldownIPs)
	// Unchanged on the allocate side, deliberately. Subtracting cooldown here is issue #2896 and
	// would make the node allocate a replacement for an address it already holds.
	assert.Equal(t, 2, stats.AvailableAddresses())
	// Changed on the release side. Only the genuinely idle address is spare.
	assert.Equal(t, 1, stats.SurplusAddresses())
}

// TestFindFreeableCidrsExcludesCooling is the enforcement half. getDeletableENI already refuses an
// ENI holding a cooling address; the per-address path must refuse the address itself.
func TestFindFreeableCidrsExcludesCooling(t *testing.T) {
	ds := NewDataStore(Testlog, NullCheckpoint{}, false, defaultNetworkCard)
	_ = ds.AddENI("eni-1", 1, true, false, false, networkutils.CalculateRouteTableId(1, 0), "")
	for _, a := range []string{"1.1.1.1", "1.1.1.2", "1.1.1.3"} {
		_ = ds.AddIPv4CidrToStore("eni-1", cidr32(a), false)
	}
	assert.Len(t, ds.FindFreeableCidrs("eni-1"), 3, "nothing assigned, nothing cooling: all three are freeable")

	key := IPAMKey{"net0", "sandbox-1", "eth0"}
	_, _, _, err := ds.AssignPodIPv4Address(key, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "p1"})
	assert.NoError(t, err)
	assert.Len(t, ds.FindFreeableCidrs("eni-1"), 2, "one assigned: two freeable")

	_, _, _, _, _, err = ds.UnassignPodIPAddress(key)
	assert.NoError(t, err)
	// Before this change the vacated /32 reappeared here immediately, because the only test was
	// AssignedIPAddressesInCidr() == 0.
	assert.Len(t, ds.FindFreeableCidrs("eni-1"), 2, "the vacated address is cooling, so still only two freeable")

	// With the cooldown switched off the vacated address is genuinely spare again.
	os.Setenv(envIPCooldownPeriod, "0")
	defer os.Unsetenv(envIPCooldownPeriod)
	dsNoCooldown := NewDataStore(Testlog, NullCheckpoint{}, false, defaultNetworkCard)
	_ = dsNoCooldown.AddENI("eni-1", 1, true, false, false, networkutils.CalculateRouteTableId(1, 0), "")
	_ = dsNoCooldown.AddIPv4CidrToStore("eni-1", cidr32("1.1.1.1"), false)
	_, _, _, err = dsNoCooldown.AssignPodIPv4Address(key, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "p1"})
	assert.NoError(t, err)
	_, _, _, _, _, err = dsNoCooldown.UnassignPodIPAddress(key)
	assert.NoError(t, err)
	assert.Len(t, dsNoCooldown.FindFreeableCidrs("eni-1"), 1, "IP_COOLDOWN_PERIOD=0 means no reuse window, so the address is freeable")
}

// TestFindFreeableCidrsExcludesCoolingInPrefixMode covers the /28 granularity. A prefix whose only
// occupant has just left holds a cooling address and must not be released.
func TestFindFreeableCidrsExcludesCoolingInPrefixMode(t *testing.T) {
	ds := NewDataStore(Testlog, NullCheckpoint{}, true, defaultNetworkCard)
	_ = ds.AddENI("eni-1", 1, true, false, false, networkutils.CalculateRouteTableId(1, 0), "")
	_, prefix1, _ := net.ParseCIDR("10.0.0.0/28")
	_, prefix2, _ := net.ParseCIDR("10.0.0.16/28")
	_ = ds.AddIPv4CidrToStore("eni-1", *prefix1, true)
	_ = ds.AddIPv4CidrToStore("eni-1", *prefix2, true)
	assert.Len(t, ds.FindFreeableCidrs("eni-1"), 2)

	key := IPAMKey{"net0", "sandbox-1", "eth0"}
	_, _, _, err := ds.AssignPodIPv4Address(key, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "p1"})
	assert.NoError(t, err)
	assert.Len(t, ds.FindFreeableCidrs("eni-1"), 1)

	_, _, _, _, _, err = ds.UnassignPodIPAddress(key)
	assert.NoError(t, err)
	assert.Len(t, ds.FindFreeableCidrs("eni-1"), 1,
		"the prefix the pod vacated holds a cooling address, so only the untouched prefix is freeable")
}

// TestGetDeletableENIRespectsCoolingAndPods is the upstream guard this change models itself on.
// Pinned so a later refactor cannot quietly drop it.
func TestGetDeletableENIRespectsCoolingAndPods(t *testing.T) {
	ds := NewDataStore(Testlog, NullCheckpoint{}, false, defaultNetworkCard)
	_ = ds.AddENI("eni-primary", 0, true, false, false, networkutils.CalculateRouteTableId(0, 0), "")
	_ = ds.AddENI("eni-sec", 1, false, false, false, networkutils.CalculateRouteTableId(1, 0), "")
	_ = ds.AddIPv4CidrToStore("eni-sec", cidr32("1.1.1.1"), false)
	// Backdate the ENI so minENILifeTime is not what is being measured.
	ds.eniPool["eni-sec"].createTime = ds.eniPool["eni-sec"].createTime.Add(-2 * minENILifeTime)

	assert.Equal(t, "eni-sec", ds.getDeletableENI(0, 0, 0).ID, "idle secondary ENI is deletable")

	key := IPAMKey{"net0", "sandbox-1", "eth0"}
	_, _, _, err := ds.AssignPodIPv4Address(key, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "p1"})
	assert.NoError(t, err)
	assert.Nil(t, ds.getDeletableENI(0, 0, 0), "an ENI with a pod on it is never deletable")

	_, _, _, _, _, err = ds.UnassignPodIPAddress(key)
	assert.NoError(t, err)
	assert.Nil(t, ds.getDeletableENI(0, 0, 0), "an ENI holding a cooling address is never deletable, even at a zero warm target")
}
