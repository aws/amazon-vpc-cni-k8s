package warm_pool

import (
	"fmt"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"time"
)

var primaryNode v1.Node

// This test scales up the cluster to maxPods, then scales it back down to minPods.
var _ = Describe("use case 1", func() {
	Context("Quick Scale Up and Down", func() {

		BeforeEach(func() {
			By("Getting Warm Pool Environment Variables Before Test")
			getWarmPoolEnvVars()
		})

		It("Scales the cluster and checks warm pool before and after", func() {
			fmt.Fprintf(GinkgoWriter, "Deploying %v minimum pods\n", minPods)

			start := time.Now().Unix()

			fmt.Fprintf(GinkgoWriter, "Scaling cluster up to %v pods\n", minPods)
			deploymentSpec := manifest.NewBusyBoxDeploymentBuilder(f.Options.TestImageRegistry).
				Namespace("default").
				Name("busybox").
				NodeName(primaryNode.Name).
				Namespace(utils.DefaultTestNamespace).
				Replicas(minPods).
				Build()

			_, err := f.K8sResourceManagers.
				DeploymentManager().
				CreateAndWaitTillDeploymentIsReady(deploymentSpec, utils.DefaultDeploymentReadyTimeout*5)
			Expect(err).ToNot(HaveOccurred())

			if minPods != 0 {
				time.Sleep(sleep)
			}

			fmt.Fprintf(GinkgoWriter, "Scaling cluster up to %v pods\n", maxPods)
			quickScale(maxPods)

			Expect(maxPods).To(Equal(busyboxPodCnt()))

			fmt.Fprintf(GinkgoWriter, "Scaling cluster down to %v pods\n", minPods)
			quickScale(minPods)

			end := time.Now().Unix()

			fmt.Fprintf(GinkgoWriter, fmt.Sprintf("Start Time: %v\n", start))
			fmt.Fprintf(GinkgoWriter, fmt.Sprintf("End Time: %v\n", end))

			By("Starting Curl Container")
			curlContainer := manifest.NewCurlContainer().
				Command([]string{"sleep", "1000"}).Build()

			getCurlPod := manifest.NewDefaultPodBuilder().
				Name("curl-pod").
				Namespace(utils.DefaultTestNamespace).
				NodeName(primaryNode.Name).
				HostNetwork(true).
				Container(curlContainer).
				Build()

			testPod, err := f.K8sResourceManagers.PodManager().
				CreateAndWaitTillPodCompleted(getCurlPod)

			logs, errLogs := f.K8sResourceManagers.PodManager().
				PodLogs(testPod.Namespace, testPod.Name)
			Expect(errLogs).ToNot(HaveOccurred())
			fmt.Fprintln(GinkgoWriter, logs)

			By("Fetching metrics via Curl Container")
			getMetrics(start, end)

			By("Deleting the deployment")
			err = f.K8sResourceManagers.DeploymentManager().DeleteAndWaitTillDeploymentIsDeleted(deploymentSpec)
			Expect(err).NotTo(HaveOccurred())

			By("Deleting Curl Container")
			err = f.K8sResourceManagers.PodManager().DeleteAndWaitTillPodDeleted(getCurlPod)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			By("Getting Warm Pool Environment Variables After Test")
			getWarmPoolEnvVars()

		})
	})
})
