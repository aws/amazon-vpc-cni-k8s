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

package calico

import (
	"testing"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
)

func TestCalico(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Calico Integration Test Suite")
}

var (
	f   *framework.Framework
	err error
	// Node on which SG port will be opened for ingress and egress
	node     v1.Node
	nodeSGID string
	openPort = 80
)

var _ = BeforeSuite(func() {
	f = framework.New(framework.GlobalOptions)

	By("getting the list of node")
	nodeList, err := f.K8sResourceManagers.NodeManager().GetAllNodes()
	Expect(err).ToNot(HaveOccurred())
	Expect(len(nodeList.Items)).Should(BeNumerically(">", 1))

	node = nodeList.Items[0]
	instanceID := utils.GetInstanceIDFromNode(node)

	By("getting the first node security group to allow ingress/egress traffic")
	instance, err := f.CloudServices.EC2().DescribeInstance(instanceID)
	Expect(err).ToNot(HaveOccurred())
	nodeSGID = *instance.SecurityGroups[0].GroupId

	By("authorizing egress/ingress on node security group")
	f.CloudServices.EC2().
		AuthorizeSecurityGroupIngress(nodeSGID, "TCP", openPort, openPort, "0.0.0.0/0")
	f.CloudServices.EC2().
		AuthorizeSecurityGroupEgress(nodeSGID, "TCP", openPort, openPort, "0.0.0.0/0")

	By("installing calico from eks/charts")
	err = f.InstallationManager.InstallCalico()
	Expect(err).ToNot(HaveOccurred())
})

var _ = AfterSuite(func() {
	By("revoking egress/ingress on node security group")
	f.CloudServices.EC2().
		RevokeSecurityGroupEgress(nodeSGID, "TCP", openPort, openPort, "0.0.0.0/0")
	f.CloudServices.EC2().
		RevokeSecurityGroupIngress(nodeSGID, "TCP", openPort, openPort, "0.0.0.0/0")

	By("uninstalling calico from eks/charts")
	err = f.InstallationManager.UninstallCalico()
	Expect(err).ToNot(HaveOccurred())
})
