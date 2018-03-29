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
	"strings"
	"syscall"

	"github.com/coreos/go-iptables/iptables"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
)

const (
	// 0- 511 can be used other higher priorities
	toPodRulePriority = 512

	// 513 - 1023, can be used priority lower than toPodRulePriority but higher than default nonVPC CIDR rule

	// 1024 is reserved for (ip rule not to <vpc's subnet> table main)
	hostRulePriority = 1024

	// 1025 - 1535 can be used priority lower than fromPodRulePriority but higher than default nonVPC CIDR rule
	fromPodRulePriority = 1536

	mainRoutingTable = 254
)

// Network defines the host level and the eni level network related operations
type Network interface {
	// SetupNodeNetwork performs node level network configuration
	SetupHostNetwork(vpcCIDR *net.IPNet, primaryAddr *net.IP) error
	// SetupENINetwork performs eni level network configuration
	SetupENINetwork(eniIP string, mac string, table int, subnetCIDR string) error
}

type netLinker interface {
	// LinkSetUp is equivalent to `ip link set $link up`
	LinkSetUp(link netlink.Link) error
	// LinkList is equivalent to: `ip link show`
	LinkList() ([]netlink.Link, error)
	// RouteAdd will add a route to the route table
	RouteAdd(route *netlink.Route) error
	// RouteDel is equivalent to `ip route del`
	RouteDel(route *netlink.Route) error
	RuleAdd(rule *netlink.Rule) error
	RuleDel(rule *netlink.Rule) error
}

type linuxNetwork struct {
	netLink netLinker
}

// New creates a linuxNetwork object
func NewLinuxNetwork() *linuxNetwork {
	return &linuxNetwork{netLink: &netlink.Handle{}}
}

func isDuplicateRuleAdd(err error) bool {
	return strings.Contains(err.Error(), "File exists")
}

// SetupNodeNetwork performs node level network configuration
// TODO : implement ip rule not to 10.0.0.0/16(vpc'subnet) table main priority  1024
func (os *linuxNetwork) SetupHostNetwork(vpcCIDR *net.IPNet, primaryAddr *net.IP) error {

	hostRule := netlink.NewRule()
	hostRule.Dst = vpcCIDR
	hostRule.Table = mainRoutingTable
	hostRule.Priority = hostRulePriority
	hostRule.Invert = true

	// if this is a restart, cleanup previous rule first
	err := os.netLink.RuleDel(hostRule)
	if err != nil && !containsNoSuchRule(err) {
		log.Errorf("Failed to cleanup old host IP rule: %v", err)
		return errors.Wrapf(err, "host network setup: failed to delete old host rule")
	}

	err = os.netLink.RuleAdd(hostRule)

	if err != nil {
		log.Errorf("Failed to add host IP rule: %v", err)
		return errors.Wrapf(err, "host network setup: failed to add host rule")
	}

	ipt, err := iptables.New()

	if err != nil {
		return errors.Wrap(err, "host network setup: failed to create iptables")
	}

	natCmd := []string{"!", "-d", vpcCIDR.String(), "-m", "comment", "--comment", "AWS, SNAT",
		"-m", "addrtype", "!", "--dst-type", "LOCAL", "-j", "SNAT", "--to-source", primaryAddr.String()}
	exists, err := ipt.Exists("nat", "POSTROUTING", natCmd...)

	if err != nil {
		return errors.Wrapf(err, "host network setup: failed to add POSTROUTING rule for primary address %s", primaryAddr)
	}

	if !exists {
		err = ipt.Append("nat", "POSTROUTING", natCmd...)

		if err != nil {
			return errors.Wrapf(err, "host network setup: failed to append POSTROUTING rule for primary adderss %s", primaryAddr)
		}
	}

	return nil
}

func containsNoSuchRule(err error) bool {
	if errno, ok := err.(syscall.Errno); ok {
		return errno == syscall.ENOENT
	}
	return false
}

// linkByMac returns linux netlink based on interface MAC
func linkByMac(mac string, netLink netLinker) (netlink.Link, error) {
	links, err := netLink.LinkList()
	if err != nil {
		return nil, err
	}

	for _, link := range links {
		if mac == link.Attrs().HardwareAddr.String() {
			log.Infof("Found the Link that uses mac address %s and its index is %d", mac, link.Attrs().Index)
			return link, nil
		}
	}

	return nil, errors.Errorf("no interface found which uses mac address %s ", mac)
}

