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

type EnforcingMode string

const (
	EnforcingModeStrict               EnforcingMode = "strict"
	EnforcingModeStandard             EnforcingMode = "standard"
	VpcCNINodeEventActionForTrunk     string        = "NeedTrunk"
	TrunkEventNote                    string        = "vpc.amazonaws.com/has-trunk-attached=false"
	VpcCNINodeEventActionForEniConfig string        = "NeedEniConfig"
	VpcCNIEventReason                 string        = "AwsNodeNotificationToRc"
)

const (
	// DefaultEnforcingMode is the default enforcing mode if not specified explicitly.
	DefaultEnforcingMode EnforcingMode = EnforcingModeStrict
	// environment variable knob to decide EnforcingMode for SGPP feature.
	envEnforcingMode = "POD_SECURITY_GROUP_ENFORCING_MODE"
)
