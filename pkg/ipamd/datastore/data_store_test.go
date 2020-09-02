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

	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"

	"github.com/stretchr/testify/assert"
)

var logConfig = logger.Configuration{
	LogLevel:    "Debug",
	LogLocation: "stdout",
}

var log = logger.New(&logConfig)

func TestAddENI(t *testing.T) {
	ds := NewDataStore(log, NullCheckpoint{})

	err := ds.AddENI("eni-1", 1, true, false, false)
	assert.NoError(t, err)

	err = ds.AddENI("eni-1", 1, true, false, false)
	assert.Error(t, err)

	err = ds.AddENI("eni-2", 2, false, false, false)
	assert.NoError(t, err)

	assert.Equal(t, len(ds.eniPool), 2)

	eniInfos := ds.GetENIInfos()
	assert.Equal(t, len(eniInfos.ENIs), 2)
}

func TestDeleteENI(t *testing.T) {
	ds := NewDataStore(log, NullCheckpoint{})

	err := ds.AddENI("eni-1", 1, true, false, false)
	assert.NoError(t, err)

	err = ds.AddENI("eni-2", 2, false, false, false)
	assert.NoError(t, err)

	err = ds.AddENI("eni-3", 3, false, false, false)
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
	err = ds.AddIPv4AddressToStore("eni-1", "1.1.1.1")
	assert.NoError(t, err)
	ip, device, err := ds.AssignPodIPv4Address(IPAMKey{"net1", "sandbox1", "eth0"})
	assert.NoError(t, err)
	assert.Equal(t, "1.1.1.1", ip)
	assert.Equal(t, 1, device)

	// Test force removal.  The first call fails because eni-1 has an IP with a pod assigned to it,
	// but the second call force-removes it and succeeds.
	err = ds.RemoveENIFromDataStore("eni-1", false)
	assert.Error(t, err)
	err = ds.RemoveENIFromDataStore("eni-1", true)
	assert.NoError(t, err)
}

func TestAddENIIPv4Address(t *testing.T) {
	ds := NewDataStore(log, NullCheckpoint{})

	err := ds.AddENI("eni-1", 1, true, false, false)
	assert.NoError(t, err)

	err = ds.AddENI("eni-2", 2, false, false, false)
	assert.NoError(t, err)

	err = ds.AddIPv4AddressToStore("eni-1", "1.1.1.1")
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 1)
	assert.Equal(t, len(ds.eniPool["eni-1"].IPv4Addresses), 1)

	err = ds.AddIPv4AddressToStore("eni-1", "1.1.1.1")
	assert.Error(t, err)
	assert.Equal(t, ds.total, 1)
	assert.Equal(t, len(ds.eniPool["eni-1"].IPv4Addresses), 1)

	err = ds.AddIPv4AddressToStore("eni-1", "1.1.1.2")
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 2)
	assert.Equal(t, len(ds.eniPool["eni-1"].IPv4Addresses), 2)

	err = ds.AddIPv4AddressToStore("eni-2", "1.1.2.2")
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 3)
	assert.Equal(t, len(ds.eniPool["eni-1"].IPv4Addresses), 2)
	assert.Equal(t, len(ds.eniPool["eni-2"].IPv4Addresses), 1)

	err = ds.AddIPv4AddressToStore("dummy-eni", "1.1.2.2")
	assert.Error(t, err)
	assert.Equal(t, ds.total, 3)
	assert.Equal(t, len(ds.eniPool["eni-1"].IPv4Addresses), 2)
	assert.Equal(t, len(ds.eniPool["eni-2"].IPv4Addresses), 1)

}

