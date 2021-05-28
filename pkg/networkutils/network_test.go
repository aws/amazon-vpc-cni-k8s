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
	"fmt"
	"net"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"

	mocks_ip "github.com/aws/amazon-vpc-cni-k8s/pkg/ipwrapper/mocks"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/netlinkwrapper/mock_netlink"
	mock_netlinkwrapper "github.com/aws/amazon-vpc-cni-k8s/pkg/netlinkwrapper/mocks"
	mock_nswrapper "github.com/aws/amazon-vpc-cni-k8s/pkg/nswrapper/mocks"
	mock_procsyswrapper "github.com/aws/amazon-vpc-cni-k8s/pkg/procsyswrapper/mocks"
)

const (
	loopback      = ""
	testMAC1      = "01:23:45:67:89:a0"
	testMAC2      = "01:23:45:67:89:a1"
	testTable     = 10
	testeniIP     = "10.10.10.20"
	testeniSubnet = "10.10.0.0/16"
	// Default MTU of ENI and veth
	// defined in plugins/routed-eni/driver/driver.go, pkg/networkutils/network.go
	testMTU   = 9001
	eniPrefix = "eni"
)

var (
	_, testENINetIPNet, _ = net.ParseCIDR(testeniSubnet)
	testENINetIP          = net.ParseIP(testeniIP)
)

func setup(t *testing.T) (*gomock.Controller,
	*mock_netlinkwrapper.MockNetLink,
	*mocks_ip.MockIP,
	*mock_nswrapper.MockNS,
	*mockIptables,
	*mock_procsyswrapper.MockProcSys) {
	ctrl := gomock.NewController(t)
	return ctrl,
		mock_netlinkwrapper.NewMockNetLink(ctrl),
		mocks_ip.NewMockIP(ctrl),
		mock_nswrapper.NewMockNS(ctrl),
		newMockIptables(),
		mock_procsyswrapper.NewMockProcSys(ctrl)
}

func TestSetupENINetwork(t *testing.T) {
	ctrl, mockNetLink, _, _, _, _ := setup(t)
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
	testeniAddr := &net.IPNet{
		IP:   net.ParseIP(testeniIP),
		Mask: testENINetIPNet.Mask,
	}
	mockNetLink.EXPECT().AddrList(gomock.Any(), unix.AF_INET).Return([]netlink.Addr{}, nil)
	mockNetLink.EXPECT().AddrAdd(gomock.Any(), &netlink.Addr{IPNet: testeniAddr}).Return(nil)

	mockNetLink.EXPECT().RouteDel(gomock.Any())
	mockNetLink.EXPECT().RouteReplace(gomock.Any()).Return(nil)

	mockNetLink.EXPECT().RouteDel(gomock.Any())
	mockNetLink.EXPECT().RouteReplace(gomock.Any()).Return(nil)

	mockNetLink.EXPECT().RouteDel(gomock.Any()).Return(nil)

	err = setupENINetwork(testeniIP, testMAC2, testTable, testeniSubnet, mockNetLink, 0*time.Second, 0*time.Second, testMTU)
	assert.NoError(t, err)
}

func TestSetupENINetworkMACFail(t *testing.T) {
	ctrl, mockNetLink, _, _, _, _ := setup(t)
	defer ctrl.Finish()

	// Emulate a delay attaching the ENI so a retry is necessary
	// First attempt gets one links
	for i := 0; i < maxAttemptsLinkByMac; i++ {
		mockNetLink.EXPECT().LinkList().Return(nil, fmt.Errorf("simulated failure"))
	}

	err := setupENINetwork(testeniIP, testMAC2, testTable, testeniSubnet, mockNetLink, 0*time.Second, 0*time.Second, testMTU)
	assert.Errorf(t, err, "simulated failure")
}

func TestSetupENINetowrkErrorOnPrimaryENI(t *testing.T) {
	ctrl, mockNetLink, _, _, _, _ := setup(t)
	defer ctrl.Finish()
	deviceNumber := 0
	err := setupENINetwork(testeniIP, testMAC2, deviceNumber, testeniSubnet, mockNetLink, 0*time.Second, 0*time.Second, testMTU)
	assert.Error(t, err)
}

func TestSetupHostNetworkNodePortDisabled(t *testing.T) {
	ctrl, mockNetLink, _, mockNS, mockIptables, _ := setup(t)
	defer ctrl.Finish()

	ln := &linuxNetwork{
		mainENIMark: 0x80,
		mtu:         testMTU,
		netLink:     mockNetLink,
		ns:          mockNS,
		newIptables: func() (iptablesIface, error) {
			return mockIptables, nil
		},
	}
	mockPrimaryInterfaceLookup(ctrl, mockNetLink)

	mockNetLink.EXPECT().LinkSetMTU(gomock.Any(), testMTU).Return(nil)
	var mainENIRule netlink.Rule
	mockNetLink.EXPECT().NewRule().Return(&mainENIRule)
	mockNetLink.EXPECT().RuleDel(&mainENIRule)

	var vpcCIDRs []string
	err := ln.SetupHostNetwork(vpcCIDRs, loopback, &testENINetIP, false)
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
	ctrl, mockNetLink, _, _, _, _ := setup(t)
	defer ctrl.Finish()

	ln := &linuxNetwork{netLink: mockNetLink}

	origRule := netlink.Rule{
		Src:   testENINetIPNet,
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

		err := ln.UpdateRuleListBySrc(tc.ruleList, *testENINetIPNet)
		assert.NoError(t, err)
	}
}

