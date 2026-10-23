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
	"net"
	"syscall"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

const (
	testTrunkLinkIndex = 3
	testVlanLinkIndex  = 7
	testLoweredMTU     = 1500
)

func testTrunkDevice(mtu int) *netlink.Device {
	hwAddr, _ := net.ParseMAC(testMAC2)
	return &netlink.Device{LinkAttrs: netlink.LinkAttrs{
		Name:         "eth1",
		Index:        testTrunkLinkIndex,
		MTU:          mtu,
		HardwareAddr: hwAddr,
	}}
}

func testVlanLink(name string, index, mtu, parentIndex int) *netlink.Vlan {
	return &netlink.Vlan{
		LinkAttrs: netlink.LinkAttrs{Name: name, Index: index, MTU: mtu, ParentIndex: parentIndex},
		VlanId:    index,
	}
}

// TestSetupENINetworkRaisesStrandedVlanMTU covers the window the customer hit: a branch ENI
// VLAN was built while the trunk was still at the boot-time 1500, ipamd then raises the trunk
// to 9001, and the kernel leaves the VLAN behind at 1500 forever. Setting up the trunk must
// bring its VLAN children back up with it.
func TestSetupENINetworkRaisesStrandedVlanMTU(t *testing.T) {
	ctrl, mockNetLink, _, _, _ := setup(t)
	defer ctrl.Finish()

	trunkBefore := testTrunkDevice(testLoweredMTU)
	trunkAfter := testTrunkDevice(testMTU)
	strandedVlan := testVlanLink("vlan.eth.7", testVlanLinkIndex, testLoweredMTU, testTrunkLinkIndex)

	// linkByMac sees the trunk at its pre-setup MTU, then the reconcile pass sees it at the
	// MTU this function just programmed, with the VLAN child still stuck low.
	findTrunk := mockNetLink.EXPECT().LinkList().Return([]netlink.Link{trunkBefore}, nil)
	reconcileList := mockNetLink.EXPECT().LinkList().Return([]netlink.Link{trunkAfter, strandedVlan}, nil)
	gomock.InOrder(findTrunk, reconcileList)

	mockNetLink.EXPECT().LinkSetMTU(trunkBefore, testMTU).Return(nil)
	mockNetLink.EXPECT().LinkSetUp(trunkBefore).Return(nil)
	mockNetLink.EXPECT().AddrList(trunkBefore, unix.AF_INET).Return([]netlink.Addr{}, nil)
	mockNetLink.EXPECT().AddrAdd(trunkBefore, gomock.Any()).Return(nil)
	mockNetLink.EXPECT().RouteDel(gomock.Any()).Return(nil)
	mockNetLink.EXPECT().RouteReplace(gomock.Any()).Return(nil)
	mockNetLink.EXPECT().RouteDel(gomock.Any()).Return(nil)
	mockNetLink.EXPECT().RouteReplace(gomock.Any()).Return(nil)
	mockNetLink.EXPECT().RouteDel(gomock.Any()).Return(syscall.ESRCH)

	// This is the repair: the stranded VLAN is raised to the trunk's new MTU.
	mockNetLink.EXPECT().LinkSetMTU(strandedVlan, testMTU).Return(nil)

	isTrunkENI := true
	err := setupENINetwork(testEniIP, testMAC2, testNetworkCard, testEniSubnet, mockNetLink,
		0*time.Second, 0*time.Second, testMTU, testMaxENIPerNIC, isTrunkENI,
		CalculateRouteTableId(testTable, 0), false)
	assert.NoError(t, err)
}

