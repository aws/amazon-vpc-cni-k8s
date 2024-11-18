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

package pod_eni

import (
	"context"
	"fmt"
	"testing"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework"
	awsUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/aws/utils"
	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	"github.com/aws/amazon-vpc-resource-controller-k8s/pkg/aws/vpc"
	corev1 "k8s.io/api/core/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	f   *framework.Framework
	err error
	// Cluster IP Address Family
	isIPv4Cluster = false
	// Security Group that will be used to to create Security Group Policy
	securityGroupId string
	// Ports that will be opened on the Security Group used for testing
	openPort = 80
	// Port than metrics server listens on
	metricsPort = 8080
	// Maximum number of Branch Interface created across all the self managed nodes
	totalBranchInterface int
	// Cluster security group ID for node to node communication
	clusterSGID string
	v4Zero      = "0.0.0.0/0"
	v6Zero      = "::/0"

	targetNode corev1.Node
	// Number of nodes in cluster
	numNodes int
)

func TestSecurityGroupForPods(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Security Group for Pods Test Suite")
}

var _ = BeforeSuite(func() {
	f = framework.New(framework.GlobalOptions)

	By("checking if cluster address family is IPv4 or IPv6")
	clusterOutput, err := f.CloudServices.EKS().DescribeCluster(context.TODO(), f.Options.ClusterName)
	Expect(err).NotTo(HaveOccurred())
	if clusterOutput.Cluster.KubernetesNetworkConfig.IpFamily == "ipv4" {
		isIPv4Cluster = true
		_, _ = fmt.Fprint(GinkgoWriter, "cluster is IPv4\n")
	} else {
		_, _ = fmt.Fprint(GinkgoWriter, "cluster is IPv6\n")
	}

	By("creating a new security group used in Security Group Policy")
	var sgName string
	if isIPv4Cluster {
		sgName = "pod-eni-automation-v4"
	} else {
		sgName = "pod-eni-automation-v6"
	}
	securityGroupOutput, err := f.CloudServices.EC2().CreateSecurityGroup(context.TODO(), sgName,
		"test created by vpc cni automation test suite", f.Options.AWSVPCID)
	Expect(err).ToNot(HaveOccurred())
	securityGroupId = *securityGroupOutput.GroupId

	By("authorizing egress and ingress on security group for client-server communication")
	if isIPv4Cluster {
		_ = f.CloudServices.EC2().AuthorizeSecurityGroupEgress(context.TODO(), securityGroupId, "tcp", openPort, openPort, v4Zero)
		_ = f.CloudServices.EC2().AuthorizeSecurityGroupIngress(context.TODO(), securityGroupId, "tcp", openPort, openPort, v4Zero, false)
	} else {
		_ = f.CloudServices.EC2().AuthorizeSecurityGroupEgress(context.TODO(), securityGroupId, "tcp", openPort, openPort, v6Zero)
		_ = f.CloudServices.EC2().AuthorizeSecurityGroupIngress(context.TODO(), securityGroupId, "tcp", openPort, openPort, v6Zero, false)
		_ = f.CloudServices.EC2().AuthorizeSecurityGroupIngress(context.TODO(), securityGroupId, "icmpv6", -1, -1, v6Zero, false)
	}

	By("getting branch ENI limits")
	nodeList, err := f.K8sResourceManagers.NodeManager().GetNodes(f.Options.NgNameLabelKey, f.Options.NgNameLabelVal)
	Expect(err).ToNot(HaveOccurred())
	numNodes = len(nodeList.Items)
	Expect(numNodes).Should(BeNumerically(">=", 1))

	node := nodeList.Items[0]
	instanceID := k8sUtils.GetInstanceIDFromNode(node)
	nodeInstance, err := f.CloudServices.EC2().DescribeInstance(context.TODO(), instanceID)
	instanceType := nodeInstance.InstanceType
	totalBranchInterface = vpc.Limits[string(instanceType)].BranchInterface * numNodes

	By("Getting Cluster Security Group ID")
	clusterRes, err := f.CloudServices.EKS().DescribeCluster(context.TODO(), f.Options.ClusterName)
	Expect(err).NotTo(HaveOccurred())
	clusterSGID = *(clusterRes.Cluster.ResourcesVpcConfig.ClusterSecurityGroupId)
	fmt.Fprintf(GinkgoWriter, "cluster security group is %s\n", clusterSGID)

	By("enabling pod eni on aws-node DaemonSet")
	k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName,
		utils.AwsNodeNamespace, utils.AwsNodeName, map[string]string{
			"ENABLE_POD_ENI": "true",
		})

	By("terminating instances")
	err = awsUtils.TerminateInstances(f)
	Expect(err).ToNot(HaveOccurred())

	By("getting target node")
	nodeList, err = f.K8sResourceManagers.NodeManager().GetNodes(f.Options.NgNameLabelKey, f.Options.NgNameLabelVal)
	Expect(err).ToNot(HaveOccurred())
	targetNode = nodeList.Items[0]
})

var _ = AfterSuite(func() {
	By("disabling pod-eni on aws-node DaemonSet")
	k8sUtils.RemoveVarFromDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName,
		utils.AwsNodeNamespace, utils.AwsNodeName, map[string]struct{}{
			"ENABLE_POD_ENI": {},
		})

	By("terminating instances")
	err := awsUtils.TerminateInstances(f)
	Expect(err).ToNot(HaveOccurred())

	By("deleting the security group")
	err = f.CloudServices.EC2().DeleteSecurityGroup(context.TODO(), securityGroupId)
	Expect(err).ToNot(HaveOccurred())
})