func TestSetupHostNetworkNodePortEnabled(t *testing.T) {
	ctrl, mockNetLink, _, mockNS, mockIptables, mockProcSys := setup(t)
	defer ctrl.Finish()

	ln := &linuxNetwork{
		useExternalSNAT:         true,
		nodePortSupportEnabled:  true,
		shouldConfigureRpFilter: true,
		mainENIMark:             defaultConnmark,
		mtu:                     testMTU,
		vethPrefix:              eniPrefix,

		netLink: mockNetLink,
		ns:      mockNS,
		newIptables: func() (iptablesIface, error) {
			return mockIptables, nil
		},
		procSys: mockProcSys,
	}

	setupNetLinkMocks(ctrl, mockNetLink)

	mockProcSys.EXPECT().Set("net/ipv4/conf/lo/rp_filter", "2").Return(nil)

	var vpcCIDRs []string

	err := ln.SetupHostNetwork(vpcCIDRs, loopback, &testENINetIP, false)
	assert.NoError(t, err)

	assert.Equal(t, map[string]map[string][][]string{
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
	}, mockIptables.dataplaneState)
}

func TestLoadMTUFromEnvTooLow(t *testing.T) {
	_ = os.Setenv(envMTU, "1")
	assert.Equal(t, GetEthernetMTU(""), minimumMTU)
}

func TestLoadMTUFromEnv1500(t *testing.T) {
	_ = os.Setenv(envMTU, "1500")
	assert.Equal(t, GetEthernetMTU(""), 1500)
}

func TestLoadMTUFromEnvTooHigh(t *testing.T) {
	_ = os.Setenv(envMTU, "65536")
	assert.Equal(t, GetEthernetMTU(""), maximumMTU)
}

func TestLoadExcludeSNATCIDRsFromEnv(t *testing.T) {
	_ = os.Setenv(envExternalSNAT, "false")
	_ = os.Setenv(envExcludeSNATCIDRs, "10.12.0.0/16,10.13.0.0/16")

	expected := []string{"10.12.0.0/16", "10.13.0.0/16"}
	assert.Equal(t, getExcludeSNATCIDRs(), expected)
}

