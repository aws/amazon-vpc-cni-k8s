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

package datastore

import (
	"net"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// GetPodIPSubnets feeds the eks_problem metric's resource label, so it must name exactly
// the subnets the allocator draws pod addresses from. Naming one it does not draw from
// points the customer at a healthy resource; missing one hides the culprit. These cases are
// the ways the two can diverge.
func TestGetPodIPSubnetsMatchesWhatTheAllocatorScans(t *testing.T) {
	newDS := func(pd bool) *DataStore {
		return NewDataStore(Testlog, NullCheckpoint{}, pd, 0)
	}
	// addENI puts a non-primary ENI in the pool with one CIDR of the given kind.
	addENI := func(ds *DataStore, id, subnet string, isPrefix, isTrunk, excluded bool, withCIDR bool) {
		require.NoError(t, ds.AddENI(id, len(ds.eniPool), false, isTrunk, false, 0, subnet))
		ds.eniPool[id].IsExcludedForPodIPs = excluded
		if withCIDR {
			if isPrefix {
				require.NoError(t, ds.AddIPv4CidrToStore(id, cidr(t, "10.0.0.0/28"), true))
			} else {
				require.NoError(t, ds.AddIPv4CidrToStore(id, cidr(t, "10.0.0.1/32"), false))
			}
		}
	}

	t.Run("a plain ENI's subnet is reported", func(t *testing.T) {
		ds := newDS(false)
		addENI(ds, "eni-1", "subnet-0aaa000000000001", false, false, false, true)
		assert.Equal(t, []string{"subnet-0aaa000000000001"}, ds.GetPodIPSubnets(false))
	})

	t.Run("duplicate subnets are collapsed", func(t *testing.T) {
		ds := newDS(false)
		addENI(ds, "eni-1", "subnet-0aaa000000000001", false, false, false, true)
		addENI(ds, "eni-2", "subnet-0aaa000000000001", false, false, false, true)
		assert.Equal(t, []string{"subnet-0aaa000000000001"}, ds.GetPodIPSubnets(false))
	})

	t.Run("several subnets are all reported", func(t *testing.T) {
		ds := newDS(false)
		addENI(ds, "eni-1", "subnet-0aaa000000000001", false, false, false, true)
		addENI(ds, "eni-2", "subnet-0bbb000000000002", false, false, false, true)
		got := ds.GetPodIPSubnets(false)
		sort.Strings(got)
		assert.Equal(t, []string{"subnet-0aaa000000000001", "subnet-0bbb000000000002"}, got)
	})

	// GetAllocatableENIs skips trunk ENIs, so the assignment path never draws from one.
	t.Run("a trunk ENI's subnet is NOT reported", func(t *testing.T) {
		ds := newDS(false)
		addENI(ds, "eni-trunk", "subnet-0ccc000000000003", false, true, false, true)
		assert.Empty(t, ds.GetPodIPSubnets(false))
	})

	t.Run("an excluded ENI's subnet is NOT reported", func(t *testing.T) {
		ds := newDS(false)
		addENI(ds, "eni-1", "subnet-0ddd000000000004", false, false, true, true)
		assert.Empty(t, ds.GetPodIPSubnets(false))
	})

	// An ENI that holds nothing BECAUSE its subnet could not supply anything is the case
	// this metric exists for, so its subnet must still be reported. Filtering it out
	// produced a false negative in the worst case: on a live cluster in prefix-delegation
	// mode both ENIs held zero prefixes and no subnet was reported while every pod failed.
	t.Run("an ENI with no CIDRs IS reported - an empty ENI is the symptom", func(t *testing.T) {
		ds := newDS(false)
		addENI(ds, "eni-1", "subnet-0eee000000000005", false, false, false, false)
		assert.Equal(t, []string{"subnet-0eee000000000005"}, ds.GetPodIPSubnets(false))
	})

	t.Run("prefix-delegation mode with no prefixes attached IS reported", func(t *testing.T) {
		ds := newDS(true)
		addENI(ds, "eni-1", "subnet-0fff000000000006", true, false, false, false)
		assert.Equal(t, []string{"subnet-0fff000000000006"}, ds.GetPodIPSubnets(false))
	})

	t.Run("prefixes in prefix-delegation mode ARE reported", func(t *testing.T) {
		ds := newDS(true)
		addENI(ds, "eni-1", "subnet-0abc000000000007", true, false, false, true)
		assert.Equal(t, []string{"subnet-0abc000000000007"}, ds.GetPodIPSubnets(false))
	})

	t.Run("an empty SubnetID is never emitted as a label", func(t *testing.T) {
		ds := newDS(false)
		addENI(ds, "eni-1", "", false, false, false, true)
		assert.Empty(t, ds.GetPodIPSubnets(false))
	})

	// skipPrimary is for custom networking, where pod addresses come only from secondary
	// ENIs in the ENIConfig subnet. GetAllocatableENIs skips the primary on the same flag.
	t.Run("skipPrimary excludes the primary ENI's subnet", func(t *testing.T) {
		ds := newDS(false)
		require.NoError(t, ds.AddENI("eni-primary", 0, true, false, false, 0, "subnet-0aaa000000000001"))
		addENI(ds, "eni-secondary", "subnet-0bbb000000000002", false, false, false, true)

		assert.Equal(t, []string{"subnet-0bbb000000000002"}, ds.GetPodIPSubnets(true))

		got := ds.GetPodIPSubnets(false)
		sort.Strings(got)
		assert.Equal(t, []string{"subnet-0aaa000000000001", "subnet-0bbb000000000002"}, got,
			"without the flag the primary's subnet is a source like any other")
	})
}

// The hold-down is derived from this, so a wrong value silently changes the gate.
func TestGetIPCooldownPeriodReportsTheConfiguredValue(t *testing.T) {
	ds := NewDataStore(Testlog, NullCheckpoint{}, false, 0)
	assert.Equal(t, ds.ipCooldownPeriod, ds.GetIPCooldownPeriod())
}

func cidr(t *testing.T, s string) net.IPNet {
	t.Helper()
	_, n, err := net.ParseCIDR(s)
	require.NoError(t, err)
	return *n
}
