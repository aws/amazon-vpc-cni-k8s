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

package engine

import (
	"github.com/aws/amazon-ecs-cni-plugins/pkg/cniipwrapper"
	"github.com/containernetworking/cni/pkg/ns"
	"github.com/containernetworking/cni/pkg/types/current"
	"github.com/pkg/errors"
)

// createVethPairContext wraps the parameters and the method to create the
// veth pair to attach the container namespace to the bridge
type createVethPairContext struct {
	interfaceName string
	mtu           int
	ip            cniipwrapper.IP
	// hostVethName is set when the closure executes. Don't expect this
	// to be initialized
	hostVethName string
	// containerInterfaceResult is set when the closure executes. Don't
	// expect this to be initialized
	containerInterfaceResult *current.Interface
}

func newCreateVethPairContext(
	interfaceName string,
	mtu int,
	ip cniipwrapper.IP) *createVethPairContext {

	return &createVethPairContext{
		interfaceName: interfaceName,
		mtu:           mtu,
		ip:            ip,
	}
}

// run defines the closure to execute within the container's namespace to
// create the veth pair
func (createVethContext *createVethPairContext) run(hostNS ns.NetNS) error {
	hostVeth, containerVeth, err := createVethContext.ip.SetupVeth(
		createVethContext.interfaceName, createVethContext.mtu, hostNS)
	if err != nil {
		return errors.Wrapf(err,
			"bridge create veth pair: unable to setup veth pair for interface: %s",
			createVethContext.interfaceName)
	}

	createVethContext.hostVethName = hostVeth.Name

	createVethContext.containerInterfaceResult = &current.Interface{
		Name: containerVeth.Name,
		Mac:  containerVeth.HardwareAddr.String(),
	}
	return nil
}
