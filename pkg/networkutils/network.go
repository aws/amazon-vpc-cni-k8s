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
	"bytes"
	"encoding/csv"
	"fmt"
	"math"
	"net"
	"os"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/coreos/go-iptables/iptables"
	"github.com/samber/lo"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/sgpp"
	"github.com/aws/amazon-vpc-cni-k8s/utils"

	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/retry"

	"github.com/pkg/errors"
	"golang.org/x/sys/unix"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"

	"github.com/vishvananda/netlink"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/iptableswrapper"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/netlinkwrapper"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/nswrapper"
	"github.com/google/nftables"
	"github.com/google/nftables/binaryutil"
	"github.com/google/nftables/expr"
)

const (
	// Vlan rule priority
	VlanRulePriority = 10

	// Local rule, needs to come after the pod ENI rules
	localRulePriority = 20

	// Rule priority for traffic destined to pod IP
	ToContainerRulePriority = 512

	// From Interface priority for multi-homed pods
	FromInterfaceRulePriority = 1

	// 513 - 1023, can be used for priority lower than fromPodRule but higher than default nonVPC CIDR rule

	// 1024 is reserved for (ip rule not to <VPC's subnet> table main)
	hostRulePriority = 1024

	// 1025 - 1534 can be used as priority lower than externalServiceIpRulePriority but higher than default nonVPC CIDR rule

	// Rule priority for traffic destined to explicit IP CIDR
	externalServiceIpRulePriority = 1535

	// Rule priority for traffic from pod
	FromPodRulePriority = 1536

	// Rule priority for traffic from primary IP on secondary ENI
	FromPrimaryIPofENIRulePriority = 32765

	// Main route table
	mainRoutingTable = unix.RT_TABLE_MAIN

	// Local route table
	localRouteTable = unix.RT_TABLE_LOCAL

	// This environment is used to specify whether an external NAT gateway will be used to provide SNAT of
	// secondary ENI IP addresses. If set to "true", the SNAT iptables rule and off-VPC ip rule will not
	// be installed and will be removed if they are already installed. Defaults to false.
	envExternalSNAT = "AWS_VPC_K8S_CNI_EXTERNALSNAT"

	// This environment is used to specify a comma-separated list of IPv4 CIDRs to exclude from SNAT. An additional rule
	// will be written to the iptables for each item. If an item is not an ipv4 range it will be skipped.
	// Defaults to empty.
	envExcludeSNATCIDRs = "AWS_VPC_K8S_CNI_EXCLUDE_SNAT_CIDRS"

	// This environment is used to specify a comma-separated list of IPv4 CIDRs that require routing lookup in
	// main routing table. An IP rule is created for each CIDR.
	envExternalServiceCIDRs = "AWS_EXTERNAL_SERVICE_CIDRS"

	// This environment is used to specify weather the SNAT rule added to iptables should randomize port allocation for
	// outgoing connections. If set to "hashrandom" the SNAT iptables rule will have the "--random" flag added to it.
	// Use "prng" if you want to use pseudo random numbers, i.e. "--random-fully".
	// Default is "prng".
	envRandomizeSNAT = "AWS_VPC_K8S_CNI_RANDOMIZESNAT"

	// envNodePortSupport is the name of environment variable that configures whether we implement support for
	// NodePorts on the primary ENI. This requires that we add additional iptables rules and loosen the kernel's
	// RPF check as described below. Defaults to true.
	envNodePortSupport = "AWS_VPC_CNI_NODE_PORT_SUPPORT"

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
	envMTU     = "AWS_VPC_ENI_MTU"
	defaultMTU = 9001
	minMTUv4   = 576

	// envVethPrefix is the environment variable to configure the prefix of the host side veth device names
	envVethPrefix = "AWS_VPC_K8S_CNI_VETHPREFIX"

	// envVethPrefixDefault is the default value for the veth prefix
	envVethPrefixDefault = "eni"

	// envEnIpv6Egress is the environment variable to enable IPv6 egress support on EKS v4 cluster
	envEnIpv6Egress = "ENABLE_V6_EGRESS"

	// number of retries to add a route
	maxRetryRouteAdd = 5

	retryRouteAddInterval = 5 * time.Second

	// number of attempts to find an ENI by MAC address after it is attached
	maxAttemptsLinkByMac = 5

	retryLinkByMacInterval = 3 * time.Second
)

var log = logger.Get()

// NetworkAPIs defines the host level and the ENI level network related operations
type NetworkAPIs interface {
	// SetupNodeNetwork performs node level network configuration
	SetupHostNetwork(vpcCIDRs []string, primaryMAC string, primaryAddr *net.IP, enablePodENI bool,
		v6Enabled bool) error
	// SetupENINetwork performs ENI level network configuration. Not needed on the primary ENI
	SetupENINetwork(eniIP string, eniMAC string, networkCard int, eniSubnetCIDR string, maxENIPerNIC int, isTrunkENI bool, routeTableID int, isRuleConfigured bool) error
	// UpdateHostIptablesRules updates the nat table iptables rules on the host
	UpdateHostIptablesRules(vpcCIDRs []string, primaryMAC string, primaryAddr *net.IP, v6Enabled bool) error
	CleanUpStaleAWSChains(v4Enabled, v6Enabled bool) error
	UseExternalSNAT() bool
	GetExcludeSNATCIDRs() []string
	GetExternalServiceCIDRs() []string
	GetRuleList(v6enabled bool) ([]netlink.Rule, error)
	GetRuleListBySrc(ruleList []netlink.Rule, src net.IPNet) ([]netlink.Rule, error)
	UpdateRuleListBySrc(ruleList []netlink.Rule, src net.IPNet) error
	UpdateExternalServiceIpRules(ruleList []netlink.Rule, externalIPs []string) error
	GetLinkByMac(mac string, retryInterval time.Duration) (netlink.Link, error)
	DeleteRulesBySrc(eniIP string, v6enabled bool) error
	GetRouteTableNumberForENI(networkCard int, eniIP string, deviceNumber int, maxENIsPerNetworkCard int, isV6 bool) (int, bool, error)
}

type linuxNetwork struct {
	useExternalSNAT        bool
	ipv6EgressEnabled      bool
	excludeSNATCIDRs       []string
	externalServiceCIDRs   []string
	typeOfSNAT             snatType
	nodePortSupportEnabled bool
	mtu                    int
	vethPrefix             string
	podSGEnforcingMode     sgpp.EnforcingMode

	netLink     netlinkwrapper.NetLink
	ns          nswrapper.NS
	newIptables func(IPProtocol iptables.Protocol) (iptableswrapper.IPTablesIface, error)
	mainENIMark uint32
}

type snatType uint32

const (
	sequentialSNAT snatType = iota
	randomHashSNAT
	randomPRNGSNAT
)

// New creates a linuxNetwork object
func New() NetworkAPIs {
	return &linuxNetwork{
		useExternalSNAT:        useExternalSNAT(),
		ipv6EgressEnabled:      ipV6EgressEnabled(),
		excludeSNATCIDRs:       parseCIDRString(envExcludeSNATCIDRs),
		externalServiceCIDRs:   parseCIDRString(envExternalServiceCIDRs),
		typeOfSNAT:             typeOfSNAT(),
		nodePortSupportEnabled: nodePortSupportEnabled(),
		mainENIMark:            getConnmark(),
		mtu:                    GetEthernetMTU(),
		vethPrefix:             getVethPrefixName(),
		podSGEnforcingMode:     sgpp.LoadEnforcingModeFromEnv(),

		netLink: netlinkwrapper.NewNetLink(),
		ns:      nswrapper.NewNS(),
		newIptables: func(IPProtocol iptables.Protocol) (iptableswrapper.IPTablesIface, error) {
			ipt, err := iptables.NewWithProtocol(IPProtocol)
			return ipt, err
		},
	}
}

// find out the primary interface name
func findPrimaryInterfaceName(primaryMAC string) (string, error) {
	log.Debugf("Trying to find primary interface that has mac : %s", primaryMAC)

	interfaces, err := net.Interfaces()
	if err != nil {
		log.Errorf("Failed to read all interfaces: %v", err)
		return "", errors.Wrapf(err, "findPrimaryInterfaceName: failed to find interfaces")
	}

	for _, intf := range interfaces {
		log.Debugf("Discovered interface: %v, mac: %v", intf.Name, intf.HardwareAddr)

		if strings.Compare(primaryMAC, intf.HardwareAddr.String()) == 0 {
			log.Infof("Discovered primary interface: %s", intf.Name)
			return intf.Name, nil
		}
	}

	log.Errorf("No primary interface found")
	return "", errors.New("no primary interface found")
}