func TestSetupHostNetworkWithExcludeSNATCIDRs(t *testing.T) {
	ctrl, mockNetLink, _, mockNS, mockIptables, mockProcSys := setup(t)
	defer ctrl.Finish()

	ln := &linuxNetwork{
		useExternalSNAT:         false,
		excludeSNATCIDRs:        []string{"10.12.0.0/16", "10.13.0.0/16"},
		nodePortSupportEnabled:  true,
		shouldConfigureRpFilter: true,
		mainENIMark:             defaultConnmark,
		mtu:                     testMTU,
		vethPrefix:              eniPrefix,

		netLink: mockNetLink,
		ns:      mockNS,
		newIptables: func() (iptablesIface, error) {
			return mockIptables, nil
		},
		procSys: mockProcSys,
	}

	setupNetLinkMocks(ctrl, mockNetLink)

	mockProcSys.EXPECT().Set("net/ipv4/conf/lo/rp_filter", "2").Return(nil)

	vpcCIDRs := []string{"10.10.0.0/16", "10.11.0.0/16"}
	err := ln.SetupHostNetwork(vpcCIDRs, loopback, &testENINetIP, false)
	assert.NoError(t, err)
	assert.Equal(t,
		map[string]map[string][][]string{
			"nat": {
				"AWS-SNAT-CHAIN-0":     [][]string{{"!", "-d", "10.10.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "AWS-SNAT-CHAIN-1"}},
				"AWS-SNAT-CHAIN-1":     [][]string{{"!", "-d", "10.11.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "AWS-SNAT-CHAIN-2"}},
				"AWS-SNAT-CHAIN-2":     [][]string{{"!", "-d", "10.12.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN EXCLUSION", "-j", "AWS-SNAT-CHAIN-3"}},
				"AWS-SNAT-CHAIN-3":     [][]string{{"!", "-d", "10.13.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN EXCLUSION", "-j", "AWS-SNAT-CHAIN-4"}},
				"AWS-SNAT-CHAIN-4":     [][]string{{"!", "-o", "vlan+", "-m", "comment", "--comment", "AWS, SNAT", "-m", "addrtype", "!", "--dst-type", "LOCAL", "-j", "SNAT", "--to-source", "10.10.10.20"}},
				"POSTROUTING":          [][]string{{"-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "AWS-SNAT-CHAIN-0"}},
				"AWS-CONNMARK-CHAIN-0": [][]string{{"!", "-d", "10.10.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, VPC CIDR", "-j", "AWS-CONNMARK-CHAIN-1"}},
				"AWS-CONNMARK-CHAIN-1": [][]string{{"!", "-d", "10.11.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, VPC CIDR", "-j", "AWS-CONNMARK-CHAIN-2"}},
				"AWS-CONNMARK-CHAIN-2": [][]string{{"!", "-d", "10.12.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, EXCLUDED CIDR", "-j", "AWS-CONNMARK-CHAIN-3"}},
				"AWS-CONNMARK-CHAIN-3": [][]string{{"!", "-d", "10.13.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, EXCLUDED CIDR", "-j", "AWS-CONNMARK-CHAIN-4"}},
				"AWS-CONNMARK-CHAIN-4": [][]string{{"-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK", "--set-xmark", "0x80/0x80"}},
				"PREROUTING": [][]string{
					{"-i", "eni+", "-m", "comment", "--comment", "AWS, outbound connections", "-m", "state", "--state", "NEW", "-j", "AWS-CONNMARK-CHAIN-0"},
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
		}, mockIptables.dataplaneState)
}

func TestSetupHostNetworkCleansUpStaleSNATRules(t *testing.T) {
	ctrl, mockNetLink, _, mockNS, mockIptables, mockProcSys := setup(t)
	defer ctrl.Finish()

	ln := &linuxNetwork{
		useExternalSNAT:         false,
		excludeSNATCIDRs:        nil,
		nodePortSupportEnabled:  true,
		shouldConfigureRpFilter: true,
		mainENIMark:             defaultConnmark,
		mtu:                     testMTU,
		vethPrefix:              eniPrefix,

		netLink: mockNetLink,
		ns:      mockNS,
		newIptables: func() (iptablesIface, error) {
			return mockIptables, nil
		},
		procSys: mockProcSys,
	}
	setupNetLinkMocks(ctrl, mockNetLink)

	mockProcSys.EXPECT().Set("net/ipv4/conf/lo/rp_filter", "2").Return(nil)

	_ = mockIptables.Append("nat", "AWS-SNAT-CHAIN-0", "!", "-d", "10.10.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAN", "-j", "AWS-SNAT-CHAIN-1") //AWS SNAT CHAN proves backwards compatibility
	_ = mockIptables.Append("nat", "AWS-SNAT-CHAIN-1", "!", "-d", "10.11.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "AWS-SNAT-CHAIN-2")
	_ = mockIptables.Append("nat", "AWS-SNAT-CHAIN-2", "!", "-d", "10.12.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN EXCLUSION", "-j", "AWS-SNAT-CHAIN-3")
	_ = mockIptables.Append("nat", "AWS-SNAT-CHAIN-3", "!", "-d", "10.13.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN EXCLUSION", "-j", "AWS-SNAT-CHAIN-4")
	_ = mockIptables.Append("nat", "AWS-SNAT-CHAIN-4", "-m", "comment", "--comment", "AWS, SNAT", "-m", "addrtype", "!", "--dst-type", "LOCAL", "-j", "SNAT", "--to-source", "10.10.10.20")
	_ = mockIptables.NewChain("nat", "AWS-SNAT-CHAIN-5")
	_ = mockIptables.Append("nat", "POSTROUTING", "-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "AWS-SNAT-CHAIN-0")
	_ = mockIptables.Append("nat", "AWS-CONNMARK-CHAIN-0", "!", "-d", "10.10.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, VPC CIDR", "-j", "AWS-CONNMARK-CHAIN-1")
	_ = mockIptables.Append("nat", "AWS-CONNMARK-CHAIN-1", "!", "-d", "10.11.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, VPC CIDR", "-j", "AWS-CONNMARK-CHAIN-2")
	_ = mockIptables.Append("nat", "AWS-CONNMARK-CHAIN-2", "!", "-d", "10.12.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, EXCLUDED CIDR", "-j", "AWS-CONNMARK-CHAIN-3")
	_ = mockIptables.Append("nat", "AWS-CONNMARK-CHAIN-3", "!", "-d", "10.13.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, EXCLUDED CIDR", "-j", "AWS-CONNMARK-CHAIN-4")
	_ = mockIptables.Append("nat", "AWS-CONNMARK-CHAIN-4", "-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK", "--set-xmark", "0x80/0x80")
	_ = mockIptables.Append("nat", "PREROUTING", "-i", "eni+", "-m", "comment", "--comment", "AWS, outbound connections", "-m", "state", "--state", "NEW", "-j", "AWS-CONNMARK-CHAIN-0")
	_ = mockIptables.Append("nat", "PREROUTING", "-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK", "--restore-mark", "--mask", "0x80")

	vpcCIDRs := []string{"10.10.0.0/16", "10.11.0.0/16"}
	err := ln.SetupHostNetwork(vpcCIDRs, loopback, &testENINetIP, false)
	assert.NoError(t, err)

	assert.Equal(t,
		map[string]map[string][][]string{
			"nat": {
				"AWS-SNAT-CHAIN-0":     [][]string{{"!", "-d", "10.10.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "AWS-SNAT-CHAIN-1"}},
				"AWS-SNAT-CHAIN-1":     [][]string{{"!", "-d", "10.11.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "AWS-SNAT-CHAIN-2"}},
				"AWS-SNAT-CHAIN-2":     [][]string{{"!", "-o", "vlan+", "-m", "comment", "--comment", "AWS, SNAT", "-m", "addrtype", "!", "--dst-type", "LOCAL", "-j", "SNAT", "--to-source", "10.10.10.20"}},
				"AWS-SNAT-CHAIN-3":     [][]string{},
				"AWS-SNAT-CHAIN-4":     [][]string{},
				"POSTROUTING":          [][]string{{"-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "AWS-SNAT-CHAIN-0"}},
				"AWS-CONNMARK-CHAIN-0": [][]string{{"!", "-d", "10.10.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, VPC CIDR", "-j", "AWS-CONNMARK-CHAIN-1"}},
				"AWS-CONNMARK-CHAIN-1": [][]string{{"!", "-d", "10.11.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, VPC CIDR", "-j", "AWS-CONNMARK-CHAIN-2"}},
				"AWS-CONNMARK-CHAIN-2": [][]string{{"-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK", "--set-xmark", "0x80/0x80"}},
				"AWS-CONNMARK-CHAIN-3": [][]string{},
				"AWS-CONNMARK-CHAIN-4": [][]string{},
				"PREROUTING": [][]string{
					{"-i", "eni+", "-m", "comment", "--comment", "AWS, outbound connections", "-m", "state", "--state", "NEW", "-j", "AWS-CONNMARK-CHAIN-0"},
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
		}, mockIptables.dataplaneState)
}

func TestSetupHostNetworkWithDifferentVethPrefix(t *testing.T) {
	ctrl, mockNetLink, _, mockNS, mockIptables, mockProcSys := setup(t)
	defer ctrl.Finish()

	ln := &linuxNetwork{
		useExternalSNAT:         false,
		excludeSNATCIDRs:        []string{"10.12.0.0/16", "10.13.0.0/16"},
		nodePortSupportEnabled:  true,
		shouldConfigureRpFilter: true,
		mainENIMark:             defaultConnmark,
		mtu:                     testMTU,
		vethPrefix:              "veth",

		netLink: mockNetLink,
		ns:      mockNS,
		newIptables: func() (iptablesIface, error) {
			return mockIptables, nil
		},
		procSys: mockProcSys,
	}

	setupNetLinkMocks(ctrl, mockNetLink)

	mockProcSys.EXPECT().Set("net/ipv4/conf/lo/rp_filter", "2").Return(nil)

	_ = mockIptables.Append("nat", "AWS-SNAT-CHAIN-0", "!", "-d", "10.10.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAN", "-j", "AWS-SNAT-CHAIN-1") //AWS SNAT CHAN proves backwards compatibility
	_ = mockIptables.Append("nat", "AWS-SNAT-CHAIN-1", "!", "-d", "10.11.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "AWS-SNAT-CHAIN-2")
	_ = mockIptables.Append("nat", "AWS-SNAT-CHAIN-2", "!", "-d", "10.12.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN EXCLUSION", "-j", "AWS-SNAT-CHAIN-3")
	_ = mockIptables.Append("nat", "AWS-SNAT-CHAIN-3", "!", "-d", "10.13.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN EXCLUSION", "-j", "AWS-SNAT-CHAIN-4")
	_ = mockIptables.Append("nat", "AWS-SNAT-CHAIN-4", "-m", "comment", "--comment", "AWS, SNAT", "-m", "addrtype", "!", "--dst-type", "LOCAL", "-j", "SNAT", "--to-source", "10.10.10.20")
	_ = mockIptables.NewChain("nat", "AWS-SNAT-CHAIN-5")
	_ = mockIptables.Append("nat", "POSTROUTING", "-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "AWS-SNAT-CHAIN-0")
	_ = mockIptables.Append("nat", "AWS-CONNMARK-CHAIN-0", "!", "-d", "10.10.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, VPC CIDR", "-j", "AWS-CONNMARK-CHAIN-1")
	_ = mockIptables.Append("nat", "AWS-CONNMARK-CHAIN-1", "!", "-d", "10.11.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, VPC CIDR", "-j", "AWS-CONNMARK-CHAIN-2")
	_ = mockIptables.Append("nat", "AWS-CONNMARK-CHAIN-2", "!", "-d", "10.12.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, EXCLUDED CIDR", "-j", "AWS-CONNMARK-CHAIN-3")
	_ = mockIptables.Append("nat", "AWS-CONNMARK-CHAIN-3", "!", "-d", "10.13.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, EXCLUDED CIDR", "-j", "AWS-CONNMARK-CHAIN-4")
	_ = mockIptables.Append("nat", "AWS-CONNMARK-CHAIN-4", "-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK", "--set-xmark", "0x80/0x80")
	_ = mockIptables.Append("nat", "PREROUTING", "-i", "eni+", "-m", "comment", "--comment", "AWS, outbound connections", "-m", "state", "--state", "NEW", "-j", "AWS-CONNMARK-CHAIN-0")
	_ = mockIptables.Append("nat", "PREROUTING", "-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK", "--restore-mark", "--mask", "0x80")

	vpcCIDRs := []string{"10.10.0.0/16", "10.11.0.0/16"}
	err := ln.SetupHostNetwork(vpcCIDRs, loopback, &testENINetIP, false)
	assert.NoError(t, err)
	assert.Equal(t,
		map[string]map[string][][]string{
			"nat": {
				"AWS-SNAT-CHAIN-0":     [][]string{{"!", "-d", "10.10.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "AWS-SNAT-CHAIN-1"}},
				"AWS-SNAT-CHAIN-1":     [][]string{{"!", "-d", "10.11.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "AWS-SNAT-CHAIN-2"}},
				"AWS-SNAT-CHAIN-2":     [][]string{{"!", "-d", "10.12.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN EXCLUSION", "-j", "AWS-SNAT-CHAIN-3"}},
				"AWS-SNAT-CHAIN-3":     [][]string{{"!", "-d", "10.13.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN EXCLUSION", "-j", "AWS-SNAT-CHAIN-4"}},
				"AWS-SNAT-CHAIN-4":     [][]string{{"!", "-o", "vlan+", "-m", "comment", "--comment", "AWS, SNAT", "-m", "addrtype", "!", "--dst-type", "LOCAL", "-j", "SNAT", "--to-source", "10.10.10.20"}},
				"POSTROUTING":          [][]string{{"-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "AWS-SNAT-CHAIN-0"}},
				"AWS-CONNMARK-CHAIN-0": [][]string{{"!", "-d", "10.10.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, VPC CIDR", "-j", "AWS-CONNMARK-CHAIN-1"}},
				"AWS-CONNMARK-CHAIN-1": [][]string{{"!", "-d", "10.11.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, VPC CIDR", "-j", "AWS-CONNMARK-CHAIN-2"}},
				"AWS-CONNMARK-CHAIN-2": [][]string{{"!", "-d", "10.12.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, EXCLUDED CIDR", "-j", "AWS-CONNMARK-CHAIN-3"}},
				"AWS-CONNMARK-CHAIN-3": [][]string{{"!", "-d", "10.13.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, EXCLUDED CIDR", "-j", "AWS-CONNMARK-CHAIN-4"}},
				"AWS-CONNMARK-CHAIN-4": [][]string{{"-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK", "--set-xmark", "0x80/0x80"}},
				"PREROUTING": [][]string{
					{"-i", "eni+", "-m", "comment", "--comment", "AWS, outbound connections", "-m", "state", "--state", "NEW", "-j", "AWS-CONNMARK-CHAIN-0"},
					{"-i", "veth+", "-m", "comment", "--comment", "AWS, outbound connections", "-m", "state", "--state", "NEW", "-j", "AWS-CONNMARK-CHAIN-0"},
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
		}, mockIptables.dataplaneState)
}
func TestSetupHostNetworkExternalNATCleanupConnmark(t *testing.T) {
	ctrl, mockNetLink, _, mockNS, mockIptables, mockProcSys := setup(t)
	defer ctrl.Finish()

	ln := &linuxNetwork{
		useExternalSNAT:         true,
		excludeSNATCIDRs:        []string{"10.12.0.0/16", "10.13.0.0/16"},
		nodePortSupportEnabled:  true,
		shouldConfigureRpFilter: true,
		mainENIMark:             defaultConnmark,
		mtu:                     testMTU,
		vethPrefix:              eniPrefix,

		netLink: mockNetLink,
		ns:      mockNS,
		newIptables: func() (iptablesIface, error) {
			return mockIptables, nil
		},
		procSys: mockProcSys,
	}
	setupNetLinkMocks(ctrl, mockNetLink)

	mockProcSys.EXPECT().Set("net/ipv4/conf/lo/rp_filter", "2").Return(nil)

	_ = mockIptables.Append("nat", "AWS-SNAT-CHAIN-0", "!", "-d", "10.10.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "AWS-SNAT-CHAIN-1")
	_ = mockIptables.Append("nat", "AWS-SNAT-CHAIN-1", "!", "-d", "10.11.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "AWS-SNAT-CHAIN-2")
	_ = mockIptables.Append("nat", "AWS-SNAT-CHAIN-2", "!", "-d", "10.12.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN EXCLUSION", "-j", "AWS-SNAT-CHAIN-3")
	_ = mockIptables.Append("nat", "AWS-SNAT-CHAIN-3", "!", "-d", "10.13.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN EXCLUSION", "-j", "AWS-SNAT-CHAIN-4")
	_ = mockIptables.Append("nat", "AWS-SNAT-CHAIN-4", "-m", "comment", "--comment", "AWS, SNAT", "-m", "addrtype", "!", "--dst-type", "LOCAL", "-j", "SNAT", "--to-source", "10.10.10.20")
	_ = mockIptables.Append("nat", "POSTROUTING", "-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "AWS-SNAT-CHAIN-0")
	_ = mockIptables.Append("nat", "AWS-CONNMARK-CHAIN-0", "!", "-d", "10.10.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, VPC CIDR", "-j", "AWS-CONNMARK-CHAIN-1")
	_ = mockIptables.Append("nat", "AWS-CONNMARK-CHAIN-1", "!", "-d", "10.11.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, VPC CIDR", "-j", "AWS-CONNMARK-CHAIN-2")
	_ = mockIptables.Append("nat", "AWS-CONNMARK-CHAIN-2", "!", "-d", "10.12.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, EXCLUDED CIDR", "-j", "AWS-CONNMARK-CHAIN-3")
	_ = mockIptables.Append("nat", "AWS-CONNMARK-CHAIN-3", "!", "-d", "10.13.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, EXCLUDED CIDR", "-j", "AWS-CONNMARK-CHAIN-4")
	_ = mockIptables.Append("nat", "AWS-CONNMARK-CHAIN-4", "-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK", "--set-xmark", "0x80/0x80")
	_ = mockIptables.Append("nat", "PREROUTING", "-i", "eni+", "-m", "comment", "--comment", "AWS, outbound connections", "-m", "state", "--state", "NEW", "-j", "AWS-CONNMARK-CHAIN-0")
	_ = mockIptables.Append("nat", "PREROUTING", "-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK", "--restore-mark", "--mask", "0x80")

	// remove exclusions
	vpcCIDRs := []string{"10.10.0.0/16", "10.11.0.0/16"}
	err := ln.SetupHostNetwork(vpcCIDRs, loopback, &testENINetIP, false)
	assert.NoError(t, err)

	assert.Equal(t,
		map[string]map[string][][]string{
			"nat": {
				"AWS-SNAT-CHAIN-0":     [][]string{},
				"AWS-SNAT-CHAIN-1":     [][]string{},
				"AWS-SNAT-CHAIN-2":     [][]string{},
				"AWS-SNAT-CHAIN-3":     [][]string{},
				"AWS-SNAT-CHAIN-4":     [][]string{},
				"POSTROUTING":          [][]string{},
				"AWS-CONNMARK-CHAIN-0": [][]string{},
				"AWS-CONNMARK-CHAIN-1": [][]string{},
				"AWS-CONNMARK-CHAIN-2": [][]string{},
				"AWS-CONNMARK-CHAIN-3": [][]string{},
				"AWS-CONNMARK-CHAIN-4": [][]string{},
				"PREROUTING":           [][]string{},
			},
			"mangle": {
				"PREROUTING": [][]string{
					{"-m", "comment", "--comment", "AWS, primary ENI", "-i", "lo", "-m", "addrtype", "--dst-type", "LOCAL", "--limit-iface-in", "-j", "CONNMARK", "--set-mark", "0x80/0x80"},
					{"-m", "comment", "--comment", "AWS, primary ENI", "-i", "eni+", "-j", "CONNMARK", "--restore-mark", "--mask", "0x80"},
					{"-m", "comment", "--comment", "AWS, primary ENI", "-i", "vlan+", "-j", "CONNMARK", "--restore-mark", "--mask", "0x80"},
				},
			},
		}, mockIptables.dataplaneState)
}
func TestSetupHostNetworkExcludedSNATCIDRsIdempotent(t *testing.T) {
	ctrl, mockNetLink, _, mockNS, mockIptables, mockProcSys := setup(t)
	defer ctrl.Finish()

	ln := &linuxNetwork{
		useExternalSNAT:         false,
		excludeSNATCIDRs:        []string{"10.12.0.0/16", "10.13.0.0/16"},
		nodePortSupportEnabled:  true,
		shouldConfigureRpFilter: true,
		mainENIMark:             defaultConnmark,
		mtu:                     testMTU,
		vethPrefix:              eniPrefix,

		netLink: mockNetLink,
		ns:      mockNS,
		newIptables: func() (iptablesIface, error) {
			return mockIptables, nil
		},
		procSys: mockProcSys,
	}
	setupNetLinkMocks(ctrl, mockNetLink)

	mockProcSys.EXPECT().Set("net/ipv4/conf/lo/rp_filter", "2").Return(nil)

	_ = mockIptables.Append("nat", "AWS-SNAT-CHAIN-0", "!", "-d", "10.10.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "AWS-SNAT-CHAIN-1")
	_ = mockIptables.Append("nat", "AWS-SNAT-CHAIN-1", "!", "-d", "10.11.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "AWS-SNAT-CHAIN-2")
	_ = mockIptables.Append("nat", "AWS-SNAT-CHAIN-2", "!", "-d", "10.12.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN EXCLUSION", "-j", "AWS-SNAT-CHAIN-3")
	_ = mockIptables.Append("nat", "AWS-SNAT-CHAIN-3", "!", "-d", "10.13.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN EXCLUSION", "-j", "AWS-SNAT-CHAIN-4")
	_ = mockIptables.Append("nat", "AWS-SNAT-CHAIN-4", "-m", "comment", "--comment", "AWS, SNAT", "-m", "addrtype", "!", "--dst-type", "LOCAL", "-j", "SNAT", "--to-source", "10.10.10.20")
	_ = mockIptables.Append("nat", "POSTROUTING", "-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "AWS-SNAT-CHAIN-0")
	_ = mockIptables.Append("nat", "AWS-CONNMARK-CHAIN-0", "!", "-d", "10.10.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, VPC CIDR", "-j", "AWS-CONNMARK-CHAIN-1")
	_ = mockIptables.Append("nat", "AWS-CONNMARK-CHAIN-1", "!", "-d", "10.11.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, VPC CIDR", "-j", "AWS-CONNMARK-CHAIN-2")
	_ = mockIptables.Append("nat", "AWS-CONNMARK-CHAIN-2", "!", "-d", "10.12.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, EXCLUDED CIDR", "-j", "AWS-CONNMARK-CHAIN-3")
	_ = mockIptables.Append("nat", "AWS-CONNMARK-CHAIN-3", "!", "-d", "10.13.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, EXCLUDED CIDR", "-j", "AWS-CONNMARK-CHAIN-4")
	_ = mockIptables.Append("nat", "AWS-CONNMARK-CHAIN-4", "-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK", "--set-xmark", "0x80/0x80")
	_ = mockIptables.Append("nat", "PREROUTING", "-i", "eni+", "-m", "comment", "--comment", "AWS, outbound connections", "-m", "state", "--state", "NEW", "-j", "AWS-CONNMARK-CHAIN-0")
	_ = mockIptables.Append("nat", "PREROUTING", "-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK", "--restore-mark", "--mask", "0x80")

	// remove exclusions
	vpcCIDRs := []string{"10.10.0.0/16", "10.11.0.0/16"}
	err := ln.SetupHostNetwork(vpcCIDRs, loopback, &testENINetIP, false)
	assert.NoError(t, err)

	assert.Equal(t,
		map[string]map[string][][]string{
			"nat": {
				"AWS-SNAT-CHAIN-0":     [][]string{{"!", "-d", "10.10.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "AWS-SNAT-CHAIN-1"}},
				"AWS-SNAT-CHAIN-1":     [][]string{{"!", "-d", "10.11.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "AWS-SNAT-CHAIN-2"}},
				"AWS-SNAT-CHAIN-2":     [][]string{{"!", "-d", "10.12.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN EXCLUSION", "-j", "AWS-SNAT-CHAIN-3"}},
				"AWS-SNAT-CHAIN-3":     [][]string{{"!", "-d", "10.13.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN EXCLUSION", "-j", "AWS-SNAT-CHAIN-4"}},
				"AWS-SNAT-CHAIN-4":     [][]string{{"!", "-o", "vlan+", "-m", "comment", "--comment", "AWS, SNAT", "-m", "addrtype", "!", "--dst-type", "LOCAL", "-j", "SNAT", "--to-source", "10.10.10.20"}},
				"POSTROUTING":          [][]string{{"-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "AWS-SNAT-CHAIN-0"}},
				"AWS-CONNMARK-CHAIN-0": [][]string{{"!", "-d", "10.10.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, VPC CIDR", "-j", "AWS-CONNMARK-CHAIN-1"}},
				"AWS-CONNMARK-CHAIN-1": [][]string{{"!", "-d", "10.11.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, VPC CIDR", "-j", "AWS-CONNMARK-CHAIN-2"}},
				"AWS-CONNMARK-CHAIN-2": [][]string{{"!", "-d", "10.12.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, EXCLUDED CIDR", "-j", "AWS-CONNMARK-CHAIN-3"}},
				"AWS-CONNMARK-CHAIN-3": [][]string{{"!", "-d", "10.13.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, EXCLUDED CIDR", "-j", "AWS-CONNMARK-CHAIN-4"}},
				"AWS-CONNMARK-CHAIN-4": [][]string{{"-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK", "--set-xmark", "0x80/0x80"}},
				"PREROUTING": [][]string{
					{"-i", "eni+", "-m", "comment", "--comment", "AWS, outbound connections", "-m", "state", "--state", "NEW", "-j", "AWS-CONNMARK-CHAIN-0"},
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
		}, mockIptables.dataplaneState)
}

func TestUpdateHostIptablesRules(t *testing.T) {
	ctrl, mockNetLink, _, mockNS, mockIptables, mockProcSys := setup(t)
	defer ctrl.Finish()

	ln := &linuxNetwork{
		useExternalSNAT:         false,
		nodePortSupportEnabled:  true,
		shouldConfigureRpFilter: true,
		mainENIMark:             defaultConnmark,
		mtu:                     testMTU,
		vethPrefix:              "veth",

		netLink: mockNetLink,
		ns:      mockNS,
		newIptables: func() (iptablesIface, error) {
			return mockIptables, nil
		},
		procSys: mockProcSys,
	}

	setupNetLinkMocks(ctrl, mockNetLink)

	mockProcSys.EXPECT().Set("net/ipv4/conf/lo/rp_filter", "2").Return(nil)

	_ = mockIptables.Append("nat", "AWS-SNAT-CHAIN-0", "!", "-d", "10.10.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAN", "-j", "AWS-SNAT-CHAIN-1") //AWS SNAT CHAN proves backwards compatibility
	_ = mockIptables.Append("nat", "AWS-SNAT-CHAIN-1", "-m", "comment", "--comment", "AWS, SNAT", "-m", "addrtype", "!", "--dst-type", "LOCAL", "-j", "SNAT", "--to-source", "10.10.10.20")
	_ = mockIptables.Append("nat", "POSTROUTING", "-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "AWS-SNAT-CHAIN-0")
	_ = mockIptables.Append("nat", "AWS-CONNMARK-CHAIN-0", "!", "-d", "10.10.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, VPC CIDR", "-j", "AWS-CONNMARK-CHAIN-1")
	_ = mockIptables.Append("nat", "AWS-CONNMARK-CHAIN-1", "-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK", "--set-xmark", "0x80/0x80")
	_ = mockIptables.Append("nat", "PREROUTING", "-i", "eni+", "-m", "comment", "--comment", "AWS, outbound connections", "-m", "state", "--state", "NEW", "-j", "AWS-CONNMARK-CHAIN-0")
	_ = mockIptables.Append("nat", "PREROUTING", "-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK", "--restore-mark", "--mask", "0x80")
	_ = mockIptables.Append("mangle", "PREROUTING", "-m", "comment", "--comment", "AWS, primary ENI", "-i", "lo", "-m", "addrtype", "--dst-type", "LOCAL", "--limit-iface-in", "-j", "CONNMARK", "--set-mark", "0x80/0x80")
	_ = mockIptables.Append("mangle", "PREROUTING", "-m", "comment", "--comment", "AWS, primary ENI", "-i", "eni+", "-j", "CONNMARK", "--restore-mark", "--mask", "0x80")
	_ = mockIptables.Append("mangle", "PREROUTING", "-m", "comment", "--comment", "AWS, primary ENI", "-i", "vlan+", "-j", "CONNMARK", "--restore-mark", "--mask", "0x80")

	vpcCIDRs := []string{"10.10.0.0/16", "10.11.0.0/16"}
	err := ln.SetupHostNetwork(vpcCIDRs, loopback, &testENINetIP, false)
	assert.NoError(t, err)
	assert.Equal(t,
		map[string]map[string][][]string{
			"nat": {
				"AWS-SNAT-CHAIN-0":     [][]string{{"!", "-d", "10.10.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "AWS-SNAT-CHAIN-1"}},
				"AWS-SNAT-CHAIN-1":     [][]string{{"!", "-d", "10.11.0.0/16", "-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "AWS-SNAT-CHAIN-2"}},
				"AWS-SNAT-CHAIN-2":     [][]string{{"!", "-o", "vlan+", "-m", "comment", "--comment", "AWS, SNAT", "-m", "addrtype", "!", "--dst-type", "LOCAL", "-j", "SNAT", "--to-source", "10.10.10.20"}},
				"POSTROUTING":          [][]string{{"-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "AWS-SNAT-CHAIN-0"}},
				"AWS-CONNMARK-CHAIN-0": [][]string{{"!", "-d", "10.10.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, VPC CIDR", "-j", "AWS-CONNMARK-CHAIN-1"}},
				"AWS-CONNMARK-CHAIN-1": [][]string{{"!", "-d", "10.11.0.0/16", "-m", "comment", "--comment", "AWS CONNMARK CHAIN, VPC CIDR", "-j", "AWS-CONNMARK-CHAIN-2"}},
				"AWS-CONNMARK-CHAIN-2": [][]string{{"-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK", "--set-xmark", "0x80/0x80"}},
				"PREROUTING": [][]string{
					{"-i", "eni+", "-m", "comment", "--comment", "AWS, outbound connections", "-m", "state", "--state", "NEW", "-j", "AWS-CONNMARK-CHAIN-0"},
					{"-i", "veth+", "-m", "comment", "--comment", "AWS, outbound connections", "-m", "state", "--state", "NEW", "-j", "AWS-CONNMARK-CHAIN-0"},
					{"-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK", "--restore-mark", "--mask", "0x80"},
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
		}, mockIptables.dataplaneState)
}
func TestSetupHostNetworkMultipleCIDRs(t *testing.T) {
	ctrl, mockNetLink, _, mockNS, mockIptables, mockProcSys := setup(t)
	defer ctrl.Finish()

	ln := &linuxNetwork{
		useExternalSNAT:         true,
		nodePortSupportEnabled:  true,
		shouldConfigureRpFilter: true,
		mainENIMark:             defaultConnmark,
		mtu:                     testMTU,
		vethPrefix:              eniPrefix,

		netLink: mockNetLink,
		ns:      mockNS,
		newIptables: func() (iptablesIface, error) {
			return mockIptables, nil
		},
		procSys: mockProcSys,
	}
	setupNetLinkMocks(ctrl, mockNetLink)

	mockProcSys.EXPECT().Set("net/ipv4/conf/lo/rp_filter", "2").Return(nil)

	vpcCIDRs := []string{"10.10.0.0/16", "10.11.0.0/16"}
	err := ln.SetupHostNetwork(vpcCIDRs, loopback, &testENINetIP, false)
	assert.NoError(t, err)
}

func TestIncrementIPv4Addr(t *testing.T) {
	testCases := []struct {
		name     string
		ip       net.IP
		expected net.IP
		err      bool
	}{
		{"increment", net.IPv4(10, 0, 0, 1), net.IPv4(10, 0, 0, 2).To4(), false},
		{"carry up 1", net.IPv4(10, 0, 0, 255), net.IPv4(10, 0, 1, 0).To4(), false},
		{"carry up 2", net.IPv4(10, 0, 255, 255), net.IPv4(10, 1, 0, 0).To4(), false},
		{"overflow", net.IPv4(255, 255, 255, 255), nil, true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := IncrementIPv4Addr(tc.ip)
			if tc.err {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tc.expected, result, tc.name)
		})
	}
}

func TestSetupHostNetworkIgnoringRpFilterUpdate(t *testing.T) {
	ctrl, mockNetLink, _, mockNS, mockIptables, mockProcSys := setup(t)
	defer ctrl.Finish()

	ln := &linuxNetwork{
		useExternalSNAT:         true,
		nodePortSupportEnabled:  true,
		shouldConfigureRpFilter: false,
		mainENIMark:             defaultConnmark,
		mtu:                     testMTU,
		vethPrefix:              eniPrefix,

		netLink: mockNetLink,
		ns:      mockNS,
		newIptables: func() (iptablesIface, error) {
			return mockIptables, nil
		},
		procSys: mockProcSys,
	}
	setupNetLinkMocks(ctrl, mockNetLink)

	var vpcCIDRs []string
	err := ln.SetupHostNetwork(vpcCIDRs, loopback, &testENINetIP, false)
	assert.NoError(t, err)
}

func TestSetupHostNetworkUpdateLocalRule(t *testing.T) {
	ctrl, mockNetLink, _, mockNS, mockIptables, mockProcSys := setup(t)
	defer ctrl.Finish()

	ln := &linuxNetwork{
		useExternalSNAT:         true,
		nodePortSupportEnabled:  true,
		shouldConfigureRpFilter: false,
		mainENIMark:             defaultConnmark,
		mtu:                     testMTU,
		vethPrefix:              eniPrefix,

		netLink: mockNetLink,
		ns:      mockNS,
		newIptables: func() (iptablesIface, error) {
			return mockIptables, nil
		},
		procSys: mockProcSys,
	}
	setupNetLinkMocks(ctrl, mockNetLink)
	setupVethNetLinkMocks(mockNetLink)

	mockNetLink.EXPECT()

	var vpcCIDRs []string
	err := ln.SetupHostNetwork(vpcCIDRs, loopback, &testENINetIP, true)
	assert.NoError(t, err)
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

type mockIptables struct {
	// dataplaneState is a map from table name to chain name to slice of rulespecs
	dataplaneState map[string]map[string][][]string
}

func newMockIptables() *mockIptables {
	return &mockIptables{dataplaneState: map[string]map[string][][]string{}}
}

func (ipt *mockIptables) Exists(table, chainName string, rulespec ...string) (bool, error) {
	chain := ipt.dataplaneState[table][chainName]
	for _, r := range chain {
		if reflect.DeepEqual(rulespec, r) {
			return true, nil
		}
	}
	return false, nil
}

func (ipt *mockIptables) Insert(table, chain string, pos int, rulespec ...string) error {
	return nil
}

func (ipt *mockIptables) Append(table, chain string, rulespec ...string) error {
	if ipt.dataplaneState[table] == nil {
		ipt.dataplaneState[table] = map[string][][]string{}
	}
	ipt.dataplaneState[table][chain] = append(ipt.dataplaneState[table][chain], rulespec)
	return nil
}

func (ipt *mockIptables) Delete(table, chainName string, rulespec ...string) error {
	chain := ipt.dataplaneState[table][chainName]
	updatedChain := chain[:0]
	found := false
	for _, r := range chain {
		if !found && reflect.DeepEqual(rulespec, r) {
			found = true
			continue
		}
		updatedChain = append(updatedChain, r)
	}
	if !found {
		return errors.New("not found")
	}
	ipt.dataplaneState[table][chainName] = updatedChain
	return nil
}

func (ipt *mockIptables) List(table, chain string) ([]string, error) {
	var chains []string
	chainContents := ipt.dataplaneState[table][chain]
	for _, ruleSpec := range chainContents {
		sanitizedRuleSpec := []string{"-A", chain}
		for _, item := range ruleSpec {
			if strings.Contains(item, " ") {
				item = fmt.Sprintf("%q", item)
			}
			sanitizedRuleSpec = append(sanitizedRuleSpec, item)
		}
		chains = append(chains, strings.Join(sanitizedRuleSpec, " "))
	}
	return chains, nil

}

func (ipt *mockIptables) NewChain(table, chain string) error {
	return nil
}

func (ipt *mockIptables) ClearChain(table, chain string) error {
	return nil
}

func (ipt *mockIptables) DeleteChain(table, chain string) error {
	return nil
}

func (ipt *mockIptables) ListChains(table string) ([]string, error) {
	var chains []string
	for chain := range ipt.dataplaneState[table] {
		chains = append(chains, chain)
	}
	return chains, nil
}

func (ipt *mockIptables) HasRandomFully() bool {
	// TODO: Work out how to write a test case for this
	return true
}
