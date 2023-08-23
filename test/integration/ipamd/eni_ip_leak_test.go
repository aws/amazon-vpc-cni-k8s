package ipamd

import (
	"time"

	v1 "k8s.io/api/core/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
)

var primaryNode v1.Node
var numOfNodes int

var _ = Describe("[CANARY][SMOKE] ENI/IP Leak Test", func() {
	Context("ENI/IP Released on Pod Deletion", func() {

		It("Verify that on Pod Deletion, ENI/IP State is restored", func() {
			// Set the WARM_ENI_TARGET to 0 to prevent all pods being scheduled on secondary ENI
			By("Setting WARM_ENI_TARGET to 0")
			k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, "aws-node", "kube-system",
				"aws-node", map[string]string{"WARM_IP_TARGET": "3", "WARM_ENI_TARGET": "0"})

			By("Recording the initial count of IP before new deployment")
			oldIP, oldENI := getCountOfIPandENIOnPrimaryInstance()

			maxPods := getMaxApplicationPodsOnPrimaryInstance()
			deploymentSpec := manifest.NewBusyBoxDeploymentBuilder(f.Options.TestImageRegistry).
				Namespace("default").
				Name("busybox").
				NodeName(primaryNode.Name).
				Namespace(utils.DefaultTestNamespace).
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

			// It takes some time to unassign IP addresses
			Eventually(func(g Gomega) {
				ip, eni := getCountOfIPandENIOnPrimaryInstance()
				g.Expect(ip).To(Equal(oldIP))
				g.Expect(eni).To(Equal(oldENI))
			}).WithTimeout(6 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())
		})

		AfterEach(func() {
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
	instanceInfo, err := f.CloudServices.EC2().DescribeInstanceType(*instanceType)
	Expect(err).NotTo(HaveOccurred())

	currInstance := instanceInfo[0]
	maxENI := currInstance.NetworkInfo.MaximumNetworkInterfaces
	maxIPPerENI := currInstance.NetworkInfo.Ipv4AddressesPerInterface

	// Deploy 50% of max pod capacity
	maxPods := *maxENI*(*maxIPPerENI-1) - int64(numOfNodes+1)
	return maxPods / 2
}
