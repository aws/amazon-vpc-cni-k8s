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
	"fmt"
	"net"
	"os"
	"time"

	"github.com/containernetworking/cni/pkg/types"
	current "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/coreos/go-iptables/iptables"
	"github.com/vishvananda/netlink"

	"github.com/aws/amazon-vpc-cni-k8s/cmd/egress-cni-plugin/snat"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/hostipamwrapper"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/iptableswrapper"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/netlinkwrapper"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/nswrapper"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/procsyswrapper"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/cniutils"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/vethwrapper"
)

const (
	ipv4MulticastRange = "224.0.0.0/4"
	ipv6MulticastRange = "ff00::/8"
	// WaitInterval Time duration CNI waits before next check for an IPv6 address assigned to an interface
	// to move to stable state.
	WaitInterval = 50 * time.Millisecond
	// DadTimeout Time duration CNI waits for an IPv6 address assigned to an interface
	// to move to stable state before error'ing out.
	DadTimeout = 10 * time.Second
)

// egressContext includes all info to run container ADD or DEL action
type egressContext struct {
	Procsys       procsyswrapper.ProcSys
	Ipam          hostipamwrapper.HostIpam
	Link          netlinkwrapper.NetLink
	Ns            nswrapper.NS
	NsPath        string
	ArgsIfName    string
	Veth          vethwrapper.Veth
	IPTablesIface iptableswrapper.IPTablesIface
	IptCreator    func(iptables.Protocol) (iptableswrapper.IPTablesIface, error)

	NetConf   *NetConf
	Result    *current.Result
	TmpResult *current.Result
	Log       logger.Logger

	Mtu int
	// SnatChain is the chain name for iptables rules
	SnatChain string
	// SnatComment is the comment for iptables rules
	SnatComment string
}

// NewEgressAddContext create a context for container egress traffic
func NewEgressAddContext(nsPath, ifName string) egressContext {
	return egressContext{
		Procsys:    procsyswrapper.NewProcSys(),
		Ipam:       hostipamwrapper.NewIpam(),
		Link:       netlinkwrapper.NewNetLink(),
		Ns:         nswrapper.NewNS(),
		NsPath:     nsPath,
		ArgsIfName: ifName,
		Veth:       vethwrapper.NewSetupVeth(),
		IptCreator: func(protocol iptables.Protocol) (iptableswrapper.IPTablesIface, error) {
			return iptableswrapper.NewIPTables(protocol)
		},
	}
}

// NewEgressDelContext create a context for container egress traffic
func NewEgressDelContext(nsPath string) egressContext {
	return egressContext{
		Ipam:   hostipamwrapper.NewIpam(),
		Link:   netlinkwrapper.NewNetLink(),
		Ns:     nswrapper.NewNS(),
		NsPath: nsPath,
		IptCreator: func(protocol iptables.Protocol) (iptableswrapper.IPTablesIface, error) {
			return iptableswrapper.NewIPTables(protocol)
		},
	}
}

