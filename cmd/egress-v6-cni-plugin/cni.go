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

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/aws/amazon-vpc-cni-k8s/utils/imds"
	"net"
	"os"
	"runtime"
	"syscall"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/netlinkwrapper"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/cni/pkg/types/current"
	cniversion "github.com/containernetworking/cni/pkg/version"
	"github.com/containernetworking/plugins/pkg/ip"
	"github.com/containernetworking/plugins/pkg/ipam"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/containernetworking/plugins/pkg/utils"
	"github.com/vishvananda/netlink"

	"github.com/aws/amazon-vpc-cni-k8s/cmd/egress-v6-cni-plugin/snat"
)

//Time duration CNI waits for an IPv6 address assigned to an interface
//to move to stable state before error'ing out.
const (
	WAIT_INTERVAL = 50 * time.Millisecond
	v6DADTimeout = 10 * time.Second
)

var version string

func init() {
	// this ensures that main runs only on main thread (thread group leader).
	// since namespace ops (unshare, setns) are done for a single thread, we
	// must ensure that the goroutine does not jump from OS thread to thread
	runtime.LockOSThread()
}

// NetConf is our CNI config structure
type NetConf struct {
	types.NetConf

	// Interface inside container to create
	IfName string `json:"ifName"`

	// MTU for Egress v4 interface
	MTU string `json:"mtu"`

	Enabled string `json:"enabled"`

	RandomizeSNAT string `json:"randomizeSNAT"`

	// IP to use as SNAT target
	NodeIP net.IP `json:"nodeIP"`

	PluginLogFile  string `json:"pluginLogFile"`
	PluginLogLevel string `json:"pluginLogLevel"`
}

func loadConf(bytes []byte) (*NetConf, logger.Logger, error) {
	conf := &NetConf{}

	if err := json.Unmarshal(bytes, conf); err != nil {
		return nil, nil, err
	}

	if conf.RawPrevResult != nil {
		if err := cniversion.ParsePrevResult(&conf.NetConf); err != nil {
			return nil, nil, fmt.Errorf("could not parse prevResult: %v", err)
		}
	}

	logConfig := logger.Configuration{
		LogLevel:    conf.PluginLogLevel,
		LogLocation: conf.PluginLogFile,
	}
	log := logger.New(&logConfig)
	return conf, log, nil
}

func getHostVethIfIndex(netns ns.NetNS, containerVethIfName string) (*int, error) {
	// find host veth interface index by using container veth interface name
	// code has to be run in container network space
	var hostVethIfIndex *int = nil
	err := netns.Do(func(hostNS ns.NetNS) error {
		_, peerIfIndex, err := ip.GetVethPeerIfindex(containerVethIfName);
		if err != nil {
			return err
		}
		hostVethIfIndex = &peerIfIndex
		return nil
	})
	if err != nil {
		return nil, err
	}

	return hostVethIfIndex, nil
}

func getContainerVethIfIndex(netns ns.NetNS, containerVethIfName string) (*int, error) {
	var containerVethIfIndex *int = nil
	err := netns.Do(func(hostNS ns.NetNS) error {
		containerVethIf, err := net.InterfaceByName(containerVethIfName)
		if err == nil {
			containerVethIfIndex = &containerVethIf.Index
		}
		return err
	})
	return containerVethIfIndex, err
}

func getHostVethIPv6ByContainerVethIfName(netns ns.NetNS, containerVethIfName string) ([]net.IP, error) {

	hostVethIfIndex, err := getHostVethIfIndex(netns, containerVethIfName)
	if err != nil {
		return nil, err
	}

	var netIPs []net.IP
	netIf, err := net.InterfaceByIndex(*hostVethIfIndex)
	if err != nil {
		return nil, err
	}

	addrs, err := netIf.Addrs()
	if err != nil {
		return nil, err
	}

	for _, addr := range addrs {
		ip :=  addr.(*net.IPNet).IP
		// search for interface's link-local IPv6 address
		if ip.To4() == nil && ip.IsLinkLocalUnicast() {
			netIPs = append(netIPs, ip)
		}
	}
	return netIPs, nil
}

