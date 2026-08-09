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
	"fmt"
	"net"

	current "github.com/containernetworking/cni/pkg/types/100"
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
func SetupAddExpectV4(ec egressContext, chain string, actualIptablesRules, actualRouteAdd, actualRouteDel *[]string) error {
	nsParent, err := _ns.GetCurrentNS()
	if err != nil {
		return err
	}

	ec.Ipam.(*mock_ipam.MockHostIpam).EXPECT().ExecAdd("host-local", gomock.Any()).Return(
		&current.Result{
			CNIVersion: "1.0.0",
			IPs: []*current.IPConfig{
				{
					Address: net.IPNet{
						IP:   net.ParseIP("169.254.172.10"),
						Mask: net.CIDRMask(22, 32),
					},
					Gateway: net.ParseIP("169.254.172.1"),
				},
			},
		}, nil)

	ec.IPTablesIface.(*mock_iptables.MockIPTablesIface).EXPECT().NewChain("nat", chain).Return(nil)

	macHost := [6]byte{0xCB, 0xB8, 0x33, 0x4C, 0x88, 0x4F}
	macCont := [6]byte{0xCC, 0xB8, 0x33, 0x4C, 0x88, 0x4F}

	ec.Ns.(*mock_ns.MockNS).EXPECT().WithNetNSPath(ec.NsPath, gomock.Any()).Do(func(_nsPath string, f func(_ns.NetNS) error) {
		f(nsParent)
	}).Return(nil)

	ec.Veth.(*mock_veth.MockVeth).EXPECT().Setup(egressIPv4InterfaceName, 9001, "", gomock.Any()).Return(
		net.Interface{
			Name:         HostIfName,
			HardwareAddr: macHost[:],
		},
		net.Interface{
			Name:         egressIPv4InterfaceName,
			HardwareAddr: macCont[:],
		},
		nil)

	ec.Link.(*mock_netlink.MockNetLink).EXPECT().AddrAdd(gomock.Any(), gomock.Any()).Return(nil)

	ec.Link.(*mock_netlink.MockNetLink).EXPECT().LinkByName(HostIfName).Return(
		&netlink.Veth{
			LinkAttrs: netlink.LinkAttrs{
				Name:  HostIfName,
				Index: 100,
			},
		}, nil)
	ec.Link.(*mock_netlink.MockNetLink).EXPECT().LinkByName(egressIPv4InterfaceName).Return(
		&netlink.Veth{
			LinkAttrs: netlink.LinkAttrs{
				Name:  egressIPv4InterfaceName,
				Index: 2,
			},
		}, nil)
	ec.Link.(*mock_netlink.MockNetLink).EXPECT().RouteDel(gomock.Any()).Do(func(arg1 interface{}) error {
		r := arg1.(*netlink.Route)
		*actualRouteDel = append(*actualRouteDel, "route del: "+r.String())
		return nil
	}).Return(nil)

	ec.Link.(*mock_netlink.MockNetLink).EXPECT().RouteAdd(gomock.Any()).Do(func(arg1 interface{}) error {
		r := arg1.(*netlink.Route)
		// container route adding
		*actualRouteAdd = append(*actualRouteAdd, "route add: "+r.String())
		return nil
	}).Return(nil).Times(3)

	ec.Ipam.(*mock_ipam.MockHostIpam).EXPECT().ConfigureIface(egressIPv4InterfaceName, gomock.Any()).Return(nil)
	ec.Procsys.(*mock_procsys.MockProcSys).EXPECT().Get("net/ipv4/ip_forward").Return("0", nil)
	ec.Procsys.(*mock_procsys.MockProcSys).EXPECT().Set("net/ipv4/ip_forward", "1").Return(nil)
	ec.IPTablesIface.(*mock_iptables.MockIPTablesIface).EXPECT().HasRandomFully().Return(true)
	ec.IPTablesIface.(*mock_iptables.MockIPTablesIface).EXPECT().ListChains("nat").Return([]string{"POSTROUTING"}, nil)

	ec.IPTablesIface.(*mock_iptables.MockIPTablesIface).EXPECT().AppendUnique("nat", gomock.Any(), gomock.Any()).Do(func(arg1, arg2 interface{}, arg3 ...interface{}) {
		actualResult := arg1.(string) + " " + arg2.(string)
		for _, arg := range arg3 {
			actualResult += " " + arg.(string)
		}
		*actualIptablesRules = append(*actualIptablesRules, actualResult)
	}).Return(nil).Times(3)

	return nil
}

