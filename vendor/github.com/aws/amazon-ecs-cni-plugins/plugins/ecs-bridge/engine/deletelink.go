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
	"github.com/containernetworking/cni/pkg/ip"
	"github.com/containernetworking/cni/pkg/ns"
	"github.com/pkg/errors"
	"github.com/vishvananda/netlink"
)

// deleteLinkContext wraps the parameters and the method to delete the
// veth pair during teardown
type deleteLinkContext struct {
	interfaceName string
	ip            cniipwrapper.IP
}

func newDeleteLinkContext(interfaceName string, ip cniipwrapper.IP) *deleteLinkContext {
	return &deleteLinkContext{
		interfaceName: interfaceName,
		ip:            ip,
	}
}

// run defines the closure to execute within the container's namespace to delete
// the veth pair
func (delContext *deleteLinkContext) run(hostNS ns.NetNS) error {
	_, err := delContext.ip.DelLinkByNameAddr(
		delContext.interfaceName, netlink.FAMILY_V4)
	if err != nil {
		if err == ip.ErrLinkNotFound {
			return nil
		}
		return errors.Wrapf(err,
			"bridge delete veth: unable to delete link for interface: %s",
			delContext.interfaceName)
	}

	return nil
}
