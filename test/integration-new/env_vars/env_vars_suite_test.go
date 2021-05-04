package env_vars

import (
	"testing"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
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
	VOLUME_NAME        = "ipamd-logs"
	VOLUME_MOUNT_PATH  = "/var/log/aws-routed-eni/"
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
	primaryNodePublicIP       string
)

func TestCni(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Cni Suite")
}

var _ = BeforeSuite(func() {
	f = framework.New(framework.GlobalOptions)
	ds, err = f.K8sResourceManagers.DaemonSetManager().GetDaemonSet(NAMESPACE, DAEMONSET)
	Expect(err).NotTo(HaveOccurred())

	nodes, err := f.K8sResourceManagers.NodeManager().GetAllNodes()
	Expect(err).NotTo(HaveOccurred())
	Expect(len(nodes.Items)).To(BeNumerically(">", 0))

	primaryNode = nodes.Items[0]
	primaryInstanceId = k8sUtils.GetInstanceIDFromNode(primaryNode)
	instance, err := f.CloudServices.EC2().DescribeInstance(primaryInstanceId)
	Expect(err).NotTo(HaveOccurred())

	primaryNodePublicIP = *instance.PublicIpAddress

	hostNetworkDeploymentSpec = manifest.NewBusyBoxDeploymentBuilder().
		Namespace("default").
		Name("host-network").
		Replicas(1).
		HostNetwork(true).
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
})

var _ = AfterSuite(func() {
	err = f.K8sResourceManagers.DeploymentManager().DeleteAndWaitTillDeploymentIsDeleted(hostNetworkDeploymentSpec)
	Expect(err).NotTo(HaveOccurred())
})
