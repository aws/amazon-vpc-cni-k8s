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

// Package networkutils is a collection of iptables and netlink functions
package networkutils

import (
	"encoding/csv"
	"fmt"
	"math"
	"net"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/retry"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/pkg/errors"
	"golang.org/x/sys/unix"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"

	"github.com/coreos/go-iptables/iptables"
	"github.com/vishvananda/netlink"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/netlinkwrapper"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/nswrapper"
)

const (
	// Local rule, needs to come after the pod ENI rules
	localRulePriority = 20

	// 513 - 1023, can be used priority lower than toPodRulePriority but higher than default nonVPC CIDR rule

	// 1024 is reserved for (ip rule not to <VPC's subnet> table main)
	hostRulePriority = 1024

	// Main route table
	mainRoutingTable = unix.RT_TABLE_MAIN

	// Local route table
	localRouteTable = unix.RT_TABLE_LOCAL

	// This environment is used to specify whether an external NAT gateway will be used to provide SNAT of
	// secondary ENI IP addresses. If set to "true", the SNAT iptables rule and off-VPC ip rule will not
	// be installed and will be removed if they are already installed. Defaults to false.
	envExternalSNAT = "AWS_VPC_K8S_CNI_EXTERNALSNAT"

	// This environment is used to specify a comma separated list of ipv4 CIDRs to exclude from SNAT. An additional rule
	// will be written to the iptables for each item. If an item is not an ipv4 range it will be skipped.
	// Defaults to empty.
	envExcludeSNATCIDRs = "AWS_VPC_K8S_CNI_EXCLUDE_SNAT_CIDRS"

	// This environment is used to specify weather the SNAT rule added to iptables should randomize port allocation for
	// outgoing connections. If set to "hashrandom" the SNAT iptables rule will have the "--random" flag added to it.
	// Use "prng" if you want to use pseudo random numbers, i.e. "--random-fully".
	// Default is "prng".
	envRandomizeSNAT = "AWS_VPC_K8S_CNI_RANDOMIZESNAT"

	// envConnmark is the name of the environment variable that overrides the default connection mark, used to
	// mark traffic coming from the primary ENI so that return traffic can be forced out of the same interface.
	// Without using a mark, NodePort DNAT and our source-based routing do not work together if the target pod
	// behind the node port is not on the main ENI. In that case, the un-DNAT is done after the source-based
	// routing, resulting in the packet being sent out of the pod's ENI, when the NodePort traffic should be
	// sent over the main ENI.
	envConnmark = "AWS_VPC_K8S_CNI_CONNMARK"

	// defaultConnmark is the default value for the connmark described above. Note: the mark space is a little crowded,
	// - kube-proxy uses 0x0000c000
	// - Calico uses 0xffff0000.
	defaultConnmark = 0x80

	// envMTU gives a way to configure the MTU size for new ENIs attached. Range is from 576 to 9001.
	envMTU = "AWS_VPC_ENI_MTU"

	// envVethPrefix is the environment variable to configure the prefix of the host side veth device names
	envVethPrefix = "AWS_VPC_K8S_CNI_VETHPREFIX"

	// envVethPrefixDefault is the default value for the veth prefix
	envVethPrefixDefault = "eni"

	// Range of MTU for each ENI and veth pair. Defaults to maximumMTU
	minimumMTU = 576
	maximumMTU = 9001

	// number of retries to add a route
	maxRetryRouteAdd             = 5
	defaultRetryRouteAddInterval = 5 * time.Second
	// number of attempts to find an ENI by MAC address after it is attached
	maxAttemptsLinkByMac          = 5
	defaultRetryLinkByMacInterval = 3 * time.Second
)

type snatType uint32

const (
	sequentialSNAT snatType = iota
	randomHashSNAT
	randomPRNGSNAT
)

var log = logger.Get()

// NetworkAPIs defines the host level and the ENI level network related operations
type NetworkAPIs interface {
	// SetupHostNetwork performs node level network configuration.
	SetupHostNetwork(primaryMAC string, primaryIP net.IP, vpcV4CIDRs []string) error

	// SetupENINetwork performs ENI level network configuration on secondary ENIs.
	SetupENINetwork(eniMAC string, eniIP net.IP, deviceNumber int, eniSubnetCIDR string) error

	// UpdateHostIptablesRules updates the nat table iptables rules on the host.
	// TODO: this API might be replaced by invoke SetupHostNetwork directly when CIDR changes, once we make SetupHostNetwork idempotent.
	UpdateHostIptablesRules(primaryMAC string, primaryIP net.IP, vpcV4CIDRs []string) error

	// TODO: such configuration better be a centralized place instead of here.
	UseExternalSNAT() bool
	GetExcludeSNATCIDRs() []string
}

var _ NetworkAPIs = &linuxNetwork{}

