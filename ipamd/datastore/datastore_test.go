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
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/aws/amazon-vpc-cni-k8s/ipamd/metrics"
)

func TestAddENI(t *testing.T) {
	m, _ := metrics.New()
	ds := NewDatastore(m)

	err := ds.AddENI("eni-1", 1, true)
	assert.NoError(t, err)

	err = ds.AddENI("eni-1", 1, true)
	assert.Error(t, err)

	err = ds.AddENI("eni-2", 2, false)
	assert.NoError(t, err)

	assert.Equal(t, len(ds.enis), 2)

	// eniInfos := ds.GetENIInfos()
	// assert.Equal(t, len(eniInfos.enis), 2)
}

func TestAddIPAddr(t *testing.T) {
	m, _ := metrics.New()
	ds := NewDatastore(m)

	err := ds.AddENI("eni-1", 1, true)
	assert.NoError(t, err)

	err = ds.AddENI("eni-2", 2, false)
	assert.NoError(t, err)

	err = ds.AddIPAddr("eni-1", "1.1.1.1")
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 1)
	assert.Equal(t, len(ds.eniMap["eni-1"].Addrs), 1)

	err = ds.AddIPAddr("eni-1", "1.1.1.1")
	assert.Error(t, err)
	assert.Equal(t, ds.total, 1)
	assert.Equal(t, len(ds.eniMap["eni-1"].Addrs), 1)

	err = ds.AddIPAddr("eni-1", "1.1.1.2")
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 2)
	assert.Equal(t, len(ds.eniMap["eni-1"].Addrs), 2)

	err = ds.AddIPAddr("eni-2", "1.1.2.2")
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 3)
	assert.Equal(t, len(ds.eniMap["eni-1"].Addrs), 2)
	assert.Equal(t, len(ds.eniMap["eni-2"].Addrs), 1)

	err = ds.AddIPAddr("dummy-eni", "1.1.2.2")
	assert.Error(t, err)
	assert.Equal(t, ds.total, 3)
	assert.Equal(t, len(ds.eniMap["eni-1"].Addrs), 2)
	assert.Equal(t, len(ds.eniMap["eni-2"].Addrs), 1)

}