func setupContainerIPv6Address(ipamType string, netns ns.NetNS, ifName string, argsStdinData []byte) (*net.IPNet, *current.Result, error) {
	// a ULA IPv6 address needs to be assigned to container interface, usually eth0
	// so that IPv4 container to communicate off-cluster IPv6 service

	var ipNet *net.IPNet = nil
	ipamResult, err := ipam.ExecAdd(ipamType, argsStdinData)
	if err != nil {
		return nil, nil, fmt.Errorf("running IPAM plugin failed: %v", err)
	}

	// Invoke IPAM del if err to avoid ip leak
	defer func() {
		if err != nil {
			ipam.ExecDel(ipamType, argsStdinData)
		}
	}()

	tmpResult, err := current.NewResultFromResult(ipamResult)
	if err != nil {
		return nil, nil, err
	}



	err = netns.Do(func(hostNS ns.NetNS) error {
		containerVethIf, err := net.InterfaceByName(ifName)
		if err != nil {
			return err
		}

		for _, ipConfig := range tmpResult.IPs {
			// for IPv6 ULA (Unique Local Address), IsGlobalUnicast return true
			if ipConfig.Address.IP.IsGlobalUnicast() && ipConfig.Version == "6" {
				ipNet = &ipConfig.Address
				ipConfig.Interface = &containerVethIf.Index
				break
			}
		}

		if ipNet == nil {
			return fmt.Errorf("no local IPv6 return from IPAM plugin")
		}

		netLink := netlinkwrapper.NewNetLink()
		link, err := netLink.LinkByName(ifName)
		if err != nil {
			return fmt.Errorf("failed to retrieve container link %s: %v", ifName, err)
		}


		err = netLink.AddrAdd(link, &netlink.Addr{
			IPNet: ipNet,
		})
		if err != nil {
			return err
		}

		deadline := time.Now().Add(v6DADTimeout)
		for {
			addrs, err := netLink.AddrList(link, netlink.FAMILY_V6)
			if err != nil {
				return fmt.Errorf("could not list container link %s IPv6 addresses: %v", ifName, err)
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
					ifName, v6DADTimeout)
			}

			time.Sleep(WAIT_INTERVAL)
		}
	})

	return ipNet, tmpResult, err
}

func setupContainerIPv6Route(netns ns.NetNS, containerVethIfIPv6 *net.IPNet, containerVethIfName string) error {
	containerVethIfIndex, err := getContainerVethIfIndex(netns, containerVethIfName)
	if err != nil {
		return err
	}

	netIPs, err := getHostVethIPv6ByContainerVethIfName(netns, containerVethIfName)
	if err != nil {
		return err
	}

	if len(netIPs) != 1 {
		return fmt.Errorf("0 or more than 1 link local IPv6 addresses found in host veth interface")
	}

	return netns.Do(func(hostNS ns.NetNS) error {
		for _, r := range []netlink.Route{
			{
				LinkIndex: *containerVethIfIndex,
				Dst: &net.IPNet{
					IP:   net.IPv6zero,
					Mask: net.CIDRMask(0, 128),
				},
				Scope: netlink.SCOPE_UNIVERSE,
				Via: &netlink.Via{
					AddrFamily: netlink.FAMILY_V6,
					Addr:       netIPs[0],
				},
			},
			{
				LinkIndex: *containerVethIfIndex,
				Dst: &net.IPNet{
					IP:   containerVethIfIPv6.IP.Mask(containerVethIfIPv6.Mask),
					Mask: containerVethIfIPv6.Mask,
				},
				Scope: netlink.SCOPE_UNIVERSE,
			},
		} {
			// set up from container off-cluster IPv6 route (egress)
			// all from container IPv6 traffic via host veth interface's link-local IPv6 address
			if err := netlink.RouteAdd(&r); err != nil {
				return fmt.Errorf("failed to add route %v: %v", r, err)
			}
		}
		return nil
	})
}

