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
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/sgpp"

	"github.com/pkg/errors"
	"golang.org/x/sys/unix"

	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/vishvananda/netlink"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/ipwrapper"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/netlinkwrapper"
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

	WAIT_INTERVAL = 50 * time.Millisecond

	//Time duration CNI waits for an IPv6 address assigned to an interface
	//to move to stable state before error'ing out.
	v6DADTimeout = 10 * time.Second
)

// NetworkAPIs defines network API calls
type NetworkAPIs interface {
	// SetupPodNetwork sets up pod network for normal ENI based pods
	SetupPodNetwork(hostVethName string, contVethName string, netnsPath string, v4Addr *net.IPNet, v6Addr *net.IPNet, deviceNumber int, mtu int, log logger.Logger) error
	// TeardownPodNetwork clean up pod network for normal ENI based pods
	TeardownPodNetwork(containerAddr *net.IPNet, deviceNumber int, log logger.Logger) error

	// SetupBranchENIPodNetwork sets up pod network for branch ENI based pods
	SetupBranchENIPodNetwork(hostVethName string, contVethName string, netnsPath string, v4Addr *net.IPNet, v6Addr *net.IPNet, vlanID int, eniMAC string,
		subnetGW string, parentIfIndex int, mtu int, podSGEnforcingMode sgpp.EnforcingMode, log logger.Logger) error
	// TeardownBranchENIPodNetwork cleans up pod network for branch ENI based pods
	TeardownBranchENIPodNetwork(containerAddr *net.IPNet, vlanID int, podSGEnforcingMode sgpp.EnforcingMode, log logger.Logger) error
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
		// Enable v6 support on Container's veth interface.
		if err = createVethContext.procSys.Set(fmt.Sprintf("net/ipv6/conf/%s/disable_ipv6", createVethContext.contVethName), "0"); err != nil {
			if !os.IsNotExist(err) {
				return errors.Wrapf(err, "setupVeth network: failed to enable IPv6 on container veth interface")
			}
		}

		// Enable v6 support on Container's lo interface inside the Pod networking namespace.
		if err = createVethContext.procSys.Set(fmt.Sprintf("net/ipv6/conf/lo/disable_ipv6"), "0"); err != nil {
			if !os.IsNotExist(err) {
				return errors.Wrapf(err, "setupVeth network: failed to enable IPv6 on container's lo interface")
			}
		}
	}

	// Add a connected route to a dummy next hop (169.254.1.1 or fe80::1)
	// # ip route show
	// default via 169.254.1.1 dev eth0
	// 169.254.1.1 dev eth0

	var gw net.IP
	var maskLen int
	var addr *netlink.Addr
	var defNet *net.IPNet

	if createVethContext.v4Addr != nil {
		gw = net.IPv4(169, 254, 1, 1)
		maskLen = 32
		addr = &netlink.Addr{IPNet: createVethContext.v4Addr}
		defNet = &net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(0, maskLen)}
	} else if createVethContext.v6Addr != nil {
		gw = net.IP{0xfe, 0x80, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}
		maskLen = 128
		addr = &netlink.Addr{IPNet: createVethContext.v6Addr}
		defNet = &net.IPNet{IP: net.IPv6zero, Mask: net.CIDRMask(0, maskLen)}
	}

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

	if createVethContext.v6Addr != nil && createVethContext.v6Addr.IP.To16() != nil {
		if err := waitForAddressesToBeStable(createVethContext.netLink, createVethContext.contVethName, v6DADTimeout); err != nil {
			return errors.Wrap(err, "setup NS network: failed while waiting for v6 addresses to be stable")
		}
	}

	// Now that the everything has been successfully set up in the container, move the "host" end of the
	// veth into the host namespace.
	if err = createVethContext.netLink.LinkSetNsFd(hostVeth, int(hostNS.Fd())); err != nil {
		return errors.Wrap(err, "setup NS network: failed to move veth to host netns")
	}
	return nil
}