type linuxNetwork struct {
	ipv4Enabled   bool
	ipv6Enabled   bool
	podENIEnabled bool

	useExternalSNAT  bool
	excludeSNATCIDRs []string
	typeOfSNAT       snatType

	mainENIMark uint32
	mtu         int
	vethPrefix  string

	retryRouteAddInterval  time.Duration
	retryLinkByMacInterval time.Duration

	netLink          netlinkwrapper.NetLink
	ns               nswrapper.NS
	ipTablesProvider func(IPProtocol iptables.Protocol) (IPTables, error)
}

// NewNetworkAPIs creates a linuxNetwork object
func NewNetworkAPIs(ipv4Enabled bool, ipv6Enabled bool, podENIEnabled bool) NetworkAPIs {
	return &linuxNetwork{
		ipv4Enabled:   ipv4Enabled,
		ipv6Enabled:   ipv6Enabled,
		podENIEnabled: podENIEnabled,

		useExternalSNAT:  useExternalSNAT(),
		excludeSNATCIDRs: getExcludeSNATCIDRs(),
		typeOfSNAT:       typeOfSNAT(),

		mainENIMark: getConnmark(),
		mtu:         GetEthernetMTU(""),
		vethPrefix:  getVethPrefixName(),

		retryRouteAddInterval:  defaultRetryRouteAddInterval,
		retryLinkByMacInterval: defaultRetryLinkByMacInterval,

		netLink:          netlinkwrapper.NewNetLink(),
		ns:               nswrapper.NewNS(),
		ipTablesProvider: NewIPTablesWithProtocol,
	}
}

// SetupHostNetwork performs node level network configuration
func (n *linuxNetwork) SetupHostNetwork(primaryMAC string, primaryIP net.IP, vpcV4CIDRs []string) error {
	log.Infof("Setting up host network with primaryMAC: %s, primaryIP: %v, vpcV4CIDRs: %v",
		primaryMAC, primaryIP, vpcV4CIDRs)

	link, err := n.netLink.LinkByMacWithRetry(primaryMAC, n.retryLinkByMacInterval, maxAttemptsLinkByMac)
	if err != nil {
		return errors.Wrapf(err, "setupHostNetwork: failed to find the link which uses MAC address %s", primaryMAC)
	}
	if err := n.netLink.LinkSetMTU(link, n.mtu); err != nil {
		return errors.Wrapf(err, "setupHostNetwork: failed to set MTU to %d for %s", n.mtu, link.Attrs().Name)
	}

	ipFamily := unix.AF_INET
	if n.ipv6Enabled {
		ipFamily = unix.AF_INET6
		if err := n.enableIPv6(); err != nil {
			return errors.Wrap(err, "setupHostNetwork: failed to enable IPv6")
		}
	}

	if err := n.ensureMainENIMarkedTrafficRule(ipFamily); err != nil {
		return errors.Wrap(err, "setupHostNetwork: failed to ensureMainENIMarkedTrafficRule")
	}

	// Note: Per Pod Security Group is not supported for V6 yet. So, cordoning off the PPSG rule (for now) with v4 specific check.
	if n.podENIEnabled && n.ipv4Enabled {
		if err := n.ensureLocalRouteHasLowerPriority(); err != nil {
			return errors.Wrap(err, "setupHostNetwork: failed to ensureLocalRouteHasLowerPriority")
		}
	}

	return n.ensureHostNetworkIPTablesRules(link.Attrs().Name, primaryIP, vpcV4CIDRs)
}

// SetupENINetwork adds default route to route table (eni-<eni_table>), so it does not need to be called on the primary ENI
func (n *linuxNetwork) SetupENINetwork(eniMAC string, eniIP net.IP, deviceNumber int, eniSubnetCIDR string) error {
	if deviceNumber == 0 {
		return errors.New("setupENINetwork should never be called on the primary ENI")
	}
	routeTableNumber := deviceNumber + 1
	log.Infof("Setting up ENI network with eniMAC: %s, eniIP: %v, subnetCIDR: %s, routeTableNumber: %d",
		eniMAC, eniIP, eniSubnetCIDR, routeTableNumber)

	link, err := n.netLink.LinkByMacWithRetry(eniMAC, n.retryLinkByMacInterval, maxAttemptsLinkByMac)
	if err != nil {
		return errors.Wrapf(err, "setupHostNetwork: failed to find the link which uses MAC address %s", eniMAC)
	}
	if err := n.netLink.LinkSetMTU(link, n.mtu); err != nil {
		return errors.Wrapf(err, "setupENINetwork: failed to set MTU to %d for %s", n.mtu, link.Attrs().Name)
	}
	if err := n.netLink.LinkSetUp(link); err != nil {
		return errors.Wrapf(err, "setupENINetwork: failed to bring up ENI %s", link.Attrs().Name)
	}
	_, eniNet, err := net.ParseCIDR(eniSubnetCIDR)
	if err != nil {
		return errors.Wrapf(err, "setupENINetwork: invalid IPv4 CIDR block %s", eniSubnetCIDR)
	}
	if err := n.ensureSecondaryENIIPAttachment(link, eniIP, eniNet); err != nil {
		return errors.Wrapf(err, "setupENINetwork: failed to ensureENIIPAttachment for %s", link.Attrs().Name)
	}
	if err := n.ensureSecondaryENIRoutes(link, eniIP, eniNet, routeTableNumber); err != nil {
		return errors.Wrapf(err, "setupENINetwork: failed to ensureSecondaryENIRoutes for %s", link.Attrs().Name)
	}

	return nil
}

