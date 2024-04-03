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
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/integration/common"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/apps/v1"
)

const (
	podLabelKey     = "role"
	podLabelVal     = "eni-subnet-discovery-test"
	AwsNodeLabelKey = "k8s-app"
	EKSCNIPolicyARN = "arn:aws:iam::aws:policy/AmazonEKS_CNI_Policy"
	EKSCNIPolicyV4  = "/testdata/amazon-eks-cni-policy-v4.json"
)

var err error
var newEniSubnetIds []string

var _ = Describe("ENI Subnet Selection Test", func() {
	var (
		deployment *v1.Deployment
	)

	Context("when creating deployment", func() {
		JustBeforeEach(func() {
			By("creating deployment")
			container := manifest.NewNetCatAlpineContainer(f.Options.TestImageRegistry).
				Command([]string{"sleep"}).
				Args([]string{"3600"}).
				Build()

			deploymentBuilder := manifest.NewBusyBoxDeploymentBuilder(f.Options.TestImageRegistry).
				Container(container).
				Replicas(50).
				PodLabel(podLabelKey, podLabelVal).
				NodeName(*primaryInstance.PrivateDnsName).
				Build()

			deployment, err = f.K8sResourceManagers.DeploymentManager().
				CreateAndWaitTillDeploymentIsReady(deploymentBuilder, utils.DefaultDeploymentReadyTimeout)
			Expect(err).ToNot(HaveOccurred())

			// Wait for deployment to settle, as if any pods restart, their pod IP will change between
			// the GET and the validation.
			time.Sleep(5 * time.Second)
		})

		JustAfterEach(func() {
			By("deleting deployment")
			err := f.K8sResourceManagers.DeploymentManager().DeleteAndWaitTillDeploymentIsDeleted(deployment)
			Expect(err).ToNot(HaveOccurred())

			By("sleeping to allow CNI Plugin to delete unused ENIs")
			time.Sleep(time.Second * 90)

			newEniSubnetIds = nil
		})

		Context("when subnet discovery is enabled", func() {
			BeforeEach(func() {
				k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace, utils.AwsNodeName, map[string]string{
					"ENABLE_SUBNET_DISCOVERY": "true",
				})
				// After updating daemonset pod, we must wait until conflist is updated so that container-runtime calls CNI ADD with the latest VETH prefix and MTU.
				// Otherwise, the stale value can cause failures in future test cases.
				time.Sleep(utils.PollIntervalMedium)
			})
			Context("using a subnet tagged with kubernetes.io/role/cni", func() {
				BeforeEach(func() {
					By("Tagging kubernetes.io/role/cni to subnet")
					_, err = f.CloudServices.EC2().
						CreateTags(
							[]string{createdSubnet},
							[]*ec2.Tag{
								{
									Key:   aws.String("kubernetes.io/role/cni"),
									Value: aws.String("1"),
								},
							},
						)
					Expect(err).ToNot(HaveOccurred())
				})
				AfterEach(func() {
					By("Untagging kubernetes.io/role/cni from subnet")
					_, err = f.CloudServices.EC2().
						DeleteTags(
							[]string{createdSubnet},
							[]*ec2.Tag{
								{
									Key:   aws.String("kubernetes.io/role/cni"),
									Value: aws.String("1"),
								},
							},
						)
					Expect(err).ToNot(HaveOccurred())
				})
				It(fmt.Sprintf("should have subnet in CIDR range %s", cidrRangeString), func() {
					checkSecondaryENISubnets(true)
				})

				Context("missing ec2:DescribeSubnets permission,", func() {
					var role string
					var EKSCNIPolicyV4ARN string
					BeforeEach(func() {
						By("getting the iam role")
						podList, err := f.K8sResourceManagers.PodManager().GetPodsWithLabelSelector(AwsNodeLabelKey, utils.AwsNodeName)
						Expect(err).ToNot(HaveOccurred())
						for _, env := range podList.Items[0].Spec.Containers[0].Env {
							if env.Name == "AWS_ROLE_ARN" {
								role = strings.Split(env.Value, "/")[1]
							}
						}
						if role == "" { // get the node instance role
							By("getting the node instance role")
							instanceProfileRoleName := strings.Split(*primaryInstance.IamInstanceProfile.Arn, "instance-profile/")[1]
							instanceProfileOutput, err := f.CloudServices.IAM().GetInstanceProfile(instanceProfileRoleName)
							Expect(err).ToNot(HaveOccurred())
							role = *instanceProfileOutput.InstanceProfile.Roles[0].RoleName
						}
						err = f.CloudServices.IAM().DetachRolePolicy(EKSCNIPolicyARN, role)
						Expect(err).ToNot(HaveOccurred())

						eksCNIPolicyV4Path := utils.GetProjectRoot() + EKSCNIPolicyV4
						eksCNIPolicyV4Bytes, err := os.ReadFile(eksCNIPolicyV4Path)
						Expect(err).ToNot(HaveOccurred())

						eksCNIPolicyV4Data := string(eksCNIPolicyV4Bytes)

						By("Creating and attaching policy AmazonEKS_CNI_Policy_V4")
						output, err := f.CloudServices.IAM().CreatePolicy("AmazonEKS_CNI_Policy_V4", eksCNIPolicyV4Data)
						Expect(err).ToNot(HaveOccurred())
						EKSCNIPolicyV4ARN = *output.Policy.Arn
						err = f.CloudServices.IAM().AttachRolePolicy(EKSCNIPolicyV4ARN, role)
						Expect(err).ToNot(HaveOccurred())

						// Sleep to allow time for CNI policy reattachment
						time.Sleep(10 * time.Second)

						RestartAwsNodePods()
					})

					AfterEach(func() {
						By("attaching VPC_CNI policy")
						err = f.CloudServices.IAM().AttachRolePolicy(EKSCNIPolicyARN, role)
						Expect(err).ToNot(HaveOccurred())

						By("Detaching and deleting policy AmazonEKS_CNI_Policy_V4")
						err = f.CloudServices.IAM().DetachRolePolicy(EKSCNIPolicyV4ARN, role)
						Expect(err).ToNot(HaveOccurred())

						err = f.CloudServices.IAM().DeletePolicy(EKSCNIPolicyV4ARN)
						Expect(err).ToNot(HaveOccurred())

						// Sleep to allow time for CNI policy detachment
						time.Sleep(10 * time.Second)

						RestartAwsNodePods()
					})
					It("should have same subnet as primary ENI", func() {
						checkSecondaryENISubnets(false)
					})
				})
			})
			Context("using a subnet tagged with kubernetes.io/role/cn", func() {
				BeforeEach(func() {
					By("Tagging kubernetes.io/role/cn to subnet")
					_, err = f.CloudServices.EC2().
						CreateTags(
							[]string{createdSubnet},
							[]*ec2.Tag{
								{
									Key:   aws.String("kubernetes.io/role/cn"),
									Value: aws.String("1"),
								},
							},
						)
					Expect(err).ToNot(HaveOccurred())
				})
				AfterEach(func() {
					By("Untagging kubernetes.io/role/cn from subnet")
					_, err = f.CloudServices.EC2().
						DeleteTags(
							[]string{createdSubnet},
							[]*ec2.Tag{
								{
									Key:   aws.String("kubernetes.io/role/cn"),
									Value: aws.String("1"),
								},
							},
						)
					Expect(err).ToNot(HaveOccurred())
				})

				It("should have same subnet as primary ENI", func() {
					checkSecondaryENISubnets(false)
				})
			})
		})

		Context("when subnet discovery is disabled", func() {
			BeforeEach(func() {
				k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace, utils.AwsNodeName, map[string]string{
					"ENABLE_SUBNET_DISCOVERY": "false",
				})
				// After updating daemonset pod, we must wait until conflist is updated so that container-runtime calls CNI ADD with the latest VETH prefix and MTU.
				// Otherwise, the stale value can cause failures in future test cases.
				time.Sleep(utils.PollIntervalMedium)
			})
			AfterEach(func() {
				// Set ENABLE_SUBNET_DISCOVERY back to true as this is the default behavior going forward.
				k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace, utils.AwsNodeName, map[string]string{
					"ENABLE_SUBNET_DISCOVERY": "true",
				})
				// After updating daemonset pod, we must wait until conflist is updated so that container-runtime calls CNI ADD with the latest VETH prefix and MTU.
				// Otherwise, the stale value can cause failures in future test cases.
				time.Sleep(utils.PollIntervalMedium)
			})
			Context("using a subnet tagged with kubernetes.io/role/cni", func() {
				BeforeEach(func() {
					By("Tagging kubernetes.io/role/cni to subnet")
					_, err = f.CloudServices.EC2().
						CreateTags(
							[]string{createdSubnet},
							[]*ec2.Tag{
								{
									Key:   aws.String("kubernetes.io/role/cni"),
									Value: aws.String("1"),
								},
							},
						)
					Expect(err).ToNot(HaveOccurred())
				})
				AfterEach(func() {
					By("Untagging kubernetes.io/role/cni from subnet")
					_, err = f.CloudServices.EC2().
						DeleteTags(
							[]string{createdSubnet},
							[]*ec2.Tag{
								{
									Key:   aws.String("kubernetes.io/role/cni"),
									Value: aws.String("1"),
								},
							},
						)
					Expect(err).ToNot(HaveOccurred())
				})
				It("should have the same subnets as the primary ENI", func() {
					checkSecondaryENISubnets(false)
				})
			})
		})
	})
})

