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
	"errors"
	"testing"
	"time"

	//"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
	"github.com/stretchr/testify/assert"
)

/*
var logConfig = logger.Configuration{
	LogLevel:    "Debug",
	LogLocation: "stdout",
}

var Testlog = logger.New(&logConfig)
*/
func TestAddENIwithPDEnabled(t *testing.T) {
	ds := NewDataStore(Testlog, NullCheckpoint{})

	err := ds.AddENI("eni-1", 1, true, false, false, true)
	assert.NoError(t, err)

	err = ds.AddENI("eni-1", 1, true, false, false, true)
	assert.Error(t, err)

	err = ds.AddENI("eni-2", 2, false, false, false, true)
	assert.NoError(t, err)

	assert.Equal(t, len(ds.eniPool), 2)

	eniInfos := ds.GetENIInfos()
	assert.Equal(t, len(eniInfos.ENIs), 2)
}

func TestDeleteENIwithPDEnabled(t *testing.T) {
	ds := NewDataStore(Testlog, NullCheckpoint{})

	err := ds.AddENI("eni-1", 1, true, false, false, true)
	assert.NoError(t, err)

	err = ds.AddENI("eni-2", 2, false, false, false, true)
	assert.NoError(t, err)

	err = ds.AddENI("eni-3", 3, false, false, false, true)
	assert.NoError(t, err)

	eniInfos := ds.GetENIInfos()
	assert.Equal(t, len(eniInfos.ENIs), 3)

	err = ds.RemoveENIFromDataStore("eni-2", false)
	assert.NoError(t, err)

	eniInfos = ds.GetENIInfos()
	assert.Equal(t, len(eniInfos.ENIs), 2)

	err = ds.RemoveENIFromDataStore("unknown-eni", false)
	assert.Error(t, err)

	eniInfos = ds.GetENIInfos()
	assert.Equal(t, len(eniInfos.ENIs), 2)

	// Add an IP and assign a pod.
	err = ds.AddIPv4PrefixToStore("eni-1", "1.1.1.0/28")
	assert.NoError(t, err)
	assert.Equal(t, ds.allocatedPrefix, 1)
	assert.Equal(t, len(ds.eniPool["eni-1"].IPv4Prefixes), 1)
	ip, device, err := ds.AssignPodIPv4Address(IPAMKey{"net1", "sandbox1", "eth0"})
	assert.NoError(t, err)
	assert.Equal(t, "1.1.1.0", ip)
	assert.Equal(t, 1, device)

	// Test force removal.  The first call fails because eni-1 has an IP with a pod assigned to it,
	// but the second call force-removes it and succeeds.
	err = ds.RemoveENIFromDataStore("eni-1", false)
	assert.Error(t, err)
	err = ds.RemoveENIFromDataStore("eni-1", true)
	assert.NoError(t, err)
}

func TestAddENIIPv4Prefix(t *testing.T) {
	ds := NewDataStore(Testlog, NullCheckpoint{})

	err := ds.AddENI("eni-1", 1, true, false, false, true)
	assert.NoError(t, err)

	err = ds.AddENI("eni-2", 2, false, false, false, true)
	assert.NoError(t, err)

	err = ds.AddIPv4PrefixToStore("eni-1", "1.1.1.0/28")
	assert.NoError(t, err)
	assert.Equal(t, ds.allocatedPrefix, 1)
	assert.Equal(t, len(ds.eniPool["eni-1"].IPv4Prefixes), 1)

	var strPrivateIPv4 string
	strPrivateIPv4, _, err = getIPv4AddrfromPrefix(ds.eniPool["eni-1"].IPv4Prefixes["1.1.1.0"])
	assert.NoError(t, err)

	err = ds.AddPrefixIPv4AddressToStore("eni-1", strPrivateIPv4)
	assert.NoError(t, err)
	assert.Equal(t, len(ds.eniPool["eni-1"].IPv4Addresses), 1)

	strPrivateIPv4, _, err = getIPv4AddrfromPrefix(ds.eniPool["eni-1"].IPv4Prefixes["1.1.1.0"])
	assert.NoError(t, err)

	err = ds.AddPrefixIPv4AddressToStore("eni-1", strPrivateIPv4)
	assert.NoError(t, err)
	assert.Equal(t, len(ds.eniPool["eni-1"].IPv4Addresses), 2)

	err = ds.AddIPv4PrefixToStore("eni-2", "2.1.1.0/28")
	assert.NoError(t, err)
	assert.Equal(t, ds.allocatedPrefix, 2)
	assert.Equal(t, len(ds.eniPool["eni-2"].IPv4Prefixes), 1)

	strPrivateIPv4, _, err = getIPv4AddrfromPrefix(ds.eniPool["eni-2"].IPv4Prefixes["2.1.1.0"])
	assert.NoError(t, err)

	err = ds.AddPrefixIPv4AddressToStore("eni-2", strPrivateIPv4)
	assert.NoError(t, err)
	assert.Equal(t, len(ds.eniPool["eni-2"].IPv4Addresses), 1)

	assert.Equal(t, len(ds.eniPool["eni-1"].IPv4Addresses), 2)
	assert.Equal(t, len(ds.eniPool["eni-2"].IPv4Addresses), 1)

	err = ds.AddIPv4PrefixToStore("dummy-eni", "3.1.1.0/28")
	assert.Error(t, err)
	assert.Equal(t, len(ds.eniPool["eni-1"].IPv4Addresses), 2)
	assert.Equal(t, len(ds.eniPool["eni-2"].IPv4Addresses), 1)

}