// SetupDelExpectV4 has all the mock EXPECT required when a container is deleted
func SetupDelExpectV4(ec egressContext, actualIptablesDel *[]string) error {
	nsParent, err := _ns.GetCurrentNS()
	if err != nil {
		return err
	}

	ec.Ipam.(*mock_ipam.MockHostIpam).EXPECT().ExecDel("host-local", gomock.Any()).Return(nil)

	ec.Ns.(*mock_ns.MockNS).EXPECT().WithNetNSPath(ec.NsPath, gomock.Any()).Do(func(_nsPath string, f func(_ns.NetNS) error) {
		f(nsParent)
	}).Return(nil)

	ec.Link.(*mock_netlink.MockNetLink).EXPECT().LinkByName(egressIPv4InterfaceName).Return(
		&netlink.Veth{
			LinkAttrs: netlink.LinkAttrs{
				Name:  egressIPv4InterfaceName,
				Index: 2,
			},
		}, nil)

	ec.Link.(*mock_netlink.MockNetLink).EXPECT().AddrList(gomock.Any(), netlink.FAMILY_V4).Return(
		[]netlink.Addr{
			{
				IPNet: &net.IPNet{
					IP:   net.ParseIP("169.254.172.10"),
					Mask: net.CIDRMask(22, 32),
				},
				LinkIndex: 2,
			},
		}, nil)

	ec.IPTablesIface.(*mock_iptables.MockIPTablesIface).EXPECT().Delete("nat", "POSTROUTING", gomock.Any()).Do(
		func(arg1 interface{}, arg2 interface{}, arg3 ...interface{}) {
			actualResult := arg1.(string) + " " + arg2.(string)
			for _, arg := range arg3 {
				actualResult += " " + arg.(string)
			}
			*actualIptablesDel = append(*actualIptablesDel, actualResult)
		}).Return(nil).AnyTimes()

	ec.IPTablesIface.(*mock_iptables.MockIPTablesIface).EXPECT().ClearChain("nat", gomock.Any()).Do(
		func(arg1 interface{}, arg2 interface{}) {
			actualResult := arg1.(string) + " " + arg2.(string)
			*actualIptablesDel = append(*actualIptablesDel, "clear chain "+actualResult)
		}).Return(nil).AnyTimes()

	ec.IPTablesIface.(*mock_iptables.MockIPTablesIface).EXPECT().DeleteChain("nat", gomock.Any()).Do(
		func(arg1 interface{}, arg2 interface{}) {
			actualResult := arg1.(string) + " " + arg2.(string)
			*actualIptablesDel = append(*actualIptablesDel, "del chain "+actualResult)
		}).Return(nil).AnyTimes()

	return nil
}

// SetupAddExpectV6 has all the mock EXPECT required when a container is added
func SetupAddExpectV6(c egressContext, chain string, actualIptablesRules, actualRouteAdd, actualRouteReplace *[]string) error {
	nsParent, err := _ns.GetCurrentNS()
	if err != nil {
		return err
	}

	c.Ipam.(*mock_ipam.MockHostIpam).EXPECT().ExecAdd("host-local", gomock.Any()).Return(
		&current.Result{
			CNIVersion: "1.0.0",
			IPs: []*current.IPConfig{
				{
					Address: net.IPNet{
						IP:   net.ParseIP("fd00::10"),
						Mask: net.CIDRMask(8, 128),
					},
				},
			},
		}, nil)

	c.IPTablesIface.(*mock_iptables.MockIPTablesIface).EXPECT().NewChain("nat", chain).Return(nil)

	macHost := [6]byte{0xCB, 0xB8, 0x33, 0x4C, 0x88, 0x4F}
	macCont := [6]byte{0xCC, 0xB8, 0x33, 0x4C, 0x88, 0x4F}

	c.Ns.(*mock_ns.MockNS).EXPECT().WithNetNSPath(c.NsPath, gomock.Any()).Do(func(_nsPath string, f func(_ns.NetNS) error) {
		f(nsParent)
	}).Return(nil).AnyTimes()

	c.Veth.(*mock_veth.MockVeth).EXPECT().Setup(egressIPv6InterfaceName, 9001, "", gomock.Any()).Return(
		net.Interface{
			Name:         HostIfName,
			HardwareAddr: macHost[:],
		},
		net.Interface{
			Name:         egressIPv6InterfaceName,
			HardwareAddr: macCont[:],
		},
		nil)

	c.Link.(*mock_netlink.MockNetLink).EXPECT().LinkByName("vethxxxx").Return(
		&netlink.Veth{
			LinkAttrs: netlink.LinkAttrs{
				Name:  "vethxxxx",
				Index: 100,
			},
		}, nil).AnyTimes()
	c.Link.(*mock_netlink.MockNetLink).EXPECT().LinkByName(egressIPv6InterfaceName).Return(
		&netlink.Veth{
			LinkAttrs: netlink.LinkAttrs{
				Name:  "v6if0",
				Index: 2,
			},
		}, nil).AnyTimes()
	c.Link.(*mock_netlink.MockNetLink).EXPECT().AddrList(gomock.Any(), netlink.FAMILY_V6).DoAndReturn(
		func(arg1 interface{}, _ interface{}) ([]netlink.Addr, error) {
			link := arg1.(netlink.Link)
			if link.Attrs().Name == "vethxxxx" {
				return []netlink.Addr{
					{
						IPNet: &net.IPNet{
							IP:   net.ParseIP("fe80::10"),
							Mask: net.CIDRMask(64, 128),
						},
						LinkIndex: 100,
					},
				}, nil
			}
			return nil, fmt.Errorf("unexpected call with link name %s", link.Attrs().Name)
		}).AnyTimes()

	c.Link.(*mock_netlink.MockNetLink).EXPECT().RouteReplace(gomock.Any()).Do(func(arg1 interface{}) error {
		r := arg1.(*netlink.Route)
		*actualRouteReplace = append(*actualRouteReplace, r.String())
		return nil
	}).Return(nil)

	c.Link.(*mock_netlink.MockNetLink).EXPECT().RouteAdd(gomock.Any()).Do(func(arg1 interface{}) error {
		r := arg1.(*netlink.Route)
		// container route adding
		*actualRouteAdd = append(*actualRouteAdd, r.String())
		return nil
	}).Return(nil).Times(1)

	c.Ipam.(*mock_ipam.MockHostIpam).EXPECT().ConfigureIface(egressIPv6InterfaceName, gomock.Any()).Return(nil)
	c.Procsys.(*mock_procsys.MockProcSys).EXPECT().Set("net/ipv6/conf/eth0/disable_ipv6", "1").Return(nil)
	c.IPTablesIface.(*mock_iptables.MockIPTablesIface).EXPECT().HasRandomFully().Return(true)
	c.IPTablesIface.(*mock_iptables.MockIPTablesIface).EXPECT().ListChains("nat").Return([]string{"POSTROUTING", c.SnatChain}, nil)

	c.IPTablesIface.(*mock_iptables.MockIPTablesIface).EXPECT().AppendUnique("nat", gomock.Any(), gomock.Any()).Do(func(arg1 interface{}, arg2 interface{}, arg3 ...interface{}) {
		actualResult := arg1.(string) + " " + arg2.(string)
		for _, arg := range arg3 {
			actualResult += " " + arg.(string)
		}
		*actualIptablesRules = append(*actualIptablesRules, actualResult)
	}).Return(nil).Times(3)

	return nil
}

