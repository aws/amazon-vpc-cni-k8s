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

package driver

import (
	"errors"
	"net"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"

	"github.com/vishvananda/netlink"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/cninswrapper/mock_ns"
	mocks_ip "github.com/aws/amazon-vpc-cni-k8s/pkg/ipwrapper/mocks"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/netlinkwrapper/mock_netlink"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/netlinkwrapper/mocks"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/nswrapper/mocks"
)

const (
	testMAC          = "01:23:45:67:89:ab"
	testMAC1         = "01:23:45:67:89:a0"
	testMAC2         = "01:23:45:67:89:a1"
	testIP           = "10.0.10.10"
	testContVethName = "eth0"
	testHostVethName = "aws-eth0"
	testFD           = 10
	testnetnsPath    = "/proc/1234/netns"
	testTable        = 10
	testeniIP        = "10.10.10.20"
	testeniMAC       = "01:23:45:67:89:ab"
	testeniSubnet    = "10.10.0.0/16"
)

func setup(t *testing.T) (*gomock.Controller,
	*mock_netlinkwrapper.MockNetLink,
	*mocks_ip.MockIP,
	*mock_nswrapper.MockNS) {
	ctrl := gomock.NewController(t)
	return ctrl,
		mock_netlinkwrapper.NewMockNetLink(ctrl),
		mocks_ip.NewMockIP(ctrl),
		mock_nswrapper.NewMockNS(ctrl)
}

func TestRun(t *testing.T) {
	ctrl, mockNetLink, mockIP, _ := setup(t)
	defer ctrl.Finish()

	mockContext := &createVethPairContext{
		contVethName: testContVethName,
		hostVethName: testHostVethName,
		netLink:      mockNetLink,
		ip:           mockIP,
		addr: &net.IPNet{
			IP:   net.ParseIP(testIP),
			Mask: net.IPv4Mask(255, 255, 255, 255),
		},
	}

	hwAddr, err := net.ParseMAC(testMAC)
	assert.NoError(t, err)

	mockLinkAttrs := &netlink.LinkAttrs{
		HardwareAddr: hwAddr,
	}
	mockHostVeth := mock_netlink.NewMockLink(ctrl)
	mockContVeth := mock_netlink.NewMockLink(ctrl)
	mockNS := mock_ns.NewMockNetNS(ctrl)
	gomock.InOrder(
		// veth pair
		mockNetLink.EXPECT().LinkAdd(gomock.Any()).Return(nil),
		//hostVeth
		mockNetLink.EXPECT().LinkByName(gomock.Any()).Return(mockHostVeth, nil),
		//host side setup
		mockNetLink.EXPECT().LinkSetUp(mockHostVeth).Return(nil),
		//container side
		mockNetLink.EXPECT().LinkByName(gomock.Any()).Return(mockContVeth, nil),
		// container setup
		mockNetLink.EXPECT().LinkSetUp(mockContVeth).Return(nil),
		// container
		mockContVeth.EXPECT().Attrs().Return(mockLinkAttrs),

		mockNetLink.EXPECT().RouteReplace(gomock.Any()).Return(nil),
		mockIP.EXPECT().AddDefaultRoute(gomock.Any(), mockContVeth).Return(nil),

		// container addr
		mockNetLink.EXPECT().AddrAdd(mockContVeth, gomock.Any()).Return(nil),

		// neighbor
		mockContVeth.EXPECT().Attrs().Return(mockLinkAttrs),
		// hostVethMAC
		mockHostVeth.EXPECT().Attrs().Return(mockLinkAttrs),
		mockNetLink.EXPECT().NeighAdd(gomock.Any()).Return(nil),

		mockNS.EXPECT().Fd().Return(uintptr(testFD)),
		// move it host namespace
		mockNetLink.EXPECT().LinkSetNsFd(mockHostVeth, testFD).Return(nil),
	)

	err = mockContext.run(mockNS)
	assert.NoError(t, err)
}

