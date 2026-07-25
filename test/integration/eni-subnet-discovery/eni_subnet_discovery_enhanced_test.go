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
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/apparentlymart/go-cidr/cidr"
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
	enhancedPodLabelKey = "test-type"
	enhancedPodLabelVal = "eni-subnet-enhanced"
)

var primarySubnetID string

// This file contains additional tests for the enhanced subnet discovery functionality
// including primary subnet exclusion and cluster-specific tags

var _ = Describe("ENI Subnet Discovery Enhanced Tests", func() {
	var (
		deployment *v1.Deployment
	)

	Context("when subnet discovery is enabled", func() {
		BeforeEach(func() {
			k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace, utils.AwsNodeName, map[string]string{
				"ENABLE_SUBNET_DISCOVERY": "true",
			})
			time.Sleep(utils.PollIntervalMedium)

			// Get primary subnet ID
			primarySubnetID = *primaryInstance.SubnetId
		})

		Context("when primary subnet is excluded with tag value 0", func() {
			BeforeEach(func() {
				By("Tagging primary subnet with kubernetes.io/role/cni=0")
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

				By("Tagging secondary subnet with kubernetes.io/role/cni=1")
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
			})

			AfterEach(func() {
				By("Removing tags from primary subnet")
				_, err = f.CloudServices.EC2().
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
			})

			It("should create ENIs only in secondary subnet", func() {
				By("creating deployment")
				container := manifest.NewNetCatAlpineContainer(f.Options.TestImageRegistry).
					Command([]string{"sleep"}).
					Args([]string{"3600"}).
					Build()

				deploymentBuilder := manifest.NewBusyBoxDeploymentBuilder(f.Options.TestImageRegistry).
					Container(container).
					Replicas(computeReplicasForBothSubnets(string(primaryInstance.InstanceType))). // Overflow past one ENI so a secondary ENI is forced; it must land in the discovered subnet.
					PodLabel(enhancedPodLabelKey, enhancedPodLabelVal).
					NodeName(*primaryInstance.PrivateDnsName).
					Build()

				deployment, err = f.K8sResourceManagers.DeploymentManager().
					CreateAndWaitTillDeploymentIsReady(deploymentBuilder, utils.DefaultDeploymentReadyTimeout)
				Expect(err).ToNot(HaveOccurred())

				// Allow deployment to stabilize
				time.Sleep(10 * time.Second)

				By("verifying all secondary ENIs are in the secondary subnet")
				instance, err := f.CloudServices.EC2().DescribeInstance(context.TODO(), *primaryInstance.InstanceId)
				Expect(err).ToNot(HaveOccurred())

				secondaryENICount := 0
				for _, nwInterface := range instance.NetworkInterfaces {
					if !common.IsPrimaryENI(nwInterface, instance.PrivateIpAddress) {
						secondaryENICount++
						// All secondary ENIs should be in the secondary subnet
						Expect(*nwInterface.SubnetId).To(Equal(createdSubnet))
						Expect(*nwInterface.SubnetId).ToNot(Equal(primarySubnetID))
					}
				}

				By("verifying at least one secondary ENI was created")
				Expect(secondaryENICount).To(BeNumerically(">", 0))

				By("deleting deployment")
				err = f.K8sResourceManagers.DeploymentManager().DeleteAndWaitTillDeploymentIsDeleted(deployment)
				Expect(err).ToNot(HaveOccurred())

				By("sleeping to allow CNI Plugin to delete unused ENIs")
				time.Sleep(time.Second * 90)
			})
		})

		Context("when using cluster-specific subnet tags", func() {
			var clusterName string
			var otherClusterSubnetID string

			BeforeEach(func() {
				// Get the cluster name from environment or use a default
				clusterName = os.Getenv("CLUSTER_NAME")
				if clusterName == "" {
					Skip("CLUSTER_NAME environment variable not set, skipping cluster-specific tag test")
				}

				By("Tagging secondary subnet with cluster-specific tag")
				_, err = f.CloudServices.EC2().
					CreateTags(
						context.TODO(),
						[]string{createdSubnet},
						[]ec2types.Tag{
							{
								Key:   aws.String("cni.networking.k8s.aws/cluster/" + clusterName),
								Value: aws.String("shared"),
							},
							{
								Key:   aws.String("kubernetes.io/role/cni"),
								Value: aws.String("1"),
							},
						},
					)
				Expect(err).ToNot(HaveOccurred())

				// Create another subnet that has a different cluster tag
				By("Creating a subnet for a different cluster")
				var subnetCidr *net.IPNet
				if useIPv6 {
					// For IPv6, calculate the appropriate number of bits to get a /64 subnet
					prefixLen, _ := cidrRange.Mask.Size()
					if prefixLen > 64 {
						Fail(fmt.Sprintf("IPv6 parent CIDR prefix length must be <= 64, got /%d", prefixLen))
					}
					// Calculate how many bits we need to extend to reach /64
					bitsToExtend := 64 - prefixLen
					subnetCidr, err = cidr.Subnet(cidrRange, bitsToExtend, 1) // Use index 1 for different subnet
					Expect(err).ToNot(HaveOccurred())
				} else {
					subnetCidr, err = cidr.Subnet(cidrRange, 2, 1) // Use a different subnet
					Expect(err).ToNot(HaveOccurred())
				}

				otherSubnetOutput, err := f.CloudServices.EC2().
					CreateSubnet(context.TODO(), subnetCidr.String(), f.Options.AWSVPCID, *primaryInstance.Placement.AvailabilityZone)
				Expect(err).ToNot(HaveOccurred())

				otherClusterSubnetID = *otherSubnetOutput.Subnet.SubnetId

				By("Tagging other subnet with different cluster tag")
				_, err = f.CloudServices.EC2().
					CreateTags(
						context.TODO(),
						[]string{otherClusterSubnetID},
						[]ec2types.Tag{
							{
								Key:   aws.String("cni.networking.k8s.aws/cluster/different-cluster"),
								Value: aws.String("shared"),
							},
							{
								Key:   aws.String("kubernetes.io/role/cni"),
								Value: aws.String("1"),
							},
						},
					)
				Expect(err).ToNot(HaveOccurred())
			})

			AfterEach(func() {
				By("Removing tags from secondary subnet")
				_, err = f.CloudServices.EC2().
					DeleteTags(
						context.TODO(),
						[]string{createdSubnet},
						[]ec2types.Tag{
							{
								Key:   aws.String("cni.networking.k8s.aws/cluster/" + clusterName),
								Value: aws.String("shared"),
							},
							{
								Key:   aws.String("kubernetes.io/role/cni"),
								Value: aws.String("1"),
							},
						},
					)
				Expect(err).ToNot(HaveOccurred())

				By("Deleting other cluster subnet")
				if otherClusterSubnetID != "" {
					err := f.CloudServices.EC2().DeleteSubnet(context.TODO(), otherClusterSubnetID)
					if err != nil {
						GinkgoWriter.Printf("Warning: Failed to delete other cluster subnet %s: %v\n", otherClusterSubnetID, err)
					}
				}
			})

			It("should only use subnets tagged for this cluster", func() {
				By("creating deployment")
				container := manifest.NewNetCatAlpineContainer(f.Options.TestImageRegistry).
					Command([]string{"sleep"}).
					Args([]string{"3600"}).
					Build()

				deploymentBuilder := manifest.NewBusyBoxDeploymentBuilder(f.Options.TestImageRegistry).
					Container(container).
					Replicas(computeReplicasForBothSubnets(string(primaryInstance.InstanceType))). // Must overflow onto a secondary ENI (primary subnet not excluded here)
					PodLabel(enhancedPodLabelKey, enhancedPodLabelVal).
					NodeName(*primaryInstance.PrivateDnsName).
					Build()

				deployment, err = f.K8sResourceManagers.DeploymentManager().
					CreateAndWaitTillDeploymentIsReady(deploymentBuilder, utils.DefaultDeploymentReadyTimeout)
				Expect(err).ToNot(HaveOccurred())

				// Allow deployment to stabilize
				time.Sleep(10 * time.Second)

				By("verifying secondary ENIs are only in cluster-tagged subnet")
				instance, err := f.CloudServices.EC2().DescribeInstance(context.TODO(), *primaryInstance.InstanceId)
				Expect(err).ToNot(HaveOccurred())

				secondaryENICount := 0
				for _, nwInterface := range instance.NetworkInterfaces {
					if !common.IsPrimaryENI(nwInterface, instance.PrivateIpAddress) {
						secondaryENICount++
						// All secondary ENIs should be in the cluster-tagged subnet, not the other cluster's subnet
						Expect(*nwInterface.SubnetId).To(Equal(createdSubnet))
						Expect(*nwInterface.SubnetId).ToNot(Equal(otherClusterSubnetID))
					}
				}

				By("verifying at least one secondary ENI was created")
				Expect(secondaryENICount).To(BeNumerically(">", 0))

				By("deleting deployment")
				err = f.K8sResourceManagers.DeploymentManager().DeleteAndWaitTillDeploymentIsDeleted(deployment)
				Expect(err).ToNot(HaveOccurred())

				By("sleeping to allow CNI Plugin to delete unused ENIs")
				time.Sleep(time.Second * 90)
			})

			It("should not exclude primary subnet when it has old kubernetes.io/cluster/ tag for different cluster", func() {
				verifyPrimarySubnetNotExcludedWithTag(
					"kubernetes.io/cluster/some-other-cluster", "shared",
					"old-tag-compat",
					"primary subnet should not be excluded by old-style cluster tags",
				)
			})

			It("should not exclude primary subnet when it has no cni tag but has new cluster tag for different cluster", func() {
				verifyPrimarySubnetNotExcludedWithTag(
					"cni.networking.k8s.aws/cluster/different-cluster", "shared",
					"no-cni-tag-compat",
					"primary subnet should not be excluded when no cni tag present",
				)
			})
		})
	})
})