// SetupDelExpectV6 has all the mock EXPECT required when a container is deleted
func SetupDelExpectV6(c egressContext, chain string, actualIptablesDel *[]string) error {
	nsParent, err := _ns.GetCurrentNS()
	if err != nil {
		return err
	}

	c.Ipam.(*mock_ipam.MockHostIpam).EXPECT().ExecDel("host-local", gomock.Any()).Return(nil)

	c.Ns.(*mock_ns.MockNS).EXPECT().WithNetNSPath(c.NsPath, gomock.Any()).Do(func(_nsPath string, f func(_ns.NetNS) error) {
		f(nsParent)
	}).Return(nil)

	c.Link.(*mock_netlink.MockNetLink).EXPECT().LinkByName(egressIPv6InterfaceName).Return(
		&netlink.Veth{
			LinkAttrs: netlink.LinkAttrs{
				Name:  egressIPv6InterfaceName,
				Index: 2,
			},
		}, nil)

	c.Link.(*mock_netlink.MockNetLink).EXPECT().AddrList(gomock.Any(), netlink.FAMILY_V6).Return(
		[]netlink.Addr{
			{
				IPNet: &net.IPNet{
					IP:   net.ParseIP("fd00::10"),
					Mask: net.CIDRMask(8, 128),
				},
				LinkIndex: 2,
			},
		}, nil).AnyTimes()

	c.IPTablesIface.(*mock_iptables.MockIPTablesIface).EXPECT().Delete("nat", "POSTROUTING", gomock.Any()).Do(
		func(arg1 interface{}, arg2 interface{}, arg3 ...interface{}) {
			actualResult := arg1.(string) + " " + arg2.(string)
			for _, arg := range arg3 {
				actualResult += " " + arg.(string)
			}
			*actualIptablesDel = append(*actualIptablesDel, actualResult)
		}).Return(nil).AnyTimes()

	c.IPTablesIface.(*mock_iptables.MockIPTablesIface).EXPECT().ClearChain("nat", chain).Do(
		func(arg1 interface{}, arg2 interface{}) {
			actualResult := arg1.(string) + " " + arg2.(string)
			*actualIptablesDel = append(*actualIptablesDel, "clear chain "+actualResult)
		}).Return(nil).AnyTimes()

	c.IPTablesIface.(*mock_iptables.MockIPTablesIface).EXPECT().DeleteChain("nat", chain).Do(
		func(arg1 interface{}, arg2 interface{}) {
			actualResult := arg1.(string) + " " + arg2.(string)
			*actualIptablesDel = append(*actualIptablesDel, "del chain "+actualResult)
		}).Return(nil).AnyTimes()

	return nil
}
