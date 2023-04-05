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
	"fmt"
	"github.com/aws/amazon-vpc-cni-k8s/cmd/egress-cni-plugin/netconf"
	"github.com/aws/amazon-vpc-cni-k8s/cmd/egress-cni-plugin/snat"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/cni/pkg/types/current"
	"github.com/containernetworking/plugins/pkg/ip"
	"github.com/containernetworking/plugins/pkg/ipam"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/vishvananda/netlink"
	"net"
	"os"
)

// The bulk of this file is mostly based on standard ptp CNI plugin.
//
// Note: There are other options, for example we could add a new
// address onto an existing container/host interface.
//
// Unfortunately kubelet's dockershim (at least) ignores the CNI
// result structure, and directly queries the addresses on the
// container's IfName - and then prefers any global v4 address found.
// We do _not_ want our v4 NAT address to become "the" pod IP!
//
// Also, standard `loopback` CNI plugin checks and aborts if it finds
// any global-scope addresses on `lo`, so we can't just do that
// either.
//
// So we have to create a new interface (not args.IfName) to hide our
// NAT address from all this logic (or patch dockershim, or (better)
// just stop using dockerd...).  Hence ptp.
//

func setupContainerVeth(netns ns.NetNS, ifName string, mtu int, pr *current.Result) (*current.Interface, *current.Interface, error) {
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

	err := netns.Do(func(hostNS ns.NetNS) error {
		hostVeth, contVeth0, err := ip.SetupVeth(ifName, mtu, hostNS)
		if err != nil {
			return err
		}
		hostInterface.Name = hostVeth.Name
		hostInterface.Mac = hostVeth.HardwareAddr.String()
		containerInterface.Name = contVeth0.Name
		containerInterface.Mac = contVeth0.HardwareAddr.String()
		containerInterface.Sandbox = netns.Path()

		for _, ipc := range pr.IPs {
			// All addresses apply to the container veth interface
			ipc.Interface = current.Int(1)
		}

		pr.Interfaces = []*current.Interface{hostInterface, containerInterface}

		if err = ipam.ConfigureIface(ifName, pr); err != nil {
			return err
		}

		contVeth, err := net.InterfaceByName(ifName)
		if err != nil {
			return fmt.Errorf("failed to look up %q: %v", ifName, err)
		}

		for _, ipc := range pr.IPs {
			// Delete the route that was automatically added
			route := netlink.Route{
				LinkIndex: contVeth.Index,
				Dst: &net.IPNet{
					IP:   ipc.Address.IP.Mask(ipc.Address.Mask),
					Mask: ipc.Address.Mask,
				},
				Scope: netlink.SCOPE_NOWHERE,
			}

			if err := netlink.RouteDel(&route); err != nil {
				return fmt.Errorf("failed to delete route %v: %v", route, err)
			}

			addrBits := 128
			if ipc.Address.IP.To4() != nil {
				addrBits = 32
			}

			for _, r := range []netlink.Route{
				{
					LinkIndex: contVeth.Index,
					Dst: &net.IPNet{
						IP:   ipc.Gateway,
						Mask: net.CIDRMask(addrBits, addrBits),
					},
					Scope: netlink.SCOPE_LINK,
					Src:   ipc.Address.IP,
				},
				{
					LinkIndex: contVeth.Index,
					Dst: &net.IPNet{
						IP:   ipc.Address.IP.Mask(ipc.Address.Mask),
						Mask: ipc.Address.Mask,
					},
					Scope: netlink.SCOPE_UNIVERSE,
					Gw:    ipc.Gateway,
					Src:   ipc.Address.IP,
				},
			} {
				if err := netlink.RouteAdd(&r); err != nil {
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

func setupHostVeth(vethName string, result *current.Result) error {
	// hostVeth moved namespaces and may have a new ifindex
	veth, err := netlink.LinkByName(vethName)
	if err != nil {
		return fmt.Errorf("failed to lookup %q: %v", vethName, err)
	}

	for _, ipc := range result.IPs {
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
		if err = netlink.AddrAdd(veth, addr); err != nil {
			return fmt.Errorf("failed to add IP addr (%#v) to veth: %v", ipn, err)
		}

		ipn = &net.IPNet{
			IP:   ipc.Address.IP,
			Mask: net.CIDRMask(maskLen, maskLen),
		}
		err := netlink.RouteAdd(&netlink.Route{
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

func CmdAddEgressV4(netns ns.NetNS, netConf *netconf.NetConf, result, tmpResult *current.Result, mtu int, chain, comment string, log logger.Logger) error {

	if err := ip.EnableForward(tmpResult.IPs); err != nil {
		return fmt.Errorf("could not enable IP forwarding: %v", err)
	}

	// NB: This uses netConf.IfName NOT args.IfName.
	hostInterface, _, err := setupContainerVeth(netns, netConf.IfName, mtu, tmpResult)
	if err != nil {
		log.Debugf("failed to setup container Veth: %v", err)
		return err
	}

	if err = setupHostVeth(hostInterface.Name, tmpResult); err != nil {
		return err
	}

	log.Debugf("Node IP: %s", netConf.NodeIP)
	if netConf.NodeIP != nil {
		for _, ipc := range tmpResult.IPs {
			if ipc.Address.IP.To4() != nil {
				//log.Printf("Configuring SNAT %s -> %s", ipc.Address.IP, netConf.SnatIP)
				if err := snat.Snat4(netConf.NodeIP, ipc.Address.IP, chain, comment, netConf.RandomizeSNAT); err != nil {
					return err
				}
			}
		}
	}

	// Copy interfaces over to result, but not IPs.
	result.Interfaces = append(result.Interfaces, tmpResult.Interfaces...)
	// Note: Useful for debug, will do away with the below log prior to release
	for _, v := range result.IPs {
		log.Debugf("Interface Name: %v; IP: %s", v.Interface, v.Address)
	}

	// Pass through the previous result
	return types.PrintResult(result, netConf.CNIVersion)
}

func CmdDelEgressIPv4(netnsPath, ifName string, nodeIP net.IP, chain, comment string, log logger.Logger) error {
	ipnets := []*net.IPNet{}

	if netnsPath != "" {
		err := ns.WithNetNSPath(netnsPath, func(hostNS ns.NetNS) error {
			var err error

			// DelLinkByNameAddr function deletes an interface and returns IPs assigned to it but it
			// excludes IPs that are not global unicast addresses (or) private IPs. Will not work for
			// our scenario as we use 169.254.0.0/16 range for v4 IPs.

			//Get the interface we want to delete
			iface, err := netlink.LinkByName(ifName)

			if err != nil {
				if _, ok := err.(netlink.LinkNotFoundError); ok {
					return nil
				}
				return nil
			}

			//Retrieve IP addresses assigned to the interface
			addrs, err := netlink.AddrList(iface, netlink.FAMILY_V4)
			if err != nil {
				return fmt.Errorf("failed to get IP addresses for %q: %v", ifName, err)
			}

			//Delete the interface/link.
			if err = netlink.LinkDel(iface); err != nil {
				return fmt.Errorf("failed to delete %q: %v", ifName, err)
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

	if nodeIP != nil {
		log.Debugf("DEL: SNAT setup, let's clean them up. Size of ipnets: %d", len(ipnets))
		for _, ipn := range ipnets {
			if err := snat.Snat4Del(ipn.IP, chain, comment); err != nil {
				return err
			}
		}
	}

	return nil
}
