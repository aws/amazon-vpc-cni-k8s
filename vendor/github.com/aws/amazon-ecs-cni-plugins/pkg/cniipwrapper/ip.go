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

package cniipwrapper

import (
	"net"

	"github.com/containernetworking/cni/pkg/ip"
	"github.com/containernetworking/cni/pkg/ns"
)

// IPAM wraps methods used from the the cni/pkg/ip package
// Seee github.com/containernetworking/cni/pkg/ip for more details
type IP interface {
	// SetupVeth creates a veth pair
	SetupVeth(contVethName string, mtu int, hostNS ns.NetNS) (net.Interface, net.Interface, error)
	// SetHWAddrByIP sets the hardware address for the interface identified
	// by the ip address
	SetHWAddrByIP(ifName string, ip4 net.IP, ip6 net.IP) error
	// DelLinkByNameAddr deletes the interface
	DelLinkByNameAddr(ifName string, family int) (*net.IPNet, error)
}

type cniIP struct{}

// New creates a new IP object
func New() IP {
	return &cniIP{}
}

func (*cniIP) SetupVeth(contVethName string, mtu int, hostNS ns.NetNS) (net.Interface, net.Interface, error) {
	return ip.SetupVeth(contVethName, mtu, hostNS)
}

func (*cniIP) SetHWAddrByIP(ifName string, ip4 net.IP, ip6 net.IP) error {
	return ip.SetHWAddrByIP(ifName, ip4, ip6)
}

func (*cniIP) DelLinkByNameAddr(ifName string, family int) (*net.IPNet, error) {
	return ip.DelLinkByNameAddr(ifName, family)
}
