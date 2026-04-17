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

package networkutils

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strconv"
)

// GeneratePodHostVethName generates the name for Pod's host-side veth device.
// The veth name is generated in a way that aligns with the value expected by Calico for NetworkPolicy enforcement.
func GeneratePodHostVethName(prefix string, podNamespace string, podName string, index int) string {

	if index > 0 {
		podName = fmt.Sprintf("%s.%s", podName, strconv.Itoa(index))
	}
	suffix := GeneratePodHostVethNameSuffix(podNamespace, podName)
	return fmt.Sprintf("%s%s", prefix, suffix)
}

// GeneratePodHostVethNameSuffix generates the name suffix for Pod's hostVeth.
func GeneratePodHostVethNameSuffix(podNamespace string, podName string) string {
	h := sha1.New()
	h.Write([]byte(fmt.Sprintf("%s.%s", podNamespace, podName)))
	return hex.EncodeToString(h.Sum(nil))[:11]
}

// Generates the interface name inside the pod namespace
func GenerateContainerVethName(defaultIfName string, prefix string, index int) string {
	if index > 0 {
		return fmt.Sprintf("%s%s", prefix, strconv.Itoa(index))
	}
	return defaultIfName
}
