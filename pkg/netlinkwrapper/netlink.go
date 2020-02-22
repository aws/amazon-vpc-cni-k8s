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

package netlinkwrapper

import (
	"syscall"

	"github.com/vishvananda/netlink"
)

// NetLink wraps methods used from the vishvananda/netlink package
type NetLink interface {
	// LinkByName gets a link object given the device name
	LinkByName(name string) (netlink.Link, error)
	// LinkSetNsFd is equivalent to `ip link set $link netns $ns`
	LinkSetNsFd(link netlink.Link, fd int) error
	// ParseAddr parses an address string
	ParseAddr(s string) (*netlink.Addr, error)
	// AddrAdd is equivalent to `ip addr add $addr dev $link`
	AddrAdd(link netlink.Link, addr *netlink.Addr) error
	// AddrDel is equivalent to `ip addr del $addr dev $link`
	AddrDel(link netlink.Link, addr *netlink.Addr) error
	// AddrList is equivalent to `ip addr show `
	AddrList(link netlink.Link, family int) ([]netlink.Addr, error)
	// LinkAdd is equivalent to `ip link add`
	LinkAdd(link netlink.Link) error
	// LinkSetUp is equivalent to `ip link set $link up`
	LinkSetUp(link netlink.Link) error
	// LinkList is equivalent to: `ip link show`
	LinkList() ([]netlink.Link, error)
	// LinkSetDown is equivalent to: `ip link set $link down`
	LinkSetDown(link netlink.Link) error
	// RouteList gets a list of routes in the system.
	RouteList(link netlink.Link, family int) ([]netlink.Route, error)
	// RouteAdd will add a route to the route table
	RouteAdd(route *netlink.Route) error
	// RouteReplace will replace the route in the route table
	RouteReplace(route *netlink.Route) error
	// RouteDel is equivalent to `ip route del`
	RouteDel(route *netlink.Route) error
	// NeighAdd equivalent to: `ip neigh add ....`
	NeighAdd(neigh *netlink.Neigh) error
	// LinkDel equivalent to: `ip link del $link`
	LinkDel(link netlink.Link) error
	// NewRule creates a new empty rule
	NewRule() *netlink.Rule
	// RuleAdd is equivalent to: ip rule add
	RuleAdd(rule *netlink.Rule) error
	// RuleDel is equivalent to: ip rule del
	RuleDel(rule *netlink.Rule) error
	// RuleList is equivalent to: ip rule list
	RuleList(family int) ([]netlink.Rule, error)
	// LinkSetMTU is equivalent to `ip link set dev $link mtu $mtu`
	LinkSetMTU(link netlink.Link, mtu int) error
}

type netLink struct {
}

// NewNetLink creates a new NetLink object
func NewNetLink() NetLink {
	return &netLink{}
}

func (*netLink) LinkAdd(link netlink.Link) error {
	return netlink.LinkAdd(link)
}

func (*netLink) LinkByName(name string) (netlink.Link, error) {
	return netlink.LinkByName(name)
}

func (*netLink) LinkSetNsFd(link netlink.Link, fd int) error {
	return netlink.LinkSetNsFd(link, fd)
}

func (*netLink) ParseAddr(s string) (*netlink.Addr, error) {
	return netlink.ParseAddr(s)
}

func (*netLink) AddrAdd(link netlink.Link, addr *netlink.Addr) error {
	return netlink.AddrAdd(link, addr)
}

func (*netLink) AddrDel(link netlink.Link, addr *netlink.Addr) error {
	return netlink.AddrDel(link, addr)
}

func (*netLink) LinkSetUp(link netlink.Link) error {
	return netlink.LinkSetUp(link)
}

func (*netLink) LinkList() ([]netlink.Link, error) {
	return netlink.LinkList()
}

func (*netLink) LinkSetDown(link netlink.Link) error {
	return netlink.LinkSetDown(link)
}

func (*netLink) RouteList(link netlink.Link, family int) ([]netlink.Route, error) {
	return netlink.RouteList(link, family)
}

func (*netLink) RouteAdd(route *netlink.Route) error {
	return netlink.RouteAdd(route)
}

func (*netLink) RouteReplace(route *netlink.Route) error {
	return netlink.RouteReplace(route)
}

func (*netLink) RouteDel(route *netlink.Route) error {
	return netlink.RouteDel(route)
}

func (*netLink) AddrList(link netlink.Link, family int) ([]netlink.Addr, error) {
	return netlink.AddrList(link, family)
}

func (*netLink) NeighAdd(neigh *netlink.Neigh) error {
	return netlink.NeighAdd(neigh)
}

func (*netLink) LinkDel(link netlink.Link) error {
	return netlink.LinkDel(link)
}

func (*netLink) NewRule() *netlink.Rule {
	return netlink.NewRule()
}

func (*netLink) RuleAdd(rule *netlink.Rule) error {
	return netlink.RuleAdd(rule)
}

func (*netLink) RuleDel(rule *netlink.Rule) error {
	return netlink.RuleDel(rule)
}

func (*netLink) RuleList(family int) ([]netlink.Rule, error) {
	return netlink.RuleList(family)
}

func (*netLink) LinkSetMTU(link netlink.Link, mtu int) error {
	return netlink.LinkSetMTU(link, mtu)
}

// IsNotExistsError returns true if the error type is syscall.ESRCH
// This helps us determine if we should ignore this error as the route
// that we want to cleanup has been deleted already routing table
func IsNotExistsError(err error) bool {
	if errno, ok := err.(syscall.Errno); ok {
		return errno == syscall.ESRCH
	}
	return false
}

// IsRouteExistsError returns true if the error type is syscall.EEXIST
// This helps us determine if we should ignore this error as the route
// we want to add has been added already in routing table
func IsRouteExistsError(err error) bool {
	if errno, ok := err.(syscall.Errno); ok {
		return errno == syscall.EEXIST
	}
	return false
}

// IsNetworkUnreachableError returns true if the error type is syscall.ENETUNREACH
// This helps us determine if we should ignore this error as the route the call
// depends on is not plumbed ready yet
func IsNetworkUnreachableError(err error) bool {
	if errno, ok := err.(syscall.Errno); ok {
		return errno == syscall.ENETUNREACH
	}
	return false
}