// Implements `SettleAddresses` functionality of the `ip` package.
// waitForAddressesToBeStable waits for all addresses on a link to leave tentative state.
// Will be particularly useful for ipv6, where all addresses need to do DAD.
// If any addresses are still tentative after timeout seconds, then error.
func waitForAddressesToBeStable(netLink netlinkwrapper.NetLink, ifName string, timeout time.Duration) error {
	link, err := netLink.LinkByName(ifName)
	if err != nil {
		return fmt.Errorf("failed to retrieve link: %v", err)
	}

	deadline := time.Now().Add(timeout)
	for {
		addrs, err := netLink.AddrList(link, netlink.FAMILY_V6)
		if err != nil {
			return fmt.Errorf("could not list addresses: %v", err)
		}

		ok := true
		for _, addr := range addrs {
			if addr.Flags&(syscall.IFA_F_TENTATIVE|syscall.IFA_F_DADFAILED) > 0 {
				ok = false
				break
			}
		}

		if ok {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("link %s still has tentative addresses after %d seconds",
				ifName,
				timeout)
		}

		time.Sleep(WAIT_INTERVAL)
	}
}

// SetupPodNetwork wires up linux networking for a pod's network
// we expect v4Addr and v6Addr to have correct IPAddress Family.
func (n *linuxNetwork) SetupPodNetwork(hostVethName string, contVethName string, netnsPath string, v4Addr *net.IPNet, v6Addr *net.IPNet,
	deviceNumber int, mtu int, log logger.Logger) error {
	log.Debugf("SetupPodNetwork: hostVethName=%s, contVethName=%s, netnsPath=%s, v4Addr=%v, v6Addr=%v, deviceNumber=%d, mtu=%d",
		hostVethName, contVethName, netnsPath, v4Addr, v6Addr, deviceNumber, mtu)

	hostVeth, err := n.setupVeth(hostVethName, contVethName, netnsPath, v4Addr, v6Addr, mtu, log)
	if err != nil {
		return errors.Wrapf(err, "SetupPodNetwork: failed to setup veth pair")
	}

	var containerAddr *net.IPNet
	if v4Addr != nil {
		containerAddr = v4Addr
	} else if v6Addr != nil {
		containerAddr = v6Addr
	}

	rtTable := unix.RT_TABLE_MAIN
	if deviceNumber > 0 {
		rtTable = deviceNumber + 1
	}
	if err := n.setupIPBasedContainerRouteRules(hostVeth, containerAddr, rtTable, log); err != nil {
		return errors.Wrapf(err, "SetupPodNetwork: unable to setup IP based container routes and rules")
	}
	return nil
}

// TeardownPodNetwork cleanup ip rules
func (n *linuxNetwork) TeardownPodNetwork(containerAddr *net.IPNet, deviceNumber int, log logger.Logger) error {
	log.Debugf("TeardownPodNetwork: containerAddr=%s, deviceNumber=%d", containerAddr.String(), deviceNumber)

	rtTable := unix.RT_TABLE_MAIN
	if deviceNumber > 0 {
		rtTable = deviceNumber + 1
	}
	if err := n.teardownIPBasedContainerRouteRules(containerAddr, rtTable, log); err != nil {
		return errors.Wrapf(err, "TeardownPodNetwork: unable to teardown IP based container routes and rules")
	}
	return nil
}

