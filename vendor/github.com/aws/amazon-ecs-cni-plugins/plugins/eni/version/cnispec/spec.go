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

package cnispec

import "github.com/containernetworking/cni/pkg/version"

// specVersionSupported is the version of the CNI spec that's supported by the
// ENI plugin. It's set to 0.3.0, which means that we support the following
// commands:
// * ADD
// * DELETE
// * VERSION
// Refer to https://github.com/containernetworking/cni/blob/master/SPEC.md
// for details
var specVersionSupported = version.PluginSupports("0.3.0")

// GetSpecVersionSupported gets the version of the CNI spec that's supported
// by the ENI plugin
func GetSpecVersionSupported() version.PluginInfo {
	return specVersionSupported
}
