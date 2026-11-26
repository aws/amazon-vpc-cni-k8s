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

package networkutils

import (
	"errors"
	"net"
	"syscall"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"

	mock_netlinkwrapper "github.com/aws/amazon-vpc-cni-k8s/pkg/netlinkwrapper/mocks"
)

const testTrunkLinkIndex = 3

func testTrunkLink(t *testing.T) *netlink.Device {
	mac, err := net.ParseMAC(testMAC2)
	assert.NoError(t, err)
	return &netlink.Device{LinkAttrs: netlink.LinkAttrs{Index: testTrunkLinkIndex, Name: "eth1", HardwareAddr: mac, MTU: 1500}}
}

func testVlanLink(name string, index, parentIndex, mtu int) *netlink.Vlan {
	return &netlink.Vlan{LinkAttrs: netlink.LinkAttrs{Index: index, Name: name, ParentIndex: parentIndex, MTU: mtu}}
}

// expectTrunkNetworkSetup expects everything setupENINetwork does to a trunk ENI apart from listing links.
func expectTrunkNetworkSetup(mockNetLink *mock_netlinkwrapper.MockNetLink, trunk netlink.Link) {
	mockNetLink.EXPECT().LinkSetMTU(trunk, testMTU).Return(nil)
	mockNetLink.EXPECT().LinkSetUp(trunk).Return(nil)
	mockNetLink.EXPECT().AddrList(trunk, unix.AF_INET).Return([]netlink.Addr{}, nil)
	mockNetLink.EXPECT().AddrAdd(trunk, gomock.Any()).Return(nil)
	mockNetLink.EXPECT().RouteDel(gomock.Any())
	mockNetLink.EXPECT().RouteReplace(gomock.Any()).Return(nil)
	mockNetLink.EXPECT().RouteDel(gomock.Any())
	mockNetLink.EXPECT().RouteReplace(gomock.Any()).Return(nil)
	mockNetLink.EXPECT().RouteDel(gomock.Any()).Return(syscall.ESRCH)
}

func setupTrunkENINetwork(mockNetLink *mock_netlinkwrapper.MockNetLink) error {
	return setupENINetwork(testEniIP, testMAC2, testNetworkCard, testEniSubnet, mockNetLink, 0*time.Second, 0*time.Second,
		testMTU, testMaxENIPerNIC, true, CalculateRouteTableId(testTable, 0), false)
}

// A VLAN created while the trunk was still at its boot-time MTU keeps that MTU, because the kernel
// lowers a VLAN with its parent but never raises it. Trunk setup must raise such VLANs (#3925).
func TestSetupENINetwork_TrunkRaisesStrandedVlanMTU(t *testing.T) {
	ctrl, mockNetLink, _, _, _ := setup(t)
	defer ctrl.Finish()

	trunk := testTrunkLink(t)
	stranded := testVlanLink("vlan.eth.4", 10, testTrunkLinkIndex, 1500)
	healthy := testVlanLink("vlan.eth.5", 11, testTrunkLinkIndex, testMTU)
	otherParent := testVlanLink("vlan.eth.6", 12, testTrunkLinkIndex+4, 1500)
	links := []netlink.Link{trunk, stranded, healthy, otherParent}

	mockNetLink.EXPECT().LinkList().Return(links, nil).Times(2)
	expectTrunkNetworkSetup(mockNetLink, trunk)
	// Only the VLAN on this trunk that is below the trunk MTU is touched.
	mockNetLink.EXPECT().LinkSetMTU(stranded, testMTU).Return(nil)

	assert.NoError(t, setupTrunkENINetwork(mockNetLink))
}

// The trunk itself is configured by then, so a failed VLAN repair must not fail trunk setup, and
// one failed VLAN must not stop the others from being repaired.
func TestSetupENINetwork_TrunkVlanMTURepairFailureIsNotFatal(t *testing.T) {
	ctrl, mockNetLink, _, _, _ := setup(t)
	defer ctrl.Finish()

	trunk := testTrunkLink(t)
	first := testVlanLink("vlan.eth.4", 10, testTrunkLinkIndex, 1500)
	second := testVlanLink("vlan.eth.5", 11, testTrunkLinkIndex, 1500)
	links := []netlink.Link{trunk, first, second}

	mockNetLink.EXPECT().LinkList().Return(links, nil).Times(2)
	expectTrunkNetworkSetup(mockNetLink, trunk)
	mockNetLink.EXPECT().LinkSetMTU(first, testMTU).Return(errors.New("simulated failure"))
	mockNetLink.EXPECT().LinkSetMTU(second, testMTU).Return(nil)

	assert.NoError(t, setupTrunkENINetwork(mockNetLink))
}

func TestSetupENINetwork_TrunkVlanListFailureIsNotFatal(t *testing.T) {
	ctrl, mockNetLink, _, _, _ := setup(t)
	defer ctrl.Finish()

	trunk := testTrunkLink(t)
	gomock.InOrder(
		mockNetLink.EXPECT().LinkList().Return([]netlink.Link{trunk}, nil),
		mockNetLink.EXPECT().LinkList().Return(nil, errors.New("simulated failure")),
	)
	expectTrunkNetworkSetup(mockNetLink, trunk)

	assert.NoError(t, setupTrunkENINetwork(mockNetLink))
}
