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
	"fmt"
	"net"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/sgpp"
	"github.com/pkg/errors"

	"github.com/coreos/go-iptables/iptables"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/iptableswrapper"
	mock_iptables "github.com/aws/amazon-vpc-cni-k8s/pkg/iptableswrapper/mocks"
	mocks_ip "github.com/aws/amazon-vpc-cni-k8s/pkg/ipwrapper/mocks"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/netlinkwrapper/mock_netlink"
	mock_netlinkwrapper "github.com/aws/amazon-vpc-cni-k8s/pkg/netlinkwrapper/mocks"
	mock_nswrapper "github.com/aws/amazon-vpc-cni-k8s/pkg/nswrapper/mocks"
)

const (
	loopback         = ""
	testMAC1         = "01:23:45:67:89:a0"
	testMAC2         = "01:23:45:67:89:a1"
	testTable        = 10
	testEniIP        = "10.10.10.20"
	testEniIP6       = "2600::2"
	testEniSubnet    = "10.10.0.0/16"
	testEniV6Subnet  = "2600::/64"
	testEniV6Gateway = "fe80::c9d:5dff:fec4:f389"
	// Default MTU of ENI and veth
	// defined in plugins/routed-eni/driver/driver.go, pkg/networkutils/network.go
	testMTU   = 9001
	eniPrefix = "eni"
)

var (
	_, testEniSubnetIPNet, _   = net.ParseCIDR(testEniSubnet)
	_, testEniV6SubnetIPNet, _ = net.ParseCIDR(testEniV6Subnet)
	testEniIPNet               = net.ParseIP(testEniIP)
	testEniIP6Net              = net.ParseIP(testEniIP6)
	testEniV6GatewayNet        = net.ParseIP(testEniV6Gateway)
)

func setup(t *testing.T) (*gomock.Controller,
	*mock_netlinkwrapper.MockNetLink,
	*mocks_ip.MockIP,
	*mock_nswrapper.MockNS,
	iptableswrapper.IPTablesIface) {
	ctrl := gomock.NewController(t)
	return ctrl,
		mock_netlinkwrapper.NewMockNetLink(ctrl),
		mocks_ip.NewMockIP(ctrl),
		mock_nswrapper.NewMockNS(ctrl),
		mock_iptables.NewMockIptables()
}

func TestSetupENINetwork(t *testing.T) {
	ctrl, mockNetLink, _, _, _ := setup(t)
	defer ctrl.Finish()

	hwAddr, err := net.ParseMAC(testMAC1)
	assert.NoError(t, err)
	mockLinkAttrs1 := &netlink.LinkAttrs{
		HardwareAddr: hwAddr,
	}
	hwAddr, err = net.ParseMAC(testMAC2)
	assert.NoError(t, err)
	mockLinkAttrs2 := &netlink.LinkAttrs{
		HardwareAddr: hwAddr,
	}
	lo := mock_netlink.NewMockLink(ctrl)
	eth1 := mock_netlink.NewMockLink(ctrl)
	// Emulate a delay attaching the ENI so a retry is necessary
	// First attempt gets one links
	firstlistSet := mockNetLink.EXPECT().LinkList().Return([]netlink.Link{lo}, nil)
	lo.EXPECT().Attrs().Return(mockLinkAttrs1)
	// Second attempt gets both links
	secondlistSet := mockNetLink.EXPECT().LinkList().Return([]netlink.Link{lo, eth1}, nil)
	lo.EXPECT().Attrs().Return(mockLinkAttrs1)
	eth1.EXPECT().Attrs().Return(mockLinkAttrs2)
	gomock.InOrder(firstlistSet, secondlistSet)
	mockNetLink.EXPECT().LinkSetMTU(gomock.Any(), testMTU).Return(nil)
	mockNetLink.EXPECT().LinkSetUp(gomock.Any()).Return(nil)
	// eth1's device
	eth1.EXPECT().Attrs().Return(mockLinkAttrs2)
	eth1.EXPECT().Attrs().Return(mockLinkAttrs2)
	// eth1's IP address
	testEniAddr := &net.IPNet{
		IP:   net.ParseIP(testEniIP),
		Mask: testEniSubnetIPNet.Mask,
	}
	mockNetLink.EXPECT().AddrList(gomock.Any(), unix.AF_INET).Return([]netlink.Addr{}, nil)
	mockNetLink.EXPECT().AddrAdd(gomock.Any(), &netlink.Addr{IPNet: testEniAddr}).Return(nil)

	mockNetLink.EXPECT().RouteDel(gomock.Any())
	mockNetLink.EXPECT().RouteReplace(gomock.Any()).Return(nil)

	mockNetLink.EXPECT().RouteDel(gomock.Any())
	mockNetLink.EXPECT().RouteReplace(gomock.Any()).Return(nil)

	mockNetLink.EXPECT().RouteDel(gomock.Any()).Return(nil)

	err = setupENINetwork(testEniIP, testMAC2, testTable, testEniSubnet, mockNetLink, 0*time.Second, 0*time.Second, testMTU)
	assert.NoError(t, err)
}

func TestSetupENIV6Network(t *testing.T) {
	ctrl, mockNetLink, _, _, _ := setup(t)
	defer ctrl.Finish()

	hwAddr, err := net.ParseMAC(testMAC1)
	assert.NoError(t, err)
	mockLinkAttrs1 := &netlink.LinkAttrs{
		HardwareAddr: hwAddr,
	}
	hwAddr, err = net.ParseMAC(testMAC2)
	assert.NoError(t, err)
	mockLinkAttrs2 := &netlink.LinkAttrs{
		HardwareAddr: hwAddr,
	}
	lo := mock_netlink.NewMockLink(ctrl)
	eth1 := mock_netlink.NewMockLink(ctrl)
	// Emulate a delay attaching the ENI so a retry is necessary
	// First attempt gets one links
	firstlistSet := mockNetLink.EXPECT().LinkList().Return([]netlink.Link{lo}, nil)
	lo.EXPECT().Attrs().Return(mockLinkAttrs1)
	// Second attempt gets both links
	secondlistSet := mockNetLink.EXPECT().LinkList().Return([]netlink.Link{lo, eth1}, nil)
	lo.EXPECT().Attrs().Return(mockLinkAttrs1)
	eth1.EXPECT().Attrs().Return(mockLinkAttrs2)
	gomock.InOrder(firstlistSet, secondlistSet)
	mockNetLink.EXPECT().LinkSetMTU(gomock.Any(), testMTU).Return(nil)
	mockNetLink.EXPECT().LinkSetUp(gomock.Any()).Return(nil)
	// eth1's device
	eth1.EXPECT().Attrs().Return(mockLinkAttrs2)
	eth1.EXPECT().Attrs().Return(mockLinkAttrs2)
	// eth1's IP address
	testEniAddr := &net.IPNet{
		IP:   net.ParseIP(testEniIP6),
		Mask: testEniV6SubnetIPNet.Mask,
	}
	mockNetLink.EXPECT().AddrList(gomock.Any(), unix.AF_INET6).Return([]netlink.Addr{}, nil)
	mockNetLink.EXPECT().AddrAdd(gomock.Any(), &netlink.Addr{IPNet: testEniAddr}).Return(nil)

	mockNetLink.EXPECT().RouteDel(gomock.Any())
	mockNetLink.EXPECT().RouteReplace(gomock.Any()).Return(nil)

	mockNetLink.EXPECT().RouteDel(gomock.Any())
	mockNetLink.EXPECT().RouteReplace(gomock.Any()).Return(nil)
	mockNetLink.EXPECT().RouteDel(gomock.Any()).Return(nil)

	err = setupENINetwork(testEniIP6, testMAC2, testTable, testEniV6Subnet, mockNetLink, 0*time.Second, 0*time.Second, testMTU)
	assert.NoError(t, err)
}

func TestSetupENINetworkMACFail(t *testing.T) {
	ctrl, mockNetLink, _, _, _ := setup(t)
	defer ctrl.Finish()

	// Emulate a delay attaching the ENI so a retry is necessary
	// First attempt gets one links
	for i := 0; i < maxAttemptsLinkByMac; i++ {
		mockNetLink.EXPECT().LinkList().Return(nil, fmt.Errorf("simulated failure"))
	}

	err := setupENINetwork(testEniIP, testMAC2, testTable, testEniSubnet, mockNetLink, 0*time.Second, 0*time.Second, testMTU)
	assert.Errorf(t, err, "simulated failure")
}

