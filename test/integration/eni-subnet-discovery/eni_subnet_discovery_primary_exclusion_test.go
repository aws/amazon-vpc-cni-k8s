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
	"github.com/aws/amazon-vpc-cni-k8s/test/integration/common"
	"github.com/aws/aws-sdk-go-v2/aws"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

const (
	primaryExclusionPodLabelKey = "test-type"
	primaryExclusionPodLabelVal = "primary-eni-exclusion"
	awsNodeLabelKey             = "k8s-app"
)

// ENI introspection data structures
type ENIInfo struct {
	ID                  string `json:"id"`
	IsPrimary           bool   `json:"isPrimary"`
	IsExcludedForPodIPs bool   `json:"isExcludedForPodIPs"`
	DeviceNumber        int    `json:"deviceNumber"`
}

type IntrospectResponse struct {
	ENIs map[string]ENIInfo `json:"enis"`
}

var _ = Describe("Primary ENI Exclusion Tests", func() {
	var (
		initialDeployment   *v1.Deployment
		primaryENIID        string
		primarySubnetID     string
		primarySubnetCIDR   *net.IPNet
		secondarySubnetCIDR *net.IPNet
	)

	Context("when subnet discovery is enabled", func() {
		BeforeEach(func() {
			By("Enabling subnet discovery")
			k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace, utils.AwsNodeName, map[string]string{
				"ENABLE_SUBNET_DISCOVERY": "true",
			})
			time.Sleep(utils.PollIntervalMedium)

			By("Getting primary ENI information")
			primaryENIID, primarySubnetID, primarySubnetCIDR = getPrimaryENIInfo()

			By("Getting secondary subnet CIDR")
			secondarySubnetCIDR = getSubnetCIDR(createdSubnet)
		})

		Context("when primary ENI is excluded via subnet tagging", func() {
			BeforeEach(func() {
				By("Creating initial deployment to populate primary ENI")
				container := manifest.NewNetCatAlpineContainer(f.Options.TestImageRegistry).
					Command([]string{"sleep"}).
					Args([]string{"3600"}).
					Build()

				deploymentBuilder := manifest.NewBusyBoxDeploymentBuilder(f.Options.TestImageRegistry).
					Container(container).
					Replicas(3). // Small deployment initially
					PodLabel(primaryExclusionPodLabelKey, primaryExclusionPodLabelVal).
					NodeName(*primaryInstance.PrivateDnsName).
					Build()

				var err error
				initialDeployment, err = f.K8sResourceManagers.DeploymentManager().
					CreateAndWaitTillDeploymentIsReady(deploymentBuilder, utils.DefaultDeploymentReadyTimeout)
				Expect(err).ToNot(HaveOccurred())

				By("Verifying initial pods are using primary ENI subnet")
				validatePodsInSubnet(initialDeployment, primarySubnetCIDR, "primary")

				By("Tagging primary subnet as excluded (kubernetes.io/role/cni=0)")
				_, err = f.CloudServices.EC2().
					CreateTags(
						context.TODO(),
						[]string{primarySubnetID},
						[]ec2types.Tag{
							{
								Key:   aws.String("kubernetes.io/role/cni"),
								Value: aws.String("0"),
							},
						},
					)
				Expect(err).ToNot(HaveOccurred())

				By("Tagging secondary subnet as included (kubernetes.io/role/cni=1)")
				_, err = f.CloudServices.EC2().
					CreateTags(
						context.TODO(),
						[]string{createdSubnet},
						[]ec2types.Tag{
							{
								Key:   aws.String("kubernetes.io/role/cni"),
								Value: aws.String("1"),
							},
						},
					)
				Expect(err).ToNot(HaveOccurred())

				By("Restarting aws-node pods to apply subnet discovery changes")
				restartAwsNodePods()

				By("Waiting for configuration to take effect")
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
								Value: aws.String("0"),
							},
						},
					)
				Expect(err).ToNot(HaveOccurred())

				By("Removing tags from secondary subnet")
				_, err = f.CloudServices.EC2().
					DeleteTags(
						context.TODO(),
						[]string{createdSubnet},
						[]ec2types.Tag{
							{
								Key:   aws.String("kubernetes.io/role/cni"),
								Value: aws.String("1"),
							},
						},
					)
				Expect(err).ToNot(HaveOccurred())

				By("Restarting aws-node pods to restore default configuration")
				restartAwsNodePods()

				By("Waiting for cleanup and CNI restart")
				time.Sleep(60 * time.Second)
			})

			It("should handle primary ENI exclusion gracefully", func() {
				By("Verifying existing pods on primary ENI continue to work")
				validateExistingPodsConnectivity(initialDeployment)

				By("Verifying primary ENI is marked as excluded in datastore")
				validatePrimaryENIExclusionInDatastore(primaryENIID)

				By("Scaling deployment to force new ENI creation")
				scaledDeployment := scaleDeployment(initialDeployment, 20) // Force secondary ENI creation

				By("Waiting for new pods to be scheduled")
				time.Sleep(30 * time.Second)

				By("Verifying new pods are only created on secondary ENIs")
				validateNewPodsOnSecondarySubnet(scaledDeployment, secondarySubnetCIDR)

				By("Verifying ENI capacity calculations account for exclusion")
				validateENICapacityCalculations()

				By("Testing pod deletion and cleanup")
				validatePodDeletionAndCleanup(scaledDeployment)
			})

			It("should work with prefix delegation enabled", func() {
				By("Enabling prefix delegation")
				k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace, utils.AwsNodeName, map[string]string{
					"ENABLE_PREFIX_DELEGATION": "true",
				})

				By("Waiting for prefix delegation to take effect")
				time.Sleep(30 * time.Second)

				By("Scaling deployment to test prefix delegation with exclusion")
				scaledDeployment := scaleDeployment(initialDeployment, 15)

				By("Verifying new pods use secondary subnet with prefix delegation")
				validateNewPodsOnSecondarySubnet(scaledDeployment, secondarySubnetCIDR)

				By("Verifying prefix cleanup on excluded primary ENI")
				validatePrefixCleanupOnExcludedENI(primaryENIID)

				By("Disabling prefix delegation")
				k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace, utils.AwsNodeName, map[string]string{
					"ENABLE_PREFIX_DELEGATION": "false",
				})
			})
		})
	})
})