// TestSetupENINetworkVlanReconcileFailureIsNotFatal asserts the trunk is still reported as set
// up when the VLAN repair cannot be done: the trunk itself is correctly configured, and the
// periodic reconciler will try again.
func TestSetupENINetworkVlanReconcileFailureIsNotFatal(t *testing.T) {
	ctrl, mockNetLink, _, _, _ := setup(t)
	defer ctrl.Finish()

	trunkBefore := testTrunkDevice(testLoweredMTU)

	findTrunk := mockNetLink.EXPECT().LinkList().Return([]netlink.Link{trunkBefore}, nil)
	reconcileList := mockNetLink.EXPECT().LinkList().Return(nil, errors.New("simulated dump failure"))
	gomock.InOrder(findTrunk, reconcileList)

	mockNetLink.EXPECT().LinkSetMTU(trunkBefore, testMTU).Return(nil)
	mockNetLink.EXPECT().LinkSetUp(trunkBefore).Return(nil)
	mockNetLink.EXPECT().AddrList(trunkBefore, unix.AF_INET).Return([]netlink.Addr{}, nil)
	mockNetLink.EXPECT().AddrAdd(trunkBefore, gomock.Any()).Return(nil)
	mockNetLink.EXPECT().RouteDel(gomock.Any()).Return(nil).Times(2)
	mockNetLink.EXPECT().RouteReplace(gomock.Any()).Return(nil).Times(2)
	mockNetLink.EXPECT().RouteDel(gomock.Any()).Return(syscall.ESRCH)

	isTrunkENI := true
	err := setupENINetwork(testEniIP, testMAC2, testNetworkCard, testEniSubnet, mockNetLink,
		0*time.Second, 0*time.Second, testMTU, testMaxENIPerNIC, isTrunkENI,
		CalculateRouteTableId(testTable, 0), false)
	assert.NoError(t, err)
}

// TestSetupENINetworkSkipsVlanReconcileForNonTrunkENI makes sure a plain secondary ENI does not
// pay for a second link dump. Expressed through the mock: no LinkList beyond linkByMac's.
func TestSetupENINetworkSkipsVlanReconcileForNonTrunkENI(t *testing.T) {
	ctrl, mockNetLink, _, _, _ := setup(t)
	defer ctrl.Finish()

	eth1 := testTrunkDevice(testLoweredMTU)

	mockNetLink.EXPECT().LinkList().Return([]netlink.Link{eth1}, nil).Times(1)
	mockNetLink.EXPECT().LinkSetMTU(eth1, testMTU).Return(nil)
	mockNetLink.EXPECT().LinkSetUp(eth1).Return(nil)
	mockNetLink.EXPECT().AddrList(eth1, unix.AF_INET).Return([]netlink.Addr{}, nil)
	mockNetLink.EXPECT().AddrAdd(eth1, gomock.Any()).Return(nil)
	mockNetLink.EXPECT().RouteDel(gomock.Any()).Return(nil).Times(2)
	mockNetLink.EXPECT().RouteReplace(gomock.Any()).Return(nil).Times(2)
	mockNetLink.EXPECT().RouteDel(gomock.Any()).Return(syscall.ESRCH)
	mockNetLink.EXPECT().RuleAdd(gomock.Any()).Return(nil)

	isTrunkENI := false
	err := setupENINetwork(testEniIP, testMAC2, testNetworkCard, testEniSubnet, mockNetLink,
		0*time.Second, 0*time.Second, testMTU, testMaxENIPerNIC, isTrunkENI,
		CalculateRouteTableId(testTable, 0), false)
	assert.NoError(t, err)
}

