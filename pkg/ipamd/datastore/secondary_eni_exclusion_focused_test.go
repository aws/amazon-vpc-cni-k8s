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
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestSecondaryENIExclusionFocused tests the exact scenarios mentioned:
// 1. When secondary ENI is excluded, new pods should avoid it
// 2. When all non-excluded ENIs are full, allocation should fail appropriately
func TestSecondaryENIExclusionFocused(t *testing.T) {
	ds := NewDataStore(Testlog, NullCheckpoint{}, false, defaultNetworkCard)

	// Setup: Primary ENI with one IP
	err := ds.AddENI("eni-primary", 0, true, false, false, 0, "")
	assert.NoError(t, err)

	primaryIP := net.IPNet{IP: net.ParseIP("10.0.1.1"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	err = ds.AddIPv4CidrToStore("eni-primary", primaryIP, false)
	assert.NoError(t, err)

	// Setup: Secondary ENI with one IP
	err = ds.AddENI("eni-secondary", 1, false, false, false, 0, "")
	assert.NoError(t, err)

	secondaryIP := net.IPNet{IP: net.ParseIP("10.0.2.1"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	err = ds.AddIPv4CidrToStore("eni-secondary", secondaryIP, false)
	assert.NoError(t, err)

	// Test 1: Before exclusion - verify we have 2 allocatable ENIs
	allocatableENIs := ds.GetAllocatableENIs(10, false) // maxIPperENI=10, skipPrimary=false
	assert.Equal(t, 2, len(allocatableENIs), "Should have 2 allocatable ENIs before exclusion")

	// Test 2: Exclude secondary ENI
	err = ds.SetENIExcludedForPodIPs("eni-secondary", true)
	assert.NoError(t, err)
	assert.True(t, ds.eniPool["eni-secondary"].IsExcludedForPodIPs)

	// Test 3: After exclusion - verify only 1 allocatable ENI remains
	allocatableENIs = ds.GetAllocatableENIs(10, false)
	assert.Equal(t, 1, len(allocatableENIs), "Should have only 1 allocatable ENI after exclusion")
	assert.Equal(t, "eni-primary", allocatableENIs[0].ID)

	// Test 4: Assign first pod - should work and go to primary ENI
	key1 := IPAMKey{"net0", "pod-1", "eth0"}
	ip1, _, _, err := ds.AssignPodIPv4Address(key1, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "pod-1"})
	assert.NoError(t, err)
	assert.True(t, strings.HasPrefix(ip1, "10.0.1."), "First pod should go to primary ENI, got: %s", ip1)

	// Test 5: Primary ENI should now be full
	assert.Equal(t, 1, ds.eniPool["eni-primary"].AssignedIPv4Addresses())
	assert.Equal(t, 0, ds.eniPool["eni-secondary"].AssignedIPv4Addresses())

	// Test 6: Try to assign second pod - should fail since primary is full and secondary is excluded
	key2 := IPAMKey{"net0", "pod-2", "eth0"}
	_, _, _, err = ds.AssignPodIPv4Address(key2, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "pod-2"})
	assert.Error(t, err, "Should fail when only non-excluded ENI is full")
	assert.Contains(t, err.Error(), "no available IP/Prefix addresses")

	// Test 7: Verify excluded secondary ENI should be deletable (it has no pods)
	ds.eniPool["eni-secondary"].createTime = time.Time{} // Make it old enough

	// Make sure IPs are out of cooldown
	for _, cidr := range ds.eniPool["eni-secondary"].AvailableIPv4Cidrs {
		for _, addr := range cidr.IPAddresses {
			addr.UnassignedTime = time.Time{}
		}
	}

	removableENI := ds.RemoveUnusedENIFromStore(0, 0, 0)
	assert.Equal(t, "eni-secondary", removableENI, "Excluded secondary ENI with no pods should be deletable")

	// Test 8: Verify secondary ENI was removed from pool
	_, exists := ds.eniPool["eni-secondary"]
	assert.False(t, exists, "Secondary ENI should be removed from pool")

	// Test 9: Primary ENI should remain unaffected
	_, exists = ds.eniPool["eni-primary"]
	assert.True(t, exists, "Primary ENI should remain in pool")
	assert.Equal(t, 1, ds.eniPool["eni-primary"].AssignedIPv4Addresses())

	// Test 10: Verify allocatable ENIs after secondary removal
	allocatableENIs = ds.GetAllocatableENIs(10, false)
	assert.Equal(t, 1, len(allocatableENIs), "Should still have 1 allocatable ENI")
	assert.Equal(t, "eni-primary", allocatableENIs[0].ID)
}

// TestSecondaryENIExclusionWithPods tests exclusion when secondary ENI has existing pods
func TestSecondaryENIExclusionWithPods(t *testing.T) {
	ds := NewDataStore(Testlog, NullCheckpoint{}, false, defaultNetworkCard)

	// Setup ENIs
	err := ds.AddENI("eni-primary", 0, true, false, false, 0, "")
	assert.NoError(t, err)

	err = ds.AddENI("eni-secondary", 1, false, false, false, 0, "")
	assert.NoError(t, err)

	// Add IPs
	primaryIP := net.IPNet{IP: net.ParseIP("10.0.1.1"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	err = ds.AddIPv4CidrToStore("eni-primary", primaryIP, false)
	assert.NoError(t, err)

	secondaryIP := net.IPNet{IP: net.ParseIP("10.0.2.1"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	err = ds.AddIPv4CidrToStore("eni-secondary", secondaryIP, false)
	assert.NoError(t, err)

	// Assign pods to both ENIs naturally through regular allocation
	key1 := IPAMKey{"net0", "pod-1", "eth0"}
	ip1, _, _, err := ds.AssignPodIPv4Address(key1, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "pod-1"})
	assert.NoError(t, err)

	key2 := IPAMKey{"net0", "pod-2", "eth0"}
	ip2, _, _, err := ds.AssignPodIPv4Address(key2, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "pod-2"})
	assert.NoError(t, err)

	// Find which key corresponds to secondary ENI assignment
	var keyOnSecondary IPAMKey
	if strings.HasPrefix(ip1, "10.0.2.") {
		keyOnSecondary = key1
	} else if strings.HasPrefix(ip2, "10.0.2.") {
		keyOnSecondary = key2
	} else {
		t.Fatal("No pod was assigned to secondary ENI")
	}

	// Verify we have assignments on both ENIs
	totalPods := ds.eniPool["eni-primary"].AssignedIPv4Addresses() + ds.eniPool["eni-secondary"].AssignedIPv4Addresses()
	assert.Equal(t, 2, totalPods)
	assert.Greater(t, ds.eniPool["eni-secondary"].AssignedIPv4Addresses(), 0)

	// Now exclude secondary ENI
	err = ds.SetENIExcludedForPodIPs("eni-secondary", true)
	assert.NoError(t, err)

	// Test: Excluded ENI with pods should NOT be deletable
	ds.eniPool["eni-secondary"].createTime = time.Time{} // Make old enough
	removableENI := ds.RemoveUnusedENIFromStore(0, 0, 0)
	assert.Equal(t, "", removableENI, "Excluded ENI with pods should not be deletable")

	// Test: New allocations should skip excluded ENI (if primary has space)
	// We'll add another IP to primary to allow new allocation
	primaryIP2 := net.IPNet{IP: net.ParseIP("10.0.1.2"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	err = ds.AddIPv4CidrToStore("eni-primary", primaryIP2, false)
	assert.NoError(t, err)

	key3 := IPAMKey{"net0", "new-pod", "eth0"}
	ip3, _, _, err := ds.AssignPodIPv4Address(key3, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "new-pod"})
	assert.NoError(t, err)
	assert.True(t, strings.HasPrefix(ip3, "10.0.1."), "New pod should avoid excluded secondary ENI, got: %s", ip3)

	// Test: After removing pod from excluded ENI, it should become deletable
	_, _, _, _, _, err = ds.UnassignPodIPAddress(keyOnSecondary)
	assert.NoError(t, err)

	// Clear cooldown
	for _, cidr := range ds.eniPool["eni-secondary"].AvailableIPv4Cidrs {
		for _, addr := range cidr.IPAddresses {
			addr.UnassignedTime = time.Time{}
		}
	}

	// Now it should be deletable
	removableENI = ds.RemoveUnusedENIFromStore(0, 0, 0)
	assert.Equal(t, "eni-secondary", removableENI, "Excluded ENI should be deletable after pod removal")
}
