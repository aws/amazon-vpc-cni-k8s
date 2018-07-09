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

package networkutils

import (
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/pkg/errors"

	log "github.com/cihub/seelog"

	"github.com/coreos/go-iptables/iptables"
	"github.com/vishvananda/netlink"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/netlinkwrapper"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/nswrapper"
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

	// This environment is used to specify whether an external NAT gateway will be used to provide SNAT of
	// secondary ENI IP addresses.  If set to "true", the SNAT iptables rule and off-VPC ip rule will not
	// be installed and will be removed if they are already installed.  Defaults to false.
	envExternalSNAT = "AWS_VPC_K8S_CNI_EXTERNALSNAT"

	// envNodePortSupport is the name of environment variable that configures whether we implement support for
	// NodePorts on the primary ENI.  This requires that we add additional iptables rules and loosen the kernel's
	// RPF check as described below.  Defaults to true.
	envNodePortSupport = "AWS_VPC_CNI_NODE_PORT_SUPPORT"

	// envConnmark is the name of the environment variable that overrides the default connection mark, used to
	// mark traffic coming from the primary ENI so that return traffic can be forced out of the same interface.
	// Without using a mark, NodePort DNAT and our source-based routing do not work together if the target pod
	// behind the node port is not on the main ENI.  In that case, the un-DNAT is done after the source-based
	// routing, resulting in the packet being sent out of the pod's ENI, when the NodePort traffic should be
	// sent over the main ENI.
	envConnmark = "AWS_VPC_K8S_CNI_CONNMARK"

	// defaultConnmark is the default value for the connmark described above.  Note: the mark space is a little crowded,
	// - kube-proxy uses 0x0000c000
	// - Calico uses 0xffff0000.
	defaultConnmark = 0x80
)

// NetworkAPIs defines the host level and the eni level network related operations
type NetworkAPIs interface {
	// SetupNodeNetwork performs node level network configuration
	SetupHostNetwork(vpcCIDR *net.IPNet, primaryAddr *net.IP) error
	// SetupENINetwork performs eni level network configuration
	SetupENINetwork(eniIP string, mac string, table int, subnetCIDR string) error
}

type linuxNetwork struct {
	useExternalSNAT        bool
	nodePortSupportEnabled bool
	connmark               uint32

	netLink     netlinkwrapper.NetLink
	ns          nswrapper.NS
	newIptables func() (iptablesIface, error)
	mainENIMark uint32
	openFile    func(name string, flag int, perm os.FileMode) (stringWriteCloser, error)
}

type iptablesIface interface {
	Exists(table, chain string, rulespec ...string) (bool, error)
	Append(table, chain string, rulespec ...string) error
	Delete(table, chain string, rulespec ...string) error
}

// New creates a linuxNetwork object
func New() NetworkAPIs {
	return &linuxNetwork{
		useExternalSNAT:        useExternalSNAT(),
		nodePortSupportEnabled: nodePortSupportEnabled(),
		mainENIMark:            getConnmark(),

		netLink: netlinkwrapper.NewNetLink(),
		ns:      nswrapper.NewNS(),
		newIptables: func() (iptablesIface, error) {
			ipt, err := iptables.New()
			return ipt, err
		},
		openFile: func(name string, flag int, perm os.FileMode) (stringWriteCloser, error) {
			return os.OpenFile(name, flag, perm)
		},
	}
}

type stringWriteCloser interface {
	io.Closer
	WriteString(s string) (int, error)
}

func isDuplicateRuleAdd(err error) bool {
	return strings.Contains(err.Error(), "File exists")
}