func TestRunLinkAddErr(t *testing.T) {
	ctrl, mockNetLink, mockIP, _ := setup(t)
	defer ctrl.Finish()

	mockContext := &createVethPairContext{
		contVethName: testContVethName,
		hostVethName: testHostVethName,
		netLink:      mockNetLink,
		ip:           mockIP,
		addr: &net.IPNet{
			IP:   net.ParseIP(testIP),
			Mask: net.IPv4Mask(255, 255, 255, 255),
		},
	}

	mockNS := mock_ns.NewMockNetNS(ctrl)
	gomock.InOrder(
		// veth pair
		mockNetLink.EXPECT().LinkAdd(gomock.Any()).Return(errors.New("error on LinkAdd")),
	)

	err := mockContext.run(mockNS)
	assert.Error(t, err)
}

func TestRunErrLinkByNameHost(t *testing.T) {
	ctrl, mockNetLink, mockIP, _ := setup(t)
	defer ctrl.Finish()

	mockContext := &createVethPairContext{
		contVethName: testContVethName,
		hostVethName: testHostVethName,
		netLink:      mockNetLink,
		ip:           mockIP,
		addr: &net.IPNet{
			IP:   net.ParseIP(testIP),
			Mask: net.IPv4Mask(255, 255, 255, 255),
		},
	}

	mockNS := mock_ns.NewMockNetNS(ctrl)
	gomock.InOrder(
		// veth pair
		mockNetLink.EXPECT().LinkAdd(gomock.Any()).Return(nil),
		//hostVeth
		mockNetLink.EXPECT().LinkByName(gomock.Any()).Return(nil, errors.New("error on LinkByName host")),
	)

	err := mockContext.run(mockNS)
	assert.Error(t, err)
}

func TestRunErrSetup(t *testing.T) {
	ctrl, mockNetLink, mockIP, _ := setup(t)
	defer ctrl.Finish()

	mockContext := &createVethPairContext{
		contVethName: testContVethName,
		hostVethName: testHostVethName,
		netLink:      mockNetLink,
		ip:           mockIP,
		addr: &net.IPNet{
			IP:   net.ParseIP(testIP),
			Mask: net.IPv4Mask(255, 255, 255, 255),
		},
	}

	mockHostVeth := mock_netlink.NewMockLink(ctrl)
	mockNS := mock_ns.NewMockNetNS(ctrl)
	gomock.InOrder(
		// veth pair
		mockNetLink.EXPECT().LinkAdd(gomock.Any()).Return(nil),
		//hostVeth
		mockNetLink.EXPECT().LinkByName(gomock.Any()).Return(mockHostVeth, nil),
		//host side setup
		mockNetLink.EXPECT().LinkSetUp(mockHostVeth).Return(errors.New("error on LinkSetup")),
	)

	err := mockContext.run(mockNS)
	assert.Error(t, err)
}

func TestRunErrLinkByNameCont(t *testing.T) {
	ctrl, mockNetLink, mockIP, _ := setup(t)
	defer ctrl.Finish()

	mockContext := &createVethPairContext{
		contVethName: testContVethName,
		hostVethName: testHostVethName,
		netLink:      mockNetLink,
		ip:           mockIP,
		addr: &net.IPNet{
			IP:   net.ParseIP(testIP),
			Mask: net.IPv4Mask(255, 255, 255, 255),
		},
	}

	mockHostVeth := mock_netlink.NewMockLink(ctrl)
	mockContVeth := mock_netlink.NewMockLink(ctrl)
	mockNS := mock_ns.NewMockNetNS(ctrl)
	gomock.InOrder(
		// veth pair
		mockNetLink.EXPECT().LinkAdd(gomock.Any()).Return(nil),
		//hostVeth
		mockNetLink.EXPECT().LinkByName(gomock.Any()).Return(mockHostVeth, nil),
		//host side setup
		mockNetLink.EXPECT().LinkSetUp(mockHostVeth).Return(nil),
		//container side
		mockNetLink.EXPECT().LinkByName(gomock.Any()).Return(mockContVeth, errors.New("error on LinkByName container")),
	)

	err := mockContext.run(mockNS)
	assert.Error(t, err)
}

