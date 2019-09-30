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
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/pkg/errors"
	"golang.org/x/sys/unix"

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

	mainRoutingTable = unix.RT_TABLE_MAIN

	// This environment is used to specify whether an external NAT gateway will be used to provide SNAT of
	// secondary ENI IP addresses.  If set to "true", the SNAT iptables rule and off-VPC ip rule will not
	// be installed and will be removed if they are already installed.  Defaults to false.
	envExternalSNAT = "AWS_VPC_K8S_CNI_EXTERNALSNAT"

	// This environment is used to specify weather the SNAT rule added to iptables should randomize port
	// allocation for outgoing connections. If set to "hashrandom" the SNAT iptables rule will have the "--random" flag
	// added to it. Set it to "prng" if you want to use a pseudo random numbers, i.e. "--random-fully".
	// Defaults to hashrandom.
	envRandomizeSNAT = "AWS_VPC_K8S_CNI_RANDOMIZESNAT"

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

	// MTU of ENI - veth MTU defined in plugins/routed-eni/driver/driver.go
	ethernetMTU = 9001

	// number of retries to add a route
	maxRetryRouteAdd = 5

	retryRouteAddInterval = 5 * time.Second

	// number of attempts to find an ENI by MAC address after it is attached
	maxAttemptsLinkByMac = 5

	retryLinkByMacInterval = 3 * time.Second
)

// NetworkAPIs defines the host level and the eni level network related operations
type NetworkAPIs interface {
	// SetupNodeNetwork performs node level network configuration
	SetupHostNetwork(vpcCIDR *net.IPNet, vpcCIDRs []*string, primaryMAC string, primaryAddr *net.IP) error
	// SetupENINetwork performs eni level network configuration
	SetupENINetwork(eniIP string, mac string, table int, subnetCIDR string) error
	UseExternalSNAT() bool
	GetRuleList() ([]netlink.Rule, error)
	GetRuleListBySrc(ruleList []netlink.Rule, src net.IPNet) ([]netlink.Rule, error)
	UpdateRuleListBySrc(ruleList []netlink.Rule, src net.IPNet, toCIDRs []string, toFlag bool) error
	DeleteRuleListBySrc(src net.IPNet) error
}

