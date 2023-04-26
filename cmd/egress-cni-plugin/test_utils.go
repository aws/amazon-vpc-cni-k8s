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

package main

import (
	"net"

	"github.com/containernetworking/cni/pkg/types/current"
	_ns "github.com/containernetworking/plugins/pkg/ns"
	"github.com/golang/mock/gomock"
	"github.com/vishvananda/netlink"

	mock_ipam "github.com/aws/amazon-vpc-cni-k8s/pkg/hostipamwrapper/mocks"
	mock_iptables "github.com/aws/amazon-vpc-cni-k8s/pkg/iptableswrapper/mocks"
	mock_netlink "github.com/aws/amazon-vpc-cni-k8s/pkg/netlinkwrapper/mocks"
	mock_ns "github.com/aws/amazon-vpc-cni-k8s/pkg/nswrapper/mocks"
	mock_procsys "github.com/aws/amazon-vpc-cni-k8s/pkg/procsyswrapper/mocks"
	mock_veth "github.com/aws/amazon-vpc-cni-k8s/pkg/vethwrapper/mocks"
)

const (
	// HostIfName is the interface name in host network namespace, it starts with veth
	HostIfName = "vethxxxx"
)

// SetupAddExpectV4 has all the mock EXPECT required when a container is added
func SetupAddExpectV4(c EgressContext, chain string, actualIptablesRules, actualRouteAdd, actualRouteDel *[]string) error {
	nsParent, err := _ns.GetCurrentNS()
	if err != nil {
		return err
	}

	c.Ipam.(*mock_ipam.MockHostIpam).EXPECT().ExecAdd("host-local", gomock.Any()).Return(
		&current.Result{
			CNIVersion: "0.4.0",
			IPs: []*current.IPConfig{
				&current.IPConfig{
					Version: "4",
					Address: net.IPNet{
						IP:   net.ParseIP("169.254.172.10"),
						Mask: net.CIDRMask(22, 32),
					},
					Gateway: net.ParseIP("169.254.172.1"),
				},
			},
		}, nil)

	c.IpTablesIface.(*mock_iptables.MockIPTablesIface).EXPECT().NewChain("nat", chain).Return(nil)

	macHost := [6]byte{0xCB, 0xB8, 0x33, 0x4C, 0x88, 0x4F}
	macCont := [6]byte{0xCC, 0xB8, 0x33, 0x4C, 0x88, 0x4F}

	c.Ns.(*mock_ns.MockNS).EXPECT().WithNetNSPath(c.NsPath, gomock.Any()).Do(func(_nsPath string, f func(_ns.NetNS) error) {
		f(nsParent)
	}).Return(nil)

	c.Veth.(*mock_veth.MockVeth).EXPECT().Setup(EgressIPv4InterfaceName, 9001, gomock.Any()).Return(
		net.Interface{
			Name:         HostIfName,
			HardwareAddr: macHost[:],
		},
		net.Interface{
			Name:         EgressIPv4InterfaceName,
			HardwareAddr: macCont[:],
		},
		nil)

	c.Link.(*mock_netlink.MockNetLink).EXPECT().AddrAdd(gomock.Any(), gomock.Any()).Return(nil)

	c.Link.(*mock_netlink.MockNetLink).EXPECT().LinkByName(HostIfName).Return(
		&netlink.Veth{
			LinkAttrs: netlink.LinkAttrs{
				Name:  HostIfName,
				Index: 100,
			},
		}, nil)
	c.Link.(*mock_netlink.MockNetLink).EXPECT().LinkByName(EgressIPv4InterfaceName).Return(
		&netlink.Veth{
			LinkAttrs: netlink.LinkAttrs{
				Name:  EgressIPv4InterfaceName,
				Index: 2,
			},
		}, nil)
	c.Link.(*mock_netlink.MockNetLink).EXPECT().RouteDel(gomock.Any()).Do(func(arg1 interface{}) error {
		r := arg1.(*netlink.Route)
		*actualRouteDel = append(*actualRouteDel, "route del: "+r.String())
		return nil
	}).Return(nil)

	c.Link.(*mock_netlink.MockNetLink).EXPECT().RouteAdd(gomock.Any()).Do(func(arg1 interface{}) error {
		r := arg1.(*netlink.Route)
		// container route adding
		*actualRouteAdd = append(*actualRouteAdd, "route add: "+r.String())
		return nil
	}).Return(nil).Times(3)

	c.Ipam.(*mock_ipam.MockHostIpam).EXPECT().ConfigureIface(EgressIPv4InterfaceName, gomock.Any()).Return(nil)
	c.Procsys.(*mock_procsys.MockProcSys).EXPECT().Get("net/ipv4/ip_forward").Return("0", nil)
	c.Procsys.(*mock_procsys.MockProcSys).EXPECT().Set("net/ipv4/ip_forward", "1").Return(nil)
	c.IpTablesIface.(*mock_iptables.MockIPTablesIface).EXPECT().HasRandomFully().Return(true)
	c.IpTablesIface.(*mock_iptables.MockIPTablesIface).EXPECT().ListChains("nat").Return([]string{"POSTROUTING"}, nil)

	c.IpTablesIface.(*mock_iptables.MockIPTablesIface).EXPECT().AppendUnique("nat", gomock.Any(), gomock.Any()).Do(func(arg1, arg2 interface{}, arg3 ...interface{}) {
		actualResult := arg1.(string) + " " + arg2.(string)
		for _, arg := range arg3 {
			actualResult += " " + arg.(string)
		}
		*actualIptablesRules = append(*actualIptablesRules, actualResult)
	}).Return(nil).Times(3)

	return nil
}

