// Copyright 2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

	"github.com/aws/amazon-vpc-cni-k8s/pkg/k8sapi"
	"github.com/stretchr/testify/assert"
)

func TestAddENI(t *testing.T) {
	ds := NewDataStore()

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
	ds := NewDataStore()

	err := ds.AddENI("eni-1", 1, true)
	assert.NoError(t, err)

	err = ds.AddENI("eni-2", 2, false)
	assert.NoError(t, err)

	err = ds.AddENI("eni-3", 3, false)
	assert.NoError(t, err)

	eniInfos := ds.GetENIInfos()
	assert.Equal(t, len(eniInfos.ENIIPPools), 3)

	err = ds.DeleteENI("eni-2")
	assert.NoError(t, err)

	eniInfos = ds.GetENIInfos()
	assert.Equal(t, len(eniInfos.ENIIPPools), 2)

	err = ds.DeleteENI("unknown-eni")
	assert.Error(t, err)

	eniInfos = ds.GetENIInfos()
	assert.Equal(t, len(eniInfos.ENIIPPools), 2)
}

func TestAddENIIPv4Address(t *testing.T) {
	ds := NewDataStore()

	err := ds.AddENI("eni-1", 1, true)
	assert.NoError(t, err)

	err = ds.AddENI("eni-2", 2, false)
	assert.NoError(t, err)

	err = ds.AddENIIPv4Address("eni-1", "1.1.1.1")
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 1)
	assert.Equal(t, len(ds.eniIPPools["eni-1"].IPv4Addresses), 1)

	err = ds.AddENIIPv4Address("eni-1", "1.1.1.1")
	assert.Error(t, err)
	assert.Equal(t, ds.total, 1)
	assert.Equal(t, len(ds.eniIPPools["eni-1"].IPv4Addresses), 1)

	err = ds.AddENIIPv4Address("eni-1", "1.1.1.2")
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 2)
	assert.Equal(t, len(ds.eniIPPools["eni-1"].IPv4Addresses), 2)

	err = ds.AddENIIPv4Address("eni-2", "1.1.2.2")
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 3)
	assert.Equal(t, len(ds.eniIPPools["eni-1"].IPv4Addresses), 2)
	assert.Equal(t, len(ds.eniIPPools["eni-2"].IPv4Addresses), 1)

	err = ds.AddENIIPv4Address("dummy-eni", "1.1.2.2")
	assert.Error(t, err)
	assert.Equal(t, ds.total, 3)
	assert.Equal(t, len(ds.eniIPPools["eni-1"].IPv4Addresses), 2)
	assert.Equal(t, len(ds.eniIPPools["eni-2"].IPv4Addresses), 1)

}

func TestGetENIIPPools(t *testing.T) {
	ds := NewDataStore()

	err := ds.AddENI("eni-1", 1, true)
	assert.NoError(t, err)

	err = ds.AddENI("eni-2", 2, false)
	assert.NoError(t, err)

	err = ds.AddENIIPv4Address("eni-1", "1.1.1.1")
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 1)
	assert.Equal(t, len(ds.eniIPPools["eni-1"].IPv4Addresses), 1)

	err = ds.AddENIIPv4Address("eni-1", "1.1.1.2")
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 2)
	assert.Equal(t, len(ds.eniIPPools["eni-1"].IPv4Addresses), 2)

	err = ds.AddENIIPv4Address("eni-2", "1.1.2.2")
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
	ds := NewDataStore()
	err := ds.AddENI("eni-1", 1, true)
	assert.NoError(t, err)

	err = ds.AddENIIPv4Address("eni-1", "1.1.1.1")
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 1)
	assert.Equal(t, len(ds.eniIPPools["eni-1"].IPv4Addresses), 1)

	err = ds.AddENIIPv4Address("eni-1", "1.1.1.2")
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 2)
	assert.Equal(t, len(ds.eniIPPools["eni-1"].IPv4Addresses), 2)

	err = ds.AddENIIPv4Address("eni-1", "1.1.1.3")
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 3)
	assert.Equal(t, len(ds.eniIPPools["eni-1"].IPv4Addresses), 3)

	err = ds.DelENIIPv4Address("eni-1", "1.1.1.2")
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 2)
	assert.Equal(t, len(ds.eniIPPools["eni-1"].IPv4Addresses), 2)

	// delete a unknown IP
	err = ds.DelENIIPv4Address("eni-1", "10.10.10.10")
	assert.Error(t, err)
	assert.Equal(t, ds.total, 2)
	assert.Equal(t, len(ds.eniIPPools["eni-1"].IPv4Addresses), 2)
}

func TestPodIPv4Address(t *testing.T) {
	ds := NewDataStore()

	ds.AddENI("eni-1", 1, true)

	ds.AddENI("eni-2", 2, false)

	ds.AddENIIPv4Address("eni-1", "1.1.1.1")

	ds.AddENIIPv4Address("eni-1", "1.1.1.2")

	ds.AddENIIPv4Address("eni-2", "1.1.2.2")

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
		Container: "container-1",
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
	_, _, err = ds.UnAssignPodIPv4Address(&podInfo)
	assert.Error(t, err)

	// Unassign pod which have same name/namespace, but different container
	podInfo = k8sapi.K8SPodInfo{
		Name:      "pod-1",
		Namespace: "ns-3",
		Container: "container-2",
	}
	_, _, err = ds.UnAssignPodIPv4Address(&podInfo)
	assert.Error(t, err)

	podInfo = k8sapi.K8SPodInfo{
		Name:      "pod-1",
		Namespace: "ns-2",
	}

	_, deviceNum, err := ds.UnAssignPodIPv4Address(&podInfo)
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 3)
	assert.Equal(t, ds.assigned, 2)
	assert.Equal(t, deviceNum, pod1Ns2Device)
	assert.Equal(t, len(ds.eniIPPools["eni-2"].IPv4Addresses), 1)
	assert.Equal(t, ds.eniIPPools["eni-2"].AssignedIPv4Addresses, 0)

	// should not able to free this eni
	eni := ds.FreeENI()
	assert.True(t, eni == "")

	ds.eniIPPools["eni-2"].createTime = time.Time{}
	ds.eniIPPools["eni-2"].lastUnAssignedTime = time.Time{}
	eni = ds.FreeENI()
	assert.Equal(t, eni, "eni-2")

	assert.Equal(t, ds.total, 2)
	assert.Equal(t, ds.assigned, 2)
}