// SetupBranchENIPodNetwork sets up the network ns for pods requesting its own security group
// we expect v4Addr and v6Addr to have correct IPAddress Family.
func (n *linuxNetwork) SetupBranchENIPodNetwork(hostVethName string, contVethName string, netnsPath string, v4Addr *net.IPNet, v6Addr *net.IPNet,
	vlanID int, eniMAC string, subnetGW string, parentIfIndex int, mtu int, podSGEnforcingMode sgpp.EnforcingMode, log logger.Logger) error {
	log.Debugf("SetupBranchENIPodNetwork: hostVethName=%s, contVethName=%s, netnsPath=%s, v4Addr=%v, v6Addr=%v, vlanID=%d, eniMAC=%s, subnetGW=%s, parentIfIndex=%d, mtu=%d, podSGEnforcingMode=%v",
		hostVethName, contVethName, netnsPath, v4Addr, v6Addr, vlanID, eniMAC, subnetGW, parentIfIndex, mtu, podSGEnforcingMode)

	hostVeth, err := n.setupVeth(hostVethName, contVethName, netnsPath, v4Addr, v6Addr, mtu, log)
	if err != nil {
		return errors.Wrapf(err, "SetupBranchENIPodNetwork: failed to setup veth pair")
	}

	// clean up any previous hostVeth ip rule recursively. (when pod with same name are recreated multiple times).
	//
	// per our understanding, previous we obtain vlanID from pod spec, it could be possible the vlanID is already updated when deleting old pod, thus the hostVeth been cleaned up during oldPod deletion is incorrect.
	// now since we obtain vlanID from prevResult during pod deletion, we should be able to correctly purge hostVeth during pod deletion and thus don't need this logic.
	// this logic is kept here for safety purpose.
	oldFromHostVethRule := n.netLink.NewRule()
	oldFromHostVethRule.IifName = hostVethName
	oldFromHostVethRule.Priority = vlanRulePriority
	if err := netLinkRuleDelAll(n.netLink, oldFromHostVethRule); err != nil {
		return errors.Wrapf(err, "SetupBranchENIPodNetwork: failed to delete hostVeth rule for %s", hostVethName)
	}

	rtTable := vlanID + 100
	vlanLink, err := n.setupVlan(vlanID, eniMAC, subnetGW, parentIfIndex, rtTable, log)
	if err != nil {
		return errors.Wrapf(err, "SetupBranchENIPodNetwork: failed to setup vlan")
	}

	var containerAddr *net.IPNet
	if v4Addr != nil {
		containerAddr = v4Addr
	} else if v6Addr != nil {
		containerAddr = v6Addr
	}

	switch podSGEnforcingMode {
	case sgpp.EnforcingModeStrict:
		if err := n.setupIIFBasedContainerRouteRules(hostVeth, containerAddr, vlanLink, rtTable, log); err != nil {
			return errors.Wrapf(err, "SetupBranchENIPodNetwork: unable to setup IIF based container routes and rules")
		}
	case sgpp.EnforcingModeStandard:
		if err := n.setupIPBasedContainerRouteRules(hostVeth, containerAddr, rtTable, log); err != nil {
			return errors.Wrapf(err, "SetupBranchENIPodNetwork: unable to setup IP based container routes and rules")
		}
	}
	return nil
}

// TeardownBranchENIPodNetwork tears down the vlan and corresponding ip rules.
func (n *linuxNetwork) TeardownBranchENIPodNetwork(containerAddr *net.IPNet, vlanID int, _ sgpp.EnforcingMode, log logger.Logger) error {
	log.Debugf("TeardownBranchENIPodNetwork: containerAddr=%s, vlanID=%d", containerAddr.String(), vlanID)

	if err := n.teardownVlan(vlanID, log); err != nil {
		return errors.Wrapf(err, "TeardownBranchENIPodNetwork: failed to teardown vlan")
	}

	// to handle the migration between different enforcingMode, we try to clean up rules under both mode since the pod might be setup with a different mode.
	rtTable := vlanID + 100
	if err := n.teardownIIFBasedContainerRouteRules(rtTable, log); err != nil {
		return errors.Wrapf(err, "TeardownBranchENIPodNetwork: unable to teardown IIF based container routes and rules")
	}
	if err := n.teardownIPBasedContainerRouteRules(containerAddr, rtTable, log); err != nil {
		return errors.Wrapf(err, "TeardownBranchENIPodNetwork: unable to teardown IP based container routes and rules")
	}

	return nil
}