type linuxNetwork struct {
	useExternalSNAT        bool
	typeOfSNAT             snatType
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
		useExternalSNAT:        useExternalSNAT(),
		typeOfSNAT:             typeOfSNAT(),
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
func (n *linuxNetwork) SetupHostNetwork(vpcCIDR *net.IPNet, vpcCIDRs []*string, primaryMAC string, primaryAddr *net.IP) error {
	log.Info("Setting up host network... ")

	hostRule := n.netLink.NewRule()
	hostRule.Dst = vpcCIDR
	hostRule.Table = mainRoutingTable
	hostRule.Priority = hostRulePriority
	hostRule.Invert = true

	// Cleanup previous rule first before CNI 1.3
	err := n.netLink.RuleDel(hostRule)
	if err != nil && !containsNoSuchRule(err) {
		log.Errorf("Failed to cleanup old host IP rule: %v", err)
		return errors.Wrapf(err, "host network setup: failed to delete old host rule")
	}

	primaryIntf := "eth0"
	if n.nodePortSupportEnabled {
		primaryIntf, err = findPrimaryInterfaceName(primaryMAC)
		if err != nil {
			return errors.Wrapf(err, "failed to SetupHostNetwork")
		}
		// If node port support is enabled, configure the kernel's reverse path filter check on eth0 for "loose"
		// filtering.  This is required because
		// - NodePorts are exposed on eth0
		// - The kernel's RPF check happens after incoming packets to NodePorts are DNATted to the pod IP.
		// - For pods assigned to secondary ENIs, the routing table includes source-based routing.  When the kernel does
		//   the RPF check, it looks up the route using the pod IP as the source.
		// - Thus, it finds the source-based route that leaves via the secondary ENI.
		// - In "strict" mode, the RPF check fails because the return path uses a different interface to the incoming
		//   packet.  In "loose" mode, the check passes because some route was found.
		primaryIntfRPFilter := "/proc/sys/net/ipv4/conf/" + primaryIntf + "/rp_filter"
		const rpFilterLoose = "2"

		log.Debugf("Setting RPF for primary interface: %s", primaryIntfRPFilter)
		err = n.setProcSys(primaryIntfRPFilter, rpFilterLoose)
		if err != nil {
			return errors.Wrapf(err, "failed to configure %s RPF check", primaryIntf)
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

	// build IPTABLES chain for SNAT of non-VPC outbound traffic
	var chains []string
	for i := 0; i <= len(vpcCIDRs); i++ {
		chain := fmt.Sprintf("AWS-SNAT-CHAIN-%d", i)
		log.Debugf("Setup Host Network: iptables -N %s -t nat", chain)
		if err = ipt.NewChain("nat", chain); err != nil && !containChainExistErr(err) {
			log.Errorf("ipt.NewChain error for chain [%s]: %v", chain, err)
			return errors.Wrapf(err, "host network setup: failed to add chain")
		}
		chains = append(chains, chain)
	}

	// build SNAT rules for outbound non-VPC traffic
	log.Debugf("Setup Host Network: iptables -A POSTROUTING -m comment --comment \"AWS SNAT CHAIN\" -j AWS-SNAT-CHAIN-0")

	var iptableRules []iptablesRule
	iptableRules = append(iptableRules, iptablesRule{
		name:        "first SNAT rules for non-VPC outbound traffic",
		shouldExist: !n.useExternalSNAT,
		table:       "nat",
		chain:       "POSTROUTING",
		rule: []string{
			"-m", "comment", "--comment", "AWS SNAT CHAN", "-j", "AWS-SNAT-CHAIN-0",
		}})

	for i, cidr := range vpcCIDRs {
		curChain := chains[i]
		nextChain := chains[i+1]
		curName := fmt.Sprintf("[%d] AWS-SNAT-CHAIN", i)

		log.Debugf("Setup Host Network: iptables -A %s ! -d %s -t nat -j %s", curChain, *cidr, nextChain)

		iptableRules = append(iptableRules, iptablesRule{
			name:        curName,
			shouldExist: !n.useExternalSNAT,
			table:       "nat",
			chain:       curChain,
			rule: []string{
				"!", "-d", *cidr, "-m", "comment", "--comment", "AWS SNAT CHAN", "-j", nextChain,
			}})
	}

	lastChain := chains[len(chains)-1]
	// Prepare the Desired Rule for SNAT Rule
	snatRule := []string{"-m", "comment", "--comment", "AWS, SNAT",
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

	iptableRules = append(iptableRules, iptablesRule{
		name:        "last SNAT rule for non-VPC outbound traffic",
		shouldExist: !n.useExternalSNAT,
		table:       "nat",
		chain:       lastChain,
		rule:        snatRule,
	})

	log.Debugf("iptableRules: %v", iptableRules)

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
			"-i", "eni+", "-j", "CONNMARK", "--restore-mark", "--mask", fmt.Sprintf("%#x", n.mainENIMark),
		},
	})

	// remove pre-1.3 AWS SNAT rules
	iptableRules = append(iptableRules, iptablesRule{
		name:        fmt.Sprintf("rule for primary address %s", primaryAddr),
		shouldExist: false,
		table:       "nat",
		chain:       "POSTROUTING",
		rule: []string{
			"!", "-d", vpcCIDR.String(),
			"-m", "comment", "--comment", "AWS, SNAT",
			"-m", "addrtype", "!", "--dst-type", "LOCAL",
			"-j", "SNAT", "--to-source", primaryAddr.String()}})

	for _, rule := range iptableRules {
		log.Debugf("execute iptable rule : %s", rule.name)

		exists, err := ipt.Exists(rule.table, rule.chain, rule.rule...)
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

func containChainExistErr(err error) bool {
	return strings.Contains(err.Error(), "Chain already exists")
}

func (n *linuxNetwork) setProcSys(key, value string) error {
	f, err := n.openFile(key, os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	_, err = f.WriteString(value)
	if err != nil {
		// If the write failed, just close
		_ = f.Close()
		return err
	}
	return f.Close()
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
		envRandomizeSNAT:   typeOfSNAT(),
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

func typeOfSNAT() snatType {
	defaultValue := randomHashSNAT
	defaultString := "hashrandom"
	strValue := os.Getenv(envRandomizeSNAT)
	switch strValue {
	case "":
		// empty means default
		return defaultValue
	case "prng":
		// prng means to use --random-fully
		// note: for old versions of iptables, this will fall back to --random
		return randomPRNGSNAT
	case "none":
		// none means to disable randomisation (no flag)
		return sequentialSNAT

	case defaultString:
		// hashrandom means to use --random
		return randomHashSNAT
	default:
		// if we get to this point, the environment variable has an invalid value
		log.Errorf("Failed to parse %s; using default: %s. Provided string was %q", envRandomizeSNAT, defaultString,
			strValue)
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
func LinkByMac(mac string, netLink netlinkwrapper.NetLink, retryInterval time.Duration) (netlink.Link, error) {
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

// SetupENINetwork adds default route to route table (eni-<eni_table>)
func (n *linuxNetwork) SetupENINetwork(eniIP string, eniMAC string, eniTable int, eniSubnetCIDR string) error {
	return setupENINetwork(eniIP, eniMAC, eniTable, eniSubnetCIDR, n.netLink, retryLinkByMacInterval)
}

func setupENINetwork(eniIP string, eniMAC string, eniTable int, eniSubnetCIDR string, netLink netlinkwrapper.NetLink, retryLinkByMacInterval time.Duration) error {
	if eniTable == 0 {
		log.Debugf("Skipping set up ENI network for primary interface")
		return nil
	}

	log.Infof("Setting up network for an ENI with IP address %s, MAC address %s, CIDR %s and route table %d",
		eniIP, eniMAC, eniSubnetCIDR, eniTable)
	link, err := LinkByMac(eniMAC, netLink, retryLinkByMacInterval)
	if err != nil {
		return errors.Wrapf(err, "setupENINetwork: failed to find the link which uses MAC address %s", eniMAC)
	}

	if err = netLink.LinkSetMTU(link, ethernetMTU); err != nil {
		return errors.Wrapf(err, "setupENINetwork: failed to set MTU for %s", eniIP)
	}

	if err = netLink.LinkSetUp(link); err != nil {
		return errors.Wrapf(err, "setupENINetwork: failed to bring up ENI %s", eniIP)
	}

	deviceNumber := link.Attrs().Index

	_, ipnet, err := net.ParseCIDR(eniSubnetCIDR)
	if err != nil {
		return errors.Wrapf(err, "setupENINetwork: invalid IPv4 CIDR block %s", eniSubnetCIDR)
	}

	gw, err := incrementIPv4Addr(ipnet.IP)
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

	log.Debugf("Setting up ENI's default gateway %v", gw)
	routes := []netlink.Route{
		// Add a direct link route for the host's ENI IP only
		{
			LinkIndex: deviceNumber,
			Dst:       &net.IPNet{IP: gw, Mask: net.CIDRMask(32, 32)},
			Scope:     netlink.SCOPE_LINK,
			Table:     eniTable,
		},
		// Route all other traffic via the host's ENI IP
		{
			LinkIndex: deviceNumber,
			Dst:       &net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(0, 32)},
			Scope:     netlink.SCOPE_UNIVERSE,
			Gw:        gw,
			Table:     eniTable,
		},
	}
	for _, r := range routes {
		err := netLink.RouteDel(&r)
		if err != nil && !netlinkwrapper.IsNotExistsError(err) {
			return errors.Wrap(err, "setupENINetwork: failed to clean up old routes")
		}

		// In case of route dependency, retry few times
		retry := 0
		for {
			if err := netLink.RouteAdd(&r); err != nil {
				if netlinkwrapper.IsNetworkUnreachableError(err) {
					retry++
					if retry > maxRetryRouteAdd {
						log.Errorf("Failed to add route %s/0 via %s table %d",
							r.Dst.IP.String(), gw.String(), eniTable)
						return errors.Wrapf(err, "setupENINetwork: failed to add route %s/0 via %s table %d",
							r.Dst.IP.String(), gw.String(), eniTable)
					}
					log.Debugf("Not able to add route route %s/0 via %s table %d (attempt %d/%d)",
						r.Dst.IP.String(), gw.String(), eniTable, retry, maxRetryRouteAdd)
					time.Sleep(retryRouteAddInterval)
				} else if netlinkwrapper.IsRouteExistsError(err) {
					if err := netLink.RouteReplace(&r); err != nil {
						return errors.Wrapf(err, "setupENINetwork: unable to replace route entry %s", r.Dst.IP.String())
					}
					log.Debugf("Successfully replaced route to be %s/0", r.Dst.IP.String())
					break
				} else {
					return errors.Wrapf(err, "setupENINetwork: unable to add route %s/0 via %s table %d",
						r.Dst.IP.String(), gw.String(), eniTable)
				}
			} else {
				log.Debugf("Successfully added route route %s/0 via %s table %d", r.Dst.IP.String(), gw.String(), eniTable)
				break
			}
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

// incrementIPv4Addr returns incremented IPv4 address
func incrementIPv4Addr(ip net.IP) (net.IP, error) {
	ip4 := ip.To4()
	if ip4 == nil {
		return nil, fmt.Errorf("%q is not a valid IPv4 Address", ip)
	}
	intIP := binary.BigEndian.Uint32([]byte(ip4))
	if intIP == (1<<32 - 1) {
		return nil, fmt.Errorf("%q will be overflowed", ip)
	}
	intIP++
	bytes := make([]byte, 4)
	binary.BigEndian.PutUint32(bytes, intIP)
	return net.IP(bytes), nil
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
func (n *linuxNetwork) UpdateRuleListBySrc(ruleList []netlink.Rule, src net.IPNet, toCIDRs []string, useExternalSNAT bool) error {
	log.Infof("Update Rule List[%v] for source[%v] with toCIDRs[%v], useExternalSNAT[%v]", ruleList, src, toCIDRs, useExternalSNAT)

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

	if useExternalSNAT {
		for _, cidr := range toCIDRs {
			podRule := n.netLink.NewRule()
			_, podRule.Dst, _ = net.ParseCIDR(cidr)
			podRule.Src = &src
			podRule.Table = srcRuleTable
			podRule.Priority = fromPodRulePriority

			err = n.netLink.RuleAdd(podRule)
			if err != nil {
				log.Errorf("Failed to add pod IP rule for external SNAT: %v", err)
				return errors.Wrapf(err, "UpdateRuleListBySrc: failed to add pod rule for CIDR %s", cidr)
			}
			var toDst string

			if podRule.Dst != nil {
				toDst = podRule.Dst.String()
			}
			log.Infof("UpdateRuleListBySrc: Successfully added pod rule[%v] to %s", podRule, toDst)
		}
	} else {
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
	}
	return nil
}
