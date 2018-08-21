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

package networkutils

import (
	"errors"
	"net"
	"os"
	"reflect"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"

	"github.com/vishvananda/netlink"

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

var (
	_, testENINetIPNet, _ = net.ParseCIDR(testeniSubnet)
	testENINetIP          = net.ParseIP(testeniIP)
)

func setup(t *testing.T) (*gomock.Controller,
	*mock_netlinkwrapper.MockNetLink,
	*mocks_ip.MockIP,
	*mock_nswrapper.MockNS,
	*mockIptables) {
	ctrl := gomock.NewController(t)
	return ctrl,
		mock_netlinkwrapper.NewMockNetLink(ctrl),
		mocks_ip.NewMockIP(ctrl),
		mock_nswrapper.NewMockNS(ctrl),
		newMockIptables()
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
	mockNetLink.EXPECT().LinkList().Return([]netlink.Link([]netlink.Link{lo, eth1}), nil)
	// 2 links
	lo.EXPECT().Attrs().Return(mockLinkAttrs1)
	eth1.EXPECT().Attrs().Return(mockLinkAttrs2)

	mockNetLink.EXPECT().LinkSetUp(gomock.Any()).Return(nil)

	// eth1's device
	eth1.EXPECT().Attrs().Return(mockLinkAttrs2)
	eth1.EXPECT().Attrs().Return(mockLinkAttrs2)

	mockNetLink.EXPECT().RouteDel(gomock.Any())
	mockNetLink.EXPECT().RouteAdd(gomock.Any()).Return(nil)

	mockNetLink.EXPECT().RouteDel(gomock.Any())
	mockNetLink.EXPECT().RouteAdd(gomock.Any()).Return(nil)

	mockNetLink.EXPECT().RouteDel(gomock.Any()).Return(nil)
	err = setupENINetwork(testeniIP, testMAC2, testTable, testeniSubnet, mockNetLink)

	assert.NoError(t, err)
}

func TestSetupENINetworkPrimary(t *testing.T) {
	ctrl, mockNetLink, _, _, _ := setup(t)
	defer ctrl.Finish()

	err := setupENINetwork(testeniIP, testMAC2, 0, testeniSubnet, mockNetLink)
	assert.NoError(t, err)
}

func TestSetupHostNetworkNodePortDisabled(t *testing.T) {
	ctrl, mockNetLink, _, mockNS, mockIptables := setup(t)
	defer ctrl.Finish()

	ln := &linuxNetwork{
		mainENIMark: 0x80,

		netLink: mockNetLink,
		ns:      mockNS,
		newIptables: func() (iptablesIface, error) {
			return mockIptables, nil
		},
	}

	var hostRule netlink.Rule
	mockNetLink.EXPECT().NewRule().Return(&hostRule)
	mockNetLink.EXPECT().RuleDel(&hostRule)
	mockNetLink.EXPECT().RuleAdd(&hostRule)
	var mainENIRule netlink.Rule
	mockNetLink.EXPECT().NewRule().Return(&mainENIRule)
	mockNetLink.EXPECT().RuleDel(&mainENIRule)

	err := ln.SetupHostNetwork(testENINetIPNet, &testENINetIP)
	assert.NoError(t, err)

	assert.Equal(t, map[string]map[string][][]string{
		"nat": {
			"POSTROUTING": [][]string{
				{
					"!", "-d", testeniSubnet,
					"-m", "comment", "--comment", "AWS, SNAT",
					"-m", "addrtype", "!", "--dst-type", "LOCAL",
					"-j", "SNAT", "--to-source", testeniIP,
				},
			},
		},
	}, mockIptables.dataplaneState)
}

func TestSetupHostNetworkNodePortEnabled(t *testing.T) {
	ctrl, mockNetLink, _, mockNS, mockIptables := setup(t)
	defer ctrl.Finish()

	var mockRPFilter mockFile
	ln := &linuxNetwork{
		useExternalSNAT:        true,
		nodePortSupportEnabled: true,
		mainENIMark:            defaultConnmark,

		netLink: mockNetLink,
		ns:      mockNS,
		newIptables: func() (iptablesIface, error) {
			return mockIptables, nil
		},
		openFile: func(name string, flag int, perm os.FileMode) (stringWriteCloser, error) {
			return &mockRPFilter, nil
		},
	}

	var hostRule netlink.Rule
	mockNetLink.EXPECT().NewRule().Return(&hostRule)
	mockNetLink.EXPECT().RuleDel(&hostRule)
	var mainENIRule netlink.Rule
	mockNetLink.EXPECT().NewRule().Return(&mainENIRule)
	mockNetLink.EXPECT().RuleDel(&mainENIRule)
	mockNetLink.EXPECT().RuleAdd(&mainENIRule)

	err := ln.SetupHostNetwork(testENINetIPNet, &testENINetIP)
	assert.NoError(t, err)

	assert.Equal(t, map[string]map[string][][]string{
		"mangle": {
			"PREROUTING": [][]string{
				{
					"-m", "comment", "--comment", "AWS, primary ENI",
					"-i", "eth0",
					"-m", "addrtype", "--dst-type", "LOCAL", "--limit-iface-in",
					"-j", "CONNMARK", "--set-mark", "0x80/0x80",
				},
				{
					"-m", "comment", "--comment", "AWS, primary ENI",
					"-i", "eni+", "-j", "CONNMARK", "--restore-mark", "--mask", "0x80",
				},
			},
		},
	}, mockIptables.dataplaneState)
	assert.Equal(t, mockFile{closed: true, data: "2"}, mockRPFilter)
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
	if ! found {
		return errors.New("not found")
	}
	ipt.dataplaneState[table][chainName] = updatedChain
	return nil
}

type mockFile struct {
	closed bool
	data   string
}

func (f *mockFile) WriteString(s string) (int, error) {
	if f.closed {
		panic("write call on closed file")
	}
	f.data += s
	return len(s), nil
}

func (f *mockFile) Close() error {
	if f.closed {
		panic("close call on closed file")
	}
	f.closed = true
	return nil
}
