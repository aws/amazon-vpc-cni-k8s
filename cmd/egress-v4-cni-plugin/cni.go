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
	"encoding/json"
	"fmt"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
	"net"
	"os"
	"runtime"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/cni/pkg/types/current"
	cniversion "github.com/containernetworking/cni/pkg/version"
	"github.com/containernetworking/plugins/pkg/ip"
	"github.com/containernetworking/plugins/pkg/ipam"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/containernetworking/plugins/pkg/utils"
	"github.com/vishvananda/netlink"
	"github.com/containernetworking/plugins/pkg/utils/sysctl"

	"github.com/aws/amazon-vpc-cni-k8s/cmd/egress-v4-cni-plugin/snat"
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

	//MTU for Egress v4 interface
	MTU int `json:"mtu"`

	Enabled string `json:"enabled"`

	// IP to use as SNAT target
	NodeIP net.IP `json:"nodeIP"`

	PluginLogFile  string `json:"pluginLogFile"`
	PluginLogLevel string `json:"pluginLogLevel"`
}

func loadConf(bytes []byte) (*NetConf, logger.Logger, error) {
	conf := &NetConf{IfName: "v4if0"}

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

			addrBits := 32
			if ipc.Version == "6" {
				addrBits = 128
			}

			for _, r := range []netlink.Route{
				{
					LinkIndex: contVeth.Index, //TODO - Should be default route
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

		//Disable IPv6 on this interface
		_, err = sysctl.Sysctl("net/ipv6/conf/"+ifName+"/disable_ipv6", "1")
		if err != nil {
			return fmt.Errorf("failed to disable IPv6 for interface: %v", err)
		}
		//Block traffic directed to 169.254.172.0/22 from the Pod
		err = snat.SetupRuleToBlockNodeLocalV4Access()
		if err != nil {
			return err
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

func main() {
	skel.PluginMain(cmdAdd, nil, cmdDel, cniversion.All, fmt.Sprintf("egress-v4 CNI plugin %s", version))
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
	//We will not be vending out this as a separate plugin by itself and it is only intended to be used as a
	//chained plugin to VPC CNI in IPv6 mode. We only need this plugin to kick in if v6 is enabled in VPC CNI. So, the
	//value of an env variable in VPC CNI determines whether this plugin should be enabled and this is an attempt to
	//pass through the variable configured in VPC CNI.
	if netConf.Enabled == "false" {
		return types.PrintResult(result, netConf.CNIVersion)
	}

	chain := utils.MustFormatChainNameWithPrefix(netConf.Name, args.ContainerID, "E4-")
	comment := utils.FormatComment(netConf.Name, args.ContainerID)

	ipamResultI, err := ipam.ExecAdd(netConf.IPAM.Type, args.StdinData)
	if err != nil {
		return fmt.Errorf("running IPAM plugin failed: %v", err)
	}

	// Invoke ipam del if err to avoid ip leak
	defer func() {
		if err != nil {
			ipam.ExecDel(netConf.IPAM.Type, args.StdinData)
		}
	}()

	tmpResult, err := current.NewResultFromResult(ipamResultI)
	if err != nil {
		return err
	}

	if len(tmpResult.IPs) == 0 {
		return fmt.Errorf("IPAM plugin returned zero IPs")
	}

	if err := ip.EnableForward(tmpResult.IPs); err != nil {
		return fmt.Errorf("could not enable IP forwarding: %v", err)
	}

	netns, err := ns.GetNS(args.Netns)
	if err != nil {
		return fmt.Errorf("failed to open netns %q: %v", args.Netns, err)
	}
	defer netns.Close()

	// NB: This uses netConf.IfName NOT args.IfName.
	hostInterface, _, err := setupContainerVeth(netns, netConf.IfName, netConf.MTU, tmpResult)
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
			if ipc.Version == "4" {
				//log.Printf("Configuring SNAT %s -> %s", ipc.Address.IP, netConf.SnatIP)
				if err := snat.Snat4(netConf.NodeIP, ipc.Address.IP, chain, comment); err != nil {
					return err
				}
			}
		}
	}

	//Copy interfaces over to result, but not IPs.
	result.Interfaces = append(result.Interfaces, tmpResult.Interfaces...)
	//Note: Useful for debug, will do away with the below log prior to release
	for _,v := range result.IPs {
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

	//We only need this plugin to kick in if v6 is enabled
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
			addrs, err := netlink.AddrList(iface, netlink.FAMILY_ALL)
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
		//CNI Spec: TODO
		if err != nil {
			log.Debugf("DEL: Executing in container ns errored out, returning", err)
			return err
		}
	}

	chain := utils.MustFormatChainNameWithPrefix(netConf.Name, args.ContainerID, "E4-")
	comment := utils.FormatComment(netConf.Name, args.ContainerID)

	if netConf.NodeIP != nil {
		log.Debugf("DEL: SNAT setup, let's clean them up. Size of ipnets: %d", len(ipnets))
		for _, ipn := range ipnets {
			if err := snat.Snat4Del(ipn.IP, chain, comment); err != nil {
				return err
			}
		}
	}

	return nil
}