func (n *linuxNetwork) enableIPv6() (err error) {
	if err = n.setupRuleToBlockNodeLocalAccess(iptables.ProtocolIPv4); err != nil {
		return errors.Wrapf(err, "setupVeth network: failed to setup route to block pod access via IPv4 address")
	}
	return nil
}

func (n *linuxNetwork) SetupRuleToBlockNodeLocalV4Access() error {
	return n.setupRuleToBlockNodeLocalAccess(iptables.ProtocolIPv4)
}

func (n *linuxNetwork) SetupRuleToBlockNodeLocalV6Access() error {
	return n.setupRuleToBlockNodeLocalAccess(iptables.ProtocolIPv6)
}

// Set up a rule to block traffic directed to v4/v6 egress interface of the Pod
func (n *linuxNetwork) setupRuleToBlockNodeLocalAccess(protocol iptables.Protocol) error {
	ipVersion := "v4"
	localIpCidr := "169.254.172.0/22"
	iptableCmd := "iptables"
	if protocol == iptables.ProtocolIPv6 {
		ipVersion = "v6"
		localIpCidr = "fd00::ac:00/118"
		iptableCmd = "ip6tables"
	}

	ipt, err := n.newIptables(protocol)
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("failed to create %s", iptableCmd))
	}

	denyRule := iptablesRule{
		name:  fmt.Sprintf("Block Node Local Pod access via IP%s", ipVersion),
		table: "filter",
		chain: "FORWARD",
		rule: []string{
			"-d", localIpCidr, "-m", "conntrack",
			"--ctstate", "NEW", "-m", "comment", "--comment", fmt.Sprintf("Block Node Local Pod access via IP%s", ipVersion), "-j", "REJECT",
		},
	}

	if exists, err := ipt.Exists(denyRule.table, denyRule.chain, denyRule.rule...); exists && err == nil {
		log.Info(fmt.Sprintf("Rule to block Node Local Pod access via IP%s is already present. Moving on.. ", ipVersion))
		return nil
	}

	//Let's add the rule. Rule is either missing (or) we're not able to validate its presence.
	if err := ipt.Insert(denyRule.table, denyRule.chain, 1, denyRule.rule...); err != nil {
		return fmt.Errorf("failed adding %s drop route: %v", ipVersion, err)
	}
	return nil
}

// SetupHostNetwork performs node level network configuration
func (n *linuxNetwork) SetupHostNetwork(vpcCIDRs []string, primaryMAC string, primaryAddr *net.IP, enablePodENI bool, v6Enabled bool) error {
	log.Info("Setting up host network... ")

	link, err := linkByMac(primaryMAC, n.netLink, retryLinkByMacInterval)
	if err != nil {
		return errors.Wrapf(err, "setupHostNetwork: failed to find the link primary ENI with MAC address %s", primaryMAC)
	}
	if err = n.netLink.LinkSetMTU(link, n.mtu); err != nil {
		return errors.Wrapf(err, "setupHostNetwork: failed to set primary interface MTU to %d", n.mtu)
	}

	ipFamily := unix.AF_INET
	if v6Enabled {
		ipFamily = unix.AF_INET6
		if err := n.enableIPv6(); err != nil {
			return errors.Wrapf(err, "failed to enable IPv6")
		}
	} else if n.ipv6EgressEnabled {
		if err := n.setupRuleToBlockNodeLocalAccess(iptables.ProtocolIPv6); err != nil {
			return errors.Wrapf(err, "failed to block node local v6 access")
		}
	}

	// If node port support is enabled, add a rule that will force force marked traffic out of the main ENI.  We then
	// add iptables rules below that will mark traffic that needs this special treatment.  In particular NodePort
	// traffic always comes in via the main ENI but response traffic would go out of the pod's assigned ENI if we
	// didn't handle it specially. This is because the routing decision is done before the NodePort's DNAT is
	// reversed so, to the routing table, it looks like the traffic is pod traffic instead of NodePort traffic.
	// Note: With v6 PD mode support, all the pods will be behind Primary ENI of the node and so we might not even need
	// to mark the packets entering via Primary ENI for NodePort support.
	mainENIRule := n.netLink.NewRule()
	mainENIRule.Mark = n.mainENIMark
	mainENIRule.Mask = &n.mainENIMark
	mainENIRule.Table = mainRoutingTable
	mainENIRule.Priority = hostRulePriority
	mainENIRule.Family = ipFamily
	// If this is a restart, cleanup previous rule first
	err = n.netLink.RuleDel(mainENIRule)
	if err != nil && !containsNoSuchRule(err) {
		log.Errorf("Failed to cleanup old main ENI rule: %v", err)
		return errors.Wrapf(err, "host network setup: failed to delete old main ENI rule")
	}

	if n.nodePortSupportEnabled || !n.useExternalSNAT {
		err = n.netLink.RuleAdd(mainENIRule)
		if err != nil {
			log.Errorf("Failed to add host main ENI rule: %v", err)
			return errors.Wrapf(err, "host network setup: failed to add main ENI rule")
		}
	}

	// In strict mode, packets egressing pod veth interfaces must route via the trunk ENI in order for security group
	// rules to be applied. Therefore, the rule to lookup the local routing table is moved to a lower priority than VLAN rules.
	if enablePodENI && n.podSGEnforcingMode == sgpp.EnforcingModeStrict {
		localRule := n.netLink.NewRule()
		localRule.Table = localRouteTable
		localRule.Priority = localRulePriority
		localRule.Family = ipFamily
		// Add new rule with higher priority
		err := n.netLink.RuleAdd(localRule)
		if err != nil && !isRuleExistsError(err) {
			return errors.Wrap(err, "ChangeLocalRulePriority: unable to update local rule priority")
		}
		// Delete the priority 0 rule
		localRule.Priority = 0
		err = n.netLink.RuleDel(localRule)
		if err != nil && !containsNoSuchRule(err) {
			return errors.Wrap(err, "ChangeLocalRulePriority: failed to delete priority 0 local rule")
		}

		// In IPv6 strict mode, ICMPv6 packets from the gateway must lookup in the local routing table so that branch interfaces can resolve their gateway.
		if v6Enabled {
			if err := n.createIPv6GatewayRule(); err != nil {
				return errors.Wrapf(err, "failed to install IPv6 gateway rule")
			}
		}
	}

	return n.updateHostIptablesRules(vpcCIDRs, primaryMAC, primaryAddr, v6Enabled)
}

// UpdateHostIptablesRules updates the NAT table rules based on the VPC CIDRs configuration
func (n *linuxNetwork) UpdateHostIptablesRules(vpcCIDRs []string, primaryMAC string, primaryAddr *net.IP,
	v6Enabled bool) error {
	return n.updateHostIptablesRules(vpcCIDRs, primaryMAC, primaryAddr, v6Enabled)
}

func (n *linuxNetwork) CleanUpStaleAWSChains(v4Enabled, v6Enabled bool) error {
	ipProtocol := iptables.ProtocolIPv4
	if v6Enabled {
		ipProtocol = iptables.ProtocolIPv6
	}

	ipt, err := n.newIptables(ipProtocol)
	if err != nil {
		return errors.Wrap(err, "stale chain cleanup: failed to create iptables")
	}

	exists, err := ipt.ChainExists("nat", "AWS-SNAT-CHAIN-1")
	if err != nil {
		return errors.Wrap(err, "stale chain cleanup: failed to check if AWS-SNAT-CHAIN-1 exists")
	}

	if exists {
		existingChains, err := ipt.ListChains("nat")
		if err != nil {
			return errors.Wrap(err, "stale chain cleanup: failed to list iptables nat chains")
		}

		for _, chain := range existingChains {
			if !strings.HasPrefix(chain, "AWS-CONNMARK-CHAIN") && !strings.HasPrefix(chain, "AWS-SNAT-CHAIN") {
				continue
			}
			parsedChain := strings.Split(chain, "-")
			chainNum, err := strconv.Atoi(parsedChain[len(parsedChain)-1])
			if err != nil {
				return errors.Wrap(err, "stale chain cleanup: failed to convert string to int")
			}
			// Chains 1 --> x (0 indexed) will be stale
			if chainNum > 0 {
				// No need to clear the chain since computeStaleIptablesRules cleans up all rules already
				log.Infof("Deleting stale chain: %s", chain)
				err := ipt.DeleteChain("nat", chain)
				if err != nil {
					return errors.Wrapf(err, "stale chain cleanup: failed to delete chain %s", chain)
				}
			}
		}
	}
	return nil
}

