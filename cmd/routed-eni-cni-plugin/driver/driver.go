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

// Package driver is the CNI network driver setting up iptables, routes and rules
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
	// vlan rule priority
	vlanRulePriority = 10
	// IP rules priority, leaving a 512 gap for the future
	toContainerRulePriority = 512
	// 1024 is reserved for (IP rule not to <VPC's subnet> table main)
	fromContainerRulePriority = 1536
	// Main routing table number
	mainRouteTable = unix.RT_TABLE_MAIN
)

// NetworkAPIs defines network API calls
type NetworkAPIs interface {
	SetupNS(hostVethName string, contVethName string, netnsPath string, v4Addr *net.IPNet, v6Addr *net.IPNet, deviceNumber int, vpcCIDRs []string, useExternalSNAT bool, mtu int, log logger.Logger) error
	TeardownNS(addr *net.IPNet, deviceNumber int, log logger.Logger) error
	SetupPodENINetwork(hostVethName string, contVethName string, netnsPath string, v4Addr *net.IPNet, v6Addr *net.IPNet, vlanID int, eniMAC string,
		subnetGW string, parentIfIndex int, mtu int, log logger.Logger) error
	TeardownPodENINetwork(vlanID int, log logger.Logger) error
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
	v4Addr       *net.IPNet
	v6Addr       *net.IPNet
	netLink      netlinkwrapper.NetLink
	ip           ipwrapper.IP
	mtu          int
	procSys      procsyswrapper.ProcSys
}