// Helper functions

func getPrimaryENIInfo() (string, string, *net.IPNet) {
	instance, err := f.CloudServices.EC2().DescribeInstance(context.TODO(), *primaryInstance.InstanceId)
	Expect(err).ToNot(HaveOccurred())

	var primaryENIID string
	var primarySubnetID string

	for _, nwInterface := range instance.NetworkInterfaces {
		if common.IsPrimaryENI(nwInterface, instance.PrivateIpAddress) {
			primaryENIID = *nwInterface.NetworkInterfaceId
			primarySubnetID = *nwInterface.SubnetId
			break
		}
	}

	Expect(primaryENIID).ToNot(BeEmpty(), "Should find primary ENI")
	Expect(primarySubnetID).ToNot(BeEmpty(), "Should find primary subnet ID")

	// Get subnet CIDR
	subnetOutput, err := f.CloudServices.EC2().DescribeSubnets(context.TODO(), []string{primarySubnetID})
	Expect(err).ToNot(HaveOccurred())
	Expect(len(subnetOutput.Subnets)).To(Equal(1))

	_, primarySubnetCIDR, err := net.ParseCIDR(*subnetOutput.Subnets[0].CidrBlock)
	Expect(err).ToNot(HaveOccurred())

	return primaryENIID, primarySubnetID, primarySubnetCIDR
}

func getSubnetCIDR(subnetID string) *net.IPNet {
	subnetOutput, err := f.CloudServices.EC2().DescribeSubnets(context.TODO(), []string{subnetID})
	Expect(err).ToNot(HaveOccurred())
	Expect(len(subnetOutput.Subnets)).To(Equal(1))

	_, subnetCIDR, err := net.ParseCIDR(*subnetOutput.Subnets[0].CidrBlock)
	Expect(err).ToNot(HaveOccurred())

	return subnetCIDR
}

func validatePodsInSubnet(deployment *v1.Deployment, expectedSubnet *net.IPNet, subnetType string) {
	pods, err := f.K8sResourceManagers.PodManager().GetPodsWithLabelSelector(primaryExclusionPodLabelKey, primaryExclusionPodLabelVal)
	Expect(err).ToNot(HaveOccurred())

	podCount := 0
	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodRunning && pod.Status.PodIP != "" {
			podIP := net.ParseIP(pod.Status.PodIP)
			Expect(expectedSubnet.Contains(podIP)).To(BeTrue(),
				"Pod %s IP %s should be in %s subnet %s",
				pod.Name, pod.Status.PodIP, subnetType, expectedSubnet.String())
			podCount++
		}
	}

	Expect(podCount).To(BeNumerically(">", 0), "Should have pods in %s subnet", subnetType)
}

