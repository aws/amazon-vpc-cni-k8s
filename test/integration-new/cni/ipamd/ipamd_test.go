package ipamd

import (
	"regexp"
	"strconv"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

const (
	ENI_ENDPOINT = "http://localhost:61679/v1/enis"
)

var _ = Describe("IP Leak Test", func() {
	Context("IP Released on Pod Deletion", func() {
		It("Verify that on Pod Deletion, Warm Pool State is restored", func() {
			deploymentSpec := manifest.NewBusyBoxDeploymentBuilder().
				Namespace("default").
				Name("busybox").
				NodeName(primaryNode.Name).
				Replicas(10).
				Build()

			By("Recording the initial count of IP before new deployment")
			totalIps, assignedIps := getTotalAndAssignedIps()

			By("Deploying a large number of Busybox Deployment")
			_, err := f.K8sResourceManagers.
				DeploymentManager().
				CreateAndWaitTillDeploymentIsReady(deploymentSpec)
			Expect(err).ToNot(HaveOccurred())

			By("Recording the count of IP after deployment")
			currTotal, currAssigned := getTotalAndAssignedIps()
			Expect(currTotal).To(Equal(totalIps))
			// Diff should be equal to number of replicas in new deployment
			Expect(currAssigned - assignedIps).To(Equal(10))

			By("Deleting the deployment")
			err = f.K8sResourceManagers.DeploymentManager().DeleteAndWaitTillDeploymentIsDeleted(deploymentSpec)
			Expect(err).NotTo(HaveOccurred())

			By("Validating that count of IP is same as before")
			ip := 0
			assigned := 0
			for i := 0; i < 3; i++ {
				// It takes some time to unassign IP addresses
				time.Sleep(120 * time.Second)
				ip, assigned = getTotalAndAssignedIps()
				if assigned == assignedIps {
					break
				}
			}
			Expect(ip).To(Equal(totalIps))
			Expect(assigned).To(Equal(assignedIps))
		})
	})
})

func getTotalAndAssignedIps() (int, int) {
	stdout, _, err := f.K8sResourceManagers.PodManager().PodExec("default", hostNetworkPod.Name, []string{"curl", ENI_ENDPOINT})
	Expect(err).NotTo(HaveOccurred())

	re := regexp.MustCompile(`\"TotalIPs\":([0-9]*),\"AssignedIPs\":([0-9]*)`)
	ipCountStr := re.FindAllStringSubmatch(stdout, -1)[0]
	Expect(len(ipCountStr)).To(Equal(3))

	total, err := strconv.Atoi(ipCountStr[1])
	Expect(err).NotTo(HaveOccurred())

	assigned, err := strconv.Atoi(ipCountStr[2])
	Expect(err).NotTo(HaveOccurred())

	return total, assigned
}
