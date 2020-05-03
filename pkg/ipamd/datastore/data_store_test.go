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
	"testing"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/k8sapi"
	"github.com/stretchr/testify/assert"
)

var logConfig = logger.Configuration{
	BinaryName:  "aws-cni",
	LogLevel:    "Debug",
	LogLocation: "stdout",
}

var log = logger.New(&logConfig)

func TestAddENI(t *testing.T) {
	ds := NewDataStore(log)

	err := ds.AddENI("eni-1", 1, true)
	assert.NoError(t, err)

	err = ds.AddENI("eni-1", 1, true)
	assert.Error(t, err)

	err = ds.AddENI("eni-2", 2, false)
	assert.NoError(t, err)

	assert.Equal(t, len(ds.eniIPPools), 2)

	eniInfos := ds.GetENIInfos()
	assert.Equal(t, len(eniInfos.ENIIPPools), 2)
}

func TestDeleteENI(t *testing.T) {
	ds := NewDataStore(log)

	err := ds.AddENI("eni-1", 1, true)
	assert.NoError(t, err)

	err = ds.AddENI("eni-2", 2, false)
	assert.NoError(t, err)

	err = ds.AddENI("eni-3", 3, false)
	assert.NoError(t, err)

	eniInfos := ds.GetENIInfos()
	assert.Equal(t, len(eniInfos.ENIIPPools), 3)

	err = ds.RemoveENIFromDataStore("eni-2", false)
	assert.NoError(t, err)

	eniInfos = ds.GetENIInfos()
	assert.Equal(t, len(eniInfos.ENIIPPools), 2)

	err = ds.RemoveENIFromDataStore("unknown-eni", false)
	assert.Error(t, err)

	eniInfos = ds.GetENIInfos()
	assert.Equal(t, len(eniInfos.ENIIPPools), 2)

	// Add an IP and assign a pod.
	err = ds.AddIPv4AddressToStore("eni-1", "1.1.1.1")
	assert.NoError(t, err)
	podInfo := &k8sapi.K8SPodInfo{
		Name:      "pod-1",
		Namespace: "ns-1",
		IP:        "1.1.1.1",
	}
	ip, device, err := ds.AssignPodIPv4Address(podInfo)
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
	ds := NewDataStore(log)

	err := ds.AddENI("eni-1", 1, true)
	assert.NoError(t, err)

	err = ds.AddENI("eni-2", 2, false)
	assert.NoError(t, err)

	err = ds.AddIPv4AddressToStore("eni-1", "1.1.1.1")
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 1)
	assert.Equal(t, len(ds.eniIPPools["eni-1"].IPv4Addresses), 1)

	err = ds.AddIPv4AddressToStore("eni-1", "1.1.1.1")
	assert.Error(t, err)
	assert.Equal(t, ds.total, 1)
	assert.Equal(t, len(ds.eniIPPools["eni-1"].IPv4Addresses), 1)

	err = ds.AddIPv4AddressToStore("eni-1", "1.1.1.2")
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 2)
	assert.Equal(t, len(ds.eniIPPools["eni-1"].IPv4Addresses), 2)

	err = ds.AddIPv4AddressToStore("eni-2", "1.1.2.2")
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 3)
	assert.Equal(t, len(ds.eniIPPools["eni-1"].IPv4Addresses), 2)
	assert.Equal(t, len(ds.eniIPPools["eni-2"].IPv4Addresses), 1)

	err = ds.AddIPv4AddressToStore("dummy-eni", "1.1.2.2")
	assert.Error(t, err)
	assert.Equal(t, ds.total, 3)
	assert.Equal(t, len(ds.eniIPPools["eni-1"].IPv4Addresses), 2)
	assert.Equal(t, len(ds.eniIPPools["eni-2"].IPv4Addresses), 1)

}

func TestGetENIIPPools(t *testing.T) {
	ds := NewDataStore(log)

	err := ds.AddENI("eni-1", 1, true)
	assert.NoError(t, err)

	err = ds.AddENI("eni-2", 2, false)
	assert.NoError(t, err)

	err = ds.AddIPv4AddressToStore("eni-1", "1.1.1.1")
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 1)
	assert.Equal(t, len(ds.eniIPPools["eni-1"].IPv4Addresses), 1)

	err = ds.AddIPv4AddressToStore("eni-1", "1.1.1.2")
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 2)
	assert.Equal(t, len(ds.eniIPPools["eni-1"].IPv4Addresses), 2)

	err = ds.AddIPv4AddressToStore("eni-2", "1.1.2.2")
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 3)
	assert.Equal(t, len(ds.eniIPPools["eni-1"].IPv4Addresses), 2)
	assert.Equal(t, len(ds.eniIPPools["eni-2"].IPv4Addresses), 1)

	eniIPPool, err := ds.GetENIIPPools("eni-1")
	assert.NoError(t, err)
	assert.Equal(t, len(eniIPPool), 2)

	_, err = ds.GetENIIPPools("dummy-eni")
	assert.Error(t, err)
}

