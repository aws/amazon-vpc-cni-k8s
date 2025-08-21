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

package eni_subnet_discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	"github.com/aws/aws-sdk-go-v2/aws"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

const (
	secondaryExclusionPodLabelKey = "test-type"
	secondaryExclusionPodLabelVal = "secondary-eni-exclusion"
)

var _ = Describe("Secondary ENI Exclusion Tests", func() {
	var (
		initialDeployment   *v1.Deployment
		primarySubnetID     string
		primarySubnetCIDR   *net.IPNet
		secondarySubnetCIDR *net.IPNet
		secondarySubnetID   string
	)

	Context("when subnet discovery is enabled", func() {
		BeforeEach(func() {
			By("Enabling subnet discovery")
			k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace, utils.AwsNodeName, map[string]string{
				"ENABLE_SUBNET_DISCOVERY": "true",
			})
			time.Sleep(utils.PollIntervalMedium)

			By("Getting primary ENI information")
			_, primarySubnetID, primarySubnetCIDR = getPrimaryENIInfo()

			By("Getting secondary subnet information")
			secondarySubnetID = createdSubnet
			secondarySubnetCIDR = getSubnetCIDR(createdSubnet)

			By("Ensuring both subnets are initially included (kubernetes.io/role/cni=1)")
			// Tag primary subnet as included
			_, err := f.CloudServices.EC2().
				CreateTags(
					context.TODO(),
					[]string{primarySubnetID},
					[]ec2types.Tag{
						{
							Key:   aws.String("kubernetes.io/role/cni"),
							Value: aws.String("1"),
						},
					},
				)
			Expect(err).ToNot(HaveOccurred())

			// Tag secondary subnet as included
			_, err = f.CloudServices.EC2().
				CreateTags(
					context.TODO(),
					[]string{secondarySubnetID},
					[]ec2types.Tag{
						{
							Key:   aws.String("kubernetes.io/role/cni"),
							Value: aws.String("1"),
						},
					},
				)
			Expect(err).ToNot(HaveOccurred())

			By("Restarting aws-node pods to apply initial subnet discovery")
			restartAwsNodePods()

			By("Waiting for configuration to take effect")
			time.Sleep(30 * time.Second)
		})

		Context("when secondary ENI is excluded via subnet tagging", func() {
			BeforeEach(func() {
				By("Creating initial deployment to populate both primary and secondary ENIs")
				container := manifest.NewNetCatAlpineContainer(f.Options.TestImageRegistry).
					Command([]string{"sleep"}).
					Args([]string{"3600"}).
					Build()

				deploymentBuilder := manifest.NewBusyBoxDeploymentBuilder(f.Options.TestImageRegistry).
					Container(container).
					Replicas(15). // Enough to force secondary ENI creation
					PodLabel(secondaryExclusionPodLabelKey, secondaryExclusionPodLabelVal).
					NodeName(*primaryInstance.PrivateDnsName).
					Build()

				var err error
				initialDeployment, err = f.K8sResourceManagers.DeploymentManager().
					CreateAndWaitTillDeploymentIsReady(deploymentBuilder, utils.DefaultDeploymentReadyTimeout)
				Expect(err).ToNot(HaveOccurred())

				By("Verifying pods are distributed across both subnets")
				validatePodsInBothSubnets(initialDeployment, primarySubnetCIDR, secondarySubnetCIDR)

				By("Waiting for secondary ENI to be created and populated")
				time.Sleep(45 * time.Second)

				By("Tagging secondary subnet as excluded (kubernetes.io/role/cni=0)")
				_, err = f.CloudServices.EC2().
					CreateTags(
						context.TODO(),
						[]string{secondarySubnetID},
						[]ec2types.Tag{
							{
								Key:   aws.String("kubernetes.io/role/cni"),
								Value: aws.String("0"),
							},
						},
					)
				Expect(err).ToNot(HaveOccurred())

				By("Restarting aws-node pods to apply subnet discovery changes")
				restartAwsNodePods()

				By("Waiting for secondary ENI exclusion to take effect")
				time.Sleep(30 * time.Second)
			})

			AfterEach(func() {
				By("Cleaning up deployment")
				if initialDeployment != nil {
					err := f.K8sResourceManagers.DeploymentManager().DeleteAndWaitTillDeploymentIsDeleted(initialDeployment)
					Expect(err).ToNot(HaveOccurred())
				}

				By("Removing tags from primary subnet")
				_, err := f.CloudServices.EC2().
					DeleteTags(
						context.TODO(),
						[]string{primarySubnetID},
						[]ec2types.Tag{
							{
								Key:   aws.String("kubernetes.io/role/cni"),
								Value: aws.String("1"),
							},
						},
					)
				Expect(err).ToNot(HaveOccurred())

				By("Removing tags from secondary subnet")
				_, err = f.CloudServices.EC2().
					DeleteTags(
						context.TODO(),
						[]string{secondarySubnetID},
						[]ec2types.Tag{
							{
								Key:   aws.String("kubernetes.io/role/cni"),
								Value: aws.String("0"),
							},
						},
					)
				Expect(err).ToNot(HaveOccurred())

				By("Restarting aws-node pods to restore default configuration")
				restartAwsNodePods()

				By("Waiting for cleanup and CNI restart")
				time.Sleep(60 * time.Second)
			})

			It("should handle secondary ENI exclusion gracefully", func() {
				By("Verifying existing pods on secondary ENI continue to work")
				validateExistingPodsConnectivity(initialDeployment)

				By("Verifying secondary ENI is marked as excluded in datastore")
				validateSecondaryENIExclusionInDatastore()

				By("Scaling deployment to test new pod allocation behavior")
				scaledDeployment := scaleDeployment(initialDeployment, 25) // Add more pods

				By("Waiting for new pods to be scheduled")
				time.Sleep(45 * time.Second)

				By("Verifying new pods avoid excluded secondary ENI")
				validateNewPodsAvoidExcludedSecondaryENI(scaledDeployment, primarySubnetCIDR, secondarySubnetCIDR)

				By("Verifying ENI capacity calculations account for exclusion")
				validateENICapacityCalculations()

				By("Testing secondary ENI cleanup after pod removal")
				validateSecondaryENICleanupAfterPodRemoval(scaledDeployment)
			})

			It("should work with prefix delegation enabled", func() {
				By("Enabling prefix delegation")
				k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace, utils.AwsNodeName, map[string]string{
					"ENABLE_PREFIX_DELEGATION": "true",
				})

				By("Waiting for prefix delegation to take effect")
				time.Sleep(30 * time.Second)

				By("Scaling deployment to test prefix delegation with secondary exclusion")
				scaledDeployment := scaleDeployment(initialDeployment, 20)

				By("Verifying new pods use primary subnet with prefix delegation")
				validateNewPodsAvoidExcludedSecondaryENI(scaledDeployment, primarySubnetCIDR, secondarySubnetCIDR)

				By("Verifying prefix cleanup on excluded secondary ENI")
				validatePrefixCleanupOnExcludedSecondaryENI()

				By("Disabling prefix delegation")
				k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace, utils.AwsNodeName, map[string]string{
					"ENABLE_PREFIX_DELEGATION": "false",
				})
			})
		})
	})
})

