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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// GetPodIPSubnets feeds the eks_insight metric's subnet label, so it must name exactly the
// subnets AssignPodIPv4Address can hand a pod an address from.
func TestGetPodIPSubnetsMatchesWhatPodsCanBeAssignedFrom(t *testing.T) {
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
		assert.Equal(t, []string{"subnet-0aaa000000000001"}, ds.GetPodIPSubnets())
	})

	t.Run("duplicate subnets are collapsed", func(t *testing.T) {
		ds := newDS(false)
		addENI(ds, "eni-1", "subnet-0aaa000000000001", false, false, false, true)
		addENI(ds, "eni-2", "subnet-0aaa000000000001", false, false, false, true)
		assert.Equal(t, []string{"subnet-0aaa000000000001"}, ds.GetPodIPSubnets())
	})

	t.Run("several subnets are all reported", func(t *testing.T) {
		ds := newDS(false)
		addENI(ds, "eni-1", "subnet-0aaa000000000001", false, false, false, true)
		addENI(ds, "eni-2", "subnet-0bbb000000000002", false, false, false, true)
		got := ds.GetPodIPSubnets()
		sort.Strings(got)
		assert.Equal(t, []string{"subnet-0aaa000000000001", "subnet-0bbb000000000002"}, got)
	})

	// A trunk ENI is owned by the VPC resource controller and ipamd never assigns pod
	// addresses to it, so it has none to hand out.
	t.Run("a trunk ENI's subnet is not reported", func(t *testing.T) {
		ds := newDS(false)
		addENI(ds, "eni-trunk", "subnet-0ccc000000000003", false, true, false, true)
		assert.Empty(t, ds.GetPodIPSubnets())
	})

	t.Run("an excluded ENI's subnet is not reported", func(t *testing.T) {
		ds := newDS(false)
		addENI(ds, "eni-1", "subnet-0ddd000000000004", false, false, true, true)
		assert.Empty(t, ds.GetPodIPSubnets())
	})

	// A subnet too short to supply even one address leaves its ENIs empty, so an empty ENI's
	// subnet must still be reported.
	t.Run("an ENI with no CIDRs is reported", func(t *testing.T) {
		ds := newDS(false)
		addENI(ds, "eni-1", "subnet-0eee000000000005", false, false, false, false)
		assert.Equal(t, []string{"subnet-0eee000000000005"}, ds.GetPodIPSubnets())
	})

	t.Run("an ENI with no prefixes is reported in prefix delegation mode", func(t *testing.T) {
		ds := newDS(true)
		addENI(ds, "eni-1", "subnet-0fff000000000006", true, false, false, false)
		assert.Equal(t, []string{"subnet-0fff000000000006"}, ds.GetPodIPSubnets())
	})

	t.Run("an ENI with prefixes is reported in prefix delegation mode", func(t *testing.T) {
		ds := newDS(true)
		addENI(ds, "eni-1", "subnet-0abc000000000007", true, false, false, true)
		assert.Equal(t, []string{"subnet-0abc000000000007"}, ds.GetPodIPSubnets())
	})

	t.Run("an empty SubnetID is never emitted as a label", func(t *testing.T) {
		ds := newDS(false)
		addENI(ds, "eni-1", "", false, false, false, true)
		assert.Empty(t, ds.GetPodIPSubnets())
	})

	t.Run("the primary ENI's subnet is reported", func(t *testing.T) {
		ds := newDS(false)
		require.NoError(t, ds.AddENI("eni-primary", 0, true, false, false, 0, "subnet-0aaa000000000001"))
		addENI(ds, "eni-secondary", "subnet-0bbb000000000002", false, false, false, true)

		got := ds.GetPodIPSubnets()
		sort.Strings(got)
		assert.Equal(t, []string{"subnet-0aaa000000000001", "subnet-0bbb000000000002"}, got)
	})
}

// The hold-down is derived from this, so a wrong value silently changes the gate.
func TestGetIPCooldownPeriodReportsTheConfiguredValue(t *testing.T) {
	t.Run("the configured value", func(t *testing.T) {
		t.Setenv("IP_COOLDOWN_PERIOD", "45")
		ds := NewDataStore(Testlog, NullCheckpoint{}, false, 0)
		assert.Equal(t, 45*time.Second, ds.GetIPCooldownPeriod())
	})

	t.Run("the default when unset", func(t *testing.T) {
		ds := NewDataStore(Testlog, NullCheckpoint{}, false, 0)
		assert.Equal(t, 30*time.Second, ds.GetIPCooldownPeriod())
	})
}

func cidr(t *testing.T, s string) net.IPNet {
	t.Helper()
	_, n, err := net.ParseCIDR(s)
	require.NoError(t, err)
	return *n
}
