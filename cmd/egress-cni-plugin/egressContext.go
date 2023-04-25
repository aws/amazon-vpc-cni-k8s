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

	"github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/cni/pkg/types/current"
	"github.com/containernetworking/plugins/pkg/ip"
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
)

// EgressContext includes all info to run container ADD/DEL action
type EgressContext struct {
	Procsys       procsyswrapper.ProcSys
	Ipam          hostipamwrapper.HostIpam
	Link          netlinkwrapper.NetLink
	Ns            nswrapper.NS
	NsPath        string
	ArgsIfName    string
	Veth          vethwrapper.Veth
	IpTablesIface iptableswrapper.IPTablesIface
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
func NewEgressAddContext(nsPath, ifName string) EgressContext {
	return EgressContext{
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
func NewEgressDelContext(nsPath string) EgressContext {
	return EgressContext{
		Ipam:   hostipamwrapper.NewIpam(),
		Link:   netlinkwrapper.NewNetLink(),
		Ns:     nswrapper.NewNS(),
		NsPath: nsPath,
		IptCreator: func(protocol iptables.Protocol) (iptableswrapper.IPTablesIface, error) {
			return iptableswrapper.NewIPTables(protocol)
		},
	}
}

func (c *EgressContext) SetupContainerVeth() (*current.Interface, *current.Interface, error) {
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

	err := c.Ns.WithNetNSPath(c.NsPath, func(hostNS ns.NetNS) error {
		hostVeth, contVeth0, err := c.Veth.Setup(c.NetConf.IfName, c.Mtu, hostNS)
		if err != nil {
			return err
		}
		hostInterface.Name = hostVeth.Name
		hostInterface.Mac = hostVeth.HardwareAddr.String()
		containerInterface.Name = contVeth0.Name
		containerInterface.Mac = contVeth0.HardwareAddr.String()
		containerInterface.Sandbox = c.NsPath

		for _, ipc := range c.TmpResult.IPs {
			// All addresses apply to the container veth interface
			ipc.Interface = current.Int(1)
		}

		c.TmpResult.Interfaces = []*current.Interface{hostInterface, containerInterface}

		if err = c.Ipam.ConfigureIface(c.NetConf.IfName, c.TmpResult); err != nil {
			return err
		}

		contVeth, err := c.Link.LinkByName(c.NetConf.IfName)
		if err != nil {
			return fmt.Errorf("failed to look up %q: %v", c.NetConf.IfName, err)
		}

		for _, ipc := range c.TmpResult.IPs {
			// Delete the route that was automatically added
			route := netlink.Route{
				LinkIndex: contVeth.Attrs().Index,
				Dst: &net.IPNet{
					IP:   ipc.Address.IP.Mask(ipc.Address.Mask),
					Mask: ipc.Address.Mask,
				},
				Scope: netlink.SCOPE_NOWHERE,
			}

			if err := c.Link.RouteDel(&route); err != nil {
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
				if err := c.Link.RouteAdd(&r); err != nil {
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

func (c *EgressContext) SetupHostVeth(vethName string) error {
	// hostVeth moved namespaces and may have a new ifindex
	veth, err := c.Link.LinkByName(vethName)
	if err != nil {
		return fmt.Errorf("failed to lookup %q: %v", vethName, err)
	}

	for _, ipc := range c.TmpResult.IPs {
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
		if err = c.Link.AddrAdd(veth, addr); err != nil {
			return fmt.Errorf("failed to add IP addr (%#v) to veth: %v", ipn, err)
		}

		ipn = &net.IPNet{
			IP:   ipc.Address.IP,
			Mask: net.CIDRMask(maskLen, maskLen),
		}
		err := c.Link.RouteAdd(&netlink.Route{
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

// CmdAddEgressV4 exec necessary settings to support IPv4 egress traffic in EKS IPv6 cluster
func (c *EgressContext) CmdAddEgressV4() error {

	if err := cniutils.EnableIpForwarding(c.Procsys, c.TmpResult.IPs); err != nil {
		return fmt.Errorf("could not enable IP forwarding: %v", err)
	}

	// NB: This uses netConf.IfName NOT args.IfName.
	hostInterface, _, err := c.SetupContainerVeth()
	if err != nil {
		c.Log.Debugf("failed to setup container Veth: %v", err)
		return err
	}

	if err = c.SetupHostVeth(hostInterface.Name); err != nil {
		return err
	}

	c.Log.Debugf("Node IP: %s", c.NetConf.NodeIP)
	if c.NetConf.NodeIP != nil {
		for _, ipc := range c.TmpResult.IPs {
			if ipc.Address.IP.To4() != nil {
				// add SNAT chain/rules necessary for the container IPv6 egress traffic
				if err = snat.Add(c.IpTablesIface, c.NetConf.NodeIP, ipc.Address.IP, ipv4MulticastRange, c.SnatChain, c.SnatComment, c.NetConf.RandomizeSNAT); err != nil {
					return err
				}
			}
		}
	}

	// Copy interfaces over to result, but not IPs.
	c.Result.Interfaces = append(c.Result.Interfaces, c.TmpResult.Interfaces...)

	// Pass through the previous result
	return types.PrintResult(c.Result, c.NetConf.CNIVersion)
}

// CmdDelEgressV4 exec clear the setting to support IPv4 egress traffic in EKS IPv6 cluster
func (c *EgressContext) CmdDelEgressV4() error {
	var ipnets []*net.IPNet

	if c.NsPath != "" {
		err := c.Ns.WithNetNSPath(c.NsPath, func(hostNS ns.NetNS) error {
			var err error

			// DelLinkByNameAddr function deletes an interface and returns IPs assigned to it but it
			// excludes IPs that are not global unicast addresses (or) private IPs. Will not work for
			// our scenario as we use 169.254.0.0/16 range for v4 IPs.

			//Get the interface we want to delete
			iface, err := c.Link.LinkByName(c.NetConf.IfName)

			if err != nil {
				if _, ok := err.(netlink.LinkNotFoundError); ok {
					return nil
				}
				return nil
			}

			//Retrieve IP addresses assigned to the interface
			addrs, err := c.Link.AddrList(iface, netlink.FAMILY_V4)
			if err != nil {
				return fmt.Errorf("failed to get IP addresses for %q: %v", c.NetConf.IfName, err)
			}

			//Delete the interface/link.
			if err = c.Link.LinkDel(iface); err != nil {
				return fmt.Errorf("failed to delete %q: %v", c.NetConf.IfName, err)
			}

			for _, addr := range addrs {
				ipnets = append(ipnets, addr.IPNet)
			}

			if err != nil && err == ip.ErrLinkNotFound {
				c.Log.Debugf("DEL: Link Not Found, returning", err)
				return nil
			}
			return err
		})

		//DEL should be best-effort. We should clean up as much as we can and avoid returning error
		if err != nil {
			c.Log.Debugf("DEL: Executing in container ns errored out, returning", err)
		}
	}

	if c.NetConf.NodeIP != nil {
		c.Log.Debugf("DEL: SNAT setup, let's clean them up. Size of ipnets: %d", len(ipnets))
		for _, ipn := range ipnets {
			if err := snat.Del(c.IpTablesIface, ipn.IP, c.SnatChain, c.SnatComment); err != nil {
				return err
			}
		}
	}

	return nil
}