func newCreateVethPairContext(contVethName string, hostVethName string, v4Addr *net.IPNet, v6Addr *net.IPNet, mtu int) *createVethPairContext {
	return &createVethPairContext{
		contVethName: contVethName,
		hostVethName: hostVethName,
		v4Addr:       v4Addr,
		v6Addr:       v6Addr,
		netLink:      netlinkwrapper.NewNetLink(),
		ip:           ipwrapper.NewIP(),
		mtu:          mtu,
		procSys:      procsyswrapper.NewProcSys(),
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

	if createVethContext.v6Addr != nil && createVethContext.v6Addr.IP.To16() != nil {
		//Enable v6 support on Container's veth interface.
		if err = createVethContext.procSys.Set(fmt.Sprintf("net/ipv6/conf/%s/disable_ipv6", createVethContext.contVethName), "0"); err != nil {
			if !os.IsNotExist(err) {
				return errors.Wrapf(err, "setupVeth network: failed to enable IPv6 on container veth interface")
			}
		}

		//Enable v6 support on Container's lo interface inside the Pod networking namespace.
		if err = createVethContext.procSys.Set(fmt.Sprintf("net/ipv6/conf/lo/disable_ipv6"), "0"); err != nil {
			if !os.IsNotExist(err) {
				return errors.Wrapf(err, "setupVeth network: failed to enable IPv6 on container's lo interface")
			}
		}

		//Enable v6 forwarding on Container's veth interface. We will set the same on Host side veth after we move it to host netns.
		if err = createVethContext.procSys.Set(fmt.Sprintf("net/ipv6/conf/%s/forwarding", createVethContext.contVethName), "1"); err != nil {
			if !os.IsNotExist(err) {
				return errors.Wrapf(err, "setupVeth network: failed to enable IPv6 forwarding container veth interface")
			}
		}
	}

	// Add a connected route to a dummy next hop (169.254.1.1 or fe80::1)
	// # ip route show
	// default via 169.254.1.1 dev eth0
	// 169.254.1.1 dev eth0

	var gwIP string
	var maskLen int
	var addr *netlink.Addr
	var defNet *net.IPNet

	if createVethContext.v4Addr != nil {
		gwIP = "169.254.1.1"
		maskLen = 32
		addr = &netlink.Addr{IPNet: createVethContext.v4Addr}
		_, defNet, _ = net.ParseCIDR("0.0.0.0/0")
	} else if createVethContext.v6Addr != nil {
		gwIP = "fe80::1"
		maskLen = 128
		addr = &netlink.Addr{IPNet: createVethContext.v6Addr}
		_, defNet, _ = net.ParseCIDR("::/0")
	}

	gw := net.ParseIP(gwIP)
	gwNet := &net.IPNet{IP: gw, Mask: net.CIDRMask(maskLen, maskLen)}

	if err = createVethContext.netLink.RouteReplace(&netlink.Route{
		LinkIndex: contVeth.Attrs().Index,
		Scope:     netlink.SCOPE_LINK,
		Dst:       gwNet}); err != nil {
		return errors.Wrap(err, "setup NS network: failed to add default gateway")
	}

	// Add a default route via dummy next hop(169.254.1.1 or fe80::1). Then all outgoing traffic will be routed by this
	// default route via dummy next hop (169.254.1.1 or fe80::1)
	if err = createVethContext.netLink.RouteAdd(&netlink.Route{
		LinkIndex: contVeth.Attrs().Index,
		Scope:     netlink.SCOPE_UNIVERSE,
		Dst:       defNet,
		Gw:        gw,
	}); err != nil {
		return errors.Wrap(err, "setup NS network: failed to add default route")
	}

	if err = createVethContext.netLink.AddrAdd(contVeth, addr); err != nil {
		return errors.Wrapf(err, "setup NS network: failed to add IP addr to %q", createVethContext.contVethName)
	}

	// add static ARP entry for default gateway
	// we are using routed mode on the host and container need this static ARP entry to resolve its default gateway.
	// IP address family is derived from the IP address passed to the function (v4 or v6)
	neigh := &netlink.Neigh{
		LinkIndex:    contVeth.Attrs().Index,
		State:        netlink.NUD_PERMANENT,
		IP:           gwNet.IP,
		HardwareAddr: hostVeth.Attrs().HardwareAddr,
	}

	if err = createVethContext.netLink.NeighAdd(neigh); err != nil {
		return errors.Wrap(err, "setup NS network: failed to add static ARP")
	}

	// Now that the everything has been successfully set up in the container, move the "host" end of the
	// veth into the host namespace.
	if err = createVethContext.netLink.LinkSetNsFd(hostVeth, int(hostNS.Fd())); err != nil {
		return errors.Wrap(err, "setup NS network: failed to move veth to host netns")
	}
	return nil
}

// SetupNS wires up linux networking for a pod's network
func (os *linuxNetwork) SetupNS(hostVethName string, contVethName string, netnsPath string, v4Addr *net.IPNet, v6Addr *net.IPNet,
	deviceNumber int, vpcCIDRs []string, useExternalSNAT bool, mtu int, log logger.Logger) error {
	log.Debugf("SetupNS: hostVethName=%s, contVethName=%s, netnsPath=%s, deviceNumber=%d, mtu=%d", hostVethName, contVethName, netnsPath, deviceNumber, mtu)
	return setupNS(hostVethName, contVethName, netnsPath, v4Addr, v6Addr, deviceNumber, vpcCIDRs, useExternalSNAT, os.netLink, os.ns, mtu, log, os.procSys)
}

func setupNS(hostVethName string, contVethName string, netnsPath string, v4Addr *net.IPNet, v6Addr *net.IPNet, deviceNumber int, vpcCIDRs []string, useExternalSNAT bool,
	netLink netlinkwrapper.NetLink, ns nswrapper.NS, mtu int, log logger.Logger, procSys procsyswrapper.ProcSys) error {

	hostVeth, err := setupVeth(hostVethName, contVethName, netnsPath, v4Addr, v6Addr, netLink, ns, mtu, procSys, log)
	if err != nil {
		return errors.Wrapf(err, "setupNS network: failed to setup veth pair.")
	}

	log.Debugf("Setup host route outgoing hostVeth, LinkIndex %d", hostVeth.Attrs().Index)

	var addrHostAddr *net.IPNet
	//We only support either v4 or v6 modes.
	if v4Addr != nil && v4Addr.IP.To4() != nil {
		addrHostAddr = &net.IPNet{
			IP:   v4Addr.IP,
			Mask: net.CIDRMask(32, 32)}
	} else if v6Addr != nil && v6Addr.IP.To16() != nil {
		addrHostAddr = &net.IPNet{
			IP:   v6Addr.IP,
			Mask: net.CIDRMask(128, 128)}
	}

	// Add host route
	route := netlink.Route{
		LinkIndex: hostVeth.Attrs().Index,
		Scope:     netlink.SCOPE_LINK,
		Dst:       addrHostAddr}

	// Add or replace route
	if err := netLink.RouteReplace(&route); err != nil {
		return errors.Wrapf(err, "setupNS: unable to add or replace route entry for %s", route.Dst.IP.String())
	}
	log.Debugf("Successfully set host route to be %s/0", route.Dst.IP.String())

	err = addContainerRule(netLink, true, addrHostAddr, mainRouteTable)

	if err != nil {
		log.Errorf("Failed to add toContainer rule for %s err=%v, ", addrHostAddr.String(), err)
		return errors.Wrap(err, "setupNS network: failed to add toContainer")
	}

	log.Infof("Added toContainer rule for %s", addrHostAddr.String())

	// add from-pod rule, only need it when it is not primary ENI
	if deviceNumber > 0 {
		// To be backwards compatible, we will have to keep this off-by one setting
		tableNumber := deviceNumber + 1
		// add rule: 1536: from <podIP> use table <table>
		err = addContainerRule(netLink, false, addrHostAddr, tableNumber)
		if err != nil {
			log.Errorf("Failed to add fromContainer rule for %s err: %v", addrHostAddr.String(), err)
			return errors.Wrap(err, "add NS network: failed to add fromContainer rule")
		}
		log.Infof("Added rule priority %d from %s table %d", fromContainerRulePriority, addrHostAddr.String(), tableNumber)
	}
	return nil
}

// setupVeth sets up veth for the pod.
func setupVeth(hostVethName string, contVethName string, netnsPath string, v4Addr *net.IPNet, v6Addr *net.IPNet, netLink netlinkwrapper.NetLink,
	ns nswrapper.NS, mtu int, procSys procsyswrapper.ProcSys, log logger.Logger) (netlink.Link, error) {
	// Clean up if hostVeth exists.
	if oldHostVeth, err := netLink.LinkByName(hostVethName); err == nil {
		if err = netLink.LinkDel(oldHostVeth); err != nil {
			return nil, errors.Wrapf(err, "setupVeth network: failed to delete old hostVeth %q", hostVethName)
		}
		log.Debugf("Cleaned up old hostVeth: %v\n", hostVethName)
	}

	log.Debugf("v4addr: %v; v6Addr: %v\n", v4Addr, v6Addr)
	createVethContext := newCreateVethPairContext(contVethName, hostVethName, v4Addr, v6Addr, mtu)
	if err := ns.WithNetNSPath(netnsPath, createVethContext.run); err != nil {
		log.Errorf("Failed to setup veth network %v", err)
		return nil, errors.Wrap(err, "setupVeth network: failed to setup veth network")
	}

	hostVeth, err := netLink.LinkByName(hostVethName)
	if err != nil {
		return nil, errors.Wrapf(err, "setupVeth network: failed to find link %q", hostVethName)
	}

	if err := procSys.Set(fmt.Sprintf("net/ipv6/conf/%s/accept_ra", hostVethName), "0"); err != nil {
		if !os.IsNotExist(err) {
			return nil, errors.Wrapf(err, "setupVeth network: failed to disable IPv6 router advertisements")
		}
		log.Debugf("setupVeth network: Ignoring '%s' writing to accept_ra: Assuming kernel lacks IPv6 support", err)
	}

	if err := procSys.Set(fmt.Sprintf("net/ipv6/conf/%s/accept_redirects", hostVethName), "0"); err != nil {
		if !os.IsNotExist(err) {
			return nil, errors.Wrapf(err, "setupVeth network: failed to disable IPv6 ICMP redirects")
		}
		log.Debugf("setupVeth network: Ignoring '%s' writing to accept_redirects: Assuming kernel lacks IPv6 support", err)
	}
	log.Debugf("setupVeth network: disabled IPv6 RA and ICMP redirects on %s", hostVethName)

	// Explicitly set the veth to UP state, because netlink doesn't always do that on all the platforms with net.FlagUp.
	// veth won't get a link local address unless it's set to UP state.
	if err = netLink.LinkSetUp(hostVeth); err != nil {
		return nil, errors.Wrapf(err, "setupVeth network: failed to set link %q up", hostVethName)
	}
	return hostVeth, nil
}

// SetupPodENINetwork sets up the network ns for pods requesting its own security group
func (os *linuxNetwork) SetupPodENINetwork(hostVethName string, contVethName string, netnsPath string, v4Addr *net.IPNet,
	v6Addr *net.IPNet, vlanID int, eniMAC string, subnetGW string, parentIfIndex int, mtu int, log logger.Logger) error {

	hostVeth, err := setupVeth(hostVethName, contVethName, netnsPath, v4Addr, v6Addr, os.netLink, os.ns, mtu, os.procSys, log)
	if err != nil {
		return errors.Wrapf(err, "SetupPodENINetwork failed to setup veth pair.")
	}

	vlanTableID := vlanID + 100
	vlanLink := buildVlanLink(vlanID, parentIfIndex, eniMAC)

	// 1a. clean up if vlan already exists (necessary when trunk ENI changes).
	if oldVlan, err := os.netLink.LinkByName(vlanLink.Name); err == nil {
		if err = os.netLink.LinkDel(oldVlan); err != nil {
			return errors.Wrapf(err, "SetupPodENINetwork: failed to delete old vlan %s", vlanLink.Name)
		}
		log.Debugf("Cleaned up old vlan: %s", vlanLink.Name)
	}

	// 1b. clean up any previous hostVeth ip rule
	oldVlanRule := os.netLink.NewRule()
	oldVlanRule.IifName = hostVethName
	oldVlanRule.Priority = vlanRulePriority
	// loop is required to clean up all existing rules created on the host (when pod with same name are recreated multiple times)
	for {
		if err := os.netLink.RuleDel(oldVlanRule); err != nil {
			if !containsNoSuchRule(err) {
				return errors.Wrapf(err, "SetupPodENINetwork: failed to delete hostveth rule for %s", hostVeth.Attrs().Name)
			}
			break
		}
	}

	// 2. add new vlan link
	err = os.netLink.LinkAdd(vlanLink)
	if err != nil {
		return errors.Wrapf(err, "SetupPodENINetwork: failed to add vlan link.")
	}

	// 3. bring up the vlan
	if err = os.netLink.LinkSetUp(vlanLink); err != nil {
		return errors.Wrapf(err, "SetupPodENINetwork: failed to set link %q up", vlanLink.Name)
	}

	// 4. create default routes for vlan
	routes := buildRoutesForVlan(vlanTableID, vlanLink.Index, net.ParseIP(subnetGW))
	for _, r := range routes {
		if err := os.netLink.RouteReplace(&r); err != nil {
			return errors.Wrapf(err, "SetupPodENINetwork: unable to replace route entry %s via %s", r.Dst.IP.String(), subnetGW)
		}
	}

	var addr *net.IPNet
	if v4Addr != nil {
		addr = v4Addr
	} else if v6Addr != nil {
		addr = v6Addr
	}

	// 5. create route entry for hostveth.
	route := netlink.Route{
		LinkIndex: hostVeth.Attrs().Index,
		Scope:     netlink.SCOPE_LINK,
		Dst:       addr,
		Table:     vlanTableID,
	}
	if err := os.netLink.RouteReplace(&route); err != nil {
		return errors.Wrapf(err, "SetupPodENINetwork: unable to add or replace route entry for %s", route.Dst.IP.String())
	}

	log.Debugf("Successfully set host route to be %s/0", route.Dst.IP.String())

	// 6. Add ip rules for the pod.
	vlanRule := os.netLink.NewRule()
	vlanRule.Table = vlanTableID
	vlanRule.Priority = vlanRulePriority
	vlanRule.IifName = vlanLink.Name
	err = os.netLink.RuleAdd(vlanRule)
	if err != nil && !isRuleExistsError(err) {
		return errors.Wrapf(err, "SetupPodENINetwork: unable to add ip rule for vlan link %s ", vlanLink.Name)
	}

	vlanRule.IifName = hostVeth.Attrs().Name
	err = os.netLink.RuleAdd(vlanRule)
	if err != nil && !isRuleExistsError(err) {
		return errors.Wrapf(err, "SetupPodENINetwork: unable to add ip rule for host veth %s", hostVethName)
	}
	return nil
}

// buildRoutesForVlan builds routes required for the vlan link.
func buildRoutesForVlan(vlanTableID int, vlanIndex int, gw net.IP) []netlink.Route {
	return []netlink.Route{
		// Add a direct link route for the pod vlan link only.
		{
			LinkIndex: vlanIndex,
			Dst:       &net.IPNet{IP: gw, Mask: net.CIDRMask(32, 32)},
			Scope:     netlink.SCOPE_LINK,
			Table:     vlanTableID,
		},
		{
			LinkIndex: vlanIndex,
			Dst:       &net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(0, 32)},
			Scope:     netlink.SCOPE_UNIVERSE,
			Gw:        gw,
			Table:     vlanTableID,
		},
	}
}

