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

// Package vethwrapper is a wrapper method for the ip.SetupVeth method
package vethwrapper

import (
	"net"

	"github.com/containernetworking/plugins/pkg/ip"
	"github.com/containernetworking/plugins/pkg/ns"
)

// Veth is an interface created to make code unit testable.
// Both the veth version and mocked version implement the same interface
type Veth interface {
	Setup(contVethName string, mtu int, contVethMac string, hostNS ns.NetNS) (net.Interface, net.Interface, error)
}

type veth struct{}

// NewSetupVeth return a new veth object
func NewSetupVeth() Veth {
	return &veth{}
}
func (v *veth) Setup(contVethName string, mtu int, contVethMac string, hostNS ns.NetNS) (net.Interface, net.Interface, error) {
	return ip.SetupVeth(contVethName, mtu, contVethMac, hostNS)
}
