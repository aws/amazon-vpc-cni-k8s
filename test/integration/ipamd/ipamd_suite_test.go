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

	"github.com/aws/amazon-vpc-cni-k8s/test/framework"
	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	"github.com/aws/aws-sdk-go/service/ec2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/apps/v1"
)

var primaryInstance *ec2.Instance
var f *framework.Framework
var err error

const (
	CoreDNSDeploymentName           = "coredns"
	KubeSystemNamespace             = "kube-system"
	CoreDNSAutoscalerDeploymentName = "coredns-autoscaler"
)

var coreDNSDeploymentCopy *v1.Deployment
var coreDNSAutoscalerDeploymentCopy *v1.Deployment

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

	// Nominate the first node as the one to run coredns deployment against
	By("adding nodeSelector in coredns deployment to be scheduled on single node")
	primaryNode := nodeList.Items[0]
	fmt.Fprintf(GinkgoWriter, "coredns node is %s\n", primaryNode.Name)
	instanceID := k8sUtils.GetInstanceIDFromNode(primaryNode)
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
		"kubernetes.io/hostname": primaryNode.Name,
	}
	err = f.K8sResourceManagers.DeploymentManager().UpdateAndWaitTillDeploymentIsReady(coreDNSDeployment,
		utils.DefaultDeploymentReadyTimeout)
	Expect(err).ToNot(HaveOccurred())

	coreDNSAutoscalerDeployment, err := f.K8sResourceManagers.DeploymentManager().GetDeployment(CoreDNSAutoscalerDeploymentName,
		KubeSystemNamespace)
	if err == nil {
		coreDNSAutoscalerDeploymentCopy = coreDNSAutoscalerDeployment.DeepCopy()
		coreDNSAutoscalerDeployment.Spec.Template.Spec.NodeSelector = map[string]string{
			"kubernetes.io/hostname": primaryNode.Name,
		}
		err = f.K8sResourceManagers.DeploymentManager().UpdateAndWaitTillDeploymentIsReady(coreDNSAutoscalerDeployment,
			utils.DefaultDeploymentReadyTimeout)
	}

	// Redefine primary node as node without coredns pods. Note that this node may have previously had coredns pods.
	primaryNode = nodeList.Items[1]
	fmt.Fprintf(GinkgoWriter, "primary node is %s\n", primaryNode.Name)
	instanceID = k8sUtils.GetInstanceIDFromNode(primaryNode)
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

	if coreDNSAutoscalerDeploymentCopy != nil {
		By("restoring coredns-autoscaler deployment")
		err = f.K8sResourceManagers.DeploymentManager().UpdateAndWaitTillDeploymentIsReady(coreDNSAutoscalerDeploymentCopy,
			utils.DefaultDeploymentReadyTimeout)
	}

	By("deleting test namespace")
	f.K8sResourceManagers.NamespaceManager().
		DeleteAndWaitTillNamespaceDeleted(utils.DefaultTestNamespace)
})

func ceil(x, y int) int {
	return (x + y - 1) / y
}

func Max(x, y int) int {
	if x < y {
		return y
	}
	return x
}

// MinIgnoreZero returns smaller of two number, if any number is zero returns the other number
func MinIgnoreZero(x, y int) int {
	if x == 0 {
		return y
	}
	if y == 0 {
		return x
	}
	if x < y {
		return x
	}
	return y
}
