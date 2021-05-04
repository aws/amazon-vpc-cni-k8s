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

package custom_networking

import (
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/apps/v1"
	coreV1 "k8s.io/api/core/v1"
)

var _ = Describe("Custom Networking Test", func() {

	var (
		deployment    *v1.Deployment
		podList       coreV1.PodList
		podLabelKey   string
		podLabelVal   string
		port          int
		replicaCount  int
		shouldConnect bool
	)

	Context("when creating deployment targeted using ENIConfig", func() {
		BeforeEach(func() {
			podLabelKey = "role"
			podLabelVal = "custom-networking-test"
		})

		JustBeforeEach(func() {
			container := manifest.NewNetCatAlpineContainer().
				Command([]string{"nc"}).
				Args([]string{"-k", "-l", strconv.Itoa(port)}).
				Build()

			deployment = manifest.NewBusyBoxDeploymentBuilder().
				Container(container).
				Replicas(replicaCount).
				NodeSelector(nodeGroupProperties.NgLabelKey, nodeGroupProperties.NgLabelVal).
				PodLabel(podLabelKey, podLabelVal).
				Build()

			deployment, err = f.K8sResourceManagers.DeploymentManager().
				CreateAndWaitTillDeploymentIsReady(deployment, utils.DefaultDeploymentReadyTimeout)
			Expect(err).ToNot(HaveOccurred())

			podList, err = f.K8sResourceManagers.PodManager().
				GetPodsWithLabelSelector(podLabelKey, podLabelVal)
			Expect(err).ToNot(HaveOccurred())

			// TODO: Parallelize the validation
			for _, pod := range podList.Items {
				By(fmt.Sprintf("verifying pod's IP %s address belong to the CIDR range %s",
					pod.Status.PodIP, cidrRange.String()))

				ip := net.ParseIP(pod.Status.PodIP)
				Expect(cidrRange.Contains(ip)).To(BeTrue())

				testContainer := manifest.NewNetCatAlpineContainer().
					Command([]string{"nc"}).
					Args([]string{"-v", "-w2", pod.Status.PodIP, strconv.Itoa(port)}).
					Build()

				testJob := manifest.NewDefaultJobBuilder().
					Container(testContainer).
					Name("test-pod").
					Parallelism(1).
					Build()

				_, err := f.K8sResourceManagers.JobManager().
					CreateAndWaitTillJobCompleted(testJob)
				if shouldConnect {
					By("verifying connection to pod succeeds on port " + strconv.Itoa(port))
					Expect(err).ToNot(HaveOccurred())
				} else {
					By("verifying connection to pod fails on port " + strconv.Itoa(port))
					Expect(err).To(HaveOccurred())
				}

				err = f.K8sResourceManagers.JobManager().
					DeleteAndWaitTillJobIsDeleted(testJob)
				Expect(err).ToNot(HaveOccurred())
			}
		})

		JustAfterEach(func() {
			err = f.K8sResourceManagers.DeploymentManager().
				DeleteAndWaitTillDeploymentIsDeleted(deployment)
			Expect(err).ToNot(HaveOccurred())
		})

		Context("when connecting to reachable port", func() {
			BeforeEach(func() {
				port = customNetworkingSGOpenPort
				replicaCount = 30
				shouldConnect = true
			})

			It("should connect", func() {})
		})

		Context("when connecting to unreachable port", func() {
			BeforeEach(func() {
				port = 8081
				replicaCount = 1
				shouldConnect = false
			})

			It("should fail to connect", func() {})
		})
	})

	Context("when creating deployment on nodes that don't have ENIConfig", func() {
		JustBeforeEach(func() {
			By("deleting ENIConfig for all availability zones")
			for _, eniConfig := range eniConfigList {
				err = f.K8sResourceManagers.CustomResourceManager().
					DeleteResource(eniConfig)
				Expect(err).ToNot(HaveOccurred())
			}
		})

		JustAfterEach(func() {
			By("re-creating ENIConfig for all availability zones")
			for _, eniConfig := range eniConfigList {
				err = f.K8sResourceManagers.CustomResourceManager().
					CreateResource(eniConfig)
				Expect(err).ToNot(HaveOccurred())
			}
		})

		It("deployment should not become ready", func() {
			By("getting the list of nodes created")
			nodeList, err := f.K8sResourceManagers.NodeManager().
				GetNodes(nodeGroupProperties.NgLabelKey, nodeGroupProperties.NgLabelVal)
			Expect(err).ToNot(HaveOccurred())

			var instanceIDs []string
			for _, node := range nodeList.Items {
				instanceIDs = append(instanceIDs, k8sUtils.GetInstanceIDFromNode(node))
			}

			By("terminating all the nodes")
			err = f.CloudServices.EC2().TerminateInstance(instanceIDs)
			Expect(err).ToNot(HaveOccurred())

			By("waiting for the node to be removed")
			time.Sleep(time.Second * 120)

			By("waiting for all nodes to become ready")
			err = f.K8sResourceManagers.NodeManager().
				WaitTillNodesReady(nodeGroupProperties.NgLabelKey, nodeGroupProperties.NgLabelVal,
					nodeGroupProperties.AsgSize)
			Expect(err).ToNot(HaveOccurred())

			deployment := manifest.NewBusyBoxDeploymentBuilder().
				Replicas(2).
				NodeSelector(nodeGroupProperties.NgLabelKey, nodeGroupProperties.NgLabelVal).
				Build()

			By("verifying deployment should not succeed")
			deployment, err = f.K8sResourceManagers.DeploymentManager().
				CreateAndWaitTillDeploymentIsReady(deployment, utils.DefaultDeploymentReadyTimeout)
			Expect(err).To(HaveOccurred())

			By("deleting the failed deployment")
			err = f.K8sResourceManagers.DeploymentManager().
				DeleteAndWaitTillDeploymentIsDeleted(deployment)
			Expect(err).ToNot(HaveOccurred())
		})
	})
})