// UpdateHostIptablesRules updates the NAT table rules based on the VPC CIDRs configuration
func (n *linuxNetwork) UpdateHostIptablesRules(primaryMAC string, primaryIP net.IP, vpcV4CIDRs []string) error {
	link, err := n.netLink.LinkByMacWithRetry(primaryMAC, n.retryLinkByMacInterval, maxAttemptsLinkByMac)
	if err != nil {
		return errors.Wrapf(err, "updateHostIptablesRules: failed to find the link which uses MAC address %s", primaryMAC)
	}
	return n.ensureHostNetworkIPTablesRules(link.Attrs().Name, primaryIP, vpcV4CIDRs)
}

func (n *linuxNetwork) enableIPv6() (err error) {
	if err = n.setupRuleToBlockNodeLocalV4Access(); err != nil {
		return errors.Wrapf(err, "setupVeth network: failed to setup route to block pod access via IPv4 address")
	}
	return nil
}

// setupRuleToBlockNodeLocalV4Access setups a rule to block traffic directed to v4 interface of the Pod
func (n *linuxNetwork) setupRuleToBlockNodeLocalV4Access() error {
	ipt, err := n.ipTablesProvider(iptables.ProtocolIPv4)
	if err != nil {
		return errors.Wrap(err, "failed to create iptables")
	}

	// TODO: this logic should be changed to be more robust to make zero changes if desired rule is already in-place.

	if err := ipt.Insert("filter", "FORWARD", 1, "-d", "169.254.172.0/22", "-m", "conntrack",
		"--ctstate", "NEW", "-m", "comment", "--comment", "Block Node Local Pod access via IPv4", "-j", "REJECT"); err != nil {
		return fmt.Errorf("failed adding v4 drop route: %v", err)
	}
	return nil
}

// ensureMainENIMarkedTrafficRule adds a rule that will force traffic marked with mainENIMark to out from the main ENI. This rule is used by both NodePort & SNAT.
// NodePort support: traffic always comes in via the main ENI but response traffic would go out of the pod's assigned ENI if we didn't handle it specially.
// 	This is because the routing decision is done before the NodePort's DNAT is reversed so, to the routing table, it looks like the traffic is pod traffic instead of NodePort traffic.
// SNAT support: we mark new connection originated from pod to have mainENI mark, so the traffic can be routed out via mainENI along with our SNAT rule.
// Note: With v6 PD mode support, all the pods will be behind Primary ENI of the node and so we might not even need to mark the packets entering via Primary ENI for NodePort support.
func (n *linuxNetwork) ensureMainENIMarkedTrafficRule(ipFamily int) error {
	mainENIRule := n.netLink.NewRule()
	mainENIRule.Mark = int(n.mainENIMark)
	mainENIRule.Mask = int(n.mainENIMark)
	mainENIRule.Table = mainRoutingTable
	mainENIRule.Priority = hostRulePriority
	mainENIRule.Family = ipFamily

	// TODO: this logic should be changed to be more robust to make zero changes if desired rule is already in-place.

	// If this is a restart, cleanup previous rule first
	if err := n.netLink.RuleDel(mainENIRule); err != nil && !netlinkwrapper.IsNoSuchRuleError(err) {
		log.Errorf("Failed to cleanup old main ENI rule: %v", err)
		return errors.Wrapf(err, "failed to delete old main ENI rule")
	}
	if err := n.netLink.RuleAdd(mainENIRule); err != nil {
		log.Errorf("Failed to add host main ENI rule: %v", err)
		return errors.Wrapf(err, "failed to add new main ENI rule")
	}
	return nil
}

// ensureLocalRouteHasLowerPriority will move the local route to a lower priority thus podENI rules can have higher priority,
// otherwise, the rp_filter check will fail.
func (n *linuxNetwork) ensureLocalRouteHasLowerPriority() error {
	localRule := n.netLink.NewRule()
	localRule.Table = localRouteTable
	localRule.Priority = localRulePriority

	// Add new rule with higher priority
	if err := n.netLink.RuleAdd(localRule); err != nil && !netlinkwrapper.IsRuleExistsError(err) {
		return errors.Wrap(err, "changeLocalRulePriority: unable to update local rule priority")
	}

	// Delete the priority 0 rule
	localRule.Priority = 0
	if err := n.netLink.RuleDel(localRule); err != nil && !netlinkwrapper.IsNoSuchRuleError(err) {
		return errors.Wrap(err, "changeLocalRulePriority: failed to delete priority 0 local rule")
	}
	return nil
}

