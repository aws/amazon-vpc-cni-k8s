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

package custom_networking_sgpp

import (
	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Trunk ENI Security Group Test", func() {
	Context("when validating security group on trunk ENI", func() {
		It("should match security group in ENIConfig", func() {
			instanceID := k8sUtils.GetInstanceIDFromNode(targetNode)
			instance, err := f.CloudServices.EC2().DescribeInstance(instanceID)
			Expect(err).ToNot(HaveOccurred())

			trunkSGMatch := false
			for _, nwInterface := range instance.NetworkInterfaces {
				if *nwInterface.InterfaceType == "trunk" {
					for _, group := range nwInterface.Groups {
						if *group.GroupId == customNetworkingSGID {
							trunkSGMatch = true
							break
						}
					}
					if trunkSGMatch {
						break
					}
				}
			}
			Expect(trunkSGMatch).To(BeTrue())
		})
	})
})