func TestGetENIIPs(t *testing.T) {
	ds := NewDataStore(log, NullCheckpoint{})

	err := ds.AddENI("eni-1", 1, true, false, false)
	assert.NoError(t, err)

	err = ds.AddENI("eni-2", 2, false, false, false)
	assert.NoError(t, err)

	err = ds.AddIPv4AddressToStore("eni-1", "1.1.1.1")
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 1)
	assert.Equal(t, len(ds.eniPool["eni-1"].IPv4Addresses), 1)

	err = ds.AddIPv4AddressToStore("eni-1", "1.1.1.2")
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 2)
	assert.Equal(t, len(ds.eniPool["eni-1"].IPv4Addresses), 2)

	err = ds.AddIPv4AddressToStore("eni-2", "1.1.2.2")
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 3)
	assert.Equal(t, len(ds.eniPool["eni-1"].IPv4Addresses), 2)
	assert.Equal(t, len(ds.eniPool["eni-2"].IPv4Addresses), 1)

	eniIPPool, err := ds.GetENIIPs("eni-1")
	assert.NoError(t, err)
	assert.Equal(t, len(eniIPPool), 2)

	_, err = ds.GetENIIPs("dummy-eni")
	assert.Error(t, err)
}

func TestDelENIIPv4Address(t *testing.T) {
	ds := NewDataStore(log, NullCheckpoint{})
	err := ds.AddENI("eni-1", 1, true, false, false)
	assert.NoError(t, err)

	err = ds.AddIPv4AddressToStore("eni-1", "1.1.1.1")
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 1)
	assert.Equal(t, len(ds.eniPool["eni-1"].IPv4Addresses), 1)

	// Assign a pod.
	key := IPAMKey{"net0", "sandbox-1", "eth0"}
	ip, device, err := ds.AssignPodIPv4Address(key)
	assert.NoError(t, err)
	assert.Equal(t, "1.1.1.1", ip)
	assert.Equal(t, 1, device)

	err = ds.AddIPv4AddressToStore("eni-1", "1.1.1.2")
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 2)
	assert.Equal(t, len(ds.eniPool["eni-1"].IPv4Addresses), 2)

	err = ds.AddIPv4AddressToStore("eni-1", "1.1.1.3")
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 3)
	assert.Equal(t, len(ds.eniPool["eni-1"].IPv4Addresses), 3)

	err = ds.DelIPv4AddressFromStore("eni-1", "1.1.1.2", false)
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 2)
	assert.Equal(t, len(ds.eniPool["eni-1"].IPv4Addresses), 2)

	// delete a unknown IP
	err = ds.DelIPv4AddressFromStore("eni-1", "10.10.10.10", false)
	assert.Error(t, err)
	assert.Equal(t, ds.total, 2)
	assert.Equal(t, len(ds.eniPool["eni-1"].IPv4Addresses), 2)

	// Test force removal.  The first call fails because the IP has a pod assigned to it, but the
	// second call force-removes it and succeeds.
	err = ds.DelIPv4AddressFromStore("eni-1", "1.1.1.1", false)
	assert.Error(t, err)
	assert.Equal(t, ds.total, 2)
	assert.Equal(t, len(ds.eniPool["eni-1"].IPv4Addresses), 2)

	err = ds.DelIPv4AddressFromStore("eni-1", "1.1.1.1", true)
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 1)
	assert.Equal(t, len(ds.eniPool["eni-1"].IPv4Addresses), 1)
}

