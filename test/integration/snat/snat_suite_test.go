package snat

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/aws/utils"
	testUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
)

var (
	f                                                     *framework.Framework
	props                                                 utils.NodeGroupProperties
	primaryNodeInPublicSubnet, primaryNodeInPrivateSubnet v1.Node
	privateSubnetId                                       string
	input                                                 string
)

// Change this if you want to use your own Key Pair
const DEFAULT_KEY_PAIR = "test-key-pair"

func TestSnat(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Snat Suite")
}

var _ = BeforeSuite(func() {
	f = framework.New(framework.GlobalOptions)

	By("creating test namespace")
	f.K8sResourceManagers.NamespaceManager().
		CreateNamespace(testUtils.DefaultTestNamespace)

	By("Getting existing nodes in the cluster")
	nodes, err := f.K8sResourceManagers.NodeManager().GetNodes(f.Options.NgNameLabelKey, f.Options.NgNameLabelVal)
	Expect(err).ToNot(HaveOccurred())

	By("verifying more than 1 nodes are present for the test")
	Expect(len(nodes.Items)).Should(BeNumerically(">", 1))

	// Set the primary node for testing
	primaryNodeInPublicSubnet = nodes.Items[0]

	By("Getting Public and Private subnets")
	vpcConfig, err := utils.GetClusterVPCConfig(f)
	Expect(err).ToNot(HaveOccurred())

	Expect(len(vpcConfig.PublicSubnetList)).To(BeNumerically(">", 0))
	Expect(len(vpcConfig.PrivateSubnetList)).To(BeNumerically(">", 0))

	msg := fmt.Sprintf("Creating a keyPair with name: %s if it doesn't exist", DEFAULT_KEY_PAIR)
	By(msg)
	keyPairOutput, _ := f.CloudServices.EC2().DescribeKey(context.TODO(), DEFAULT_KEY_PAIR)

	exists := false
	if keyPairOutput != nil {
		for _, keyPair := range keyPairOutput.KeyPairs {
			if *keyPair.KeyName == DEFAULT_KEY_PAIR {
				exists = true
				break
			}
		}
	}

	if exists {
		_, _ = fmt.Fprintln(GinkgoWriter, "KeyPair already exists")
	} else {
		_, _ = fmt.Fprintln(GinkgoWriter, "KeyPair doesn't exist, will be created")
		_, err := f.CloudServices.EC2().CreateKey(context.TODO(), DEFAULT_KEY_PAIR)
		Expect(err).NotTo(HaveOccurred())
	}

	privateSubnetId = vpcConfig.PrivateSubnetList[0]

	By("Getting Cluster Security Group Id")
	out, err := f.CloudServices.EKS().DescribeCluster(context.TODO(), f.Options.ClusterName)
	Expect(err).NotTo(HaveOccurred())

	clusterSecurityGroupId := out.Cluster.ResourcesVpcConfig.ClusterSecurityGroupId

	msg = fmt.Sprintf("Deploying a self managed nodegroup of size 1 in private subnet %s", privateSubnetId)
	By(msg)
	props = utils.NodeGroupProperties{
		NgLabelKey:    "test-label-key",
		NgLabelVal:    "test-label-val",
		AsgSize:       1,
		NodeGroupName: "snat-test-ng",
		Subnet: []string{
			privateSubnetId,
		},
		InstanceType: "m5.large",
		KeyPairName:  DEFAULT_KEY_PAIR,
	}

	err = utils.CreateAndWaitTillSelfManagedNGReady(f, props)
	Expect(err).NotTo(HaveOccurred())

	nodeList, err := f.K8sResourceManagers.NodeManager().GetNodes(props.NgLabelKey,
		props.NgLabelVal)
	Expect(err).ToNot(HaveOccurred())
	Expect(len(nodeList.Items)).Should(BeNumerically(">", 0))

	// Get ref to the only node from newly created nodegroup
	primaryNodeInPrivateSubnet = nodeList.Items[0]
	providerID := primaryNodeInPrivateSubnet.Spec.ProviderID
	Expect(len(providerID)).To(BeNumerically(">", 0))

	awsUrl, err := url.Parse(providerID)
	Expect(err).NotTo(HaveOccurred())

	instanceID := path.Base(awsUrl.Path)
	Expect(len(instanceID)).To(BeNumerically(">", 0))

	By("Fetching existing Security Groups from the newly created node group instance")

	instance, err := f.CloudServices.EC2().DescribeInstance(context.TODO(), instanceID)
	Expect(err).NotTo(HaveOccurred())

	existingSecurityGroups := instance.SecurityGroups
	networkInterfaceId := getPrimaryNetworkInterfaceId(instance.NetworkInterfaces, instance.PrivateIpAddress)
	Expect(networkInterfaceId).NotTo(Equal(BeNil()))

	securityGroupIds := make([]string, 0, len(existingSecurityGroups)+1)
	for _, sg := range existingSecurityGroups {
		securityGroupIds = append(securityGroupIds, aws.ToString(sg.GroupId))
	}
	securityGroupIds = append(securityGroupIds, aws.ToString(clusterSecurityGroupId))
	By("Adding ClusterSecurityGroup to the new nodegroup Instance")
	_, err = f.CloudServices.EC2().ModifyNetworkInterfaceSecurityGroups(context.TODO(), securityGroupIds, networkInterfaceId)
	Expect(err).NotTo(HaveOccurred())
})

var _ = AfterSuite(func() {
	//using default key pair created by test
	if DEFAULT_KEY_PAIR == "test-key-pair" {
		By("Deleting key-pair")
		err := f.CloudServices.EC2().DeleteKey(context.TODO(), DEFAULT_KEY_PAIR)
		Expect(err).NotTo(HaveOccurred())
	}

	By("Deleting test namespace")
	f.K8sResourceManagers.NamespaceManager().
		DeleteAndWaitTillNamespaceDeleted(testUtils.DefaultTestNamespace)

	By("Deleting Managed Nodegroup")
	err := utils.DeleteAndWaitTillSelfManagedNGStackDeleted(f, props)
	Expect(err).NotTo(HaveOccurred())
})

func getPrimaryNetworkInterfaceId(networkInterfaces []ec2types.InstanceNetworkInterface, instanceIPAddr *string) *string {
	for _, ni := range networkInterfaces {
		if strings.Compare(*ni.PrivateIpAddress, *instanceIPAddr) == 0 {
			return ni.NetworkInterfaceId
		}
	}
	return nil
}