// ensureSecondaryENIIPAttachment explicitly set the IP on the device if not already set.
// Required for older kernels.
// ip addr show
// ip addr del <eniIP> dev <link> (if necessary)
// ip addr add <eniIP> dev <link>
func (n *linuxNetwork) ensureSecondaryENIIPAttachment(eniLink netlink.Link, eniIP net.IP, eniNet *net.IPNet) error {
	log.Debugf("Setting up primary IP %v on ENI %s", eniIP, eniLink.Attrs().Name)
	addrs, err := n.netLink.AddrList(eniLink, unix.AF_INET)
	if err != nil {
		return errors.Wrapf(err, "failed to list IP address for ENI %s", eniLink.Attrs().Name)
	}

	// TODO: this logic should be changed to be more robust to make zero changes if desired IPAddresses is already in-place.
	for _, addr := range addrs {
		log.Debugf("Deleting existing IP address %s", addr.String())
		if err := n.netLink.AddrDel(eniLink, &addr); err != nil {
			return errors.Wrapf(err, "failed to delete IP addr from ENI %s", eniLink.Attrs().Name)
		}
	}
	eniAddr := &net.IPNet{
		IP:   eniIP,
		Mask: eniNet.Mask,
	}
	log.Debugf("Adding IP address %s", eniAddr.String())
	if err := n.netLink.AddrAdd(eniLink, &netlink.Addr{IPNet: eniAddr}); err != nil {
		return errors.Wrapf(err, "failed to add IP addr to ENI %s", eniLink.Attrs().Name)
	}
	return nil
}

func (n *linuxNetwork) ensureSecondaryENIRoutes(eniLink netlink.Link, eniIP net.IP, eniNet *net.IPNet, routeTableNumber int) error {
	gatewayIP, err := IncrementIPv4Addr(eniNet.IP)
	if err != nil {
		return errors.Wrapf(err, "setupENINetwork: failed to define gateway address from %v", eniNet.IP)
	}
	linkIndex := eniLink.Attrs().Index
	log.Debugf("Setting up ENI's default gateway %v, table %d, linkIndex %d on ENI %s", gatewayIP, routeTableNumber, linkIndex, eniLink.Attrs().Name)

	routes := []netlink.Route{
		// Add a direct link route for the host's ENI IP only
		{
			LinkIndex: linkIndex,
			Dst:       &net.IPNet{IP: gatewayIP, Mask: net.CIDRMask(32, 32)},
			Scope:     netlink.SCOPE_LINK,
			Table:     routeTableNumber,
		},
		// Route all other traffic via the host's ENI IP
		{
			LinkIndex: linkIndex,
			Dst:       &net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(0, 32)},
			Scope:     netlink.SCOPE_UNIVERSE,
			Gw:        gatewayIP,
			Table:     routeTableNumber,
		},
	}

	// TODO: this logic should be changed to be more robust to make zero changes if desired routes is already in-place.
	for _, r := range routes {
		if err := n.netLink.RouteDel(&r); err != nil && !netlinkwrapper.IsNotExistsError(err) {
			return errors.Wrap(err, "failed to clean up old routes")
		}

		if err := retry.NWithBackoff(retry.NewSimpleBackoff(500*time.Millisecond, n.retryRouteAddInterval, 0.15, 2.0), maxRetryRouteAdd, func() error {
			if err := n.netLink.RouteReplace(&r); err != nil {
				log.Debugf("Not able to set route %s/0 via %s table %d", r.Dst.IP.String(), gatewayIP.String(), routeTableNumber)
				return errors.Wrapf(err, "unable to replace route entry %s", r.Dst.IP.String())
			}
			log.Debugf("Successfully added/replaced route to be %s/0", r.Dst.IP.String())
			return nil
		}); err != nil {
			return err
		}
	}

	// Remove the route that default out to ENI-x out of main route table
	defaultRoute := netlink.Route{
		Dst:   eniNet,
		Src:   eniIP,
		Table: mainRoutingTable,
		Scope: netlink.SCOPE_LINK,
	}

	if err := n.netLink.RouteDel(&defaultRoute); err != nil {
		if !netlinkwrapper.IsNotExistsError(err) {
			return errors.Wrapf(err, "unable to delete default route %s for source IP %s", eniNet.String(), eniIP.String())
		}
	}
	return nil
}

// ensureHostNetworkIPTablesRules ensures the IPTables rules for host network are configured properly.
func (n *linuxNetwork) ensureHostNetworkIPTablesRules(primaryInterfaceName string, primaryIP net.IP, vpcV4CIDRs []string) error {
	ipProtocol := iptables.ProtocolIPv4
	if n.ipv6Enabled {
		//Essentially a stub function for now in V6 mode. We will need it when we support v6 in secondary IP and
		//custom networking modes. We don't need to install any SNAT rules in v6 mode and currently there is no need
		//to mark packets entering via Primary ENI as all the pods in v6 mode will be behind primary ENI. Will have to
		//start doing that once we start supporting custom networking mode in v6.
		ipProtocol = iptables.ProtocolIPv6
	}

	ipt, err := n.ipTablesProvider(ipProtocol)
	if err != nil {
		return errors.Wrap(err, "ensureHostNetworkIPTablesRules: failed to create iptables")
	}

	if n.ipv4Enabled {
		iptablesSNATRules, err := n.buildIptablesSNATRules(vpcV4CIDRs, primaryIP, primaryInterfaceName, ipt)
		if err != nil {
			return err
		}
		if err := n.updateIptablesRules(iptablesSNATRules, ipt); err != nil {
			return err
		}

		iptablesConnmarkRules, err := n.buildIptablesConnmarkRules(vpcV4CIDRs, ipt)
		if err != nil {
			return err
		}
		if err := n.updateIptablesRules(iptablesConnmarkRules, ipt); err != nil {
			return err
		}
	}
	return nil
}