func enableHostIPv6Forwarding() error {
	var hostPrimaryIf string
	err := ip.EnableIP6Forward()
	if err != nil {
		return err
	}

	// figure out host primary interface and set accept_ra = 2

	primaryMAC, err := imds.GetMetaData("mac")
	if err != nil {
		return err
	}

	links, err := netlink.LinkList()
	if err != nil {
		return err
	}

	for _, link := range links {
		if link.Attrs().HardwareAddr.String() == primaryMAC {
			hostPrimaryIf = link.Attrs().Name
			break
		}
	}

	if len(hostPrimaryIf) > 0 {
		var entry = "/proc/sys/net/ipv6/conf/" + hostPrimaryIf + "/accept_ra"

		if content, err := os.ReadFile(entry); err == nil {
			if bytes.Equal(bytes.TrimSpace(content), []byte("2")) {
				return nil
			}
		}
		return os.WriteFile(entry, []byte("2"), 0644)
	} else {
		return fmt.Errorf("failed to get host primary interface name with mac: %s", primaryMAC)
	}
}

func setupHostIPv6Route(netns ns.NetNS, containerVethIfName string, containerIPv6 *net.IPNet) error {

	hostVethIfIndex, err := getHostVethIfIndex(netns, containerVethIfName)
	if err != nil {
		return err
	}
	// set up to container traffic route
	return netlink.RouteAdd(&netlink.Route{
		LinkIndex: *hostVethIfIndex,
		Scope:     netlink.SCOPE_HOST,
		Dst:       containerIPv6,
	})
}

func setupHostIPv6Snat(containerIPv6 net.IP, hostPrimaryIfIPv6 net.IP, chain, comment, randomizeSNAT string) error {
	return  snat.Snat6(hostPrimaryIfIPv6, containerIPv6, chain, comment, randomizeSNAT)
}

func main() {
	skel.PluginMain(cmdAdd, nil, cmdDel, cniversion.All, fmt.Sprintf("egress-v6 CNI plugin %s", version))
}

func cmdAdd(args *skel.CmdArgs) error {
	netConf, log, err := loadConf(args.StdinData)
	if err != nil {
		log.Debugf("Received Add request: Failed to parse config")
		return fmt.Errorf("failed to parse config: %v", err)
	}

	if netConf.PrevResult == nil {
		return fmt.Errorf("must be called as a chained plugin")
	}

	result, err := current.GetResult(netConf.PrevResult)
	if err != nil {
		return err
	}

	log.Debugf("Received an ADD request for: conf=%v; Plugin enabled=%s", netConf, netConf.Enabled)
	log.Debugf("PrevResult: %v", result)
	// We will not be vending out this as a separate plugin by itself, and it is only intended to be used as a
	// chained plugin to VPC CNI in IPv4 mode.
	// We only need this plugin to kick in if v6 is NOT enabled in VPC CNI. So, the
	// value of an env variable in VPC CNI determines whether this plugin should be enabled and this is an attempt to
	// pass through the variable configured in VPC CNI.
	if netConf.Enabled == "false" {
		return types.PrintResult(result, netConf.CNIVersion)
	}

	hostPrimaryIfIPv6AddrStr, err := imds.GetMetaData("ipv6")
	if err != nil {
		log.Debugf("IPv6 address not found on host primary interface which is needed for IPv6 egress: %v", err)
		return err
	}

	hostPrimaryIfIPv6 := net.ParseIP(hostPrimaryIfIPv6AddrStr)
	if hostPrimaryIfIPv6 == nil || !hostPrimaryIfIPv6.IsGlobalUnicast() {
		return fmt.Errorf("gobal unicast IPv6 address is needed on host primary interface for IPv6 egress")
	}
	log.Debugf("IPv6 address retrieved: %s", hostPrimaryIfIPv6AddrStr)

	chain := utils.MustFormatChainNameWithPrefix(netConf.Name, args.ContainerID, "E6-")
	comment := utils.FormatComment(netConf.Name, args.ContainerID)

	netns, err := ns.GetNS(args.Netns)
	if err != nil {
		log.Debugf("failed to open netns %q: %v", args.Netns, err)
		return fmt.Errorf("failed to open netns %q: %v", args.Netns, err)
	}
	defer netns.Close()

	// assign a ULA (Unique Local Address) ipv6 address in POD
	containerIPNetV6, ipamResult, err := setupContainerIPv6Address(netConf.IPAM.Type, netns, args.IfName, args.StdinData)
	if err != nil {
		log.Debugf("setupContainerIPv6Address failed: %v", err)
		return err
	}

	log.Debugf("container IPv6 assigned: %s", containerIPNetV6.String())

	err = setupContainerIPv6Route(netns, containerIPNetV6, args.IfName)
	if err != nil {
		log.Debugf("setupContainerIPv6Route failed: %v", err)
		return err
	}
	log.Debugf("container route set up successfully")

	err = enableHostIPv6Forwarding()
	if err != nil {
		log.Debugf("enableHostIPv6Forwarding failed: %v", err)
		return err
	}
	log.Debugf("enable host IPv6 forwarding successfully")

	err = setupHostIPv6Route(netns, args.IfName, containerIPNetV6)
	if err != nil {
		log.Debugf("setupHostIPv6Route failed: %v", err)
		return err
	}
	log.Debugf("host IPv6 route set up successfully")

	err = setupHostIPv6Snat(containerIPNetV6.IP, hostPrimaryIfIPv6, chain, comment, netConf.RandomizeSNAT)
	if err != nil {
		log.Debugf("setupHostIPv6Snat failed: %v", err)
		return err
	}

	log.Debugf("host IPv6 SNAT set up successfully")

	// Copy IPs over to result
	result.IPs = append(result.IPs, ipamResult.IPs...)
	// Note: Useful for debug, will do away with the below log prior to release
	for _, v := range result.IPs {
		log.Debugf("Interface Name: %v; IP: %s", v.Interface, v.Address)
	}

	// Pass through the previous result
	return types.PrintResult(result, netConf.CNIVersion)
}