func TestReconcileVlanMTU(t *testing.T) {
	tests := []struct {
		name string
		// links is what the netlink dump returns.
		links []netlink.Link
		// wantSetMTU lists the VLAN links that must be raised, and to what.
		wantSetMTU  map[string]int
		setMTUErr   error
		linkListErr error
		wantHealed  int
		wantErr     bool
	}{
		{
			name:       "raises a VLAN stranded below the trunk MTU",
			links:      []netlink.Link{testTrunkDevice(testMTU), testVlanLink("vlan.eth.7", testVlanLinkIndex, testLoweredMTU, testTrunkLinkIndex)},
			wantSetMTU: map[string]int{"vlan.eth.7": testMTU},
			wantHealed: 1,
		},
		{
			name:       "no-op when the VLAN already matches the trunk",
			links:      []netlink.Link{testTrunkDevice(testMTU), testVlanLink("vlan.eth.7", testVlanLinkIndex, testMTU, testTrunkLinkIndex)},
			wantHealed: 0,
		},
		{
			// The trunk was lowered out of band. The kernel lowers the VLANs for us, and the
			// kernel would reject a VLAN above its parent with ERANGE anyway, so leave it be.
			name:       "never lowers a VLAN and never exceeds the trunk MTU",
			links:      []netlink.Link{testTrunkDevice(testLoweredMTU), testVlanLink("vlan.eth.7", testVlanLinkIndex, testMTU, testTrunkLinkIndex)},
			wantHealed: 0,
		},
		{
			name: "ignores VLANs parented on another link",
			links: []netlink.Link{
				testTrunkDevice(testMTU),
				testVlanLink("vlan.eth.9", 9, testLoweredMTU, testTrunkLinkIndex+1),
			},
			wantHealed: 0,
		},
		{
			name: "ignores non-VLAN links below the trunk MTU",
			links: []netlink.Link{
				testTrunkDevice(testMTU),
				&netlink.Device{LinkAttrs: netlink.LinkAttrs{Name: "eni1234", Index: 11, MTU: testLoweredMTU}},
			},
			wantHealed: 0,
		},
		{
			name: "raises every stranded VLAN under the trunk",
			links: []netlink.Link{
				testTrunkDevice(testMTU),
				testVlanLink("vlan.eth.7", 7, testLoweredMTU, testTrunkLinkIndex),
				testVlanLink("vlan.eth.8", 8, testLoweredMTU, testTrunkLinkIndex),
			},
			wantSetMTU: map[string]int{"vlan.eth.7": testMTU, "vlan.eth.8": testMTU},
			wantHealed: 2,
		},
		{
			name: "one failing VLAN does not hide the others",
			links: []netlink.Link{
				testTrunkDevice(testMTU),
				testVlanLink("vlan.eth.7", 7, testLoweredMTU, testTrunkLinkIndex),
				testVlanLink("vlan.eth.8", 8, testLoweredMTU, testTrunkLinkIndex),
			},
			wantSetMTU: map[string]int{"vlan.eth.7": testMTU, "vlan.eth.8": testMTU},
			setMTUErr:  errors.New("simulated LinkSetMTU failure"),
			wantHealed: 0,
			wantErr:    true,
		},
		{
			name:       "errors when the trunk link is not in the dump",
			links:      []netlink.Link{testVlanLink("vlan.eth.7", testVlanLinkIndex, testLoweredMTU, testTrunkLinkIndex)},
			wantHealed: 0,
			wantErr:    true,
		},
		{
			name:        "errors when the link dump fails",
			linkListErr: errors.New("simulated dump failure"),
			wantHealed:  0,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl, mockNetLink, _, _, _ := setup(t)
			defer ctrl.Finish()

			mockNetLink.EXPECT().LinkList().Return(tt.links, tt.linkListErr)
			for _, link := range tt.links {
				vlan, isVlan := link.(*netlink.Vlan)
				if !isVlan {
					continue
				}
				if mtu, want := tt.wantSetMTU[vlan.Name]; want {
					mockNetLink.EXPECT().LinkSetMTU(vlan, mtu).Return(tt.setMTUErr)
				}
			}

			healed, err := reconcileVlanMTU(mockNetLink, testTrunkLinkIndex)
			assert.Equal(t, tt.wantHealed, healed)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestReconcileVlanMTUForTrunk exercises the exported entry point the periodic ipamd
// reconciler calls.
func TestReconcileVlanMTUForTrunk(t *testing.T) {
	ctrl, mockNetLink, _, _, _ := setup(t)
	defer ctrl.Finish()

	strandedVlan := testVlanLink("vlan.eth.7", testVlanLinkIndex, testLoweredMTU, testTrunkLinkIndex)
	mockNetLink.EXPECT().LinkList().Return([]netlink.Link{testTrunkDevice(testMTU), strandedVlan}, nil)
	mockNetLink.EXPECT().LinkSetMTU(strandedVlan, testMTU).Return(nil)

	ln := &linuxNetwork{netLink: mockNetLink}
	healed, err := ln.ReconcileVlanMTUForTrunk(testTrunkLinkIndex)
	assert.NoError(t, err)
	assert.Equal(t, 1, healed)
}
