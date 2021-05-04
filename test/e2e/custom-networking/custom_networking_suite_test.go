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

package custom_networking

import (
	"flag"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/apis/crd/v1alpha1"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework"
	awsUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/aws/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"

	"github.com/apparentlymart/go-cidr/cidr"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestCustomNetworking(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "CNI Custom Networking e2e Test Suite")
}

var (
	f *framework.Framework
	// VPC Configuration with the details of public subnet and availability
	// zone present in the cluster's subnets
	clusterVPCConfig *awsUtils.ClusterVPCConfig
	// The CIDR Range that will be associated with the VPC to create new
	// subnet for custom networking
	cidrRangeString        string
	cidrRange              *net.IPNet
	cidrBlockAssociationID string
	// Key Pair is required for creating a self managed node group
	keyPairName = "custom-networking-key"
	// Security Group that will be used in ENIConfig
	customNetworkingSGID         string
	customNetworkingSGOpenPort   = 8080
	customNetworkingSubnetIDList []string
	// List of ENIConfig per Availability Zone
	eniConfigList []*v1alpha1.ENIConfig
	// Properties of the self managed node group created using CFN template
	nodeGroupProperties awsUtils.NodeGroupProperties
	err                 error
)

// Parse test specific variable from flag
func init() {
	flag.StringVar(&cidrRangeString, "custom-networking-cidr-range", "10.10.0.0/16", "custom networking cidr range to be associated with the VPC")
}

var _ = BeforeSuite(func() {
	f = framework.New(framework.GlobalOptions)

	_, cidrRange, err = net.ParseCIDR(cidrRangeString)
	Expect(err).ToNot(HaveOccurred())

	By("creating test namespace")
	f.K8sResourceManagers.NamespaceManager().
		CreateNamespace(utils.DefaultTestNamespace)

	By("getting the cluster VPC Config")
	clusterVPCConfig, err = awsUtils.GetClusterVPCConfig(f)
	Expect(err).ToNot(HaveOccurred())

	By("creating ec2 key-pair for the new node group")
	_, err := f.CloudServices.EC2().CreateKey(keyPairName)
	Expect(err).ToNot(HaveOccurred())

	By("creating security group to be used by custom networking")
	createSecurityGroupOutput, err := f.CloudServices.EC2().
		CreateSecurityGroup("custom-networking-test", "custom networking", f.Options.AWSVPCID)
	Expect(err).ToNot(HaveOccurred())
	customNetworkingSGID = *createSecurityGroupOutput.GroupId

	By("authorizing egress and ingress on security group for single port")
	f.CloudServices.EC2().
		AuthorizeSecurityGroupEgress(customNetworkingSGID, "TCP", customNetworkingSGOpenPort, customNetworkingSGOpenPort, "0.0.0.0/0")
	f.CloudServices.EC2().
		AuthorizeSecurityGroupIngress(customNetworkingSGID, "TCP", customNetworkingSGOpenPort, customNetworkingSGOpenPort, "0.0.0.0/0")

	By("associating cidr range to the VPC")
	association, err := f.CloudServices.EC2().AssociateVPCCIDRBlock(f.Options.AWSVPCID, cidrRange.String())
	Expect(err).ToNot(HaveOccurred())
	cidrBlockAssociationID = *association.CidrBlockAssociation.AssociationId

	for i, az := range clusterVPCConfig.AvailZones {
		By(fmt.Sprintf("creating the subnet in %s", az))

		subnetCidr, err := cidr.Subnet(cidrRange, 8, 5*i)
		Expect(err).ToNot(HaveOccurred())

		createSubnetOutput, err := f.CloudServices.EC2().
			CreateSubnet(subnetCidr.String(), f.Options.AWSVPCID, az)
		Expect(err).ToNot(HaveOccurred())

		subnetID := *createSubnetOutput.Subnet.SubnetId

		By("associating the route table with the newly created subnet")
		err = f.CloudServices.EC2().
			AssociateRouteTableToSubnet(clusterVPCConfig.PublicRouteTableID, subnetID)
		Expect(err).ToNot(HaveOccurred())

		eniConfig, err := manifest.NewENIConfigBuilder().
			Name(az).
			SubnetID(subnetID).
			SecurityGroup([]string{customNetworkingSGID}).
			Build()
		Expect(err).ToNot(HaveOccurred())

		// For deleting later
		customNetworkingSubnetIDList = append(customNetworkingSubnetIDList, subnetID)
		eniConfigList = append(eniConfigList, eniConfig.DeepCopy())

		By("creating the ENIConfig with az name")
		err = f.K8sResourceManagers.CustomResourceManager().CreateResource(eniConfig)
		Expect(err).ToNot(HaveOccurred())
	}

	By("enabling custom networking on aws-node DaemonSet")
	k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName,
		utils.AwsNodeNamespace, utils.AwsNodeName, map[string]string{
			"AWS_VPC_K8S_CNI_CUSTOM_NETWORK_CFG": "true",
			"ENI_CONFIG_LABEL_DEF":               "failure-domain.beta.kubernetes.io/zone",
			"WARM_ENI_TARGET":                    "0",
		})

	nodeGroupProperties = awsUtils.NodeGroupProperties{
		NgLabelKey:                "node-type",
		NgLabelVal:                "custom-networking-node",
		AsgSize:                   2,
		NodeGroupName:             "custom-networking-node",
		IsCustomNetworkingEnabled: true,
		Subnet:                    clusterVPCConfig.PublicSubnetList,
		InstanceType:              "c5.xlarge",
		KeyPairName:               keyPairName,
	}

	By("creating a new self managed node group")
	err = awsUtils.CreateAndWaitTillSelfManagedNGReady(f, nodeGroupProperties)
	Expect(err).ToNot(HaveOccurred())
})

var _ = AfterSuite(func() {
	By("deleting test namespace")
	f.K8sResourceManagers.NamespaceManager().
		DeleteAndWaitTillNamespaceDeleted(utils.DefaultTestNamespace)

	By("waiting for some time to allow CNI to delete ENI for IP being cooled down")
	time.Sleep(time.Second * 60)

	By("deleting the self managed node group")
	err = awsUtils.DeleteAndWaitTillSelfManagedNGStackDeleted(f, nodeGroupProperties)
	Expect(err).ToNot(HaveOccurred())

	By("deleting the key pair")
	f.CloudServices.EC2().DeleteKey(keyPairName)

	err = f.CloudServices.EC2().DeleteSecurityGroup(customNetworkingSGID)
	Expect(err).ToNot(HaveOccurred())

	for _, subnet := range customNetworkingSubnetIDList {
		By(fmt.Sprintf("deleting the subnet %s", subnet))
		err = f.CloudServices.EC2().DeleteSubnet(subnet)
		Expect(err).ToNot(HaveOccurred())
	}

	By("disassociating the CIDR range to the VPC")
	err = f.CloudServices.EC2().DisAssociateVPCCIDRBlock(cidrBlockAssociationID)
	Expect(err).ToNot(HaveOccurred())

	By("disabling custom networking on aws-node DaemonSet")
	k8sUtils.RemoveVarFromDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName,
		utils.AwsNodeNamespace, utils.AwsNodeName, map[string]struct{}{
			"AWS_VPC_K8S_CNI_CUSTOM_NETWORK_CFG": {},
			"ENI_CONFIG_LABEL_DEF":               {},
			"WARM_ENI_TARGET":                    {},
		})

	for _, eniConfig := range eniConfigList {
		By("deleting ENIConfig")
		err = f.K8sResourceManagers.CustomResourceManager().DeleteResource(eniConfig)
		Expect(err).ToNot(HaveOccurred())
	}
})
