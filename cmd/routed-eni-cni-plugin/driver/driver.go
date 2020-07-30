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

package driver

import (
	"fmt"
	"net"
	"os"
	"syscall"

	"github.com/pkg/errors"
	"golang.org/x/sys/unix"

	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/vishvananda/netlink"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/ipwrapper"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/netlinkwrapper"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/networkutils"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/nswrapper"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/procsyswrapper"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
)

const (
	// IP rules priority, leaving a 512 gap for the future
	toContainerRulePriority = 512
	// 1024 is reserved for (IP rule not to <VPC's subnet> table main)
	fromContainerRulePriority = 1536
	// Main routing table number
	mainRouteTable = unix.RT_TABLE_MAIN
)

var (
	dummyLinkLocalIPv4 = net.IPv4(169, 254, 1, 1)
	dummyLinkLocalIPv6 = net.IP{0xfe, 0x80, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}
)

// NetworkAPIs defines network API calls
type NetworkAPIs interface {
	SetupNS(hostVethName string, contVethName string, netnsPath string, addrs []*net.IP, table int, vpcCIDRs []string, useExternalSNAT bool, mtu int, log logger.Logger) error
	TeardownNS(addrs []*net.IP, table int, log logger.Logger) error
}

type linuxNetwork struct {
	netLink netlinkwrapper.NetLink
	ns      nswrapper.NS
	procSys procsyswrapper.ProcSys
}

// New creates linuxNetwork object
func New() NetworkAPIs {
	return &linuxNetwork{
		netLink: netlinkwrapper.NewNetLink(),
		ns:      nswrapper.NewNS(),
		procSys: procsyswrapper.NewProcSys(),
	}
}

// createVethPairContext wraps the parameters and the method to create the
// veth pair to attach the container namespace
type createVethPairContext struct {
	contVethName string
	hostVethName string
	addrs        []*net.IP
	netLink      netlinkwrapper.NetLink
	ip           ipwrapper.IP
	mtu          int
}

func newCreateVethPairContext(contVethName string, hostVethName string, addrs []*net.IP, mtu int) *createVethPairContext {
	return &createVethPairContext{
		contVethName: contVethName,
		hostVethName: hostVethName,
		addrs:        addrs,
		netLink:      netlinkwrapper.NewNetLink(),
		ip:           ipwrapper.NewIP(),
		mtu:          mtu,
	}
}

