package warm_pool

import (
	"fmt"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// This test does a random operation with a random number of pods over a preset number of iterations.
var _ = Describe("use case 4", func() {
	Context("Random Scale Random Add and Subtract Operations", func() {

		BeforeEach(func() {
			By("Getting Warm Pool Environment Variables Before Test")
			getWarmPoolEnvVars()
		})

		It("Scales the cluster and checks warm pool before and after", func() {
			replicas := minPods

			fmt.Fprintf(GinkgoWriter, "Deploying %v minimum pods\n", minPods)

			start := time.Now().Unix()

			deploymentSpec := manifest.NewBusyBoxDeploymentBuilder(f.Options.TestImageRegistry).
				Namespace("default").
				Name("busybox").
				NodeName(primaryNode.Name).
				Namespace(utils.DefaultTestNamespace).
				Replicas(replicas).
				Build()

			_, err := f.K8sResourceManagers.
				DeploymentManager().
				CreateAndWaitTillDeploymentIsReady(deploymentSpec, utils.DefaultDeploymentReadyTimeout*5)
			Expect(err).ToNot(HaveOccurred())

			if minPods != 0 {
				time.Sleep(sleep)
			}

			for i := 0; i < iterations; i++ {
				By("Loop " + strconv.Itoa(i))
				result, op, randPods := randOpLoop(replicas)
				replicas = checkInRange(result)
				if op == "no change" {
					fmt.Fprintf(GinkgoWriter, "No change to cluster, %v pods", replicas)
				} else {
					fmt.Fprintf(GinkgoWriter, "Scaling cluster to %v pods by %v %v pods\n", replicas, op, randPods)
				}
				quickScale(replicas)
				Expect(replicas).To(Equal(busyboxPodCnt()))
			}

			end := time.Now().Unix()

			fmt.Fprintf(GinkgoWriter, fmt.Sprintf("Start Time: %v\n", start))
			fmt.Fprintf(GinkgoWriter, fmt.Sprintf("End Time: %v\n", end))

			By("Starting Curl Container")
			curlContainer := manifest.NewCurlContainer().
				Command([]string{"sleep", "3600"}).Build()

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
