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

package pod_eni

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/agent"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"

	vpcControllerFW "github.com/aws/amazon-vpc-resource-controller-k8s/test/framework/manifest"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

var _ = Describe("Security Group for Pods Test", func() {
	var (
		// The Pod labels for client and server in order to retrieve the
		// client and server Pods belonging to a Deployment/Jobs
		labelKey          = "app"
		serverPodLabelVal = "server-pod"
		clientPodLabelVal = "client-pod"
		// The Security Group Policy take list of Pod Label Value and if the
		// Pod has any label in the list, it should get Branch ENI
		branchPodLabelVal       []string
		serverDeploymentBuilder *manifest.DeploymentBuilder
	)

	JustBeforeEach(func() {
		By("creating test namespace")
		f.K8sResourceManagers.NamespaceManager().
			CreateNamespace(utils.DefaultTestNamespace)

		serverDeploymentBuilder = manifest.NewDefaultDeploymentBuilder().
			Name("traffic-server").
			NodeSelector(nodeGroupProperties.NgLabelKey, nodeGroupProperties.NgLabelVal)

		sgp, err := vpcControllerFW.NewSGPBuilder().
			Namespace(utils.DefaultTestNamespace).
			Name("test-sgp").
			SecurityGroup([]string{securityGroupId}).
			PodMatchExpression(labelKey, metaV1.LabelSelectorOpIn, branchPodLabelVal...).
			Build()
		Expect(err).ToNot(HaveOccurred())

		By("creating the Security Group Policy")
		err = f.K8sResourceManagers.
			CustomResourceManager().CreateResource(sgp)
		Expect(err).ToNot(HaveOccurred())
	})

	JustAfterEach(func() {
		By("deleting test namespace")
		f.K8sResourceManagers.NamespaceManager().
			DeleteAndWaitTillNamespaceDeleted(utils.DefaultTestNamespace)

		By("waiting for the branch ENI to be cooled down")
		time.Sleep(time.Second * 60)
	})

	Context("when testing traffic between branch ENI pods and regular pods", func() {
		BeforeEach(func() {
			// Only the Server Pods will get Branch ENI
			branchPodLabelVal = []string{serverPodLabelVal}
		})

		It("should have 99%+ success rate", func() {
			trafficTester := agent.TrafficTest{
				Framework:                      f,
				TrafficServerDeploymentBuilder: serverDeploymentBuilder,
				ServerPort:                     openPort,
				ServerProtocol:                 "tcp",
				ClientCount:                    20,
				ServerCount:                    totalBranchInterface,
				ServerPodLabelKey:              labelKey,
				ServerPodLabelVal:              serverPodLabelVal,
				ClientPodLabelKey:              labelKey,
				ClientPodLabelVal:              clientPodLabelVal,
				ValidateServerPods:             ValidatePodsHaveBranchENI,
			}

			successRate, err := trafficTester.TestTraffic()
			Expect(err).ToNot(HaveOccurred())
			Expect(successRate).Should(BeNumerically(">=", float64(99)))
		})
	})

	Context("when testing traffic between branch ENI and branch ENI pods", func() {
		BeforeEach(func() {
			// Both the Server and Client Pods will get Branch ENI
			branchPodLabelVal = []string{serverPodLabelVal, clientPodLabelVal}
		})

		It("should have 99%+ success rate", func() {
			t := agent.TrafficTest{
				Framework:                      f,
				TrafficServerDeploymentBuilder: serverDeploymentBuilder,
				ServerPort:                     openPort,
				ServerProtocol:                 "tcp",
				ClientCount:                    totalBranchInterface / 2,
				ServerCount:                    totalBranchInterface / 2,
				ServerPodLabelKey:              labelKey,
				ServerPodLabelVal:              serverPodLabelVal,
				ClientPodLabelKey:              labelKey,
				ClientPodLabelVal:              clientPodLabelVal,
				ValidateServerPods:             ValidatePodsHaveBranchENI,
				ValidateClientPods:             ValidatePodsHaveBranchENI,
			}

			successRate, err := t.TestTraffic()
			Expect(err).ToNot(HaveOccurred())
			Expect(successRate).Should(BeNumerically(">=", float64(99)))
		})
	})

	Context("when testing traffic to a port on Branch ENI that's not open", func() {
		BeforeEach(func() {
			// Only the Server Pods will get Branch ENI
			branchPodLabelVal = []string{serverPodLabelVal}
		})

		It("should have 0% success rate", func() {
			t := agent.TrafficTest{
				Framework:                      f,
				TrafficServerDeploymentBuilder: serverDeploymentBuilder,
				ServerPort:                     2271,
				ServerProtocol:                 "tcp",
				ClientCount:                    2,
				ServerCount:                    5,
				ServerPodLabelKey:              labelKey,
				ServerPodLabelVal:              serverPodLabelVal,
				ClientPodLabelKey:              labelKey,
				ClientPodLabelVal:              clientPodLabelVal,
				ValidateServerPods:             ValidatePodsHaveBranchENI,
			}

			successRate, err := t.TestTraffic()
			Expect(err).ToNot(HaveOccurred())
			Expect(successRate).Should(Equal(float64(0)))
		})
	})

	Context("when toggling DISABLE_TCP_EARLY_DEMUX", func() {
		var (
			// Parameters for the liveliness probe
			initialDelay = 10
			periodSecond = 10
			failureCount = 3
			// If liveliness probe will fail then the container would have
			// restarted
			containerRestartCount = 0
			// Value for the Environment variable DISABLE_TCP_EARLY_DEMUX
			disableTCPEarlyDemux string
		)

		JustBeforeEach(func() {
			k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName,
				utils.AwsNodeNamespace, utils.AWSInitContainerName,
				map[string]string{"DISABLE_TCP_EARLY_DEMUX": disableTCPEarlyDemux})

			tcpProbe := &v1.Probe{
				Handler: v1.Handler{
					TCPSocket: &v1.TCPSocketAction{
						Port: intstr.IntOrString{IntVal: 80},
					},
				},
				InitialDelaySeconds: int32(initialDelay),
				PeriodSeconds:       int32(periodSecond),
				FailureThreshold:    int32(failureCount),
			}

			port := v1.ContainerPort{
				ContainerPort: 80,
			}

			container := manifest.NewCurlContainer().
				LivenessProbe(tcpProbe).
				Image("nginx").
				Port(port).
				Build()

			pod := manifest.NewDefaultPodBuilder().
				Name("liveliness-pod").
				Container(container).
				PodLabel(labelKey, serverPodLabelVal).
				NodeSelector(nodeGroupProperties.NgLabelKey, nodeGroupProperties.NgLabelVal).
				RestartPolicy(v1.RestartPolicyAlways).
				Build()

			By("creating branch ENI pod with liveliness probe")
			pod, err := f.K8sResourceManagers.PodManager().
				CreatAndWaitTillRunning(pod)
			Expect(err).ToNot(HaveOccurred())

			ValidatePodsHaveBranchENI(v1.PodList{Items: []v1.Pod{*pod}})

			timeAfterLivelinessProbeFails := initialDelay + (periodSecond * failureCount) + 10

			By("waiting for the liveliness probe to succeed/fail")
			time.Sleep(time.Second * time.Duration(timeAfterLivelinessProbeFails))

			By("getting the updated branch ENI pod")
			pod, err = f.K8sResourceManagers.PodManager().GetPod(pod.Namespace, pod.Name)
			Expect(err).ToNot(HaveOccurred())

			By(fmt.Sprintf("verifying the container restarted %d times", containerRestartCount))
			Expect(int(pod.Status.ContainerStatuses[0].RestartCount)).
				To(Equal(containerRestartCount))
		})

		JustAfterEach(func() {
			k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName,
				utils.AwsNodeNamespace, utils.AWSInitContainerName,
				map[string]string{"DISABLE_TCP_EARLY_DEMUX": "false"})
		})

		Context("when disabling DISABLE_TCP_EARLY_DEMUX", func() {
			BeforeEach(func() {
				containerRestartCount = 1
				disableTCPEarlyDemux = "false"
			})
			It("TCP liveness probe will fail", func() {})
		})

		Context("when enabling DISABLE_TCP_EARLY_DEMUX", func() {
			BeforeEach(func() {
				containerRestartCount = 0
				disableTCPEarlyDemux = "true"
			})
			It("TCP liveness probe will succeed", func() {})
		})
	})
})

func ValidatePodsHaveBranchENI(podList v1.PodList) error {
	for _, pod := range podList.Items {
		if val, ok := pod.Annotations["vpc.amazonaws.com/pod-eni"]; ok {
			type ENIDetails struct {
				IPV4Addr string `json:"privateIp"`
				ID       string `json:"eniId"`
			}
			var eniList []ENIDetails
			err := json.Unmarshal([]byte(val), &eniList)
			if err != nil {
				return fmt.Errorf("failed to unmarshall the branch ENI annotation %v", err)
			}

			if eniList[0].IPV4Addr != pod.Status.PodIP {
				return fmt.Errorf("expected the pod to have IP %s but recieved %s",
					eniList[0].IPV4Addr, pod.Status.PodIP)
			}

			By(fmt.Sprintf("validating pod %s has branch ENI %s", pod.Name, eniList[0].ID))

		} else {
			return fmt.Errorf("failed to validate pod %v", pod)
		}
	}
	return nil
}