// run defines the closure to execute within the container's namespace to create the veth pair
func (createVethContext *createVethPairContext) run(hostNS ns.NetNS) error {
	veth := &netlink.Veth{
		LinkAttrs: netlink.LinkAttrs{
			Name:  createVethContext.contVethName,
			Flags: net.FlagUp,
			MTU:   createVethContext.mtu,
		},
		PeerName: createVethContext.hostVethName,
	}

	if err := createVethContext.netLink.LinkAdd(veth); err != nil {
		return err
	}

	hostVeth, err := createVethContext.netLink.LinkByName(createVethContext.hostVethName)
	if err != nil {
		return errors.Wrapf(err, "setup NS network: failed to find link %q", createVethContext.hostVethName)
	}

	// Explicitly set the veth to UP state, because netlink doesn't always do that on all the platforms with net.FlagUp.
	// veth won't get a link local address unless it's set to UP state.
	if err = createVethContext.netLink.LinkSetUp(hostVeth); err != nil {
		return errors.Wrapf(err, "setup NS network: failed to set link %q up", createVethContext.hostVethName)
	}

	contVeth, err := createVethContext.netLink.LinkByName(createVethContext.contVethName)
	if err != nil {
		return errors.Wrapf(err, "setup NS network: failed to find link %q", createVethContext.contVethName)
	}

	// Explicitly set the veth to UP state, because netlink doesn't always do that on all the platforms with net.FlagUp.
	// veth won't get a link local address unless it's set to UP state.
	if err = createVethContext.netLink.LinkSetUp(contVeth); err != nil {
		return errors.Wrapf(err, "setup NS network: failed to set link %q up", createVethContext.contVethName)
	}

	for _, addr := range createVethContext.addrs {
		var gw net.IP
		if addr4 := addr.To4(); addr4 != nil {
			// Add a connected route to a dummy next hop (169.254.1.1)
			// # ip route show
			// default via 169.254.1.1 dev eth0
			// 169.254.1.1 dev eth0
			gw = dummyLinkLocalIPv4
		} else {
			// IPv6 version
			// # ip -6 route show
			// default via fe80::1 dev eth0
			// fe80::/64 dev eth0
			gw = dummyLinkLocalIPv6
		}

		if err = createVethContext.netLink.RouteReplace(&netlink.Route{
			LinkIndex: contVeth.Attrs().Index,
			Scope:     netlink.SCOPE_LINK,
			Dst:       networkutils.IpToCIDR(&gw)}); err != nil {
			return errors.Wrap(err, "setup NS network: failed to add default gateway")
		}

		// Add a default route via dummy next hop(169.254.1.1). Then all outgoing traffic will be routed by this
		// default route via dummy next hop (169.254.1.1).
		if err = createVethContext.ip.AddDefaultRoute(gw, contVeth); err != nil {
			return errors.Wrap(err, "setup NS network: failed to add default route")
		}

		if err = createVethContext.netLink.AddrAdd(contVeth, &netlink.Addr{IPNet: networkutils.IpToCIDR(addr)}); err != nil {
			return errors.Wrapf(err, "setup NS network: failed to add IP addr to %q", createVethContext.contVethName)
		}

		// add static ARP entry for default gateway
		// we are using routed mode on the host and container need this static ARP entry to resolve its default gateway.
		neigh := &netlink.Neigh{
			LinkIndex:    contVeth.Attrs().Index,
			State:        netlink.NUD_PERMANENT,
			IP:           gw,
			HardwareAddr: hostVeth.Attrs().HardwareAddr,
		}

		if err = createVethContext.netLink.NeighAdd(neigh); err != nil {
			return errors.Wrap(err, "setup NS network: failed to add static ARP")
		}
	}

	// Now that the everything has been successfully set up in the container, move the "host" end of the
	// veth into the host namespace.
	if err = createVethContext.netLink.LinkSetNsFd(hostVeth, int(hostNS.Fd())); err != nil {
		return errors.Wrap(err, "setup NS network: failed to move veth to host netns")
	}
	return nil
}

func sameIPFamily(a, b *net.IP) bool {
	a4, b4 := a.To4(), b.To4()
	return (a4 != nil) == (b4 != nil)
}

// SetupNS wires up linux networking for a pod's network
func (os *linuxNetwork) SetupNS(hostVethName string, contVethName string, netnsPath string, addrs []*net.IP, table int, vpcCIDRs []string, useExternalSNAT bool, mtu int, log logger.Logger) error {
	log.Debugf("SetupNS: hostVethName=%s, contVethName=%s, netnsPath=%s, table=%d, mtu=%d", hostVethName, contVethName, netnsPath, table, mtu)
	return setupNS(hostVethName, contVethName, netnsPath, addrs, table, vpcCIDRs, useExternalSNAT, os.netLink, os.ns, mtu, log, os.procSys)
}

