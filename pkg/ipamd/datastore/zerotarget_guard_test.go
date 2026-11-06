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
	"fmt"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/networkutils"
)

// TestZeroTargetPredicatesAreAlwaysFalse establishes that the `target != 0` short-circuits in
// getDeletableENI carry no information. All three predicates have the shape `count < target` with a
// non-negative count, so at a target of zero they are false for every datastore shape. Swapping the
// short-circuit for an is-set test therefore cannot change which ENI is deletable.
//
// Recorded as a test rather than as a comment because the asymmetry reads like a defect: the
// allocation path does special-case a zero target, and the deallocation path does not.
func TestZeroTargetPredicatesAreAlwaysFalse(t *testing.T) {
	for _, pd := range []bool{false, true} {
		for _, shape := range []struct {
			name           string
			enis           int
			cidrsPerENI    int
			assignedPerENI int
		}{
			{"empty", 1, 0, 0},
			{"one eni idle", 1, 3, 0},
			{"one eni full", 1, 3, 3},
			{"two enis idle", 2, 3, 0},
			{"two enis partly used", 2, 3, 1},
			{"four enis idle", 4, 14, 0},
		} {
			name := fmt.Sprintf("pd=%v/%s", pd, shape.name)
			t.Run(name, func(t *testing.T) {
				ds := NewDataStore(Testlog, NullCheckpoint{}, pd, defaultNetworkCard)
				for e := 0; e < shape.enis; e++ {
					eniID := fmt.Sprintf("eni-%d", e)
					_ = ds.AddENI(eniID, e, e == 0, false, false, networkutils.CalculateRouteTableId(e, 0), "")
					for c := 0; c < shape.cidrsPerENI; c++ {
						var cidr net.IPNet
						if pd {
							cidr = net.IPNet{IP: net.ParseIP(fmt.Sprintf("10.%d.%d.0", e, c*16)), Mask: net.CIDRMask(28, 32)}
						} else {
							cidr = net.IPNet{IP: net.ParseIP(fmt.Sprintf("10.%d.0.%d", e, c+1)), Mask: net.CIDRMask(32, 32)}
						}
						_ = ds.AddIPv4CidrToStore(eniID, cidr, pd)
					}
					for a := 0; a < shape.assignedPerENI; a++ {
						key := IPAMKey{"net0", fmt.Sprintf("sandbox-%d-%d", e, a), "eth0"}
						_, _, _, _ = ds.AssignPodIPv4Address(key, IPAMMetadata{K8SPodName: key.ContainerID})
					}
				}
				for _, eni := range ds.eniPool {
					assert.False(t, ds.isRequiredForWarmIPTarget(0, eni),
						"isRequiredForWarmIPTarget must be false at a zero target, ENI %s", eni.ID)
					assert.False(t, ds.isRequiredForMinimumIPTarget(0, eni),
						"isRequiredForMinimumIPTarget must be false at a zero target, ENI %s", eni.ID)
					assert.False(t, ds.isRequiredForWarmPrefixTarget(0, eni),
						"isRequiredForWarmPrefixTarget must be false at a zero target, ENI %s", eni.ID)
				}
			})
		}
	}
}

// TestCoolingGuardDoesNotCoverANeverUsedENI is the limit of the cooldown fix, stated as a test.
// An ENI whose addresses were never assigned to a pod has no cooldown timestamps, so neither the
// ENI-level guard nor the new address-level guard holds it. That is the exact shape of the idle-node
// allocate/free loop in upstream issue #1451, which is therefore NOT fixed by the cooldown work.
func TestCoolingGuardDoesNotCoverANeverUsedENI(t *testing.T) {
	ds := NewDataStore(Testlog, NullCheckpoint{}, false, defaultNetworkCard)
	_ = ds.AddENI("eni-primary", 0, true, false, false, networkutils.CalculateRouteTableId(0, 0), "")
	_ = ds.AddENI("eni-sec", 1, false, false, false, networkutils.CalculateRouteTableId(1, 0), "")
	for i := 1; i <= 14; i++ {
		_ = ds.AddIPv4CidrToStore("eni-sec", net.IPNet{IP: net.ParseIP(fmt.Sprintf("10.1.0.%d", i)), Mask: net.CIDRMask(32, 32)}, false)
	}
	ds.eniPool["eni-sec"].createTime = ds.eniPool["eni-sec"].createTime.Add(-2 * minENILifeTime)

	assert.Len(t, ds.FindFreeableCidrs("eni-sec"), 14, "no address was ever assigned, so none is cooling")
	deletable := ds.getDeletableENI(0, 0, 0)
	assert.NotNil(t, deletable)
	assert.Equal(t, "eni-sec", deletable.ID,
		"a never-used ENI is deletable whatever the warm target, so the cooldown guard cannot break the #1451 loop")
}
