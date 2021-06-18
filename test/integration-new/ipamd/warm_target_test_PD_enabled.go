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
	"fmt"
	"strconv"
	"time"

	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

// IMPORTANT: THE NODEGROUP TO RUN THE TEST MUST NOT HAVE ANY POD
// Ideally we should drain the node, but drain from go client is non trivial
// IMPORTANT: Only support nodes that can have 16+ Secondary IPV4s across at least 3 ENI
var _ = Describe("test warm target variables", func() {

	Context("when warm and min IP target is set with PD enabled", func() {
		var warmIPTarget, minIPTarget int

		JustBeforeEach(func() {
			var availPrefixes int
			podLabelKey := "eks.amazonaws.com/component"
			podLabelVal := "coredns"

			// Set the WARM IP TARGET
			k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f,
				utils.AwsNodeName, utils.AwsNodeNamespace, utils.AwsNodeName,
				map[string]string{
					"WARM_IP_TARGET":           strconv.Itoa(warmIPTarget),
					"MINIMUM_IP_TARGET":        strconv.Itoa(minIPTarget),
					"ENABLE_PREFIX_DELEGATION": "true",
				})

			// Allow for IPAMD to reconcile it's state
			time.Sleep(utils.PollIntervalLong)

			// Query the EC2 Instance to get the list of available Prefixes on the instance
			primaryInstance, err = f.CloudServices.
				EC2().DescribeInstance(*primaryInstance.InstanceId)
			Expect(err).ToNot(HaveOccurred())

			//Query for coredns pods
			podList, perr := f.K8sResourceManagers.PodManager().
				GetPodsWithLabelSelector(podLabelKey, podLabelVal)
			Expect(perr).ToNot(HaveOccurred())

			assigned := 0
			for _, pod := range podList.Items {
				By(fmt.Sprintf("verifying in node %s but pod's IP %s address belongs to node name %s",
					*primaryInstance.PrivateDnsName, pod.Status.PodIP, pod.Spec.NodeName))
				if pod.Spec.NodeName == *primaryInstance.PrivateDnsName {
					assigned++
					break
				}
			}

			// Sum all the IPs on all network interfaces minus the primary IPv4 address per ENI
			for _, networkInterface := range primaryInstance.NetworkInterfaces {
				availPrefixes += len(networkInterface.Ipv4Prefixes)
			}

			// Validated avail IP equals the warm IP Size
			prefixNeededForWarmIPTarget := ceil(assigned+warmIPTarget, 16)
			prefixNeededForMinIPTarget := ceil(minIPTarget, 16)
			Expect(availPrefixes).Should(Equal(Max(prefixNeededForWarmIPTarget, prefixNeededForMinIPTarget)))
		})

		JustAfterEach(func() {
			k8sUtils.RemoveVarFromDaemonSetAndWaitTillUpdated(f,
				utils.AwsNodeName, utils.AwsNodeNamespace, utils.AwsNodeName,
				map[string]struct{}{
					"WARM_IP_TARGET":           {},
					"MINIMUM_IP_TARGET":        {},
					"ENABLE_PREFIX_DELEGATION": {},
				})
		})

		Context("when WARM_IP_TARGET = 2", func() {
			BeforeEach(func() {
				warmIPTarget = 2
				minIPTarget = 0
			})

			It("should have 1 prefix", func() {})
		})

		Context("when WARM_IP_TARGET = 16", func() {
			BeforeEach(func() {
				warmIPTarget = 16
				minIPTarget = 0
			})

			It("should have 1 prefix", func() {})
		})

		Context("when MINIMUM_IP_TARGET = 2", func() {
			BeforeEach(func() {
				warmIPTarget = 0
				minIPTarget = 2
			})

			It("should have 1 prefix", func() {})
		})

		Context("when MINIMUM_IP_TARGET = 16", func() {
			BeforeEach(func() {
				warmIPTarget = 0
				minIPTarget = 16
			})

			It("should have 1 prefixe", func() {})
		})

		Context("when MINIMUM_IP_TARGET = 6 and WARM_IP_TARGET = 10", func() {
			BeforeEach(func() {
				warmIPTarget = 6
				minIPTarget = 10
			})

			It("should have 1 prefix", func() {})
		})
	})

	Context("when warm prefix target is set with PD enabled", func() {
		var warmPrefixTarget int

		JustBeforeEach(func() {
			var availPrefixes int
			podLabelKey := "eks.amazonaws.com/component"
			podLabelVal := "coredns"

			// Set the WARM IP TARGET
			k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f,
				utils.AwsNodeName, utils.AwsNodeNamespace, utils.AwsNodeName,
				map[string]string{
					"WARM_PREFIX_TARGET":       strconv.Itoa(warmPrefixTarget),
					"ENABLE_PREFIX_DELEGATION": "true",
				})

			// Allow for IPAMD to reconcile it's state
			time.Sleep(utils.PollIntervalLong)

			// Query the EC2 Instance to get the list of available Prefixes on the instance
			primaryInstance, err = f.CloudServices.
				EC2().DescribeInstance(*primaryInstance.InstanceId)
			Expect(err).ToNot(HaveOccurred())

			//Query for coredns pods
			podList, perr := f.K8sResourceManagers.PodManager().
				GetPodsWithLabelSelector(podLabelKey, podLabelVal)
			Expect(perr).ToNot(HaveOccurred())

			assigned := 0
			for _, pod := range podList.Items {
				By(fmt.Sprintf("verifying in node %s but pod's IP %s address belongs to node name %s",
					*primaryInstance.PrivateDnsName, pod.Status.PodIP, pod.Spec.NodeName))
				if pod.Spec.NodeName == *primaryInstance.PrivateDnsName {
					assigned++
					break
				}
			}

			// Sum all the IPs on all network interfaces minus the primary IPv4 address per ENI
			for _, networkInterface := range primaryInstance.NetworkInterfaces {
				availPrefixes += len(networkInterface.Ipv4Prefixes)
			}

			// Validated avail IP equals the warm IP Size
			prefixNeededForAssignedPods := ceil(assigned, 16)
			Expect(availPrefixes).Should(Equal(prefixNeededForAssignedPods + warmPrefixTarget))
		})

		JustAfterEach(func() {
			k8sUtils.RemoveVarFromDaemonSetAndWaitTillUpdated(f,
				utils.AwsNodeName, utils.AwsNodeNamespace, utils.AwsNodeName,
				map[string]struct{}{
					"WARM_PREFIX_TARGET":       {},
					"ENABLE_PREFIX_DELEGATION": {},
				})
		})

		Context("when WARM_PREFIX_TARGET = 2", func() {
			BeforeEach(func() {
				warmPrefixTarget = 2
			})

			It("should have 2 free prefixes", func() {})
		})

		Context("when WARM_PREFIX_TARGET = 0", func() {
			BeforeEach(func() {
				warmPrefixTarget = 0
			})

			It("should have no free prefixes", func() {})
		})
	})

	Context("when warm prefix, warm IP and min IP target is set with PD enabled", func() {
		var warmIPTarget, minIPTarget, warmPrefixTarget int

		JustBeforeEach(func() {
			var availPrefixes int
			podLabelKey := "eks.amazonaws.com/component"
			podLabelVal := "coredns"

			// Set the WARM IP TARGET
			k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f,
				utils.AwsNodeName, utils.AwsNodeNamespace, utils.AwsNodeName,
				map[string]string{
					"WARM_IP_TARGET":           strconv.Itoa(warmIPTarget),
					"MINIMUM_IP_TARGET":        strconv.Itoa(minIPTarget),
					"WARM_PREFIX_TARGET":       strconv.Itoa(warmPrefixTarget),
					"ENABLE_PREFIX_DELEGATION": "true",
				})

			// Allow for IPAMD to reconcile it's state
			time.Sleep(utils.PollIntervalLong)

			// Query the EC2 Instance to get the list of available Prefixes on the instance
			primaryInstance, err = f.CloudServices.
				EC2().DescribeInstance(*primaryInstance.InstanceId)
			Expect(err).ToNot(HaveOccurred())

			//Query for coredns pods
			podList, perr := f.K8sResourceManagers.PodManager().
				GetPodsWithLabelSelector(podLabelKey, podLabelVal)
			Expect(perr).ToNot(HaveOccurred())

			assigned := 0
			for _, pod := range podList.Items {
				By(fmt.Sprintf("verifying in node %s but pod's IP %s address belongs to node name %s",
					*primaryInstance.PrivateDnsName, pod.Status.PodIP, pod.Spec.NodeName))
				if pod.Spec.NodeName == *primaryInstance.PrivateDnsName {
					assigned++
					break
				}
			}

			// Sum all the IPs on all network interfaces minus the primary IPv4 address per ENI
			for _, networkInterface := range primaryInstance.NetworkInterfaces {
				availPrefixes += len(networkInterface.Ipv4Prefixes)
			}

			// Validated avail IP equals the warm IP Size
			prefixNeededForWarmIPTarget := ceil(assigned+warmIPTarget, 16)
			prefixNeededForMinIPTarget := ceil(minIPTarget, 16)
			Expect(availPrefixes).Should(Equal(Max(prefixNeededForWarmIPTarget, prefixNeededForMinIPTarget)))
		})

		JustAfterEach(func() {
			k8sUtils.RemoveVarFromDaemonSetAndWaitTillUpdated(f,
				utils.AwsNodeName, utils.AwsNodeNamespace, utils.AwsNodeName,
				map[string]struct{}{
					"WARM_IP_TARGET":           {},
					"MINIMUM_IP_TARGET":        {},
					"WARM_PREFIX_TARGET":       {},
					"ENABLE_PREFIX_DELEGATION": {},
				})
		})

		Context("when WARM_IP_TARGET = 2 and WARM_PREFIX_TARGET = 1", func() {
			BeforeEach(func() {
				warmIPTarget = 2
				minIPTarget = 0
				warmPrefixTarget = 1
			})

			It("should have 1 prefix", func() {})
		})

		Context("when WARM_IP_TARGET = 16 and WARM_PREFIX_TARGET = 0", func() {
			BeforeEach(func() {
				warmIPTarget = 16
				minIPTarget = 0
				warmPrefixTarget = 0
			})

			It("should have 1 prefix", func() {})
		})

		Context("when MINIMUM_IP_TARGET = 2 and WARM_PREFIX_TARGET = 2", func() {
			BeforeEach(func() {
				warmIPTarget = 0
				minIPTarget = 2
				warmPrefixTarget = 2
			})

			It("should have 1 prefix", func() {})
		})

		Context("when MINIMUM_IP_TARGET = 16 and WARM_PREFIX_TARGET = 0", func() {
			BeforeEach(func() {
				warmIPTarget = 0
				minIPTarget = 16
				warmPrefixTarget = 0
			})

			It("should have 1 prefixe", func() {})
		})

		Context("when MINIMUM_IP_TARGET = 6, WARM_IP_TARGET = 10 and WARM_PREFIX_TARGET = 1", func() {
			BeforeEach(func() {
				warmIPTarget = 6
				minIPTarget = 10
				warmPrefixTarget = 1
			})

			It("should have 1 prefix", func() {})
		})
	})
})

func Max(x, y int) int {
	if x < y {
		return y
	}
	return x
}

// MinIgnoreZero returns smaller of two number, if any number is zero returns the other number
func MinIgnoreZero(x, y int) int {
	if x == 0 {
		return y
	}
	if y == 0 {
		return x
	}
	if x < y {
		return x
	}
	return y
}

func ceil(x, y int) int {
	return (x + y - 1) / y
}
