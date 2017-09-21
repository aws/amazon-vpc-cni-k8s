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

package cniipamwrapper

import (
	cni_ipam "github.com/containernetworking/cni/pkg/ipam"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/cni/pkg/types/current"
)

// IPAM wraps methods used from the the cni/pkg/ipam package
type IPAM interface {
	// ExecAdd invokes the IPAM plugin with the ADD command
	ExecAdd(plugin string, netconf []byte) (types.Result, error)
	// ExecDel invokes the IPAM plugin with the ADD command
	ExecDel(plugin string, netconf []byte) error
	// ConfigureIface configures the interface named ifName
	// with the result object
	ConfigureIface(ifName string, res *current.Result) error
}

type ipam struct{}

// New creates a new IPAM object
func New() IPAM {
	return &ipam{}
}

func (*ipam) ExecAdd(plugin string, netconf []byte) (types.Result, error) {
	return cni_ipam.ExecAdd(plugin, netconf)
}

func (*ipam) ExecDel(plugin string, netconf []byte) error {
	return cni_ipam.ExecDel(plugin, netconf)
}

func (*ipam) ConfigureIface(ifName string, res *current.Result) error {
	return cni_ipam.ConfigureIface(ifName, res)
}