func TestSetupENINetworkErrorOnPrimaryENI(t *testing.T) {
	ctrl, mockNetLink, _, _, _ := setup(t)
	defer ctrl.Finish()
	deviceNumber := 0
	err := setupENINetwork(testEniIP, testMAC2, deviceNumber, testEniSubnet, mockNetLink, 0*time.Second, 0*time.Second, testMTU)
	assert.Error(t, err)
}

func TestUpdateIPv6GatewayRule(t *testing.T) {
	ctrl, mockNetLink, _, _, _ := setup(t)
	defer ctrl.Finish()

	ln := &linuxNetwork{
		netLink:            mockNetLink,
		podSGEnforcingMode: sgpp.EnforcingModeStrict,
	}

	icmpRule := netlink.Rule{
		Src:      &net.IPNet{IP: testEniV6GatewayNet, Mask: net.CIDRMask(128, 128)},
		IPProto:  unix.IPPROTO_ICMPV6,
		Table:    localRouteTable,
		Priority: 0,
		Family:   unix.AF_INET6,
	}

	// Validate rule add in strict mode
	mockNetLink.EXPECT().NewRule().Return(&icmpRule)
	mockNetLink.EXPECT().RuleAdd(&icmpRule)

	err := ln.createIPv6GatewayRule()
	assert.NoError(t, err)

	// Validate rule del in non-strict mode
	ln.podSGEnforcingMode = sgpp.EnforcingModeStandard
	mockNetLink.EXPECT().NewRule().Return(&icmpRule)
	mockNetLink.EXPECT().RuleDel(&icmpRule)

	err = ln.createIPv6GatewayRule()
	assert.NoError(t, err)
}

func TestSetupHostNetworkNodePortDisabledAndSNATDisabled(t *testing.T) {
	ctrl, mockNetLink, _, mockNS, mockIptables := setup(t)
	defer ctrl.Finish()

	ln := &linuxNetwork{
		useExternalSNAT:        true,
		nodePortSupportEnabled: false,
		mainENIMark:            defaultConnmark,
		mtu:                    testMTU,
		netLink:                mockNetLink,
		ns:                     mockNS,
		newIptables: func(iptables.Protocol) (iptableswrapper.IPTablesIface, error) {
			return mockIptables, nil
		},
	}
	mockPrimaryInterfaceLookup(ctrl, mockNetLink)

	mockNetLink.EXPECT().LinkSetMTU(gomock.Any(), testMTU).Return(nil)
	var mainENIRule netlink.Rule
	mockNetLink.EXPECT().NewRule().Return(&mainENIRule)
	mockNetLink.EXPECT().RuleDel(&mainENIRule)

	var vpcCIDRs []string
	err := ln.SetupHostNetwork(vpcCIDRs, loopback, &testEniIPNet, false, true, false)
	assert.NoError(t, err)
}

func mockPrimaryInterfaceLookup(ctrl *gomock.Controller, mockNetLink *mock_netlinkwrapper.MockNetLink) {
	lo := mock_netlink.NewMockLink(ctrl)
	mockLinkAttrs1 := &netlink.LinkAttrs{
		HardwareAddr: net.HardwareAddr{},
	}
	mockNetLink.EXPECT().LinkList().Return([]netlink.Link{lo}, nil)
	lo.EXPECT().Attrs().AnyTimes().Return(mockLinkAttrs1)
}

func TestUpdateRuleListBySrc(t *testing.T) {
	ctrl, mockNetLink, _, _, _ := setup(t)
	defer ctrl.Finish()

	ln := &linuxNetwork{netLink: mockNetLink}

	origRule := netlink.Rule{
		Src:   testEniSubnetIPNet,
		Table: testTable,
	}
	testCases := []struct {
		name         string
		oldRule      netlink.Rule
		requiresSNAT bool
		ruleList     []netlink.Rule
		newRule      netlink.Rule
	}{
		{
			"multiple destinations",
			origRule,
			true,
			[]netlink.Rule{origRule},
			netlink.Rule{},
		},
		{
			"single destination",
			origRule,
			false,
			[]netlink.Rule{origRule},
			netlink.Rule{},
		},
	}

	for _, tc := range testCases {
		mockNetLink.EXPECT().RuleDel(&tc.oldRule)
		mockNetLink.EXPECT().NewRule().Return(&tc.newRule)
		mockNetLink.EXPECT().RuleAdd(&tc.newRule)

		err := ln.UpdateRuleListBySrc(tc.ruleList, *testEniSubnetIPNet)
		assert.NoError(t, err)
	}
}

func TestSetupHostNetworkNodePortEnabledAndSNATDisabled(t *testing.T) {
	ctrl, mockNetLink, _, mockNS, mockIptables := setup(t)
	defer ctrl.Finish()

	ln := &linuxNetwork{
		useExternalSNAT:        true,
		ipv6EgressEnabled:      true,
		nodePortSupportEnabled: true,
		mainENIMark:            defaultConnmark,
		mtu:                    testMTU,
		vethPrefix:             eniPrefix,

		netLink: mockNetLink,
		ns:      mockNS,
		newIptables: func(iptables.Protocol) (iptableswrapper.IPTablesIface, error) {
			return mockIptables, nil
		},
	}

	log.Debugf("mockIPtables.Dp state: ", mockIptables.(*mock_iptables.MockIptables).DataplaneState)
	setupNetLinkMocks(ctrl, mockNetLink)
	log.Debugf("After: mockIPtables.Dp state: ", mockIptables.(*mock_iptables.MockIptables).DataplaneState)

	var vpcCIDRs []string
	err := ln.SetupHostNetwork(vpcCIDRs, loopback, &testEniIPNet, false, true, false)
	assert.NoError(t, err)

	assert.Equal(t, map[string]map[string][][]string{
		"filter": {
			"FORWARD": [][]string{
				{
					"-d", "fd00::ac:00/118", "-m", "conntrack", "--ctstate", "NEW", "-m", "comment",
					"--comment", "Block Node Local Pod access via IPv6", "-j", "REJECT",
				},
			},
		},
		"mangle": {
			"PREROUTING": [][]string{
				{
					"-m", "comment", "--comment", "AWS, primary ENI",
					"-i", "lo",
					"-m", "addrtype", "--dst-type", "LOCAL", "--limit-iface-in",
					"-j", "CONNMARK", "--set-mark", "0x80/0x80",
				},
				{
					"-m", "comment", "--comment", "AWS, primary ENI",
					"-i", "eni+", "-j", "CONNMARK", "--restore-mark", "--mask", "0x80",
				},
				{
					"-m", "comment", "--comment", "AWS, primary ENI",
					"-i", "vlan+", "-j", "CONNMARK", "--restore-mark", "--mask", "0x80",
				},
			},
		},
		"nat": {
			"AWS-SNAT-CHAIN-0":     [][]string{{"-N", "AWS-SNAT-CHAIN-0"}},
			"AWS-CONNMARK-CHAIN-0": [][]string{{"-N", "AWS-CONNMARK-CHAIN-0"}},
		},
	}, mockIptables.(*mock_iptables.MockIptables).DataplaneState)
}

