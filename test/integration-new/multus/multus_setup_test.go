package multus

import (
	"errors"
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	MASTER_PLUGIN_STR = "Using MASTER_PLUGIN: 10-aws.conflist"
	MULTUS_CONF_STR   = "Config file created @ /host/etc/cni/net.d/00-multus.conf"
	SUCCESS_STR       = "Entering sleep (success)"
)

var _ = Describe("test Multus Deployment", func() {
	Context("validate Multus Deployment", func() {
		It("multus pod logs shouldnt have any errors", func() {
			By("Check Multus pod logs running on all worker nodes")
			multusPods, err := f.K8sResourceManagers.PodManager().GetPodsWithLabelSelector("name", "multus")
			Expect(err).NotTo(HaveOccurred())

			for _, pod := range multusPods.Items {
				podStr := fmt.Sprintf("Validating logs for pod: %v in Namespaace: %v", pod.Name, pod.Namespace)
				By(podStr)
				logs, err := f.K8sResourceManagers.PodManager().PodLogs(pod.Namespace, pod.Name)
				Expect(err).NotTo(HaveOccurred())
				fmt.Fprintln(GinkgoWriter, logs)

				err = validateMultusLogs(logs)
				Expect(err).NotTo(HaveOccurred())
			}
		})
	})
})

func validateMultusLogs(logStr string) error {
	// Check if MASTER_PLUGIN is correctly set
	if !strings.Contains(logStr, MASTER_PLUGIN_STR) {
		return errors.New("MASTER_PLUGIN incorrect")
	}
	if !strings.Contains(logStr, MULTUS_CONF_STR) {
		return errors.New("Missing entry for multus configuration file")
	}
	if !strings.Contains(logStr, SUCCESS_STR) {
		return errors.New("Multus not started")
	}
	return nil
}