func TestPodIPv4Address(t *testing.T) {
	checkpoint := NewTestCheckpoint(struct{}{})
	ds := NewDataStore(log, checkpoint)

	err := ds.AddENI("eni-1", 1, true, false, false)
	assert.NoError(t, err)

	err = ds.AddENI("eni-2", 2, false, false, false)
	assert.NoError(t, err)

	err = ds.AddIPv4AddressToStore("eni-1", "1.1.1.1")
	assert.NoError(t, err)

	key1 := IPAMKey{"net0", "sandbox-1", "eth0"}
	ip, _, err := ds.AssignPodIPv4Address(key1)

	assert.NoError(t, err)
	assert.Equal(t, "1.1.1.1", ip)
	assert.Equal(t, 1, ds.total)
	assert.Equal(t, 1, len(ds.eniPool["eni-1"].IPv4Addresses))
	assert.Equal(t, 1, ds.eniPool["eni-1"].AssignedIPv4Addresses())
	assert.Equal(t, checkpoint.Data, &CheckpointData{
		Version: CheckpointFormatVersion,
		Allocations: []CheckpointEntry{
			{IPAMKey: IPAMKey{NetworkName: "net0", ContainerID: "sandbox-1", IfName: "eth0"}, IPv4: "1.1.1.1"},
		},
	})

	podsInfos := ds.AllocatedIPs()
	assert.Equal(t, len(podsInfos), 1)

	err = ds.AddIPv4AddressToStore("eni-2", "1.1.2.2")
	assert.NoError(t, err)

	// duplicate add
	ip, _, err = ds.AssignPodIPv4Address(key1) // same id
	assert.NoError(t, err)
	assert.Equal(t, ip, "1.1.1.1")
	assert.Equal(t, ds.total, 2)
	assert.Equal(t, ds.assigned, 1)
	assert.Equal(t, len(ds.eniPool["eni-1"].IPv4Addresses), 1)
	assert.Equal(t, ds.eniPool["eni-1"].AssignedIPv4Addresses(), 1)
	assert.Equal(t, len(ds.eniPool["eni-2"].IPv4Addresses), 1)
	assert.Equal(t, ds.eniPool["eni-2"].AssignedIPv4Addresses(), 0)

	// Checkpoint error
	checkpoint.Error = errors.New("fake checkpoint error")
	key2 := IPAMKey{"net0", "sandbox-2", "eth0"}
	_, _, err = ds.AssignPodIPv4Address(key2)
	assert.Error(t, err)
	assert.Equal(t, checkpoint.Data, &CheckpointData{
		Version: CheckpointFormatVersion,
		Allocations: []CheckpointEntry{
			{IPAMKey: IPAMKey{NetworkName: "net0", ContainerID: "sandbox-1", IfName: "eth0"}, IPv4: "1.1.1.1"},
		},
	})
	checkpoint.Error = nil

	ip, pod1Ns2Device, err := ds.AssignPodIPv4Address(key2)
	assert.NoError(t, err)
	assert.Equal(t, ip, "1.1.2.2")
	assert.Equal(t, ds.total, 2)
	assert.Equal(t, ds.assigned, 2)
	assert.Equal(t, len(ds.eniPool["eni-2"].IPv4Addresses), 1)
	assert.Equal(t, ds.eniPool["eni-2"].AssignedIPv4Addresses(), 1)
	assert.Equal(t, len(checkpoint.Data.(*CheckpointData).Allocations), 2)

	podsInfos = ds.AllocatedIPs()
	assert.Equal(t, len(podsInfos), 2)

	err = ds.AddIPv4AddressToStore("eni-1", "1.1.1.2")
	assert.NoError(t, err)

	key3 := IPAMKey{"net0", "sandbox-3", "eth0"}
	ip, _, err = ds.AssignPodIPv4Address(key3)
	assert.NoError(t, err)
	assert.Equal(t, ip, "1.1.1.2")
	assert.Equal(t, ds.total, 3)
	assert.Equal(t, ds.assigned, 3)
	assert.Equal(t, len(ds.eniPool["eni-1"].IPv4Addresses), 2)
	assert.Equal(t, ds.eniPool["eni-1"].AssignedIPv4Addresses(), 2)
	assert.Equal(t, len(checkpoint.Data.(*CheckpointData).Allocations), 3)

	// no more IP addresses
	key4 := IPAMKey{"net0", "sandbox-4", "eth0"}
	_, _, err = ds.AssignPodIPv4Address(key4)
	assert.Error(t, err)
	// Unassign unknown Pod
	_, _, err = ds.UnassignPodIPv4Address(key4)
	assert.Error(t, err)

	_, deviceNum, err := ds.UnassignPodIPv4Address(key2)
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 3)
	assert.Equal(t, ds.assigned, 2)
	assert.Equal(t, deviceNum, pod1Ns2Device)
	assert.Equal(t, len(ds.eniPool["eni-2"].IPv4Addresses), 1)
	assert.Equal(t, ds.eniPool["eni-2"].AssignedIPv4Addresses(), 0)
	assert.Equal(t, len(checkpoint.Data.(*CheckpointData).Allocations), 2)

	noWarmIPTarget := 0
	noMinimumIPTarget := 0

	// Should not be able to free this ENI
	eni := ds.RemoveUnusedENIFromStore(noWarmIPTarget, noMinimumIPTarget)
	assert.True(t, eni == "")

	ds.eniPool["eni-2"].createTime = time.Time{}
	ds.eniPool["eni-2"].IPv4Addresses["1.1.2.2"].UnassignedTime = time.Time{}
	eni = ds.RemoveUnusedENIFromStore(noWarmIPTarget, noMinimumIPTarget)
	assert.Equal(t, eni, "eni-2")

	assert.Equal(t, ds.total, 2)
	assert.Equal(t, ds.assigned, 2)
}

