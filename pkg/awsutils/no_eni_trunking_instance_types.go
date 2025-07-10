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

// Package awsutils is a utility package for calling EC2 or IMDS
package awsutils

// NoENITrunkingInstanceTypes contains instance type that does not support ENI Trunking. Documentation found at
// https://docs.aws.amazon.com/AmazonECS/latest/developerguide/eni-trunking-supported-instance-types.html
var noENITrunkingInstanceTypes = []string{
	"a1.metal",
	"c5.metal",
	"c5a.8xlarge",
	"c5ad.8xlarge",
	"c5d.metal",
	"m5.metal",
	"p3dn.24xlarge",
	"r5.metal",
	"r5.8xlarge",
	"r5d.metal",
}

// NoENITrunkingInstanceFamilies contains instance family that does not support ENI Trunking. Documentation found at
// https://docs.aws.amazon.com/AmazonECS/latest/developerguide/eni-trunking-supported-instance-types.html
var noENITrunkingInstanceFamilies = []string{
	"c5n",
	"d3",
	"d3en",
	"g3",
	"g3s",
	"g4dn",
	"i3",
	"i3en",
	"inf1",
	"m5dn",
	"m5n",
	"m5zn",
	"mac1",
	"r5b",
	"r5n",
	"r5dn",
	"u-12tb1",
	"u-6tb1",
	"u-9tb1",
	"z1d",
}
