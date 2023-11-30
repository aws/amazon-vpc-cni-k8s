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

	"github.com/aws/amazon-vpc-cni-k8s/test/agent/pkg/input"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/agent"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"

	"github.com/aws/amazon-vpc-resource-controller-k8s/apis/vpcresources/v1beta1"
	vpcControllerFW "github.com/aws/amazon-vpc-resource-controller-k8s/test/framework/manifest"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

type TestType int

const (
	NetworkingTearDownSucceeds TestType = iota
	NetworkingSetupSucceeds
)

var _ = Describe("Security Group for Pods Test", func() {
	var (
		// The Pod labels for client and server in order to retrieve the
		// client and server Pods belonging to a Deployment/Jobs
		labelKey           = "app"
		serverPodLabelVal  = "server-pod"
		clientPodLabelVal  = "client-pod"
		busyboxPodLabelVal = "busybox-pod"
		// The Security Group Policy take list of Pod Label Value and if the
		// Pod has any label in the list, it should get Branch ENI
		branchPodLabelVal       []string
		serverDeploymentBuilder *manifest.DeploymentBuilder
		securityGroupPolicy     *v1beta1.SecurityGroupPolicy
	)

	JustBeforeEach(func() {
		By("creating test namespace")
		f.K8sResourceManagers.NamespaceManager().
			CreateNamespace(utils.DefaultTestNamespace)

		serverDeploymentBuilder = manifest.NewDefaultDeploymentBuilder().
			Name("traffic-server")

		securityGroupPolicy, err = vpcControllerFW.NewSGPBuilder().
			Namespace(utils.DefaultTestNamespace).
			Name("test-sgp").
			SecurityGroup([]string{securityGroupId}).
			PodMatchExpression(labelKey, metaV1.LabelSelectorOpIn, branchPodLabelVal...).
			Build()
		Expect(err).ToNot(HaveOccurred())

		By("creating the Security Group Policy")
		err = f.K8sResourceManagers.CustomResourceManager().CreateResource(securityGroupPolicy)
		Expect(err).ToNot(HaveOccurred())
	})

	JustAfterEach(func() {
		By("deleting test namespace")
		f.K8sResourceManagers.NamespaceManager().
			DeleteAndWaitTillNamespaceDeleted(utils.DefaultTestNamespace)

		By("Deleting Security Group Policy")
		f.K8sResourceManagers.CustomResourceManager().DeleteResource(securityGroupPolicy)

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
				IsV6Enabled:                    !isIPv4Cluster,
			}

			By("performing traffic test")
			successRate, err := trafficTester.TestTraffic()
			Expect(err).ToNot(HaveOccurred())
			Expect(successRate).Should(BeNumerically(">=", float64(99)))
		})
	})

	Context("when testing traffic between branch ENI and branch ENI pods", func() {
		BeforeEach(func() {
			// Both the Server and Client Pods will get Branch ENI
			branchPodLabelVal = []string{serverPodLabelVal, clientPodLabelVal}

			// Allow Ingress on cluster security group so client pods can communicate with metric pod
			// 8080: metric-pod listener port
			By("Adding an additional Ingress Rule on NodeSecurityGroupID to allow client-to-metric traffic")
			if isIPv4Cluster {
				err := f.CloudServices.EC2().AuthorizeSecurityGroupIngress(clusterSGID, "TCP", metricsPort, metricsPort, v4Zero)
				Expect(err).ToNot(HaveOccurred())
			} else {
				err := f.CloudServices.EC2().AuthorizeSecurityGroupIngress(clusterSGID, "TCP", metricsPort, metricsPort, v6Zero)
				Expect(err).ToNot(HaveOccurred())
			}
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
				IsV6Enabled:                    !isIPv4Cluster,
			}

			successRate, err := t.TestTraffic()
			Expect(err).ToNot(HaveOccurred())
			Expect(successRate).Should(BeNumerically(">=", float64(99)))
		})

		AfterEach(func() {
			// Revoke the Ingress rule for traffic from client pods added to Node Security Group
			By("Revoking the additional Ingress rule added to allow client-to-metric traffic")
			if isIPv4Cluster {
				err := f.CloudServices.EC2().RevokeSecurityGroupIngress(clusterSGID, "TCP", metricsPort, metricsPort, v4Zero)
				Expect(err).ToNot(HaveOccurred())
			} else {
				err := f.CloudServices.EC2().RevokeSecurityGroupIngress(clusterSGID, "TCP", metricsPort, metricsPort, v6Zero)
				Expect(err).ToNot(HaveOccurred())
			}
		})
	})

	Context("when testing traffic to a port on Branch ENI that is not open", func() {
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
				IsV6Enabled:                    !isIPv4Cluster,
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
				ProbeHandler: v1.ProbeHandler{
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
				RestartPolicy(v1.RestartPolicyAlways).
				Build()

			By("creating branch ENI pod with liveness probe")
			pod, err := f.K8sResourceManagers.PodManager().CreateAndWaitTillRunning(pod)
			Expect(err).ToNot(HaveOccurred())

			ValidatePodsHaveBranchENI(v1.PodList{Items: []v1.Pod{*pod}})

			timeAfterLivelinessProbeFails := initialDelay + (periodSecond * failureCount) + 10

			By("waiting for the liveness probe to succeed/fail")
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

	Context("Verify HostNetworking", func() {
		BeforeEach(func() {
			// BusyBox Pods will get Branch ENI
			branchPodLabelVal = []string{busyboxPodLabelVal}
		})
		It("Deploy BusyBox Pods with branch ENI and verify HostNetworking", func() {
			// Pin deployment to primary node
			deployment := manifest.NewBusyBoxDeploymentBuilder(f.Options.TestImageRegistry).
				Replicas(totalBranchInterface/numNodes).
				PodLabel(labelKey, busyboxPodLabelVal).
				NodeName(targetNode.Name).
				Build()

			By("creating a deployment to launch pod using Branch ENI")
			_, err = f.K8sResourceManagers.DeploymentManager().
				CreateAndWaitTillDeploymentIsReady(deployment, utils.DefaultDeploymentReadyTimeout)
			Expect(err).ToNot(HaveOccurred())

			By("getting the list of pods using BranchENI")
			podList, err := f.K8sResourceManagers.
				PodManager().
				GetPodsWithLabelSelector(labelKey, busyboxPodLabelVal)
			Expect(err).ToNot(HaveOccurred())

			By("generating the pod networking validation input to be passed to tester")
			input, err := GetPodNetworkingValidationInput(podList).Serialize()
			Expect(err).NotTo(HaveOccurred())

			By("validating host networking setup is setup correctly")
			ValidateHostNetworking(NetworkingSetupSucceeds, input)

			By("deleting the deployment to test teardown")
			err = f.K8sResourceManagers.DeploymentManager().
				DeleteAndWaitTillDeploymentIsDeleted(deployment)
			Expect(err).ToNot(HaveOccurred())

			By("waiting to allow CNI to tear down networking for terminated pods")
			time.Sleep(time.Second * 60)

			By("validating host networking is teared down correctly")
			ValidateHostNetworking(NetworkingTearDownSucceeds, input)
		})
	})
})

func GetPodNetworkingValidationInput(podList v1.PodList) input.PodNetworkingValidationInput {
	var ipFamily string
	if isIPv4Cluster {
		ipFamily = "IPv4"
	} else {
		ipFamily = "IPv6"
	}
	ip := input.PodNetworkingValidationInput{
		IPFamily:    ipFamily,
		VethPrefix:  "vlan",
		PodList:     []input.Pod{},
		ValidateMTU: true,
		MTU:         9001,
	}

	for _, pod := range podList.Items {
		if isIPv4Cluster {
			ip.PodList = append(ip.PodList, input.Pod{
				PodName:        pod.Name,
				PodNamespace:   pod.Namespace,
				PodIPv4Address: pod.Status.PodIP,
			})
		} else {
			ip.PodList = append(ip.PodList, input.Pod{
				PodName:        pod.Name,
				PodNamespace:   pod.Namespace,
				PodIPv6Address: pod.Status.PodIP,
			})

		}
	}
	return ip
}

func ValidateHostNetworking(testType TestType, podValidationInputString string) {
	testerArgs := []string{fmt.Sprintf("-pod-networking-validation-input=%s",
		podValidationInputString)}

	if NetworkingSetupSucceeds == testType {
		testerArgs = append(testerArgs, "-test-setup=true", "-test-ppsg=true")
	} else if NetworkingTearDownSucceeds == testType {
		testerArgs = append(testerArgs, "-test-cleanup=true", "-test-ppsg=true")
	}

	testContainer := manifest.NewTestHelperContainer(f.Options.TestImageRegistry).
		Command([]string{"./networking"}).
		Args(testerArgs).
		Build()

	// Pin pod to primary node
	testPod := manifest.NewDefaultPodBuilder().
		Container(testContainer).
		NodeName(targetNode.Name).
		HostNetwork(true).
		Build()

	By("creating pod to test host networking setup")
	testPod, err := f.K8sResourceManagers.PodManager().
		CreateAndWaitTillPodCompleted(testPod)
	Expect(err).ToNot(HaveOccurred())

	logs, errLogs := f.K8sResourceManagers.PodManager().
		PodLogs(testPod.Namespace, testPod.Name)
	Expect(errLogs).ToNot(HaveOccurred())

	fmt.Fprintln(GinkgoWriter, logs)

	By("deleting the host networking setup pod")
	err = f.K8sResourceManagers.PodManager().
		DeleteAndWaitTillPodDeleted(testPod)
	Expect(err).ToNot(HaveOccurred())
}

func ValidatePodsHaveBranchENI(podList v1.PodList) error {
	for _, pod := range podList.Items {
		if val, ok := pod.Annotations["vpc.amazonaws.com/pod-eni"]; ok {
			type ENIDetails struct {
				IPV4Addr string `json:"privateIp"`
				IPV6Addr string `json:"ipv6addr"`
				ID       string `json:"eniId"`
			}
			var eniList []ENIDetails
			err := json.Unmarshal([]byte(val), &eniList)
			if err != nil {
				return fmt.Errorf("failed to unmarshall the branch ENI annotation %v", err)
			}

			if isIPv4Cluster {
				if eniList[0].IPV4Addr != pod.Status.PodIP {
					return fmt.Errorf("expected the pod to have IP %s but recieved %s",
						eniList[0].IPV4Addr, pod.Status.PodIP)
				}
			} else {
				if eniList[0].IPV6Addr != pod.Status.PodIP {
					return fmt.Errorf("expected the pod to have IP %s but recieved %s",
						eniList[0].IPV6Addr, pod.Status.PodIP)
				}

			}
			By(fmt.Sprintf("validating pod %s has branch ENI %s", pod.Name, eniList[0].ID))

		} else {
			return fmt.Errorf("failed to validate pod %v", pod)
		}
	}
	return nil
}
