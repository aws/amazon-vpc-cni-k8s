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

package cni

import (
	"context"
	"fmt"
	"testing"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework"
	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
)

const (
	InstanceTypeNodeLabelKey = "beta.kubernetes.io/instance-type"
	DEFAULT_VETH_PREFIX      = "eni"
	DEFAULT_MTU_VAL          = "9001"
)

var primaryNode v1.Node
var secondaryNode v1.Node
var instanceSecurityGroupID string
var vpcCIDRs []string

func TestCNIPodNetworking(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "CNI Pod Networking Suite")
}

var _ = BeforeSuite(func() {
	f = framework.New(framework.GlobalOptions)

	By("creating test namespace")
	f.K8sResourceManagers.NamespaceManager().CreateNamespace(utils.DefaultTestNamespace)

	By(fmt.Sprintf("getting the node with the node label key %s and value %s",
		f.Options.NgNameLabelKey, f.Options.NgNameLabelVal))
	nodes, err := f.K8sResourceManagers.NodeManager().GetNodes(f.Options.NgNameLabelKey, f.Options.NgNameLabelVal)
	Expect(err).ToNot(HaveOccurred())

	By("verifying more than 1 nodes are present for the test")
	Expect(len(nodes.Items)).Should(BeNumerically(">", 1))

	// Set the primary and secondary node for testing, these are used for a pod traffic test between two pods
	for i := range nodes.Items {
		n := nodes.Items[i]
		if len(n.Spec.Taints) == 0 {
			if primaryNode.Name == "" {
				primaryNode = n
			} else {
				secondaryNode = n
				break
			}
		}
	}
	Expect(primaryNode.Name).To(Not(HaveLen(0)), "expected to find a non-tainted node")
	Expect(secondaryNode.Name).To(Not(HaveLen(0)), "expected to find a non-tainted secondary node")

	// Get the node security group
	instanceID := k8sUtils.GetInstanceIDFromNode(primaryNode)
	primaryInstance, err := f.CloudServices.EC2().DescribeInstance(context.TODO(), instanceID)
	Expect(err).ToNot(HaveOccurred())

	// This won't work if the first SG is only associated with the primary instance.
	// Need a robust substring in the SGP name to identify node SGP
	instanceSecurityGroupID = *primaryInstance.NetworkInterfaces[0].Groups[0].GroupId

	By("describing the VPC to get the VPC CIDRs")
	describeVPCOutput, err := f.CloudServices.EC2().DescribeVPC(context.TODO(), f.Options.AWSVPCID)
	Expect(err).ToNot(HaveOccurred())

	for _, cidrBlockAssociationSet := range describeVPCOutput.Vpcs[0].CidrBlockAssociationSet {
		vpcCIDRs = append(vpcCIDRs, *cidrBlockAssociationSet.CidrBlock)
	}

	// WARM_ENI_TARGET=1 grows the IP pool an ENI at a time; WARM_IP_TARGET must
	// stay unset because ipamd ignores WARM_ENI_TARGET when it is set and the
	// pool then grows too slowly for a spanning deployment to converge.
	// WARM_PREFIX_TARGET=1 keeps the warm pool valid for prefix delegation
	// tests; zero is unsupported when warm and minimum IP targets are unset.
	// IP_COOLDOWN_PERIOD stays unset so ipamd uses its 30s default.
	k8sUtils.UpdateEnvVarOnDaemonSetAndWaitUntilReady(f, "aws-node", "kube-system",
		"aws-node", map[string]string{
			"WARM_ENI_TARGET":    "1",
			"WARM_PREFIX_TARGET": "1",
		}, map[string]struct{}{
			"WARM_IP_TARGET": {},
		})
})

var _ = AfterSuite(func() {
	By("deleting test namespace")
	f.K8sResourceManagers.NamespaceManager().
		DeleteAndWaitTillNamespaceDeleted(utils.DefaultTestNamespace)

	k8sUtils.UpdateEnvVarOnDaemonSetAndWaitUntilReady(f, "aws-node", "kube-system",
		"aws-node", map[string]string{
			"AWS_VPC_ENI_MTU":            DEFAULT_MTU_VAL,
			"AWS_VPC_K8S_CNI_VETHPREFIX": DEFAULT_VETH_PREFIX,
		},
		map[string]struct{}{
			"WARM_IP_TARGET":     {},
			"WARM_ENI_TARGET":    {},
			"WARM_PREFIX_TARGET": {},
			"IP_COOLDOWN_PERIOD": {},
		})
})