func TestGetENIIPswithPDEnabled(t *testing.T) {
	ds := NewDataStore(Testlog, NullCheckpoint{})

	err := ds.AddENI("eni-1", 1, true, false, false, true)
	assert.NoError(t, err)

	err = ds.AddENI("eni-2", 2, false, false, false, true)
	assert.NoError(t, err)

	err = ds.AddIPv4PrefixToStore("eni-1", "1.1.1.0/28")
	assert.NoError(t, err)
	assert.Equal(t, ds.allocatedPrefix, 1)
	assert.Equal(t, len(ds.eniPool["eni-1"].IPv4Prefixes), 1)

	var strPrivateIPv4 string
	strPrivateIPv4, _, err = getIPv4AddrfromPrefix(ds.eniPool["eni-1"].IPv4Prefixes["1.1.1.0"])
	assert.NoError(t, err)

	err = ds.AddPrefixIPv4AddressToStore("eni-1", strPrivateIPv4)
	assert.NoError(t, err)
	assert.Equal(t, len(ds.eniPool["eni-1"].IPv4Addresses), 1)

	strPrivateIPv4, _, err = getIPv4AddrfromPrefix(ds.eniPool["eni-1"].IPv4Prefixes["1.1.1.0"])
	assert.NoError(t, err)

	err = ds.AddPrefixIPv4AddressToStore("eni-1", strPrivateIPv4)
	assert.NoError(t, err)
	assert.Equal(t, len(ds.eniPool["eni-1"].IPv4Addresses), 2)

	err = ds.AddIPv4PrefixToStore("eni-2", "2.1.1.0/28")
	assert.NoError(t, err)
	assert.Equal(t, ds.allocatedPrefix, 2)
	assert.Equal(t, len(ds.eniPool["eni-2"].IPv4Prefixes), 1)

	strPrivateIPv4, _, err = getIPv4AddrfromPrefix(ds.eniPool["eni-2"].IPv4Prefixes["2.1.1.0"])
	assert.NoError(t, err)

	err = ds.AddPrefixIPv4AddressToStore("eni-2", strPrivateIPv4)
	assert.NoError(t, err)
	assert.Equal(t, len(ds.eniPool["eni-2"].IPv4Addresses), 1)

	assert.Equal(t, len(ds.eniPool["eni-1"].IPv4Addresses), 2)
	assert.Equal(t, len(ds.eniPool["eni-2"].IPv4Addresses), 1)

	eniIPPool, err := ds.GetENIIPs("eni-1")
	assert.NoError(t, err)
	assert.Equal(t, len(eniIPPool), 2)

	_, err = ds.GetENIIPs("dummy-eni")
	assert.Error(t, err)
}

