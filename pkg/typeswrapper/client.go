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

package typeswrapper

import (
	cnitypes "github.com/containernetworking/cni/pkg/types"
)

type CNITYPES interface {
	LoadArgs(args string, container interface{}) error
	PrintResult(result cnitypes.Result, version string) error
}

type cniTYPES struct{}

func New() CNITYPES {
	return &cniTYPES{}
}

func (*cniTYPES) LoadArgs(args string, container interface{}) error {
	return cnitypes.LoadArgs(args, container)
}

func (*cniTYPES) PrintResult(result cnitypes.Result, version string) error {
	return cnitypes.PrintResult(result, version)
}
