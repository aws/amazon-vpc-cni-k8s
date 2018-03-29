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

package network

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vishvananda/netlink"
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

type mockNetLinker struct {
	mLinkSetUp func(link netlink.Link) error
	mLinkList  func() ([]netlink.Link, error)
	mRouteAdd  func(route *netlink.Route) error
	mRouteDel  func(route *netlink.Route) error
	mRuleAdd   func(rule *netlink.Rule) error
	mRuleDel   func(rule *netlink.Rule) error
}

func (m *mockNetLinker) LinkSetUp(link netlink.Link) error   { return m.mLinkSetUp(link) }
func (m *mockNetLinker) LinkList() ([]netlink.Link, error)   { return m.mLinkList() }
func (m *mockNetLinker) RouteAdd(route *netlink.Route) error { return m.mRouteAdd(route) }
func (m *mockNetLinker) RouteDel(route *netlink.Route) error { return m.mRouteDel(route) }
func (m *mockNetLinker) RuleAdd(rule *netlink.Rule) error    { return m.RuleAdd(rule) }
func (m *mockNetLinker) RuleDel(rule *netlink.Rule) error    { return m.RuleDel(rule) }

type mockLink struct {
	attrs *netlink.LinkAttrs
}

func (m *mockLink) Attrs() *netlink.LinkAttrs { return m.attrs }
func (m *mockLink) Type() string              { return "" }

func TestSetupENINetwork(t *testing.T) {
	mockNetLink := &mockNetLinker{
		mLinkSetUp: func(link netlink.Link) error { return nil },
		mLinkList:  func() ([]netlink.Link, error) { return nil, nil },
		mRouteAdd:  func(route *netlink.Route) error { return nil },
		mRouteDel:  func(route *netlink.Route) error { return nil },
		mRuleAdd:   func(rule *netlink.Rule) error { return nil },
		mRuleDel:   func(rule *netlink.Rule) error { return nil },
	}
	err := setupENINetwork(testeniIP, testMAC2, 0, testeniSubnet, mockNetLink)
	assert.NoError(t, err)
}

func TestSetupENINetworkPrimary(t *testing.T) {
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

	mockNetLink := &mockNetLinker{
		mLinkSetUp: func(link netlink.Link) error { return nil },
		mLinkList: func() ([]netlink.Link, error) {
			return []netlink.Link{
				&mockLink{mockLinkAttrs1},
				&mockLink{mockLinkAttrs2},
			}, nil
		},
		mRouteAdd: func(route *netlink.Route) error { return nil },
		mRouteDel: func(route *netlink.Route) error { return nil },
	}
	err = setupENINetwork(testeniIP, testMAC2, 0, testeniSubnet, mockNetLink)
	assert.NoError(t, err)
}
