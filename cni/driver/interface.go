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
	"net"

	"github.com/containernetworking/cni/pkg/ip"
	"github.com/containernetworking/cni/pkg/ns"
	"github.com/vishvananda/netlink"
)

type NS interface {
	WithNetNSPath(nspath string, toRun func(ns.NetNS) error) error
}

type nsType int

func (nsType) WithNetNSPath(nspath string, toRun func(ns.NetNS) error) error {
	return ns.WithNetNSPath(nspath, toRun)
}

type IP interface {
	AddDefaultRoute(gw net.IP, dev netlink.Link) error
}

type ipRoute int

func (ipRoute) AddDefaultRoute(gw net.IP, dev netlink.Link) error {
	return ip.AddDefaultRoute(gw, dev)
}

type NetLink interface {
	// LinkByName gets a link object given the device name
	LinkByName(name string) (netlink.Link, error)
	// LinkSetNsFd is equivalent to `ip link set $link netns $ns`
	LinkSetNsFd(link netlink.Link, fd int) error
	// ParseAddr parses an address string
	// ParseAddr(s string) (*netlink.Addr, error)
	// AddrAdd is equivalent to `ip addr add $addr dev $link`
	AddrAdd(link netlink.Link, addr *netlink.Addr) error
	// AddrList is equivalent to `ip addr show `
	// AddrList(link netlink.Link, family int) ([]netlink.Addr, error)
	// LinkAdd is equivalent to `ip link add`
	LinkAdd(link netlink.Link) error
	// LinkSetUp is equivalent to `ip link set $link up`
	LinkSetUp(link netlink.Link) error
	// LinkList is equivalent to: `ip link show`
	// LinkList() ([]netlink.Link, error)
	// LinkSetDown is equivalent to: `ip link set $link down`
	// LinkSetDown(link netlink.Link) error
	// RouteList gets a list of routes in the system.
	// RouteList(link netlink.Link, family int) ([]netlink.Route, error)
	// RouteAdd will add a route to the route table
	RouteAdd(route *netlink.Route) error
	// RouteDel is equivalent to `ip route del`
	// RouteDel(route *netlink.Route) error

	NeighAdd(neigh *netlink.Neigh) error
	LinkDel(link netlink.Link) error

	RuleDel(rule *netlink.Rule) error
	RuleAdd(rule *netlink.Rule) error
}
