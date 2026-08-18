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
	"context"
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
	var primaryNode *corev1.Node
	for _, n := range nodeList.Items {
		if len(n.Spec.Taints) == 0 {
			primaryNode = &n
			break
		}
	}
	Expect(primaryNode).To(Not(BeNil()), "expected to find a non-tainted node")
	fmt.Fprintf(GinkgoWriter, "coredns node is %s\n", primaryNode.Name)
	instanceID := k8sUtils.GetInstanceIDFromNode(*primaryNode)
	primaryInstance, err = f.CloudServices.EC2().DescribeInstance(context.TODO(), instanceID)
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
	primaryInstance, err = f.CloudServices.EC2().DescribeInstance(context.TODO(), instanceID)
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
	// DeferCleanup so a failed coredns-restore Expect below cannot skip namespace teardown.
	DeferCleanup(func() {
		By("deleting test namespace")
		Expect(f.K8sResourceManagers.NamespaceManager().
			DeleteAndWaitTillNamespaceDeleted(utils.DefaultTestNamespace)).To(Succeed())
	})

	// coreDNSDeploymentCopy is nil if BeforeSuite failed before capturing it; there is
	// nothing to restore in that case.
	if coreDNSDeploymentCopy == nil {
		return
	}

	// Restore coredns to its original scheduling by removing the nodeSelector the
	// BeforeSuite added. Re-fetch the live deployment first: coreDNSDeploymentCopy was
	// captured before the pin, so its resourceVersion is stale and replaying it directly
	// conflicts on every retry and silently no-ops (the error was previously unchecked),
	// leaving coredns pinned to a single node. If a later suite sharing this cluster
	// terminates that node, coredns has nowhere to schedule and cluster DNS goes down.
	By("restoring coredns deployment")
	coreDNSDeployment, err := f.K8sResourceManagers.DeploymentManager().
		GetDeployment(CoreDNSDeploymentName, KubeSystemNamespace)
	Expect(err).ToNot(HaveOccurred())
	coreDNSDeployment.Spec.Template.Spec.NodeSelector = coreDNSDeploymentCopy.Spec.Template.Spec.NodeSelector
	err = f.K8sResourceManagers.DeploymentManager().UpdateAndWaitTillDeploymentIsReady(coreDNSDeployment,
		utils.DefaultDeploymentReadyTimeout)
	Expect(err).ToNot(HaveOccurred())
})