func TestDelENIIPv4Prefix(t *testing.T) {
	ds := NewDataStore(Testlog, NullCheckpoint{})
	err := ds.AddENI("eni-1", 1, true, false, false, true)
	assert.NoError(t, err)

	err = ds.AddIPv4PrefixToStore("eni-1", "1.1.1.0/28")
	assert.NoError(t, err)
	assert.Equal(t, ds.allocatedPrefix, 1)
	assert.Equal(t, len(ds.eniPool["eni-1"].IPv4Prefixes), 1)

	// Assign a pod.
	key := IPAMKey{"net0", "sandbox-1", "eth0"}
	ip, device, err := ds.AssignPodIPv4Address(key)
	assert.NoError(t, err)
	assert.Equal(t, "1.1.1.0", ip)
	assert.Equal(t, 1, device)

	// Test force removal.  The first call fails because the Prefix has a pod assigned to it, but the
	// second call force-removes it and succeeds.
	err = ds.DelIPv4PrefixFromStore("eni-1", "1.1.1.0/28", false)
	assert.Error(t, err)
	assert.Equal(t, ds.allocatedPrefix, 1)
	assert.Equal(t, len(ds.eniPool["eni-1"].IPv4Prefixes), 1)

	err = ds.DelIPv4PrefixFromStore("eni-1", "1.1.1.0/28", true)
	assert.NoError(t, err)
	assert.Equal(t, ds.allocatedPrefix, 0)
	assert.Equal(t, len(ds.eniPool["eni-1"].IPv4Prefixes), 0)
}

