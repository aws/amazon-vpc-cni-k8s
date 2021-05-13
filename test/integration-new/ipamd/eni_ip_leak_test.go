package ipamd

import (
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

const (
	NAMESPACE          = "kube-system"
	DAEMONSET          = "aws-node"
	HOST_POD_LABEL_KEY = "network"
	HOST_POD_LABEL_VAL = "host"
)

var _ = Describe("ENI/IP Leak Test", func() {
	Context("ENI/IP Released on Pod Deletion", func() {
		It("Verify that on Pod Deletion, ENI/IP State is restored", func() {
			// Set the WARM_ENI_TARGET to 0 to prevent all pods being scheduled on secondary ENI
			By("Setting WARM_ENI_TARGET to 0")
			k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, "aws-node", "kube-system",
				"aws-node", map[string]string{"WARM_IP_TARGET": "3", "WARM_ENI_TARGET": "0"})

			By("Recording the initial count of IP before new deployment")
			oldIP, oldENI := getCountOfIPandENIOnPrimaryInstance()

			maxPods := getMaxApplicationPodsOnPrimaryInstance()
			deploymentSpec := manifest.NewBusyBoxDeploymentBuilder().
				Namespace("default").
				Name("busybox").
				NodeName(primaryNode.Name).
				Replicas(int(maxPods)).
				Build()

			By("Deploying a max number of Busybox pods")
			_, err := f.K8sResourceManagers.
				DeploymentManager().
				CreateAndWaitTillDeploymentIsReady(deploymentSpec, utils.DefaultDeploymentReadyTimeout*5)
			Expect(err).ToNot(HaveOccurred())

			By("Deleting the deployment")
			err = f.K8sResourceManagers.DeploymentManager().DeleteAndWaitTillDeploymentIsDeleted(deploymentSpec)
			Expect(err).NotTo(HaveOccurred())

			By("Validating that count of ENI/IP is same as before")
			ip := 0
			eni := 0
			for i := 0; i < 3; i++ {
				// It takes some time to unassign IP addresses
				time.Sleep(120 * time.Second)
				ip, eni = getCountOfIPandENIOnPrimaryInstance()
				if ip == oldIP {
					break
				}
			}
			Expect(ip).To(Equal(oldIP))
			Expect(eni).To(Equal(oldENI))

			By("Restoring WARM ENI Target value")
			k8sUtils.RemoveVarFromDaemonSetAndWaitTillUpdated(f, "aws-node", "kube-system",
				"aws-node", map[string]struct{}{"WARM_IP_TARGET": {}, "WARM_ENI_TARGET": {}})
		})
	})
})

func getCountOfIPandENIOnPrimaryInstance() (int, int) {
	eni := len(primaryInstance.NetworkInterfaces)
	ip := 0
	for _, ni := range primaryInstance.NetworkInterfaces {
		ip += len(ni.PrivateIpAddresses)
	}
	return ip, eni
}

func getMaxApplicationPodsOnPrimaryInstance() int64 {
	instanceType := primaryInstance.InstanceType
	instaceInfo, err := f.CloudServices.EC2().DescribeInstanceType(*instanceType)
	Expect(err).NotTo(HaveOccurred())

	currInstance := instaceInfo[0]
	maxENI := currInstance.NetworkInfo.MaximumNetworkInterfaces
	maxIPPerENI := currInstance.NetworkInfo.Ipv4AddressesPerInterface

	// If core-dns pods are running on this instance then we need to exclude them as well
	maxPods := *maxENI*(*maxIPPerENI-1) - int64(numOfNodes)
	return maxPods
}
