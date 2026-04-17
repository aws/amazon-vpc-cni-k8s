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

package sgpp

import (
	"os"
)

const vlanInterfacePrefix = "vlan"

// BuildHostVethNamePrefix computes the name prefix for host-side veth pairs for SGPP pods
// for the "standard" mode, we use the same hostVethNamePrefix as normal pods, which is "eni" by default, but can be overwritten as well.
// for the "strict" mode, we use dedicated "vlan" hostVethNamePrefix, which is to opt-out SNAT support and opt-out calico's workload management.
func BuildHostVethNamePrefix(hostVethNamePrefix string, podSGEnforcingMode EnforcingMode) string {
	switch podSGEnforcingMode {
	case EnforcingModeStrict:
		return vlanInterfacePrefix
	case EnforcingModeStandard:
		return hostVethNamePrefix
	default:
		return vlanInterfacePrefix
	}
}

// LoadEnforcingModeFromEnv tries to load the enforcing mode from environment variable and fall-back to DefaultEnforcingMode.
func LoadEnforcingModeFromEnv() EnforcingMode {
	envVal, _ := os.LookupEnv(envEnforcingMode)
	switch envVal {
	case string(EnforcingModeStrict):
		return EnforcingModeStrict
	case string(EnforcingModeStandard):
		return EnforcingModeStandard
	default:
		return DefaultEnforcingMode
	}
}