func TestRunErrRouteAdd(t *testing.T) {
	ctrl, mockNetLink, mockIP, _ := setup(t)
	defer ctrl.Finish()

	mockContext := &createVethPairContext{
		contVethName: testContVethName,
		hostVethName: testHostVethName,
		netLink:      mockNetLink,
		ip:           mockIP,
		addr: &net.IPNet{
			IP:   net.ParseIP(testIP),
			Mask: net.IPv4Mask(255, 255, 255, 255),
		},
	}

	hwAddr, err := net.ParseMAC(testMAC)
	assert.NoError(t, err)

	mockLinkAttrs := &netlink.LinkAttrs{
		HardwareAddr: hwAddr,
	}
	mockHostVeth := mock_netlink.NewMockLink(ctrl)
	mockContVeth := mock_netlink.NewMockLink(ctrl)
	mockNS := mock_ns.NewMockNetNS(ctrl)
	gomock.InOrder(
		// veth pair
		mockNetLink.EXPECT().LinkAdd(gomock.Any()).Return(nil),
		//hostVeth
		mockNetLink.EXPECT().LinkByName(gomock.Any()).Return(mockHostVeth, nil),
		//host side setup
		mockNetLink.EXPECT().LinkSetUp(mockHostVeth).Return(nil),
		//container side
		mockNetLink.EXPECT().LinkByName(gomock.Any()).Return(mockContVeth, nil),
		// container setup
		mockNetLink.EXPECT().LinkSetUp(mockContVeth).Return(nil),
		// container
		mockContVeth.EXPECT().Attrs().Return(mockLinkAttrs),

		mockNetLink.EXPECT().RouteReplace(gomock.Any()).Return(errors.New("error on RouteReplace")),
	)

	err = mockContext.run(mockNS)
	assert.Error(t, err)
}

func TestRunErrAddDefaultRoute(t *testing.T) {
	ctrl, mockNetLink, mockIP, _ := setup(t)
	defer ctrl.Finish()

	mockContext := &createVethPairContext{
		contVethName: testContVethName,
		hostVethName: testHostVethName,
		netLink:      mockNetLink,
		ip:           mockIP,
		addr: &net.IPNet{
			IP:   net.ParseIP(testIP),
			Mask: net.IPv4Mask(255, 255, 255, 255),
		},
	}

	hwAddr, err := net.ParseMAC(testMAC)
	assert.NoError(t, err)

	mockLinkAttrs := &netlink.LinkAttrs{
		HardwareAddr: hwAddr,
	}
	mockHostVeth := mock_netlink.NewMockLink(ctrl)
	mockContVeth := mock_netlink.NewMockLink(ctrl)
	mockNS := mock_ns.NewMockNetNS(ctrl)
	gomock.InOrder(
		// veth pair
		mockNetLink.EXPECT().LinkAdd(gomock.Any()).Return(nil),
		//hostVeth
		mockNetLink.EXPECT().LinkByName(gomock.Any()).Return(mockHostVeth, nil),
		//host side setup
		mockNetLink.EXPECT().LinkSetUp(mockHostVeth).Return(nil),
		//container side
		mockNetLink.EXPECT().LinkByName(gomock.Any()).Return(mockContVeth, nil),
		// container setup
		mockNetLink.EXPECT().LinkSetUp(mockContVeth).Return(nil),
		// container
		mockContVeth.EXPECT().Attrs().Return(mockLinkAttrs),

		mockNetLink.EXPECT().RouteReplace(gomock.Any()).Return(nil),
		mockIP.EXPECT().AddDefaultRoute(gomock.Any(), mockContVeth).Return(errors.New("error on AddDefaultRoute")),
	)

	err = mockContext.run(mockNS)
	assert.Error(t, err)
}

