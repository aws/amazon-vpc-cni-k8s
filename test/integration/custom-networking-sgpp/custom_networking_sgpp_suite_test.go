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

package custom_networking_sgpp

import (
	"context"
	"flag"
	"fmt"
	"net"
	"testing"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/apis/crd/v1alpha1"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework"
	awsUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/aws/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	"github.com/aws/amazon-vpc-resource-controller-k8s/pkg/aws/vpc"
	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"

	"github.com/apparentlymart/go-cidr/cidr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestCustomNetworkingSGPP(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "CNI Custom Networking + Security Groups for Pods Test Suite")
}

var (
	f *framework.Framework
	// VPC Configuration with the details of public subnet and availability zone present in the cluster's subnets
	clusterVPCConfig *awsUtils.ClusterVPCConfig
	// The CIDR Range that will be associated with the VPC to create new subnet for Custom Networking
	cidrRangeString        string
	cidrRange              *net.IPNet
	cidrBlockAssociationID string
	// Security Group that will be used in ENIConfig
	customNetworkingSGID         string
	customNetworkingSubnetIDList []string
	// List of ENIConfig per Availability Zone
	eniConfigList        []*v1alpha1.ENIConfig
	eniConfigBuilderList []*manifest.ENIConfigBuilder
	// Security Group that will be used to create Security Group Policy
	podEniSGID string
	// Port that will be opened for Security Groups for Pods testing
	podEniOpenPort = 80
	metricsPort    = 8080
	// Maximum number of branch interfaces that can be created across all nodes
	totalBranchInterface int
	// Cluster security group ID for node to node communication
	clusterSGID string

	targetNode corev1.Node
	v4Zero     = "0.0.0.0/0"
	v6Zero     = "::/0"
	numNodes   int // number of nodes in cluster
)

// Parse test specific variable from flag
func init() {
	flag.StringVar(&cidrRangeString, "custom-networking-cidr-range", "100.64.0.0/16", "custom networking cidr range to be associated with the VPC")
}