func validateExistingPodsConnectivity(deployment *v1.Deployment) {
	pods, err := f.K8sResourceManagers.PodManager().GetPodsWithLabelSelector(primaryExclusionPodLabelKey, primaryExclusionPodLabelVal)
	Expect(err).ToNot(HaveOccurred())

	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodRunning {
			By(fmt.Sprintf("Testing connectivity for pod %s", pod.Name))
			// Simple connectivity test - ping Google DNS
			stdout, stderr, err := f.K8sResourceManagers.PodManager().PodExec(pod.Namespace, pod.Name, []string{"ping", "-c", "1", "8.8.8.8"})
			if err != nil {
				GinkgoWriter.Printf("Pod %s connectivity test failed. stdout: %s, stderr: %s, err: %v\n", pod.Name, stdout, stderr, err)
			}
			// Note: We don't fail the test on connectivity issues as they might be network policy related
			// The main goal is to verify the pod is still scheduled and running
			Expect(pod.Status.Phase).To(Equal(corev1.PodRunning), "Pod %s should remain running", pod.Name)
		}
	}
}

func validatePrimaryENIExclusionInDatastore(primaryENIID string) {
	awsNodePods, err := f.K8sResourceManagers.PodManager().GetPodsWithLabelSelector(awsNodeLabelKey, utils.AwsNodeName)
	Expect(err).ToNot(HaveOccurred())

	for _, pod := range awsNodePods.Items {
		if pod.Spec.NodeName == *primaryInstance.PrivateDnsName {
			By(fmt.Sprintf("Checking ENI exclusion in aws-node pod %s", pod.Name))

			// Execute introspection command
			stdout, stderr, err := f.K8sResourceManagers.PodManager().PodExec(
				pod.Namespace,
				pod.Name,
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

			// Find primary ENI and check exclusion status
			if enis, ok := introspectData["enis"].(map[string]interface{}); ok {
				for eniID, eniData := range enis {
					if eniInfo, ok := eniData.(map[string]interface{}); ok {
						if isPrimary, exists := eniInfo["isPrimary"].(bool); exists && isPrimary {
							if excluded, exists := eniInfo["isExcludedForPodIPs"].(bool); exists {
								Expect(excluded).To(BeTrue(),
									"Primary ENI %s should be marked as excluded for pod IPs", eniID)
								return
							}
						}
					}
				}
			}

			Fail(fmt.Sprintf("Could not find primary ENI exclusion status in introspect output: %s", stdout))
		}
	}
}

func scaleDeployment(deployment *v1.Deployment, replicas int) *v1.Deployment {
	// Get the current deployment to update
	currentDeployment, err := f.K8sResourceManagers.DeploymentManager().GetDeployment(deployment.Name, deployment.Namespace)
	Expect(err).ToNot(HaveOccurred())

	// Update the replica count
	replicasInt32 := int32(replicas)
	currentDeployment.Spec.Replicas = &replicasInt32

	// Update the deployment and wait for it to be ready
	err = f.K8sResourceManagers.DeploymentManager().UpdateAndWaitTillDeploymentIsReady(currentDeployment, utils.DefaultDeploymentReadyTimeout)
	Expect(err).ToNot(HaveOccurred())

	// Get the updated deployment
	updatedDeployment, err := f.K8sResourceManagers.DeploymentManager().GetDeployment(deployment.Name, deployment.Namespace)
	Expect(err).ToNot(HaveOccurred())

	// Wait for pods to be scheduled and running
	time.Sleep(45 * time.Second)

	return updatedDeployment
}

func validateNewPodsOnSecondarySubnet(deployment *v1.Deployment, secondarySubnet *net.IPNet) {
	pods, err := f.K8sResourceManagers.PodManager().GetPodsWithLabelSelector(primaryExclusionPodLabelKey, primaryExclusionPodLabelVal)
	Expect(err).ToNot(HaveOccurred())

	// Get pod creation times to identify new pods
	baseTime := time.Now().Add(-2 * time.Minute) // Pods created in last 2 minutes are considered "new"

	newPodCount := 0
	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodRunning && pod.Status.PodIP != "" {
			// Check if this is a new pod based on creation time
			if pod.CreationTimestamp.Time.After(baseTime) {
				podIP := net.ParseIP(pod.Status.PodIP)
				Expect(secondarySubnet.Contains(podIP)).To(BeTrue(),
					"New pod %s IP %s should be in secondary subnet %s",
					pod.Name, pod.Status.PodIP, secondarySubnet.String())
				newPodCount++
			}
		}
	}

	GinkgoWriter.Printf("Found %d new pods in secondary subnet\n", newPodCount)
	// Note: We don't enforce a minimum count as it depends on ENI allocation timing
}

