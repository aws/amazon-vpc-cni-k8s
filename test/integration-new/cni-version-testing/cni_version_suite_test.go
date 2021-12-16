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

package versiontesting

import (
	"fmt"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework"
	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	"github.com/aws/aws-sdk-go/service/eks"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
)

const InstanceTypeNodeLabelKey = "beta.kubernetes.io/instance-type"

var f *framework.Framework
var maxIPPerInterface int
var primaryNode v1.Node
var secondaryNode v1.Node
var instanceSecurityGroupID string
var vpcCIDRs []string

var k8sVersion string
var initialCNIVersion string
var finalCNIVersion string

func TestCNIVersion(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "CNI Upgrade/Downgrade Testing Suite")
}

var (
	describeAddonOutput *eks.DescribeAddonOutput
)
var _ = BeforeSuite(func() {
	f = framework.New(framework.GlobalOptions)

	By("creating test namespace")
	f.K8sResourceManagers.NamespaceManager().
		CreateNamespace(utils.DefaultTestNamespace)

	By(fmt.Sprintf("getting the node with the node label key %s and value %s",
		f.Options.NgNameLabelKey, f.Options.NgNameLabelVal))
	nodes, err := f.K8sResourceManagers.NodeManager().GetNodes(f.Options.NgNameLabelKey, f.Options.NgNameLabelVal)
	Expect(err).ToNot(HaveOccurred())

	By("verifying more than 1 nodes are present for the test")
	Expect(len(nodes.Items)).Should(BeNumerically(">", 1))

	// Set the primary and secondary node for testing
	primaryNode = nodes.Items[0]
	secondaryNode = nodes.Items[1]

	// Get the node security group
	instanceID := k8sUtils.GetInstanceIDFromNode(primaryNode)
	primaryInstance, err := f.CloudServices.EC2().DescribeInstance(instanceID)
	Expect(err).ToNot(HaveOccurred())

	// This won't work if the first SG is only associated with the primary instance.
	// Need a robust substring in the SGP name to identify node SGP
	instanceSecurityGroupID = *primaryInstance.NetworkInterfaces[0].Groups[0].GroupId

	By("getting the instance type from node label " + InstanceTypeNodeLabelKey)
	instanceType := primaryNode.Labels[InstanceTypeNodeLabelKey]

	By("getting the network interface details from ec2")
	instanceOutput, err := f.CloudServices.EC2().DescribeInstanceType(instanceType)
	Expect(err).ToNot(HaveOccurred())

	// Pods often get stuck due insufficient capacity, so adding some buffer to the maxIPPerInterface
	maxIPPerInterface = int(*instanceOutput[0].NetworkInfo.Ipv4AddressesPerInterface) - 5

	By("describing the VPC to get the VPC CIDRs")
	describeVPCOutput, err := f.CloudServices.EC2().DescribeVPC(f.Options.AWSVPCID)
	Expect(err).ToNot(HaveOccurred())

	for _, cidrBlockAssociationSet := range describeVPCOutput.Vpcs[0].CidrBlockAssociationSet {
		vpcCIDRs = append(vpcCIDRs, *cidrBlockAssociationSet.CidrBlock)
	}

	By("getting the cluster k8s version")
	describeClusterOutput, err := f.CloudServices.EKS().DescribeCluster(f.Options.ClusterName)
	Expect(err).ToNot(HaveOccurred())

	k8sVersion = *describeClusterOutput.Cluster.Version
	initialCNIVersion = f.Options.InitialCNIVersion
	finalCNIVersion = f.Options.FinalCNIVersion

})

var _ = AfterSuite(func() {
	By("deleting test namespace")
	f.K8sResourceManagers.NamespaceManager().
		DeleteAndWaitTillNamespaceDeleted(utils.DefaultTestNamespace)

})

func ApplyAddOn(versionName string) {
	By("getting the current addon")
	describeAddonOutput, err = f.CloudServices.EKS().DescribeAddon("vpc-cni", f.Options.ClusterName)
	if err == nil {
		fmt.Printf("%s,%s", *describeAddonOutput.Addon.AddonVersion, versionName)
		By("checking if the current addon is same as initial addon")
		if *describeAddonOutput.Addon.AddonVersion != versionName {

			By("deleting the current vpc cni addon ")
			_, err = f.CloudServices.EKS().DeleteAddon("vpc-cni", f.Options.ClusterName)
			Expect(err).ToNot(HaveOccurred())

			_, err = f.CloudServices.EKS().DescribeAddon("vpc-cni", f.Options.ClusterName)
			for err == nil {
				time.Sleep(5 * time.Second)
				_, err = f.CloudServices.EKS().DescribeAddon("vpc-cni", f.Options.ClusterName)

			}

			By("apply initial addon version")
			_, err = f.CloudServices.EKS().CreateAddonWithVersion("vpc-cni", f.Options.ClusterName, versionName)
			Expect(err).ToNot(HaveOccurred())

		}
	} else {
		By("apply initial addon version")
		_, err = f.CloudServices.EKS().CreateAddonWithVersion("vpc-cni", f.Options.ClusterName, versionName)
		Expect(err).ToNot(HaveOccurred())
	}

	var status string = ""

	By("waiting for initial addon to be ACTIVE")
	for status != "ACTIVE" {
		describeAddonOutput, err = f.CloudServices.EKS().DescribeAddon("vpc-cni", f.Options.ClusterName)
		Expect(err).ToNot(HaveOccurred())
		status = *describeAddonOutput.Addon.Status
		time.Sleep(5 * time.Second)
	}
}