// SetupHostNetwork performs node level network configuration
// TODO : implement ip rule not to 10.0.0.0/16(vpc'subnet) table main priority  1024
func (n *linuxNetwork) SetupHostNetwork(vpcCIDR *net.IPNet, primaryAddr *net.IP) error {
	log.Info("Setting up host network")
	hostRule := n.netLink.NewRule()
	hostRule.Dst = vpcCIDR
	hostRule.Table = mainRoutingTable
	hostRule.Priority = hostRulePriority
	hostRule.Invert = true

	// If this is a restart, cleanup previous rule first
	err := n.netLink.RuleDel(hostRule)
	if err != nil && !containsNoSuchRule(err) {
		log.Errorf("Failed to cleanup old host IP rule: %v", err)
		return errors.Wrapf(err, "host network setup: failed to delete old host rule")
	}

	// Only include the rule if SNAT is not being handled by an external NAT gateway and needs to be
	// handled on-node.
	if !n.useExternalSNAT {
		err = n.netLink.RuleAdd(hostRule)
		if err != nil {
			log.Errorf("Failed to add host IP rule: %v", err)
			return errors.Wrapf(err, "host network setup: failed to add host rule")
		}
	}

	if n.nodePortSupportEnabled {
		// If node port support is enabled, configure the kernel's reverse path filter check on eth0 for "loose"
		// filtering.  This is required because
		// - NodePorts are exposed on eth0
		// - The kernel's RPF check happens after incoming packets to NodePorts are DNATted to the pod IP.
		// - For pods assigned to secondary ENIs, the routing table includes source-based routing.  When the kernel does
		//   the RPF check, it looks up the route using the pod IP as the source.
		// - Thus, it finds the source-based route that leaves via the secondary ENI.
		// - In "strict" mode, the RPF check fails because the return path uses a different interface to the incoming
		//   packet.  In "loose" mode, the check passes because some route was found.
		const eth0RPFilter = "/proc/sys/net/ipv4/conf/eth0/rp_filter"
		const rpFilterLoose = "2"
		err := n.setProcSys(eth0RPFilter, rpFilterLoose)
		if err != nil {
			return errors.Wrapf(err, "failed to configure eth0 RPF check")
		}
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

	ipt, err := n.newIptables()

	if err != nil {
		return errors.Wrap(err, "host network setup: failed to create iptables")
	}

	for _, rule := range []iptablesRule{
		{
			name:        "connmark for primary ENI",
			shouldExist: n.nodePortSupportEnabled,
			table:       "mangle",
			chain:       "PREROUTING",
			rule: []string{
				"-m", "comment", "--comment", "AWS, primary ENI",
				"-i", "eth0",
				"-m", "addrtype", "--dst-type", "LOCAL", "--limit-iface-in",
				"-j", "CONNMARK", "--set-mark", fmt.Sprintf("%#x/%#x", n.mainENIMark, n.mainENIMark),
			},
		},
		{
			name:        "connmark restore for primary ENI",
			shouldExist: n.nodePortSupportEnabled,
			table:       "mangle",
			chain:       "PREROUTING",
			rule: []string{
				"-m", "comment", "--comment", "AWS, primary ENI",
				"-i", "eni+", "-j", "CONNMARK", "--restore-mark", "--mask", fmt.Sprintf("%#x", n.mainENIMark),
			},
		},
		{
			name:        fmt.Sprintf("rule for primary address %s", primaryAddr),
			shouldExist: !n.useExternalSNAT,
			table:       "nat",
			chain:       "POSTROUTING",
			rule: []string{
				"!", "-d", vpcCIDR.String(),
				"-m", "comment", "--comment", "AWS, SNAT",
				"-m", "addrtype", "!", "--dst-type", "LOCAL",
				"-j", "SNAT", "--to-source", primaryAddr.String()},
		},
	} {
		exists, err := ipt.Exists(rule.table, rule.chain, rule.rule...)
		if err != nil {
			return errors.Wrapf(err, "host network setup: failed to check existence of %v", rule)
		}

		if !exists && rule.shouldExist {
			err = ipt.Append(rule.table, rule.chain, rule.rule...)
			if err != nil {
				return errors.Wrapf(err, "host network setup: failed to add %v", rule)
			}
		} else if exists && !rule.shouldExist {
			err = ipt.Delete(rule.table, rule.chain, rule.rule...)
			if err != nil {
				return errors.Wrapf(err, "host network setup: failed to delete %v", rule)
			}
		}
	}

	return nil
}

func (n *linuxNetwork) setProcSys(key, value string) error {
	f, err := n.openFile(key, os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(value)
	if err != nil {
		return err
	}
	return nil
}

type iptablesRule struct {
	name         string
	shouldExist  bool
	table, chain string
	rule         []string
}

func (r iptablesRule) String() string {
	return fmt.Sprintf("%s/%s rule %s", r.table, r.chain, r.name)
}

func containsNoSuchRule(err error) bool {
	if errno, ok := err.(syscall.Errno); ok {
		return errno == syscall.ENOENT
	}
	return false
}

// GetConfigForDebug returns the active values of the configuration env vars (for debugging purposes).
func GetConfigForDebug() map[string]interface{} {
	return map[string]interface{}{
		envExternalSNAT:    useExternalSNAT(),
		envNodePortSupport: nodePortSupportEnabled(),
		envConnmark:        getConnmark(),
	}
}

// useExternalSNAT returns whether SNAT of secondary ENI IPs should be handled with an external
// NAT gateway rather than on node.  Failure to parse the setting will result in a log and the
// setting will be disabled.
func useExternalSNAT() bool {
	return getBoolEnvVar(envExternalSNAT, false)
}

func nodePortSupportEnabled() bool {
	return getBoolEnvVar(envNodePortSupport, true)
}

func getBoolEnvVar(name string, defaultValue bool) bool {
	if strValue := os.Getenv(name); strValue != "" {
		parsedValue, err := strconv.ParseBool(strValue)
		if err != nil {
			log.Error("Failed to parse "+name+"; using default: "+fmt.Sprint(defaultValue), err.Error())
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
			log.Error("Failed to parse "+envConnmark+"; will use ", defaultConnmark, err.Error())
			return defaultConnmark
		}
		if mark > math.MaxUint32 || mark <= 0 {
			log.Error(""+envConnmark+" out of range; will use ", defaultConnmark)
			return defaultConnmark
		}
		return uint32(mark)
	}
	return defaultConnmark
}

// LinkByMac returns linux netlink based on interface MAC
func LinkByMac(mac string, netLink netlinkwrapper.NetLink) (netlink.Link, error) {
	links, err := netLink.LinkList()

	if err != nil {
		return nil, err
	}

	for _, link := range links {
		if mac == link.Attrs().HardwareAddr.String() {
			log.Debugf("Found the Link that uses mac address %s and its index is %d",
				mac, link.Attrs().Index)
			return link, nil
		}
	}

	return nil, errors.Errorf("no interface found which uses mac address %s ", mac)
}

// SetupENINetwork adds default route to route table (eni-<eni_table>)
func (n *linuxNetwork) SetupENINetwork(eniIP string, eniMAC string, eniTable int, eniSubnetCIDR string) error {
	return setupENINetwork(eniIP, eniMAC, eniTable, eniSubnetCIDR, n.netLink)
}

func setupENINetwork(eniIP string, eniMAC string, eniTable int, eniSubnetCIDR string, netLink netlinkwrapper.NetLink) error {

	if eniTable == 0 {
		log.Debugf("Skipping set up eni network for primary interface")
		return nil
	}

	log.Infof("Setting up network for an eni with ip address %s, mac address %s, cidr %s and route table %d",
		eniIP, eniMAC, eniSubnetCIDR, eniTable)
	link, err := LinkByMac(eniMAC, netLink)
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
		{
			LinkIndex: deviceNumber,
			Dst:       &net.IPNet{IP: gw.IP, Mask: net.CIDRMask(32, 32)},
			Scope:     netlink.SCOPE_LINK,
			Table:     eniTable,
		},
		// Route all other traffic via the host's ENI IP
		{
			LinkIndex: deviceNumber,
			Dst:       &net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(0, 32)},
			Scope:     netlink.SCOPE_UNIVERSE,
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
			if err := netlink.RouteReplace(&r); err != nil {
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
		Scope: netlink.SCOPE_LINK,
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
