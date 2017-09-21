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
	"github.com/aws/amazon-ecs-cni-plugins/pkg/cniipamwrapper"
	"github.com/aws/amazon-ecs-cni-plugins/pkg/cniipwrapper"
	"github.com/aws/amazon-ecs-cni-plugins/pkg/netlinkwrapper"
	"github.com/containernetworking/cni/pkg/ns"
	"github.com/containernetworking/cni/pkg/types/current"
	"github.com/pkg/errors"
	"github.com/vishvananda/netlink"
)

// configureVethContext wraps the parameters and the method to configure the
// veth interface in container's namespace
type configureVethContext struct {
	interfaceName string
	result        *current.Result
	ip            cniipwrapper.IP
	ipam          cniipamwrapper.IPAM
	netLink       netlinkwrapper.NetLink
}

func newConfigureVethContext(interfaceName string,
	result *current.Result,
	ip cniipwrapper.IP,
	ipam cniipamwrapper.IPAM,
	netLink netlinkwrapper.NetLink) *configureVethContext {

	return &configureVethContext{
		interfaceName: interfaceName,
		result:        result,
		ip:            ip,
		ipam:          ipam,
		netLink:       netLink,
	}
}

// run defines the closure to execute within the container's namespace to
// configure the veth interface
func (configContext *configureVethContext) run(hostNS ns.NetNS) error {
	// Configure routes in the container
	err := configContext.ipam.ConfigureIface(
		configContext.interfaceName, configContext.result)
	if err != nil {
		return errors.Wrapf(err,
			"bridge configure veth: unable to configure interface: %s",
			configContext.interfaceName)
	}
	// Generate and set the hardware address for the interface, given
	// its IP
	err = configContext.ip.SetHWAddrByIP(
		configContext.interfaceName, configContext.result.IPs[0].Address.IP, nil)
	if err != nil {
		return errors.Wrapf(err,
			"bridge configure veth: unable to set hardware address for interface: %s",
			configContext.interfaceName)
	}

	link, err := configContext.netLink.LinkByName(configContext.interfaceName)
	if err != nil {
		return errors.Wrapf(err,
			"bridge configure veth: unable to get link for interface: %s",
			configContext.interfaceName)
	}

	routes, err := configContext.netLink.RouteList(link, netlink.FAMILY_ALL)
	if err != nil {
		return errors.Wrapf(err,
			"bridge configure veth: unable to fetch routes for interface: %s",
			configContext.interfaceName)
	}

	// Delete all default routes within the container
	for _, route := range routes {
		if route.Gw == nil {
			err = configContext.netLink.RouteDel(&route)
			if err != nil {
				return errors.Wrapf(err,
					"bridge configure veth: unable to delete route: %v", route)
			}
		}
	}

	return nil
}