// setupVeth sets up veth for the pod.
func (n *linuxNetwork) setupVeth(hostVethName string, contVethName string, netnsPath string, v4Addr *net.IPNet, v6Addr *net.IPNet, mtu int, log logger.Logger) (netlink.Link, error) {
	// Clean up if hostVeth exists.
	if oldHostVeth, err := n.netLink.LinkByName(hostVethName); err == nil {
		if err = n.netLink.LinkDel(oldHostVeth); err != nil {
			return nil, errors.Wrapf(err, "failed to delete old hostVeth %s", hostVethName)
		}
		log.Debugf("Successfully deleted old hostVeth %s", hostVethName)
	}

	createVethContext := newCreateVethPairContext(contVethName, hostVethName, v4Addr, v6Addr, mtu)
	if err := n.ns.WithNetNSPath(netnsPath, createVethContext.run); err != nil {
		return nil, errors.Wrap(err, "failed to setup veth network")
	}

	hostVeth, err := n.netLink.LinkByName(hostVethName)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to find hostVeth %s", hostVethName)
	}

	if err := n.procSys.Set(fmt.Sprintf("net/ipv6/conf/%s/accept_ra", hostVethName), "0"); err != nil {
		if !os.IsNotExist(err) {
			return nil, errors.Wrapf(err, "failed to disable IPv6 router advertisements")
		}
		log.Debugf("Ignoring '%v' writing to accept_ra: Assuming kernel lacks IPv6 support", err)
	}

	if err := n.procSys.Set(fmt.Sprintf("net/ipv6/conf/%s/accept_redirects", hostVethName), "0"); err != nil {
		if !os.IsNotExist(err) {
			return nil, errors.Wrapf(err, "failed to disable IPv6 ICMP redirects")
		}
		log.Debugf("Ignoring '%v' writing to accept_redirects: Assuming kernel lacks IPv6 support", err)
	}
	log.Debugf("Successfully disabled IPv6 RA and ICMP redirects on hostVeth %s", hostVethName)

	// Explicitly set the veth to UP state, because netlink doesn't always do that on all the platforms with net.FlagUp.
	// veth won't get a link local address unless it's set to UP state.
	if err = n.netLink.LinkSetUp(hostVeth); err != nil {
		return nil, errors.Wrapf(err, "failed to setup hostVeth %s", hostVethName)
	}
	return hostVeth, nil
}

// setupVlan sets up the vlan interface for branchENI, and configures default routes in specified route table
func (n *linuxNetwork) setupVlan(vlanID int, eniMAC string, subnetGW string, parentIfIndex int, rtTable int, log logger.Logger) (netlink.Link, error) {
	vlanLinkName := buildVlanLinkName(vlanID)
	// 1. clean up if vlan already exists (necessary when trunk ENI changes).
	if oldVlan, err := n.netLink.LinkByName(vlanLinkName); err == nil {
		if err := n.netLink.LinkDel(oldVlan); err != nil {
			return nil, errors.Wrapf(err, "failed to delete old vlan link %s", vlanLinkName)
		}
		log.Debugf("Successfully deleted old vlan link: %s", vlanLinkName)
	}

	// 2. add new vlan link
	vlanLink := buildVlanLink(vlanLinkName, vlanID, parentIfIndex, eniMAC)
	if err := n.netLink.LinkAdd(vlanLink); err != nil {
		return nil, errors.Wrapf(err, "failed to add vlan link %s", vlanLinkName)
	}

	// 3. bring up the vlan
	if err := n.netLink.LinkSetUp(vlanLink); err != nil {
		return nil, errors.Wrapf(err, "failed to setUp vlan link %s", vlanLinkName)
	}

	// 4. create default routes for vlan
	routes := buildRoutesForVlan(rtTable, vlanLink.Index, net.ParseIP(subnetGW))
	for _, r := range routes {
		if err := n.netLink.RouteReplace(&r); err != nil {
			return nil, errors.Wrapf(err, "failed to replace route entry %s via %s", r.Dst.IP.String(), subnetGW)
		}
	}
	return vlanLink, nil
}

func (n *linuxNetwork) teardownVlan(vlanID int, log logger.Logger) error {
	vlanLinkName := buildVlanLinkName(vlanID)
	if vlan, err := n.netLink.LinkByName(vlanLinkName); err == nil {
		if err := n.netLink.LinkDel(vlan); err != nil {
			return errors.Wrapf(err, "failed to delete vlan link %s", vlanLinkName)
		}
		log.Debugf("Successfully deleted vlan link %s", vlanLinkName)
	}
	return nil
}