func (n *linuxNetwork) buildIptablesSNATRules(vpcV4CIDRs []string, primaryIP net.IP, primaryIntf string, ipt IPTables) ([]iptablesRule, error) {
	type snatCIDR struct {
		cidr        string
		isExclusion bool
	}
	var allCIDRs []snatCIDR
	for _, cidr := range vpcV4CIDRs {
		log.Debugf("Adding %s CIDR to NAT chain", cidr)
		allCIDRs = append(allCIDRs, snatCIDR{cidr: cidr, isExclusion: false})
	}
	for _, cidr := range n.excludeSNATCIDRs {
		log.Debugf("Adding %s Excluded CIDR to NAT chain", cidr)
		allCIDRs = append(allCIDRs, snatCIDR{cidr: cidr, isExclusion: true})
	}

	log.Debugf("Total CIDRs to program - %d", len(allCIDRs))
	// build IPTABLES chain for SNAT of non-VPC outbound traffic and excluded CIDRs
	var chains []string
	for i := 0; i <= len(allCIDRs); i++ {
		chain := fmt.Sprintf("AWS-SNAT-CHAIN-%d", i)
		log.Debugf("Setup Host Network: iptables -N %s -t nat", chain)
		if err := ipt.NewChain("nat", chain); err != nil && !IsChainExistErr(err) {
			log.Errorf("ipt.NewChain error for chain [%s]: %v", chain, err)
			return []iptablesRule{}, errors.Wrapf(err, "host network setup: failed to add chain")
		}
		chains = append(chains, chain)
	}

	// build SNAT rules for outbound non-VPC traffic
	var iptableRules []iptablesRule
	log.Debugf("Setup Host Network: iptables -A POSTROUTING -m comment --comment \"AWS SNAT CHAIN\" -j AWS-SNAT-CHAIN-0")
	iptableRules = append(iptableRules, iptablesRule{
		name:        "first SNAT rules for non-VPC outbound traffic",
		shouldExist: !n.useExternalSNAT,
		table:       "nat",
		chain:       "POSTROUTING",
		rule: []string{
			"-m", "comment", "--comment", "AWS SNAT CHAIN", "-j", "AWS-SNAT-CHAIN-0",
		}})

	for i, cidr := range allCIDRs {
		curChain := chains[i]
		curName := fmt.Sprintf("[%d] AWS-SNAT-CHAIN", i)
		nextChain := chains[i+1]
		comment := "AWS SNAT CHAIN"
		if cidr.isExclusion {
			comment += " EXCLUSION"
		}
		log.Debugf("Setup Host Network: iptables -A %s ! -d %s -t nat -j %s", curChain, cidr, nextChain)

		iptableRules = append(iptableRules, iptablesRule{
			name:        curName,
			shouldExist: !n.useExternalSNAT,
			table:       "nat",
			chain:       curChain,
			rule: []string{
				"!", "-d", cidr.cidr, "-m", "comment", "--comment", comment, "-j", nextChain,
			}})
	}

	// Prepare the Desired Rule for SNAT Rule for non-pod ENIs
	snatRule := []string{"!", "-o", "vlan+",
		"-m", "comment", "--comment", "AWS, SNAT",
		"-m", "addrtype", "!", "--dst-type", "LOCAL",
		"-j", "SNAT", "--to-source", primaryIP.String()}
	if n.typeOfSNAT == randomHashSNAT {
		snatRule = append(snatRule, "--random")
	}
	if n.typeOfSNAT == randomPRNGSNAT {
		if ipt.HasRandomFully() {
			snatRule = append(snatRule, "--random-fully")
		} else {
			log.Warn("prng (--random-fully) requested, but iptables version does not support it. " +
				"Falling back to hashrandom (--random)")
			snatRule = append(snatRule, "--random")
		}
	}

	lastChain := chains[len(chains)-1]
	iptableRules = append(iptableRules, iptablesRule{
		name:        "last SNAT rule for non-VPC outbound traffic",
		shouldExist: !n.useExternalSNAT,
		table:       "nat",
		chain:       lastChain,
		rule:        snatRule,
	})

	snatStaleRules, err := computeStaleIptablesRules(ipt, "nat", "AWS-SNAT-CHAIN", iptableRules, chains)
	if err != nil {
		return []iptablesRule{}, err
	}

	iptableRules = append(iptableRules, snatStaleRules...)

	iptableRules = append(iptableRules, iptablesRule{
		name:        "connmark for primary ENI",
		shouldExist: true,
		table:       "mangle",
		chain:       "PREROUTING",
		rule: []string{
			"-m", "comment", "--comment", "AWS, primary ENI",
			"-i", primaryIntf,
			"-m", "addrtype", "--dst-type", "LOCAL", "--limit-iface-in",
			"-j", "CONNMARK", "--set-mark", fmt.Sprintf("%#x/%#x", n.mainENIMark, n.mainENIMark),
		},
	})

	iptableRules = append(iptableRules, iptablesRule{
		name:        "connmark restore for primary ENI",
		shouldExist: true,
		table:       "mangle",
		chain:       "PREROUTING",
		rule: []string{
			"-m", "comment", "--comment", "AWS, primary ENI",
			"-i", n.vethPrefix + "+", "-j", "CONNMARK", "--restore-mark", "--mask", fmt.Sprintf("%#x", n.mainENIMark),
		},
	})

	iptableRules = append(iptableRules, iptablesRule{
		name:        "connmark restore for primary ENI from vlan",
		shouldExist: true,
		table:       "mangle",
		chain:       "PREROUTING",
		rule: []string{
			"-m", "comment", "--comment", "AWS, primary ENI",
			"-i", "vlan+", "-j", "CONNMARK", "--restore-mark", "--mask", fmt.Sprintf("%#x", n.mainENIMark),
		},
	})

	log.Debugf("iptableRules: %v", iptableRules)
	return iptableRules, nil
}