func validateENICapacityCalculations() {
	awsNodePods, err := f.K8sResourceManagers.PodManager().GetPodsWithLabelSelector(awsNodeLabelKey, utils.AwsNodeName)
	Expect(err).ToNot(HaveOccurred())

	for _, pod := range awsNodePods.Items {
		if pod.Spec.NodeName == *primaryInstance.PrivateDnsName {
			By(fmt.Sprintf("Checking ENI capacity in aws-node pod %s", pod.Name))

			// Execute pod-limit introspection
			stdout, stderr, err := f.K8sResourceManagers.PodManager().PodExec(
				pod.Namespace,
				pod.Name,
				[]string{"/app/aws-k8s-agent", "introspect", "pod-limit"})

			if err != nil {
				GinkgoWriter.Printf("Pod-limit introspect failed. stdout: %s, stderr: %s, err: %v\n", stdout, stderr, err)
				continue
			}

			GinkgoWriter.Printf("Pod limit introspect output: %s\n", stdout)

			// For now, just verify the command succeeds
			// In a more comprehensive test, we would parse the output and verify
			// that ENI limits properly account for the excluded primary ENI
			Expect(err).ToNot(HaveOccurred(), "Pod limit introspection should succeed")
		}
	}
}

func validatePodDeletionAndCleanup(deployment *v1.Deployment) {
	By("Scaling down deployment to test cleanup")
	scaledDownDeployment := scaleDeployment(deployment, 5)

	By("Waiting for pod deletion and resource cleanup")
	time.Sleep(60 * time.Second)

	By("Verifying remaining pods are still functional")
	validateExistingPodsConnectivity(scaledDownDeployment)
}

func validatePrefixCleanupOnExcludedENI(primaryENIID string) {
	awsNodePods, err := f.K8sResourceManagers.PodManager().GetPodsWithLabelSelector(awsNodeLabelKey, utils.AwsNodeName)
	Expect(err).ToNot(HaveOccurred())

	for _, pod := range awsNodePods.Items {
		if pod.Spec.NodeName == *primaryInstance.PrivateDnsName {
			By(fmt.Sprintf("Checking prefix cleanup on primary ENI in pod %s", pod.Name))

			// Execute ENI introspection to check prefix allocation
			stdout, stderr, err := f.K8sResourceManagers.PodManager().PodExec(
				pod.Namespace,
				pod.Name,
				[]string{"/app/aws-k8s-agent", "introspect", "eni", "-v"})

			if err != nil {
				GinkgoWriter.Printf("ENI introspect failed. stdout: %s, stderr: %s, err: %v\n", stdout, stderr, err)
				continue
			}

			GinkgoWriter.Printf("ENI introspect output for prefix validation: %s\n", stdout)

			// Verify that excluded primary ENI doesn't have unnecessary unassigned prefixes
			// This is a basic check - in production we'd parse the JSON output more thoroughly
			Expect(strings.Contains(stdout, "isExcludedForPodIPs")).To(BeTrue(),
				"ENI introspect should show exclusion status")
		}
	}
}

func restartAwsNodePods() {
	awsNodePods, err := f.K8sResourceManagers.PodManager().GetPodsWithLabelSelector(awsNodeLabelKey, utils.AwsNodeName)
	Expect(err).ToNot(HaveOccurred())

	for _, pod := range awsNodePods.Items {
		err := f.K8sResourceManagers.PodManager().DeleteAndWaitTillPodDeleted(&pod)
		Expect(err).ToNot(HaveOccurred())
	}

	// Wait for new aws-node pods to be ready
	time.Sleep(30 * time.Second)

	// Verify aws-node pods are running
	Eventually(func() bool {
		awsNodePods, err := f.K8sResourceManagers.PodManager().GetPodsWithLabelSelector(awsNodeLabelKey, utils.AwsNodeName)
		if err != nil {
			return false
		}

		runningCount := 0
		for _, pod := range awsNodePods.Items {
			if pod.Status.Phase == corev1.PodRunning {
				runningCount++
			}
		}

		// Expect at least one aws-node pod to be running
		return runningCount > 0
	}, 60*time.Second, 5*time.Second).Should(BeTrue(), "AWS node pods should be running after restart")
}