// SetupENINetwork adds default route to route table (eni-<eni_table>)
func (os *linuxNetwork) SetupENINetwork(eniIP string, eniMAC string, eniTable int, eniSubnetCIDR string) error {
	return setupENINetwork(eniIP, eniMAC, eniTable, eniSubnetCIDR, os.netLink)
}

func setupENINetwork(eniIP string, eniMAC string, eniTable int, eniSubnetCIDR string, netLink netLinker) error {

	if eniTable == 0 {
		log.Debugf("Skipping set up eni network for primary interface")
		return nil
	}

	log.Infof("Setting up network for an eni with ip address %s, mac address %s, cidr %s and route table %d",
		eniIP, eniMAC, eniSubnetCIDR, eniTable)
	link, err := linkByMac(eniMAC, netLink)
	if err != nil {
		return errors.Wrapf(err, "eni network setup: failed to find the link which uses mac address %s", eniMAC)
	}

	if err = netLink.LinkSetUp(link); err != nil {
		return errors.Wrapf(err, "eni network setup: failed to bring up eni %s", eniIP)
	}

	deviceNumber := link.Attrs().Index

	_, gw, err := net.ParseCIDR(eniSubnetCIDR)

	if err != nil {
		return errors.Wrapf(err, "eni network setup: invalid ipv4 cidr block %s", eniSubnetCIDR)
	}

	// TODO: big/little endian:  convert subnet to gw
	gw.IP[3] = gw.IP[3] + 1

	log.Debugf("Setting up ENI's default gateway %v", gw.IP)

	for _, r := range []netlink.Route{
		// Add a direct link route for the host's ENI IP only
		netlink.Route{
			LinkIndex: deviceNumber,
			Dst:       &net.IPNet{IP: gw.IP, Mask: net.CIDRMask(32, 32)},
			Scope:     SCOPE_LINK,
			Table:     eniTable,
		},
		// Route all other traffic via the host's ENI IP
		netlink.Route{
			LinkIndex: deviceNumber,
			Dst:       &net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(0, 32)},
			Scope:     SCOPE_UNIVERSE,
			Gw:        gw.IP,
			Table:     eniTable,
		},
	} {
		err := netLink.RouteDel(&r)
		if err != nil && !isNotExistsError(err) {
			return errors.Wrap(err, "eni network setup: failed to clean up old routes")
		}

		if err := netLink.RouteAdd(&r); err != nil {
			if !isRouteExistsError(err) {
				return errors.Wrapf(err, "eni network setup: unable to add route %s/0 via %s table %d", r.Dst.IP.String(), gw.IP.String(), eniTable)
			}
			if err := RouteReplace(&r); err != nil {
				return errors.Wrapf(err, "eni network setup: unable to replace route entry %s", r.Dst.IP.String())
			}
		}
	}

	// remove the route that default out to eni-x out of main route table
	_, cidr, err := net.ParseCIDR(eniSubnetCIDR)

	if err != nil {
		return errors.Wrapf(err, "eni network setup: invalid ipv4 cidr block %s", eniSubnetCIDR)
	}
	defaultRoute := netlink.Route{
		Dst:   cidr,
		Src:   net.ParseIP(eniIP),
		Table: mainRoutingTable,
		Scope: SCOPE_LINK,
	}

	if err := netLink.RouteDel(&defaultRoute); err != nil {
		if !isNotExistsError(err) {
			return errors.Wrapf(err, "eni network setup: unable to delete route %s for source is %s", cidr.String(), eniIP)

		}
	}
	return nil
}

// isNotExistsError returns true if the error type is syscall.ESRCH
// This helps us determine if we should ignore this error as the route
// that we want to cleanup has been deleted already routing table
func isNotExistsError(err error) bool {
	if errno, ok := err.(syscall.Errno); ok {
		return errno == syscall.ESRCH
	}

	return false
}

// isRouteExistsError returns true if the error type is syscall.EEXIST
// This helps us determine if we should ignore this error as the route
// we want to add has been added already in routing table
func isRouteExistsError(err error) bool {

	if errno, ok := err.(syscall.Errno); ok {
		return errno == syscall.EEXIST
	}
	return false

}
