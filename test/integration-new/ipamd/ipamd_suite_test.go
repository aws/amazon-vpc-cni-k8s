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
	"testing"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	"github.com/aws/aws-sdk-go/service/ec2"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	appsV1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
)

const (
	NAMESPACE          = "kube-system"
	DAEMONSET          = "aws-node"
	HOST_POD_LABEL_KEY = "network"
	HOST_POD_LABEL_VAL = "host"
)

var (
	primaryNode               v1.Node
	primaryInstanceId         string
	ds                        *appsV1.DaemonSet
	f                         *framework.Framework
	hostNetworkDeploymentSpec *appsV1.Deployment
	hostNetworkDeployment     *appsV1.Deployment
	err                       error
	hostNetworkPod            v1.Pod
	primaryInstance           *ec2.Instance
	numOfNodes                int
)

func TestIPAMD(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "VPC IPAMD Test Suite")
}

var _ = BeforeSuite(func() {
	f = framework.New(framework.GlobalOptions)
	ds, err = f.K8sResourceManagers.DaemonSetManager().GetDaemonSet(NAMESPACE, DAEMONSET)
	Expect(err).NotTo(HaveOccurred())

	nodeList, err := f.K8sResourceManagers.NodeManager().GetAllNodes()
	Expect(err).ToNot(HaveOccurred())

	numOfNodes = len(nodeList.Items)
	Expect(numOfNodes).Should(BeNumerically(">", 1))

	// Nominate the first node as the primary node
	primaryNode = nodeList.Items[0]

	instanceID := k8sUtils.GetInstanceIDFromNode(primaryNode)
	primaryInstance, err = f.CloudServices.EC2().DescribeInstance(instanceID)
	Expect(err).ToNot(HaveOccurred())

	curlContainer := manifest.NewBusyBoxContainerBuilder().Image("curlimages/curl:7.76.1").Name("curler").Build()

	hostNetworkDeploymentSpec = manifest.NewDefaultDeploymentBuilder().
		Namespace("default").
		Name("host-network").
		Replicas(1).
		HostNetwork(true).
		Container(curlContainer).
		PodLabel(HOST_POD_LABEL_KEY, HOST_POD_LABEL_VAL).
		NodeName(primaryNode.Name).
		Build()

	hostNetworkDeployment, err = f.K8sResourceManagers.
		DeploymentManager().
		CreateAndWaitTillDeploymentIsReady(hostNetworkDeploymentSpec)
	Expect(err).NotTo(HaveOccurred())

	pods, err := f.K8sResourceManagers.PodManager().GetPodsWithLabelSelector(HOST_POD_LABEL_KEY, HOST_POD_LABEL_VAL)
	Expect(err).NotTo(HaveOccurred())

	hostNetworkPod = pods.Items[0]

	// Set the WARM_ENI_TARGET to 0 to prevent all pods being scheduled on secondary ENI
	k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, "aws-node", "kube-system",
		"aws-node", map[string]string{"WARM_IP_TARGET": "3", "WARM_ENI_TARGET": "0"})
})

var _ = AfterSuite(func() {
	err = f.K8sResourceManagers.DeploymentManager().DeleteAndWaitTillDeploymentIsDeleted(hostNetworkDeploymentSpec)
	Expect(err).NotTo(HaveOccurred())

	k8sUtils.RemoveVarFromDaemonSetAndWaitTillUpdated(f, "aws-node", "kube-system",
		"aws-node", map[string]struct{}{"WARM_IP_TARGET": {}, "WARM_ENI_TARGET": {}})
})
