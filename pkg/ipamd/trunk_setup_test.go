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
	"context"
	"errors"
	"testing"

	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/stretchr/testify/assert"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/awsutils"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/networkutils"
)

const trunkENIPrimaryIP = "10.10.20.5"

func newTrunkSetupContext(m *testMocks, useSubnetDiscovery bool) *IPAMContext {
	c := &IPAMContext{
		awsClient:          m.awsutils,
		networkClient:      m.network,
		primaryIP:          make(map[string]string),
		maxENI:             4,
		numNetworkCards:    1,
		enableIPv4:         true,
		useSubnetDiscovery: useSubnetDiscovery,
	}
	c.dataStoreAccess = testDatastore()
	return c
}

func trunkENIMetadata() awsutils.ENIMetadata {
	primary := true
	addr := trunkENIPrimaryIP
	return awsutils.ENIMetadata{
		ENIID:          secENIid,
		MAC:            secMAC,
		DeviceNumber:   secDevice,
		SubnetIPv4CIDR: secSubnet,
		SubnetID:       "subnet-trunk",
		NetworkCard:    defaultNetworkCard,
		IPv4Addresses:  []ec2types.NetworkInterfacePrivateIpAddress{{PrivateIpAddress: &addr, Primary: &primary}},
	}
}

func expectTrunkRouteTable(m *testMocks, c *IPAMContext) int {
	rt := networkutils.CalculateRouteTableId(secDevice, 0)
	m.awsutils.EXPECT().GetPrimaryENI().Return(primaryENIid).AnyTimes()
	m.network.EXPECT().GetRouteTableNumberForENI(defaultNetworkCard, trunkENIPrimaryIP, secDevice, c.maxENI, false).Return(rt, false, nil)
	return rt
}

// AddNetwork admits a pod ENI request as soon as GetTrunkENI() is non-empty, so the trunk must not
// be visible there until SetupENINetwork has raised its MTU and brought it up. Otherwise a branch
// VLAN can be created on a 1500 MTU trunk and is stranded there (#3925).
func TestSetupENI_TrunkNotPublishedUntilNetworkConfigured(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()
	c := newTrunkSetupContext(m, false)
	eni := trunkENIMetadata()
	rt := expectTrunkRouteTable(m, c)

	ds := c.dataStoreAccess.GetDataStore(defaultNetworkCard)
	m.network.EXPECT().SetupENINetwork(trunkENIPrimaryIP, secMAC, defaultNetworkCard, secSubnet, c.maxENI, true, rt, false).
		DoAndReturn(func(string, string, int, string, int, bool, int, bool) error {
			assert.Empty(t, ds.GetTrunkENI(), "trunk ENI must not be visible to AddNetwork while its link is being configured")
			_, inDatastore := ds.GetENIInfos().ENIs[secENIid]
			assert.False(t, inDatastore, "trunk ENI must not be in the datastore while its link is being configured")
			return nil
		})

	err := c.setupENI(context.Background(), eni.ENIID, eni, true, false)
	assert.NoError(t, err)
	assert.Equal(t, secENIid, ds.GetTrunkENI())
	assert.Equal(t, trunkENIPrimaryIP, c.primaryIP[secENIid])
}

func TestSetupENI_TrunkNotPublishedWhenNetworkSetupFails(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()
	c := newTrunkSetupContext(m, false)
	eni := trunkENIMetadata()
	rt := expectTrunkRouteTable(m, c)

	m.network.EXPECT().SetupENINetwork(trunkENIPrimaryIP, secMAC, defaultNetworkCard, secSubnet, c.maxENI, true, rt, false).
		Return(errors.New("simulated setup failure"))

	err := c.setupENI(context.Background(), eni.ENIID, eni, true, false)
	assert.Error(t, err)
	ds := c.dataStoreAccess.GetDataStore(defaultNetworkCard)
	assert.Empty(t, ds.GetTrunkENI())
	_, inDatastore := ds.GetENIInfos().ENIs[secENIid]
	assert.False(t, inDatastore)
	_, hasPrimaryIP := c.primaryIP[secENIid]
	assert.False(t, hasPrimaryIP)
}

// A trunk holds no pod IPs, so it is not checked against subnet exclusion. The mock fails the
// test on any IsSubnetExcluded call.
func TestSetupENI_TrunkSkipsSubnetExclusionCheck(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()
	c := newTrunkSetupContext(m, true)
	eni := trunkENIMetadata()
	rt := expectTrunkRouteTable(m, c)

	m.network.EXPECT().SetupENINetwork(trunkENIPrimaryIP, secMAC, defaultNetworkCard, secSubnet, c.maxENI, true, rt, false).Return(nil)

	err := c.setupENI(context.Background(), eni.ENIID, eni, true, false)
	assert.NoError(t, err)
	assert.Equal(t, secENIid, c.dataStoreAccess.GetDataStore(defaultNetworkCard).GetTrunkENI())
}