func (n *linuxNetwork) buildIptablesConnmarkRules(vpcCIDRs []string, ipt IPTables) ([]iptablesRule, error) {
	var allCIDRs []string
	allCIDRs = append(allCIDRs, vpcCIDRs...)
	allCIDRs = append(allCIDRs, n.excludeSNATCIDRs...)
	excludeCIDRs := sets.NewString(n.excludeSNATCIDRs...)

	log.Debugf("Total CIDRs to exempt from connmark rules - %d", len(allCIDRs))
	var chains []string
	for i := 0; i <= len(allCIDRs); i++ {
		chain := fmt.Sprintf("AWS-CONNMARK-CHAIN-%d", i)
		log.Debugf("Setup Host Network: iptables -N %s -t nat", chain)
		if err := ipt.NewChain("nat", chain); err != nil && !IsChainExistErr(err) {
			log.Errorf("ipt.NewChain error for chain [%s]: %v", chain, err)
			return []iptablesRule{}, errors.Wrapf(err, "host network setup: failed to add chain")
		}
		chains = append(chains, chain)
	}

	var iptableRules []iptablesRule
	log.Debugf("Setup Host Network: iptables -t nat -A PREROUTING -i %s+ -m comment --comment \"AWS, outbound connections\" -m state --state NEW -j AWS-CONNMARK-CHAIN-0", n.vethPrefix)
	iptableRules = append(iptableRules, iptablesRule{
		name:        "connmark rule for non-VPC outbound traffic",
		shouldExist: !n.useExternalSNAT,
		table:       "nat",
		chain:       "PREROUTING",
		rule: []string{
			"-i", n.vethPrefix + "+", "-m", "comment", "--comment", "AWS, outbound connections",
			"-m", "state", "--state", "NEW", "-j", "AWS-CONNMARK-CHAIN-0",
		}})

	for i, cidr := range allCIDRs {
		curChain := chains[i]
		curName := fmt.Sprintf("[%d] AWS-SNAT-CHAIN", i)
		nextChain := chains[i+1]
		comment := "AWS CONNMARK CHAIN, VPC CIDR"
		if excludeCIDRs.Has(cidr) {
			comment = "AWS CONNMARK CHAIN, EXCLUDED CIDR"
		}
		log.Debugf("Setup Host Network: iptables -A %s ! -d %s -t nat -j %s", curChain, cidr, nextChain)

		iptableRules = append(iptableRules, iptablesRule{
			name:        curName,
			shouldExist: !n.useExternalSNAT,
			table:       "nat",
			chain:       curChain,
			rule: []string{
				"!", "-d", cidr, "-m", "comment", "--comment", comment, "-j", nextChain,
			}})
	}

	iptableRules = append(iptableRules, iptablesRule{
		name:        "connmark rule for external  outbound traffic",
		shouldExist: !n.useExternalSNAT,
		table:       "nat",
		chain:       chains[len(chains)-1],
		rule: []string{
			"-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK",
			"--set-xmark", fmt.Sprintf("%#x/%#x", n.mainENIMark, n.mainENIMark),
		},
	})

	// Force delete existing restore mark rule so that the subsequent rule gets added to the end
	iptableRules = append(iptableRules, iptablesRule{
		name:        "connmark to fwmark copy",
		shouldExist: false,
		table:       "nat",
		chain:       "PREROUTING",
		rule: []string{
			"-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK",
			"--restore-mark", "--mask", fmt.Sprintf("%#x", n.mainENIMark),
		},
	})

	iptableRules = append(iptableRules, iptablesRule{
		name:        "connmark to fwmark copy",
		shouldExist: !n.useExternalSNAT,
		table:       "nat",
		chain:       "PREROUTING",
		rule: []string{
			"-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK",
			"--restore-mark", "--mask", fmt.Sprintf("%#x", n.mainENIMark),
		},
	})

	connmarkStaleRules, err := computeStaleIptablesRules(ipt, "nat", "AWS-CONNMARK-CHAIN", iptableRules, chains)
	if err != nil {
		return []iptablesRule{}, err
	}
	iptableRules = append(iptableRules, connmarkStaleRules...)

	log.Debugf("iptableRules: %v", iptableRules)
	return iptableRules, nil
}

