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

package cni_egress

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
)

var f *framework.Framework
var maxIPPerInterface int
var primaryNode v1.Node
var primaryNodeIp *string
var vpcCIDRs []string
var isIPv4Cluster bool

// var used for v4 egress
type v4EgressVars struct {
	pods    v1.PodList
	podsIPs map[string]string
}

// var used for v6 egress
type v6EgressVars struct {
	podsInPrimaryENI      []v1.Pod
	podsInPrimaryENIIPs   map[string]string
	podsInSecondaryENI    []v1.Pod
	podsInSecondaryENIIPs map[string]string
}

func TestCNIPodNetworking(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "CNI Pod Egress Networking Suite")
}

var _ = BeforeSuite(func() {
	f = framework.New(framework.GlobalOptions)

	By("checking cluster v4 or v6")
	clusterOutput, err := f.CloudServices.EKS().DescribeCluster(context.TODO(), f.Options.ClusterName)
	Expect(err).NotTo(HaveOccurred())
	isIPv4Cluster = false
	if clusterOutput.Cluster.KubernetesNetworkConfig.IpFamily == "ipv4" {
		isIPv4Cluster = true
	}
	By("creating test namespace")
	_ = f.K8sResourceManagers.NamespaceManager().
		CreateNamespace(utils.DefaultTestNamespace)

	By(fmt.Sprintf("getting the node with the node label key %s and value %s",
		f.Options.NgNameLabelKey, f.Options.NgNameLabelVal))
	nodes, err := f.K8sResourceManagers.NodeManager().GetNodes(f.Options.NgNameLabelKey, f.Options.NgNameLabelVal)
	Expect(err).ToNot(HaveOccurred())

	By("verifying at least 1 node present for the test")
	Expect(len(nodes.Items)).Should(BeNumerically(">", 0))

	// Set the primary for testing
	for _, node := range nodes.Items {
		if len(node.Spec.Taints) == 0 {
			if primaryNode.Name == "" {
				primaryNode = node
				break
			}
		}
	}
	Expect(primaryNode.Name).To(Not(HaveLen(0)), "expected to find a non-tainted node")

	instanceID := k8sUtils.GetInstanceIDFromNode(primaryNode)
	primaryInstance, err := f.CloudServices.EC2().DescribeInstance(context.TODO(), instanceID)
	Expect(err).ToNot(HaveOccurred())

	if isIPv4Cluster {
		primaryNodeIp = primaryInstance.Ipv6Address
	} else {
		primaryNodeIp = primaryInstance.PublicIpAddress
	}
	Expect(primaryNodeIp).NotTo(BeNil())

	By("getting the instance type from node label " + InstanceTypeNodeLabelKey)
	instanceType := primaryNode.Labels[InstanceTypeNodeLabelKey]

	By("getting the network interface details from ec2")
	instanceOutput, err := f.CloudServices.EC2().DescribeInstanceType(context.TODO(), instanceType)
	Expect(err).ToNot(HaveOccurred())

	// Subtract 2 for coredns pods if any, both could be on same Interface
	maxIPPerInterface = int(*instanceOutput[0].NetworkInfo.Ipv4AddressesPerInterface) - 2

	if isIPv4Cluster {
		k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, "aws-node", "kube-system",
			"aws-node", map[string]string{
				"ENABLE_V6_EGRESS": "true",
			})
		k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, "aws-node", "kube-system",
			"aws-vpc-cni-init", map[string]string{
				"ENABLE_V6_EGRESS": "true",
			})
	}
})

var _ = AfterSuite(func() {
	By("deleting test namespace")
	f.K8sResourceManagers.NamespaceManager().
		DeleteAndWaitTillNamespaceDeleted(utils.DefaultTestNamespace)

	By("restoring default daemonset values")
	k8sUtils.RemoveVarFromDaemonSetAndWaitTillUpdated(f, "aws-node", "kube-system",
		"aws-vpc-cni-init", map[string]struct{}{
			"ENABLE_V6_EGRESS": {},
		})
	k8sUtils.RemoveVarFromDaemonSetAndWaitTillUpdated(f, "aws-node", "kube-system",
		"aws-node", map[string]struct{}{
			"ENABLE_V6_EGRESS": {},
		})
})
