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

package utils

import "time"

const (
	DefaultTestNamespace = "cni-automation"
	AwsNodeNamespace     = "kube-system"
	AwsNodeName          = "aws-node"
	AWSInitContainerName = "aws-vpc-cni-init"

	// See https://gallery.ecr.aws/r3i6j7b0/aws-vpc-cni-test-helper
	TestAgentImage = "public.ecr.aws/r3i6j7b0/aws-vpc-cni-test-helper:66180f40"
)

const (
	PollIntervalShort  = time.Second * 2
	PollIntervalMedium = time.Second * 5
	PollIntervalLong   = time.Second * 20

	DefaultDeploymentReadyTimeout = time.Second * 120
)
