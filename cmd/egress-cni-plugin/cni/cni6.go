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

package cni

import (
	"bytes"
	"fmt"
	"os"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/netlinkwrapper"
	"github.com/coreos/go-iptables/iptables"

	"net"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/cniutils"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"

	"github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/cni/pkg/types/current"
	"github.com/containernetworking/plugins/pkg/ip"
	"github.com/containernetworking/plugins/pkg/ipam"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/vishvananda/netlink"

	"github.com/aws/amazon-vpc-cni-k8s/cmd/egress-cni-plugin/netconf"
	"github.com/aws/amazon-vpc-cni-k8s/cmd/egress-cni-plugin/snat"
)

// Time duration CNI waits for an IPv6 address assigned to an interface
// to move to stable state before error'ing out.
const (
	WaitInterval = 50 * time.Millisecond
	DadTimeout   = 10 * time.Second
)

func setupHostIPv6(log logger.Logger) error {
	err := ip.EnableIP6Forward()
	if err != nil {
		log.Errorf("failed to enable host IPv6 forwarding: %v", err)
		return err
	}
	hostPrimaryInterfaceName, err := cniutils.GetHostPrimaryInterfaceName()
	if err != nil {
		log.Errorf("failed to get host primary interface name: %v", err)
		return err
	}
	err = cniutils.SetIPv6AcceptRa(hostPrimaryInterfaceName, "2")
	if err != nil {
		log.Errorf("failed to set host interface %s IPv6 accept_ra value %s", hostPrimaryInterfaceName, "2")
		return err
	}
	return err
}

func setupHostIPv6Route(hostInterface *current.Interface, containerIPv6 net.IP) error {

	hostIf, err := net.InterfaceByName(hostInterface.Name)
	if err != nil {
		return err
	}

	if err != nil {
		return err
	}
	// set up to container return traffic route in host
	return netlink.RouteAdd(&netlink.Route{
		LinkIndex: hostIf.Index,
		Scope:     netlink.SCOPE_HOST,
		Dst: &net.IPNet{
			IP:   containerIPv6,
			Mask: net.CIDRMask(128, 128),
		},
	})
}

func setupContainerVethIPv6(netns ns.NetNS, ifName string, mtu int, pr *current.Result) (hostInterface, containerInterface *current.Interface, err error) {
	netLink := netlinkwrapper.NewNetLink()
	err = netns.Do(func(hostNS ns.NetNS) error {
		hostVeth, contVeth, err := ip.SetupVeth(ifName, mtu, hostNS)
		if err != nil {
			return err
		}

		hostInterface = &current.Interface{
			Name: hostVeth.Name,
			Mac:  hostVeth.HardwareAddr.String(),
		}
		containerInterface = &current.Interface{
			Name:    contVeth.Name,
			Mac:     contVeth.HardwareAddr.String(),
			Sandbox: netns.Path(),
		}
		pr.Interfaces = []*current.Interface{hostInterface, containerInterface}
		for _, ipc := range pr.IPs {
			// Address (IPv6 ULA address) apply to the container veth interface
			ipc.Interface = current.Int(1)
		}

		err = ipam.ConfigureIface(ifName, pr)
		if err != nil {
			return err
		}

		return cniutils.WaitForAddressesToBeStable(netLink, contVeth.Name, DadTimeout, WaitInterval)
	})
	return hostInterface, containerInterface, err
}

func setupContainerIPv6Route(netns ns.NetNS, hostInterface, containerInterface *current.Interface) error {
	var hostIfIPv6 net.IP
	hostNetIf, err := net.InterfaceByName(hostInterface.Name)
	if err != nil {
		return err
	}

	addrs, err := hostNetIf.Addrs()
	if err != nil {
		return err
	}

	for _, addr := range addrs {
		ip := addr.(*net.IPNet).IP
		// search for interface's link-local IPv6 address
		if ip.To4() == nil && ip.IsLinkLocalUnicast() {
			hostIfIPv6 = ip
			break
		}
	}
	if hostIfIPv6 == nil {
		return fmt.Errorf("link-local IPv6 address not found on host interface %s", hostInterface.Name)
	}

	return netns.Do(func(hostNS ns.NetNS) error {
		containerVethIf, err := net.InterfaceByName(containerInterface.Name)
		if err != nil {
			return err
		}
		// set up from container off-cluster IPv6 route (egress)
		// all from container IPv6 traffic via host veth interface's link-local IPv6 address
		if err := netlink.RouteReplace(&netlink.Route{
			LinkIndex: containerVethIf.Index,
			Dst: &net.IPNet{
				IP:   net.IPv6zero,
				Mask: net.CIDRMask(0, 128),
			},
			Scope: netlink.SCOPE_UNIVERSE,
			Gw:    hostIfIPv6}); err != nil {
			return fmt.Errorf("failed to add default IPv6 route via %s: %v", hostIfIPv6, err)
		}
		return nil
	})
}

func mergeResult(result *current.Result, tmpResult *current.Result) {
	lastInterfaceIndex := len(result.Interfaces)
	result.Interfaces = append(result.Interfaces, tmpResult.Interfaces...)
	for _, ip := range tmpResult.IPs {
		ip.Interface = current.Int(lastInterfaceIndex + *ip.Interface)
		result.IPs = append(result.IPs, ip)
	}
}