// verifyAPIServerConnectivity checks that pods can reach the Kubernetes API server.
// Uses wget --server-response (not -q) so the HTTP status is always printed,
// and parses the echoed exit code to distinguish connectivity failures from
// expected auth errors (401/403).
func verifyAPIServerConnectivity(labelKey, labelVal string) {
	pods, err := f.K8sResourceManagers.PodManager().GetPodsWithLabelSelector(labelKey, labelVal)
	Expect(err).ToNot(HaveOccurred())

	testedCount := 0
	for _, pod := range pods.Items {
		if pod.Status.Phase != corev1.PodRunning || testedCount >= 3 {
			continue
		}
		By(fmt.Sprintf("Testing API server connectivity from pod %s", pod.Name))
		// Use --server-response so wget always prints the HTTP status line.
		// The API server returns 401/403 for unauthenticated requests, which
		// still proves network connectivity. wget returns non-zero for HTTP
		// errors, so we echo the exit code and only fail on network-level errors.
		stdout, stderr, _ := f.K8sResourceManagers.PodManager().PodExec(
			pod.Namespace, pod.Name,
			[]string{"sh", "-c",
				"wget --server-response --timeout=5 -O /dev/null " +
					"https://$KUBERNETES_SERVICE_HOST:$KUBERNETES_SERVICE_PORT/api " +
					"--no-check-certificate 2>&1; echo EXIT:$?"},
		)
		combined := stdout + stderr
		// wget exit code 4 = network failure (timeout/connection refused).
		// Any other exit code (0, 6, 8) means the server was reached.
		if strings.Contains(combined, "EXIT:4") {
			Fail(fmt.Sprintf("Pod %s failed to reach API server (network failure). output: %s", pod.Name, combined))
		}
		GinkgoWriter.Printf("Pod %s reached API server\n", pod.Name)
		testedCount++
	}
	Expect(testedCount).To(BeNumerically(">", 0), "Should have tested at least one pod for connectivity")
}