// buildVlanLink builds vlan link for the pod.
func buildVlanLink(vlanID int, parentIfIndex int, eniMAC string) *netlink.Vlan {
	la := netlink.NewLinkAttrs()
	la.Name = fmt.Sprintf("vlan.eth.%d", vlanID)
	la.ParentIndex = parentIfIndex
	la.HardwareAddr, _ = net.ParseMAC(eniMAC)
	return &netlink.Vlan{LinkAttrs: la, VlanId: vlanID}
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
func (os *linuxNetwork) TeardownNS(addr *net.IPNet, deviceNumber int, log logger.Logger) error {
	log.Debugf("TeardownNS: addr %s, deviceNumber %d", addr.String(), deviceNumber)
	return tearDownNS(addr, deviceNumber, os.netLink, log)
}

func tearDownNS(addr *net.IPNet, deviceNumber int, netLink netlinkwrapper.NetLink, log logger.Logger) error {
	if addr == nil {
		return errors.New("can't tear down network namespace with no IP address")
	}
	// Remove to-pod rule
	toContainerRule := netLink.NewRule()
	toContainerRule.Dst = addr
	toContainerRule.Priority = toContainerRulePriority
	err := netLink.RuleDel(toContainerRule)

	if err != nil {
		log.Errorf("Failed to delete toContainer rule for %s err %v", addr.String(), err)
	} else {
		log.Infof("Delete toContainer rule for %s ", addr.String())
	}

	if deviceNumber > 0 {
		// remove from-pod rule only for non main table
		err := deleteRuleListBySrc(*addr)
		if err != nil {
			log.Errorf("Failed to delete fromContainer for %s %v", addr.String(), err)
			return errors.Wrapf(err, "delete NS network: failed to delete fromContainer rule for %s", addr.String())
		}
		tableNumber := deviceNumber + 1
		log.Infof("Delete fromContainer rule for %s in table %d", addr.String(), tableNumber)
	}

	addrHostAddr := &net.IPNet{
		IP:   addr.IP,
		Mask: net.CIDRMask(32, 32)}

	// cleanup host route:
	if err = netLink.RouteDel(&netlink.Route{
		Scope: netlink.SCOPE_LINK,
		Dst:   addrHostAddr}); err != nil {
		log.Errorf("delete NS network: failed to delete host route for %s, %v", addr.String(), err)
	}
	log.Debug("Tear down of NS complete")
	return nil
}

// TeardownPodENINetwork tears down the vlan and corresponding ip rules.
func (os *linuxNetwork) TeardownPodENINetwork(vlanID int, log logger.Logger) error {
	log.Infof("Tear down of pod ENI namespace")

	// 1. delete vlan
	if vlan, err := os.netLink.LinkByName(fmt.Sprintf("vlan.eth.%d",
		vlanID)); err == nil {
		err := os.netLink.LinkDel(vlan)
		if err != nil {
			return errors.Wrapf(err, "TeardownPodENINetwork: failed to delete vlan link for %d", vlanID)
		}
	}

	// 2. delete two ip rules associated with the vlan
	vlanRule := os.netLink.NewRule()
	vlanRule.Table = vlanID + 100
	vlanRule.Priority = vlanRulePriority

	for {
		// Loop until both the rules are deleted.
		// one of them handles vlan traffic and other is for pod host veth traffic.
		if err := os.netLink.RuleDel(vlanRule); err != nil {
			if !containsNoSuchRule(err) {
				return errors.Wrapf(err, "TeardownPodENINetwork: failed to delete container rule for %d", vlanID)
			}
			break
		}
	}
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
