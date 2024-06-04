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

package ipamd

import (
	"fmt"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework"
	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
)

const (
	CoreDNSDeploymentName = "coredns"
	KubeSystemNamespace   = "kube-system"
)

var coreDNSDeploymentCopy *v1.Deployment
var primaryNode *corev1.Node

func TestIPAMD(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "VPC IPAMD Test Suite")
}

var _ = BeforeSuite(func() {
	f = framework.New(framework.GlobalOptions)

	By("creating test namespace")
	f.K8sResourceManagers.NamespaceManager().
		CreateNamespace(utils.DefaultTestNamespace)

	nodeList, err := f.K8sResourceManagers.NodeManager().GetNodes(f.Options.NgNameLabelKey,
		f.Options.NgNameLabelVal)
	Expect(err).ToNot(HaveOccurred())

	numOfNodes = len(nodeList.Items)
	Expect(numOfNodes).Should(BeNumerically(">", 1))

	// Nominate the first untainted node as the one to run coredns deployment against
	By("adding nodeSelector in coredns deployment to be scheduled on single node")
	for _, n := range nodeList.Items {
		if len(n.Spec.Taints) == 0 {
			primaryNode = &n
			break
		}
	}
	Expect(primaryNode).To(Not(BeNil()), "expected to find a non-tainted node")
	fmt.Fprintf(GinkgoWriter, "coredns node is %s\n", primaryNode.Name)
	instanceID := k8sUtils.GetInstanceIDFromNode(*primaryNode)
	primaryInstance, err = f.CloudServices.EC2().DescribeInstance(instanceID)
	Expect(err).ToNot(HaveOccurred())

	By("getting node with no pods scheduled to run tests")
	coreDNSDeployment, err := f.K8sResourceManagers.DeploymentManager().GetDeployment(CoreDNSDeploymentName,
		KubeSystemNamespace)
	Expect(err).ToNot(HaveOccurred())

	// Copy the deployment to restore later
	coreDNSDeploymentCopy = coreDNSDeployment.DeepCopy()

	// Add nodeSelector label to coredns deployment so coredns pods are scheduled on 'primary' node
	coreDNSDeployment.Spec.Template.Spec.NodeSelector = map[string]string{
		"kubernetes.io/hostname": primaryNode.Labels["kubernetes.io/hostname"],
	}
	err = f.K8sResourceManagers.DeploymentManager().UpdateAndWaitTillDeploymentIsReady(coreDNSDeployment,
		utils.DefaultDeploymentReadyTimeout)
	Expect(err).ToNot(HaveOccurred())

	// Redefine primary node as node without coredns pods. Note that this node may have previously had coredns pods.
	for _, n := range nodeList.Items {
		if len(n.Spec.Taints) == 0 && n.Name != primaryNode.Name {
			primaryNode = &n
			break
		}
	}
	fmt.Fprintf(GinkgoWriter, "primary node is %s\n", primaryNode.Name)
	instanceID = k8sUtils.GetInstanceIDFromNode(*primaryNode)
	primaryInstance, err = f.CloudServices.EC2().DescribeInstance(instanceID)
	Expect(err).ToNot(HaveOccurred())

	// Set default values- WARM_ENI_TARGET to 1, and remove WARM_IP_TARGET, MINIMUM_IP_TARGET and WARM_PREFIX_TARGET
	k8sUtils.UpdateEnvVarOnDaemonSetAndWaitUntilReady(f, "aws-node", "kube-system",
		"aws-node", map[string]string{
			"WARM_ENI_TARGET": "1"},
		map[string]struct{}{
			"WARM_IP_TARGET":     {},
			"MINIMUM_IP_TARGET":  {},
			"WARM_PREFIX_TARGET": {},
		})

	// Allow reconciler to free up ENIs if any
	time.Sleep(utils.PollIntervalLong * 2)
})

var _ = AfterSuite(func() {
	// Restore coredns deployment
	By("restoring coredns deployment")
	err = f.K8sResourceManagers.DeploymentManager().UpdateAndWaitTillDeploymentIsReady(coreDNSDeploymentCopy,
		utils.DefaultDeploymentReadyTimeout)

	By("deleting test namespace")
	f.K8sResourceManagers.NamespaceManager().
		DeleteAndWaitTillNamespaceDeleted(utils.DefaultTestNamespace)
})
