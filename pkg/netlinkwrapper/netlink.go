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

// Package netlinkwrapper is a wrapper methods for the netlink package
package netlinkwrapper

import (
	"fmt"
	"github.com/pkg/errors"
	"github.com/prometheus/common/log"
	"syscall"
	"time"

	"github.com/vishvananda/netlink"
)

// goNetLink directly wraps methods exposed by the "github.com/vishvananda/netlink" package
type goNetLink interface {
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

var _ goNetLink = &goNetLinkWrapper{}

type goNetLinkWrapper struct{}

func (*goNetLinkWrapper) LinkAdd(link netlink.Link) error {
	return netlink.LinkAdd(link)
}

func (*goNetLinkWrapper) LinkByName(name string) (netlink.Link, error) {
	return netlink.LinkByName(name)
}

func (*goNetLinkWrapper) LinkSetNsFd(link netlink.Link, fd int) error {
	return netlink.LinkSetNsFd(link, fd)
}

func (*goNetLinkWrapper) ParseAddr(s string) (*netlink.Addr, error) {
	return netlink.ParseAddr(s)
}

func (*goNetLinkWrapper) AddrAdd(link netlink.Link, addr *netlink.Addr) error {
	return netlink.AddrAdd(link, addr)
}

func (*goNetLinkWrapper) AddrDel(link netlink.Link, addr *netlink.Addr) error {
	return netlink.AddrDel(link, addr)
}

func (*goNetLinkWrapper) LinkSetUp(link netlink.Link) error {
	return netlink.LinkSetUp(link)
}

func (*goNetLinkWrapper) LinkList() ([]netlink.Link, error) {
	return netlink.LinkList()
}

func (*goNetLinkWrapper) LinkSetDown(link netlink.Link) error {
	return netlink.LinkSetDown(link)
}

func (*goNetLinkWrapper) RouteList(link netlink.Link, family int) ([]netlink.Route, error) {
	return netlink.RouteList(link, family)
}

func (*goNetLinkWrapper) RouteAdd(route *netlink.Route) error {
	return netlink.RouteAdd(route)
}

func (*goNetLinkWrapper) RouteReplace(route *netlink.Route) error {
	return netlink.RouteReplace(route)
}

func (*goNetLinkWrapper) RouteDel(route *netlink.Route) error {
	return netlink.RouteDel(route)
}

func (*goNetLinkWrapper) AddrList(link netlink.Link, family int) ([]netlink.Addr, error) {
	return netlink.AddrList(link, family)
}

func (*goNetLinkWrapper) NeighAdd(neigh *netlink.Neigh) error {
	return netlink.NeighAdd(neigh)
}

func (*goNetLinkWrapper) LinkDel(link netlink.Link) error {
	return netlink.LinkDel(link)
}

func (*goNetLinkWrapper) NewRule() *netlink.Rule {
	return netlink.NewRule()
}

func (*goNetLinkWrapper) RuleAdd(rule *netlink.Rule) error {
	return netlink.RuleAdd(rule)
}

func (*goNetLinkWrapper) RuleDel(rule *netlink.Rule) error {
	return netlink.RuleDel(rule)
}

func (*goNetLinkWrapper) RuleList(family int) ([]netlink.Rule, error) {
	return netlink.RuleList(family)
}

func (*goNetLinkWrapper) LinkSetMTU(link netlink.Link, mtu int) error {
	return netlink.LinkSetMTU(link, mtu)
}

type NetLink interface {
	goNetLink

	// LinkByMac gets a link object given the device mac
	LinkByMac(mac string) (netlink.Link, error)

	// LinkByMacWithRetry retries to get a link object given the device mac.
	LinkByMacWithRetry(mac string, interval time.Duration, maxAttempts int) (netlink.Link, error)
}

var _ NetLink = &netLink{}

type netLink struct {
	*goNetLinkWrapper
}

// NewNetLink creates a new netLink object
func NewNetLink() NetLink {
	return &netLink{
		goNetLinkWrapper: &goNetLinkWrapper{},
	}
}

func (n *netLink) LinkByMac(mac string) (netlink.Link, error) {
	links, err := n.LinkList()
	if err != nil {
		return nil, err
	}

	for _, link := range links {
		if link.Attrs().HardwareAddr.String() == mac {
			return link, nil
		}
	}
	return nil, fmt.Errorf("link mac %s not found", mac)
}

func (n *netLink) LinkByMacWithRetry(mac string, interval time.Duration, maxAttempt int) (netlink.Link, error) {
	var lastErr error
	for round := 1; round <= maxAttempt; round++ {
		if round > 1 {
			time.Sleep(interval)
		}
		link, err := n.LinkByMac(mac)
		if err != nil {
			lastErr = errors.Wrapf(err, "attempt %d/%d", round, maxAttempt)
			log.Debugf(lastErr.Error())
		}
		return link, nil
	}
	return nil, lastErr
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

// IsNoSuchRuleError returns true if the error type is syscall.ENOENT.
func IsNoSuchRuleError(err error) bool {
	if errno, ok := err.(syscall.Errno); ok {
		return errno == syscall.ENOENT
	}
	return false
}

// IsRuleExistsError returns true if the error type is syscall.EEXIST.
func IsRuleExistsError(err error) bool {
	if errno, ok := err.(syscall.Errno); ok {
		return errno == syscall.EEXIST
	}
	return false
}