func (n *linuxNetwork) updateHostIptablesRules(vpcCIDRs []string, primaryMAC string, primaryAddr *net.IP,
	v6Enabled bool) error {
	primaryIntf, err := findPrimaryInterfaceName(primaryMAC)
	if err != nil {
		return errors.Wrapf(err, "failed to SetupHostNetwork")
	}

	ipProtocol := iptables.ProtocolIPv4
	if v6Enabled {
		// For v6, we don't add any SNAT or Connmark rules as traffic enters and exits from the ENI it came from
		return nil
	}

	if !v6Enabled {
		ipt, err := n.newIptables(ipProtocol)
		if err != nil {
			return errors.Wrap(err, "host network setup: failed to create iptables")
		}

		iptablesSNATRules, err := n.buildIptablesSNATRules(vpcCIDRs, primaryAddr, primaryIntf, ipt)
		if err != nil {
			return err
		}
		if err := n.updateIptablesRules(iptablesSNATRules, ipt); err != nil {
			return err
		}

		// iptablesConnmarkRules, err := n.buildIptablesConnmarkRules(vpcCIDRs, ipt)
		// if err != nil {
		// 	return err
		// }
		// if err := n.updateIptablesRules(iptablesConnmarkRules, ipt); err != nil {
		// 	return err
		// }

		if err := n.setupNftablesConnmarkRules(vpcCIDRs); err != nil {
			fmt.Printf("!!!!! ERROR IN CONNMARK: %s", err)
			return err
		}
		if err := n.cleanupIptablesConnmarkRules(ipt); err != nil {
			fmt.Printf("!!!!! ERROR IN CLEANUP: %s", err)
			return err
		}
	}

	return nil
}

func (n *linuxNetwork) cleanupIptablesConnmarkRules(ipt iptableswrapper.IPTablesIface) error {
	// Delete the PREROUTING jump rule to AWS-CONNMARK-CHAIN-0
	jumpRule := []string{
		"-i", n.vethPrefix + "+", "-m", "comment", "--comment", "AWS, outbound connections",
		"-j", "AWS-CONNMARK-CHAIN-0",
	}
	if err := ipt.Delete("nat", "PREROUTING", jumpRule...); err != nil && !isNotExistError(err) {
		log.Warnf("failed to delete connmark jump rule: %v", err)
	}

	// Delete the PREROUTING restore-mark rule
	restoreRule := []string{
		"-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK",
		"--restore-mark", "--mask", fmt.Sprintf("%#x", n.mainENIMark),
	}
	if err := ipt.Delete("nat", "PREROUTING", restoreRule...); err != nil && !isNotExistError(err) {
		log.Warnf("failed to delete connmark restore rule: %v", err)
	}

	// Flush and delete AWS-CONNMARK-CHAIN-0 if it exists
	if err := ipt.ClearChain("nat", "AWS-CONNMARK-CHAIN-0"); err != nil && !isNotExistError(err) {
		log.Warnf("failed to clear AWS-CONNMARK-CHAIN-0: %v", err)
	}
	if err := ipt.DeleteChain("nat", "AWS-CONNMARK-CHAIN-0"); err != nil && !isNotExistError(err) {
		log.Warnf("failed to delete AWS-CONNMARK-CHAIN-0: %v", err)
	}

	return nil
}

func isNotExistError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "does not exist")
}

const (
	nftTableName     = "aws-cni"
	nftBaseChainName = "nat-prerouting"
	nftChainName     = "snat-mark"
)

func getBaseChain(conn *nftables.Conn, table *nftables.Table) *nftables.Chain {
	chain, err := conn.ListChain(table, nftBaseChainName)
	if err != nil {
		log.Error(err.Error())
		return nil
	}
	return chain
}

func getDesiredPriority(conn *nftables.Conn) nftables.ChainPriority {
	return nftables.ChainPriority(-90)
}

func isBaseChainConfigCorrect(conn *nftables.Conn, chain *nftables.Chain) bool {
	return chain.Hooknum == nftables.ChainHookPrerouting &&
		chain.Priority != nil && *chain.Priority == getDesiredPriority(conn) &&
		chain.Policy != nil && *chain.Policy == nftables.ChainPolicyAccept
}

func ensureBaseChain(conn *nftables.Conn, table *nftables.Table) *nftables.Chain {
	existing := getBaseChain(conn, table)
	if existing != nil && !isBaseChainConfigCorrect(conn, existing) {
		// delete and re-add chain
		// need to flush the rules before deleting chain.
		// https://wiki.nftables.org/wiki-nftables/index.php/Configuring_chains#Deleting_chains
		conn.FlushChain(existing)
		conn.DelChain(existing)
		existing = nil
	}
	if existing == nil {
		priority := getDesiredPriority(conn)
		chain := conn.AddChain(&nftables.Chain{
			Name:     nftBaseChainName,
			Table:    table,
			Type:     nftables.ChainTypeNAT,
			Hooknum:  nftables.ChainHookPrerouting,
			Priority: &priority,
		})
		return chain
	}
	return existing
}

func ensureConnmarkChain(conn *nftables.Conn, table *nftables.Table) (*nftables.Chain, error) {
	existing := getConnmarkChain(conn, table)
	if existing != nil {
		return existing, nil
	}
	return conn.AddChain(&nftables.Chain{
		Name:  nftChainName,
		Table: table,
	}), nil
}

func getConnmarkChain(conn *nftables.Conn, table *nftables.Table) *nftables.Chain {
	chain, err := conn.ListChain(table, nftChainName)
	if err != nil {
		log.Error(err.Error())
		return nil
	}
	return chain
}

// it has two rules, one to match vethprefix and jump to connmark chain, second is to mark packet with conntrack mark
func ensureBaseChainRules(conn *nftables.Conn, table *nftables.Table, baseChain, targetChain *nftables.Chain, vethPrefix string, mark uint32) error {
	rules, err := conn.GetRules(table, baseChain)
	if err != nil {
		log.Error(err.Error())
		return err
	}
	hasJumpRule := false
	hasRestoreRule := false

	for _, rule := range rules {
		if isJumpRule(rule, targetChain.Name, vethPrefix) {
			hasJumpRule = true
			continue
		}
		if isRestoreRule(rule, mark) {
			hasRestoreRule = true
		}
	}
	if !hasJumpRule {
		addJumpRule(conn, table, baseChain, targetChain, vethPrefix)
	}
	if !hasRestoreRule {
		addRestoreRule(conn, table, baseChain, mark)
	}
	return nil
}
func addJumpRule(conn *nftables.Conn, table *nftables.Table, baseChain, targetChain *nftables.Chain, vethPrefix string) {
	conn.AddRule(&nftables.Rule{
		Table: table,
		Chain: baseChain,
		Exprs: []expr.Any{
			&expr.Meta{Key: expr.MetaKeyIIFNAME, Register: 1},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte(vethPrefix + "*\x00")},
			&expr.Counter{},
			&expr.Verdict{
				Kind:  expr.VerdictJump,
				Chain: targetChain.Name,
			},
		},
	})
}

func addRestoreRule(conn *nftables.Conn, table *nftables.Table, chain *nftables.Chain, mark uint32) {
	markBytes := binaryutil.NativeEndian.PutUint32(mark)
	conn.AddRule(&nftables.Rule{
		Table: table,
		Chain: chain,
		Exprs: []expr.Any{
			&expr.Counter{},
			&expr.Ct{Key: expr.CtKeyMARK, Register: 1},                                                          // load ct mark
			&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: 4, Mask: markBytes, Xor: []byte{0, 0, 0, 0}}, // AND with mask
			&expr.Meta{Key: expr.MetaKeyMARK, SourceRegister: true, Register: 1},                                // store to fwmark
		},
	})
}

