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
	"flag"
	"fmt"
	"net"
	"testing"
	"time"

	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/apparentlymart/go-cidr/cidr"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework"
	awsUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/aws/utils"
	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
)

func TestCustomNetworking(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "CNI ENI Subnet Selection Test Suite")
}

var (
	f                      *framework.Framework
	clusterVPCConfig       *awsUtils.ClusterVPCConfig
	cidrRangeString        string
	cidrRange              *net.IPNet
	cidrBlockAssociationID string
	createdSubnet          string
	primaryInstance        ec2types.Instance
)

// Parse test specific variable from flag
func init() {
	flag.StringVar(&cidrRangeString, "secondary-cidr-range", "100.64.0.0/16", "second cidr range to be associated with the VPC")
}

var _ = BeforeSuite(func() {
	f = framework.New(framework.GlobalOptions)

	nodeList, err := f.K8sResourceManagers.NodeManager().GetNodes(f.Options.NgNameLabelKey,
		f.Options.NgNameLabelVal)
	Expect(err).ToNot(HaveOccurred())

	numOfNodes := len(nodeList.Items)
	Expect(numOfNodes).Should(BeNumerically(">", 1))

	// Nominate the first untainted node as the one to run deployment against
	By("finding the first untainted node for the deployment")
	var primaryNode *corev1.Node
	for _, n := range nodeList.Items {
		if len(n.Spec.Taints) == 0 {
			primaryNode = &n
			break
		}
	}
	Expect(primaryNode).To(Not(BeNil()), "expected to find a non-tainted node")

	instanceID := k8sUtils.GetInstanceIDFromNode(*primaryNode)
	primaryInstance, err = f.CloudServices.EC2().DescribeInstance(context.TODO(), instanceID)
	Expect(err).ToNot(HaveOccurred())

	_, cidrRange, err = net.ParseCIDR(cidrRangeString)
	Expect(err).ToNot(HaveOccurred())

	By("creating test namespace")
	_ = f.K8sResourceManagers.NamespaceManager().CreateNamespace(utils.DefaultTestNamespace)

	By("getting the cluster VPC Config")
	clusterVPCConfig, err = awsUtils.GetClusterVPCConfig(f)
	Expect(err).ToNot(HaveOccurred())

	By("associating cidr range to the VPC")
	association, err := f.CloudServices.EC2().AssociateVPCCIDRBlock(context.TODO(), f.Options.AWSVPCID, cidrRange.String())
	Expect(err).ToNot(HaveOccurred())
	cidrBlockAssociationID = *association.CidrBlockAssociation.AssociationId

	By(fmt.Sprintf("creating the subnet in %s", *primaryInstance.Placement.AvailabilityZone))

	// Subnet must be greater than /19
	subnetCidr, err := cidr.Subnet(cidrRange, 2, 0)
	Expect(err).ToNot(HaveOccurred())

	createSubnetOutput, err := f.CloudServices.EC2().
		CreateSubnet(context.TODO(), subnetCidr.String(), f.Options.AWSVPCID, *primaryInstance.Placement.AvailabilityZone)
	Expect(err).ToNot(HaveOccurred())

	subnetID := *createSubnetOutput.Subnet.SubnetId

	By("associating the route table with the newly created subnet")
	err = f.CloudServices.EC2().AssociateRouteTableToSubnet(context.TODO(), clusterVPCConfig.PublicRouteTableID, subnetID)
	Expect(err).ToNot(HaveOccurred())

	By("try detaching all ENIs by setting WARM_ENI_TARGET to 0")
	k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace,
		utils.AwsNodeName, map[string]string{"WARM_ENI_TARGET": "0"})

	By("sleeping to allow CNI Plugin to delete unused ENIs")
	time.Sleep(time.Second * 90)

	createdSubnet = subnetID
})

var _ = AfterSuite(func() {
	By("deleting test namespace")
	_ = f.K8sResourceManagers.NamespaceManager().
		DeleteAndWaitTillNamespaceDeleted(utils.DefaultTestNamespace)

	var errs prometheus.MultiError

	By("sleeping to allow CNI Plugin to delete unused ENIs")
	time.Sleep(time.Second * 90)

	By(fmt.Sprintf("deleting the subnet %s", createdSubnet))
	errs.Append(f.CloudServices.EC2().DeleteSubnet(context.TODO(), createdSubnet))

	By("disassociating the CIDR range to the VPC")
	errs.Append(f.CloudServices.EC2().DisAssociateVPCCIDRBlock(context.TODO(), cidrBlockAssociationID))

	Expect(errs.MaybeUnwrap()).ToNot(HaveOccurred())

	By("by setting WARM_ENI_TARGET to 1")
	k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace,
		utils.AwsNodeName, map[string]string{"WARM_ENI_TARGET": "1"})
})
