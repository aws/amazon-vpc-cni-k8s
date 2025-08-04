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
	"os"
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
)

const (
	enhancedPodLabelKey = "test-type"
	enhancedPodLabelVal = "eni-subnet-enhanced"
)

var customSGID string
var primarySubnetID string

// This file contains additional tests for the enhanced subnet discovery functionality
// including primary subnet exclusion, custom security groups, and cluster-specific tags

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
					Replicas(30). // Enough to require secondary ENIs
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

		Context("when using custom security groups for secondary subnets", func() {
			BeforeEach(func() {
				By("Creating custom security group")
				createSecurityGroupOutput, err := f.CloudServices.EC2().
					CreateSecurityGroup(context.TODO(), "cni-subnet-discovery-test", "custom security group for CNI", f.Options.AWSVPCID)
				Expect(err).ToNot(HaveOccurred())
				customSGID = *createSecurityGroupOutput.GroupId

				By("Tagging custom security group with kubernetes.io/role/cni=1")
				_, err = f.CloudServices.EC2().
					CreateTags(
						context.TODO(),
						[]string{customSGID},
						[]ec2types.Tag{
							{
								Key:   aws.String("kubernetes.io/role/cni"),
								Value: aws.String("1"),
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

				By("Deleting custom security group")
				err = f.CloudServices.EC2().DeleteSecurityGroup(context.TODO(), customSGID)
				Expect(err).ToNot(HaveOccurred())
			})

			It("should use custom security group for ENIs in secondary subnet", func() {
				By("creating deployment")
				container := manifest.NewNetCatAlpineContainer(f.Options.TestImageRegistry).
					Command([]string{"sleep"}).
					Args([]string{"3600"}).
					Build()

				deploymentBuilder := manifest.NewBusyBoxDeploymentBuilder(f.Options.TestImageRegistry).
					Container(container).
					Replicas(30). // Enough to require secondary ENIs
					PodLabel(enhancedPodLabelKey, enhancedPodLabelVal).
					NodeName(*primaryInstance.PrivateDnsName).
					Build()

				deployment, err = f.K8sResourceManagers.DeploymentManager().
					CreateAndWaitTillDeploymentIsReady(deploymentBuilder, utils.DefaultDeploymentReadyTimeout)
				Expect(err).ToNot(HaveOccurred())

				// Allow deployment to stabilize
				time.Sleep(10 * time.Second)

				By("verifying secondary ENIs use custom security group")
				instance, err := f.CloudServices.EC2().DescribeInstance(context.TODO(), *primaryInstance.InstanceId)
				Expect(err).ToNot(HaveOccurred())

				// Get primary ENI security groups for comparison
				var primaryENISGs []string
				for _, nwInterface := range instance.NetworkInterfaces {
					if common.IsPrimaryENI(nwInterface, instance.PrivateIpAddress) {
						for _, sg := range nwInterface.Groups {
							primaryENISGs = append(primaryENISGs, *sg.GroupId)
						}
						break
					}
				}

				// Check secondary ENIs
				secondaryENICount := 0
				for _, nwInterface := range instance.NetworkInterfaces {
					if !common.IsPrimaryENI(nwInterface, instance.PrivateIpAddress) {
						secondaryENICount++

						// Secondary ENIs in secondary subnet should use custom SG
						if *nwInterface.SubnetId == createdSubnet {
							hasCustomSG := false
							for _, sg := range nwInterface.Groups {
								if *sg.GroupId == customSGID {
									hasCustomSG = true
									break
								}
							}
							Expect(hasCustomSG).To(BeTrue(), "Secondary ENI should have custom security group")
						}
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
								Key:   aws.String("kubernetes.io/cluster/" + clusterName),
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
				subnetCidr, err := cidr.Subnet(cidrRange, 2, 1) // Use a different subnet
				Expect(err).ToNot(HaveOccurred())

				otherSubnetOutput, err := f.CloudServices.EC2().
					CreateSubnet(context.TODO(), subnetCidr.String(), f.Options.AWSVPCID, *primaryInstance.Placement.AvailabilityZone)
				Expect(err).ToNot(HaveOccurred())

				otherSubnetID := *otherSubnetOutput.Subnet.SubnetId

				By("Tagging other subnet with different cluster tag")
				_, err = f.CloudServices.EC2().
					CreateTags(
						context.TODO(),
						[]string{otherSubnetID},
						[]ec2types.Tag{
							{
								Key:   aws.String("kubernetes.io/cluster/different-cluster"),
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
								Key:   aws.String("kubernetes.io/cluster/" + clusterName),
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

			It("should only use subnets tagged for this cluster", func() {
				By("creating deployment")
				container := manifest.NewNetCatAlpineContainer(f.Options.TestImageRegistry).
					Command([]string{"sleep"}).
					Args([]string{"3600"}).
					Build()

				deploymentBuilder := manifest.NewBusyBoxDeploymentBuilder(f.Options.TestImageRegistry).
					Container(container).
					Replicas(30). // Enough to require secondary ENIs
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
						// All secondary ENIs should be in the cluster-tagged subnet
						Expect(*nwInterface.SubnetId).To(Equal(createdSubnet))
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
	})
})
