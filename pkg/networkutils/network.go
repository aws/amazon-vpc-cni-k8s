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
	"encoding/binary"
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

	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/retry"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/pkg/errors"
	"golang.org/x/sys/unix"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"

	"github.com/coreos/go-iptables/iptables"
	"github.com/vishvananda/netlink"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/netlinkwrapper"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/nswrapper"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/procsyswrapper"
)

const (
	// Local rule, needs to come after the pod ENI rules
	localRulePriority = 20

	// 513 - 1023, can be used priority lower than toPodRulePriority but higher than default nonVPC CIDR rule

	// 1024 is reserved for (ip rule not to <VPC's subnet> table main)
	hostRulePriority = 1024

	// 1025 - 1535 can be used priority lower than fromPodRulePriority but higher than default nonVPC CIDR rule
	fromPodRulePriority = 1536

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

	// This environment variable indicates if ipamd should configure rp filter for primary interface. Default value is
	// true. If set to false, then rp filter should be configured through init container.
	envConfigureRpfilter = "AWS_VPC_K8S_CNI_CONFIGURE_RPFILTER"

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
	SetupHostNetwork(vpcCIDRs []string, primaryMAC string, primaryAddr *net.IP, enablePodENI bool) error
	// SetupENINetwork performs ENI level network configuration. Not needed on the primary ENI
	SetupENINetwork(eniIP string, mac string, deviceNumber int, subnetCIDR string) error
	// UpdateHostIptablesRules updates the nat table iptables rules on the host
	UpdateHostIptablesRules(vpcCIDRs []string, primaryMAC string, primaryAddr *net.IP) error
	UseExternalSNAT() bool
	GetExcludeSNATCIDRs() []string
	GetRuleList() ([]netlink.Rule, error)
	GetRuleListBySrc(ruleList []netlink.Rule, src net.IPNet) ([]netlink.Rule, error)
	UpdateRuleListBySrc(ruleList []netlink.Rule, src net.IPNet) error
	DeleteRuleListBySrc(src net.IPNet) error
	GetLinkByMac(mac string, retryInterval time.Duration) (netlink.Link, error)
}

type linuxNetwork struct {
	useExternalSNAT         bool
	excludeSNATCIDRs        []string
	typeOfSNAT              snatType
	nodePortSupportEnabled  bool
	shouldConfigureRpFilter bool
	mtu                     int
	vethPrefix              string

	netLink     netlinkwrapper.NetLink
	ns          nswrapper.NS
	newIptables func() (iptablesIface, error)
	mainENIMark uint32
	procSys     procsyswrapper.ProcSys
}