func TestRunErrAddrAdd(t *testing.T) {
	ctrl, mockNetLink, mockIP, _ := setup(t)
	defer ctrl.Finish()

	mockContext := &createVethPairContext{
		contVethName: testContVethName,
		hostVethName: testHostVethName,
		netLink:      mockNetLink,
		ip:           mockIP,
		addr: &net.IPNet{
			IP:   net.ParseIP(testIP),
			Mask: net.IPv4Mask(255, 255, 255, 255),
		},
	}

	hwAddr, err := net.ParseMAC(testMAC)
	assert.NoError(t, err)

	mockLinkAttrs := &netlink.LinkAttrs{
		HardwareAddr: hwAddr,
	}
	mockHostVeth := mock_netlink.NewMockLink(ctrl)
	mockContVeth := mock_netlink.NewMockLink(ctrl)
	mockNS := mock_ns.NewMockNetNS(ctrl)
	gomock.InOrder(
		// veth pair
		mockNetLink.EXPECT().LinkAdd(gomock.Any()).Return(nil),
		//hostVeth
		mockNetLink.EXPECT().LinkByName(gomock.Any()).Return(mockHostVeth, nil),
		//host side setup
		mockNetLink.EXPECT().LinkSetUp(mockHostVeth).Return(nil),
		//container side
		mockNetLink.EXPECT().LinkByName(gomock.Any()).Return(mockContVeth, nil),
		// container setup
		mockNetLink.EXPECT().LinkSetUp(mockContVeth).Return(nil),
		// container
		mockContVeth.EXPECT().Attrs().Return(mockLinkAttrs),

		mockNetLink.EXPECT().RouteReplace(gomock.Any()).Return(nil),
		mockIP.EXPECT().AddDefaultRoute(gomock.Any(), mockContVeth).Return(nil),

		// container addr
		mockNetLink.EXPECT().AddrAdd(mockContVeth, gomock.Any()).Return(errors.New("error on AddrAdd")),
	)

	err = mockContext.run(mockNS)
	assert.Error(t, err)
}

func TestRunErrNeighAdd(t *testing.T) {
	ctrl, mockNetLink, mockIP, _ := setup(t)
	defer ctrl.Finish()

	mockContext := &createVethPairContext{
		contVethName: testContVethName,
		hostVethName: testHostVethName,
		netLink:      mockNetLink,
		ip:           mockIP,
		addr: &net.IPNet{
			IP:   net.ParseIP(testIP),
			Mask: net.IPv4Mask(255, 255, 255, 255),
		},
	}

	hwAddr, err := net.ParseMAC(testMAC)
	assert.NoError(t, err)

	mockLinkAttrs := &netlink.LinkAttrs{
		HardwareAddr: hwAddr,
	}
	mockHostVeth := mock_netlink.NewMockLink(ctrl)
	mockContVeth := mock_netlink.NewMockLink(ctrl)
	mockNS := mock_ns.NewMockNetNS(ctrl)
	gomock.InOrder(
		// veth pair
		mockNetLink.EXPECT().LinkAdd(gomock.Any()).Return(nil),
		//hostVeth
		mockNetLink.EXPECT().LinkByName(gomock.Any()).Return(mockHostVeth, nil),
		//host side setup
		mockNetLink.EXPECT().LinkSetUp(mockHostVeth).Return(nil),
		//container side
		mockNetLink.EXPECT().LinkByName(gomock.Any()).Return(mockContVeth, nil),
		// container setup
		mockNetLink.EXPECT().LinkSetUp(mockContVeth).Return(nil),
		// container
		mockContVeth.EXPECT().Attrs().Return(mockLinkAttrs),

		mockNetLink.EXPECT().RouteReplace(gomock.Any()).Return(nil),
		mockIP.EXPECT().AddDefaultRoute(gomock.Any(), mockContVeth).Return(nil),

		// container addr
		mockNetLink.EXPECT().AddrAdd(mockContVeth, gomock.Any()).Return(nil),
		// neighbor
		mockContVeth.EXPECT().Attrs().Return(mockLinkAttrs),
		// hostVethMAC
		mockHostVeth.EXPECT().Attrs().Return(mockLinkAttrs),
		mockNetLink.EXPECT().NeighAdd(gomock.Any()).Return(errors.New("error on NeighAdd")),
	)

	err = mockContext.run(mockNS)
	assert.Error(t, err)
}

