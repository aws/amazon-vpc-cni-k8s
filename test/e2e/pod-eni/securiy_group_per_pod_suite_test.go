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
	"strings"
	"testing"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework"
	awsUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/aws/utils"
	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"

	"github.com/aws/amazon-vpc-resource-controller-k8s/pkg/aws/vpc"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

const AmazonEKSVPCResourceControllerARN = "arn:aws:iam::aws:policy/AmazonEKSVPCResourceController"

var (
	f   *framework.Framework
	err error
	// Key pair used for creating new self managed node group
	keyPairName = "pod-eni-test"
	// Security Group that will be used to to create Security Group Policy
	securityGroupId string
	// Ports that will be opened on the Security Group used for testing
	openPort = 80
	// Size of the Auto Scaling Group used for testing Security Group For Pods
	asgSize = 3
	// Nitro Based instance type only
	instanceType = "c5.xlarge"
	// Maximum number of Branch Interface created across all the self managed nodes
	totalBranchInterface int
	// Self managed node group
	nodeGroupProperties awsUtils.NodeGroupProperties
	// Cluster Role name derived from cluster Role ARN, used to attach VPC Controller Policy
	clusterRoleName string
)

func TestSecurityGroupForPods(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Security Group for Pods e2e Test Suite")
}

var _ = BeforeSuite(func() {
	f = framework.New(framework.GlobalOptions)

	By("creating ec2 key-pair for the new node group")
	_, err := f.CloudServices.EC2().CreateKey(keyPairName)
	Expect(err).ToNot(HaveOccurred())

	By("creating a new security group used in Security Group Policy")
	securityGroupOutput, err := f.CloudServices.EC2().CreateSecurityGroup("pod-eni-automation",
		"test created by vpc cni automation test suite", f.Options.AWSVPCID)
	Expect(err).ToNot(HaveOccurred())
	securityGroupId = *securityGroupOutput.GroupId

	By("authorizing egress and ingress on security group for client-server communication")
	f.CloudServices.EC2().
		AuthorizeSecurityGroupEgress(securityGroupId, "TCP", openPort, openPort, "0.0.0.0/0")
	f.CloudServices.EC2().
		AuthorizeSecurityGroupIngress(securityGroupId, "TCP", openPort, openPort, "0.0.0.0/0")

	By("getting the cluster VPC Config")
	clusterVPCConfig, err := awsUtils.GetClusterVPCConfig(f)
	Expect(err).ToNot(HaveOccurred())

	By("getting the cluster role name")
	describeClusterOutput, err := f.CloudServices.EKS().DescribeCluster(f.Options.ClusterName)
	Expect(err).ToNot(HaveOccurred())
	clusterRoleName = strings.Split(*describeClusterOutput.Cluster.RoleArn, "/")[1]

	By("attaching the AmazonEKSVPCResourceController policy from the cluster role")
	err = f.CloudServices.IAM().
		AttachRolePolicy(AmazonEKSVPCResourceControllerARN, clusterRoleName)
	Expect(err).ToNot(HaveOccurred())

	nodeGroupProperties = awsUtils.NodeGroupProperties{
		NgLabelKey:    "node-type",
		NgLabelVal:    "pod-eni-node",
		AsgSize:       asgSize,
		NodeGroupName: "pod-eni-node",
		Subnet:        clusterVPCConfig.PublicSubnetList,
		InstanceType:  instanceType,
		KeyPairName:   keyPairName,
	}

	totalBranchInterface = vpc.Limits[instanceType].BranchInterface * asgSize

	By("creating a new self managed node group")
	err = awsUtils.CreateAndWaitTillSelfManagedNGReady(f, nodeGroupProperties)
	Expect(err).ToNot(HaveOccurred())

	By("enabling pod eni on aws-node DaemonSet")
	k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName,
		utils.AwsNodeNamespace, utils.AwsNodeName, map[string]string{
			"ENABLE_POD_ENI": "true",
		})
})

var _ = AfterSuite(func() {
	By("disabling pod-eni on aws-node DaemonSet")
	k8sUtils.RemoveVarFromDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName,
		utils.AwsNodeNamespace, utils.AwsNodeName, map[string]struct{}{
			"ENABLE_POD_ENI": {},
		})

	By("deleting the key-pair used to create nodegroup")
	err = f.CloudServices.EC2().DeleteKey(keyPairName)
	Expect(err).ToNot(HaveOccurred())

	By("deleting the self managed node group")
	err = awsUtils.DeleteAndWaitTillSelfManagedNGStackDeleted(f, nodeGroupProperties)
	Expect(err).ToNot(HaveOccurred())

	By("deleting the security group")
	err = f.CloudServices.EC2().DeleteSecurityGroup(securityGroupId)
	Expect(err).ToNot(HaveOccurred())

	By("detaching the AmazonEKSVPCResourceController policy from the cluster role")
	err = f.CloudServices.IAM().
		DetachRolePolicy(AmazonEKSVPCResourceControllerARN, clusterRoleName)
	Expect(err).ToNot(HaveOccurred())
})