// setupIPBasedContainerRouteRules setups the routes and route rules for containers based on IP.
// traffic to container(to containerAddr) will be routed via the `main` route table.
// traffic from container(from containerAddr) will be routed via the specified rtTable.
func (n *linuxNetwork) setupIPBasedContainerRouteRules(hostVeth netlink.Link, containerAddr *net.IPNet, rtTable int, log logger.Logger) error {
	route := netlink.Route{
		LinkIndex: hostVeth.Attrs().Index,
		Scope:     netlink.SCOPE_LINK,
		Dst:       containerAddr,
		Table:     unix.RT_TABLE_MAIN,
	}
	if err := n.netLink.RouteReplace(&route); err != nil {
		return errors.Wrapf(err, "failed to setup container route, containerAddr=%s, hostVeth=%s, rtTable=%v",
			containerAddr.String(), hostVeth.Attrs().Name, "main")
	}
	log.Debugf("Successfully setup container route, containerAddr=%s, hostVeth=%s, rtTable=%v",
		containerAddr.String(), hostVeth.Attrs().Name, "main")

	toContainerRule := n.netLink.NewRule()
	toContainerRule.Dst = containerAddr
	toContainerRule.Priority = toContainerRulePriority
	toContainerRule.Table = unix.RT_TABLE_MAIN
	if err := n.netLink.RuleAdd(toContainerRule); err != nil && !isRuleExistsError(err) {
		return errors.Wrapf(err, "failed to setup toContainer rule, containerAddr=%s, rtTable=%v", containerAddr.String(), "main")
	}

	log.Debugf("Successfully setup toContainer rule, containerAddr=%s, rtTable=%v", containerAddr.String(), "main")

	if rtTable != unix.RT_TABLE_MAIN {
		fromContainerRule := n.netLink.NewRule()
		fromContainerRule.Src = containerAddr
		fromContainerRule.Priority = fromContainerRulePriority
		fromContainerRule.Table = rtTable
		if err := n.netLink.RuleAdd(fromContainerRule); err != nil && !isRuleExistsError(err) {
			return errors.Wrapf(err, "failed to setup fromContainer rule, containerAddr=%s, rtTable=%v", containerAddr.String(), rtTable)
		}
		log.Debugf("Successfully setup fromContainer rule, containerAddr=%s, rtTable=%v", containerAddr.String(), rtTable)
	}

	return nil
}

func (n *linuxNetwork) teardownIPBasedContainerRouteRules(containerAddr *net.IPNet, rtTable int, log logger.Logger) error {
	toContainerRule := n.netLink.NewRule()
	toContainerRule.Dst = containerAddr
	toContainerRule.Priority = toContainerRulePriority
	toContainerRule.Table = unix.RT_TABLE_MAIN
	if err := n.netLink.RuleDel(toContainerRule); err != nil && !containsNoSuchRule(err) {
		return errors.Wrapf(err, "failed to delete toContainer rule, containerAddr=%s, rtTable=%v", containerAddr.String(), "main")
	}
	log.Debugf("Successfully deleted toContainer rule, containerAddr=%s, rtTable=%v", containerAddr.String(), "main")

	if rtTable != unix.RT_TABLE_MAIN {
		fromContainerRule := netlink.NewRule()
		fromContainerRule.Src = containerAddr
		fromContainerRule.Priority = fromContainerRulePriority
		fromContainerRule.Table = rtTable

		// note: older version CNI sets up multiple CIDR based from container rule, so we recursively delete them to be backwards-compatible.
		if err := netLinkRuleDelAll(n.netLink, fromContainerRule); err != nil {
			return errors.Wrapf(err, "failed to delete fromContainer rule, containerAddr=%s, rtTable=%v", containerAddr.String(), rtTable)
		}
		log.Debugf("Successfully deleted fromContainer rule, containerAddr=%s, rtTable=%v", containerAddr.String(), rtTable)
	}

	route := netlink.Route{
		Scope: netlink.SCOPE_LINK,
		Dst:   containerAddr,
		Table: unix.RT_TABLE_MAIN,
	}

	// routes will be automatically deleted by kernel when the hostVeth is deleted.
	// we try to delete route and only log a warning even deletion failed.
	if err := n.netLink.RouteDel(&route); err != nil && !netlinkwrapper.IsNotExistsError(err) {
		log.Warnf("failed to delete container route, containerAddr=%s, rtTable=%v: %v", containerAddr.String(), "main", err)
	} else {
		log.Debugf("Successfully deleted container route, containerAddr=%s, rtTable=%v", containerAddr.String(), "main")
	}

	return nil
}