func TestRunErrLinkSetNsFd(t *testing.T) {
	ctrl, mockNetLink, mockIP, _ := setup(t)
	defer ctrl.Finish()

	mockContext := &createVethPairContext{
		contVethName: testContVethName,
		hostVethName: testHostVethName,
		netLink:      mockNetLink,
		ip:           mockIP,
		addr: &net.IPNet{
			IP:   net.ParseIP(testIP),
			Mask: net.IPv4Mask(255, 255, 255, 255),
		},
	}

	hwAddr, err := net.ParseMAC(testMAC)
	assert.NoError(t, err)

	mockLinkAttrs := &netlink.LinkAttrs{
		HardwareAddr: hwAddr,
	}
	mockHostVeth := mock_netlink.NewMockLink(ctrl)
	mockContVeth := mock_netlink.NewMockLink(ctrl)
	mockNS := mock_ns.NewMockNetNS(ctrl)
	gomock.InOrder(
		// veth pair
		mockNetLink.EXPECT().LinkAdd(gomock.Any()).Return(nil),
		//hostVeth
		mockNetLink.EXPECT().LinkByName(gomock.Any()).Return(mockHostVeth, nil),
		//host side setup
		mockNetLink.EXPECT().LinkSetUp(mockHostVeth).Return(nil),
		//container side
		mockNetLink.EXPECT().LinkByName(gomock.Any()).Return(mockContVeth, nil),
		// container setup
		mockNetLink.EXPECT().LinkSetUp(mockContVeth).Return(nil),
		// container
		mockContVeth.EXPECT().Attrs().Return(mockLinkAttrs),

		mockNetLink.EXPECT().RouteReplace(gomock.Any()).Return(nil),
		mockIP.EXPECT().AddDefaultRoute(gomock.Any(), mockContVeth).Return(nil),

		// container addr
		mockNetLink.EXPECT().AddrAdd(mockContVeth, gomock.Any()).Return(nil),
		// neighbor
		mockContVeth.EXPECT().Attrs().Return(mockLinkAttrs),
		// hostVethMAC
		mockHostVeth.EXPECT().Attrs().Return(mockLinkAttrs),
		mockNetLink.EXPECT().NeighAdd(gomock.Any()).Return(nil),

		mockNS.EXPECT().Fd().Return(uintptr(testFD)),
		// move it host namespace
		mockNetLink.EXPECT().LinkSetNsFd(mockHostVeth, testFD).Return(errors.New("error on LinkSetNsFd")),
	)

	err = mockContext.run(mockNS)
	assert.Error(t, err)
}

func TestSetupPodNetwork(t *testing.T) {
	ctrl, mockNetLink, _, mockNS := setup(t)
	defer ctrl.Finish()

	mockHostVeth := mock_netlink.NewMockLink(ctrl)

	mockNetLink.EXPECT().LinkByName(testHostVethName).Return(mockHostVeth, errors.New("hostVeth already exists"))
	mockNS.EXPECT().WithNetNSPath(testnetnsPath, gomock.Any()).Return(nil)
	mockNetLink.EXPECT().LinkByName(testHostVethName).Return(mockHostVeth, nil)

	mockNetLink.EXPECT().LinkSetUp(mockHostVeth).Return(nil)

	hwAddr, err := net.ParseMAC(testMAC)
	assert.NoError(t, err)
	mockLinkAttrs := &netlink.LinkAttrs{
		HardwareAddr: hwAddr,
	}
	//log.Printf
	mockHostVeth.EXPECT().Attrs().Return(mockLinkAttrs)
	//add host route
	mockHostVeth.EXPECT().Attrs().Return(mockLinkAttrs)
	mockNetLink.EXPECT().RouteReplace(gomock.Any()).Return(nil)

	testRule := &netlink.Rule{
		SuppressIfgroup:   -1,
		SuppressPrefixlen: -1,
		Priority:          -1,
		Mark:              -1,
		Mask:              -1,
		Goto:              -1,
		Flow:              -1,
	}
	mockNetLink.EXPECT().NewRule().Return(testRule)
	// test to-pod rule
	mockNetLink.EXPECT().RuleDel(gomock.Any()).Return(nil)
	mockNetLink.EXPECT().RuleAdd(gomock.Any()).Return(nil)

	// test from-pod rule
	mockNetLink.EXPECT().NewRule().Return(testRule)
	mockNetLink.EXPECT().RuleDel(gomock.Any()).Return(nil)
	mockNetLink.EXPECT().RuleAdd(gomock.Any()).Return(nil)

	addr := &net.IPNet{
		IP:   net.ParseIP(testIP),
		Mask: net.IPv4Mask(255, 255, 255, 255),
	}
	var cidrs []string
	err = setupNS(testHostVethName, testContVethName, testnetnsPath, addr, testTable, cidrs, true, mockNetLink, mockNS)
	assert.NoError(t, err)
}