func TestWarmENIInteractions(t *testing.T) {
	ds := NewDataStore(log, NullCheckpoint{})

	_ = ds.AddENI("eni-1", 1, true, false, false)
	_ = ds.AddENI("eni-2", 2, false, false, false)
	_ = ds.AddENI("eni-3", 3, false, false, false)

	_ = ds.AddIPv4AddressToStore("eni-1", "1.1.1.1")
	key1 := IPAMKey{"net0", "sandbox-1", "eth0"}
	_, _, err := ds.AssignPodIPv4Address(key1)
	assert.NoError(t, err)

	_ = ds.AddIPv4AddressToStore("eni-1", "1.1.1.2")
	key2 := IPAMKey{"net0", "sandbox-2", "eth0"}
	_, _, err = ds.AssignPodIPv4Address(key2)
	assert.NoError(t, err)

	_ = ds.AddIPv4AddressToStore("eni-2", "1.1.2.1")
	_ = ds.AddIPv4AddressToStore("eni-2", "1.1.2.2")
	_ = ds.AddIPv4AddressToStore("eni-3", "1.1.3.1")

	noWarmIPTarget := 0

	ds.eniPool["eni-2"].createTime = time.Time{}
	ds.eniPool["eni-3"].createTime = time.Time{}

	// We have three ENIs, 5 IPs and two pods on ENI 1. Each ENI can handle two pods.
	// We should not be able to remove any ENIs if either warmIPTarget >= 3 or minimumWarmIPTarget >= 5
	eni := ds.RemoveUnusedENIFromStore(3, 1)
	assert.Equal(t, "", eni)
	// Should not be able to free this ENI because we want at least 5 IPs, which requires at least three ENIs
	eni = ds.RemoveUnusedENIFromStore(1, 5)
	assert.Equal(t, "", eni)
	// Should be able to free an ENI because both warmIPTarget and minimumWarmIPTarget are both effectively 4
	removedEni := ds.RemoveUnusedENIFromStore(2, 4)
	assert.Contains(t, []string{"eni-2", "eni-3"}, removedEni)

	// Should not be able to free an ENI because minimumWarmIPTarget requires at least two ENIs and no warm IP target
	eni = ds.RemoveUnusedENIFromStore(noWarmIPTarget, 3)
	assert.Equal(t, "", eni)
	// Should be able to free an ENI because one ENI can provide a minimum count of 2 IPs
	secondRemovedEni := ds.RemoveUnusedENIFromStore(noWarmIPTarget, 2)
	assert.Contains(t, []string{"eni-2", "eni-3"}, secondRemovedEni)

	assert.NotEqual(t, removedEni, secondRemovedEni, "The two removed ENIs should not be the same ENI.")

	_ = ds.AddENI("eni-4", 3, false, true, false)
	_ = ds.AddENI("eni-5", 3, false, false, true)

	_ = ds.AddIPv4AddressToStore("eni-4", "1.1.4.1")
	_ = ds.AddIPv4AddressToStore("eni-5", "1.1.5.1")

	ds.eniPool["eni-4"].createTime = time.Time{}
	ds.eniPool["eni-5"].createTime = time.Time{}
	thirdRemovedEni := ds.RemoveUnusedENIFromStore(noWarmIPTarget, 2)
	// None of the others can be removed...
	assert.Equal(t, "", thirdRemovedEni)
	assert.Equal(t, 3, ds.GetENIs())
}