func setupNS(hostVethName string, contVethName string, netnsPath string, addrs []*net.IP, table int, vpcCIDRs []string, useExternalSNAT bool,
	netLink netlinkwrapper.NetLink, ns nswrapper.NS, mtu int, log logger.Logger, procSys procsyswrapper.ProcSys) error {
	// Clean up if hostVeth exists.
	if oldHostVeth, err := netLink.LinkByName(hostVethName); err == nil {
		if err = netLink.LinkDel(oldHostVeth); err != nil {
			return errors.Wrapf(err, "setupNS network: failed to delete old hostVeth %q", hostVethName)
		}
		log.Debugf("Clean up old hostVeth: %v\n", hostVethName)
	}

	createVethContext := newCreateVethPairContext(contVethName, hostVethName, addrs, mtu)
	if err := ns.WithNetNSPath(netnsPath, createVethContext.run); err != nil {
		log.Errorf("Failed to setup NS network %v", err)
		return errors.Wrap(err, "setupNS network: failed to setup NS network")
	}

	hostVeth, err := netLink.LinkByName(hostVethName)
	if err != nil {
		return errors.Wrapf(err, "setupNS network: failed to find link %q", hostVethName)
	}

	// NB: Must be set after move to host namespace, or kernel will reset to defaults.
	if err := procSys.Set(fmt.Sprintf("net/ipv6/conf/%s/accept_ra", hostVethName), "0"); err != nil {
		if !os.IsNotExist(err) {
			return errors.Wrapf(err, "setup NS network: failed to disable IPv6 router advertisements")
		}
		log.Debugf("SetupNS: Ignoring '%s' writing to accept_ra: Assuming kernel lacks IPv6 support", err)
	}

	if err := procSys.Set(fmt.Sprintf("net/ipv6/conf/%s/accept_redirects", hostVethName), "0"); err != nil {
		if !os.IsNotExist(err) {
			return errors.Wrapf(err, "setup NS network: failed to disable IPv6 ICMP redirects")
		}
		log.Debugf("SetupNS: Ignoring '%s' writing to accept_redirects: Assuming kernel lacks IPv6 support", err)
	}
	log.Debugf("SetupNS: disabled IPv6 RA and ICMP redirects on %s", hostVethName)

	// Explicitly set the veth to UP state, because netlink doesn't always do that on all the platforms with net.FlagUp.
	// veth won't get a link local address unless it's set to UP state.
	if err = netLink.LinkSetUp(hostVeth); err != nil {
		return errors.Wrapf(err, "setupNS network: failed to set link %q up", hostVethName)
	}

	for _, addr := range addrs {
		log.Debugf("Setup host route outgoing hostVeth, LinkIndex %d", hostVeth.Attrs().Index)

		// Add host route
		route := netlink.Route{
			LinkIndex: hostVeth.Attrs().Index,
			Scope:     netlink.SCOPE_LINK,
			Dst:       networkutils.IpToCIDR(addr),
		}

		// Add or replace route
		if err := netLink.RouteReplace(&route); err != nil {
			return errors.Wrapf(err, "setupNS: unable to add or replace route entry for %s", route.Dst.IP.String())
		}
		log.Debugf("Successfully set host route to be %s/0", route.Dst.IP.String())

		err = addContainerRule(netLink, true, networkutils.IpToCIDR(addr), mainRouteTable)

		if err != nil {
			log.Errorf("Failed to add toContainer rule for %s err=%v, ", addr.String(), err)
			return errors.Wrap(err, "setupNS network: failed to add toContainer")
		}

		log.Infof("Added toContainer rule for %s", addr.String())

		// add from-pod rule, only need it when it is not primary ENI
		if table > 0 {
			if useExternalSNAT {
				// add rule: 1536: from <podIP> use table <table>
				err = addContainerRule(netLink, false, networkutils.IpToCIDR(addr), table)
				if err != nil {
					log.Errorf("Failed to add fromContainer rule for %s err: %v", addr.String(), err)
					return errors.Wrap(err, "add NS network: failed to add fromContainer rule")
				}
				log.Infof("Added rule priority %d from %s table %d", fromContainerRulePriority, addr.String(), table)
			} else {
				// add rule: 1536: list of from <podIP> to <vpcCIDR> use table <table>
				for _, cidr := range vpcCIDRs {
					podRule := netLink.NewRule()
					_, cidrParsed, err := net.ParseCIDR(cidr)
					if err != nil {
						log.Warnf("Skipping VPC CIDR %q due to %s", cidr, err)
						continue
					}
					if !sameIPFamily(&cidrParsed.IP, addr) {
						log.Debugf("VPC CIDR %s wrong family for %s - skipping", cidrParsed, addr)
						continue
					}
					podRule.Dst = cidrParsed
					podRule.Src = networkutils.IpToCIDR(addr)
					podRule.Table = table
					podRule.Priority = fromContainerRulePriority

					err = netLink.RuleAdd(podRule)
					if isRuleExistsError(err) {
						log.Warnf("Rule already exists [%v]", podRule)
					} else {
						if err != nil {
							log.Errorf("Failed to add pod IP rule [%v]: %v", podRule, err)
							return errors.Wrapf(err, "setupNS: failed to add pod rule [%v]", podRule)
						}
					}
					var toDst string

					if podRule.Dst != nil {
						toDst = podRule.Dst.String()
					}
					log.Infof("Successfully added pod rule[%v] to %s", podRule, toDst)
				}
			}
		}
	}
	return nil
}

