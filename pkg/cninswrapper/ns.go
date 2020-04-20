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

package cninswrapper

import "github.com/containernetworking/plugins/pkg/ns"

// NS wraps methods used from the cni/pkg/ns package
type NS interface {
	GetNS(nspath string) (ns.NetNS, error)
	WithNetNSPath(nspath string, toRun func(ns.NetNS) error) error
}

type cniNS struct {
}

// NewNS creates a new NS object
func NewNS() NS {
	return &cniNS{}
}

func (*cniNS) GetNS(nspath string) (ns.NetNS, error) {
	return ns.GetNS(nspath)
}

func (*cniNS) WithNetNSPath(nspath string, toRun func(ns.NetNS) error) error {
	return ns.WithNetNSPath(nspath, toRun)
}