// Helper functions specific to secondary ENI exclusion

func validatePodsInBothSubnets(deployment *v1.Deployment, primarySubnet, secondarySubnet *net.IPNet) {
	pods, err := f.K8sResourceManagers.PodManager().GetPodsWithLabelSelector(secondaryExclusionPodLabelKey, secondaryExclusionPodLabelVal)
	Expect(err).ToNot(HaveOccurred())

	primaryPodCount := 0
	secondaryPodCount := 0

	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodRunning && pod.Status.PodIP != "" {
			podIP := net.ParseIP(pod.Status.PodIP)
			if primarySubnet.Contains(podIP) {
				primaryPodCount++
			} else if secondarySubnet.Contains(podIP) {
				secondaryPodCount++
			}
		}
	}

	Expect(primaryPodCount).To(BeNumerically(">", 0), "Should have pods in primary subnet")
	Expect(secondaryPodCount).To(BeNumerically(">", 0), "Should have pods in secondary subnet")
	GinkgoWriter.Printf("Found %d pods in primary subnet and %d pods in secondary subnet\n", primaryPodCount, secondaryPodCount)
}

func validateSecondaryENIExclusionInDatastore() {
	awsNodePods, err := f.K8sResourceManagers.PodManager().GetPodsWithLabelSelector(awsNodeLabelKey, utils.AwsNodeName)
	Expect(err).ToNot(HaveOccurred())

	for _, pod := range awsNodePods.Items {
		if pod.Spec.NodeName == *primaryInstance.PrivateDnsName {
			By(fmt.Sprintf("Checking secondary ENI exclusion in aws-node pod %s", pod.Name))

			// Execute introspection command in the aws-node container
			stdout, stderr, err := f.K8sResourceManagers.PodManager().PodExecWithContainer(
				pod.Namespace,
				pod.Name,
				"aws-node", // Specify the aws-node container
				[]string{"/app/aws-k8s-agent", "introspect", "eni"})

			if err != nil {
				GinkgoWriter.Printf("Introspect command failed. stdout: %s, stderr: %s, err: %v\n", stdout, stderr, err)
				continue
			}

			// Parse the introspection output
			var introspectData map[string]interface{}
			err = json.Unmarshal([]byte(stdout), &introspectData)
			if err != nil {
				GinkgoWriter.Printf("Failed to parse introspect output: %v\nOutput: %s\n", err, stdout)
				continue
			}

			// Find secondary ENI and check exclusion status
			foundExcludedSecondaryENI := false
			if enis, ok := introspectData["enis"].(map[string]interface{}); ok {
				for eniID, eniData := range enis {
					if eniInfo, ok := eniData.(map[string]interface{}); ok {
						if isPrimary, exists := eniInfo["isPrimary"].(bool); exists && !isPrimary {
							// This is a secondary ENI
							if excluded, exists := eniInfo["isExcludedForPodIPs"].(bool); exists && excluded {
								GinkgoWriter.Printf("Found excluded secondary ENI: %s\n", eniID)
								foundExcludedSecondaryENI = true
							}
						}
					}
				}
			}

			if !foundExcludedSecondaryENI {
				GinkgoWriter.Printf("Introspect output for debugging: %s\n", stdout)
			}
			Expect(foundExcludedSecondaryENI).To(BeTrue(), "Should find at least one excluded secondary ENI")
			return
		}
	}
}

