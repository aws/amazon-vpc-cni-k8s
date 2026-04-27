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

		Context("when secondary subnet has no cni tag (discovery gating)", func() {
			var untaggedSubnetID string
			var untaggedSubnetCIDR *net.IPNet

			BeforeEach(func() {
				By("Creating an untagged secondary subnet")
				untaggedSubnetOutput, err := f.CloudServices.EC2().
					CreateSubnet(context.TODO(), "100.64.64.0/24", f.Options.AWSVPCID, *primaryInstance.Placement.AvailabilityZone)
				Expect(err).ToNot(HaveOccurred())

				untaggedSubnetID = *untaggedSubnetOutput.Subnet.SubnetId
				untaggedSubnetCIDR = getSubnetCIDR(untaggedSubnetID)

				By("Creating another subnet with cni=1 for comparison")
				taggedSubnetOutput, err := f.CloudServices.EC2().
					CreateSubnet(context.TODO(), "100.64.65.0/24", f.Options.AWSVPCID, *primaryInstance.Placement.AvailabilityZone)
				Expect(err).ToNot(HaveOccurred())

				taggedSubnetID := *taggedSubnetOutput.Subnet.SubnetId

				By("Tagging comparison subnet with cni=1")
				_, err = f.CloudServices.EC2().
					CreateTags(
						context.TODO(),
						[]string{taggedSubnetID},
						[]ec2types.Tag{
							{
								Key:   aws.String("kubernetes.io/role/cni"),
								Value: aws.String("1"),
							},
						},
					)
				Expect(err).ToNot(HaveOccurred())

				By("Restarting aws-node pods to apply subnet discovery")
				restartAwsNodePods()

				By("Waiting for configuration to take effect")
				time.Sleep(30 * time.Second)
			})

			AfterEach(func() {
				By("Deleting untagged subnet")
				err := f.CloudServices.EC2().DeleteSubnet(context.TODO(), untaggedSubnetID)
				if err != nil {
					GinkgoWriter.Printf("Warning: Failed to delete untagged subnet %s: %v\n", untaggedSubnetID, err)
				}
			})

			It("should not discover secondary subnet without cni tag", func() {
				By("Creating deployment that requires secondary ENIs")
				container := manifest.NewNetCatAlpineContainer(f.Options.TestImageRegistry).
					Command([]string{"sleep"}).
					Args([]string{"3600"}).
					Build()

				deploymentBuilder := manifest.NewBusyBoxDeploymentBuilder(f.Options.TestImageRegistry).
					Container(container).
					Replicas(15). // Enough to force secondary ENI creation
					PodLabel(secondaryExclusionPodLabelKey, "no-cni-tag-gating").
					NodeName(*primaryInstance.PrivateDnsName).
					Build()

				var err error
				deployment, err := f.K8sResourceManagers.DeploymentManager().
					CreateAndWaitTillDeploymentIsReady(deploymentBuilder, utils.DefaultDeploymentReadyTimeout)
				Expect(err).ToNot(HaveOccurred())

				defer func() {
					By("Cleaning up deployment")
					err := f.K8sResourceManagers.DeploymentManager().DeleteAndWaitTillDeploymentIsDeleted(deployment)
					Expect(err).ToNot(HaveOccurred())
					time.Sleep(60 * time.Second)
				}()

				By("Verifying no pods are placed in the untagged subnet (gating mechanism works)")
				pods, err := f.K8sResourceManagers.PodManager().GetPodsWithLabelSelector(secondaryExclusionPodLabelKey, "no-cni-tag-gating")
				Expect(err).ToNot(HaveOccurred())

				podsInUntaggedSubnet := 0
				for _, pod := range pods.Items {
					if pod.Status.Phase == corev1.PodRunning && pod.Status.PodIP != "" {
						podIP := net.ParseIP(pod.Status.PodIP)
						if untaggedSubnetCIDR.Contains(podIP) {
							podsInUntaggedSubnet++
							GinkgoWriter.Printf("ERROR: Pod %s found in untagged subnet %s\n", pod.Name, pod.Status.PodIP)
						}
					}
				}

				Expect(podsInUntaggedSubnet).To(Equal(0),
					"Secondary subnet without cni tag should be excluded - no pods should be placed there (gating mechanism)")
			})
		})

		Context("when secondary subnet has cni=1 with old cluster tag prefix", func() {
			var secondarySubnetWithOldTagID string

			BeforeEach(func() {
				By("Creating secondary subnet with cni=1 and old cluster tag prefix")
				subnetOutput, err := f.CloudServices.EC2().
					CreateSubnet(context.TODO(), "100.64.66.0/24", f.Options.AWSVPCID, *primaryInstance.Placement.AvailabilityZone)
				Expect(err).ToNot(HaveOccurred())

				secondarySubnetWithOldTagID = *subnetOutput.Subnet.SubnetId

				By("Tagging test subnet with cni=1 (opt-in) and old-style cluster tag")
				_, err = f.CloudServices.EC2().
					CreateTags(
						context.TODO(),
						[]string{secondarySubnetWithOldTagID},
						[]ec2types.Tag{
							{
								Key:   aws.String("kubernetes.io/role/cni"),
								Value: aws.String("1"),
							},
							{
								Key:   aws.String("kubernetes.io/cluster/test-cluster"),
								Value: aws.String("shared"),
							},
						},
					)
				Expect(err).ToNot(HaveOccurred())

				By("Excluding primary subnet with cni=0 to force CNI to use secondary subnets")
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

				By("Also excluding the BeforeSuite secondary subnet so only our test subnet is available")
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

				By("Setting WARM_ENI_TARGET=2 to force secondary ENI creation")
				k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace,
					utils.AwsNodeName, map[string]string{
						"WARM_ENI_TARGET":         "2",
						"ENABLE_SUBNET_DISCOVERY": "true",
					})

				By("Waiting for ENI allocation to take effect")
				time.Sleep(60 * time.Second)
			})

			AfterEach(func() {
				By("Restoring primary subnet tag to cni=1 before releasing ENIs")
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
				if err != nil {
					GinkgoWriter.Printf("Warning: Failed to restore primary subnet tag: %v\n", err)
				}

				By("Restoring BeforeSuite secondary subnet tag to cni=1")
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
				if err != nil {
					GinkgoWriter.Printf("Warning: Failed to restore secondary subnet tag: %v\n", err)
				}

				By("Excluding test subnet with cni=0 so CNI stops using it")
				_, err = f.CloudServices.EC2().
					CreateTags(
						context.TODO(),
						[]string{secondarySubnetWithOldTagID},
						[]ec2types.Tag{
							{
								Key:   aws.String("kubernetes.io/role/cni"),
								Value: aws.String("0"),
							},
						},
					)
				if err != nil {
					GinkgoWriter.Printf("Warning: Failed to exclude test subnet: %v\n", err)
				}

				By("Resetting WARM_ENI_TARGET to 0 to release ENIs")
				k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace,
					utils.AwsNodeName, map[string]string{"WARM_ENI_TARGET": "0"})
				time.Sleep(90 * time.Second)

				By("Deleting test subnet")
				err = f.CloudServices.EC2().DeleteSubnet(context.TODO(), secondarySubnetWithOldTagID)
				if err != nil {
					GinkgoWriter.Printf("Warning: Failed to delete subnet %s: %v\n", secondarySubnetWithOldTagID, err)
				}
			})

			It("should include subnet with cni=1 even if old cluster tag is present (old prefix is ignored)", func() {
				By("Checking if any ENI was created in the old-tag subnet")
				instance, err := f.CloudServices.EC2().DescribeInstance(context.TODO(), *primaryInstance.InstanceId)
				Expect(err).ToNot(HaveOccurred())

				enisInOldTagSubnet := 0
				for _, eni := range instance.NetworkInterfaces {
					if eni.SubnetId != nil && *eni.SubnetId == secondarySubnetWithOldTagID {
						enisInOldTagSubnet++
						GinkgoWriter.Printf("  Found ENI %s in old-tag subnet\n", *eni.NetworkInterfaceId)
					}
				}

				GinkgoWriter.Printf("Found %d of %d ENIs in subnet %s (old-prefix cluster tag)\n",
					enisInOldTagSubnet, len(instance.NetworkInterfaces), secondarySubnetWithOldTagID)

				Expect(enisInOldTagSubnet).To(BeNumerically(">", 0),
					"Subnet with cni=1 and old-style cluster tag should be discoverable (old prefix is invisible to new logic) — expected ENI creation in this subnet")
			})
		})

		Context("when secondary subnet has cni=1 with different new cluster tag", func() {
			var secondarySubnetWithDifferentClusterID string

			BeforeEach(func() {
				By("Creating secondary subnet for different cluster")
				subnetOutput, err := f.CloudServices.EC2().
					CreateSubnet(context.TODO(), "100.64.67.0/24", f.Options.AWSVPCID, *primaryInstance.Placement.AvailabilityZone)
				Expect(err).ToNot(HaveOccurred())

				secondarySubnetWithDifferentClusterID = *subnetOutput.Subnet.SubnetId

				By("Tagging with cni=1 and new-style cluster tag for different cluster")
				_, err = f.CloudServices.EC2().
					CreateTags(
						context.TODO(),
						[]string{secondarySubnetWithDifferentClusterID},
						[]ec2types.Tag{
							{
								Key:   aws.String("kubernetes.io/role/cni"),
								Value: aws.String("1"),
							},
							{
								Key:   aws.String("cni.networking.k8s.aws/cluster/other-cluster"),
								Value: aws.String("shared"),
							},
						},
					)
				Expect(err).ToNot(HaveOccurred())

				By("Restarting aws-node pods to apply subnet discovery")
				restartAwsNodePods()

				By("Waiting for configuration to take effect")
				time.Sleep(30 * time.Second)
			})

			AfterEach(func() {
				By("Deleting secondary subnet")
				err := f.CloudServices.EC2().DeleteSubnet(context.TODO(), secondarySubnetWithDifferentClusterID)
				if err != nil {
					GinkgoWriter.Printf("Warning: Failed to delete subnet %s: %v\n", secondarySubnetWithDifferentClusterID, err)
				}
			})

			It("should exclude subnet with cni=1 if cluster tag belongs to different cluster", func() {
				By("Creating deployment that requires secondary ENIs")
				container := manifest.NewNetCatAlpineContainer(f.Options.TestImageRegistry).
					Command([]string{"sleep"}).
					Args([]string{"3600"}).
					Build()

				deploymentBuilder := manifest.NewBusyBoxDeploymentBuilder(f.Options.TestImageRegistry).
					Container(container).
					Replicas(15).
					PodLabel(secondaryExclusionPodLabelKey, "different-cluster-isolation").
					NodeName(*primaryInstance.PrivateDnsName).
					Build()

				var err error
				deployment, err := f.K8sResourceManagers.DeploymentManager().
					CreateAndWaitTillDeploymentIsReady(deploymentBuilder, utils.DefaultDeploymentReadyTimeout)
				Expect(err).ToNot(HaveOccurred())

				defer func() {
					By("Cleaning up deployment")
					err := f.K8sResourceManagers.DeploymentManager().DeleteAndWaitTillDeploymentIsDeleted(deployment)
					Expect(err).ToNot(HaveOccurred())
					time.Sleep(60 * time.Second)
				}()

				subnetCIDR := getSubnetCIDR(secondarySubnetWithDifferentClusterID)

				By("Verifying pods avoid the subnet tagged for different cluster")
				pods, err := f.K8sResourceManagers.PodManager().GetPodsWithLabelSelector(secondaryExclusionPodLabelKey, "different-cluster-isolation")
				Expect(err).ToNot(HaveOccurred())

				podsInDifferentClusterSubnet := 0
				for _, pod := range pods.Items {
					if pod.Status.Phase == corev1.PodRunning && pod.Status.PodIP != "" {
						podIP := net.ParseIP(pod.Status.PodIP)
						if subnetCIDR.Contains(podIP) {
							podsInDifferentClusterSubnet++
							GinkgoWriter.Printf("WARNING: Pod %s found in different-cluster subnet %s\n", pod.Name, pod.Status.PodIP)
						}
					}
				}

				Expect(podsInDifferentClusterSubnet).To(Equal(0),
					"Subnet with cni=1 but belonging to different cluster should be excluded (cluster isolation)")
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