func TestPodIPv4Address(t *testing.T) {
	m, _ := metrics.New()
	ds := NewDatastore(m)

	ds.AddENI("eni-1", 1, true)

	ds.AddENI("eni-2", 2, false)

	ds.AddIPAddr("eni-1", "1.1.1.1")

	ds.AddIPAddr("eni-1", "1.1.1.2")

	ds.AddIPAddr("eni-2", "1.1.2.2")

	// podInfo := K8SPodInfo{
	// 	Name:      "pod-1",
	// 	Namespace: "ns-1",
	// 	IP:        "1.1.1.1",
	// }

	ctx := context.Background()

	// ip, _, err := ds.AssignPodIP(ctx, &podInfo)
	ip, err := ds.AssignPodIP(ctx, "pod-1", "ns-1")

	assert.NoError(t, err)
	assert.Equal(t, ip.IP.String(), "1.1.1.1")
	assert.Equal(t, ds.total, 3)
	assert.Equal(t, len(ds.eniMap["eni-1"].Addrs), 2)
	assert.Equal(t, ds.eniMap["eni-1"].AssignedIPs(), 1)
	ip, err = ds.AssignPodIP(ctx, "pod-1", "ns-1")
	// ip, _, err = ds.AssignPodIP(ctx, &podInfo)

	// podsInfos := ds.GetPodInfos()
	// assert.Equal(t, len(*podsInfos), 1)

	// duplicate add
	ip, err = ds.AssignPodIP(ctx, "pod-1", "ns-1")
	// ip, _, err = ds.AssignPodIP(ctx, &podInfo)
	assert.NoError(t, err)
	assert.Equal(t, ip.IP.String(), "1.1.1.1")
	assert.Equal(t, ds.total, 3)
	assert.Equal(t, len(ds.eniMap["eni-1"].Addrs), 2)
	assert.Equal(t, ds.eniMap["eni-1"].AssignedIPs(), 1)

	// wrong ip address
	// podInfo = K8SPodInfo{
	// 	Name:      "pod-1",
	// 	Namespace: "ns-1",
	// 	IP:        "1.1.2.10",
	// }

	// ip, _, err = ds.AssignPodIP(ctx, &podInfo)
	// assert.Error(t, err)

	// podInfo = K8SPodInfo{
	// 	Name:      "pod-1",
	// 	Namespace: "ns-2",
	// 	IP:        "1.1.2.2",
	// }

	// ip, pod1Ns2Device, err := ds.AssignPodIP(ctx, &podInfo)
	ip, err = ds.AssignPodIP(ctx, "pod-1", "ns-2")
	assert.NoError(t, err)
	// assert.Equal(t, ip.IP.String(), "1.1.2.2")
	assert.Equal(t, ds.total, 3)
	assert.Equal(t, ds.assignedIPs(), 2)
	// assert.Equal(t, len(ds.eniMap["eni-2"].Addrs), 1)
	// assert.Equal(t, ds.eniMap["eni-2"].AssignedIPs(), 1)

	// podsInfos = ds.GetPodInfos()
	// assert.Equal(t, len(*podsInfos), 2)

	// podInfo = K8SPodInfo{
	// 	Name:      "pod-1",
	// 	Namespace: "ns-3",
	// 	// Container: "container-1",
	// }

	// ip, _, err = ds.AssignPodIP(ctx, &podInfo)
	ip, err = ds.AssignPodIP(ctx, "pod-1", "ns-3")
	assert.NoError(t, err)
	// assert.Equal(t, ip.IP.String(), "1.1.1.2")
	assert.Equal(t, ds.total, 3)
	assert.Equal(t, ds.assignedIPs(), 3)
	// assert.Equal(t, len(ds.eniMap["eni-1"].Addrs), 2)
	// assert.Equal(t, ds.eniMap["eni-1"].AssignedIPs(), 2)

	// // no more IP addresses
	// podInfo = K8SPodInfo{
	// 	Name:      "pod-2",
	// 	Namespace: "ns-3",
	// }

	_, err = ds.AssignPodIP(ctx, "pod-2", "ns-3")
	assert.Error(t, err)
	// Unassign unknown Pod
	_, err = ds.UnassignPodIP(ctx, "pod-2", "ns-3")
	assert.Error(t, err)

	// Unassign pod which have same name/namespace, but different container
	// podInfo = K8SPodInfo{
	// 	Name:      "pod-1",
	// 	Namespace: "ns-3",
	// 	// Container: "container-2",
	// }
	// _, _, err = ds.UnAssignPodIPv4Address(ctx, &podInfo)
	// assert.Error(t, err)

	// podInfo = K8SPodInfo{
	// 	Name:      "pod-1",
	// 	Namespace: "ns-2",
	// }

	// ip, deviceNum, err := ds.UnAssignPodIPv4Address(ctx, &podInfo)
	ip, err = ds.UnassignPodIP(ctx, "pod-1", "ns-2")
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 3)
	assert.Equal(t, ds.assignedIPs(), 2)
	// assert.Equal(t, ip.ENI.Device, pod1Ns2Device) // TODO(tvi): Fix.
	assert.Equal(t, len(ds.eniMap["eni-2"].Addrs), 1)
	// assert.Equal(t, ds.eniMap["eni-2"].AssignedIPs(), 0)

	// should not able to free this eni
	eni, _ := ds.FreeENI()
	assert.True(t, (eni == ""))

	// ds.eniMap["eni-2"].Created = time.Time{}
	// ds.eniMap["eni-2"].Updated = time.Time{}
	// eni, err = ds.FreeENI()
	// assert.NoError(t, err)
	// assert.Equal(t, eni, "eni-2")

	// assert.Equal(t, ds.total, 2)
	// assert.Equal(t, ds.assignedIPs(), 2)
}
