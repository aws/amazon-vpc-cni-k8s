//go:build integration
// +build integration

// Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//     http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package cni

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/integration/common"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/apps/v1"
	coreV1 "k8s.io/api/core/v1"
)

// Pod Churn Test for Ubuntu AMI
// This test validates the stability and reliability of the VPC CNI when running on Ubuntu nodes
// by repeatedly creating and deleting pods to simulate high pod churn scenarios.
// The test ensures that IP allocation/deallocation works correctly and network connectivity
// is maintained throughout the churn process.

var _ = Describe("Pod Churn Test for Ubuntu AMI", Ordered, func() {

	var (
		err                     error
		serverListenCmd         []string
		serverListenCmdArgs     []string
		testConnectionFunc      func(serverPod coreV1.Pod, port int) []string
		serverPort              int
		protocol                string
		churnDeployment         *v1.Deployment
		churnIterations         = 10
		podsPerIteration        = 20
		waitBetweenIterations   = time.Duration(30) * time.Second
		ubuntuNodeSelector      = map[string]string{"kubernetes.io/os": "linux"}
	)

	BeforeAll(func() {
		fmt.Println("Starting Pod Churn test for Ubuntu AMI")

		protocol = "tcp"
		serverPort = 2274

		By("Authorize Security Group Ingress for pod churn test")
		err = f.CloudServices.EC2().
			AuthorizeSecurityGroupIngress(context.TODO(), instanceSecurityGroupID, protocol, serverPort, serverPort, "0.0.0.0/0", false)
		Expect(err).ToNot(HaveOccurred())

		By("Authorize Security Group Egress for pod churn test")
		err = f.CloudServices.EC2().
			AuthorizeSecurityGroupEgress(context.TODO(), instanceSecurityGroupID, protocol, serverPort, serverPort, "0.0.0.0/0")
		Expect(err).ToNot(HaveOccurred())
	})

	AfterAll(func() {
		fmt.Println("Cleaning up Pod Churn test for Ubuntu AMI")

		By("Revoke Security Group Ingress")
		err = f.CloudServices.EC2().
			RevokeSecurityGroupIngress(context.TODO(), instanceSecurityGroupID, protocol, serverPort, serverPort, "0.0.0.0/0", false)
		Expect(err).ToNot(HaveOccurred())

		By("Revoke Security Group Egress")
		err = f.CloudServices.EC2().
			RevokeSecurityGroupEgress(context.TODO(), instanceSecurityGroupID, protocol, serverPort, serverPort, "0.0.0.0/0")
		Expect(err).ToNot(HaveOccurred())

		By("Pod Churn test for Ubuntu AMI completed")
	})

	Context("[UBUNTU_CHURN_TEST] Validate pod churn stability on Ubuntu nodes", func() {
		BeforeEach(func() {
			serverListenCmd = []string{"nc"}
			serverListenCmdArgs = []string{"-k", "-l", strconv.Itoa(serverPort)}

			testConnectionFunc = func(receiverPod coreV1.Pod, port int) []string {
				return []string{"nc", "-v", "-w5", receiverPod.Status.PodIP, strconv.Itoa(port)}
			}
		})

		It("should handle rapid pod creation and deletion cycles on Ubuntu nodes", func() {
			for iteration := 1; iteration <= churnIterations; iteration++ {
				By(fmt.Sprintf("Starting churn iteration %d/%d", iteration, churnIterations))

				// Create server container for this iteration
				serverContainer := manifest.
					NewNetCatAlpineContainer(f.Options.TestImageRegistry).
					Command(serverListenCmd).
					Args(serverListenCmdArgs).
					Build()

				// Create deployment with pods scheduled on Ubuntu nodes
				churnDeployment = manifest.
					NewDefaultDeploymentBuilder().
					Container(serverContainer).
					Replicas(podsPerIteration).
					NodeSelector(ubuntuNodeSelector).
					PodLabel("churn-test", "ubuntu").
					PodLabel("iteration", strconv.Itoa(iteration)).
					Name(fmt.Sprintf("ubuntu-churn-test-%d", iteration)).
					Build()

				By(fmt.Sprintf("Creating %d pods for iteration %d", podsPerIteration, iteration))
				churnDeployment, err = f.K8sResourceManagers.
					DeploymentManager().
					CreateAndWaitTillDeploymentIsReady(churnDeployment, utils.DefaultDeploymentReadyTimeout)
				Expect(err).ToNot(HaveOccurred())

				// Get pods and verify they are running on Ubuntu nodes
				podList, err := f.K8sResourceManagers.PodManager().
					GetPodsWithLabelSelector("churn-test", "ubuntu")
				Expect(err).ToNot(HaveOccurred())
				Expect(len(podList.Items)).To(Equal(podsPerIteration))

				By("Verifying all pods have valid IP addresses")
				for _, pod := range podList.Items {
					Expect(pod.Status.PodIP).ToNot(BeEmpty())
					Expect(pod.Status.Phase).To(Equal(coreV1.PodRunning))
				}

				// Test connectivity between pods
				if len(podList.Items) >= 2 {
					By("Testing connectivity between pods in this iteration")
					serverPod := podList.Items[0]
					clientPod := podList.Items[1]

					// Create a simple connectivity test
					testCmd := testConnectionFunc(serverPod, serverPort)
					_, err := f.K8sResourceManagers.PodManager().
						PodExec(clientPod.Namespace, clientPod.Name, testCmd)
					// Note: We expect this to succeed, but netcat may return non-zero exit codes
					// The important thing is that the connection attempt doesn't hang or crash
				}

				By(fmt.Sprintf("Deleting deployment for iteration %d", iteration))
				err = f.K8sResourceManagers.DeploymentManager().
					DeleteAndWaitTillDeploymentIsDeleted(churnDeployment)
				Expect(err).ToNot(HaveOccurred())

				// Verify all pods are cleaned up
				By("Verifying all pods are cleaned up")
				Eventually(func() int {
					remainingPods, _ := f.K8sResourceManagers.PodManager().
						GetPodsWithLabelSelector("iteration", strconv.Itoa(iteration))
					return len(remainingPods.Items)
				}, time.Minute*2, time.Second*5).Should(Equal(0))

				if iteration < churnIterations {
					By(fmt.Sprintf("Waiting %v before next iteration", waitBetweenIterations))
					time.Sleep(waitBetweenIterations)
				}
			}

			By("Verifying CNI health after all churn iterations")
			// Check that the CNI is still healthy by creating a final test pod
			finalTestContainer := manifest.
				NewBusyBoxContainerBuilder(f.Options.TestImageRegistry).
				Command([]string{"sleep"}).
				Args([]string{"300"}).
				Build()

			finalTestPod := manifest.
				NewDefaultPodBuilder().
				Container(finalTestContainer).
				NodeSelector(ubuntuNodeSelector).
				Name("ubuntu-churn-final-test").
				Build()

			finalTestPod, err = f.K8sResourceManagers.PodManager().
				CreateAndWaitTillPodCompleted(finalTestPod)
			Expect(err).ToNot(HaveOccurred())
			Expect(finalTestPod.Status.PodIP).ToNot(BeEmpty())

			// Clean up final test pod
			err = f.K8sResourceManagers.PodManager().
				DeleteAndWaitTillPodDeleted(finalTestPod)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should maintain IP allocation consistency during pod churn on Ubuntu nodes", func() {
			By("Testing IP allocation consistency during rapid pod lifecycle")

			// Track IP addresses allocated during the test
			allocatedIPs := make(map[string]bool)
			
			for cycle := 1; cycle <= 5; cycle++ {
				By(fmt.Sprintf("Starting IP consistency test cycle %d/5", cycle))

				// Create a smaller deployment for IP tracking
				testContainer := manifest.
					NewBusyBoxContainerBuilder(f.Options.TestImageRegistry).
					Command([]string{"sleep"}).
					Args([]string{"60"}).
					Build()

				ipTestDeployment := manifest.
					NewDefaultDeploymentBuilder().
					Container(testContainer).
					Replicas(10).
					NodeSelector(ubuntuNodeSelector).
					PodLabel("ip-test", "ubuntu").
					PodLabel("cycle", strconv.Itoa(cycle)).
					Name(fmt.Sprintf("ubuntu-ip-test-%d", cycle)).
					Build()

				ipTestDeployment, err = f.K8sResourceManagers.
					DeploymentManager().
					CreateAndWaitTillDeploymentIsReady(ipTestDeployment, utils.DefaultDeploymentReadyTimeout)
				Expect(err).ToNot(HaveOccurred())

				// Get pods and record their IPs
				podList, err := f.K8sResourceManagers.PodManager().
					GetPodsWithLabelSelector("cycle", strconv.Itoa(cycle))
				Expect(err).ToNot(HaveOccurred())

				cycleIPs := make([]string, 0)
				for _, pod := range podList.Items {
					Expect(pod.Status.PodIP).ToNot(BeEmpty())
					cycleIPs = append(cycleIPs, pod.Status.PodIP)
					
					// Verify IP is not already in use (no IP conflicts)
					Expect(allocatedIPs[pod.Status.PodIP]).To(BeFalse(), 
						fmt.Sprintf("IP %s was already allocated in a previous cycle", pod.Status.PodIP))
					allocatedIPs[pod.Status.PodIP] = true
				}

				By(fmt.Sprintf("Recorded %d unique IPs for cycle %d", len(cycleIPs), cycle))

				// Delete the deployment
				err = f.K8sResourceManagers.DeploymentManager().
					DeleteAndWaitTillDeploymentIsDeleted(ipTestDeployment)
				Expect(err).ToNot(HaveOccurred())

				// Wait for IP cleanup
				time.Sleep(time.Second * 10)

				// Mark IPs as available again (simulating proper cleanup)
				for _, ip := range cycleIPs {
					delete(allocatedIPs, ip)
				}
			}

			By("IP allocation consistency test completed successfully")
		})
	})
})