func TestDelENIIPv4Address(t *testing.T) {
	ds := NewDataStore(log)
	err := ds.AddENI("eni-1", 1, true)
	assert.NoError(t, err)

	err = ds.AddIPv4AddressToStore("eni-1", "1.1.1.1")
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 1)
	assert.Equal(t, len(ds.eniIPPools["eni-1"].IPv4Addresses), 1)

	err = ds.AddIPv4AddressToStore("eni-1", "1.1.1.2")
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 2)
	assert.Equal(t, len(ds.eniIPPools["eni-1"].IPv4Addresses), 2)

	err = ds.AddIPv4AddressToStore("eni-1", "1.1.1.3")
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 3)
	assert.Equal(t, len(ds.eniIPPools["eni-1"].IPv4Addresses), 3)

	err = ds.DelIPv4AddressFromStore("eni-1", "1.1.1.2", false)
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 2)
	assert.Equal(t, len(ds.eniIPPools["eni-1"].IPv4Addresses), 2)

	// delete a unknown IP
	err = ds.DelIPv4AddressFromStore("eni-1", "10.10.10.10", false)
	assert.Error(t, err)
	assert.Equal(t, ds.total, 2)
	assert.Equal(t, len(ds.eniIPPools["eni-1"].IPv4Addresses), 2)

	// Assign a pod.
	podInfo := &k8sapi.K8SPodInfo{
		Name:      "pod-1",
		Namespace: "ns-1",
		IP:        "1.1.1.1",
	}
	ip, device, err := ds.AssignPodIPv4Address(podInfo)
	assert.NoError(t, err)
	assert.Equal(t, "1.1.1.1", ip)
	assert.Equal(t, 1, device)

	// Test force removal.  The first call fails because the IP has a pod assigned to it, but the
	// second call force-removes it and succeeds.
	err = ds.DelIPv4AddressFromStore("eni-1", "1.1.1.1", false)
	assert.Error(t, err)
	assert.Equal(t, ds.total, 2)
	assert.Equal(t, len(ds.eniIPPools["eni-1"].IPv4Addresses), 2)

	err = ds.DelIPv4AddressFromStore("eni-1", "1.1.1.1", true)
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 1)
	assert.Equal(t, len(ds.eniIPPools["eni-1"].IPv4Addresses), 1)
}