func TestPodIPv4AddresswithPDEnabled(t *testing.T) {
	checkpoint := NewTestCheckpoint(struct{}{})
	ds := NewDataStore(Testlog, checkpoint)

	err := ds.AddENI("eni-1", 1, true, false, false, true)
	assert.NoError(t, err)

	err = ds.AddENI("eni-2", 2, false, false, false, true)
	assert.NoError(t, err)

	err = ds.AddIPv4PrefixToStore("eni-1", "1.1.1.0/28")
	assert.NoError(t, err)
	assert.Equal(t, ds.allocatedPrefix, 1)
	assert.Equal(t, len(ds.eniPool["eni-1"].IPv4Prefixes), 1)

	key1 := IPAMKey{"net0", "sandbox-1", "eth0"}
	ip, _, err := ds.AssignPodIPv4Address(key1)

	assert.NoError(t, err)
	assert.Equal(t, "1.1.1.0", ip)
	assert.Equal(t, 1, ds.allocatedPrefix)
	assert.Equal(t, 1, len(ds.eniPool["eni-1"].IPv4Addresses))
	assert.Equal(t, 1, ds.eniPool["eni-1"].AssignedIPv4Addresses())
	assert.Equal(t, checkpoint.Data, &CheckpointData{
		Version: CheckpointFormatVersion,
		Allocations: []CheckpointEntry{
			{IPAMKey: IPAMKey{NetworkName: "net0", ContainerID: "sandbox-1", IfName: "eth0"}, IPv4: "1.1.1.0"},
		},
	})

	podsInfos := ds.AllocatedIPs()
	assert.Equal(t, len(podsInfos), 1)

	// duplicate add
	ip, _, err = ds.AssignPodIPv4Address(key1) // same id
	assert.NoError(t, err)
	assert.Equal(t, ip, "1.1.1.0")
	assert.Equal(t, ds.assigned, 1)
	assert.Equal(t, len(ds.eniPool["eni-1"].IPv4Addresses), 1)
	assert.Equal(t, len(ds.eniPool["eni-1"].IPv4Prefixes), 1)
	assert.Equal(t, ds.eniPool["eni-1"].AssignedIPv4Addresses(), 1)

	// Checkpoint error
	checkpoint.Error = errors.New("fake checkpoint error")
	key2 := IPAMKey{"net0", "sandbox-2", "eth0"}
	_, _, err = ds.AssignPodIPv4Address(key2)
	assert.Error(t, err)
	assert.Equal(t, checkpoint.Data, &CheckpointData{
		Version: CheckpointFormatVersion,
		Allocations: []CheckpointEntry{
			{IPAMKey: IPAMKey{NetworkName: "net0", ContainerID: "sandbox-1", IfName: "eth0"}, IPv4: "1.1.1.0"},
		},
	})
	checkpoint.Error = nil

	ip, pod1Ns2Device, err := ds.AssignPodIPv4Address(key2)
	assert.NoError(t, err)
	assert.Equal(t, ip, "1.1.1.1")
	assert.Equal(t, ds.assigned, 2)
	assert.Equal(t, len(ds.eniPool["eni-1"].IPv4Addresses), 2)
	assert.Equal(t, ds.eniPool["eni-1"].AssignedIPv4Addresses(), 2)
	assert.Equal(t, len(checkpoint.Data.(*CheckpointData).Allocations), 2)

	podsInfos = ds.AllocatedIPs()
	assert.Equal(t, len(podsInfos), 2)

	key3 := IPAMKey{"net0", "sandbox-3", "eth0"}
	ip, _, err = ds.AssignPodIPv4Address(key3)
	assert.NoError(t, err)
	assert.Equal(t, ip, "1.1.1.2")
	assert.Equal(t, ds.assigned, 3)
	assert.Equal(t, len(ds.eniPool["eni-1"].IPv4Addresses), 3)
	assert.Equal(t, ds.eniPool["eni-1"].AssignedIPv4Addresses(), 3)
	assert.Equal(t, len(checkpoint.Data.(*CheckpointData).Allocations), 3)

	key4 := IPAMKey{"net0", "sandbox-4", "eth0"}
	ip, _, err = ds.AssignPodIPv4Address(key4)
	assert.NoError(t, err)
	assert.Equal(t, ip, "1.1.1.3")
	assert.Equal(t, ds.assigned, 4)
	assert.Equal(t, len(ds.eniPool["eni-1"].IPv4Addresses), 4)
	assert.Equal(t, ds.eniPool["eni-1"].AssignedIPv4Addresses(), 4)
	assert.Equal(t, len(checkpoint.Data.(*CheckpointData).Allocations), 4)

	key5 := IPAMKey{"net0", "sandbox-5", "eth0"}
	ip, _, err = ds.AssignPodIPv4Address(key5)
	assert.NoError(t, err)
	assert.Equal(t, ip, "1.1.1.4")
	assert.Equal(t, ds.assigned, 5)
	assert.Equal(t, len(ds.eniPool["eni-1"].IPv4Addresses), 5)
	assert.Equal(t, ds.eniPool["eni-1"].AssignedIPv4Addresses(), 5)
	assert.Equal(t, len(checkpoint.Data.(*CheckpointData).Allocations), 5)

	key6 := IPAMKey{"net0", "sandbox-6", "eth0"}
	ip, _, err = ds.AssignPodIPv4Address(key6)
	assert.NoError(t, err)
	assert.Equal(t, ip, "1.1.1.5")
	assert.Equal(t, ds.assigned, 6)
	assert.Equal(t, len(ds.eniPool["eni-1"].IPv4Addresses), 6)
	assert.Equal(t, ds.eniPool["eni-1"].AssignedIPv4Addresses(), 6)
	assert.Equal(t, len(checkpoint.Data.(*CheckpointData).Allocations), 6)

	key7 := IPAMKey{"net0", "sandbox-7", "eth0"}
	ip, _, err = ds.AssignPodIPv4Address(key7)
	assert.NoError(t, err)
	assert.Equal(t, ip, "1.1.1.6")
	assert.Equal(t, ds.assigned, 7)
	assert.Equal(t, len(ds.eniPool["eni-1"].IPv4Addresses), 7)
	assert.Equal(t, ds.eniPool["eni-1"].AssignedIPv4Addresses(), 7)
	assert.Equal(t, len(checkpoint.Data.(*CheckpointData).Allocations), 7)

	key8 := IPAMKey{"net0", "sandbox-8", "eth0"}
	ip, _, err = ds.AssignPodIPv4Address(key8)
	assert.NoError(t, err)
	assert.Equal(t, ip, "1.1.1.7")
	assert.Equal(t, ds.assigned, 8)
	assert.Equal(t, len(ds.eniPool["eni-1"].IPv4Addresses), 8)
	assert.Equal(t, ds.eniPool["eni-1"].AssignedIPv4Addresses(), 8)
	assert.Equal(t, len(checkpoint.Data.(*CheckpointData).Allocations), 8)

	key9 := IPAMKey{"net0", "sandbox-9", "eth0"}
	ip, _, err = ds.AssignPodIPv4Address(key9)
	assert.NoError(t, err)
	assert.Equal(t, ip, "1.1.1.8")
	assert.Equal(t, ds.assigned, 9)
	assert.Equal(t, len(ds.eniPool["eni-1"].IPv4Addresses), 9)
	assert.Equal(t, ds.eniPool["eni-1"].AssignedIPv4Addresses(), 9)
	assert.Equal(t, len(checkpoint.Data.(*CheckpointData).Allocations), 9)

	key10 := IPAMKey{"net0", "sandbox-10", "eth0"}
	ip, _, err = ds.AssignPodIPv4Address(key10)
	assert.NoError(t, err)
	assert.Equal(t, ip, "1.1.1.9")
	assert.Equal(t, ds.assigned, 10)
	assert.Equal(t, len(ds.eniPool["eni-1"].IPv4Addresses), 10)
	assert.Equal(t, ds.eniPool["eni-1"].AssignedIPv4Addresses(), 10)
	assert.Equal(t, len(checkpoint.Data.(*CheckpointData).Allocations), 10)

	key11 := IPAMKey{"net0", "sandbox-11", "eth0"}
	ip, _, err = ds.AssignPodIPv4Address(key11)
	assert.NoError(t, err)
	assert.Equal(t, ip, "1.1.1.10")
	assert.Equal(t, ds.assigned, 11)
	assert.Equal(t, len(ds.eniPool["eni-1"].IPv4Addresses), 11)
	assert.Equal(t, ds.eniPool["eni-1"].AssignedIPv4Addresses(), 11)
	assert.Equal(t, len(checkpoint.Data.(*CheckpointData).Allocations), 11)

	key12 := IPAMKey{"net0", "sandbox-12", "eth0"}
	ip, _, err = ds.AssignPodIPv4Address(key12)
	assert.NoError(t, err)
	assert.Equal(t, ip, "1.1.1.11")
	assert.Equal(t, ds.assigned, 12)
	assert.Equal(t, len(ds.eniPool["eni-1"].IPv4Addresses), 12)
	assert.Equal(t, ds.eniPool["eni-1"].AssignedIPv4Addresses(), 12)
	assert.Equal(t, len(checkpoint.Data.(*CheckpointData).Allocations), 12)

	key13 := IPAMKey{"net0", "sandbox-13", "eth0"}
	ip, _, err = ds.AssignPodIPv4Address(key13)
	assert.NoError(t, err)
	assert.Equal(t, ip, "1.1.1.12")
	assert.Equal(t, ds.assigned, 13)
	assert.Equal(t, len(ds.eniPool["eni-1"].IPv4Addresses), 13)
	assert.Equal(t, ds.eniPool["eni-1"].AssignedIPv4Addresses(), 13)
	assert.Equal(t, len(checkpoint.Data.(*CheckpointData).Allocations), 13)

	key14 := IPAMKey{"net0", "sandbox-14", "eth0"}
	ip, _, err = ds.AssignPodIPv4Address(key14)
	assert.NoError(t, err)
	assert.Equal(t, ip, "1.1.1.13")
	assert.Equal(t, ds.assigned, 14)
	assert.Equal(t, len(ds.eniPool["eni-1"].IPv4Addresses), 14)
	assert.Equal(t, ds.eniPool["eni-1"].AssignedIPv4Addresses(), 14)
	assert.Equal(t, len(checkpoint.Data.(*CheckpointData).Allocations), 14)

	key15 := IPAMKey{"net0", "sandbox-15", "eth0"}
	ip, _, err = ds.AssignPodIPv4Address(key15)
	assert.NoError(t, err)
	assert.Equal(t, ip, "1.1.1.14")
	assert.Equal(t, ds.assigned, 15)
	assert.Equal(t, len(ds.eniPool["eni-1"].IPv4Addresses), 15)
	assert.Equal(t, ds.eniPool["eni-1"].AssignedIPv4Addresses(), 15)
	assert.Equal(t, len(checkpoint.Data.(*CheckpointData).Allocations), 15)

	key16 := IPAMKey{"net0", "sandbox-16", "eth0"}
	ip, _, err = ds.AssignPodIPv4Address(key16)
	assert.NoError(t, err)
	assert.Equal(t, ip, "1.1.1.15")
	assert.Equal(t, ds.assigned, 16)
	assert.Equal(t, len(ds.eniPool["eni-1"].IPv4Addresses), 16)
	assert.Equal(t, ds.eniPool["eni-1"].AssignedIPv4Addresses(), 16)
	assert.Equal(t, len(checkpoint.Data.(*CheckpointData).Allocations), 16)

	// no more IP addresses
	key17 := IPAMKey{"net0", "sandbox-17", "eth0"}
	_, _, err = ds.AssignPodIPv4Address(key17)
	assert.Error(t, err)
	// Unassign unknown Pod
	_, _, _, err = ds.UnassignPodIPv4Address(key17)
	assert.Error(t, err)

	_, _, deviceNum, err := ds.UnassignPodIPv4Address(key2)
	assert.NoError(t, err)
	assert.Equal(t, ds.assigned, 15)
	assert.Equal(t, deviceNum, pod1Ns2Device)
	assert.Equal(t, len(ds.eniPool["eni-1"].IPv4Addresses), 15)
	//assert.Equal(t, len(checkpoint.Data.(*CheckpointData).Allocations), 15)

	//Index will be in cooldown so IP won't be available
	_, _, err = ds.AssignPodIPv4Address(key2)
	assert.Error(t, err)

	noWarmIPTarget := 0
	noMinimumIPTarget := 0
	noWarmPrefixTarget := 0

	// Should not be able to free this ENI
	eni := ds.RemoveUnusedENIFromStore(noWarmIPTarget, noMinimumIPTarget, noWarmPrefixTarget)
	assert.True(t, eni == "")

	ds.eniPool["eni-2"].createTime = time.Time{}
	eni = ds.RemoveUnusedENIFromStore(noWarmIPTarget, noMinimumIPTarget, noWarmPrefixTarget)
	assert.Equal(t, eni, "eni-2")

	assert.Equal(t, ds.assigned, 15)
}

