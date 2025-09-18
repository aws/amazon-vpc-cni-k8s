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
	"errors"
	"fmt"
	"syscall"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
	"github.com/aws/amazon-vpc-cni-k8s/utils"
	"github.com/vishvananda/netlink"
)

var log = logger.Get()

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

const (
	// Environment variable to configure netlink max retry attempts
	envNetlinkMaxRetries = "AWS_VPC_K8S_CNI_NETLINK_MAX_RETRIES"
	defaultMaxAttempts   = 5
)

// getMaxAttempts returns the configured maximum retry attempts for netlink operations
func getMaxAttempts() int {
	maxAttempts, _, _ := utils.GetIntFromStringEnvVar(envNetlinkMaxRetries, defaultMaxAttempts)
	if maxAttempts < 1 {
		log.Warnf("Invalid netlink max retries value %d (must be >= 1), using default %d", maxAttempts, defaultMaxAttempts)
		return defaultMaxAttempts
	}
	return maxAttempts
}

func retryOnErrDumpInterrupted(f func() error) error {
	var lastErr error
	maxAttempts := getMaxAttempts()
	for attempt := 0; attempt < maxAttempts; attempt++ {
		err := f()
		if err == nil {
			log.Debugf("netlink operation succeeded on attempt %d of %d", attempt+1, maxAttempts)
			return nil
		}
		if !errors.Is(err, netlink.ErrDumpInterrupted) {
			log.Errorf("netlink operation failed with unrecoverable error on attempt %d of %d: %v", attempt+1, maxAttempts, err)
			return fmt.Errorf("netlink operation failed: %w", err)
		}
		log.Debugf("netlink operation interrupted on attempt %d of %d", attempt+1, maxAttempts)
		lastErr = err
	}
	log.Errorf("netlink operation interruption persisted after %d attempt(s): %v", maxAttempts, lastErr)
	return fmt.Errorf("netlink operation interruption persisted after %d attempt(s): %w", maxAttempts, lastErr)
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
	var links []netlink.Link
	var err error
	err = retryOnErrDumpInterrupted(func() error {
		links, err = netlink.LinkList()
		return err
	})
	return links, err
}

func (*netLink) LinkSetDown(link netlink.Link) error {
	return netlink.LinkSetDown(link)
}

func (*netLink) RouteList(link netlink.Link, family int) ([]netlink.Route, error) {
	var routes []netlink.Route
	var err error
	err = retryOnErrDumpInterrupted(func() error {
		routes, err = netlink.RouteList(link, family)
		return err
	})
	return routes, err
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
	var addrs []netlink.Addr
	var err error
	err = retryOnErrDumpInterrupted(func() error {
		addrs, err = netlink.AddrList(link, family)
		return err
	})
	return addrs, err
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
	var rules []netlink.Rule
	var err error
	err = retryOnErrDumpInterrupted(func() error {
		rules, err = netlink.RuleList(family)
		return err
	})
	return rules, err
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
