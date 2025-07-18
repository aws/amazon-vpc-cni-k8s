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
	"crypto/rand"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/sgpp"

	"github.com/pkg/errors"
	"golang.org/x/sys/unix"

	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/vishvananda/netlink"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/ipwrapper"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/netlinkwrapper"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/networkutils"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/nswrapper"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/procsyswrapper"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/cniutils"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
)

const (
	WAIT_INTERVAL = 50 * time.Millisecond

	//Time duration CNI waits for an IPv6 address assigned to an interface
	//to move to stable state before error'ing out.
	v6DADTimeout = 10 * time.Second
)

type VirtualInterfaceMetadata struct {
	IPAddress         *net.IPNet
	DeviceNumber      int
	RouteTable        int
	HostVethName      string
	ContainerVethName string
}

// NetworkAPIs defines network API calls
type NetworkAPIs interface {
	// SetupPodNetwork sets up pod network for normal ENI based pods
	SetupPodNetwork(vethMetadata []VirtualInterfaceMetadata, netnsPath string, mtu int, log logger.Logger) error
	// TeardownPodNetwork clean up pod network for normal ENI based pods
	TeardownPodNetwork(vethMetadata []VirtualInterfaceMetadata, log logger.Logger) error
	// SetupBranchENIPodNetwork sets up pod network for branch ENI based pods
	SetupBranchENIPodNetwork(vethMetadata VirtualInterfaceMetadata, netnsPath string, vlanID int, eniMAC string,
		subnetGW string, parentIfIndex int, mtu int, podSGEnforcingMode sgpp.EnforcingMode, log logger.Logger) error
	// TeardownBranchENIPodNetwork cleans up pod network for branch ENI based pods
	TeardownBranchENIPodNetwork(vethMetadata VirtualInterfaceMetadata, vlanID int, podSGEnforcingMode sgpp.EnforcingMode, log logger.Logger) error
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
	ipAddr       *net.IPNet
	netLink      netlinkwrapper.NetLink
	ip           ipwrapper.IP
	mtu          int
	procSys      procsyswrapper.ProcSys
	index        int
	log          logger.Logger
	hostMACAddr  net.HardwareAddr
}

