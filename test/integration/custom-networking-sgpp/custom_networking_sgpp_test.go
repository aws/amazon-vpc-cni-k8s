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

package custom_networking_sgpp

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/test/agent/pkg/input"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/agent"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"

	"github.com/aws/amazon-vpc-resource-controller-k8s/apis/vpcresources/v1beta1"
	vpcControllerFW "github.com/aws/amazon-vpc-resource-controller-k8s/test/framework/manifest"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type TestType int

var err error

const (
	NetworkingTearDownSucceeds TestType = iota
	NetworkingSetupSucceeds
	// Custom Networking does not support IPv6 clusters
	isIPv4Cluster = true
)

// NOTE: This file is a near identical copy of $PROJECT_ROOT/test/integration/pod-eni/security_group_per_pod_test.go, but it excludes the DISABLE_TCP_EARLY_DEMUX tests.

var _ = Describe("Custom Networking + Security Groups for Pods Test", func() {
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
			SecurityGroup([]string{podEniSGID}).
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
				ServerPort:                     podEniOpenPort,
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
			// TODO: uncomment after Custom Networking SGID clones cluster SGID
			//By("Adding an additional Ingress Rule on NodeSecurityGroupID to allow client-to-metric traffic")
			//if isIPv4Cluster {
			//	err := f.CloudServices.EC2().AuthorizeSecurityGroupIngress(customNetworkingSGID, "TCP", metricsPort, metricsPort, v4Zero)
			//	Expect(err).ToNot(HaveOccurred())
			//} else {
			//	err := f.CloudServices.EC2().AuthorizeSecurityGroupIngress(customNetworkingSGID, "TCP", metricsPort, metricsPort, v6Zero)
			//	Expect(err).ToNot(HaveOccurred())
			//}
		})

		It("should have 99%+ success rate", func() {
			t := agent.TrafficTest{
				Framework:                      f,
				TrafficServerDeploymentBuilder: serverDeploymentBuilder,
				ServerPort:                     podEniOpenPort,
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

		// TODO: uncomment after Custom Networking SGID clones cluster SGID
		//AfterEach(func() {
		//	// Revoke the Ingress rule for traffic from client pods added to Node Security Group
		//	By("Revoking the additional Ingress rule added to allow client-to-metric traffic")
		//	if isIPv4Cluster {
		//		err := f.CloudServices.EC2().RevokeSecurityGroupIngress(clusterSGID, "TCP", metricsPort, metricsPort, v4Zero)
		//		Expect(err).ToNot(HaveOccurred())
		//	} else {
		//		err := f.CloudServices.EC2().RevokeSecurityGroupIngress(clusterSGID, "TCP", metricsPort, metricsPort, v6Zero)
		//		Expect(err).ToNot(HaveOccurred())
		//	}
		//})
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
