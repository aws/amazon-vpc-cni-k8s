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

// This test will create a single burst to maxPods at a random interval depending on the preset number of iterations
var _ = Describe("use case 7", func() {
	Context("Single Burst Behavior", func() {

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

			randBurst := rand.Intn(iterations)

			for i := 0; i < iterations; i++ {
				By("Loop " + strconv.Itoa(i))

				if i == randBurst {
					fmt.Fprintf(GinkgoWriter, "Burst behavior from %v to %v pods\n", replicas, maxPods)
					quickScale(maxPods)
					continue
				}

				if i == randBurst+1 {
					fmt.Fprintf(GinkgoWriter, "Burst behavior over, scaling down from %v to %v pods\n", maxPods,
						replicas)
					quickScale(replicas)
					continue
				}

				result, op := randOp(replicas, iterPods)
				replicas = checkInRange(result)
				fmt.Fprintf(GinkgoWriter, "%v %v pod to cluster to equal %v pods\n", op, iterPods, replicas)
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