func disableInterfaceIPv6(netns ns.NetNS, ifName string) error {
	err := netns.Do(func(hostNS ns.NetNS) error {
		var entry = "/proc/sys/net/ipv6/conf/" + ifName + "/disable_ipv6"

		if content, err := os.ReadFile(entry); err == nil {
			if bytes.Equal(bytes.TrimSpace(content), []byte("1")) {
				return nil
			}
		}
		return os.WriteFile(entry, []byte("1"), 0644)
	})
	return err
}

// CmdAddEgressV6 exec necessary settings to support IPv6 egress traffic in EKS IPv4 cluster
func CmdAddEgressV6(netns ns.NetNS, netConf *netconf.NetConf, result, tmpResult *current.Result, mtu int,
	argsIfName, chain, comment string, log logger.Logger) error {
	// per best practice, a new veth pair is created between container ns and node ns
	// this newly created veth pair is used for container's egress IPv6 traffic
	// NOTE:
	//	1. link-local IPv6 addresses are automatically assigned to veth both ends.
	//	2. unique-local IPv6 address allocated from IPAM plugin is assigned to veth container end only
	//  3. veth node end has no unique-local IPv6 address assigned, only link-local IPv6 address
	//  4. IPv6 traffic egress through node primary interface (eth0) which has a IPv6 global unicast address
	//  5. all containers IPv6 egress traffic share node primary interface through SNAT

	// first disable IPv6 on container's primary interface (eth0)
	err := disableInterfaceIPv6(netns, argsIfName)
	if err != nil {
		log.Errorf("failed to disable IPv6 on container interface %s", argsIfName)
		return err
	}

	hostInterface, containerInterface, err := setupContainerVethIPv6(netns, netConf.IfName, mtu, tmpResult)
	if err != nil {
		log.Errorf("veth created failed, ns: %s name: %s, mtu: %d, ipam-result: %+v err: %v",
			netns.Path(), netConf.IfName, mtu, *tmpResult, err)
		return err
	}
	log.Debugf("veth pair created for container IPv6 egress traffic, container interface: %s ,host interface: %s",
		containerInterface.Name, hostInterface.Name)

	containerIPv6, err := cniutils.GetIPsByInterfaceName(netns, containerInterface.Name, func(ip net.IP) bool {
		return ip.To4() == nil && ip.IsGlobalUnicast()
	})
	if err != nil {
		return err
	}
	if len(containerIPv6) > 1 {
		log.Warnf("more than one IPv6 global unicast address found, ifName: %s, IPs: %s", containerInterface.Name, containerIPv6)
	}
	err = setupContainerIPv6Route(netns, hostInterface, containerInterface)
	if err != nil {
		log.Errorf("setupContainerIPv6Route failed: %v", err)
		return err
	}
	log.Debugf("container route set up successfully")

	err = setupHostIPv6(log)
	if err != nil {
		log.Errorf("failed to setup host IPv6: %v", err)
		return err
	}
	log.Debugf("setup host IPv6 forwarding/accept_ra successfully")

	err = setupHostIPv6Route(hostInterface, containerIPv6[0])
	if err != nil {
		log.Errorf("setupHostIPv6Route failed: %v", err)
		return err
	}
	log.Debugf("host IPv6 route set up successfully")

	// set up SNAT in host for container IPv6 egress traffic
	err = snat.Add(iptables.ProtocolIPv6, netConf.NodeIP, containerIPv6[0], chain, comment, netConf.RandomizeSNAT)
	if err != nil {
		log.Errorf("setup host snat failed: %v", err)
		return err
	}

	log.Debugf("host IPv6 SNAT set up successfully")

	mergeResult(result, tmpResult)
	log.Debugf("output result: %+v", *result)

	// Pass through the previous result
	return types.PrintResult(result, netConf.CNIVersion)
}

// CmdDelEgressV6 exec clear the setting to support IPv6 egress traffic in EKS IPv4 cluster
func CmdDelEgressV6(netnsPath string, ifName string, chain, comment string, log logger.Logger) (err error) {
	var contIPNets []*net.IPNet

	if netnsPath != "" {
		err = ns.WithNetNSPath(netnsPath, func(hostNS ns.NetNS) error {
			contIPNets, err = ip.DelLinkByNameAddr(ifName)
			if err != nil {
				log.Debugf("failed to delete veth %s in container: %v", ifName, err)
			} else {
				log.Debugf("Successfully deleted veth %s in container", ifName)
			}
			return err
		})
	}

	// range loop exec 0 times if confIPNets is nil
	for _, contIPNet := range contIPNets {
		// remove host SNAT chain/rule for container
		err = snat.Del(iptables.ProtocolIPv6, contIPNet.IP, chain, comment)
		if err != nil {
			log.Errorf("Delete host SNAT for container IPv6 %s failed: %v.", contIPNet.IP, err)
		}
		log.Debugf("Successfully deleted SNAT chain/rule for container IPv6 egress traffic: %s", contIPNet.String())
	}

	return nil
}