func checkSecondaryENISubnets(expectNewCidr bool) {
	instance, err := f.CloudServices.EC2().DescribeInstance(*primaryInstance.InstanceId)
	Expect(err).ToNot(HaveOccurred())

	By("retrieving secondary ENIs")
	for _, nwInterface := range instance.NetworkInterfaces {
		primaryENI := common.IsPrimaryENI(nwInterface, instance.PrivateIpAddress)
		if !primaryENI {
			newEniSubnetIds = append(newEniSubnetIds, *nwInterface.SubnetId)
		}
	}

	By("verifying at least one new Secondary ENI is created")
	Expect(len(newEniSubnetIds)).Should(BeNumerically(">", 0))

	vpcOutput, err := f.CloudServices.EC2().DescribeVPC(*primaryInstance.VpcId)
	Expect(err).ToNot(HaveOccurred())

	expectedCidrRangeString := *vpcOutput.Vpcs[0].CidrBlock
	expectedCidrSplit := strings.Split(*vpcOutput.Vpcs[0].CidrBlock, "/")
	expectedSuffix, _ := strconv.Atoi(expectedCidrSplit[1])
	_, expectedCIDR, _ := net.ParseCIDR(*vpcOutput.Vpcs[0].CidrBlock)

	if expectNewCidr {
		expectedCidrRangeString = cidrRangeString
		expectedCidrSplit = strings.Split(cidrRangeString, "/")
		expectedSuffix, _ = strconv.Atoi(expectedCidrSplit[1])
		_, expectedCIDR, _ = net.ParseCIDR(cidrRangeString)
	}

	By(fmt.Sprintf("checking the secondary ENI subnets are in the CIDR %s", expectedCidrRangeString))
	for _, subnetID := range newEniSubnetIds {
		subnetOutput, err := f.CloudServices.EC2().DescribeSubnet(subnetID)
		Expect(err).ToNot(HaveOccurred())
		cidrSplit := strings.Split(*subnetOutput.Subnets[0].CidrBlock, "/")
		actualSubnetIp, _, _ := net.ParseCIDR(*subnetOutput.Subnets[0].CidrBlock)
		Expect(expectedCIDR.Contains(actualSubnetIp))
		suffix, _ := strconv.Atoi(cidrSplit[1])
		Expect(suffix).Should(BeNumerically(">=", expectedSuffix))
	}
}

func RestartAwsNodePods() {
	By("Restarting aws-node pods")
	podList, err := f.K8sResourceManagers.PodManager().GetPodsWithLabelSelector(AwsNodeLabelKey, utils.AwsNodeName)
	Expect(err).ToNot(HaveOccurred())
	for _, pod := range podList.Items {
		f.K8sResourceManagers.PodManager().DeleteAndWaitTillPodDeleted(&pod)
	}
}
