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

package input

import "encoding/json"

type TestStatus struct {
	SuccessCount int
	FailureCount int
	SourcePod    string
	Failures     []Failure
}

type Failure struct {
	DestinationIP string
	FailureReason string
}

type PodNetworkingValidationInput struct {
	// CIDR Range associated with the VPC
	VPCCidrRange []string
	// Prefix for the veth pair on host network ns
	VethPrefix string
	// List of pod to validate the networking
	PodList []Pod
	// Should Validate MTU value, by default it will false
	ValidateMTU bool
	// Expected MTU value
	MTU int
}

type Pod struct {
	// Name of the pod
	PodName string
	// Namespace of the pod, used to generate the Link
	PodNamespace string
	// IPv4 Address of the pod
	PodIPv4Address string
	// Set to true when the Pod is scheduled on IP
	// from the Secondary ENI
	IsIPFromSecondaryENI bool
}

func (ip PodNetworkingValidationInput) Serialize() (string, error) {
	inputBytes, err := json.Marshal(ip)
	if err != nil {
		return "", err
	}
	return string(inputBytes), nil
}
