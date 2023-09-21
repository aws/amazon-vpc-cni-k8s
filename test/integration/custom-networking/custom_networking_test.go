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

	awsUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/aws/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"

	. "github.com/onsi/ginkgo/v2"
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
			container := manifest.NewNetCatAlpineContainer(f.Options.TestImageRegistry).
				Command([]string{"nc"}).
				Args([]string{"-k", "-l", strconv.Itoa(port)}).
				Build()

			deployment = manifest.NewBusyBoxDeploymentBuilder(f.Options.TestImageRegistry).
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

				testContainer := manifest.NewNetCatAlpineContainer(f.Options.TestImageRegistry).
					Command([]string{"nc"}).
					Args([]string{"-v", "-w3", pod.Status.PodIP, strconv.Itoa(port)}).
					Build()

				testJob := manifest.NewDefaultJobBuilder().
					Container(testContainer).
					Name("test-pod").
					Parallelism(1).
					Build()

				_, err := f.K8sResourceManagers.JobManager().CreateAndWaitTillJobCompleted(testJob)
				if shouldConnect {
					By("verifying connection to pod succeeds on port " + strconv.Itoa(port))
					Expect(err).ToNot(HaveOccurred())
				} else {
					By("verifying connection to pod fails on port " + strconv.Itoa(port))
					Expect(err).To(HaveOccurred())
				}

				err = f.K8sResourceManagers.JobManager().DeleteAndWaitTillJobIsDeleted(testJob)
				Expect(err).ToNot(HaveOccurred())
			}
		})

		JustAfterEach(func() {
			err = f.K8sResourceManagers.DeploymentManager().DeleteAndWaitTillDeploymentIsDeleted(deployment)
			Expect(err).ToNot(HaveOccurred())
		})

		Context("when connecting to reachable port", func() {
			BeforeEach(func() {
				port = customNetworkingSGOpenPort
				replicaCount = 16
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

	Context("when creating deployment on nodes that do not have ENIConfig", func() {
		JustBeforeEach(func() {
			By("deleting ENIConfig for all availability zones")
			for _, eniConfig := range eniConfigList {
				err = f.K8sResourceManagers.CustomResourceManager().DeleteResource(eniConfig)
				Expect(err).ToNot(HaveOccurred())
			}
		})

		JustAfterEach(func() {
			By("re-creating ENIConfig for all availability zones")
			for _, eniConfig := range eniConfigList {
				err = f.K8sResourceManagers.CustomResourceManager().CreateResource(eniConfig)
				Expect(err).ToNot(HaveOccurred())
			}
		})

		It("deployment should not become ready", func() {
			By("terminating instances")
			err := awsUtils.TerminateInstances(f, nodeGroupProperties.NgLabelKey, nodeGroupProperties.NgLabelVal)
			Expect(err).ToNot(HaveOccurred())

			// Nodes should be stuck in NotReady state since no ENIs could be attached and no pod
			// IP addresses are available.
			deployment := manifest.NewBusyBoxDeploymentBuilder(f.Options.TestImageRegistry).
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

	Context("when creating ENIConfigs without security groups", func() {
		JustBeforeEach(func() {
			By("deleting ENIConfig for each availability zone")
			for _, eniConfig := range eniConfigList {
				err = f.K8sResourceManagers.CustomResourceManager().DeleteResource(eniConfig)
				Expect(err).ToNot(HaveOccurred())
			}
			By("re-creating ENIConfigs with no security group")
			eniConfigList = nil
			for _, eniConfigBuilder := range eniConfigBuilderList {
				eniConfigBuilder.SecurityGroup(nil)
				eniConfig, err := eniConfigBuilder.Build()
				eniConfigList = append(eniConfigList, eniConfig.DeepCopy())

				err = f.K8sResourceManagers.CustomResourceManager().CreateResource(eniConfig)
				Expect(err).ToNot(HaveOccurred())
			}
		})

		It("deployment should become ready", func() {
			By("terminating instances")
			err := awsUtils.TerminateInstances(f, nodeGroupProperties.NgLabelKey, nodeGroupProperties.NgLabelVal)
			Expect(err).ToNot(HaveOccurred())

			deployment := manifest.NewBusyBoxDeploymentBuilder(f.Options.TestImageRegistry).
				Replicas(2).
				NodeSelector(nodeGroupProperties.NgLabelKey, nodeGroupProperties.NgLabelVal).
				Build()

			By("verifying deployment succeeds")
			deployment, err = f.K8sResourceManagers.DeploymentManager().
				CreateAndWaitTillDeploymentIsReady(deployment, utils.DefaultDeploymentReadyTimeout)
			Expect(err).ToNot(HaveOccurred())

			By("deleting the deployment")
			err = f.K8sResourceManagers.DeploymentManager().
				DeleteAndWaitTillDeploymentIsDeleted(deployment)
			Expect(err).ToNot(HaveOccurred())
		})
	})
})
