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

package ipamd

import (
	"context"
	"strconv"
	"time"

	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// IMPORTANT: THE NODEGROUP TO RUN THE TEST MUST NOT HAVE ANY POD
// Ideally we should drain the node, but drain from go client is non trivial
// IMPORTANT: Only support nodes that can have 16+ Secondary IPV4s across at least 3 ENI
var _ = Describe("test warm target variables", func() {

	Context("when warm ENI target is used", func() {
		var warmENITarget int
		var maxENI int

		JustBeforeEach(func() {
			k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f,
				utils.AwsNodeName, utils.AwsNodeNamespace, utils.AwsNodeName,
				map[string]string{
					"WARM_ENI_TARGET": strconv.Itoa(warmENITarget),
					"MAX_ENI":         strconv.Itoa(maxENI),
				})

			Eventually(func(g Gomega) {
				primaryInstance, err = f.CloudServices.
					EC2().DescribeInstance(context.TODO(), *primaryInstance.InstanceId)
				g.Expect(err).ToNot(HaveOccurred())

				// Validate number of allocated ENIs
				g.Expect(len(primaryInstance.NetworkInterfaces)).
					Should(Equal(MinIgnoreZero(warmENITarget, maxENI)))
			}).WithTimeout(10 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())
		})

		JustAfterEach(func() {
			k8sUtils.RemoveVarFromDaemonSetAndWaitTillUpdated(f,
				utils.AwsNodeName, utils.AwsNodeNamespace, utils.AwsNodeName,
				map[string]struct{}{"WARM_ENI_TARGET": {}, "MAX_ENI": {}})
		})

		Context("when WARM_ENI_TARGET = 3 and MAX_ENI = 2", func() {
			BeforeEach(func() {
				warmENITarget = 3
				maxENI = 2
			})

			It("instance should have only 2 ENIs", func() {})
		})

		Context("when WARM_ENI_TARGET = 3", func() {
			BeforeEach(func() {
				warmENITarget = 3
				maxENI = 0
			})

			It("instance should have only 3 ENIs", func() {})
		})

		Context("when WARM_ENI_TARGET = 2", func() {
			BeforeEach(func() {
				warmENITarget = 2
				maxENI = 0
			})

			It("instance should have only 2 ENIs", func() {})
		})

	})

	Context("when warm IP target is set", func() {
		var warmIPTarget int
		var minIPTarget int

		JustBeforeEach(func() {
			// Set the WARM IP TARGET
			k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f,
				utils.AwsNodeName, utils.AwsNodeNamespace, utils.AwsNodeName,
				map[string]string{
					"WARM_IP_TARGET":    strconv.Itoa(warmIPTarget),
					"MINIMUM_IP_TARGET": strconv.Itoa(minIPTarget),
				})

			Eventually(func(g Gomega) {
				var availIPs int
				// Query the EC2 Instance to get the list of available IPs on the instance
				primaryInstance, err = f.CloudServices.
					EC2().DescribeInstance(context.TODO(), *primaryInstance.InstanceId)
				g.Expect(err).ToNot(HaveOccurred())

				// Sum all the IPs on all network interfaces minus the primary IPv4 address per ENI
				for _, networkInterface := range primaryInstance.NetworkInterfaces {
					availIPs += len(networkInterface.PrivateIpAddresses) - 1
				}

				// Validated avail IP equals the warm IP Size
				g.Expect(availIPs).Should(Equal(Max(warmIPTarget, minIPTarget)))
			}).WithTimeout(10 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())
		})

		JustAfterEach(func() {
			k8sUtils.RemoveVarFromDaemonSetAndWaitTillUpdated(f,
				utils.AwsNodeName, utils.AwsNodeNamespace, utils.AwsNodeName,
				map[string]struct{}{"WARM_IP_TARGET": {}, "MINIMUM_IP_TARGET": {}})
		})

		Context("when WARM_IP_TARGET = 2", func() {
			BeforeEach(func() {
				warmIPTarget = 2
				minIPTarget = 0
			})

			It("should have 2 secondary IPv4 addresses", func() {})
		})

		Context("when WARM_IP_TARGET = 16", func() {
			BeforeEach(func() {
				warmIPTarget = 16
				minIPTarget = 0
			})

			It("should have 16 secondary IPv4 addresses", func() {})
		})

		Context("when MINIMUM_IP_TARGET = 2", func() {
			BeforeEach(func() {
				warmIPTarget = 0
				minIPTarget = 2
			})

			It("should have 2 secondary IPv4 addresses", func() {})
		})

		Context("when MINIMUM_IP_TARGET = 16", func() {
			BeforeEach(func() {
				warmIPTarget = 0
				minIPTarget = 16
			})

			It("should have 16 secondary IPv4 addresses", func() {})
		})

		Context("when MINIMUM_IP_TARGET = 6 and WARM_IP_TARGET = 10", func() {
			BeforeEach(func() {
				warmIPTarget = 6
				minIPTarget = 10
			})

			It("should have 10 secondary IPv4 addresses", func() {})
		})
	})
})