func (n *linuxNetwork) updateIptablesRules(iptableRules []iptablesRule, ipt IPTables) error {
	for _, rule := range iptableRules {
		log.Debugf("execute iptable rule : %s", rule.name)

		exists, err := ipt.Exists(rule.table, rule.chain, rule.rule...)
		log.Debugf("rule %v exists %v, err %v", rule, exists, err)
		if err != nil {
			log.Errorf("host network setup: failed to check existence of %v, %v", rule, err)
			return errors.Wrapf(err, "host network setup: failed to check existence of %v", rule)
		}

		if !exists && rule.shouldExist {
			err = ipt.Append(rule.table, rule.chain, rule.rule...)
			if err != nil {
				log.Errorf("host network setup: failed to add %v, %v", rule, err)
				return errors.Wrapf(err, "host network setup: failed to add %v", rule)
			}
		} else if exists && !rule.shouldExist {
			err = ipt.Delete(rule.table, rule.chain, rule.rule...)
			if err != nil {
				log.Errorf("host network setup: failed to delete %v, %v", rule, err)
				return errors.Wrapf(err, "host network setup: failed to delete %v", rule)
			}
		}
	}
	return nil
}

func listCurrentIptablesRules(ipt IPTables, table, chainPrefix string) ([]iptablesRule, error) {
	var toClear []iptablesRule
	log.Debugf("Setup Host Network: loading existing iptables %s rules with chain prefix %s", table, chainPrefix)
	existingChains, err := ipt.ListChains(table)
	if err != nil {
		return nil, errors.Wrapf(err, "host network setup: failed to list iptables %s chains", table)
	}
	for _, chain := range existingChains {
		if !strings.HasPrefix(chain, chainPrefix) {
			continue
		}
		rules, err := ipt.List(table, chain)
		if err != nil {
			return nil, errors.Wrap(err, fmt.Sprintf("host network setup: failed to list iptables nat chain %s", chain))
		}
		for i, rule := range rules {
			r := csv.NewReader(strings.NewReader(rule))
			r.Comma = ' '
			ruleSpec, err := r.Read()
			if err != nil {
				return nil, errors.Wrap(err, fmt.Sprintf("host network setup: failed to parse iptables nat chain %s rule %s", chain, rule))
			}
			log.Debugf("host network setup: found potentially stale SNAT rule for chain %s: %v", chain, ruleSpec)
			toClear = append(toClear, iptablesRule{
				name:        fmt.Sprintf("[%d] %s", i, chain),
				shouldExist: false, // To trigger ipt.Delete for stale rules
				table:       table,
				chain:       chain,
				rule:        ruleSpec[2:], //drop action and chain name
			})
		}
	}
	return toClear, nil
}

func computeStaleIptablesRules(ipt IPTables, table, chainPrefix string, newRules []iptablesRule, chains []string) ([]iptablesRule, error) {
	var staleRules []iptablesRule
	existingRules, err := listCurrentIptablesRules(ipt, table, chainPrefix)
	if err != nil {
		return []iptablesRule{}, errors.Wrapf(err, "host network setup: failed to list rules from table %s with chain prefix %s", table, chainPrefix)
	}
	activeChains := sets.NewString(chains...)
	log.Debugf("Setup Host Network: computing stale iptables rules for %s table with chain prefix %s")
	for _, staleRule := range existingRules {
		if len(staleRule.rule) == 0 && activeChains.Has(staleRule.chain) {
			log.Debugf("Setup Host Network: active chain found: %s", staleRule.chain)
			continue
		}
		keepRule := false
		for _, newRule := range newRules {
			if staleRule.chain == newRule.chain && reflect.DeepEqual(newRule.rule, staleRule.rule) {
				log.Debugf("Setup Host Network: active rule found: %s", staleRule)
				keepRule = true
				break
			}
		}
		if !keepRule {
			log.Debugf("Setup Host Network: stale rule found: %s", staleRule)
			staleRules = append(staleRules, staleRule)
		}
	}
	return staleRules, nil
}

type iptablesRule struct {
	name         string
	shouldExist  bool
	table, chain string
	rule         []string
}

func (r iptablesRule) String() string {
	return fmt.Sprintf("%s/%s rule %s shouldExist %v rule %v", r.table, r.chain, r.name, r.shouldExist, r.rule)
}