func TestSetupHostNetworkNodePortDisabledAndSNATEnabled(t *testing.T) {
	ctrl, mockNetLink, _, mockNS, mockIptables := setup(t)
	defer ctrl.Finish()

	ln := &linuxNetwork{
		useExternalSNAT:        false,
		ipv6EgressEnabled:      false,
		nodePortSupportEnabled: false,
		mainENIMark:            defaultConnmark,
		mtu:                    testMTU,
		vethPrefix:             eniPrefix,

		netLink: mockNetLink,
		ns:      mockNS,
		newIptables: func(iptables.Protocol) (iptableswrapper.IPTablesIface, error) {
			return mockIptables, nil
		},
	}

	log.Debugf("mockIPtables.Dp state: ", mockIptables.(*mock_iptables.MockIptables).DataplaneState)
	setupNetLinkMocks(ctrl, mockNetLink)
	log.Debugf("After: mockIPtables.Dp state: ", mockIptables.(*mock_iptables.MockIptables).DataplaneState)

	var vpcCIDRs []string

	err := ln.SetupHostNetwork(vpcCIDRs, loopback, &testEniIPNet, false, true, false)
	assert.NoError(t, err)

	assert.Equal(t, map[string]map[string][][]string{
		"nat": {
			"AWS-SNAT-CHAIN-0":     [][]string{{"-N", "AWS-SNAT-CHAIN-0"}, {"!", "-o", "vlan+", "-m", "comment", "--comment", "AWS, SNAT", "-m", "addrtype", "!", "--dst-type", "LOCAL", "-j", "SNAT", "--to-source", "10.10.10.20"}},
			"POSTROUTING":          [][]string{{"-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "AWS-SNAT-CHAIN-0"}},
			"AWS-CONNMARK-CHAIN-0": [][]string{{"-N", "AWS-CONNMARK-CHAIN-0"}, {"-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK", "--set-xmark", "0x80/0x80"}},
			"PREROUTING": [][]string{
				{"-i", "eni+", "-m", "comment", "--comment", "AWS, outbound connections", "-j", "AWS-CONNMARK-CHAIN-0"},
				{"-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK", "--restore-mark", "--mask", "0x80"},
			},
		},
		"mangle": {
			"PREROUTING": [][]string{
				{
					"-m", "comment", "--comment", "AWS, primary ENI",
					"-i", "eni+", "-j", "CONNMARK", "--restore-mark", "--mask", "0x80",
				},
			},
		},
	}, mockIptables.(*mock_iptables.MockIptables).DataplaneState)
}

func TestLoadMTUFromEnvTooLow(t *testing.T) {
	os.Setenv(envMTU, "1")
	assert.Equal(t, GetEthernetMTU(""), minimumMTU)
}

func TestLoadMTUFromEnv1500(t *testing.T) {
	os.Setenv(envMTU, "1500")
	assert.Equal(t, GetEthernetMTU(""), 1500)
}

func TestLoadMTUFromEnvTooHigh(t *testing.T) {
	os.Setenv(envMTU, "65536")
	assert.Equal(t, GetEthernetMTU(""), maximumMTU)
}

func TestLoadExcludeSNATCIDRsFromEnv(t *testing.T) {
	os.Setenv(envExternalSNAT, "false")
	os.Setenv(envExcludeSNATCIDRs, "10.12.0.0/16,10.13.0.0/16")

	expected := []string{"10.12.0.0/16", "10.13.0.0/16"}
	assert.Equal(t, parseCIDRString(envExcludeSNATCIDRs), expected)
}

func TestSetupHostNetworkWithExcludeSNATCIDRs(t *testing.T) {
	ctrl, mockNetLink, _, mockNS, mockIptables := setup(t)
	defer ctrl.Finish()

	ln := &linuxNetwork{
		useExternalSNAT:        false,
		ipv6EgressEnabled:      false,
		excludeSNATCIDRs:       []string{"10.12.0.0/16", "10.13.0.0/16"},
		nodePortSupportEnabled: true,
		mainENIMark:            defaultConnmark,
		mtu:                    testMTU,
		vethPrefix:             eniPrefix,

		netLink: mockNetLink,
		ns:      mockNS,
		newIptables: func(iptables.Protocol) (iptableswrapper.IPTablesIface, error) {
			return mockIptables, nil
		},
	}
	setupNetLinkMocks(ctrl, mockNetLink)

	vpcCIDRs := []string{"10.10.0.0/16", "10.11.0.0/16"}
	err := ln.SetupHostNetwork(vpcCIDRs, loopback, &testEniIPNet, false, true, false)
	assert.NoError(t, err)
	assert.Equal(t,
		map[string]map[string][][]string{
			"nat": {
				"AWS-SNAT-CHAIN-0": [][]string{
					{"-N", "AWS-SNAT-CHAIN-0"},
					{"-d", "10.13.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN EXCLUSION", "-j", "RETURN"},
					{"-d", "10.12.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN EXCLUSION", "-j", "RETURN"},
					{"-d", "10.11.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "RETURN"},
					{"-d", "10.10.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "RETURN"},
					{"!", "-o", "vlan+", "-m", "comment", "--comment", "AWS, SNAT", "-m", "addrtype", "!", "--dst-type", "LOCAL", "-j", "SNAT", "--to-source", "10.10.10.20"},
				},
				"POSTROUTING": [][]string{{"-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "AWS-SNAT-CHAIN-0"}},
				"AWS-CONNMARK-CHAIN-0": [][]string{
					{"-N", "AWS-CONNMARK-CHAIN-0"},
					{"-d", "10.13.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, EXCLUDED CIDR", "-j", "RETURN"},
					{"-d", "10.12.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, EXCLUDED CIDR", "-j", "RETURN"},
					{"-d", "10.11.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, VPC CIDR", "-j", "RETURN"},
					{"-d", "10.10.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, VPC CIDR", "-j", "RETURN"},
					{"-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK", "--set-xmark", "0x80/0x80"},
				},
				"PREROUTING": [][]string{
					{"-i", "eni+", "-m", "comment", "--comment", "AWS, outbound connections", "-j", "AWS-CONNMARK-CHAIN-0"},
					{"-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK", "--restore-mark", "--mask", "0x80"},
				},
			},
			"mangle": {
				"PREROUTING": [][]string{
					{"-m", "comment", "--comment", "AWS, primary ENI", "-i", "lo", "-m", "addrtype", "--dst-type", "LOCAL", "--limit-iface-in", "-j", "CONNMARK", "--set-mark", "0x80/0x80"},
					{"-m", "comment", "--comment", "AWS, primary ENI", "-i", "eni+", "-j", "CONNMARK", "--restore-mark", "--mask", "0x80"},
					{"-m", "comment", "--comment", "AWS, primary ENI", "-i", "vlan+", "-j", "CONNMARK", "--restore-mark", "--mask", "0x80"},
				},
			},
		}, mockIptables.(*mock_iptables.MockIptables).DataplaneState)
}

func TestSetupHostNetworkCleansUpStaleSNATRules(t *testing.T) {
	ctrl, mockNetLink, _, mockNS, mockIptables := setup(t)
	defer ctrl.Finish()

	ln := &linuxNetwork{
		useExternalSNAT:        false,
		ipv6EgressEnabled:      false,
		excludeSNATCIDRs:       nil,
		nodePortSupportEnabled: true,
		mainENIMark:            defaultConnmark,
		mtu:                    testMTU,
		vethPrefix:             eniPrefix,

		netLink: mockNetLink,
		ns:      mockNS,
		newIptables: func(iptables.Protocol) (iptableswrapper.IPTablesIface, error) {
			return mockIptables, nil
		},
	}
	setupNetLinkMocks(ctrl, mockNetLink)

	mockIptables.NewChain("nat", "AWS-SNAT-CHAIN-0")
	mockIptables.NewChain("nat", "AWS-CONNMARK-CHAIN-0")
	mockIptables.Insert("nat", "AWS-SNAT-CHAIN-0", 1, "-d", "10.10.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAN", "-j", "RETURN") //AWS SNAT CHAN proves backwards compatibility
	mockIptables.Insert("nat", "AWS-SNAT-CHAIN-0", 1, "-d", "10.11.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "RETURN")
	mockIptables.Insert("nat", "AWS-SNAT-CHAIN-0", 1, "-d", "10.12.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN EXCLUSION", "-j", "RETURN")
	mockIptables.Insert("nat", "AWS-SNAT-CHAIN-0", 1, "-d", "10.13.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN EXCLUSION", "-j", "RETURN")
	mockIptables.Append("nat", "AWS-SNAT-CHAIN-0", "-m", "comment", "--comment", "AWS, SNAT", "-m", "addrtype", "!", "--dst-type", "LOCAL", "-j", "SNAT", "--to-source", "10.10.10.20")
	mockIptables.NewChain("nat", "AWS-SNAT-CHAIN-1")
	mockIptables.Append("nat", "POSTROUTING", "-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "AWS-SNAT-CHAIN-0")
	mockIptables.Insert("nat", "AWS-CONNMARK-CHAIN-0", 1, "-d", "10.10.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, VPC CIDR", "-j", "RETURN")
	mockIptables.Insert("nat", "AWS-CONNMARK-CHAIN-0", 1, "-d", "10.11.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, VPC CIDR", "-j", "RETURN")
	mockIptables.Insert("nat", "AWS-CONNMARK-CHAIN-0", 1, "-d", "10.12.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, EXCLUDED CIDR", "-j", "RETURN")
	mockIptables.Insert("nat", "AWS-CONNMARK-CHAIN-0", 1, "-d", "10.13.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, EXCLUDED CIDR", "-j", "RETURN")
	mockIptables.Append("nat", "AWS-CONNMARK-CHAIN-0", "-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK", "--set-xmark", "0x80/0x80")
	mockIptables.Append("nat", "PREROUTING", "-i", "eni+", "-m", "comment", "--comment", "AWS, outbound connections", "-j", "AWS-CONNMARK-CHAIN-0")
	mockIptables.Append("nat", "PREROUTING", "-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK", "--restore-mark", "--mask", "0x80")

	vpcCIDRs := []string{"10.10.0.0/16", "10.11.0.0/16"}
	err := ln.SetupHostNetwork(vpcCIDRs, loopback, &testEniIPNet, false, true, false)
	assert.NoError(t, err)

	assert.Equal(t,
		map[string]map[string][][]string{
			"nat": {
				"AWS-SNAT-CHAIN-0": [][]string{
					{"-N", "AWS-SNAT-CHAIN-0"},
					{"-d", "10.10.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "RETURN"},
					{"-d", "10.11.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "RETURN"},
					{"!", "-o", "vlan+", "-m", "comment", "--comment", "AWS, SNAT", "-m", "addrtype", "!", "--dst-type", "LOCAL", "-j", "SNAT", "--to-source", "10.10.10.20"},
				},
				"AWS-SNAT-CHAIN-1": [][]string{
					{"-N", "AWS-SNAT-CHAIN-1"},
				},
				"POSTROUTING": [][]string{{"-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "AWS-SNAT-CHAIN-0"}},
				"AWS-CONNMARK-CHAIN-0": [][]string{
					{"-N", "AWS-CONNMARK-CHAIN-0"},
					{"-d", "10.11.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, VPC CIDR", "-j", "RETURN"},
					{"-d", "10.10.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, VPC CIDR", "-j", "RETURN"},
					{"-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK", "--set-xmark", "0x80/0x80"},
				},
				"PREROUTING": [][]string{
					{"-i", "eni+", "-m", "comment", "--comment", "AWS, outbound connections", "-j", "AWS-CONNMARK-CHAIN-0"},
					{"-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK", "--restore-mark", "--mask", "0x80"},
				},
			},
			"mangle": {
				"PREROUTING": [][]string{
					{"-m", "comment", "--comment", "AWS, primary ENI", "-i", "lo", "-m", "addrtype", "--dst-type", "LOCAL", "--limit-iface-in", "-j", "CONNMARK", "--set-mark", "0x80/0x80"},
					{"-m", "comment", "--comment", "AWS, primary ENI", "-i", "eni+", "-j", "CONNMARK", "--restore-mark", "--mask", "0x80"},
					{"-m", "comment", "--comment", "AWS, primary ENI", "-i", "vlan+", "-j", "CONNMARK", "--restore-mark", "--mask", "0x80"},
				},
			},
		}, mockIptables.(*mock_iptables.MockIptables).DataplaneState)
}

func TestSetupHostNetworkWithDifferentVethPrefix(t *testing.T) {
	ctrl, mockNetLink, _, mockNS, mockIptables := setup(t)
	defer ctrl.Finish()

	ln := &linuxNetwork{
		useExternalSNAT:        false,
		ipv6EgressEnabled:      false,
		excludeSNATCIDRs:       []string{"10.12.0.0/16", "10.13.0.0/16"},
		nodePortSupportEnabled: true,
		mainENIMark:            defaultConnmark,
		mtu:                    testMTU,
		vethPrefix:             "veth",

		netLink: mockNetLink,
		ns:      mockNS,
		newIptables: func(iptables.Protocol) (iptableswrapper.IPTablesIface, error) {
			return mockIptables, nil
		},
	}
	setupNetLinkMocks(ctrl, mockNetLink)

	mockIptables.NewChain("nat", "AWS-SNAT-CHAIN-0")
	mockIptables.NewChain("nat", "AWS-CONNMARK-CHAIN-0")
	mockIptables.Insert("nat", "AWS-SNAT-CHAIN-0", 1, "-d", "10.10.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAN", "-j", "RETURN") //AWS SNAT CHAN proves backwards compatibility
	mockIptables.Insert("nat", "AWS-SNAT-CHAIN-0", 1, "-d", "10.11.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "RETURN")
	mockIptables.Insert("nat", "AWS-SNAT-CHAIN-0", 1, "-d", "10.12.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN EXCLUSION", "-j", "RETURN")
	mockIptables.Insert("nat", "AWS-SNAT-CHAIN-0", 1, "-d", "10.13.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN EXCLUSION", "-j", "RETURN")
	mockIptables.Append("nat", "AWS-SNAT-CHAIN-0", "-m", "comment", "--comment", "AWS, SNAT", "-m", "addrtype", "!", "--dst-type", "LOCAL", "-j", "SNAT", "--to-source", "10.10.10.20")
	mockIptables.NewChain("nat", "AWS-SNAT-CHAIN-1")
	mockIptables.Append("nat", "POSTROUTING", "-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "AWS-SNAT-CHAIN-0")
	mockIptables.Insert("nat", "AWS-CONNMARK-CHAIN-0", 1, "-d", "10.10.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, VPC CIDR", "-j", "RETURN")
	mockIptables.Insert("nat", "AWS-CONNMARK-CHAIN-0", 1, "-d", "10.11.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, VPC CIDR", "-j", "RETURN")
	mockIptables.Insert("nat", "AWS-CONNMARK-CHAIN-0", 1, "-d", "10.12.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, EXCLUDED CIDR", "-j", "RETURN")
	mockIptables.Insert("nat", "AWS-CONNMARK-CHAIN-0", 1, "-d", "10.13.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, EXCLUDED CIDR", "-j", "RETURN")
	mockIptables.Append("nat", "AWS-CONNMARK-CHAIN-0", "-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK", "--set-xmark", "0x80/0x80")
	mockIptables.Append("nat", "PREROUTING", "-i", "eni+", "-m", "comment", "--comment", "AWS, outbound connections", "-j", "AWS-CONNMARK-CHAIN-0")
	mockIptables.Append("nat", "PREROUTING", "-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK", "--restore-mark", "--mask", "0x80")

	vpcCIDRs := []string{"10.10.0.0/16", "10.11.0.0/16"}
	err := ln.SetupHostNetwork(vpcCIDRs, loopback, &testEniIPNet, false, true, false)
	assert.NoError(t, err)
	assert.Equal(t,
		map[string]map[string][][]string{
			"nat": {
				"AWS-SNAT-CHAIN-0": [][]string{
					{"-N", "AWS-SNAT-CHAIN-0"},
					{"-d", "10.10.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "RETURN"},
					{"-d", "10.13.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN EXCLUSION", "-j", "RETURN"},
					{"-d", "10.12.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN EXCLUSION", "-j", "RETURN"},
					{"-d", "10.11.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "RETURN"},
					{"!", "-o", "vlan+", "-m", "comment", "--comment", "AWS, SNAT", "-m", "addrtype", "!", "--dst-type", "LOCAL", "-j", "SNAT", "--to-source", "10.10.10.20"},
				},
				"AWS-SNAT-CHAIN-1": [][]string{
					{"-N", "AWS-SNAT-CHAIN-1"},
				},
				"POSTROUTING": [][]string{{"-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "AWS-SNAT-CHAIN-0"}},
				"AWS-CONNMARK-CHAIN-0": [][]string{
					{"-N", "AWS-CONNMARK-CHAIN-0"},
					{"-d", "10.13.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, EXCLUDED CIDR", "-j", "RETURN"},
					{"-d", "10.12.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, EXCLUDED CIDR", "-j", "RETURN"},
					{"-d", "10.11.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, VPC CIDR", "-j", "RETURN"},
					{"-d", "10.10.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, VPC CIDR", "-j", "RETURN"},
					{"-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK", "--set-xmark", "0x80/0x80"},
				},
				"PREROUTING": [][]string{
					{"-i", "eni+", "-m", "comment", "--comment", "AWS, outbound connections", "-j", "AWS-CONNMARK-CHAIN-0"},
					{"-i", "veth+", "-m", "comment", "--comment", "AWS, outbound connections", "-j", "AWS-CONNMARK-CHAIN-0"},
					{"-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK", "--restore-mark", "--mask", "0x80"},
				},
			},
			"mangle": {
				"PREROUTING": [][]string{
					{"-m", "comment", "--comment", "AWS, primary ENI", "-i", "lo", "-m", "addrtype", "--dst-type", "LOCAL", "--limit-iface-in", "-j", "CONNMARK", "--set-mark", "0x80/0x80"},
					{"-m", "comment", "--comment", "AWS, primary ENI", "-i", "veth+", "-j", "CONNMARK", "--restore-mark", "--mask", "0x80"},
					{"-m", "comment", "--comment", "AWS, primary ENI", "-i", "vlan+", "-j", "CONNMARK", "--restore-mark", "--mask", "0x80"},
				},
			},
		}, mockIptables.(*mock_iptables.MockIptables).DataplaneState)
}
func TestSetupHostNetworkExternalNATCleanupConnmark(t *testing.T) {
	ctrl, mockNetLink, _, mockNS, mockIptables := setup(t)
	defer ctrl.Finish()

	ln := &linuxNetwork{
		useExternalSNAT:        true,
		ipv6EgressEnabled:      false,
		excludeSNATCIDRs:       []string{"10.12.0.0/16", "10.13.0.0/16"},
		nodePortSupportEnabled: true,
		mainENIMark:            defaultConnmark,
		mtu:                    testMTU,
		vethPrefix:             eniPrefix,

		netLink: mockNetLink,
		ns:      mockNS,
		newIptables: func(iptables.Protocol) (iptableswrapper.IPTablesIface, error) {
			return mockIptables, nil
		},
	}
	setupNetLinkMocks(ctrl, mockNetLink)

	mockIptables.NewChain("nat", "AWS-SNAT-CHAIN-0")
	mockIptables.NewChain("nat", "AWS-CONNMARK-CHAIN-0")
	mockIptables.Insert("nat", "AWS-SNAT-CHAIN-0", 1, "-d", "10.10.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "RETURN")
	mockIptables.Insert("nat", "AWS-SNAT-CHAIN-0", 1, "-d", "10.11.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "RETURN")
	mockIptables.Insert("nat", "AWS-SNAT-CHAIN-0", 1, "-d", "10.12.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN EXCLUSION", "-j", "RETURN")
	mockIptables.Insert("nat", "AWS-SNAT-CHAIN-0", 1, "-d", "10.13.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN EXCLUSION", "-j", "RETURN")
	mockIptables.Append("nat", "AWS-SNAT-CHAIN-0", "-m", "comment", "--comment", "AWS, SNAT", "-m", "addrtype", "!", "--dst-type", "LOCAL", "-j", "SNAT", "--to-source", "10.10.10.20")
	mockIptables.Append("nat", "POSTROUTING", "-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "AWS-SNAT-CHAIN-0")
	mockIptables.Insert("nat", "AWS-CONNMARK-CHAIN-0", 1, "-d", "10.10.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, VPC CIDR", "-j", "RETURN")
	mockIptables.Insert("nat", "AWS-CONNMARK-CHAIN-0", 1, "-d", "10.11.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, VPC CIDR", "-j", "RETURN")
	mockIptables.Insert("nat", "AWS-CONNMARK-CHAIN-0", 1, "-d", "10.12.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, EXCLUDED CIDR", "-j", "RETURN")
	mockIptables.Insert("nat", "AWS-CONNMARK-CHAIN-0", 1, "-d", "10.13.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, EXCLUDED CIDR", "-j", "RETURN")
	mockIptables.Append("nat", "AWS-CONNMARK-CHAIN-0", "-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK", "--set-xmark", "0x80/0x80")
	mockIptables.Append("nat", "PREROUTING", "-i", "eni+", "-m", "comment", "--comment", "AWS, outbound connections", "-j", "AWS-CONNMARK-CHAIN-0")
	mockIptables.Append("nat", "PREROUTING", "-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK", "--restore-mark", "--mask", "0x80")

	// remove exclusions
	vpcCIDRs := []string{"10.10.0.0/16", "10.11.0.0/16"}
	err := ln.SetupHostNetwork(vpcCIDRs, loopback, &testEniIPNet, false, true, false)
	assert.NoError(t, err)

	assert.Equal(t,
		map[string]map[string][][]string{
			"nat": {
				"AWS-SNAT-CHAIN-0":     [][]string{{"-N", "AWS-SNAT-CHAIN-0"}},
				"POSTROUTING":          [][]string{},
				"AWS-CONNMARK-CHAIN-0": [][]string{{"-N", "AWS-CONNMARK-CHAIN-0"}},
				"PREROUTING":           [][]string{},
			},
			"mangle": {
				"PREROUTING": [][]string{
					{"-m", "comment", "--comment", "AWS, primary ENI", "-i", "lo", "-m", "addrtype", "--dst-type", "LOCAL", "--limit-iface-in", "-j", "CONNMARK", "--set-mark", "0x80/0x80"},
					{"-m", "comment", "--comment", "AWS, primary ENI", "-i", "eni+", "-j", "CONNMARK", "--restore-mark", "--mask", "0x80"},
					{"-m", "comment", "--comment", "AWS, primary ENI", "-i", "vlan+", "-j", "CONNMARK", "--restore-mark", "--mask", "0x80"},
				},
			},
		}, mockIptables.(*mock_iptables.MockIptables).DataplaneState)
}
func TestSetupHostNetworkExcludedSNATCIDRsIdempotent(t *testing.T) {
	ctrl, mockNetLink, _, mockNS, mockIptables := setup(t)
	defer ctrl.Finish()

	ln := &linuxNetwork{
		useExternalSNAT:        false,
		ipv6EgressEnabled:      false,
		excludeSNATCIDRs:       []string{"10.12.0.0/16", "10.13.0.0/16"},
		nodePortSupportEnabled: true,
		mainENIMark:            defaultConnmark,
		mtu:                    testMTU,
		vethPrefix:             eniPrefix,

		netLink: mockNetLink,
		ns:      mockNS,
		newIptables: func(iptables.Protocol) (iptableswrapper.IPTablesIface, error) {
			return mockIptables, nil
		},
	}
	setupNetLinkMocks(ctrl, mockNetLink)

	mockIptables.NewChain("nat", "AWS-SNAT-CHAIN-0")
	mockIptables.NewChain("nat", "AWS-CONNMARK-CHAIN-0")
	mockIptables.Insert("nat", "AWS-SNAT-CHAIN-0", 1, "-d", "10.10.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "RETURN")
	mockIptables.Insert("nat", "AWS-SNAT-CHAIN-0", 1, "-d", "10.11.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "RETURN")
	mockIptables.Insert("nat", "AWS-SNAT-CHAIN-0", 1, "-d", "10.12.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN EXCLUSION", "-j", "RETURN")
	mockIptables.Insert("nat", "AWS-SNAT-CHAIN-0", 1, "-d", "10.13.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN EXCLUSION", "-j", "RETURN")
	mockIptables.Append("nat", "AWS-SNAT-CHAIN-0", "-m", "comment", "--comment", "AWS, SNAT", "-m", "addrtype", "!", "--dst-type", "LOCAL", "-j", "SNAT", "--to-source", "10.10.10.20")
	mockIptables.Append("nat", "POSTROUTING", "-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "AWS-SNAT-CHAIN-0")
	mockIptables.Insert("nat", "AWS-CONNMARK-CHAIN-0", 1, "-d", "10.10.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, VPC CIDR", "-j", "RETURN")
	mockIptables.Insert("nat", "AWS-CONNMARK-CHAIN-0", 1, "-d", "10.11.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, VPC CIDR", "-j", "RETURN")
	mockIptables.Insert("nat", "AWS-CONNMARK-CHAIN-0", 1, "-d", "10.12.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, EXCLUDED CIDR", "-j", "RETURN")
	mockIptables.Insert("nat", "AWS-CONNMARK-CHAIN-0", 1, "-d", "10.13.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, EXCLUDED CIDR", "-j", "RETURN")
	mockIptables.Append("nat", "AWS-CONNMARK-CHAIN-0", "-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK", "--set-xmark", "0x80/0x80")
	mockIptables.Append("nat", "PREROUTING", "-i", "eni+", "-m", "comment", "--comment", "AWS, outbound connections", "-j", "AWS-CONNMARK-CHAIN-0")
	mockIptables.Append("nat", "PREROUTING", "-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK", "--restore-mark", "--mask", "0x80")

	// remove exclusions
	vpcCIDRs := []string{"10.10.0.0/16", "10.11.0.0/16"}
	err := ln.SetupHostNetwork(vpcCIDRs, loopback, &testEniIPNet, false, true, false)
	assert.NoError(t, err)

	assert.Equal(t,
		map[string]map[string][][]string{
			"nat": {
				"AWS-SNAT-CHAIN-0": [][]string{
					{"-N", "AWS-SNAT-CHAIN-0"},
					{"-d", "10.13.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN EXCLUSION", "-j", "RETURN"},
					{"-d", "10.12.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN EXCLUSION", "-j", "RETURN"},
					{"-d", "10.11.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "RETURN"},
					{"-d", "10.10.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "RETURN"},
					{"!", "-o", "vlan+", "-m", "comment", "--comment", "AWS, SNAT", "-m", "addrtype", "!", "--dst-type", "LOCAL", "-j", "SNAT", "--to-source", "10.10.10.20"},
				},
				"POSTROUTING": [][]string{{"-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "AWS-SNAT-CHAIN-0"}},
				"AWS-CONNMARK-CHAIN-0": [][]string{
					{"-N", "AWS-CONNMARK-CHAIN-0"},
					{"-d", "10.13.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, EXCLUDED CIDR", "-j", "RETURN"},
					{"-d", "10.12.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, EXCLUDED CIDR", "-j", "RETURN"},
					{"-d", "10.11.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, VPC CIDR", "-j", "RETURN"},
					{"-d", "10.10.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, VPC CIDR", "-j", "RETURN"},
					{"-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK", "--set-xmark", "0x80/0x80"},
				},
				"PREROUTING": [][]string{
					{"-i", "eni+", "-m", "comment", "--comment", "AWS, outbound connections", "-j", "AWS-CONNMARK-CHAIN-0"},
					{"-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK", "--restore-mark", "--mask", "0x80"},
				},
			},
			"mangle": {
				"PREROUTING": [][]string{
					{"-m", "comment", "--comment", "AWS, primary ENI", "-i", "lo", "-m", "addrtype", "--dst-type", "LOCAL", "--limit-iface-in", "-j", "CONNMARK", "--set-mark", "0x80/0x80"},
					{"-m", "comment", "--comment", "AWS, primary ENI", "-i", "eni+", "-j", "CONNMARK", "--restore-mark", "--mask", "0x80"},
					{"-m", "comment", "--comment", "AWS, primary ENI", "-i", "vlan+", "-j", "CONNMARK", "--restore-mark", "--mask", "0x80"},
				},
			},
		}, mockIptables.(*mock_iptables.MockIptables).DataplaneState)
}

func TestUpdateHostIptablesRules(t *testing.T) {
	ctrl, mockNetLink, _, mockNS, mockIptables := setup(t)
	defer ctrl.Finish()

	ln := &linuxNetwork{
		useExternalSNAT:        false,
		ipv6EgressEnabled:      true,
		nodePortSupportEnabled: true,
		mainENIMark:            defaultConnmark,
		mtu:                    testMTU,
		vethPrefix:             "veth",

		netLink: mockNetLink,
		ns:      mockNS,
		newIptables: func(iptables.Protocol) (iptableswrapper.IPTablesIface, error) {
			return mockIptables, nil
		},
	}
	setupNetLinkMocks(ctrl, mockNetLink)

	mockIptables.NewChain("nat", "AWS-SNAT-CHAIN-0")
	mockIptables.NewChain("nat", "AWS-CONNMARK-CHAIN-0")
	mockIptables.Insert("nat", "AWS-SNAT-CHAIN-0", 1, "-d", "10.10.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAN", "-j", "RETURN") //AWS SNAT CHAN proves backwards compatibility
	mockIptables.Append("nat", "AWS-SNAT-CHAIN-0", "-m", "comment", "--comment", "AWS, SNAT", "-m", "addrtype", "!", "--dst-type", "LOCAL", "-j", "SNAT", "--to-source", "10.10.10.20")
	mockIptables.Append("nat", "POSTROUTING", "-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "AWS-SNAT-CHAIN-0")
	mockIptables.Insert("nat", "AWS-CONNMARK-CHAIN-0", 1, "-d", "10.10.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, VPC CIDR", "-j", "RETURN")
	mockIptables.Append("nat", "AWS-CONNMARK-CHAIN-0", "-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK", "--set-xmark", "0x80/0x80")
	mockIptables.Append("nat", "PREROUTING", "-i", "eni+", "-m", "comment", "--comment", "AWS, outbound connections", "-j", "AWS-CONNMARK-CHAIN-0")
	mockIptables.Append("nat", "PREROUTING", "-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK", "--restore-mark", "--mask", "0x80")
	mockIptables.Append("mangle", "PREROUTING", "-m", "comment", "--comment", "AWS, primary ENI", "-i", "lo", "-m", "addrtype", "--dst-type", "LOCAL", "--limit-iface-in", "-j", "CONNMARK", "--set-mark", "0x80/0x80")
	mockIptables.Append("mangle", "PREROUTING", "-m", "comment", "--comment", "AWS, primary ENI", "-i", "eni+", "-j", "CONNMARK", "--restore-mark", "--mask", "0x80")
	mockIptables.Append("mangle", "PREROUTING", "-m", "comment", "--comment", "AWS, primary ENI", "-i", "vlan+", "-j", "CONNMARK", "--restore-mark", "--mask", "0x80")

	vpcCIDRs := []string{"10.10.0.0/16", "10.11.0.0/16"}
	err := ln.SetupHostNetwork(vpcCIDRs, loopback, &testEniIPNet, false, true, false)
	assert.NoError(t, err)
	assert.Equal(t,
		map[string]map[string][][]string{
			"nat": {
				"AWS-SNAT-CHAIN-0": [][]string{
					{"-N", "AWS-SNAT-CHAIN-0"},
					{"-d", "10.11.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "RETURN"},
					{"-d", "10.10.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "RETURN"},
					{"!", "-o", "vlan+", "-m", "comment", "--comment", "AWS, SNAT", "-m", "addrtype", "!", "--dst-type", "LOCAL", "-j", "SNAT", "--to-source", "10.10.10.20"},
				},
				"POSTROUTING": [][]string{{"-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "AWS-SNAT-CHAIN-0"}},
				"AWS-CONNMARK-CHAIN-0": [][]string{
					{"-N", "AWS-CONNMARK-CHAIN-0"},
					{"-d", "10.11.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, VPC CIDR", "-j", "RETURN"},
					{"-d", "10.10.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, VPC CIDR", "-j", "RETURN"},
					{"-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK", "--set-xmark", "0x80/0x80"},
				},
				"PREROUTING": [][]string{
					{"-i", "eni+", "-m", "comment", "--comment", "AWS, outbound connections", "-j", "AWS-CONNMARK-CHAIN-0"},
					{"-i", "veth+", "-m", "comment", "--comment", "AWS, outbound connections", "-j", "AWS-CONNMARK-CHAIN-0"},
					{"-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK", "--restore-mark", "--mask", "0x80"},
				},
			},
			"filter": {
				"FORWARD": [][]string{
					{
						"-d", "fd00::ac:00/118", "-m", "conntrack", "--ctstate", "NEW", "-m", "comment",
						"--comment", "Block Node Local Pod access via IPv6", "-j", "REJECT",
					},
				},
			},
			"mangle": {
				"PREROUTING": [][]string{
					{"-m", "comment", "--comment", "AWS, primary ENI", "-i", "lo", "-m", "addrtype", "--dst-type", "LOCAL", "--limit-iface-in", "-j", "CONNMARK", "--set-mark", "0x80/0x80"},
					{"-m", "comment", "--comment", "AWS, primary ENI", "-i", "eni+", "-j", "CONNMARK", "--restore-mark", "--mask", "0x80"},
					{"-m", "comment", "--comment", "AWS, primary ENI", "-i", "vlan+", "-j", "CONNMARK", "--restore-mark", "--mask", "0x80"},
					{"-m", "comment", "--comment", "AWS, primary ENI", "-i", "veth+", "-j", "CONNMARK", "--restore-mark", "--mask", "0x80"},
				},
			},
		}, mockIptables.(*mock_iptables.MockIptables).DataplaneState)
}

func TestCleanUpStaleAWSChains(t *testing.T) {
	ctrl, mockNetLink, _, mockNS, mockIptables := setup(t)
	defer ctrl.Finish()

	ln := &linuxNetwork{
		useExternalSNAT:        false,
		ipv6EgressEnabled:      true,
		nodePortSupportEnabled: true,
		mainENIMark:            defaultConnmark,
		mtu:                    testMTU,
		vethPrefix:             eniPrefix,

		netLink: mockNetLink,
		ns:      mockNS,
		newIptables: func(iptables.Protocol) (iptableswrapper.IPTablesIface, error) {
			return mockIptables, nil
		},
	}
	setupNetLinkMocks(ctrl, mockNetLink)

	mockIptables.NewChain("nat", "AWS-SNAT-CHAIN-1")
	mockIptables.NewChain("nat", "AWS-SNAT-CHAIN-2")
	mockIptables.NewChain("nat", "AWS-SNAT-CHAIN-3")
	mockIptables.NewChain("nat", "AWS-SNAT-CHAIN-4")
	mockIptables.NewChain("nat", "AWS-CONNMARK-CHAIN-1")
	mockIptables.NewChain("nat", "AWS-CONNMARK-CHAIN-2")
	mockIptables.NewChain("nat", "AWS-CONNMARK-CHAIN-3")
	mockIptables.NewChain("nat", "AWS-CONNMARK-CHAIN-4")

	vpcCIDRs := []string{"10.10.0.0/16", "10.11.0.0/16"}
	err := ln.SetupHostNetwork(vpcCIDRs, loopback, &testEniIPNet, false, true, false)
	assert.NoError(t, err)

	err = ln.CleanUpStaleAWSChains(true, false)
	assert.NoError(t, err)

	assert.Equal(t,
		map[string]map[string][][]string{
			"nat": {
				"AWS-SNAT-CHAIN-0": [][]string{
					{"-N", "AWS-SNAT-CHAIN-0"},
					{"-d", "10.11.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "RETURN"},
					{"-d", "10.10.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "RETURN"},
					{"!", "-o", "vlan+", "-m", "comment", "--comment", "AWS, SNAT", "-m", "addrtype", "!", "--dst-type", "LOCAL", "-j", "SNAT", "--to-source", "10.10.10.20"},
				},
				"POSTROUTING": [][]string{{"-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "AWS-SNAT-CHAIN-0"}},
				"AWS-CONNMARK-CHAIN-0": [][]string{
					{"-N", "AWS-CONNMARK-CHAIN-0"},
					{"-d", "10.11.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, VPC CIDR", "-j", "RETURN"},
					{"-d", "10.10.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, VPC CIDR", "-j", "RETURN"},
					{"-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK", "--set-xmark", "0x80/0x80"},
				},
				"PREROUTING": [][]string{
					{"-i", "eni+", "-m", "comment", "--comment", "AWS, outbound connections", "-j", "AWS-CONNMARK-CHAIN-0"},
					{"-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK", "--restore-mark", "--mask", "0x80"},
				},
			},
			"filter": {
				"FORWARD": [][]string{
					{
						"-d", "fd00::ac:00/118", "-m", "conntrack", "--ctstate", "NEW", "-m", "comment",
						"--comment", "Block Node Local Pod access via IPv6", "-j", "REJECT",
					},
				},
			},
			"mangle": {
				"PREROUTING": [][]string{
					{"-m", "comment", "--comment", "AWS, primary ENI", "-i", "lo", "-m", "addrtype", "--dst-type", "LOCAL", "--limit-iface-in", "-j", "CONNMARK", "--set-mark", "0x80/0x80"},
					{"-m", "comment", "--comment", "AWS, primary ENI", "-i", "eni+", "-j", "CONNMARK", "--restore-mark", "--mask", "0x80"},
					{"-m", "comment", "--comment", "AWS, primary ENI", "-i", "vlan+", "-j", "CONNMARK", "--restore-mark", "--mask", "0x80"},
				},
			},
		}, mockIptables.(*mock_iptables.MockIptables).DataplaneState)
}

func TestSetupHostNetworkMultipleCIDRs(t *testing.T) {
	ctrl, mockNetLink, _, mockNS, mockIptables := setup(t)
	defer ctrl.Finish()

	ln := &linuxNetwork{
		useExternalSNAT:        true,
		nodePortSupportEnabled: true,
		mainENIMark:            defaultConnmark,
		mtu:                    testMTU,
		vethPrefix:             eniPrefix,

		netLink: mockNetLink,
		ns:      mockNS,
		newIptables: func(iptables.Protocol) (iptableswrapper.IPTablesIface, error) {
			return mockIptables, nil
		},
	}
	setupNetLinkMocks(ctrl, mockNetLink)

	vpcCIDRs := []string{"10.10.0.0/16", "10.11.0.0/16"}
	err := ln.SetupHostNetwork(vpcCIDRs, loopback, &testEniIPNet, false, true, false)
	assert.NoError(t, err)
}

func TestSetupHostNetworkWithIPv6Enabled(t *testing.T) {
	ctrl, mockNetLink, _, mockNS, mockIptables := setup(t)
	defer ctrl.Finish()

	ln := &linuxNetwork{
		useExternalSNAT:        false,
		excludeSNATCIDRs:       nil,
		nodePortSupportEnabled: true,
		mainENIMark:            defaultConnmark,
		mtu:                    testMTU,
		vethPrefix:             eniPrefix,

		netLink: mockNetLink,
		ns:      mockNS,
		newIptables: func(iptables.Protocol) (iptableswrapper.IPTablesIface, error) {
			return mockIptables, nil
		},
	}
	setupNetLinkMocks(ctrl, mockNetLink)

	var vpcCIDRs []string
	err := ln.SetupHostNetwork(vpcCIDRs, loopback, &testEniIPNet, false, false, true)
	assert.NoError(t, err)

	assert.Equal(t, map[string]map[string][][]string{
		"filter": {
			"FORWARD": [][]string{
				{
					"-d", "169.254.172.0/22",
					"-m", "conntrack", "--ctstate", "NEW",
					"-m", "comment", "--comment", "Block Node Local Pod access via IPv4",
					"-j", "REJECT",
				},
			},
		},
	}, mockIptables.(*mock_iptables.MockIptables).DataplaneState)
}

func TestIncrementIPAddr(t *testing.T) {
	testCases := []struct {
		name     string
		ip       string
		expected string
	}{
		{"increment v4", "10.0.0.1", "10.0.0.2"},
		{"increment v6", "2600::", "2600::1"},
		{"carry up 1 v4", "10.0.0.255", "10.0.1.0"},
		{"carry up 2 v4", "10.0.255.255", "10.1.0.0"},
		{"v6 case 2", "2600::5", "2600::6"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			origIP := net.ParseIP(tc.ip)
			incrementIPAddr(origIP)
			assert.Equal(t, tc.expected, origIP.String())
		})
	}
}

func TestSetupHostNetworkIgnoringRpFilterUpdate(t *testing.T) {
	ctrl, mockNetLink, _, mockNS, mockIptables := setup(t)
	defer ctrl.Finish()

	ln := &linuxNetwork{
		useExternalSNAT:        true,
		nodePortSupportEnabled: true,
		mainENIMark:            defaultConnmark,
		mtu:                    testMTU,
		vethPrefix:             eniPrefix,

		netLink: mockNetLink,
		ns:      mockNS,
		newIptables: func(iptables.Protocol) (iptableswrapper.IPTablesIface, error) {
			return mockIptables, nil
		},
	}
	setupNetLinkMocks(ctrl, mockNetLink)

	var vpcCIDRs []string
	err := ln.SetupHostNetwork(vpcCIDRs, loopback, &testEniIPNet, false, true, false)
	assert.NoError(t, err)
}

func TestSetupHostNetworkUpdateLocalRule(t *testing.T) {
	ctrl, mockNetLink, _, mockNS, mockIptables := setup(t)
	defer ctrl.Finish()

	ln := &linuxNetwork{
		useExternalSNAT:        true,
		nodePortSupportEnabled: true,
		mainENIMark:            defaultConnmark,
		mtu:                    testMTU,
		podSGEnforcingMode:     sgpp.EnforcingModeStrict,
		vethPrefix:             eniPrefix,

		netLink: mockNetLink,
		ns:      mockNS,
		newIptables: func(iptables.Protocol) (iptableswrapper.IPTablesIface, error) {
			return mockIptables, nil
		},
	}
	setupNetLinkMocks(ctrl, mockNetLink)
	setupVethNetLinkMocks(mockNetLink)

	mockNetLink.EXPECT()

	var vpcCIDRs []string
	err := ln.SetupHostNetwork(vpcCIDRs, loopback, &testEniIPNet, true, true, false)
	assert.NoError(t, err)
}

func TestSetupHostNetworkDeleteOldConnmarkRuleForNonVpcOutboundTraffic(t *testing.T) {
	ctrl, mockNetLink, _, mockNS, mockIptables := setup(t)
	defer ctrl.Finish()

	ln := &linuxNetwork{
		useExternalSNAT:        false,
		excludeSNATCIDRs:       []string{"10.12.0.0/16", "10.13.0.0/16"},
		nodePortSupportEnabled: true,
		mainENIMark:            defaultConnmark,
		mtu:                    testMTU,
		vethPrefix:             eniPrefix,

		netLink: mockNetLink,
		ns:      mockNS,
		newIptables: func(iptables.Protocol) (iptableswrapper.IPTablesIface, error) {
			return mockIptables, nil
		},
	}
	setupNetLinkMocks(ctrl, mockNetLink)

	// add the "old" rule used in an ealier version of the CNI
	mockIptables.Append("nat", "PREROUTING", "-i", "eni+", "-m", "comment", "--comment", "AWS, outbound connections", "-m", "state", "--state", "NEW", "-j", "AWS-CONNMARK-CHAIN-0")

	var vpcCIDRs []string
	err := ln.SetupHostNetwork(vpcCIDRs, loopback, &testEniIPNet, false, true, false)
	assert.NoError(t, err)

	var exists bool
	// after setting up the host network, the "old" rule should no longer be present
	exists, _ = mockIptables.Exists("nat", "PREROUTING", "-i", "eni+", "-m", "comment", "--comment", "AWS, outbound connections", "-m", "state", "--state", "NEW", "-j", "AWS-CONNMARK-CHAIN-0")
	assert.False(t, exists, "old rule is still present")
	// it should have been replaced by the "new" rule
	exists, _ = mockIptables.Exists("nat", "PREROUTING", "-i", "eni+", "-m", "comment", "--comment", "AWS, outbound connections", "-j", "AWS-CONNMARK-CHAIN-0")
	assert.True(t, exists, "new rule is not present")
}

// Validate parsing in parseCIDRString
func TestGetExternalServiceCIDRs(t *testing.T) {
	ln := &linuxNetwork{}

	testCases := []struct {
		name          string
		cidrString    string
		expectedCidrs []string
	}{
		{
			"empty CIDR string returns nil",
			"",
			nil,
		},
		{
			"single IPV4 CIDR is accepted",
			"10.0.0.0/32",
			[]string{"10.0.0.0/32"},
		},
		{
			"valid IPV6 CIDR is rejected",
			"2000::1/128",
			nil,
		},
		{
			"IPV4 CIDR missing mask is rejected",
			"10.0.0.0",
			nil,
		},
		{
			"multiple CIDRs, only IPV4 CIDRs are accepted",
			"10.0.0.0/32, 10.0.0.1/32, 2000::1/128",
			[]string{"10.0.0.0/32", "10.0.0.1/32"},
		},
		{
			"multiple CIDRs, some malformed",
			"10.0.0.0/32,abcd",
			[]string{"10.0.0.0/32"},
		},
	}

	for _, tc := range testCases {
		os.Setenv(envExternalServiceCIDRs, tc.cidrString)
		cidrs := ln.GetExternalServiceCIDRs()
		assert.Equal(t, cidrs, tc.expectedCidrs)
	}
}

// Validate cleanup and programming in UpdateExternalIpRules
func TestUpdateExternalIpRules(t *testing.T) {
	ctrl, mockNetLink, _, _, _ := setup(t)
	defer ctrl.Finish()

	ln := &linuxNetwork{netLink: mockNetLink}
	_, cidrOne, _ := net.ParseCIDR("10.0.1.1/32")
	ruleOne := netlink.Rule{
		Dst:      cidrOne,
		Priority: externalServiceIpRulePriority,
		Table:    mainRoutingTable,
	}
	_, cidrTwo, _ := net.ParseCIDR("10.0.2.1/32")
	ruleTwo := netlink.Rule{
		Dst:      cidrTwo,
		Priority: 0,
		Table:    mainRoutingTable,
	}
	_, cidrThree, _ := net.ParseCIDR("10.0.3.1/32")
	ruleThree := netlink.Rule{
		Dst:      cidrThree,
		Priority: externalServiceIpRulePriority,
		Table:    mainRoutingTable,
	}

	testCases := []struct {
		name          string
		existingRules []netlink.Rule
		newCidrs      []string
	}{
		{
			"no existing, no new rules",
			nil,
			nil,
		},
		{
			"add new rules",
			nil,
			[]string{"10.0.0.0/32", "10.0.0.1/32"},
		},
		{
			"delete existing rules",
			[]netlink.Rule{ruleOne, ruleThree},
			nil,
		},
		{
			"skip existing rules",
			[]netlink.Rule{ruleTwo},
			nil,
		},
		{
			"add and delete existing rule",
			[]netlink.Rule{ruleOne},
			[]string{cidrOne.String()},
		},
		{
			"add and delete rules",
			[]netlink.Rule{ruleOne, ruleTwo, ruleThree},
			[]string{"10.0.0.0/32", "10.0.0.1/32"},
		},
	}

	for _, tc := range testCases {
		// Check for delete calls on expected rules
		for idx, rule := range tc.existingRules {
			if rule.Priority == externalServiceIpRulePriority {
				// Need to use index as array iteration creates a copy
				mockNetLink.EXPECT().RuleDel(&tc.existingRules[idx])
			}
		}
		// Check for add calls
		for _, cidr := range tc.newCidrs {
			_, netCidr, _ := net.ParseCIDR(cidr)
			newRule := netlink.Rule{
				Dst:      netCidr,
				Priority: externalServiceIpRulePriority,
				Table:    mainRoutingTable,
			}
			mockNetLink.EXPECT().NewRule().Return(&newRule)
			mockNetLink.EXPECT().RuleAdd(&newRule)
		}

		err := ln.UpdateExternalServiceIpRules(tc.existingRules, tc.newCidrs)
		assert.NoError(t, err)
	}
}

func setupNetLinkMocks(ctrl *gomock.Controller, mockNetLink *mock_netlinkwrapper.MockNetLink) {
	mockPrimaryInterfaceLookup(ctrl, mockNetLink)
	mockNetLink.EXPECT().LinkSetMTU(gomock.Any(), testMTU).Return(nil)

	var mainENIRule netlink.Rule
	mockNetLink.EXPECT().NewRule().Return(&mainENIRule)
	mockNetLink.EXPECT().RuleDel(&mainENIRule)
	mockNetLink.EXPECT().RuleAdd(&mainENIRule)
}

func setupVethNetLinkMocks(mockNetLink *mock_netlinkwrapper.MockNetLink) {
	var localRule netlink.Rule
	mockNetLink.EXPECT().NewRule().Return(&localRule)
	mockNetLink.EXPECT().RuleAdd(&localRule)
	mockNetLink.EXPECT().RuleDel(&localRule)
}

func Test_netLinkRuleDelAll(t *testing.T) {
	testRule := netlink.NewRule()
	testRule.IifName = "eni00bcc08c834"
	testRule.Priority = VlanRulePriority

	type ruleDelCall struct {
		rule *netlink.Rule
		err  error
	}

	type fields struct {
		ruleDelCalls []ruleDelCall
	}

	type args struct {
		rule *netlink.Rule
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr error
	}{
		{
			name: "single rule, succeed to delete",
			fields: fields{
				ruleDelCalls: []ruleDelCall{
					{
						rule: testRule,
					},
					{
						rule: testRule,
						err:  syscall.ENOENT,
					},
				},
			},
			args: args{
				rule: testRule,
			},
		},
		{
			name: "single rule, failed to delete",
			fields: fields{
				ruleDelCalls: []ruleDelCall{
					{
						rule: testRule,
						err:  errors.New("some error"),
					},
				},
			},
			args: args{
				rule: testRule,
			},
			wantErr: errors.New("some error"),
		},
		{
			name: "multiple rules, succeed to delete",
			fields: fields{
				ruleDelCalls: []ruleDelCall{
					{
						rule: testRule,
					},
					{
						rule: testRule,
					},
					{
						rule: testRule,
						err:  syscall.ENOENT,
					},
				},
			},
			args: args{
				rule: testRule,
			},
		},
		{
			name: "multiple rules, failed to delete",
			fields: fields{
				ruleDelCalls: []ruleDelCall{
					{
						rule: testRule,
					},
					{
						rule: testRule,
						err:  errors.New("some error"),
					},
				},
			},
			args: args{
				rule: testRule,
			},
			wantErr: errors.New("some error"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			netLink := mock_netlinkwrapper.NewMockNetLink(ctrl)
			for _, call := range tt.fields.ruleDelCalls {
				netLink.EXPECT().RuleDel(call.rule).Return(call.err)
			}

			err := NetLinkRuleDelAll(netLink, tt.args.rule)
			if tt.wantErr != nil {
				assert.EqualError(t, err, tt.wantErr.Error())
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func Test_containsNoSuchRule(t *testing.T) {
	type args struct {
		err error
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "syscall.EEXIST is rule not exists error",
			args: args{
				err: syscall.ENOENT,
			},
			want: true,
		},
		{
			name: "syscall.ENOENT isn't rule not exists error",
			args: args{
				err: syscall.EEXIST,
			},
			want: false,
		},
		{
			name: "non syscall error isn't rule not exists error",
			args: args{
				err: errors.New("some error"),
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsNoSuchRule(tt.args.err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func Test_isRuleExistsError(t *testing.T) {
	type args struct {
		err error
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "syscall.EEXIST is rule exists error",
			args: args{
				err: syscall.EEXIST,
			},
			want: true,
		},
		{
			name: "syscall.ENOENT isn't rule exists error",
			args: args{
				err: syscall.ENOENT,
			},
			want: false,
		},
		{
			name: "non syscall error isn't rule exists error",
			args: args{
				err: errors.New("some error"),
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isRuleExistsError(tt.args.err)
			assert.Equal(t, tt.want, got)
		})
	}
}