func TestSetupPodNetworkErrLinkByName(t *testing.T) {
	ctrl, mockNetLink, _, mockNS := setup(t)
	defer ctrl.Finish()

	mockHostVeth := mock_netlink.NewMockLink(ctrl)

	mockNetLink.EXPECT().LinkByName(testHostVethName).Return(mockHostVeth, errors.New("hostVeth already exists"))
	mockNS.EXPECT().WithNetNSPath(testnetnsPath, gomock.Any()).Return(nil)
	mockNetLink.EXPECT().LinkByName(testHostVethName).Return(mockHostVeth, errors.New("error on hostVethName"))

	addr := &net.IPNet{
		IP:   net.ParseIP(testIP),
		Mask: net.IPv4Mask(255, 255, 255, 255),
	}
	var cidrs []string
	err := setupNS(testHostVethName, testContVethName, testnetnsPath, addr, testTable, cidrs, false, mockNetLink, mockNS)

	assert.Error(t, err)
}

func TestSetupPodNetworkErrLinkSetup(t *testing.T) {
	ctrl, mockNetLink, _, mockNS := setup(t)
	defer ctrl.Finish()

	mockHostVeth := mock_netlink.NewMockLink(ctrl)

	mockNetLink.EXPECT().LinkByName(testHostVethName).Return(mockHostVeth, errors.New("hostVeth already exists"))
	mockNS.EXPECT().WithNetNSPath(testnetnsPath, gomock.Any()).Return(nil)
	mockNetLink.EXPECT().LinkByName(testHostVethName).Return(mockHostVeth, nil)

	mockNetLink.EXPECT().LinkSetUp(mockHostVeth).Return(errors.New("error on LinkSetup"))

	addr := &net.IPNet{
		IP:   net.ParseIP(testIP),
		Mask: net.IPv4Mask(255, 255, 255, 255),
	}
	var cidrs []string
	err := setupNS(testHostVethName, testContVethName, testnetnsPath, addr, testTable, cidrs, false, mockNetLink, mockNS)

	assert.Error(t, err)
}

func TestSetupPodNetworkErrRouteReplace(t *testing.T) {
	ctrl, mockNetLink, _, mockNS := setup(t)
	defer ctrl.Finish()

	mockHostVeth := mock_netlink.NewMockLink(ctrl)

	mockNetLink.EXPECT().LinkByName(testHostVethName).Return(mockHostVeth, errors.New("hostVeth already exists"))
	mockNS.EXPECT().WithNetNSPath(testnetnsPath, gomock.Any()).Return(nil)
	mockNetLink.EXPECT().LinkByName(testHostVethName).Return(mockHostVeth, nil)

	mockNetLink.EXPECT().LinkSetUp(mockHostVeth).Return(nil)

	hwAddr, err := net.ParseMAC(testMAC)
	assert.NoError(t, err)
	mockLinkAttrs := &netlink.LinkAttrs{
		HardwareAddr: hwAddr,
	}
	//log.Printf
	mockHostVeth.EXPECT().Attrs().Return(mockLinkAttrs)
	//add host route
	mockHostVeth.EXPECT().Attrs().Return(mockLinkAttrs)
	mockNetLink.EXPECT().RouteReplace(gomock.Any()).Return(errors.New("error on RouteReplace"))

	addr := &net.IPNet{
		IP:   net.ParseIP(testIP),
		Mask: net.IPv4Mask(255, 255, 255, 255),
	}
	var cidrs []string
	err = setupNS(testHostVethName, testContVethName, testnetnsPath, addr, testTable, cidrs, false, mockNetLink, mockNS)

	assert.Error(t, err)
}

