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

// Package hostipamwrapper is a wrapper method for the hostipam package
package hostipamwrapper

import (
	"github.com/containernetworking/cni/pkg/types"
	current "github.com/containernetworking/cni/pkg/types/100"
	_ipam "github.com/containernetworking/plugins/pkg/ipam"
)

// HostIpam is an interface created to make code unit testable.
// Both the hostipam package version and mocked version implement the same interface
type HostIpam interface {
	ExecAdd(plugin string, netconf []byte) (types.Result, error)

	ExecCheck(plugin string, netconf []byte) error

	ExecDel(plugin string, netconf []byte) error

	ConfigureIface(ifName string, res *current.Result) error
}

type hostipam struct{}

// NewIpam return a new HostIpam object
func NewIpam() HostIpam {
	return &hostipam{}
}
func (h *hostipam) ExecAdd(plugin string, netconf []byte) (types.Result, error) {
	return _ipam.ExecAdd(plugin, netconf)
}

func (h *hostipam) ExecCheck(plugin string, netconf []byte) error {
	return _ipam.ExecCheck(plugin, netconf)
}

func (h *hostipam) ExecDel(plugin string, netconf []byte) error {
	return _ipam.ExecDel(plugin, netconf)
}

func (h *hostipam) ConfigureIface(ifName string, res *current.Result) error {
	return _ipam.ConfigureIface(ifName, res)
}
