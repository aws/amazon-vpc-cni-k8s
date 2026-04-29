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
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	awsUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/aws/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/apps/v1"
	coreV1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
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
			deploymentBuilder := manifest.NewBusyBoxDeploymentBuilder(f.Options.TestImageRegistry).
				Container(container).
				Replicas(replicaCount).
				PodLabel(podLabelKey, podLabelVal).
				Build()

			var err error
			deployment, err = f.K8sResourceManagers.DeploymentManager().
				CreateAndWaitTillDeploymentIsReady(deploymentBuilder, utils.DefaultDeploymentReadyTimeout)
			Expect(err).ToNot(HaveOccurred())

			// Wait for deployment to settle, as if any pods restart, their pod IP will change between
			// the GET and the validation.
			time.Sleep(5 * time.Second)

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

				// Force client Job onto a DIFFERENT node than THIS target pod so that
				// traffic actually traverses the ENI and AWS SG enforcement applies
				// (same-node pod-to-pod bypasses ENI-level SG evaluation in the Linux bridge).
				// We only exclude the one target pod's node, not all target nodes, so the
				// Job can still schedule on 2-node clusters when targets span both nodes.
				testJob.Spec.Template.Spec.Affinity = &coreV1.Affinity{
					NodeAffinity: &coreV1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &coreV1.NodeSelector{
							NodeSelectorTerms: []coreV1.NodeSelectorTerm{{
								MatchExpressions: []coreV1.NodeSelectorRequirement{{
									Key:      "kubernetes.io/hostname",
									Operator: coreV1.NodeSelectorOpNotIn,
									Values:   []string{pod.Spec.NodeName},
								}},
							}},
						},
					},
				}

				_, err := f.K8sResourceManagers.JobManager().CreateAndWaitTillJobCompleted(testJob)
				logJobPodDiag(testJob.Name)
				logTargetENIDiag(pod)

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
			err := f.K8sResourceManagers.DeploymentManager().DeleteAndWaitTillDeploymentIsDeleted(deployment)
			Expect(err).ToNot(HaveOccurred())
		})

		Context("when connecting to reachable port", func() {
			BeforeEach(func() {
				port = customNetworkingSGOpenPort
				replicaCount = 10
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

	Context("when a custom-networking pod connects to an external endpoint on an egress-allowed port", func() {
		It("should succeed reaching 1.1.1.1:53", func() {
			testJob := manifest.NewDefaultJobBuilder().
				Container(manifest.NewNetCatAlpineContainer(f.Options.TestImageRegistry).
					Command([]string{"nc"}).
					Args([]string{"-z", "-v", "-w5", "1.1.1.1", "53"}).
					Build()).
				Name("external-reachability").
				Parallelism(1).
				Build()
			_, err := f.K8sResourceManagers.JobManager().CreateAndWaitTillJobCompleted(testJob)
			logJobPodDiag(testJob.Name)
			Expect(err).ToNot(HaveOccurred())
			Expect(f.K8sResourceManagers.JobManager().DeleteAndWaitTillJobIsDeleted(testJob)).ToNot(HaveOccurred())
		})
	})

	Context("when a custom-networking pod connects to the Kubernetes API ClusterIP", func() {
		It("should reach the API server via ClusterIP", func() {
			// Use the well-known kubernetes.default ClusterIP service which exists on every cluster.
			// Resolve the ClusterIP dynamically to avoid hardcoding.
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()

			kubeSvc, err := f.K8sResourceManagers.ServiceManager().GetService(ctx, "default", "kubernetes")
			Expect(err).ToNot(HaveOccurred())
			clusterIP := kubeSvc.Spec.ClusterIP

			testJob := manifest.NewDefaultJobBuilder().
				Container(manifest.NewNetCatAlpineContainer(f.Options.TestImageRegistry).
					Command([]string{"nc"}).
					Args([]string{"-z", "-v", "-w5", clusterIP, "443"}).
					Build()).
				Name("k8s-api-clusterip").
				Parallelism(1).
				Build()
			_, err = f.K8sResourceManagers.JobManager().CreateAndWaitTillJobCompleted(testJob)
			logJobPodDiag(testJob.Name)
			Expect(err).ToNot(HaveOccurred())
			Expect(f.K8sResourceManagers.JobManager().DeleteAndWaitTillJobIsDeleted(testJob)).ToNot(HaveOccurred())
		})
	})

	Context("when creating deployment on nodes that do not have ENIConfig", func() {
		JustBeforeEach(func() {
			By("deleting ENIConfig for all availability zones")
			for _, eniConfig := range eniConfigList {
				err := f.K8sResourceManagers.CustomResourceManager().DeleteResource(eniConfig)
				Expect(err).ToNot(HaveOccurred())
			}
		})

		JustAfterEach(func() {
			By("re-creating ENIConfig for all availability zones")
			for _, eniConfig := range eniConfigList {
				err := f.K8sResourceManagers.CustomResourceManager().CreateResource(eniConfig)
				Expect(err).ToNot(HaveOccurred())
			}
		})

		It("deployment should not become ready", func() {
			By("terminating instances")
			err := awsUtils.TerminateInstances(f)
			Expect(err).ToNot(HaveOccurred())

			// Nodes should be stuck in NotReady state since no ENIs could be attached and no pod
			// IP addresses are available.
			deployment := manifest.NewBusyBoxDeploymentBuilder(f.Options.TestImageRegistry).
				Replicas(2).
				Build()

			By("verifying deployment should not succeed")
			deployment, err = f.K8sResourceManagers.DeploymentManager().
				CreateAndWaitTillDeploymentIsReady(deployment, utils.ShortDeploymentReadyTimeout)
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
				err := f.K8sResourceManagers.CustomResourceManager().DeleteResource(eniConfig)
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
			err := awsUtils.TerminateInstances(f)
			Expect(err).ToNot(HaveOccurred())

			deployment := manifest.NewBusyBoxDeploymentBuilder(f.Options.TestImageRegistry).
				Replicas(2).
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

// logJobPodDiag prints pod name, node, IP, phase, and container logs for every
// pod belonging to the given Job. Useful for post-mortem when a Job fails.
func logJobPodDiag(jobName string) {
	pods, err := f.K8sResourceManagers.PodManager().GetPodsWithLabelSelector("job-name", jobName)
	if err != nil {
		return
	}
	for _, p := range pods.Items {
		logs, _ := f.K8sResourceManagers.PodManager().PodLogs(p.Namespace, p.Name)
		fmt.Fprintf(GinkgoWriter, "[DIAG] pod=%s node=%s ip=%s phase=%s\n%s\n",
			p.Name, p.Spec.NodeName, p.Status.PodIP, p.Status.Phase, logs)
	}
}

// logTargetENIDiag looks up the EC2 ENI that owns the target pod's IP and
// prints its ENI ID, subnet, and security groups.
func logTargetENIDiag(pod coreV1.Pod) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	node := &coreV1.Node{}
	if err := f.K8sClient.Get(ctx, types.NamespacedName{Name: pod.Spec.NodeName}, node); err != nil {
		return
	}
	instance, err := f.CloudServices.EC2().DescribeInstance(ctx, k8sUtils.GetInstanceIDFromNode(*node))
	if err != nil {
		return
	}
	for _, nic := range instance.NetworkInterfaces {
		for _, pip := range nic.PrivateIpAddresses {
			if pip.PrivateIpAddress != nil && *pip.PrivateIpAddress == pod.Status.PodIP {
				var sgs []string
				for _, g := range nic.Groups {
					sgs = append(sgs, *g.GroupId)
				}
				fmt.Fprintf(GinkgoWriter, "[DIAG] target ENI=%s subnet=%s SGs=%v\n",
					*nic.NetworkInterfaceId, *nic.SubnetId, sgs)
				return
			}
		}
	}
}