func cmdDel(args *skel.CmdArgs) error {
	netConf, log, err := loadConf(args.StdinData)
	if err != nil {
		return fmt.Errorf("failed to parse config: %v", err)
	}

	// We only need this plugin to kick in if v6 is enabled
	if netConf.Enabled == "false" {
		return nil
	}

	log.Debugf("Received Del Request: conf=%v", netConf)
	if err := ipam.ExecDel(netConf.IPAM.Type, args.StdinData); err != nil {
		log.Debugf("running IPAM plugin failed: %v", err)
		return fmt.Errorf("running IPAM plugin failed: %v", err)
	}

	ipnets := []*net.IPNet{}
	if args.Netns != "" {
		err := ns.WithNetNSPath(args.Netns, func(hostNS ns.NetNS) error {
			var err error

			// DelLinkByNameAddr function deletes an interface and returns IPs assigned to it but it
			// excludes IPs that are not global unicast addresses (or) private IPs. Will not work for
			// our scenario as we use 169.254.0.0/16 range for v4 IPs.

			//Get the interface we want to delete
			iface, err := netlink.LinkByName(netConf.IfName)

			if err != nil {
				if _, ok := err.(netlink.LinkNotFoundError); ok {
					return nil
				}
				return nil
			}

			//Retrieve IP addresses assigned to the interface
			addrs, err := netlink.AddrList(iface, netlink.FAMILY_V4)
			if err != nil {
				return fmt.Errorf("failed to get IP addresses for %q: %v", netConf.IfName, err)
			}

			//Delete the interface/link.
			if err = netlink.LinkDel(iface); err != nil {
				return fmt.Errorf("failed to delete %q: %v", netConf.IfName, err)
			}

			for _, addr := range addrs {
				ipnets = append(ipnets, addr.IPNet)
			}

			if err != nil && err == ip.ErrLinkNotFound {
				log.Debugf("DEL: Link Not Found, returning", err)
				return nil
			}
			return err
		})

		//DEL should be best effort. We should clean up as much as we can and avoid returning error
		if err != nil {
			log.Debugf("DEL: Executing in container ns errored out, returning", err)
		}
	}

	chain := utils.MustFormatChainNameWithPrefix(netConf.Name, args.ContainerID, "E4-")
	comment := utils.FormatComment(netConf.Name, args.ContainerID)

	if netConf.NodeIP != nil {
		log.Debugf("DEL: SNAT setup, let's clean them up. Size of ipnets: %d", len(ipnets))
		for _, ipn := range ipnets {
			if err := snat.Snat6Del(ipn.IP, chain, comment); err != nil {
				return err
			}
		}
	}

	return nil
}
