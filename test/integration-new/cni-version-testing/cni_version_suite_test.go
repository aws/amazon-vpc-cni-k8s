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
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/eks"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	coreV1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"testing"
	"time"
)

const InstanceTypeNodeLabelKey = "beta.kubernetes.io/instance-type"

var (
	f                       *framework.Framework
	maxIPPerInterface       int
	primaryNode             v1.Node
	secondaryNode           v1.Node
	instanceSecurityGroupID string
	vpcCIDRs                []string

	k8sVersion        string
	initialCNIVersion string
	finalCNIVersion   string
	err               error
	// The command to run on server pods, to allow incoming
	// connections for different traffic type
	serverListenCmd []string
	// Arguments to the server listen command
	serverListenCmdArgs []string
	// The function that generates command which will be sent from
	// tester pod to receiver pod
	testConnectionCommandFunc func(serverPod coreV1.Pod, port int) []string
	// The functions reinforces that the positive test is working as
	// expected by creating a negative test command that should fail
	testFailedConnectionCommandFunc func(serverPod coreV1.Pod, port int) []string
	// Expected stdout from the exec command on testing connection
	// from tester to server
	testerExpectedStdOut string
	// Expected stderr from the exec command on testing connection
	// from tester to server
	testerExpectedStdErr string
	// The port on which server is listening for new connections
	serverPort int
	// Protocol for establishing connection to server
	protocol string
)

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
		fmt.Printf("By applying addon %s\n", versionName)
		By("checking if the current addon is same as addon to be applied")
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
		By("apply addon version")
		_, err = f.CloudServices.EKS().CreateAddonWithVersion("vpc-cni", f.Options.ClusterName, versionName)
		Expect(err).ToNot(HaveOccurred())
	}

	var status = ""

	By("waiting for  addon to be ACTIVE")
	for status != "ACTIVE" {
		describeAddonOutput, err = f.CloudServices.EKS().DescribeAddon("vpc-cni", f.Options.ClusterName)
		Expect(err).ToNot(HaveOccurred())
		status = *describeAddonOutput.Addon.Status
		time.Sleep(5 * time.Second)
	}
}

type InterfaceTypeToPodList struct {
	PodsOnPrimaryENI   []coreV1.Pod
	PodsOnSecondaryENI []coreV1.Pod
}

// GetPodsOnPrimaryAndSecondaryInterface returns the list of Pods on Primary Networking
// Interface and Secondary Network Interface on a given Node
func GetPodsOnPrimaryAndSecondaryInterface(node coreV1.Node,
	podLabelKey string, podLabelVal string) InterfaceTypeToPodList {
	podList, err := f.K8sResourceManagers.
		PodManager().
		GetPodsWithLabelSelector(podLabelKey, podLabelVal)
	Expect(err).ToNot(HaveOccurred())

	instance, err := f.CloudServices.EC2().
		DescribeInstance(k8sUtils.GetInstanceIDFromNode(node))
	Expect(err).ToNot(HaveOccurred())

	interfaceToPodList := InterfaceTypeToPodList{
		PodsOnPrimaryENI:   []coreV1.Pod{},
		PodsOnSecondaryENI: []coreV1.Pod{},
	}

	ipToPod := map[string]coreV1.Pod{}
	for _, pod := range podList.Items {
		ipToPod[pod.Status.PodIP] = pod
	}

	for _, nwInterface := range instance.NetworkInterfaces {
		isPrimary := IsPrimaryENI(nwInterface, instance.PrivateIpAddress)
		for _, ip := range nwInterface.PrivateIpAddresses {
			if pod, found := ipToPod[*ip.PrivateIpAddress]; found {
				if isPrimary {
					interfaceToPodList.PodsOnPrimaryENI =
						append(interfaceToPodList.PodsOnPrimaryENI, pod)
				} else {
					interfaceToPodList.PodsOnSecondaryENI =
						append(interfaceToPodList.PodsOnSecondaryENI, pod)
				}
			}
		}
	}
	return interfaceToPodList
}

func IsPrimaryENI(nwInterface *ec2.InstanceNetworkInterface, instanceIPAddr *string) bool {
	for _, privateIPAddress := range nwInterface.PrivateIpAddresses {
		if *privateIPAddress.PrivateIpAddress == *instanceIPAddr {
			return true
		}
	}
	return false
}