func TestPodIPv4Address(t *testing.T) {
	ds := NewDataStore(log)

	ds.AddENI("eni-1", 1, true)

	ds.AddENI("eni-2", 2, false)

	ds.AddIPv4AddressToStore("eni-1", "1.1.1.1")

	ds.AddIPv4AddressToStore("eni-1", "1.1.1.2")

	ds.AddIPv4AddressToStore("eni-2", "1.1.2.2")

	podInfo := k8sapi.K8SPodInfo{
		Name:      "pod-1",
		Namespace: "ns-1",
		IP:        "1.1.1.1",
	}

	ip, _, err := ds.AssignPodIPv4Address(&podInfo)

	assert.NoError(t, err)
	assert.Equal(t, "1.1.1.1", ip)
	assert.Equal(t, 3, ds.total)
	assert.Equal(t, 2, len(ds.eniIPPools["eni-1"].IPv4Addresses))
	assert.Equal(t, 1, ds.eniIPPools["eni-1"].AssignedIPv4Addresses)

	ip, _, err = ds.AssignPodIPv4Address(&podInfo)
	assert.NoError(t, err)
	assert.Equal(t, "1.1.1.1", ip)

	podsInfos := ds.GetPodInfos()
	assert.Equal(t, len(*podsInfos), 1)

	// duplicate add
	ip, _, err = ds.AssignPodIPv4Address(&podInfo)
	assert.NoError(t, err)
	assert.Equal(t, ip, "1.1.1.1")
	assert.Equal(t, ds.total, 3)
	assert.Equal(t, len(ds.eniIPPools["eni-1"].IPv4Addresses), 2)
	assert.Equal(t, ds.eniIPPools["eni-1"].AssignedIPv4Addresses, 1)

	// wrong ip address
	podInfo = k8sapi.K8SPodInfo{
		Name:      "pod-1",
		Namespace: "ns-1",
		IP:        "1.1.2.10",
	}

	_, _, err = ds.AssignPodIPv4Address(&podInfo)
	assert.Error(t, err)

	podInfo = k8sapi.K8SPodInfo{
		Name:      "pod-1",
		Namespace: "ns-2",
		IP:        "1.1.2.2",
	}

	ip, pod1Ns2Device, err := ds.AssignPodIPv4Address(&podInfo)
	assert.NoError(t, err)
	assert.Equal(t, ip, "1.1.2.2")
	assert.Equal(t, ds.total, 3)
	assert.Equal(t, ds.assigned, 2)
	assert.Equal(t, len(ds.eniIPPools["eni-2"].IPv4Addresses), 1)
	assert.Equal(t, ds.eniIPPools["eni-2"].AssignedIPv4Addresses, 1)

	podsInfos = ds.GetPodInfos()
	assert.Equal(t, len(*podsInfos), 2)

	podInfo = k8sapi.K8SPodInfo{
		Name:      "pod-1",
		Namespace: "ns-3",
		Sandbox:   "container-1",
	}

	ip, _, err = ds.AssignPodIPv4Address(&podInfo)
	assert.NoError(t, err)
	assert.Equal(t, ip, "1.1.1.2")
	assert.Equal(t, ds.total, 3)
	assert.Equal(t, ds.assigned, 3)
	assert.Equal(t, len(ds.eniIPPools["eni-1"].IPv4Addresses), 2)
	assert.Equal(t, ds.eniIPPools["eni-1"].AssignedIPv4Addresses, 2)

	// no more IP addresses
	podInfo = k8sapi.K8SPodInfo{
		Name:      "pod-2",
		Namespace: "ns-3",
	}

	_, _, err = ds.AssignPodIPv4Address(&podInfo)
	assert.Error(t, err)
	// Unassign unknown Pod
	_, _, err = ds.UnassignPodIPv4Address(&podInfo)
	assert.Error(t, err)

	// Unassign pod which have same name/namespace, but different container
	podInfo = k8sapi.K8SPodInfo{
		Name:      "pod-1",
		Namespace: "ns-3",
		Sandbox:   "container-2",
	}
	_, _, err = ds.UnassignPodIPv4Address(&podInfo)
	assert.Error(t, err)

	podInfo = k8sapi.K8SPodInfo{
		Name:      "pod-1",
		Namespace: "ns-2",
	}

	_, deviceNum, err := ds.UnassignPodIPv4Address(&podInfo)
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 3)
	assert.Equal(t, ds.assigned, 2)
	assert.Equal(t, deviceNum, pod1Ns2Device)
	assert.Equal(t, len(ds.eniIPPools["eni-2"].IPv4Addresses), 1)
	assert.Equal(t, ds.eniIPPools["eni-2"].AssignedIPv4Addresses, 0)

	noWarmIPTarget := 0
	noMinimumIPTarget := 0

	// Should not be able to free this ENI
	eni := ds.RemoveUnusedENIFromStore(noWarmIPTarget, noMinimumIPTarget)
	assert.True(t, eni == "")

	ds.eniIPPools["eni-2"].createTime = time.Time{}
	ds.eniIPPools["eni-2"].lastUnassignedTime = time.Time{}
	eni = ds.RemoveUnusedENIFromStore(noWarmIPTarget, noMinimumIPTarget)
	assert.Equal(t, eni, "eni-2")

	assert.Equal(t, ds.total, 2)
	assert.Equal(t, ds.assigned, 2)
}

func TestWarmENIInteractions(t *testing.T) {
	ds := NewDataStore(log)

	ds.AddENI("eni-1", 1, true)
	ds.AddENI("eni-2", 2, false)
	ds.AddENI("eni-3", 3, false)
	ds.AddIPv4AddressToStore("eni-1", "1.1.1.1")
	ds.AddIPv4AddressToStore("eni-1", "1.1.1.2")
	ds.AddIPv4AddressToStore("eni-2", "1.1.2.1")
	ds.AddIPv4AddressToStore("eni-2", "1.1.2.2")
	ds.AddIPv4AddressToStore("eni-3", "1.1.3.1")

	podInfo := k8sapi.K8SPodInfo{
		Name:      "pod-1",
		Namespace: "ns-1",
		IP:        "1.1.1.1",
	}
	_, _, err := ds.AssignPodIPv4Address(&podInfo)
	assert.NoError(t, err)

	podInfo = k8sapi.K8SPodInfo{
		Name:      "pod-2",
		Namespace: "ns-2",
		IP:        "1.1.1.2",
	}
	_, _, err = ds.AssignPodIPv4Address(&podInfo)
	assert.NoError(t, err)

	noWarmIPTarget := 0

	ds.eniIPPools["eni-2"].createTime = time.Time{}
	ds.eniIPPools["eni-2"].lastUnassignedTime = time.Time{}
	ds.eniIPPools["eni-3"].createTime = time.Time{}
	ds.eniIPPools["eni-3"].lastUnassignedTime = time.Time{}

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
}