func TestWarmPrefixInteractions(t *testing.T) {
	ds := NewDataStore(Testlog, NullCheckpoint{})

	_ = ds.AddENI("eni-1", 1, true, false, false, true)
	_ = ds.AddENI("eni-2", 2, false, false, false, true)
	_ = ds.AddENI("eni-3", 3, false, false, false, true)

	err := ds.AddIPv4PrefixToStore("eni-1", "1.1.1.0/28")
	assert.NoError(t, err)
	assert.Equal(t, ds.allocatedPrefix, 1)
	assert.Equal(t, len(ds.eniPool["eni-1"].IPv4Prefixes), 1)

	err = ds.AddIPv4PrefixToStore("eni-2", "2.1.1.0/28")
	assert.NoError(t, err)
	assert.Equal(t, ds.allocatedPrefix, 2)
	assert.Equal(t, len(ds.eniPool["eni-2"].IPv4Prefixes), 1)

	err = ds.AddIPv4PrefixToStore("eni-3", "1.1.1.0/28")
	assert.NoError(t, err)
	assert.Equal(t, ds.allocatedPrefix, 3)
	assert.Equal(t, len(ds.eniPool["eni-3"].IPv4Prefixes), 1)

	ds.eniPool["eni-2"].createTime = time.Time{}
	ds.eniPool["eni-3"].createTime = time.Time{}

	//Setting WARM IP TARGET and ENI TARGET since it should be ignored
	// We have three ENIs, 3 prefixes
	// We should not be able to remove any ENIs if either warmPrefixTarget >= 3
	eni := ds.RemoveUnusedENIFromStore(1, 1, 3)
	assert.Equal(t, "", eni)

	// Should be able to free 2 ENI because prefix target is 1
	removedEni := ds.RemoveUnusedENIFromStore(2, 4, 1)
	assert.Contains(t, []string{"eni-2", "eni-3"}, removedEni)

	noWarmPrefixTarget := 0
	// Should be able to free 2 ENI
	secondRemovedEni := ds.RemoveUnusedENIFromStore(1, 2, noWarmPrefixTarget)
	assert.Contains(t, []string{"eni-2", "eni-3"}, secondRemovedEni)

	assert.NotEqual(t, removedEni, secondRemovedEni, "The two removed ENIs should not be the same ENI.")

	assert.Equal(t, 1, ds.GetENIs())
}
