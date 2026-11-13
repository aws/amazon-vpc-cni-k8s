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

package ipamd

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/ipamd/datastore"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/networkutils"
)

// TestCoolingAddressIsNotSurplus is the release-side half at the ipamd granularity. On the
// recommended WARM_IP_TARGET=1 configuration, a single departing pod used to make the pool read one
// address too high and hand the release path the cooling address as a candidate.
func TestCoolingAddressIsNotSurplus(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	c := &IPAMContext{
		awsClient:       m.awsutils,
		networkClient:   m.network,
		primaryIP:       make(map[string]string),
		dataStoreAccess: testDatastore(),
		maxIPsPerENI:    14,
		maxENI:          4,
		warmENITarget:   defaultWarmENITarget,
		warmIPTarget:    1,
		warmIPTargetSet: true,
		maxPods:         110,
		numNetworkCards: 1,
	}
	ds := c.dataStoreAccess.GetDataStore(defaultNetworkCard)
	_ = ds.AddENI(primaryENIid, 0, true, false, false, networkutils.CalculateRouteTableId(0, 0), "")
	for _, a := range []string{ipaddr01, ipaddr02, ipaddr03} {
		_ = ds.AddIPv4CidrToStore(primaryENIid, net.IPNet{IP: net.ParseIP(a), Mask: net.IPv4Mask(255, 255, 255, 255)}, false)
	}
	keys := []datastore.IPAMKey{
		{NetworkName: "net0", ContainerID: "sandbox-1", IfName: "eth0"},
		{NetworkName: "net0", ContainerID: "sandbox-2", IfName: "eth0"},
	}
	for _, k := range keys {
		_, _, _, err := ds.AssignPodIPv4Address(k, datastore.IPAMMetadata{K8SPodNamespace: "default", K8SPodName: k.ContainerID})
		assert.NoError(t, err)
	}

	// 3 total, 2 assigned, 1 free, warm target 1. At target.
	stats := ds.GetIPStats(ipV4AddrFamily)
	_, over, _ := c.datastoreTargetState(stats, defaultNetworkCard)
	assert.Equal(t, 0, over)
	assert.False(t, c.isDatastorePoolTooHigh(stats, defaultNetworkCard))

	// One pod departs. 3 total, 1 assigned, 1 cooling, 1 free, warm target 1. Still at target.
	_, _, _, _, _, err := ds.UnassignPodIPAddress(keys[0])
	assert.NoError(t, err)

	stats = ds.GetIPStats(ipV4AddrFamily)
	assert.Equal(t, 1, stats.CooldownIPs)
	_, over, _ = c.datastoreTargetState(stats, defaultNetworkCard)
	assert.Equal(t, 0, over, "the cooling address is held for the next pod, so the pool is not over target")
	assert.False(t, c.isDatastorePoolTooHigh(stats, defaultNetworkCard),
		"a single pod churn must not trigger a release at WARM_IP_TARGET=1")
	assert.Len(t, ds.FindFreeableCidrs(primaryENIid), 1,
		"only the genuinely idle address is a release candidate")
}