// SetupDelExpectV4 has all the mock EXPECT required when a container is deleted
func SetupDelExpectV4(c EgressContext, actualLinkDel, actualIptablesDel *[]string) error {
	nsParent, err := _ns.GetCurrentNS()
	if err != nil {
		return err
	}

	c.Ipam.(*mock_ipam.MockHostIpam).EXPECT().ExecDel("host-local", gomock.Any()).Return(nil)

	c.Ns.(*mock_ns.MockNS).EXPECT().WithNetNSPath(c.NsPath, gomock.Any()).Do(func(_nsPath string, f func(_ns.NetNS) error) {
		f(nsParent)
	}).Return(nil)

	c.Link.(*mock_netlink.MockNetLink).EXPECT().LinkByName(EgressIPv4InterfaceName).Return(
		&netlink.Veth{
			LinkAttrs: netlink.LinkAttrs{
				Name:  EgressIPv4InterfaceName,
				Index: 2,
			},
		}, nil)

	c.Link.(*mock_netlink.MockNetLink).EXPECT().AddrList(gomock.Any(), netlink.FAMILY_V4).Return(
		[]netlink.Addr{
			{
				IPNet: &net.IPNet{
					IP:   net.ParseIP("169.254.172.10"),
					Mask: net.CIDRMask(22, 32),
				},
				LinkIndex: 2,
			},
		}, nil)

	c.Link.(*mock_netlink.MockNetLink).EXPECT().LinkDel(gomock.Any()).Do(
		func(arg1 interface{}) error {
			link := arg1.(netlink.Link)
			*actualLinkDel = append(*actualLinkDel, "link del - name: "+link.Attrs().Name)
			return nil
		}).Return(nil)

	c.IpTablesIface.(*mock_iptables.MockIPTablesIface).EXPECT().Delete("nat", "POSTROUTING", gomock.Any()).Do(
		func(arg1 interface{}, arg2 interface{}, arg3 ...interface{}) {
			actualResult := arg1.(string) + " " + arg2.(string)
			for _, arg := range arg3 {
				actualResult += " " + arg.(string)
			}
			*actualIptablesDel = append(*actualIptablesDel, actualResult)
		}).Return(nil).AnyTimes()

	c.IpTablesIface.(*mock_iptables.MockIPTablesIface).EXPECT().ClearChain("nat", gomock.Any()).Do(
		func(arg1 interface{}, arg2 interface{}) {
			actualResult := arg1.(string) + " " + arg2.(string)
			*actualIptablesDel = append(*actualIptablesDel, "clear chain "+actualResult)
		}).Return(nil).AnyTimes()

	c.IpTablesIface.(*mock_iptables.MockIPTablesIface).EXPECT().DeleteChain("nat", gomock.Any()).Do(
		func(arg1 interface{}, arg2 interface{}) {
			actualResult := arg1.(string) + " " + arg2.(string)
			*actualIptablesDel = append(*actualIptablesDel, "del chain "+actualResult)
		}).Return(nil).AnyTimes()

	return nil
}