func isJumpRule(rule *nftables.Rule, targetChain, vethPrefix string) bool {
	// check if rule is a jump rule to connmark chain
	// return true if it is
	// return false if it is not
	hasIFaceMatch := false
	hasJump := false
	hasCounter := false

	for _, e := range rule.Exprs {
		if cmp, ok := e.(*expr.Cmp); ok && bytes.Equal(cmp.Data, []byte(vethPrefix+"*\x00")) {
			hasIFaceMatch = true
		}
		if _, ok := e.(*expr.Counter); ok {
			hasCounter = true
		}
		if v, ok := e.(*expr.Verdict); ok && v.Kind == expr.VerdictJump && v.Chain == targetChain {
			hasJump = true
		}
	}
	return hasIFaceMatch && hasJump && hasCounter
}

func isRestoreRule(rule *nftables.Rule, mark uint32) bool {
	// check if rule is a restore rule
	// return true if it is
	// return false if it is not
	hasCounter := false
	hasCtLoad := false
	hasBitwise := false
	hasMetaStore := false
	markBytes := []byte{byte(mark), byte(mark >> 8), byte(mark >> 16), byte(mark >> 24)}
	for _, e := range rule.Exprs {
		if _, ok := e.(*expr.Counter); ok {
			hasCounter = true
		}
		if ct, ok := e.(*expr.Ct); ok && ct.Key == expr.CtKeyMARK && !ct.SourceRegister {
			hasCtLoad = true
		}
		if bw, ok := e.(*expr.Bitwise); ok && bytes.Equal(bw.Mask, markBytes) {
			hasBitwise = true
		}
		if m, ok := e.(*expr.Meta); ok && m.Key == expr.MetaKeyMARK && m.SourceRegister {
			hasMetaStore = true
		}
	}
	return hasCounter && hasCtLoad && hasBitwise && hasMetaStore
}

func ensureConnmarkChainRules(conn *nftables.Conn, table *nftables.Table, chain *nftables.Chain, vpcCIDRs, excludeCIDRs []string, mark uint32) error {
	rules, err := conn.GetRules(table, chain)
	if err != nil {
		return err
	}

	currentCIDRs := make(map[string]*nftables.Rule)
	var setMarkRule *nftables.Rule
	var unknownRules []*nftables.Rule

	for _, r := range rules {
		if cidr := extractCIDRFromRule(r); cidr != "" {
			currentCIDRs[cidr] = r
		} else if isSetMarkRule(r, mark) {
			setMarkRule = r
		} else {
			unknownRules = append(unknownRules, r)
		}
	}

	// Delete unknown rules
	for _, r := range unknownRules {
		conn.DelRule(r)
	}

	desiredCIDRs := make(map[string]bool)
	for _, cidr := range append(vpcCIDRs, excludeCIDRs...) {
		desiredCIDRs[cidr] = true
	}

	// Delete stale CIDRs
	for cidr, rule := range currentCIDRs {
		if !desiredCIDRs[cidr] {
			conn.DelRule(rule)
		}
	}

	// Insert missing CIDRs (prepends - order doesn't matter for CIDR rules)
	for cidr := range desiredCIDRs {
		if _, exists := currentCIDRs[cidr]; !exists {
			insertCIDRReturnRule(conn, table, chain, cidr) // InsertRule
		}
	}

	// Ensure set-mark rule exists (AddRule appends to end)
	if setMarkRule == nil {
		addSetMarkRule(conn, table, chain, mark)
	}

	return nil
}
func extractCIDRFromRule(rule *nftables.Rule) string {
	var ip net.IP
	var mask net.IPMask
	hasPayload := false
	hasReturn := false

	for _, e := range rule.Exprs {
		if _, ok := e.(*expr.Payload); ok {
			hasPayload = true
		}
		if v, ok := e.(*expr.Verdict); ok && v.Kind == expr.VerdictReturn {
			hasReturn = true
		}
		if bw, ok := e.(*expr.Bitwise); ok && len(bw.Mask) == 4 {
			mask = net.IPMask(bw.Mask)
		}
		if cmp, ok := e.(*expr.Cmp); ok && cmp.Op == expr.CmpOpEq && len(cmp.Data) == 4 {
			ip = net.IP(cmp.Data)
		}
	}

	if ip == nil || mask == nil || !hasPayload || !hasReturn {
		return ""
	}
	ones, bits := mask.Size()
	if bits != 32 {
		return ""
	}
	return fmt.Sprintf("%s/%d", ip.String(), ones)
}

func isSetMarkRule(rule *nftables.Rule, mark uint32) bool {
	hasCtLoad := false
	hasBitwise := false
	hasCtStore := false
	markBytes := binaryutil.NativeEndian.PutUint32(mark)

	for _, e := range rule.Exprs {
		if ct, ok := e.(*expr.Ct); ok && ct.Key == expr.CtKeyMARK {
			if ct.SourceRegister {
				hasCtStore = true
			} else {
				hasCtLoad = true
			}
		}
		// ct mark | 0x80 uses Mask=0xFFFFFFFF, Xor=mark
		if bw, ok := e.(*expr.Bitwise); ok {
			if bytes.Equal(bw.Xor, markBytes) && bytes.Equal(bw.Mask, []byte{0xff, 0xff, 0xff, 0xff}) {
				hasBitwise = true
			}
		}
	}
	return hasCtLoad && hasBitwise && hasCtStore
}

func addSetMarkRule(conn *nftables.Conn, table *nftables.Table, chain *nftables.Chain, mark uint32) {
	markBytes := binaryutil.NativeEndian.PutUint32(mark)
	conn.AddRule(&nftables.Rule{
		Table: table,
		Chain: chain,
		Exprs: []expr.Any{
			&expr.Counter{},
			&expr.Ct{Key: expr.CtKeyMARK, Register: 1},
			&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: 4, Mask: []byte{0xff, 0xff, 0xff, 0xff}, Xor: markBytes},
			&expr.Ct{Key: expr.CtKeyMARK, Register: 1, SourceRegister: true},
		},
	})
}

func insertCIDRReturnRule(conn *nftables.Conn, table *nftables.Table, chain *nftables.Chain, cidrStr string) {
	_, cidr, err := net.ParseCIDR(cidrStr)
	if err != nil {
		return
	}
	conn.InsertRule(&nftables.Rule{
		Table: table,
		Chain: chain,
		Exprs: []expr.Any{
			&expr.Counter{},
			&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 16, Len: 4},
			&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: 4, Mask: cidr.Mask, Xor: []byte{0, 0, 0, 0}},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: cidr.IP.To4()},
			&expr.Verdict{Kind: expr.VerdictReturn},
		},
	})
}

func (n *linuxNetwork) setupNftablesConnmarkRules(vpcCIDRs []string) error {
	// 1. check for current state of nftable
	// 2.
	if n.useExternalSNAT {
		return n.cleanupNftablesConnmarkRules()
	}

	conn, err := nftables.New()
	if err != nil {
		return errors.Wrap(err, "failed to create nftables connection")
	}

	// idempotent
	table := conn.AddTable(&nftables.Table{
		Family: nftables.TableFamilyIPv4,
		Name:   nftTableName,
	})

	baseChain := ensureBaseChain(conn, table)
	connmarkChain, _ := ensureConnmarkChain(conn, table)
	if err := conn.Flush(); err != nil {
		return errors.Wrap(err, "failed to flush nftable after base chain reconciliation")
	}

	err = ensureBaseChainRules(conn, table, baseChain, connmarkChain, n.vethPrefix, n.mainENIMark)
	if err != nil {
		log.Error(err.Error())
	}
	err = ensureConnmarkChainRules(conn, table, connmarkChain, vpcCIDRs, n.excludeSNATCIDRs, n.mainENIMark)
	if err != nil {
		log.Error(err.Error())
	}

	if err := conn.Flush(); err != nil {
		return errors.Wrap(err, "failed to flush nftable after chain rules reconciliation")
	}

	return nil
}

func (n *linuxNetwork) cleanupNftablesConnmarkRules() error {
	conn, err := nftables.New()
	if err != nil {
		return errors.Wrap(err, "failed to create nftables connection")
	}

	conn.DelTable(&nftables.Table{
		Family: nftables.TableFamilyIPv4,
		Name:   nftTableName,
	})

	_ = conn.Flush()
	return nil
}