var _ = BeforeSuite(func() {
	f = framework.New(framework.GlobalOptions)

	_, cidrRange, err = net.ParseCIDR(cidrRangeString)
	Expect(err).ToNot(HaveOccurred())

	By("getting the cluster VPC Config")
	clusterVPCConfig, err = awsUtils.GetClusterVPCConfig(f)
	Expect(err).ToNot(HaveOccurred())

	By("Getting Cluster Security Group ID")
	clusterRes, err := f.CloudServices.EKS().DescribeCluster(context.TODO(), f.Options.ClusterName)
	Expect(err).NotTo(HaveOccurred())
	clusterSGID = *(clusterRes.Cluster.ResourcesVpcConfig.ClusterSecurityGroupId)
	_, _ = fmt.Fprintf(GinkgoWriter, "cluster security group is %s\n", clusterSGID)

	// Custom Networking setup
	// TODO: Ideally, we would clone the Custom Networking SG from the cluster SG. Unfortunately, the EC2 API does not support this.
	By("creating security group to be used by custom networking")
	createSecurityGroupOutput, err := f.CloudServices.EC2().
		CreateSecurityGroup(context.TODO(), "custom-networking-test", "custom networking", f.Options.AWSVPCID)
	Expect(err).ToNot(HaveOccurred())
	customNetworkingSGID = *createSecurityGroupOutput.GroupId

	By("authorizing egress and ingress for security group in ENIConfig")
	_ = f.CloudServices.EC2().AuthorizeSecurityGroupEgress(context.TODO(), customNetworkingSGID, "-1", -1, -1, v4Zero)
	_ = f.CloudServices.EC2().AuthorizeSecurityGroupIngress(context.TODO(), customNetworkingSGID, "-1", -1, -1, v4Zero, false)

	By("associating cidr range to the VPC")
	association, err := f.CloudServices.EC2().AssociateVPCCIDRBlock(context.TODO(), f.Options.AWSVPCID, cidrRange.String())
	Expect(err).ToNot(HaveOccurred())
	cidrBlockAssociationID = *association.CidrBlockAssociation.AssociationId

	for i, az := range clusterVPCConfig.AvailZones {
		By(fmt.Sprintf("creating the subnet in %s", az))

		subnetCidr, err := cidr.Subnet(cidrRange, 8, 5*i)
		Expect(err).ToNot(HaveOccurred())

		createSubnetOutput, err := f.CloudServices.EC2().
			CreateSubnet(context.TODO(), subnetCidr.String(), f.Options.AWSVPCID, az)
		Expect(err).ToNot(HaveOccurred())

		subnetID := *createSubnetOutput.Subnet.SubnetId

		By("associating the route table with the newly created subnet")
		err = f.CloudServices.EC2().AssociateRouteTableToSubnet(context.TODO(), clusterVPCConfig.PublicRouteTableID, subnetID)
		Expect(err).ToNot(HaveOccurred())

		eniConfigBuilder := manifest.NewENIConfigBuilder().
			Name(az).
			SubnetID(subnetID).
			SecurityGroup([]string{customNetworkingSGID})
		eniConfig, err := eniConfigBuilder.Build()
		Expect(err).ToNot(HaveOccurred())

		// For updating/deleting later
		customNetworkingSubnetIDList = append(customNetworkingSubnetIDList, subnetID)
		eniConfigBuilderList = append(eniConfigBuilderList, eniConfigBuilder)
		eniConfigList = append(eniConfigList, eniConfig.DeepCopy())

		By("creating the ENIConfig with az name")
		err = f.K8sResourceManagers.CustomResourceManager().CreateResource(eniConfig)
		Expect(err).ToNot(HaveOccurred())
	}

	// Security Groups for Pods setup
	// Note that Custom Networking only supports IPv4 clusters, so IPv4 setup can be assumed.
	By("creating a new security group for use in Security Group Policy")
	podEniSGName := "pod-eni-automation-v4"
	securityGroupOutput, err := f.CloudServices.EC2().CreateSecurityGroup(context.TODO(), podEniSGName,
		"test created by vpc cni automation test suite", f.Options.AWSVPCID)
	Expect(err).ToNot(HaveOccurred())
	podEniSGID = *securityGroupOutput.GroupId

	By("authorizing egress and ingress on security group for client-server communication")
	_ = f.CloudServices.EC2().AuthorizeSecurityGroupEgress(context.TODO(), podEniSGID, "tcp", podEniOpenPort, podEniOpenPort, v4Zero)
	_ = f.CloudServices.EC2().AuthorizeSecurityGroupIngress(context.TODO(), podEniSGID, "tcp", podEniOpenPort, podEniOpenPort, v4Zero, false)

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

	By("enabling custom networking and sgpp on aws-node DaemonSet")
	k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName,
		utils.AwsNodeNamespace, utils.AwsNodeName, map[string]string{
			"AWS_VPC_K8S_CNI_CUSTOM_NETWORK_CFG": "true",
			"ENI_CONFIG_LABEL_DEF":               "topology.kubernetes.io/zone",
			"ENABLE_POD_ENI":                     "true",
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
	var errs prometheus.MultiError
	for _, eniConfig := range eniConfigList {
		By("deleting ENIConfig")
		errs.Append(f.K8sResourceManagers.CustomResourceManager().DeleteResource(eniConfig))
	}

	By("disabling custom networking and pod eni on aws-node DaemonSet")
	k8sUtils.RemoveVarFromDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName,
		utils.AwsNodeNamespace, utils.AwsNodeName, map[string]struct{}{
			"AWS_VPC_K8S_CNI_CUSTOM_NETWORK_CFG": {},
			"ENI_CONFIG_LABEL_DEF":               {},
			"ENABLE_POD_ENI":                     {},
		})

	By("terminating instances")
	errs.Append(awsUtils.TerminateInstances(f))

	By("deleting Custom Networking security group")
	errs.Append(f.CloudServices.EC2().DeleteSecurityGroup(context.TODO(), customNetworkingSGID))

	By("deleting pod ENI security group")
	errs.Append(f.CloudServices.EC2().DeleteSecurityGroup(context.TODO(), podEniSGID))

	for _, subnet := range customNetworkingSubnetIDList {
		By(fmt.Sprintf("deleting the subnet %s", subnet))
		errs.Append(f.CloudServices.EC2().DeleteSubnet(context.TODO(), subnet))
	}

	By("disassociating the CIDR range to the VPC")
	errs.Append(f.CloudServices.EC2().DisAssociateVPCCIDRBlock(context.TODO(), cidrBlockAssociationID))

	Expect(errs.MaybeUnwrap()).ToNot(HaveOccurred())
})