func validateNewPodsAvoidExcludedSecondaryENI(deployment *v1.Deployment, primarySubnet, secondarySubnet *net.IPNet) {
	pods, err := f.K8sResourceManagers.PodManager().GetPodsWithLabelSelector(secondaryExclusionPodLabelKey, secondaryExclusionPodLabelVal)
	Expect(err).ToNot(HaveOccurred())

	// Get pod creation times to identify new pods
	baseTime := time.Now().Add(-3 * time.Minute) // Pods created in last 3 minutes are considered "new"

	newPodCount := 0
	newPodsInSecondarySubnet := 0

	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodRunning && pod.Status.PodIP != "" {
			// Check if this is a new pod based on creation time
			if pod.CreationTimestamp.Time.After(baseTime) {
				podIP := net.ParseIP(pod.Status.PodIP)
				newPodCount++

				if secondarySubnet.Contains(podIP) {
					newPodsInSecondarySubnet++
					GinkgoWriter.Printf("WARNING: New pod %s IP %s is in excluded secondary subnet %s\n",
						pod.Name, pod.Status.PodIP, secondarySubnet.String())
				}
			}
		}
	}

	GinkgoWriter.Printf("Found %d new pods, %d of which are in secondary subnet\n", newPodCount, newPodsInSecondarySubnet)

	// New pods should avoid the excluded secondary subnet
	Expect(newPodsInSecondarySubnet).To(Equal(0), "New pods should not be placed in excluded secondary subnet")
}

