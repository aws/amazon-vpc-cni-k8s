package ipamd

import (
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

const (
	ENI_ENDPOINT = "http://localhost:61679/v1/enis"
)

var _ = Describe("ENI/IP Leak Test", func() {
	Context("ENI/IP Released on Pod Deletion", func() {
		It("Verify that on Pod Deletion, ENI/IP State is restored", func() {
			By("Recording the initial count of IP before new deployment")
			oldIP, oldENI := getCountOfIPAndENI(primaryInstanceId)

			deploymentSpec := manifest.NewBusyBoxDeploymentBuilder().
				Namespace("default").
				Name("busybox").
				NodeName(primaryNode.Name).
				Replicas(17).
				Build()

			By("Deploying a large number of Busybox Deployment")
			_, err := f.K8sResourceManagers.
				DeploymentManager().
				CreateAndWaitTillDeploymentIsReady(deploymentSpec)
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
				ip, eni = getCountOfIPAndENI(primaryInstanceId)
				if ip == oldIP {
					break
				}
			}
			Expect(ip).To(Equal(oldIP))
			Expect(eni).To(Equal(oldENI))
		})
	})
})

func getCountOfIPAndENI(instanceId string) (int, int) {
	instance, err := f.CloudServices.EC2().DescribeInstance(instanceId)
	Expect(err).NotTo(HaveOccurred())

	eni := len(instance.NetworkInterfaces)
	ip := 0
	for _, ni := range instance.NetworkInterfaces {
		ip += len(ni.PrivateIpAddresses)
	}
	return eni, ip
}
