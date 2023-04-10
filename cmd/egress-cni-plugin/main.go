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
	"runtime"
	"strconv"

	"github.com/aws/amazon-vpc-cni-k8s/cmd/egress-cni-plugin/cni"
	"github.com/aws/amazon-vpc-cni-k8s/cmd/egress-cni-plugin/netconf"
	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/cni/pkg/types/current"
	cniversion "github.com/containernetworking/cni/pkg/version"
	"github.com/containernetworking/plugins/pkg/ipam"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/containernetworking/plugins/pkg/utils"
)

var version string

func init() {
	// this ensures that main runs only on main thread (thread group leader).
	// since namespace ops (unshare, setns) are done for a single thread, we
	// must ensure that the goroutine does not jump from OS thread to thread
	runtime.LockOSThread()
}

func main() {
	skel.PluginMain(cmdAdd, nil, cmdDel, cniversion.All, fmt.Sprintf("egress CNI plugin %s", version))
}

func cmdAdd(args *skel.CmdArgs) error {
	netConf, log, err := netconf.LoadConf(args.StdinData)
	if err != nil {
		log.Debugf("Received Add request: Failed to parse config: %v", err)
		return fmt.Errorf("failed to parse config: %v", err)
	}

	if netConf.PrevResult == nil {
		log.Debugf("must be called as a chained plugin")
		return fmt.Errorf("must be called as a chained plugin")
	}

	result, err := current.GetResult(netConf.PrevResult)
	if err != nil {
		log.Errorf("failed to get PrevResult: %v", err)
		return err
	}

	log.Debugf("Received an ADD request for: conf=%v; Plugin enabled=%s", netConf, netConf.Enabled)
	// We will not be vending out this as a separate plugin by itself and it is only intended to be used as a
	// chained plugin to VPC CNI. We only need this plugin to kick in if egress is enabled in VPC CNI. So, the
	// value of an env variable in VPC CNI determines whether this plugin should be enabled and this is an attempt to
	// pass through the variable configured in VPC CNI.
	if netConf.Enabled == "false" {
		return types.PrintResult(result, netConf.CNIVersion)
	}

	isIPv6Egress := netConf.NodeIP.To4() == nil
	var chainPrefix string
	if isIPv6Egress {
		if netConf.NodeIP == nil || !netConf.NodeIP.IsGlobalUnicast() {
			return fmt.Errorf("global unicast IPv6 not found in host primary interface which is mandatory to support IPv6 egress")
		}
		chainPrefix = "E6-"
	} else {
		chainPrefix = "E4-"
	}

	chain := utils.MustFormatChainNameWithPrefix(netConf.Name, args.ContainerID, chainPrefix)
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

	// Convert MTU from string to int
	mtu, err := strconv.Atoi(netConf.MTU)
	if err != nil {
		log.Debugf("failed to parse MTU: %s, err: %v", netConf.MTU, err)
		return err
	}

	netns, err := ns.GetNS(args.Netns)
	if err != nil {
		return fmt.Errorf("failed to open netns %q: %v", args.Netns, err)
	}
	defer netns.Close()

	if isIPv6Egress {
		netConf.IfName = netconf.EgressIPv6InterfaceName
		return cni.CmdAddEgressV6(netns, netConf, result, tmpResult, mtu, args.IfName, chain, comment, log)
	}
	netConf.IfName = netconf.EgressIPv4InterfaceName
	return cni.CmdAddEgressV4(netns, netConf, result, tmpResult, mtu, chain, comment, log)
}

func cmdDel(args *skel.CmdArgs) error {
	netConf, log, err := netconf.LoadConf(args.StdinData)
	if err != nil {
		log.Debugf("Received Del request: Failed to parse config: %v", err)
		return fmt.Errorf("failed to parse config: %v", err)
	}

	// We only need this plugin to kick in if egress is enabled
	if netConf.Enabled != "true" {
		log.Debugf("egress-cni plugin is disabled")
		return nil
	}

	log.Debugf("Received Del Request: netnsPath: %s conf=%v", args.Netns, netConf)
	if err := ipam.ExecDel(netConf.IPAM.Type, args.StdinData); err != nil {
		log.Debugf("running IPAM plugin failed: %v", err)
		return fmt.Errorf("running IPAM plugin failed: %v", err)
	}

	nodeIP := netConf.NodeIP
	isIPv6Egress := nodeIP.To4() == nil
	var chainPrefix string
	if isIPv6Egress {
		chainPrefix = "E6-"
	} else {
		chainPrefix = "E4-"
	}
	chain := utils.MustFormatChainNameWithPrefix(netConf.Name, args.ContainerID, chainPrefix)
	comment := utils.FormatComment(netConf.Name, args.ContainerID)

	if isIPv6Egress {
		netConf.IfName = netconf.EgressIPv6InterfaceName
		return cni.CmdDelEgressV6(args.Netns, netConf.IfName, chain, comment, log)
	}
	netConf.IfName = netconf.EgressIPv4InterfaceName
	return cni.CmdDelEgressV4(args.Netns, netConf.IfName, nodeIP, chain, comment, log)
}