func addContainerRule(netLink netlinkwrapper.NetLink, isToContainer bool, addr *net.IPNet, table int) error {
	if addr == nil {
		return errors.New("can't add container rules without an IP address")
	}
	containerRule := netLink.NewRule()
	if isToContainer {
		// Example: 512:	from all to 10.200.202.222 lookup main
		containerRule.Dst = addr
		containerRule.Priority = toContainerRulePriority
	} else {
		// Example: 1536:	from 10.200.202.222 to 10.200.0.0/16 lookup 2
		containerRule.Src = addr
		containerRule.Priority = fromContainerRulePriority
	}
	containerRule.Table = table

	err := netLink.RuleDel(containerRule)
	if err != nil && !containsNoSuchRule(err) {
		return errors.Wrapf(err, "addContainerRule: failed to delete old container rule for %s", addr.String())
	}

	err = netLink.RuleAdd(containerRule)
	if err != nil {
		return errors.Wrapf(err, "addContainerRule: failed to add container rule for %s", addr.String())
	}
	return nil
}

// TeardownPodNetwork cleanup ip rules
func (os *linuxNetwork) TeardownNS(addrs []*net.IP, table int, log logger.Logger) error {
	log.Debugf("TeardownNS: addrs %s, table %d", addrs, table)
	return tearDownNS(addrs, table, os.netLink, log)
}

func tearDownNS(addrs []*net.IP, table int, netLink netlinkwrapper.NetLink, log logger.Logger) error {
	if len(addrs) == 0 {
		return errors.New("can't tear down network namespace with no IP address")
	}
	for _, addr := range addrs {
		if addr == nil {
			panic("addr is nil. This should not be possible.")
		}

		// Remove to-pod rule
		toContainerRule := netLink.NewRule()
		toContainerRule.Dst = networkutils.IpToCIDR(addr)
		toContainerRule.Priority = toContainerRulePriority
		err := netLink.RuleDel(toContainerRule)

		if err != nil {
			log.Errorf("Failed to delete toContainer rule for %s err %v", addr.String(), err)
		} else {
			log.Infof("Delete toContainer rule for %s ", addr.String())
		}

		if table > 0 {
			// remove from-pod rule only for non main table
			err := deleteRuleListBySrc(*networkutils.IpToCIDR(addr))
			if err != nil {
				log.Errorf("Failed to delete fromContainer for %s %v", addr.String(), err)
				return errors.Wrapf(err, "delete NS network: failed to delete fromContainer rule for %s", addr.String())
			}
			log.Infof("Delete fromContainer rule for %s in table %d", addr.String(), table)
		}

		// cleanup host route:
		if err = netLink.RouteDel(&netlink.Route{
			Scope: netlink.SCOPE_LINK,
			Dst:   networkutils.IpToCIDR(addr)}); err != nil {
			log.Errorf("delete NS network: failed to delete host route for %s, %v", addr.String(), err)
		}
	}

	log.Debug("Tear down of NS complete")
	return nil
}

func deleteRuleListBySrc(src net.IPNet) error {
	networkClient := networkutils.New()
	return networkClient.DeleteRuleListBySrc(src)
}

func containsNoSuchRule(err error) bool {
	if errno, ok := err.(syscall.Errno); ok {
		return errno == syscall.ENOENT
	}
	return false
}

func isRuleExistsError(err error) bool {
	if errno, ok := err.(syscall.Errno); ok {
		return errno == syscall.EEXIST
	}
	return false
}