// verifyPrimarySubnetNotExcludedWithTag is a shared helper for tests that verify
// the primary subnet is not excluded when tagged with various cluster tag prefixes.
func verifyPrimarySubnetNotExcludedWithTag(tagKey, tagValue, labelVal, assertMsg string) {
	By(fmt.Sprintf("Tagging primary subnet with %s=%s", tagKey, tagValue))
	_, err := f.CloudServices.EC2().
		CreateTags(context.TODO(), []string{primarySubnetID}, []ec2types.Tag{
			{Key: aws.String(tagKey), Value: aws.String(tagValue)},
		})
	Expect(err).ToNot(HaveOccurred())

	defer func() {
		By(fmt.Sprintf("Removing tag %s from primary subnet", tagKey))
		_, err = f.CloudServices.EC2().
			DeleteTags(context.TODO(), []string{primarySubnetID}, []ec2types.Tag{
				{Key: aws.String(tagKey), Value: aws.String(tagValue)},
			})
		Expect(err).ToNot(HaveOccurred())
	}()

	By("creating deployment")
	container := manifest.NewNetCatAlpineContainer(f.Options.TestImageRegistry).
		Command([]string{"sleep"}).Args([]string{"3600"}).Build()

	deploymentBuilder := manifest.NewBusyBoxDeploymentBuilder(f.Options.TestImageRegistry).
		Container(container).Replicas(5).
		PodLabel(enhancedPodLabelKey, labelVal).
		NodeName(*primaryInstance.PrivateDnsName).Build()

	dep, err := f.K8sResourceManagers.DeploymentManager().
		CreateAndWaitTillDeploymentIsReady(deploymentBuilder, utils.DefaultDeploymentReadyTimeout)
	Expect(err).ToNot(HaveOccurred())

	By("verifying pods are running")
	pods, err := f.K8sResourceManagers.PodManager().GetPodsWithLabelSelector(enhancedPodLabelKey, labelVal)
	Expect(err).ToNot(HaveOccurred())
	Expect(len(pods.Items)).To(BeNumerically(">", 0), "Pods should be running, "+assertMsg)

	By("deleting deployment")
	err = f.K8sResourceManagers.DeploymentManager().DeleteAndWaitTillDeploymentIsDeleted(dep)
	Expect(err).ToNot(HaveOccurred())

	By("sleeping to allow CNI Plugin to delete unused ENIs")
	time.Sleep(time.Second * 90)
}
