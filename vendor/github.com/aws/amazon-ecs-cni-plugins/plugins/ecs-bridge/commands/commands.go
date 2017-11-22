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

package commands

import (
	"github.com/aws/amazon-ecs-cni-plugins/pkg/utils"
	"github.com/aws/amazon-ecs-cni-plugins/plugins/ecs-bridge/engine"
	"github.com/aws/amazon-ecs-cni-plugins/plugins/ecs-bridge/types"

	log "github.com/cihub/seelog"
	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types/current"
)

// Add invokes the command to create the bridge add the veth pair to
// connect container's namespace with the bridge
func Add(args *skel.CmdArgs) error {
	defer log.Flush()
	return add(args, engine.New())
}

// Del invokes the command to tear down the bridge and the veth pair
func Del(args *skel.CmdArgs) error {
	defer log.Flush()
	return del(args, engine.New())
}

func add(args *skel.CmdArgs, engine engine.Engine) error {
	conf, err := types.NewConf(args)
	if err != nil {
		// TODO: We log and return errors throughout this function.
		// Either should be sufficient.
		log.Errorf("Error loading config from args: %v", err)
		return err
	}

	log.Infof("Creating the bridge: %s", conf.BridgeName)
	bridge, err := engine.CreateBridge(conf.BridgeName, conf.MTU)
	if err != nil {
		return err
	}

	log.Infof("Creating veth pair for namespace: %s", args.Netns)
	containerVethInterface, hostVethName, err := engine.CreateVethPair(
		args.Netns, conf.MTU, args.IfName)
	if err != nil {
		return err
	}

	log.Infof("Attaching veth pair %s to bridge", hostVethName)
	hostVethInterface, err := engine.AttachHostVethInterfaceToBridge(hostVethName, bridge)
	if err != nil {
		return err
	}

	log.Infof("Running IPAM plugin ADD: %s", conf.IPAM.Type)
	result, err := engine.RunIPAMPluginAdd(conf.IPAM.Type, args.StdinData)
	if err != nil {
		return err
	}

	// Construct the Interfaces list for the result returned from the IPAM
	// plugin. This consists of all the interfaces we know about. This
	// includes the bridge, the two ends of the veth interface created
	result.Interfaces = []*current.Interface{
		// the bridge interface
		0: &current.Interface{
			Name: bridge.Attrs().Name,
			Mac:  bridge.Attrs().HardwareAddr.String(),
		},
		// the host veth interface
		1: hostVethInterface,
		// the container veth interface
		2: containerVethInterface,
	}

	// Set the index for the container veth interface in the `Interfaces`
	// list populated above.
	// The `ipam.ConfigureIface` method needs this index to be set as it
	// needs to know which interface should be used when adding routes
	result.IPs[0].Interface = 2

	log.Infof("Configuring container's interface: %s", args.Netns)
	err = engine.ConfigureContainerVethInterface(args.Netns, result, args.IfName)
	if err != nil {
		return err
	}

	log.Infof("Configuring bridge: %s", conf.BridgeName)
	err = engine.ConfigureBridge(result, bridge)
	if err != nil {
		return err
	}
	return result.Print()
}

func del(args *skel.CmdArgs, engine engine.Engine) error {
	conf, err := types.NewConf(args)
	if err != nil {
		// TODO: We log and return errors throughout this function.
		// Either should be sufficient.
		log.Errorf("Error loading config from args: %v", err)
		return err
	}

	if utils.ZeroOrNil(conf.IPAM) {
		log.Infof("IPAM configuration not found, skip DEL for IPAM")
		return nil
	}

	log.Infof("Running IPAM plugin DEL: %s", conf.IPAM.Type)
	err = engine.RunIPAMPluginDel(conf.IPAM.Type, args.StdinData)
	if err != nil {
		return err
	}

	log.Infof("Deleting container interface: %s", args.Netns)
	return engine.DeleteVeth(args.Netns, args.IfName)
}
