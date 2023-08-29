package warm_pool

import (
	"fmt"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	"math/rand"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// This test will add a random amount of pods until it gets to maxPods, then subtract a random amount of pods until it
// gets to minPods
var _ = Describe("use case 9", func() {
	Context("Random Add to Max, Random Sub to Min", func() {

		BeforeEach(func() {
			By("Getting Warm Pool Environment Variables Before Test")
			getWarmPoolEnvVars()
		})

		It("Scales the cluster and checks warm pool before and after", func() {
			replicas := minPods

			start := time.Now().Unix()

			fmt.Fprintf(GinkgoWriter, "Deploying %v minimum pods\n", minPods)
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

			for replicas < maxPods {
				i := 0
				randPods := incIf(rand.Intn(randDigits))
				By("Loop " + strconv.Itoa(i))
				// Will scale to a maximum of maxPods
				replicas = min(replicas+randPods, maxPods)
				fmt.Fprintf(GinkgoWriter, "Scaling cluster up to %v pods by adding %v pods\n", replicas, randPods)
				quickScale(replicas)
				Expect(replicas).To(Equal(busyboxPodCnt()))
				i++
			}

			for replicas > minPods {
				i := 0
				randPods := incIf(rand.Intn(randDigits))
				By("Loop " + strconv.Itoa(i))
				// Will scale to a minimum of minPods
				replicas = max(replicas-randPods, minPods)
				fmt.Fprintf(GinkgoWriter, "Scaling cluster down to %v pods by subtracting %v pods\n", replicas,
					randPods)
				quickScale(replicas)
				Expect(replicas).To(Equal(busyboxPodCnt()))
				i++
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