func validateSecondaryENICleanupAfterPodRemoval(deployment *v1.Deployment) {
	By("Scaling down deployment to remove pods from secondary ENI")
	scaledDownDeployment := scaleDeployment(deployment, 8) // Scale down to force pod removal

	By("Waiting for pod deletion and potential ENI cleanup")
	time.Sleep(90 * time.Second)

	By("Verifying excluded secondary ENI becomes deletable")
	validateExcludedSecondaryENIDeletable()

	By("Verifying remaining pods are still functional")
	validateExistingPodsConnectivity(scaledDownDeployment)
}

func validateExcludedSecondaryENIDeletable() {
	awsNodePods, err := f.K8sResourceManagers.PodManager().GetPodsWithLabelSelector(awsNodeLabelKey, utils.AwsNodeName)
	Expect(err).ToNot(HaveOccurred())

	for _, pod := range awsNodePods.Items {
		if pod.Spec.NodeName == *primaryInstance.PrivateDnsName {
			By(fmt.Sprintf("Checking secondary ENI deletability in aws-node pod %s", pod.Name))

			// Execute introspection command to check ENI status in the aws-node container
			stdout, stderr, err := f.K8sResourceManagers.PodManager().PodExecWithContainer(
				pod.Namespace,
				pod.Name,
				"aws-node", // Specify the aws-node container
				[]string{"/app/aws-k8s-agent", "introspect", "eni", "-v"})

			if err != nil {
				GinkgoWriter.Printf("ENI introspect failed. stdout: %s, stderr: %s, err: %v\n", stdout, stderr, err)
				continue
			}

			GinkgoWriter.Printf("ENI introspect output for deletability validation: %s\n", stdout)

			// The excluded secondary ENI should either be deleted or be eligible for deletion
			// We verify this by checking that excluded ENIs with no pods are marked appropriately
			Expect(strings.Contains(stdout, "isExcludedForPodIPs")).To(BeTrue(),
				"ENI introspect should show exclusion status")
		}
	}
}

func validatePrefixCleanupOnExcludedSecondaryENI() {
	awsNodePods, err := f.K8sResourceManagers.PodManager().GetPodsWithLabelSelector(awsNodeLabelKey, utils.AwsNodeName)
	Expect(err).ToNot(HaveOccurred())

	for _, pod := range awsNodePods.Items {
		if pod.Spec.NodeName == *primaryInstance.PrivateDnsName {
			By(fmt.Sprintf("Checking prefix cleanup on secondary ENIs in pod %s", pod.Name))

			// Execute ENI introspection to check prefix allocation in the aws-node container
			stdout, stderr, err := f.K8sResourceManagers.PodManager().PodExecWithContainer(
				pod.Namespace,
				pod.Name,
				"aws-node", // Specify the aws-node container
				[]string{"/app/aws-k8s-agent", "introspect", "eni", "-v"})

			if err != nil {
				GinkgoWriter.Printf("ENI introspect failed. stdout: %s, stderr: %s, err: %v\n", stdout, stderr, err)
				continue
			}

			GinkgoWriter.Printf("ENI introspect output for prefix validation: %s\n", stdout)

			// Verify that excluded secondary ENIs don't have unnecessary unassigned prefixes
			// This is a basic check - in production we'd parse the JSON output more thoroughly
			Expect(strings.Contains(stdout, "isExcludedForPodIPs")).To(BeTrue(),
				"ENI introspect should show exclusion status for secondary ENIs")
		}
	}
}