func TestSetupPodNetworkPrimaryIntf(t *testing.T) {
	ctrl, mockNetLink, _, mockNS := setup(t)
	defer ctrl.Finish()

	mockHostVeth := mock_netlink.NewMockLink(ctrl)

	mockNetLink.EXPECT().LinkByName(testHostVethName).Return(mockHostVeth, errors.New("hostVeth already exists"))
	mockNS.EXPECT().WithNetNSPath(testnetnsPath, gomock.Any()).Return(nil)
	mockNetLink.EXPECT().LinkByName(testHostVethName).Return(mockHostVeth, nil)

	mockNetLink.EXPECT().LinkSetUp(mockHostVeth).Return(nil)

	hwAddr, err := net.ParseMAC(testMAC)
	assert.NoError(t, err)
	mockLinkAttrs := &netlink.LinkAttrs{
		HardwareAddr: hwAddr,
	}
	//log.Printf
	mockHostVeth.EXPECT().Attrs().Return(mockLinkAttrs)
	//add host route
	mockHostVeth.EXPECT().Attrs().Return(mockLinkAttrs)
	mockNetLink.EXPECT().RouteReplace(gomock.Any()).Return(nil)

	testRule := &netlink.Rule{
		SuppressIfgroup:   -1,
		SuppressPrefixlen: -1,
		Priority:          -1,
		Mark:              -1,
		Mask:              -1,
		Goto:              -1,
		Flow:              -1,
	}
	mockNetLink.EXPECT().NewRule().Return(testRule)
	// test to-pod rule
	mockNetLink.EXPECT().RuleDel(gomock.Any()).Return(nil)
	mockNetLink.EXPECT().RuleAdd(gomock.Any()).Return(nil)

	addr := &net.IPNet{
		IP:   net.ParseIP(testIP),
		Mask: net.IPv4Mask(255, 255, 255, 255),
	}

	var cidrs []string
	err = setupNS(testHostVethName, testContVethName, testnetnsPath, addr, 0, cidrs, false, mockNetLink, mockNS)

	assert.NoError(t, err)
}

func TestTearDownPodNetwork(t *testing.T) {
	ctrl, mockNetLink, _, _ := setup(t)
	defer ctrl.Finish()

	testRule := &netlink.Rule{
		SuppressIfgroup:   -1,
		SuppressPrefixlen: -1,
		Priority:          -1,
		Mark:              -1,
		Mask:              -1,
		Goto:              -1,
		Flow:              -1,
	}
	gomock.InOrder(
		mockNetLink.EXPECT().NewRule().Return(testRule),
		// test to-pod rule
		mockNetLink.EXPECT().RuleDel(gomock.Any()).Return(nil),

		// test from-pod rule
		mockNetLink.EXPECT().RouteDel(gomock.Any()).Return(nil),
	)

	addr := &net.IPNet{
		IP:   net.ParseIP(testIP),
		Mask: net.IPv4Mask(255, 255, 255, 255),
	}
	err := tearDownNS(addr, 0, mockNetLink)
	assert.NoError(t, err)
}

func TestTearDownPodNetworkMain(t *testing.T) {
	ctrl, mockNetLink, _, _ := setup(t)
	defer ctrl.Finish()

	testRule := &netlink.Rule{
		SuppressIfgroup:   -1,
		SuppressPrefixlen: -1,
		Priority:          -1,
		Mark:              -1,
		Mask:              -1,
		Goto:              -1,
		Flow:              -1,
	}
	gomock.InOrder(
		mockNetLink.EXPECT().NewRule().Return(testRule),
		// test to-pod rule
		mockNetLink.EXPECT().RuleDel(gomock.Any()).Return(nil),

		mockNetLink.EXPECT().RouteDel(gomock.Any()).Return(nil),
	)

	addr := &net.IPNet{
		IP:   net.ParseIP(testIP),
		Mask: net.IPv4Mask(255, 255, 255, 255),
	}
	err := tearDownNS(addr, 0, mockNetLink)
	assert.NoError(t, err)
}
