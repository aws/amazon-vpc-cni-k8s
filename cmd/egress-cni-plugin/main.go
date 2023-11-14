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

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	current "github.com/containernetworking/cni/pkg/types/100"
	cniversion "github.com/containernetworking/cni/pkg/version"
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
	ec := NewEgressAddContext(args.Netns, args.IfName)
	return add(args, &ec)
}

func add(args *skel.CmdArgs, ec *egressContext) (err error) {
	ec.NetConf, ec.Log, err = LoadConf(args.StdinData)
	if err != nil {
		return fmt.Errorf("failed to parse config: %v", err)
	}

	ec.Log.Debugf("Received an Add request: nsPath: %s conf=%+v", ec.NsPath, *ec.NetConf)
	// uncomment following lines to debug inputs
	//stdin := args.StdinData
	//args.StdinData = nil
	//ec.Log.Debugf("args: %+v, stdinData: %+v", *args, string(stdin))
	//args.StdinData = stdin

	if ec.NetConf.PrevResult == nil {
		ec.Log.Debugf("must be called as a chained plugin")
		return fmt.Errorf("must be called as a chained plugin")
	}

	ec.Result, err = current.GetResult(ec.NetConf.PrevResult)
	if err != nil {
		ec.Log.Errorf("failed to get PrevResult: %v", err)
		return err
	}
	// Convert MTU from string to int
	ec.Mtu, err = strconv.Atoi(ec.NetConf.MTU)
	if err != nil {
		ec.Log.Errorf("failed to parse MTU: %s, err: %v", ec.NetConf.MTU, err)
		return err
	}
	// We will not be vending out this as a separate plugin by itself, and it is only intended to be used as a
	// chained plugin to VPC CNI. We only need this plugin to kick in if egress is enabled in VPC CNI. So, the
	// value of an env variable in VPC CNI determines whether this plugin should be enabled and this is an attempt to
	// pass through the variable configured in VPC CNI.
	if ec.NetConf.Enabled != "true" {
		return types.PrintResult(ec.Result, ec.NetConf.CNIVersion)
	}

	// Invoke ipam del if err to avoid ip leak
	defer func() {
		if err != nil {
			ec.Ipam.ExecDel(ec.NetConf.IPAM.Type, args.StdinData)
		}
	}()
	err = ec.hostLocalIpamAdd(args.StdinData)
	if err != nil {
		ec.Log.Errorf("failed to get one ip address from host-local ipam: %v", err)
		return err
	}

	ec.SnatComment = utils.FormatComment(ec.NetConf.Name, args.ContainerID)
	if ec.NetConf.NodeIP.To4() == nil { // NodeIP is not IPv4 address, pod IPv6 egress for eks IPv4 cluster
		if ec.NetConf.NodeIP == nil || !ec.NetConf.NodeIP.IsGlobalUnicast() {
			return fmt.Errorf("global unicast IPv6 not found in host primary interface which is mandatory to support IPv6 egress")
		}
		ec.SnatChain = utils.MustFormatChainNameWithPrefix(ec.NetConf.Name, args.ContainerID, "E6-")
		ec.NetConf.IfName = egressIPv6InterfaceName
		err = ec.cmdAddEgressV6()
	} else { // NodeIP is IPv4 address, pod IPv4 egress for eks IPv6 cluster
		ec.SnatChain = utils.MustFormatChainNameWithPrefix(ec.NetConf.Name, args.ContainerID, "E4-")
		ec.NetConf.IfName = egressIPv4InterfaceName
		err = ec.cmdAddEgressV4()
	}
	return err
}

func cmdDel(args *skel.CmdArgs) error {
	ec := NewEgressDelContext(args.Netns)
	return del(args, &ec)
}

func del(args *skel.CmdArgs, ec *egressContext) (err error) {
	ec.NetConf, ec.Log, err = LoadConf(args.StdinData)
	if err != nil {
		return fmt.Errorf("failed to parse config: %v", err)
	}
	ec.Log.Debugf("Received a Del request: nsPath: %s conf=%+v", ec.NsPath, *ec.NetConf)

	// We only need this plugin to kick in if egress is enabled
	if ec.NetConf.Enabled != "true" {
		ec.Log.Debugf("egress-cni plugin is disabled")
		return nil
	}

	if err = ec.Ipam.ExecDel(ec.NetConf.IPAM.Type, args.StdinData); err != nil {
		ec.Log.Debugf("running IPAM plugin failed: %v", err)
		return fmt.Errorf("running IPAM plugin failed: %v", err)
	}

	ec.SnatComment = utils.FormatComment(ec.NetConf.Name, args.ContainerID)
	var ipv4 bool
	if ec.NetConf.NodeIP.To4() == nil { // NodeIP is not IPv4 address
		ipv4 = false
		ec.SnatChain = utils.MustFormatChainNameWithPrefix(ec.NetConf.Name, args.ContainerID, "E6-")
		// IPv6 egress
		ec.NetConf.IfName = egressIPv6InterfaceName
	} else {
		ipv4 = true
		ec.SnatChain = utils.MustFormatChainNameWithPrefix(ec.NetConf.Name, args.ContainerID, "E4-")
		// IPv4 egress
		ec.NetConf.IfName = egressIPv4InterfaceName
	}

	return ec.cmdDelEgress(ipv4)
}