func (ec *egressContext) setupContainerVethV4() (*current.Interface, *current.Interface, error) {
	// The IPAM result will be something like IP=192.168.3.5/24, GW=192.168.3.1.
	// What we want is really a point-to-point link but veth does not support IFF_POINTTOPOINT.
	// Next best thing would be to let it ARP but set interface to 192.168.3.5/32 and
	// add a route like "192.168.3.0/24 via 192.168.3.1 dev $ifName".
	// Unfortunately that won't work as the GW will be outside the interface's subnet.

	// Our solution is to configure the interface with 192.168.3.5/24, then delete the
	// "192.168.3.0/24 dev $ifName" route that was automatically added. Then we add
	// "192.168.3.1/32 dev $ifName" and "192.168.3.0/24 via 192.168.3.1 dev $ifName".
	// In other words we force all traffic to ARP via the gateway except for GW itself.

	hostInterface := &current.Interface{}
	containerInterface := &current.Interface{}

	err := ec.Ns.WithNetNSPath(ec.NsPath, func(hostNS ns.NetNS) error {
		// Empty veth MAC is passed
		hostVeth, contVeth0, err := ec.Veth.Setup(ec.NetConf.IfName, ec.Mtu, "", hostNS)
		if err != nil {
			return err
		}
		hostInterface.Name = hostVeth.Name
		hostInterface.Mac = hostVeth.HardwareAddr.String()
		containerInterface.Name = contVeth0.Name
		containerInterface.Mac = contVeth0.HardwareAddr.String()
		containerInterface.Sandbox = ec.NsPath

		for _, ipc := range ec.TmpResult.IPs {
			// All addresses apply to the container veth interface
			ipc.Interface = current.Int(1)
		}
		ec.TmpResult.Interfaces = []*current.Interface{hostInterface, containerInterface}

		if err = ec.Ipam.ConfigureIface(ec.NetConf.IfName, ec.TmpResult); err != nil {
			return err
		}

		contVeth, err := ec.Link.LinkByName(ec.NetConf.IfName)
		if err != nil {
			return fmt.Errorf("failed to look up %q: %v", ec.NetConf.IfName, err)
		}

		for _, ipc := range ec.TmpResult.IPs {
			// Delete the route that was automatically added
			route := netlink.Route{
				LinkIndex: contVeth.Attrs().Index,
				Dst: &net.IPNet{
					IP:   ipc.Address.IP.Mask(ipc.Address.Mask),
					Mask: ipc.Address.Mask,
				},
				Scope: netlink.SCOPE_NOWHERE,
			}

			if err := ec.Link.RouteDel(&route); err != nil {
				return fmt.Errorf("failed to delete route %v: %v", route, err)
			}

			addrBits := 128
			if ipc.Address.IP.To4() != nil {
				addrBits = 32
			}

			for _, r := range []netlink.Route{
				{
					LinkIndex: contVeth.Attrs().Index,
					Dst: &net.IPNet{
						IP:   ipc.Gateway,
						Mask: net.CIDRMask(addrBits, addrBits),
					},
					Scope: netlink.SCOPE_LINK,
					Src:   ipc.Address.IP,
				},
				{
					LinkIndex: contVeth.Attrs().Index,
					Dst: &net.IPNet{
						IP:   ipc.Address.IP.Mask(ipc.Address.Mask),
						Mask: ipc.Address.Mask,
					},
					Scope: netlink.SCOPE_UNIVERSE,
					Gw:    ipc.Gateway,
					Src:   ipc.Address.IP,
				},
			} {
				if err := ec.Link.RouteAdd(&r); err != nil {
					return fmt.Errorf("failed to add route %v: %v", r, err)
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return hostInterface, containerInterface, nil
}
func (ec *egressContext) setupHostVethV4(vethName string) error {
	// hostVeth moved namespaces and may have a new ifindex
	veth, err := ec.Link.LinkByName(vethName)
	if err != nil {
		return fmt.Errorf("failed to lookup %q: %v", vethName, err)
	}

	for _, ipc := range ec.TmpResult.IPs {
		maskLen := 128
		if ipc.Address.IP.To4() != nil {
			maskLen = 32
		}

		// NB: this is modified from standard ptp plugin.

		ipn := &net.IPNet{
			IP:   ipc.Gateway,
			Mask: net.CIDRMask(maskLen, maskLen),
		}
		addr := &netlink.Addr{
			IPNet: ipn,
			Scope: int(netlink.SCOPE_LINK), // <- ptp uses SCOPE_UNIVERSE here
		}
		if err = ec.Link.AddrAdd(veth, addr); err != nil {
			return fmt.Errorf("failed to add IP addr (%#v) to veth: %v", ipn, err)
		}

		ipn = &net.IPNet{
			IP:   ipc.Address.IP,
			Mask: net.CIDRMask(maskLen, maskLen),
		}
		err := ec.Link.RouteAdd(&netlink.Route{
			LinkIndex: veth.Attrs().Index,
			Scope:     netlink.SCOPE_LINK, // <- ptp uses SCOPE_HOST here
			Dst:       ipn,
		})
		if err != nil && !os.IsExist(err) {
			return fmt.Errorf("failed to add route on host: %v", err)
		}
	}

	return nil
}

// cmdAddEgressV4 exec necessary settings to support IPv4 egress traffic in EKS IPv6 cluster
func (ec *egressContext) cmdAddEgressV4() (err error) {
	if ec.IPTablesIface == nil {
		if ec.IPTablesIface, err = ec.IptCreator(iptables.ProtocolIPv4); err != nil {
			ec.Log.Error("command iptables not found")
			return err
		}
	}
	if err = cniutils.EnableIpForwarding(ec.Procsys, ec.TmpResult.IPs); err != nil {
		return fmt.Errorf("could not enable IP forwarding: %v", err)
	}

	// NB: This uses netConf.IfName NOT args.IfName.
	hostInterface, _, err := ec.setupContainerVethV4()
	if err != nil {
		ec.Log.Debugf("failed to setup container Veth: %v", err)
		return err
	}

	if err = ec.setupHostVethV4(hostInterface.Name); err != nil {
		return err
	}

	ec.Log.Debugf("Node IP: %s", ec.NetConf.NodeIP)
	if ec.NetConf.NodeIP != nil {
		for _, ipc := range ec.TmpResult.IPs {
			if ipc.Address.IP.To4() != nil {
				// add SNAT chain/rules necessary for the container IPv6 egress traffic
				if err = snat.Add(ec.IPTablesIface, ec.NetConf.NodeIP, ipc.Address.IP, ipv4MulticastRange, ec.SnatChain, ec.SnatComment, ec.NetConf.RandomizeSNAT); err != nil {
					return err
				}
			}
		}
	}

	// Copy interfaces over to result, but not IPs.
	ec.Result.Interfaces = append(ec.Result.Interfaces, ec.TmpResult.Interfaces...)

	// Pass through the previous result
	return types.PrintResult(ec.Result, ec.NetConf.CNIVersion)
}

// cmdDelEgressV4 exec clear the setting to support IPv4 egress traffic in EKS IPv6 cluster
func (ec *egressContext) cmdDelEgress(ipv4 bool) (err error) {
	var contIPAddrs []netlink.Addr

	protocol := iptables.ProtocolIPv4
	ipFamily := netlink.FAMILY_V4
	if !ipv4 {
		protocol = iptables.ProtocolIPv6
		ipFamily = netlink.FAMILY_V6
	}

	if ec.IPTablesIface == nil {
		if ec.IPTablesIface, err = ec.IptCreator(protocol); err != nil {
			ec.Log.Error("command iptables not found")
			// without iptables ir ip6tables, chain/rules could not be removed
			return err
		}
	}
	if ec.NsPath != "" {
		_ = ec.Ns.WithNetNSPath(ec.NsPath, func(hostNS ns.NetNS) error {
			// DelLinkByNameAddr function deletes a link and returns IPs assigned to it, but it
			// excludes IPs that are not global unicast addresses (or) private IPs. Will not work for
			// our scenario as we use 169.254.0.0/16 range for v4 IPs.

			var _err error
			var link netlink.Link
			link, _err = ec.Link.LinkByName(ec.NetConf.IfName)
			if _err != nil {
				if !cniutils.IsLinkNotFoundError(_err) {
					ec.Log.Errorf("failed to get container link by name %s: %v", ec.NetConf.IfName, _err)
				}
				return nil
			}

			//Retrieve IP addresses assigned to the link
			contIPAddrs, _err = ec.Link.AddrList(link, ipFamily)
			if _err != nil {
				ec.Log.Errorf("failed to get IP addresses for link %s: %v", ec.NetConf.IfName, _err)
			}

			return _err
		})
	}
	for _, ipAddr := range contIPAddrs {
		// for IPv4 egress, IP address is a link-local IPv4 address
		// for IPv6 egress, IP address is a unique-local IPv6 address
		// NOTE: IsGlobalUnicast returns true for unique-local IPv6 address
		if (ipv4 && ipAddr.IP.To4() != nil && ipAddr.IP.IsLinkLocalUnicast()) ||
			(!ipv4 && ipAddr.IP.To4() == nil && ipAddr.IP.IsGlobalUnicast()) {
			err = snat.Del(ec.IPTablesIface, ipAddr.IP, ec.SnatChain, ec.SnatComment)
			if err != nil {
				ec.Log.Errorf("failed to remove iptables chain %s: %v", ec.SnatChain, err)
			} else {
				ec.Log.Infof("successfully removed iptables chain %s", ec.SnatChain)
			}
		}
	}
	return nil
}

// cmdAddEgressV6 exec necessary settings to support IPv6 egress traffic in EKS IPv4 cluster
func (ec *egressContext) cmdAddEgressV6() (err error) {
	// Per best practice, a new veth pair is created between container ns and node ns
	// this newly created veth pair is used for container's egress IPv6 traffic
	// NOTE:
	// 1. link-local IPv6 addresses are automatically assigned to veth's both ends.
	// 2. unique-local IPv6 address allocated from host-local IPAM plugin is assigned to veth's container end only
	// 3. veth node end has no unique-local IPv6 address assigned, only link-local IPv6 address
	// 4. container IPv6 egress traffic go through node primary interface (eth0) which has an IPv6 global unicast address
	// 5. IPv6 egress traffic of all containers in a node shares node primary interface (eth0) through SNAT

	if ec.IPTablesIface == nil {
		if ec.IPTablesIface, err = ec.IptCreator(iptables.ProtocolIPv6); err != nil {
			ec.Log.Error("command ip6tables not found")
			return err
		}
	}
	// first disable IPv6 on container's primary interface (eth0)
	err = ec.disableContainerInterfaceIPv6(ec.ArgsIfName)
	if err != nil {
		ec.Log.Errorf("failed to disable IPv6 on container interface %s", ec.ArgsIfName)
		return err
	}

	hostInterface, containerInterface, err := ec.setupContainerVethV6()
	if err != nil {
		ec.Log.Errorf("veth created failed, ns: %s name: %s, mtu: %d, ipam-result: %+v err: %v",
			ec.NsPath, ec.NetConf.IfName, ec.Mtu, *ec.TmpResult, err)
		return err
	}
	ec.Log.Debugf("veth pair created for container IPv6 egress traffic, container interface: %s ,host interface: %s",
		containerInterface.Name, hostInterface.Name)

	containerIPv6 := ec.TmpResult.IPs[0].Address.IP

	err = ec.setupContainerIPv6Route(hostInterface, containerInterface)
	if err != nil {
		ec.Log.Errorf("setupContainerIPv6Route failed: %v", err)
		return err
	}
	ec.Log.Debugf("container route set up successfully")

	err = ec.setupHostIPv6Route(hostInterface, containerIPv6)
	if err != nil {
		ec.Log.Errorf("setupHostIPv6Route failed: %v", err)
		return err
	}
	ec.Log.Debugf("host IPv6 route set up successfully")

	// set up SNAT in host for container IPv6 egress traffic
	// following line adds an ip6tables entries to NAT for IPv6 traffic between container v6if0 and node primary ENI (eth0)
	err = snat.Add(ec.IPTablesIface, ec.NetConf.NodeIP, containerIPv6, ipv6MulticastRange, ec.SnatChain, ec.SnatComment, ec.NetConf.RandomizeSNAT)
	if err != nil {
		ec.Log.Errorf("setup host snat failed: %v", err)
		return err
	}
	ec.Log.Debugf("host IPv6 SNAT set up successfully")

	// Copy interfaces over to result, but not IPs.
	ec.Result.Interfaces = append(ec.Result.Interfaces, ec.TmpResult.Interfaces...)

	// Pass through the previous result
	return types.PrintResult(ec.Result, ec.NetConf.CNIVersion)
}

func (ec *egressContext) disableContainerInterfaceIPv6(ifName string) error {
	return ec.Ns.WithNetNSPath(ec.NsPath, func(hostNS ns.NetNS) error {
		var entry = "net/ipv6/conf/" + ifName + "/disable_ipv6"
		return ec.Procsys.Set(entry, "1")
	})
}

func (ec *egressContext) setupContainerIPv6Route(hostInterface, containerInterface *current.Interface) (err error) {
	var hostIfIPv6 net.IP
	var hostNetIf netlink.Link
	var addrs []netlink.Addr
	hostNetIf, err = ec.Link.LinkByName(hostInterface.Name)
	if err != nil {
		return err
	}
	addrs, err = ec.Link.AddrList(hostNetIf, netlink.FAMILY_V6)
	if err != nil {
		return err
	}

	for _, addr := range addrs {
		// search for interface's link-local IPv6 address
		if addr.IP.To4() == nil && addr.IP.IsLinkLocalUnicast() {
			hostIfIPv6 = addr.IP
			break
		}
	}
	if hostIfIPv6 == nil {
		return fmt.Errorf("link-local IPv6 address not found on host interface %s", hostInterface.Name)
	}

	return ec.Ns.WithNetNSPath(ec.NsPath, func(hostNS ns.NetNS) error {
		var containerVethIf netlink.Link
		containerVethIf, err = ec.Link.LinkByName(containerInterface.Name)
		if err != nil {
			return err
		}
		// set up from container off-cluster IPv6 route (egress)
		// all from container IPv6 traffic via host veth interface's link-local IPv6 address
		if err := ec.Link.RouteReplace(&netlink.Route{
			LinkIndex: containerVethIf.Attrs().Index,
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

// setupHostIPv6Route adds a IPv6 route for traffic destined to container/pod from external/off-cluster
func (ec *egressContext) setupHostIPv6Route(hostInterface *current.Interface, containerIPv6 net.IP) error {
	link := ec.Link
	hostIf, err := link.LinkByName(hostInterface.Name)
	if err != nil {
		return err
	}
	// set up to container return traffic route in host
	return link.RouteAdd(&netlink.Route{
		LinkIndex: hostIf.Attrs().Index,
		Scope:     netlink.SCOPE_HOST,
		Dst: &net.IPNet{
			IP:   containerIPv6,
			Mask: net.CIDRMask(128, 128),
		},
	})
}

func (ec *egressContext) setupContainerVethV6() (hostInterface, containerInterface *current.Interface, err error) {
	err = ec.Ns.WithNetNSPath(ec.NsPath, func(hostNS ns.NetNS) error {
		var hostVeth net.Interface
		var contVeth net.Interface

		// Empty veth MAC is passed
		hostVeth, contVeth, err = ec.Veth.Setup(ec.NetConf.IfName, ec.Mtu, "", hostNS)
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
			Sandbox: ec.NsPath,
		}
		ec.TmpResult.Interfaces = []*current.Interface{hostInterface, containerInterface}
		for _, ipc := range ec.TmpResult.IPs {
			// Address (IPv6 ULA address) apply to the container veth interface - v6if0
			ipc.Interface = current.Int(1)
		}

		err = ec.Ipam.ConfigureIface(ec.NetConf.IfName, ec.TmpResult)
		if err != nil {
			return err
		}

		return cniutils.WaitForAddressesToBeStable(ec.Link, contVeth.Name, DadTimeout, WaitInterval)
	})
	return hostInterface, containerInterface, err
}

func (ec *egressContext) hostLocalIpamAdd(stdinData []byte) (err error) {
	var ipamResultI types.Result
	if ipamResultI, err = ec.Ipam.ExecAdd(ec.NetConf.IPAM.Type, stdinData); err != nil {
		return fmt.Errorf("running IPAM plugin failed: %v", err)
	}

	if ec.TmpResult, err = current.NewResultFromResult(ipamResultI); err != nil {
		return err
	}

	ipCount := len(ec.TmpResult.IPs)
	if ipCount == 0 {
		return fmt.Errorf("IPAM plugin returned zero IPs")
	} else if ipCount > 1 {
		return fmt.Errorf("IPAM plugin is expected to return 1 IP address, but returned %d IPs, ", ipCount)
	}
	return nil
}
