package multus

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("test Multus Deployment", func() {
	Context("validate Multus Deployment", func() {
		It("multus pod logs shouldnt have any errors", func() {
			By("Check Multus pod logs running on all worker nodes")
			multusPods, err := f.K8sResourceManagers.PodManager().GetPodsWithLabelSelector("name", "multus")
			Expect(err).NotTo(HaveOccurred())

			for _, pod := range multusPods.Items {
				podStr := fmt.Sprintf("Validating logs for pod: %v in Namespace: %v", pod.Name, pod.Namespace)
				By(podStr)
				logs, err := f.K8sResourceManagers.PodManager().PodLogs(pod.Namespace, pod.Name)
				Expect(err).NotTo(HaveOccurred())
				fmt.Fprintln(GinkgoWriter, logs)
			}
		})
	})
})
