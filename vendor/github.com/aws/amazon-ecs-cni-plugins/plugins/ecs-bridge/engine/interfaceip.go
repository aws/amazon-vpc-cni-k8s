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
	"github.com/aws/amazon-ecs-cni-plugins/pkg/netlinkwrapper"
	"github.com/containernetworking/cni/pkg/ns"
	"github.com/pkg/errors"
	"github.com/vishvananda/netlink"
)

// getContainerIPV4Context wraps the parameters and the method to
// get the ipv4 address of the container's veth's interface. This should
// only be needed if there's a need to determine the IPv4 address of the
// interface before executing the DEL command using the ipam plugin
type getContainerIPV4Context struct {
	interfaceName string
	netLink       netlinkwrapper.NetLink
	// ipv4Addr is set when the closure executes. Don't expect this
	// to be initialized
	ipv4Addr string
}

func newGetContainerIPV4Context(
	interfaceName string,
	netLink netlinkwrapper.NetLink) *getContainerIPV4Context {
	return &getContainerIPV4Context{
		interfaceName: interfaceName,
		netLink:       netLink,
	}
}

// run defines the closure to execute within container's namespace to get
// the ipv4 address of the veth interface
func (ipv4Context *getContainerIPV4Context) run(hostNS ns.NetNS) error {
	link, err := ipv4Context.netLink.LinkByName(ipv4Context.interfaceName)
	if err != nil {
		return errors.Wrapf(err,
			"bridge getipv4 address: unable to get link for interface: %s",
			ipv4Context.interfaceName)
	}

	addrs, err := ipv4Context.netLink.AddrList(link, netlink.FAMILY_V4)
	if err != nil {
		return errors.Wrapf(err,
			"bridge getipv4 address: unable to list ipv4 addresses for interface: %s",
			ipv4Context.interfaceName)
	}

	if len(addrs) == 0 {
		return errors.Errorf(
			"bridge getipv4 address: no ipv4 addresses returned for interface: %s",
			ipv4Context.interfaceName)
	}

	ipv4Context.ipv4Addr = addrs[0].IPNet.IP.String()
	return nil
}