// GetConfigForDebug returns the active values of the configuration env vars (for debugging purposes).
func GetConfigForDebug() map[string]interface{} {
	return map[string]interface{}{
		envConnmark:         getConnmark(),
		envExcludeSNATCIDRs: getExcludeSNATCIDRs(),
		envExternalSNAT:     useExternalSNAT(),
		envMTU:              GetEthernetMTU(""),
		envVethPrefix:       getVethPrefixName(),
		envRandomizeSNAT:    typeOfSNAT(),
	}
}

// UseExternalSNAT returns whether SNAT of secondary ENI IPs should be handled with an external
// NAT gateway rather than on node. Failure to parse the setting will result in a log and the
// setting will be disabled.
func (n *linuxNetwork) UseExternalSNAT() bool {
	return useExternalSNAT()
}

func useExternalSNAT() bool {
	return getBoolEnvVar(envExternalSNAT, false)
}

// GetExcludeSNATCIDRs returns a list of cidrs that should be excluded from SNAT if UseExternalSNAT is false,
// otherwise it returns an empty list.
func (n *linuxNetwork) GetExcludeSNATCIDRs() []string {
	return getExcludeSNATCIDRs()
}

func getExcludeSNATCIDRs() []string {
	if useExternalSNAT() {
		return nil
	}

	excludeCIDRs := os.Getenv(envExcludeSNATCIDRs)
	if excludeCIDRs == "" {
		return nil
	}
	var cidrs []string
	for _, excludeCIDR := range strings.Split(excludeCIDRs, ",") {
		_, parseCIDR, err := net.ParseCIDR(excludeCIDR)
		if err != nil {
			log.Errorf("getExcludeSNATCIDRs : ignoring %v is not a valid IPv4 CIDR", excludeCIDR)
		} else {
			cidrs = append(cidrs, parseCIDR.String())
		}
	}
	return cidrs
}

func typeOfSNAT() snatType {
	defaultValue := randomPRNGSNAT
	strValue := os.Getenv(envRandomizeSNAT)
	switch strValue {
	case "":
		// empty means default, which is --random-fully
		return defaultValue
	case "prng":
		// prng means to use --random-fully
		// note: for old versions of iptables, this will fall back to --random
		return randomPRNGSNAT
	case "none":
		// none means to disable randomisation (no flag)
		return sequentialSNAT
	case "hashrandom":
		// hashrandom means to use --random
		return randomHashSNAT
	default:
		// if we get to this point, the environment variable has an invalid value
		log.Errorf("Failed to parse %s; using default: %s. Provided string was %q", envRandomizeSNAT, "prng", strValue)
		return defaultValue
	}
}

func getBoolEnvVar(name string, defaultValue bool) bool {
	if strValue := os.Getenv(name); strValue != "" {
		parsedValue, err := strconv.ParseBool(strValue)
		if err != nil {
			log.Errorf("Failed to parse "+name+"; using default: "+fmt.Sprint(defaultValue), err.Error())
			return defaultValue
		}
		return parsedValue
	}
	return defaultValue
}

func getConnmark() uint32 {
	if connmark := os.Getenv(envConnmark); connmark != "" {
		mark, err := strconv.ParseInt(connmark, 0, 64)
		if err != nil {
			log.Infof("Failed to parse %s; will use %d, error: %v", envConnmark, defaultConnmark, err)
			return defaultConnmark
		}
		if mark > math.MaxUint32 || mark <= 0 {
			log.Infof("%s out of range; will use %s", envConnmark, defaultConnmark)
			return defaultConnmark
		}
		return uint32(mark)
	}
	return defaultConnmark
}

// GetEthernetMTU gets the MTU setting from AWS_VPC_ENI_MTU if set, or takes the passed in string. Defaults to 9001 if not set.
func GetEthernetMTU(envMTUValue string) int {
	inputStr, found := os.LookupEnv(envMTU)
	if found {
		envMTUValue = inputStr
	}
	if envMTUValue != "" {
		mtu, err := strconv.Atoi(envMTUValue)
		if err != nil {
			log.Errorf("Failed to parse %s will use %d: %v", envMTU, maximumMTU, err.Error())
			return maximumMTU
		}
		// Restrict range between jumbo frame and the maximum required size to assemble.
		// Details in https://tools.ietf.org/html/rfc879 and
		// https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/network_mtu.html
		if mtu < minimumMTU {
			log.Errorf("%s is too low: %d. Will use %d", envMTU, mtu, minimumMTU)
			return minimumMTU
		}
		if mtu > maximumMTU {
			log.Errorf("%s is too high: %d. Will use %d", envMTU, mtu, maximumMTU)
			return maximumMTU
		}
		return mtu
	}
	return maximumMTU
}

// getVethPrefixName gets the name prefix of the veth devices based on the AWS_VPC_K8S_CNI_VETHPREFIX environment variable
func getVethPrefixName() string {
	if envVal, found := os.LookupEnv(envVethPrefix); found {
		return envVal
	}
	return envVethPrefixDefault
}