type iptablesIface interface {
	Exists(table, chain string, rulespec ...string) (bool, error)
	Insert(table, chain string, pos int, rulespec ...string) error
	Append(table, chain string, rulespec ...string) error
	Delete(table, chain string, rulespec ...string) error
	List(table, chain string) ([]string, error)
	NewChain(table, chain string) error
	ClearChain(table, chain string) error
	DeleteChain(table, chain string) error
	ListChains(table string) ([]string, error)
	HasRandomFully() bool
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
		useExternalSNAT:         useExternalSNAT(),
		excludeSNATCIDRs:        getExcludeSNATCIDRs(),
		typeOfSNAT:              typeOfSNAT(),
		nodePortSupportEnabled:  nodePortSupportEnabled(),
		shouldConfigureRpFilter: shouldConfigureRpFilter(),
		mainENIMark:             getConnmark(),
		mtu:                     GetEthernetMTU(""),
		vethPrefix:              getVethPrefixName(),

		netLink: netlinkwrapper.NewNetLink(),
		ns:      nswrapper.NewNS(),
		newIptables: func() (iptablesIface, error) {
			ipt, err := iptables.New()
			return ipt, err
		},
		procSys: procsyswrapper.NewProcSys(),
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

// SetupHostNetwork performs node level network configuration
func (n *linuxNetwork) SetupHostNetwork(vpcCIDRs []string, primaryMAC string, primaryAddr *net.IP, enablePodENI bool) error {
	log.Info("Setting up host network... ")

	var err error
	primaryIntf := "eth0"
	if n.nodePortSupportEnabled {
		primaryIntf, err = findPrimaryInterfaceName(primaryMAC)
		if err != nil {
			return errors.Wrapf(err, "failed to SetupHostNetwork")
		}
		// If node port support is enabled, configure the kernel's reverse path filter check on eth0 for "loose"
		// filtering. This is required because
		// - NodePorts are exposed on eth0
		// - The kernel's RPF check happens after incoming packets to NodePorts are DNATted to the pod IP.
		// - For pods assigned to secondary ENIs, the routing table includes source-based routing. When the kernel does
		//   the RPF check, it looks up the route using the pod IP as the source.
		// - Thus, it finds the source-based route that leaves via the secondary ENI.
		// - In "strict" mode, the RPF check fails because the return path uses a different interface to the incoming
		//   packet. In "loose" mode, the check passes because some route was found.
		primaryIntfRPFilter := "net/ipv4/conf/" + primaryIntf + "/rp_filter"
		const rpFilterLoose = "2"

		if n.shouldConfigureRpFilter {
			log.Debugf("Setting RPF for primary interface: %s", primaryIntfRPFilter)
			err = n.procSys.Set(primaryIntfRPFilter, rpFilterLoose)
			if err != nil {
				return errors.Wrapf(err, "failed to configure %s RPF check", primaryIntf)
			}
		} else {
			log.Infof("Skip updating RPF for primary interface: %s", primaryIntfRPFilter)
		}
	}

	link, err := linkByMac(primaryMAC, n.netLink, retryLinkByMacInterval)
	if err != nil {
		return errors.Wrapf(err, "setupHostNetwork: failed to find the link primary ENI with MAC address %s", primaryMAC)
	}
	if err = n.netLink.LinkSetMTU(link, n.mtu); err != nil {
		return errors.Wrapf(err, "setupHostNetwork: failed to set MTU to %d for %s", n.mtu, primaryIntf)
	}

	// If node port support is enabled, add a rule that will force force marked traffic out of the main ENI.  We then
	// add iptables rules below that will mark traffic that needs this special treatment.  In particular NodePort
	// traffic always comes in via the main ENI but response traffic would go out of the pod's assigned ENI if we
	// didn't handle it specially. This is because the routing decision is done before the NodePort's DNAT is
	// reversed so, to the routing table, it looks like the traffic is pod traffic instead of NodePort traffic.
	mainENIRule := n.netLink.NewRule()
	mainENIRule.Mark = int(n.mainENIMark)
	mainENIRule.Mask = int(n.mainENIMark)
	mainENIRule.Table = mainRoutingTable
	mainENIRule.Priority = hostRulePriority
	// If this is a restart, cleanup previous rule first
	err = n.netLink.RuleDel(mainENIRule)
	if err != nil && !containsNoSuchRule(err) {
		log.Errorf("Failed to cleanup old main ENI rule: %v", err)
		return errors.Wrapf(err, "host network setup: failed to delete old main ENI rule")
	}

	if n.nodePortSupportEnabled {
		err = n.netLink.RuleAdd(mainENIRule)
		if err != nil {
			log.Errorf("Failed to add host main ENI rule: %v", err)
			return errors.Wrapf(err, "host network setup: failed to add main ENI rule")
		}
	}

	// If we want per pod ENIs, we need to give pod ENIs veth bridges a lower priority that the local table,
	// or the rp_filter check will fail.
	if enablePodENI {
		localRule := n.netLink.NewRule()
		localRule.Table = localRouteTable
		localRule.Priority = localRulePriority
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
	}

	return n.updateHostIptablesRules(vpcCIDRs, primaryMAC, primaryAddr)
}

// UpdateHostIptablesRules updates the NAT table rules based on the VPC CIDRs configuration
func (n *linuxNetwork) UpdateHostIptablesRules(vpcCIDRs []string, primaryMAC string, primaryAddr *net.IP) error {
	return n.updateHostIptablesRules(vpcCIDRs, primaryMAC, primaryAddr)
}

func (n *linuxNetwork) updateHostIptablesRules(vpcCIDRs []string, primaryMAC string, primaryAddr *net.IP) error {
	primaryIntf, err := findPrimaryInterfaceName(primaryMAC)
	if err != nil {
		return errors.Wrapf(err, "failed to SetupHostNetwork")
	}
	ipt, err := n.newIptables()
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

	iptablesConnmarkRules, err := n.buildIptablesConnmarkRules(vpcCIDRs, ipt)
	if err != nil {
		return err
	}
	if err := n.updateIptablesRules(iptablesConnmarkRules, ipt); err != nil {
		return err
	}
	return nil
}

func (n *linuxNetwork) buildIptablesSNATRules(vpcCIDRs []string, primaryAddr *net.IP, primaryIntf string, ipt iptablesIface) ([]iptablesRule, error) {
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
	for i := 0; i <= len(allCIDRs); i++ {
		chain := fmt.Sprintf("AWS-SNAT-CHAIN-%d", i)
		log.Debugf("Setup Host Network: iptables -N %s -t nat", chain)
		if err := ipt.NewChain("nat", chain); err != nil && !containChainExistErr(err) {
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
		shouldExist: n.nodePortSupportEnabled,
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

func (n *linuxNetwork) buildIptablesConnmarkRules(vpcCIDRs []string, ipt iptablesIface) ([]iptablesRule, error) {
	var allCIDRs []string
	allCIDRs = append(allCIDRs, vpcCIDRs...)
	allCIDRs = append(allCIDRs, n.excludeSNATCIDRs...)
	excludeCIDRs := sets.NewString(n.excludeSNATCIDRs...)

	log.Debugf("Total CIDRs to exempt from connmark rules - %d", len(allCIDRs))
	var chains []string
	for i := 0; i <= len(allCIDRs); i++ {
		chain := fmt.Sprintf("AWS-CONNMARK-CHAIN-%d", i)
		log.Debugf("Setup Host Network: iptables -N %s -t nat", chain)
		if err := ipt.NewChain("nat", chain); err != nil && !containChainExistErr(err) {
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

func (n *linuxNetwork) updateIptablesRules(iptableRules []iptablesRule, ipt iptablesIface) error {
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

func listCurrentIptablesRules(ipt iptablesIface, table, chainPrefix string) ([]iptablesRule, error) {
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

func computeStaleIptablesRules(ipt iptablesIface, table, chainPrefix string, newRules []iptablesRule, chains []string) ([]iptablesRule, error) {
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

// GetConfigForDebug returns the active values of the configuration env vars (for debugging purposes).
func GetConfigForDebug() map[string]interface{} {
	return map[string]interface{}{
		envConfigureRpfilter: shouldConfigureRpFilter(),
		envConnmark:          getConnmark(),
		envExcludeSNATCIDRs:  getExcludeSNATCIDRs(),
		envExternalSNAT:      useExternalSNAT(),
		envMTU:               GetEthernetMTU(""),
		envVethPrefix:        getVethPrefixName(),
		envNodePortSupport:   nodePortSupportEnabled(),
		envRandomizeSNAT:     typeOfSNAT(),
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

func nodePortSupportEnabled() bool {
	return getBoolEnvVar(envNodePortSupport, true)
}

func shouldConfigureRpFilter() bool {
	return getBoolEnvVar(envConfigureRpfilter, true)
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

// SetupENINetwork adds default route to route table (eni-<eni_table>), so it does not need to be called on the primary ENI
func (n *linuxNetwork) SetupENINetwork(eniIP string, eniMAC string, deviceNumber int, eniSubnetCIDR string) error {
	return setupENINetwork(eniIP, eniMAC, deviceNumber, eniSubnetCIDR, n.netLink, retryLinkByMacInterval, retryRouteAddInterval, n.mtu)
}

func setupENINetwork(eniIP string, eniMAC string, deviceNumber int, eniSubnetCIDR string, netLink netlinkwrapper.NetLink,
	retryLinkByMacInterval time.Duration, retryRouteAddInterval time.Duration, mtu int) error {
	if deviceNumber == 0 {
		return errors.New("setupENINetwork should never be called on the primary ENI")
	}
	tableNumber := deviceNumber + 1
	log.Infof("Setting up network for an ENI with IP address %s, MAC address %s, CIDR %s and route table %d",
		eniIP, eniMAC, eniSubnetCIDR, tableNumber)
	link, err := linkByMac(eniMAC, netLink, retryLinkByMacInterval)
	if err != nil {
		return errors.Wrapf(err, "setupENINetwork: failed to find the link which uses MAC address %s", eniMAC)
	}

	if err = netLink.LinkSetMTU(link, mtu); err != nil {
		return errors.Wrapf(err, "setupENINetwork: failed to set MTU to %d for %s", mtu, eniIP)
	}

	if err = netLink.LinkSetUp(link); err != nil {
		return errors.Wrapf(err, "setupENINetwork: failed to bring up ENI %s", eniIP)
	}

	_, ipnet, err := net.ParseCIDR(eniSubnetCIDR)
	if err != nil {
		return errors.Wrapf(err, "setupENINetwork: invalid IPv4 CIDR block %s", eniSubnetCIDR)
	}

	gw, err := IncrementIPv4Addr(ipnet.IP)
	if err != nil {
		return errors.Wrapf(err, "setupENINetwork: failed to define gateway address from %v", ipnet.IP)
	}

	// Explicitly set the IP on the device if not already set.
	// Required for older kernels.
	// ip addr show
	// ip add del <eniIP> dev <link> (if necessary)
	// ip add add <eniIP> dev <link>
	log.Debugf("Setting up ENI's primary IP %s", eniIP)
	addrs, err := netLink.AddrList(link, unix.AF_INET)
	if err != nil {
		return errors.Wrap(err, "setupENINetwork: failed to list IP address for ENI")
	}

	for _, addr := range addrs {
		log.Debugf("Deleting existing IP address %s", addr.String())
		if err = netLink.AddrDel(link, &addr); err != nil {
			return errors.Wrap(err, "setupENINetwork: failed to delete IP addr from ENI")
		}
	}
	eniAddr := &net.IPNet{
		IP:   net.ParseIP(eniIP),
		Mask: ipnet.Mask,
	}
	log.Debugf("Adding IP address %s", eniAddr.String())
	if err = netLink.AddrAdd(link, &netlink.Addr{IPNet: eniAddr}); err != nil {
		return errors.Wrap(err, "setupENINetwork: failed to add IP addr to ENI")
	}

	linkIndex := link.Attrs().Index
	log.Debugf("Setting up ENI's default gateway %v, table %d, linkIndex %d", gw, tableNumber, linkIndex)
	routes := []netlink.Route{
		// Add a direct link route for the host's ENI IP only
		{
			LinkIndex: linkIndex,
			Dst:       &net.IPNet{IP: gw, Mask: net.CIDRMask(32, 32)},
			Scope:     netlink.SCOPE_LINK,
			Table:     tableNumber,
		},
		// Route all other traffic via the host's ENI IP
		{
			LinkIndex: linkIndex,
			Dst:       &net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(0, 32)},
			Scope:     netlink.SCOPE_UNIVERSE,
			Gw:        gw,
			Table:     tableNumber,
		},
	}
	for _, r := range routes {
		err := netLink.RouteDel(&r)
		if err != nil && !netlinkwrapper.IsNotExistsError(err) {
			return errors.Wrap(err, "setupENINetwork: failed to clean up old routes")
		}

		err = retry.NWithBackoff(retry.NewSimpleBackoff(500*time.Millisecond, retryRouteAddInterval, 0.15, 2.0), maxRetryRouteAdd, func() error {
			if err := netLink.RouteReplace(&r); err != nil {
				log.Debugf("Not able to set route %s/0 via %s table %d", r.Dst.IP.String(), gw.String(), tableNumber)
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
	_, cidr, err := net.ParseCIDR(eniSubnetCIDR)
	if err != nil {
		return errors.Wrapf(err, "setupENINetwork: invalid IPv4 CIDR block %s", eniSubnetCIDR)
	}
	defaultRoute := netlink.Route{
		Dst:   cidr,
		Src:   net.ParseIP(eniIP),
		Table: mainRoutingTable,
		Scope: netlink.SCOPE_LINK,
	}

	if err := netLink.RouteDel(&defaultRoute); err != nil {
		if !netlinkwrapper.IsNotExistsError(err) {
			return errors.Wrapf(err, "setupENINetwork: unable to delete default route %s for source IP %s", cidr.String(), eniIP)
		}
	}
	return nil
}

// IncrementIPv4Addr returns incremented IPv4 address
func IncrementIPv4Addr(ip net.IP) (net.IP, error) {
	ip4 := ip.To4()
	if ip4 == nil {
		return nil, fmt.Errorf("%q is not a valid IPv4 Address", ip)
	}
	intIP := binary.BigEndian.Uint32(ip4)
	if intIP == (1<<32 - 1) {
		return nil, fmt.Errorf("%q will be overflowed", ip)
	}
	intIP++
	nextIPv4 := make(net.IP, 4)
	binary.BigEndian.PutUint32(nextIPv4, intIP)
	return nextIPv4, nil
}

// GetRuleList returns IP rules
func (n *linuxNetwork) GetRuleList() ([]netlink.Rule, error) {
	return n.netLink.RuleList(unix.AF_INET)
}

// GetRuleListBySrc returns IP rules with matching source IP
func (n *linuxNetwork) GetRuleListBySrc(ruleList []netlink.Rule, src net.IPNet) ([]netlink.Rule, error) {
	var srcRuleList []netlink.Rule
	for _, rule := range ruleList {
		if rule.Src != nil && rule.Src.IP.Equal(src.IP) {
			srcRuleList = append(srcRuleList, rule)
		}
	}
	return srcRuleList, nil
}

// DeleteRuleListBySrc deletes IP rules that have a matching source IP
func (n *linuxNetwork) DeleteRuleListBySrc(src net.IPNet) error {
	log.Infof("Delete Rule List By Src [%v]", src)

	ruleList, err := n.GetRuleList()
	if err != nil {
		log.Errorf("DeleteRuleListBySrc: failed to get rule list %v", err)
		return err
	}

	srcRuleList, err := n.GetRuleListBySrc(ruleList, src)
	if err != nil {
		log.Errorf("DeleteRuleListBySrc: failed to retrieve rule list %v", err)
		return err
	}

	log.Infof("Remove current list [%v]", srcRuleList)
	for _, rule := range srcRuleList {
		if err := n.netLink.RuleDel(&rule); err != nil && !containsNoSuchRule(err) {
			log.Errorf("Failed to cleanup old IP rule: %v", err)
			return errors.Wrapf(err, "DeleteRuleListBySrc: failed to delete old rule")
		}

		var toDst string
		if rule.Dst != nil {
			toDst = rule.Dst.String()
		}
		log.Debugf("DeleteRuleListBySrc: Successfully removed current rule [%v] to %s", rule, toDst)
	}
	return nil
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
	for _, rule := range srcRuleList {
		srcRuleTable = rule.Table
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
	podRule.Priority = fromPodRulePriority

	err = n.netLink.RuleAdd(podRule)
	if err != nil {
		log.Errorf("Failed to add pod IP rule: %v", err)
		return errors.Wrapf(err, "UpdateRuleListBySrc: failed to add pod rule")
	}
	log.Infof("UpdateRuleListBySrc: Successfully added pod rule[%v]", podRule)

	return nil
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