func (n *linuxNetwork) buildIptablesSNATRules(vpcCIDRs []string, primaryAddr *net.IP, primaryIntf string, ipt iptableswrapper.IPTablesIface) ([]iptablesRule, error) {
	type snatCIDR struct {
		cidr        string
		isExclusion bool
	}
	var allCIDRs []snatCIDR
	for _, cidr := range vpcCIDRs {
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
	chain := "AWS-SNAT-CHAIN-0"
	log.Debugf("Setup Host Network: iptables -N %s -t nat", chain)
	if err := ipt.NewChain("nat", chain); err != nil && !containChainExistErr(err) {
		log.Errorf("ipt.NewChain error for chain [%s]: %v", chain, err)
		return []iptablesRule{}, errors.Wrapf(err, "host network setup: failed to add chain")
	}
	chains = append(chains, chain)

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

	// Exclude VPC traffic from SNAT rule
	for _, cidr := range allCIDRs {
		comment := "AWS SNAT CHAIN"
		if cidr.isExclusion {
			comment += " EXCLUSION"
		}
		log.Debugf("Setup Host Network: iptables -A %s -d %s -m comment --comment %s -t nat -j %s", chain, cidr.cidr, comment, "RETURN")

		iptableRules = append(iptableRules, iptablesRule{
			name:        chain,
			shouldExist: !n.useExternalSNAT,
			table:       "nat",
			chain:       chain,
			rule: []string{
				"-d", cidr.cidr, "-m", "comment", "--comment", comment, "-j", "RETURN",
			}})
	}

	// Prepare the Desired Rule for SNAT Rule for non-pod ENIs
	snatRule := []string{"!", "-o", "vlan+",
		"-m", "comment", "--comment", "AWS, SNAT",
		"-m", "addrtype", "!", "--dst-type", "LOCAL",
		"-j", "SNAT", "--to-source", primaryAddr.String()}
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

	snatStaleRules, err := computeStaleIptablesRules(ipt, "nat", "AWS-SNAT-CHAIN", iptableRules, chains)
	if err != nil {
		return []iptablesRule{}, err
	}

	iptableRules = append(iptableRules, snatStaleRules...)

	iptableRules = append(iptableRules, iptablesRule{
		name:        "last SNAT rule for non-VPC outbound traffic",
		shouldExist: !n.useExternalSNAT,
		table:       "nat",
		chain:       chain,
		rule:        snatRule,
	})

	iptableRules = append(iptableRules, iptablesRule{
		name:        "connmark for primary ENI",
		shouldExist: n.nodePortSupportEnabled,
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
		shouldExist: n.nodePortSupportEnabled || !n.useExternalSNAT,
		table:       "mangle",
		chain:       "PREROUTING",
		rule: []string{
			"-m", "comment", "--comment", "AWS, primary ENI",
			"-i", n.vethPrefix + "+", "-j", "CONNMARK", "--restore-mark", "--mask", fmt.Sprintf("%#x", n.mainENIMark),
		},
	})

	iptableRules = append(iptableRules, iptablesRule{
		name:        "connmark restore for primary ENI from vlan",
		shouldExist: n.nodePortSupportEnabled,
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

func (n *linuxNetwork) buildIptablesConnmarkRules(vpcCIDRs []string, ipt iptableswrapper.IPTablesIface) ([]iptablesRule, error) {
	var allCIDRs []string
	allCIDRs = append(allCIDRs, vpcCIDRs...)
	allCIDRs = append(allCIDRs, n.excludeSNATCIDRs...)
	excludeCIDRs := sets.NewString(n.excludeSNATCIDRs...)

	log.Debugf("Total CIDRs to exempt from connmark rules - %d", len(allCIDRs))

	var chains []string
	chain := "AWS-CONNMARK-CHAIN-0"
	log.Debugf("Setup Host Network: iptables -N %s -t nat", chain)
	if err := ipt.NewChain("nat", chain); err != nil && !containChainExistErr(err) {
		log.Errorf("ipt.NewChain error for chain [%s]: %v", chain, err)
		return []iptablesRule{}, errors.Wrapf(err, "host network setup: failed to add chain")
	}
	chains = append(chains, chain)

	var iptableRules []iptablesRule
	log.Debugf("Setup Host Network: iptables -t nat -A PREROUTING -i %s+ -m comment --comment \"AWS, outbound connections\" -j AWS-CONNMARK-CHAIN-0", n.vethPrefix)
	// Force delete legacy rule: the rule was matching on "-m state --state NEW", which is
	// always true for packets traversing the nat table
	iptableRules = append(iptableRules, iptablesRule{
		name:        "connmark rule for non-VPC outbound traffic",
		shouldExist: false,
		table:       "nat",
		chain:       "PREROUTING",
		rule: []string{
			"-i", n.vethPrefix + "+", "-m", "comment", "--comment", "AWS, outbound connections",
			"-m", "state", "--state", "NEW", "-j", "AWS-CONNMARK-CHAIN-0",
		}})
	iptableRules = append(iptableRules, iptablesRule{
		name:        "connmark rule for non-VPC outbound traffic",
		shouldExist: !n.useExternalSNAT,
		table:       "nat",
		chain:       "PREROUTING",
		rule: []string{
			"-i", n.vethPrefix + "+", "-m", "comment", "--comment", "AWS, outbound connections",
			"-j", "AWS-CONNMARK-CHAIN-0",
		}})

	for _, cidr := range allCIDRs {
		comment := "AWS CONNMARK CHAIN, VPC CIDR"
		if excludeCIDRs.Has(cidr) {
			comment = "AWS CONNMARK CHAIN, EXCLUDED CIDR"
		}
		log.Debugf("Setup Host Network: iptables -A %s -d %s -t nat -j %s", chain, cidr, "RETURN")

		iptableRules = append(iptableRules, iptablesRule{
			name:        chain,
			shouldExist: !n.useExternalSNAT,
			table:       "nat",
			chain:       chain,
			rule: []string{
				"-d", cidr, "-m", "comment", "--comment", comment, "-j", "RETURN",
			}})
	}

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

	// Being in the nat table, this only applies to the first packet of the connection. The mark
	// will be restored in the mangle table for subsequent packets.
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

	iptableRules = append(iptableRules, iptablesRule{
		name:        "connmark rule for external outbound traffic",
		shouldExist: !n.useExternalSNAT,
		table:       "nat",
		chain:       chain,
		rule: []string{
			"-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK",
			"--set-xmark", fmt.Sprintf("%#x/%#x", n.mainENIMark, n.mainENIMark),
		},
	})

	log.Debugf("iptableRules: %v", iptableRules)
	return iptableRules, nil
}

func (n *linuxNetwork) updateIptablesRules(iptableRules []iptablesRule, ipt iptableswrapper.IPTablesIface) error {
	for _, rule := range iptableRules {
		log.Debugf("execute iptable rule : %s", rule.name)
		exists, err := ipt.Exists(rule.table, rule.chain, rule.rule...)
		log.Debugf("rule %v exists %v, err %v", rule, exists, err)
		if err != nil {
			log.Errorf("host network setup: failed to check existence of %v, %v", rule, err)
			return errors.Wrapf(err, "host network setup: failed to check existence of %v", rule)
		}

		if !exists && rule.shouldExist {
			if rule.name == "AWS-CONNMARK-CHAIN-0" || rule.name == "AWS-SNAT-CHAIN-0" {
				// All CIDR rules must go before the SNAT/Mark rule
				err = ipt.Insert(rule.table, rule.chain, 1, rule.rule...)
				if err != nil {
					log.Errorf("host network setup: failed to insert %v, %v", rule, err)
					return errors.Wrapf(err, "host network setup: failed to add %v", rule)
				}
			} else {
				err = ipt.Append(rule.table, rule.chain, rule.rule...)
				if err != nil {
					log.Errorf("host network setup: failed to add %v, %v", rule, err)
					return errors.Wrapf(err, "host network setup: failed to add %v", rule)
				}
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

func listCurrentIptablesRules(ipt iptableswrapper.IPTablesIface, table, chainPrefix string) ([]iptablesRule, error) {
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
			log.Debugf("host network setup: found potentially stale rule for chain %s: %v", chain, ruleSpec)
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

func computeStaleIptablesRules(ipt iptableswrapper.IPTablesIface, table, chainPrefix string, newRules []iptablesRule, chains []string) ([]iptablesRule, error) {
	var staleRules []iptablesRule
	existingRules, err := listCurrentIptablesRules(ipt, table, chainPrefix)
	if err != nil {
		return []iptablesRule{}, errors.Wrapf(err, "host network setup: failed to list rules from table %s with chain prefix %s", table, chainPrefix)
	}
	activeChains := sets.NewString(chains...)
	log.Debugf("Setup Host Network: computing stale iptables rules for %s table with chain prefix %s", table, chainPrefix)
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

func containChainExistErr(err error) bool {
	return strings.Contains(err.Error(), "Chain already exists")
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

// NetLinkRuleDelAll deletes all matching route rules (instead of only first instance).
func NetLinkRuleDelAll(nl netlinkwrapper.NetLink, rule *netlink.Rule) error {
	for {
		if err := nl.RuleDel(rule); err != nil {
			if !containsNoSuchRule(err) {
				return err
			}
			break
		}
	}
	return nil
}

func ContainsNoSuchRule(err error) bool {
	return containsNoSuchRule(err)
}

func containsNoSuchRule(err error) bool {
	if errno, ok := err.(syscall.Errno); ok {
		return errno == syscall.ENOENT
	}
	return false
}

func IsRuleExistsError(err error) bool {
	return isRuleExistsError(err)
}

func isRuleExistsError(err error) bool {
	if errno, ok := err.(syscall.Errno); ok {
		return errno == syscall.EEXIST
	}
	return false
}

// GetConfigForDebug returns the active values of the configuration env vars (for debugging purposes).
func GetConfigForDebug() map[string]interface{} {
	return map[string]interface{}{
		envConnmark:             getConnmark(),
		envExcludeSNATCIDRs:     parseCIDRString(envExcludeSNATCIDRs),
		envExternalSNAT:         useExternalSNAT(),
		envExternalServiceCIDRs: parseCIDRString(envExternalServiceCIDRs),
		envMTU:                  GetEthernetMTU(),
		envVethPrefix:           getVethPrefixName(),
		envNodePortSupport:      nodePortSupportEnabled(),
		envRandomizeSNAT:        typeOfSNAT(),
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

func (n *linuxNetwork) Ipv6EgressEnabled() bool {
	return ipV6EgressEnabled()
}

func ipV6EgressEnabled() bool {
	return getBoolEnvVar(envEnIpv6Egress, false)
}

// GetExcludeSNATCIDRs returns a list of CIDRs that should be excluded from SNAT if UseExternalSNAT is false,
// otherwise it returns an empty list.
func (n *linuxNetwork) GetExcludeSNATCIDRs() []string {
	if useExternalSNAT() {
		return nil
	}
	return parseCIDRString(envExcludeSNATCIDRs)
}

// GetExternalServiceCIDRs return a list of CIDRs that should always be routed to via main routing table.
func (n *linuxNetwork) GetExternalServiceCIDRs() []string {
	return parseCIDRString(envExternalServiceCIDRs)
}

func parseCIDRString(envVar string) []string {
	cidrString := os.Getenv(envVar)
	if cidrString == "" {
		return nil
	}
	var cidrs []string
	for _, cidr := range strings.Split(cidrString, ",") {
		_, parseCIDR, err := net.ParseCIDR(strings.TrimSpace(cidr))
		if err != nil {
			log.Errorf("%v from %s is not a valid CIDR", cidr, envVar)
		} else if parseCIDR.IP.To4() == nil {
			// Skip IPV6 CIDRs
			log.Errorf("%v from %s is an IPV6 CIDR", cidr, envVar)
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

func nodePortSupportEnabled() bool {
	return getBoolEnvVar(envNodePortSupport, true)
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

// GetLinkByMac returns linux netlink based on interface MAC
func (n *linuxNetwork) GetLinkByMac(mac string, retryInterval time.Duration) (netlink.Link, error) {
	return linkByMac(mac, n.netLink, retryInterval)
}

// linkByMac returns linux netlink based on interface MAC
func linkByMac(mac string, netLink netlinkwrapper.NetLink, retryInterval time.Duration) (netlink.Link, error) {
	// The adapter might not be immediately available, so we perform retries
	var lastErr error
	attempt := 0
	for {
		attempt++
		if attempt > maxAttemptsLinkByMac {
			return nil, lastErr
		} else if attempt > 1 {
			time.Sleep(retryInterval)
		}

		links, err := netLink.LinkList()
		if err != nil {
			lastErr = errors.Errorf("%s (attempt %d/%d)", err, attempt, maxAttemptsLinkByMac)
			log.Debugf(lastErr.Error())
			continue
		}

		for _, link := range links {
			if mac == link.Attrs().HardwareAddr.String() {
				log.Debugf("Found the Link that uses mac address %s and its index is %d (attempt %d/%d)",
					mac, link.Attrs().Index, attempt, maxAttemptsLinkByMac)
				return link, nil
			}
		}

		lastErr = errors.Errorf("no interface found which uses mac address %s (attempt %d/%d)", mac, attempt, maxAttemptsLinkByMac)
		log.Debugf(lastErr.Error())
	}
}

// On AWS/VPC, the subnet gateway can always be reached at FE80:EC2::1
// https://aws.amazon.com/about-aws/whats-new/2022/11/ipv6-subnet-default-gateway-router-multiple-addresses/
func GetIPv6Gateway() net.IP {
	return net.IP{0xfe, 0x80, 0x0e, 0xc2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}
}

func GetIPv4Gateway(eniSubnetCIDR *net.IPNet) net.IP {
	gw := eniSubnetCIDR.IP
	incrementIPAddr(gw)
	return gw
}

func (n *linuxNetwork) GetRouteTableNumberForENI(networkCard int, eniIP string, deviceNumber int, maxENIPerNIC int, isV6 bool) (int, bool, error) {
	return getRouteTableNumberForENI(networkCard, eniIP, lo.Ternary(isV6, 128, 32), deviceNumber, maxENIPerNIC, isV6, n.netLink)
}

// SetupENINetwork adds default route to route table (eni-<eni_table>), so it does not need to be called on the primary ENI
func (n *linuxNetwork) SetupENINetwork(eniIP string, eniMAC string, networkCard int, eniSubnetCIDR string, maxENIPerNIC int, isTrunkENI bool, routeTableID int, isRuleConfigured bool) error {
	return setupENINetwork(eniIP, eniMAC, networkCard, eniSubnetCIDR, n.netLink, retryLinkByMacInterval, retryRouteAddInterval, n.mtu, maxENIPerNIC, isTrunkENI, routeTableID, isRuleConfigured)
}

func setupENINetwork(eniIP string, eniMac string, networkCard int, eniSubnetCIDR string, netLink netlinkwrapper.NetLink,
	retryLinkByMacInterval time.Duration, retryRouteAddInterval time.Duration, mtu int, maxENIPerNIC int, isTrunkENI bool, routeTableID int, isRuleConfigured bool) error {

	// routeTableID should only be unix.RT_TABLE_MAIN for primary ENI and should never be passed to this function
	if routeTableID == unix.RT_TABLE_MAIN {
		return errors.New("setupENINetwork should never be called on the primary ENI")
	}

	isV6 := strings.Contains(eniSubnetCIDR, ":")
	_, eniSubnetIPNet, err := net.ParseCIDR(eniSubnetCIDR)
	if err != nil {
		return errors.Wrapf(err, "setupENINetwork: invalid IP CIDR block %s", eniSubnetCIDR)
	}

	// Get Networking defaults
	family, mask, zeroAddr, gw := func() (int, int, net.IP, net.IP) {
		if isV6 {
			return unix.AF_INET6, 128, net.IPv6zero, GetIPv6Gateway()
		}
		return unix.AF_INET, 32, net.IPv4zero, GetIPv4Gateway(eniSubnetIPNet)
	}()

	log.Infof("Setting up network for an ENI with IP address %s, MAC address %s, CIDR %s, route table %d and Network Card %d",
		eniIP, eniMac, eniSubnetCIDR, routeTableID, networkCard)

	link, err := linkByMac(eniMac, netLink, retryLinkByMacInterval)
	if err != nil {
		return errors.Wrapf(err, "setupENINetwork: failed to find the link which uses MAC address %s", eniMac)
	}

	if err = netLink.LinkSetMTU(link, mtu); err != nil {
		return errors.Wrapf(err, "setupENINetwork: failed to set MTU to %d for %s", mtu, eniIP)
	}

	eniAddr := &net.IPNet{
		IP:   net.ParseIP(eniIP),
		Mask: eniSubnetIPNet.Mask,
	}

	if err = netLink.LinkSetUp(link); err != nil {
		return errors.Wrapf(err, "setupENINetwork: failed to bring up ENI %s", eniIP)
	}

	// Explicitly delete IP addresses assigned to the device before assign ENI IP.
	// For IPv6, do not delete the link-local address.
	log.Debugf("Setting up ENI's primary IP %s", eniIP)
	var addrs []netlink.Addr
	addrs, err = netLink.AddrList(link, family)
	if err != nil {
		return errors.Wrap(err, "setupENINetwork: failed to list IP address for ENI")
	}

	for _, addr := range addrs {
		if addr.IP.IsGlobalUnicast() {
			log.Debugf("Deleting existing IP address %s", addr.String())
			if err = netLink.AddrDel(link, &addr); err != nil {
				return errors.Wrap(err, "setupENINetwork: failed to delete IP addr from ENI")
			}
		}
	}

	log.Debugf("Adding IP address %s", eniAddr.String())
	if err = netLink.AddrAdd(link, &netlink.Addr{IPNet: eniAddr}); err != nil {
		return errors.Wrap(err, "setupENINetwork: failed to add IP addr to ENI")
	}

	linkIndex := link.Attrs().Index
	log.Debugf("Setting up ENI's default gateway %v, table %d, linkIndex %d", gw, routeTableID, linkIndex)

	routes := []netlink.Route{
		// Add a direct link route for the host's ENI IP only
		{
			LinkIndex: linkIndex,
			Dst:       &net.IPNet{IP: gw, Mask: net.CIDRMask(mask, mask)},
			Scope:     netlink.SCOPE_LINK,
			Table:     routeTableID,
		},
		// Route all other traffic via the host's ENI IP
		{
			LinkIndex: linkIndex,
			Dst:       &net.IPNet{IP: zeroAddr, Mask: net.CIDRMask(0, mask)},
			Scope:     netlink.SCOPE_UNIVERSE,
			Gw:        gw,
			Table:     routeTableID,
		},
	}
	for _, r := range routes {
		err := netLink.RouteDel(&r)
		if err != nil && !netlinkwrapper.IsNotExistsError(err) {
			return errors.Wrap(err, "setupENINetwork: failed to clean up old routes")
		}

		err = retry.NWithBackoff(retry.NewSimpleBackoff(500*time.Millisecond, retryRouteAddInterval, 0.15, 2.0), maxRetryRouteAdd, func() error {
			if err := netLink.RouteReplace(&r); err != nil {
				log.Debugf("Not able to set route %s/0 via %s table %d", r.Dst.IP.String(), gw.String(), routeTableID)
				return errors.Wrapf(err, "setupENINetwork: unable to replace route entry %s", r.Dst.IP.String())
			}
			log.Debugf("Successfully added/replaced route to be %s/0", r.Dst.IP.String())
			return nil
		})
		if err != nil {
			return err
		}
	}

	// Remove the route that default out to ENI-x out of main route table
	var defaultRoute netlink.Route
	if isV6 {
		defaultRoute = netlink.Route{
			LinkIndex: linkIndex,
			Dst:       eniSubnetIPNet,
			Table:     mainRoutingTable,
		}
	} else {
		// eniSubnetIPNet was modified by GetIPv4Gateway, so the string must be parsed again
		_, eniSubnetCIDRNet, err := net.ParseCIDR(eniSubnetCIDR)
		if err != nil {
			return errors.Wrapf(err, "setupENINetwork: invalid IPv4 CIDR block: %s", eniSubnetCIDR)
		}
		defaultRoute = netlink.Route{
			Dst:   eniSubnetCIDRNet,
			Src:   net.ParseIP(eniIP),
			Table: mainRoutingTable,
			Scope: netlink.SCOPE_LINK,
		}
	}
	if err := netLink.RouteDel(&defaultRoute); err != nil {
		if !netlinkwrapper.IsNotExistsError(err) {
			return errors.Wrapf(err, "setupENINetwork: unable to delete default route %s for source IP %s", eniSubnetIPNet.String(), eniIP)
		}
	}

	// Add IP rule for Primary IP of the ENI
	if !isTrunkENI && !isRuleConfigured {
		ruleForPrimaryIPofENI := netlink.NewRule()
		ruleForPrimaryIPofENI.Src = &net.IPNet{IP: net.ParseIP(eniIP), Mask: net.CIDRMask(mask, mask)}
		ruleForPrimaryIPofENI.Table = routeTableID
		ruleForPrimaryIPofENI.Priority = FromPrimaryIPofENIRulePriority
		ruleForPrimaryIPofENI.Family = family
		log.Infof("Adding rule for Primary IP [%v]", ruleForPrimaryIPofENI)
		if err := netLink.RuleAdd(ruleForPrimaryIPofENI); err != nil {
			if !isRuleExistsError(err) {
				return errors.Wrapf(err, "setupENINetwork: unable to add rule for ENI IP %s", eniIP)
			}
			log.Debugf("setupENINetwork: rule for Primary IP %s already exists", eniIP)
		} else {
			log.Debugf("setupENINetwork: rule for Primary IP %s added", eniIP)
		}
	} else {
		log.Debugf("setupENINetwork: skipping rule for Primary IP of ENI %s, conditions IsTrunkENI: %t, RuleExists: %t", eniIP, isTrunkENI, isRuleConfigured)
	}

	return nil
}

func getRouteTableNumberForENI(networkCard int, eniIP string, mask int, deviceNumber int, maxENIsPerNetworkCard int, isV6 bool, netLink netlinkwrapper.NetLink) (int, bool, error) {
	var ruleExists bool = false
	var srcRuleList []netlink.Rule

	// Calculate the route table number based on device number and network card
	tableNumber := CalculateRouteTableId(deviceNumber, networkCard)

	if networkCard > 0 {
		ruleList, err := getRuleList(isV6, netLink)
		if err != nil {
			log.Errorf("checkENIHasExistingRules: failed to retrieve ip rule list %v", err)
			return 0, false, err
		}
		// Src IP has to be the /32 or /128 address of the ENI
		srcRuleList, err = getRuleListBySrc(ruleList, net.IPNet{
			IP:   net.ParseIP(eniIP),
			Mask: net.CIDRMask(mask, mask),
		})
		if err != nil {
			log.Errorf("checkENIHasExistingRules: failed to get rule list by source %v", err)
			return 0, false, err
		}

		if len(srcRuleList) == 1 {
			// Reuse the rules present on the node. This happens
			// 1. When AMI is old, CNI had previously setup the ENI (Route Table, IP Rules) or
			// 2. When AMI sets up the ENI Route Table Number
			tableNumber = srcRuleList[0].Table
			ruleExists = true
		} else if len(srcRuleList) > 1 {
			// This will happen on following sequence
			// 1. AMI has been updated which sets up the ENI. CNI is not updated, so it overrides the ENI setup
			// 2. CNI is now updated to the new version on the same node, so now we have two rules
			oldTableNumber := CalculateOldRouteTableId(deviceNumber, networkCard, maxENIsPerNetworkCard)
			for _, rule := range srcRuleList {
				if rule.Table == oldTableNumber {
					tableNumber = oldTableNumber
					break
				}
			}
			ruleExists = true
		}
		// len(srcRuleList) == 0 → No existing rules, so we will create a new one

		log.Infof("checkENIHasExistingRules: found %d rules for source IP %s, will use table number %d", len(srcRuleList), eniIP, tableNumber)
	}

	return tableNumber, ruleExists, nil
}

// For IPv6 strict mode, ICMPv6 packets from the gateway must lookup in the local routing table so that branch interfaces can resolve their gateway.
func (n *linuxNetwork) createIPv6GatewayRule() error {
	gatewayRule := n.netLink.NewRule()
	gatewayRule.Src = &net.IPNet{IP: GetIPv6Gateway(), Mask: net.CIDRMask(128, 128)}
	gatewayRule.IPProto = unix.IPPROTO_ICMPV6
	gatewayRule.Table = localRouteTable
	gatewayRule.Priority = 0
	gatewayRule.Family = unix.AF_INET6
	if n.podSGEnforcingMode == sgpp.EnforcingModeStrict {
		err := n.netLink.RuleAdd(gatewayRule)
		if err != nil && !isRuleExistsError(err) {
			return errors.Wrap(err, "createIPv6GatewayRule: unable to create rule for IPv6 gateway")
		}
	} else {
		// Rule must be deleted when not in strict mode to support transitions.
		err := n.netLink.RuleDel(gatewayRule)
		if !netlinkwrapper.IsNotExistsError(err) {
			return errors.Wrap(err, "createIPv6GatewayRule: unable to delete rule for IPv6 gateway")
		}
	}
	return nil
}

// Increment the given net.IP by one. Incrementing the last IP in an IP space (IPv4, IPV6) is undefined.
func incrementIPAddr(ip net.IP) {
	for i := len(ip) - 1; i >= 0; i-- {
		ip[i]++
		// only add to the next byte if we overflowed
		if ip[i] != 0 {
			break
		}
	}
}

func getRuleList(v6enabled bool, netLink netlinkwrapper.NetLink) ([]netlink.Rule, error) {
	if v6enabled {
		return netLink.RuleList(unix.AF_INET6)
	}
	return netLink.RuleList(unix.AF_INET)
}

// GetRuleList returns IP rules
func (n *linuxNetwork) GetRuleList(v6enabled bool) ([]netlink.Rule, error) {
	return getRuleList(v6enabled, n.netLink)
}

// GetRuleListBySrc returns IP rules with matching source IP
func getRuleListBySrc(ruleList []netlink.Rule, src net.IPNet) ([]netlink.Rule, error) {
	var srcRuleList []netlink.Rule
	for _, rule := range ruleList {
		if rule.Src != nil && rule.Src.IP.Equal(src.IP) {
			srcRuleList = append(srcRuleList, rule)
		}
	}
	return srcRuleList, nil
}

// GetRuleListBySrc returns IP rules with matching source IP
func (n *linuxNetwork) GetRuleListBySrc(ruleList []netlink.Rule, src net.IPNet) ([]netlink.Rule, error) {
	return getRuleListBySrc(ruleList, src)
}

// UpdateRuleListBySrc modify IP rules that have a matching source IP
func (n *linuxNetwork) UpdateRuleListBySrc(ruleList []netlink.Rule, src net.IPNet) error {
	log.Debugf("Update Rule List[%v] for source[%v] ", ruleList, src)

	srcRuleList, err := n.GetRuleListBySrc(ruleList, src)
	if err != nil {
		log.Errorf("UpdateRuleListBySrc: failed to retrieve rule list %v", err)
		return err
	}

	log.Infof("Remove current list [%v]", srcRuleList)
	var srcRuleTable int
	var srcRuleFamily int
	for _, rule := range srcRuleList {
		srcRuleTable = rule.Table
		srcRuleFamily = rule.Family
		if err := n.netLink.RuleDel(&rule); err != nil && !containsNoSuchRule(err) {
			log.Errorf("Failed to cleanup old IP rule: %v", err)
			return errors.Wrapf(err, "UpdateRuleListBySrc: failed to delete old rule")
		}
		var toDst string
		if rule.Dst != nil {
			toDst = rule.Dst.String()
		}
		log.Debugf("UpdateRuleListBySrc: Successfully removed current rule [%v] to %s", rule, toDst)
	}

	if len(srcRuleList) == 0 {
		log.Debug("UpdateRuleListBySrc: empty list, no need to update")
		return nil
	}

	podRule := n.netLink.NewRule()

	podRule.Src = &src
	podRule.Table = srcRuleTable
	podRule.Priority = FromPodRulePriority
	podRule.Family = srcRuleFamily

	err = n.netLink.RuleAdd(podRule)
	if err != nil {
		log.Errorf("Failed to add pod IP rule: %v", err)
		return errors.Wrapf(err, "UpdateRuleListBySrc: failed to add pod rule")
	}
	log.Infof("UpdateRuleListBySrc: Successfully added pod rule[%v]", podRule)

	return nil
}

// UpdateExternalServiceIpRules reconciles existing set of IP rules for external IPs with new set
func (n *linuxNetwork) UpdateExternalServiceIpRules(ruleList []netlink.Rule, externalServiceCidrs []string) error {
	log.Debugf("Update Rule List with set %v", externalServiceCidrs)

	// Delete all existing rules for external service CIDRs. Note that a bulk delete would be ideal here, but
	// netlink does not support bulk delete by priority, so we must iterate over rule list.
	for _, rule := range ruleList {
		if rule.Priority == externalServiceIpRulePriority && rule.Table == mainRoutingTable {
			if err := n.netLink.RuleDel(&rule); err != nil && !containsNoSuchRule(err) {
				log.Errorf("Failed to cleanup old IP rule: %v", err)
				return errors.Wrapf(err, "UpdateExternalServiceIpRules: failed to delete old rule")
			}
		}
	}

	// Program new rules
	for _, cidr := range externalServiceCidrs {
		extIpRule := n.netLink.NewRule()
		extIpRule.Table = mainRoutingTable
		_, netCidr, err := net.ParseCIDR(cidr)
		if err != nil {
			log.Errorf("Failed to create IPNet from %s", cidr)
			return errors.Wrapf(err, "UpdateExternalServiceIpRules: failed to create IPNet")
		}
		extIpRule.Dst = netCidr
		extIpRule.Priority = externalServiceIpRulePriority
		if err := n.netLink.RuleAdd(extIpRule); err != nil {
			log.Errorf("Failed to add external service CIDR rule: %v", err)
			return errors.Wrapf(err, "UpdateExternalServiceIpRules: failed to add rule")
		}
		log.Infof("UpdateExternalServiceIpRules: successfully added rule[%v]", extIpRule)
	}

	return nil
}

// GetEthernetMTU returns the MTU value to program for ENIs. Note that the value was already validated during container initialization.
func GetEthernetMTU() int {
	mtu, _, _ := utils.GetIntFromStringEnvVar(envMTU, defaultMTU)
	return mtu
}

// GetPodMTU validates the pod MTU value. If an invalid value is passed, the default is used.
func GetPodMTU(podMTU string) int {
	mtu, err := strconv.Atoi(podMTU)
	if err != nil {
		log.Errorf("Failed to parse pod MTU %s: %v", podMTU, err)
		return defaultMTU
	}

	// Only IPv4 bounds can be enforced, but note that the conflist value is already validated during container initialization.
	if mtu < minMTUv4 || mtu > defaultMTU {
		return defaultMTU
	}
	return mtu
}

// getVethPrefixName gets the name prefix of the veth devices based on the AWS_VPC_K8S_CNI_VETHPREFIX environment variable
func getVethPrefixName() string {
	if envVal, found := os.LookupEnv(envVethPrefix); found {
		return envVal
	}
	return envVethPrefixDefault
}

func (n *linuxNetwork) DeleteRulesBySrc(eniIP string, isV6 bool) error {

	if eniIP == "" {
		log.Info("DeleteRulesBySrc: eniIP is empty, nothing to delete from rules")
		return nil
	}

	var mask net.IPMask

	if isV6 {
		mask = net.CIDRMask(128, 128)
	} else {
		mask = net.CIDRMask(32, 32)
	}

	eniAddr := &net.IPNet{
		IP:   net.ParseIP(eniIP),
		Mask: mask,
	}

	ruleList, err := n.GetRuleList(isV6)
	if err != nil {
		log.Errorf("DeleteRulesBySrc: failed to retrieve rule list %v", err)
		return err
	}

	srcRuleList, err := n.GetRuleListBySrc(ruleList, *eniAddr)
	if err != nil {
		log.Errorf("DeleteRulesBySrc: failed to retrieve rule list by src %v", err)
		return err
	}

	log.Infof("Removing rules for Primary IP %s of ENI if exists [%v]", eniIP, srcRuleList)
	for _, rule := range srcRuleList {
		if err := n.netLink.RuleDel(&rule); err != nil && !containsNoSuchRule(err) {
			log.Errorf("Failed to cleanup IP rule: %v", err)
			return errors.Wrapf(err, "DeleteRulesBySrc: failed to delete old rule")
		}
		log.Debugf("DeleteRulesBySrc: Successfully removed current rule [%v] from %s", rule, eniIP)
	}
	return nil
}