func newCreateVethPairContext(contVethName string, hostVethName string, ipAddr *net.IPNet, mtu int, index int, hostMACAddr net.HardwareAddr, log logger.Logger) *createVethPairContext {
	return &createVethPairContext{
		contVethName: contVethName,
		hostVethName: hostVethName,
		ipAddr:       ipAddr,
		netLink:      netlinkwrapper.NewNetLink(),
		ip:           ipwrapper.NewIP(),
		mtu:          mtu,
		procSys:      procsyswrapper.NewProcSys(),
		index:        index,
		hostMACAddr:  hostMACAddr,
		log:          log,
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
		PeerName:         createVethContext.hostVethName,
		PeerHardwareAddr: createVethContext.hostMACAddr,
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

	// this means it's a V6 IP address
	if createVethContext.ipAddr.IP.To4() == nil {
		// Enable v6 support on Container's veth interface.
		if err = createVethContext.procSys.Set(fmt.Sprintf("net/ipv6/conf/%s/disable_ipv6", createVethContext.contVethName), "0"); err != nil {
			if !os.IsNotExist(err) {
				return errors.Wrapf(err, "setupVeth network: failed to enable IPv6 on container veth interface")
			}
		}

		// Enable v6 support on Container's lo interface inside the Pod networking namespace.
		if err = createVethContext.procSys.Set("net/ipv6/conf/lo/disable_ipv6", "0"); err != nil {
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
	var family int

	if networkutils.IsIPv4(createVethContext.ipAddr.IP) {
		gw = networkutils.CalculatePodIPv4GatewayIP(createVethContext.index)
		maskLen = 32
		addr = &netlink.Addr{IPNet: createVethContext.ipAddr}
		defNet = &net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(0, maskLen)}
		family = netlink.FAMILY_V4
	} else {
		gw = networkutils.CalculatePodIPv6GatewayIP(createVethContext.index)
		maskLen = 128
		addr = &netlink.Addr{IPNet: createVethContext.ipAddr}
		defNet = &net.IPNet{IP: net.IPv6zero, Mask: net.CIDRMask(0, maskLen)}
		family = netlink.FAMILY_V6
	}

	gwNet := &net.IPNet{IP: gw, Mask: net.CIDRMask(maskLen, maskLen)}

	// If Index  > 0 that means it has multiple IPs. Add IP rule + add default route to
	rtTable := unix.RT_TABLE_MAIN
	if createVethContext.index > 0 {
		rtTable = createVethContext.index
	}

	if err = createVethContext.netLink.RouteReplace(&netlink.Route{
		LinkIndex: contVeth.Attrs().Index,
		Scope:     netlink.SCOPE_LINK,
		Dst:       gwNet,
		Table:     rtTable}); err != nil {
		return errors.Wrap(err, "setup NS network: failed to add default gateway")
	}

	if createVethContext.index > 0 {
		// Add a from interface rule
		fromInterfaceRule := createVethContext.netLink.NewRule()
		fromInterfaceRule.Src = createVethContext.ipAddr
		fromInterfaceRule.Priority = networkutils.FromInterfaceRulePriority
		fromInterfaceRule.Table = rtTable
		fromInterfaceRule.Family = family
		if err := createVethContext.netLink.RuleAdd(fromInterfaceRule); err != nil && !networkutils.IsRuleExistsError(err) {
			return errors.Wrapf(err, "failed to setup fromInterface rule, containerAddr=%s, rtTable=%v", createVethContext.ipAddr.String(), createVethContext.index)
		}
	}

	// Add a default route via dummy next hop(169.254.1.1 or fe80::1). Then all outgoing traffic will be routed by this
	// default route via dummy next hop (169.254.1.1 or fe80::1)
	if err = createVethContext.netLink.RouteAdd(&netlink.Route{
		LinkIndex: contVeth.Attrs().Index,
		Scope:     netlink.SCOPE_UNIVERSE,
		Dst:       defNet,
		Gw:        gw,
		Table:     rtTable,
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
	// if IP is not IPv4 or a v4 in v6 address, it return nil
	if !networkutils.IsIPv4(createVethContext.ipAddr.IP) {
		if err := cniutils.WaitForAddressesToBeStable(createVethContext.netLink, createVethContext.contVethName, v6DADTimeout, WAIT_INTERVAL); err != nil {
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

// SetupPodNetwork wires up linux networking for a pod's network
// we expect v4Addr and v6Addr to have correct IPAddress Family.
func (n *linuxNetwork) SetupPodNetwork(vethMetadata []VirtualInterfaceMetadata, netnsPath string, mtu int, log logger.Logger) error {
	for index, vethData := range vethMetadata {

		log.Debugf("SetupPodNetwork: hostVethName=%s, contVethName=%s, netnsPath=%s, ipAddr=%v, routeTableNumber=%d, mtu=%d",
			vethData.HostVethName, vethData.ContainerVethName, netnsPath, vethData.IPAddress, vethData.RouteTable, mtu)

		hostVeth, err := n.setupVeth(vethData.HostVethName, vethData.ContainerVethName, netnsPath, vethData.IPAddress, mtu, log, index)
		if err != nil {
			return errors.Wrapf(err, "SetupPodNetwork: failed to setup veth pair")
		}

		rtTable := unix.RT_TABLE_MAIN
		if vethData.RouteTable > 1 {
			rtTable = vethData.RouteTable
		}

		if err := n.setupIPBasedContainerRouteRules(hostVeth, vethData.IPAddress, rtTable, log); err != nil {
			return errors.Wrapf(err, "SetupPodNetwork: unable to setup IP based container routes and rules")
		}
	}
	return nil
}

// TeardownPodNetwork cleanup ip rules
func (n *linuxNetwork) TeardownPodNetwork(vethMetadata []VirtualInterfaceMetadata, log logger.Logger) error {

	for _, vethData := range vethMetadata {

		log.Debugf("TeardownPodNetwork: containerAddr=%s, routeTable=%d", vethData.IPAddress.String(), vethData.RouteTable)

		// Route table ID for primary ENI (Network 0, Device 0) => (0* MaxENI + 0 + 1)
		// which is why we only update if the RT > 1
		rtTable := unix.RT_TABLE_MAIN
		if vethData.RouteTable > 1 {
			rtTable = vethData.RouteTable
		}

		if err := n.teardownIPBasedContainerRouteRules(vethData.IPAddress, rtTable, log); err != nil {
			return errors.Wrapf(err, "TeardownPodNetwork: unable to teardown IP based container routes and rules")
		}
	}

	return nil
}

// SetupBranchENIPodNetwork sets up the network ns for pods requesting its own security group
// we expect v4Addr and v6Addr to have correct IPAddress Family.
func (n *linuxNetwork) SetupBranchENIPodNetwork(vethMetadata VirtualInterfaceMetadata, netnsPath string,
	vlanID int, eniMAC string, subnetGW string, parentIfIndex int, mtu int, podSGEnforcingMode sgpp.EnforcingMode, log logger.Logger) error {

	log.Debugf("SetupBranchENIPodNetwork: hostVethName=%s, contVethName=%s, netnsPath=%s, ipAddr=%v, vlanID=%d, eniMAC=%s, subnetGW=%s, parentIfIndex=%d, mtu=%d, podSGEnforcingMode=%v",
		vethMetadata.HostVethName, vethMetadata.ContainerVethName, netnsPath, vethMetadata.IPAddress, vlanID, eniMAC, subnetGW, parentIfIndex, mtu, podSGEnforcingMode)

	hostVeth, err := n.setupVeth(vethMetadata.HostVethName, vethMetadata.ContainerVethName, netnsPath, vethMetadata.IPAddress, mtu, log, 0)
	if err != nil {
		return errors.Wrapf(err, "SetupBranchENIPodNetwork: failed to setup veth pair")
	}

	// clean up any previous hostVeth ip rule recursively. (when pod with same name are recreated multiple times).
	//
	// per our understanding, previous we obtain vlanID from pod spec, it could be possible the vlanID is already updated when deleting old pod, thus the hostVeth been cleaned up during oldPod deletion is incorrect.
	// now since we obtain vlanID from prevResult during pod deletion, we should be able to correctly purge hostVeth during pod deletion and thus don't need this logic.
	// this logic is kept here for safety purpose.
	oldFromHostVethRule := n.netLink.NewRule()
	oldFromHostVethRule.IifName = vethMetadata.HostVethName
	oldFromHostVethRule.Priority = networkutils.VlanRulePriority

	// If IPv4 it returns the IP address back
	if vethMetadata.IPAddress.IP.To4() == nil {
		oldFromHostVethRule.Family = unix.AF_INET6
	}
	if err := networkutils.NetLinkRuleDelAll(n.netLink, oldFromHostVethRule); err != nil {
		return errors.Wrapf(err, "SetupBranchENIPodNetwork: failed to delete hostVeth rule for %s", vethMetadata.HostVethName)
	}

	rtTable := vlanID + 100
	vlanLink, err := n.setupVlan(vlanID, eniMAC, subnetGW, parentIfIndex, rtTable, log)
	if err != nil {
		return errors.Wrapf(err, "SetupBranchENIPodNetwork: failed to setup vlan")
	}

	switch podSGEnforcingMode {
	case sgpp.EnforcingModeStrict:
		if err := n.setupIIFBasedContainerRouteRules(hostVeth, vethMetadata.IPAddress, vlanLink, rtTable, log); err != nil {
			return errors.Wrapf(err, "SetupBranchENIPodNetwork: unable to setup IIF based container routes and rules")
		}
	case sgpp.EnforcingModeStandard:
		if err := n.setupIPBasedContainerRouteRules(hostVeth, vethMetadata.IPAddress, rtTable, log); err != nil {
			return errors.Wrapf(err, "SetupBranchENIPodNetwork: unable to setup IP based container routes and rules")
		}
	}
	return nil
}

// TeardownBranchENIPodNetwork tears down the vlan and corresponding ip rules.
func (n *linuxNetwork) TeardownBranchENIPodNetwork(vethMetadata VirtualInterfaceMetadata, vlanID int, _ sgpp.EnforcingMode, log logger.Logger) error {
	log.Debugf("TeardownBranchENIPodNetwork: containerAddr=%s, vlanID=%d", vethMetadata.IPAddress.String(), vlanID)

	if err := n.teardownVlan(vlanID, log); err != nil {
		return errors.Wrapf(err, "TeardownBranchENIPodNetwork: failed to teardown vlan")
	}

	ipFamily := unix.AF_INET
	if !networkutils.IsIPv4(vethMetadata.IPAddress.IP) {
		ipFamily = unix.AF_INET6
	}
	// to handle the migration between different enforcingMode, we try to clean up rules under both mode since the pod might be setup with a different mode.
	rtTable := vlanID + 100
	if err := n.teardownIIFBasedContainerRouteRules(rtTable, ipFamily, log); err != nil {
		return errors.Wrapf(err, "TeardownBranchENIPodNetwork: unable to teardown IIF based container routes and rules")
	}
	if err := n.teardownIPBasedContainerRouteRules(vethMetadata.IPAddress, rtTable, log); err != nil {
		return errors.Wrapf(err, "TeardownBranchENIPodNetwork: unable to teardown IP based container routes and rules")
	}

	return nil
}

// setupVeth sets up veth for the pod.
func (n *linuxNetwork) setupVeth(hostVethName string, contVethName string, netnsPath string, ipAddr *net.IPNet, mtu int, log logger.Logger, index int) (netlink.Link, error) {
	// Clean up if hostVeth exists.
	if oldHostVeth, err := n.netLink.LinkByName(hostVethName); err == nil {
		if err = n.netLink.LinkDel(oldHostVeth); err != nil {
			return nil, errors.Wrapf(err, "failed to delete old hostVeth %s", hostVethName)
		}
		log.Debugf("Successfully deleted old hostVeth %s", hostVethName)
	}
	macAddrStr, err := NewMACGenerator().generateUniqueRandomMAC()
	if err != nil {
		return nil, errors.Wrap(err, "failed to generate Unique MAC addr for host side veth")
	}
	macAddr, err := net.ParseMAC(macAddrStr)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse unique generated mac addr %s", macAddrStr)
	}
	createVethContext := newCreateVethPairContext(contVethName, hostVethName, ipAddr, mtu, index, macAddr, log)
	if err := n.ns.WithNetNSPath(netnsPath, createVethContext.run); err != nil {
		return nil, errors.Wrap(err, "failed to setup veth network")
	}

	hostVeth, err := n.netLink.LinkByName(hostVethName)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to find hostVeth %s", hostVethName)
	}

	// For IPv6, host veth sysctls must be set to:
	// 1. accept_ra=0
	// 2. accept_redirects=1
	// 3. forwarding=0
	if err := n.procSys.Set(fmt.Sprintf("net/ipv6/conf/%s/accept_ra", hostVethName), "0"); err != nil {
		if !os.IsNotExist(err) {
			return nil, errors.Wrapf(err, "failed to disable IPv6 router advertisements")
		}
		log.Debugf("Ignoring '%v' writing to accept_ra: Assuming kernel lacks IPv6 support", err)
	}
	if err := n.procSys.Set(fmt.Sprintf("net/ipv6/conf/%s/accept_redirects", hostVethName), "1"); err != nil {
		if !os.IsNotExist(err) {
			return nil, errors.Wrapf(err, "failed to disable IPv6 ICMP redirects")
		}
		log.Debugf("Ignoring '%v' writing to accept_redirects: Assuming kernel lacks IPv6 support", err)
	}
	if err := n.procSys.Set(fmt.Sprintf("net/ipv6/conf/%s/forwarding", hostVethName), "0"); err != nil {
		if !os.IsNotExist(err) {
			return nil, errors.Wrapf(err, "failed to disable IPv6 forwarding")
		}
		log.Debugf("Ignoring '%v' writing to forwarding: Assuming kernel lacks IPv6 support", err)
	}
	log.Debugf("Successfully set IPv6 sysctls on hostVeth %s", hostVethName)

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

	// 3. Set IPv6 sysctls
	//    accept_ra=0
	//    accept_redirects=1
	//    forwarding=0
	if err := n.procSys.Set(fmt.Sprintf("net/ipv6/conf/%s/accept_ra", vlanLinkName), "0"); err != nil {
		if !os.IsNotExist(err) {
			return nil, errors.Wrapf(err, "failed to disable IPv6 router advertisements")
		}
		log.Debugf("Ignoring '%v' writing to accept_ra: Assuming kernel lacks IPv6 support", err)
	}

	if err := n.procSys.Set(fmt.Sprintf("net/ipv6/conf/%s/accept_redirects", vlanLinkName), "1"); err != nil {
		if !os.IsNotExist(err) {
			return nil, errors.Wrapf(err, "failed to enable IPv6 ICMP redirects")
		}
		log.Debugf("Ignoring '%v' writing to accept_redirects: Assuming kernel lacks IPv6 support", err)
	}

	if err := n.procSys.Set(fmt.Sprintf("net/ipv6/conf/%s/forwarding", vlanLinkName), "0"); err != nil {
		if !os.IsNotExist(err) {
			return nil, errors.Wrapf(err, "failed to disable IPv6 forwarding")
		}
		log.Debugf("Ignoring '%v' writing to forwarding: Assuming kernel lacks IPv6 support", err)
	}

	// 4. bring up the vlan
	if err := n.netLink.LinkSetUp(vlanLink); err != nil {
		return nil, errors.Wrapf(err, "failed to setUp vlan link %s", vlanLinkName)
	}

	// 5. create default routes for vlan
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
	toContainerRule.Priority = networkutils.ToContainerRulePriority
	toContainerRule.Table = unix.RT_TABLE_MAIN
	if err := n.netLink.RuleAdd(toContainerRule); err != nil && !networkutils.IsRuleExistsError(err) {
		return errors.Wrapf(err, "failed to setup toContainer rule, containerAddr=%s, rtTable=%v", containerAddr.String(), "main")
	}

	log.Debugf("Successfully setup toContainer rule, containerAddr=%s, rtTable=%v", containerAddr.String(), "main")

	if rtTable != unix.RT_TABLE_MAIN {
		fromContainerRule := n.netLink.NewRule()
		fromContainerRule.Src = containerAddr
		fromContainerRule.Priority = networkutils.FromPodRulePriority
		fromContainerRule.Table = rtTable
		if err := n.netLink.RuleAdd(fromContainerRule); err != nil && !networkutils.IsRuleExistsError(err) {
			return errors.Wrapf(err, "failed to setup fromContainer rule, containerAddr=%s, rtTable=%v", containerAddr.String(), rtTable)
		}
		log.Debugf("Successfully setup fromContainer rule, containerAddr=%s, rtTable=%v", containerAddr.String(), rtTable)
	}

	return nil
}

func (n *linuxNetwork) teardownIPBasedContainerRouteRules(containerAddr *net.IPNet, rtTable int, log logger.Logger) error {
	toContainerRule := n.netLink.NewRule()
	toContainerRule.Dst = containerAddr
	toContainerRule.Priority = networkutils.ToContainerRulePriority
	toContainerRule.Table = unix.RT_TABLE_MAIN
	if err := n.netLink.RuleDel(toContainerRule); err != nil && !networkutils.ContainsNoSuchRule(err) {
		return errors.Wrapf(err, "failed to delete toContainer rule, containerAddr=%s, rtTable=%v", containerAddr.String(), "main")
	}
	log.Debugf("Successfully deleted toContainer rule, containerAddr=%s, rtTable=%v", containerAddr.String(), "main")

	if rtTable != unix.RT_TABLE_MAIN {
		fromContainerRule := netlink.NewRule()
		fromContainerRule.Src = containerAddr
		fromContainerRule.Priority = networkutils.FromPodRulePriority
		fromContainerRule.Table = rtTable

		// note: older version CNI sets up multiple CIDR based from container rule, so we recursively delete them to be backwards-compatible.
		if err := networkutils.NetLinkRuleDelAll(n.netLink, fromContainerRule); err != nil {
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
	isV6 := !networkutils.IsIPv4(containerAddr.IP)
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
	fromHostVlanRule.Priority = networkutils.VlanRulePriority
	fromHostVlanRule.Table = rtTable
	if isV6 {
		fromHostVlanRule.Family = unix.AF_INET6
	}
	if err := n.netLink.RuleAdd(fromHostVlanRule); err != nil && !networkutils.IsRuleExistsError(err) {
		return errors.Wrapf(err, "unable to setup fromHostVlan rule, hostVlan=%s, rtTable=%v", hostVlan.Attrs().Name, rtTable)
	}
	log.Debugf("Successfully setup fromHostVlan rule, hostVlan=%s, rtTable=%v", hostVlan.Attrs().Name, rtTable)

	fromHostVethRule := n.netLink.NewRule()
	fromHostVethRule.IifName = hostVeth.Attrs().Name
	fromHostVethRule.Priority = networkutils.VlanRulePriority
	fromHostVethRule.Table = rtTable
	if isV6 {
		fromHostVethRule.Family = unix.AF_INET6
	}
	if err := n.netLink.RuleAdd(fromHostVethRule); err != nil && !networkutils.IsRuleExistsError(err) {
		return errors.Wrapf(err, "unable to setup fromHostVeth rule, hostVeth=%s, rtTable=%v", hostVeth.Attrs().Name, rtTable)
	}
	log.Debugf("Successfully setup fromHostVeth rule, hostVeth=%s, rtTable=%v", hostVeth.Attrs().Name, rtTable)

	return nil
}

func (n *linuxNetwork) teardownIIFBasedContainerRouteRules(rtTable int, family int, log logger.Logger) error {
	rule := n.netLink.NewRule()
	rule.Priority = networkutils.VlanRulePriority
	rule.Table = rtTable
	rule.Family = family

	if err := networkutils.NetLinkRuleDelAll(n.netLink, rule); err != nil {
		return errors.Wrapf(err, "failed to delete IIF based rules, rtTable=%v", rtTable)
	}
	log.Debugf("Successfully deleted IIF based rules, rtTable=%v", rtTable)

	return nil
}

// buildRoutesForVlan builds routes required for the vlan link.
func buildRoutesForVlan(vlanTableID int, vlanIndex int, gw net.IP) []netlink.Route {
	maskLen := 32
	zeroAddr := net.IPv4zero
	if gw.To4() == nil {
		maskLen = 128
		zeroAddr = net.IPv6zero
	}
	return []netlink.Route{
		// Add a direct link route for the pod vlan link only.
		{
			LinkIndex: vlanIndex,
			Dst:       &net.IPNet{IP: gw, Mask: net.CIDRMask(maskLen, maskLen)},
			Scope:     netlink.SCOPE_LINK,
			Table:     vlanTableID,
		},
		{
			LinkIndex: vlanIndex,
			Dst:       &net.IPNet{IP: zeroAddr, Mask: net.CIDRMask(0, maskLen)},
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

type MACGenerator struct {
	netlink   netlinkwrapper.NetLink
	randMACfn func() string
}

func NewMACGenerator() MACGenerator {
	return MACGenerator{netlink: netlinkwrapper.NewNetLink(), randMACfn: generateRandomMAC}
}

func generateRandomMAC() string {
	mac := make([]byte, 6)
	rand.Read(mac)
	// Set the local bit and unset the multicast bit
	mac[0] = (mac[0] | 2) & 0xfe

	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x",
		mac[0], mac[1], mac[2], mac[3], mac[4], mac[5])
}

// generateUniqueRandomMAC will compare randomly generated Mac to mac addresses of veth already present in host.
func (m MACGenerator) generateUniqueRandomMAC() (string, error) {
	ll, err := m.netlink.LinkList()
	if err != nil {
		return "", err
	}
	macMap := make(map[string]struct{})
	for _, link := range ll {
		if link.Attrs() != nil {
			macMap[link.Attrs().HardwareAddr.String()] = struct{}{}
		}
	}

	for i := 0; i < 10; i++ {
		macAttempt := m.randMACfn()
		if _, ok := macMap[macAttempt]; !ok {
			return macAttempt, nil
		}
	}
	return "", errors.New("failed to generate unique mac after 10 attempts.")
}