// setupIIFBasedContainerRouteRules setups the routes and route rules for containers based on input network interface.
// traffic to container(iif hostVlan) will be routed via the specified rtTable.
// traffic from container(iif hostVeth) will be routed via the specified rtTable.
func (n *linuxNetwork) setupIIFBasedContainerRouteRules(hostVeth netlink.Link, containerAddr *net.IPNet, hostVlan netlink.Link, rtTable int, log logger.Logger) error {
	route := netlink.Route{
		LinkIndex: hostVeth.Attrs().Index,
		Scope:     netlink.SCOPE_LINK,
		Dst:       containerAddr,
		Table:     rtTable,
	}
	if err := n.netLink.RouteReplace(&route); err != nil {
		return errors.Wrapf(err, "failed to setup container route, containerAddr=%s, hostVeth=%s, rtTable=%v",
			containerAddr.String(), hostVeth.Attrs().Name, rtTable)
	}
	log.Debugf("Successfully setup container route, containerAddr=%s, hostVeth=%s, rtTable=%v",
		containerAddr.String(), hostVeth.Attrs().Name, rtTable)

	fromHostVlanRule := n.netLink.NewRule()
	fromHostVlanRule.IifName = hostVlan.Attrs().Name
	fromHostVlanRule.Priority = vlanRulePriority
	fromHostVlanRule.Table = rtTable
	if err := n.netLink.RuleAdd(fromHostVlanRule); err != nil && !isRuleExistsError(err) {
		return errors.Wrapf(err, "unable to setup fromHostVlan rule, hostVlan=%s, rtTable=%v", hostVlan.Attrs().Name, rtTable)
	}
	log.Debugf("Successfully setup fromHostVlan rule, hostVlan=%s, rtTable=%v", hostVlan.Attrs().Name, rtTable)

	fromHostVethRule := n.netLink.NewRule()
	fromHostVethRule.IifName = hostVeth.Attrs().Name
	fromHostVethRule.Priority = vlanRulePriority
	fromHostVethRule.Table = rtTable
	if err := n.netLink.RuleAdd(fromHostVethRule); err != nil && !isRuleExistsError(err) {
		return errors.Wrapf(err, "unable to setup fromHostVeth rule, hostVeth=%s, rtTable=%v", hostVeth.Attrs().Name, rtTable)
	}
	log.Debugf("Successfully setup fromHostVeth rule, hostVeth=%s, rtTable=%v", hostVeth.Attrs().Name, rtTable)

	return nil
}

func (n *linuxNetwork) teardownIIFBasedContainerRouteRules(rtTable int, log logger.Logger) error {
	rule := n.netLink.NewRule()
	rule.Priority = vlanRulePriority
	rule.Table = rtTable

	if err := netLinkRuleDelAll(n.netLink, rule); err != nil {
		return errors.Wrapf(err, "failed to delete IIF based rules, rtTable=%v", rtTable)
	}
	log.Debugf("Successfully deleted IIF based rules, rtTable=%v", rtTable)

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

// buildVlanLinkName builds the name for vlan link.
func buildVlanLinkName(vlanID int) string {
	return fmt.Sprintf("vlan.eth.%d", vlanID)
}

// buildVlanLink builds vlan link for the pod.
func buildVlanLink(vlanName string, vlanID int, parentIfIndex int, eniMAC string) *netlink.Vlan {
	la := netlink.NewLinkAttrs()
	la.Name = vlanName
	la.ParentIndex = parentIfIndex
	la.HardwareAddr, _ = net.ParseMAC(eniMAC)
	return &netlink.Vlan{LinkAttrs: la, VlanId: vlanID}
}
