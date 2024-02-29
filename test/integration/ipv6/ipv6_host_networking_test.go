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

package ipv6

import (
	"fmt"
	"strconv"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/test/agent/pkg/input"
	v1 "k8s.io/api/core/v1"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type TestType int

const (
	NetworkingTearDownSucceeds TestType = iota
	NetworkingTearDownFails
	NetworkingSetupSucceeds
	NetworkingSetupFails
)

const (
	AWS_VPC_ENI_MTU            = "AWS_VPC_ENI_MTU"
	AWS_VPC_K8S_CNI_VETHPREFIX = "AWS_VPC_K8S_CNI_VETHPREFIX"
	POD_MTU                    = "POD_MTU"
	NEW_MTU_VAL                = 1300
	NEW_POD_MTU                = 1280
	NEW_VETH_PREFIX            = "veth"
	DEFAULT_MTU_VAL            = "9001"
	DEFAULT_VETH_PREFIX        = "eni"
	podLabelKey                = "app"
	podLabelVal                = "host-networking-test"
)

var err error

var _ = Describe("[CANARY] test ipv6 host netns setup", func() {

	Context("when pods using IP from primary ENI are created", func() {
		AfterEach(func() {
			k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace, utils.AwsNodeName, map[string]string{
				AWS_VPC_ENI_MTU:            DEFAULT_MTU_VAL,
				AWS_VPC_K8S_CNI_VETHPREFIX: DEFAULT_VETH_PREFIX,
			})
			k8sUtils.RemoveVarFromDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName,
				utils.AwsNodeNamespace, utils.AwsNodeName, map[string]struct{}{
					"POD_MTU": {},
				})
			// After updating daemonset pod, we must wait until conflist is updated so that container-runtime calls CNI ADD with the latest VETH prefix and MTU.
			// Otherwise, the stale value can cause failures in future test cases.
			time.Sleep(utils.PollIntervalMedium)
		})
		It("should have correct host netns setup when running and cleaned up once terminated", func() {
			deployment := manifest.NewBusyBoxDeploymentBuilder(f.Options.TestImageRegistry).
				Replicas(2).
				PodLabel(podLabelKey, podLabelVal).
				NodeName(primaryNode.Name).
				Build()

			By("creating a deployment to launch pod using primary ENI IPs")
			deployment, err = f.K8sResourceManagers.DeploymentManager().
				CreateAndWaitTillDeploymentIsReady(deployment, utils.DefaultDeploymentReadyTimeout)
			Expect(err).ToNot(HaveOccurred())

			By("getting the list of IPv6 pods using IPs from primary ENI")
			podList := GetIPv6Pods(podLabelKey, podLabelVal)

			Expect(len(podList.Items)).Should(BeNumerically(">", 0))

			By("generating the pod networking validation input to be passed to tester")
			input, err := GetIPv6PodNetworkingValidationInput(podList).Serialize()
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

		Context("Validate Host Networking setup after changing Veth Prefix and", func() {
			It("ENI MTU", func() {
				mtuValidationTest(false, NEW_MTU_VAL)
			})
			It("POD MTU", func() {
				mtuValidationTest(true, NEW_POD_MTU)
			})
		})
	})

	Context("when host netns setup is tested on invalid input", func() {
		It("tester pod should error out", func() {
			By("creating a single pod on the test node")
			parkingPod := manifest.NewDefaultPodBuilder().
				Container(manifest.NewBusyBoxContainerBuilder(f.Options.TestImageRegistry).Build()).
				Name("parking-pod").
				NodeName(primaryNode.Name).
				Build()

			parkingPod, err = f.K8sResourceManagers.PodManager().CreateAndWaitTillRunning(parkingPod)
			Expect(err).ToNot(HaveOccurred())

			validInput, err := GetIPv6PodNetworkingValidationInput(v1.PodList{
				Items: []v1.Pod{*parkingPod},
			}).Serialize()
			Expect(err).NotTo(HaveOccurred())

			By("first validating the tester work on valid input")
			ValidateHostNetworking(NetworkingSetupSucceeds, validInput)

			By("validating tester fails when invalid IP is passed")
			invalidPod := parkingPod.DeepCopy()
			invalidPod.Status.PodIP = "1.1.1.1"

			invalidInput, err := GetIPv6PodNetworkingValidationInput(v1.PodList{
				Items: []v1.Pod{*invalidPod},
			}).Serialize()
			Expect(err).NotTo(HaveOccurred())

			ValidateHostNetworking(NetworkingSetupFails, invalidInput)

			By("validating the tester fails when invalid namespace is passed")
			invalidPod = parkingPod.DeepCopy()
			// veth pair name is generated using namespace+name so the test should fail
			invalidPod.Namespace = "different"

			invalidInput, err = GetIPv6PodNetworkingValidationInput(v1.PodList{
				Items: []v1.Pod{*invalidPod},
			}).Serialize()
			Expect(err).NotTo(HaveOccurred())

			ValidateHostNetworking(NetworkingSetupFails, invalidInput)

			By("validating the tester fails when tear down check is run on running pod")
			ValidateHostNetworking(NetworkingTearDownFails, validInput)

			By("deleting the parking pod")
			err = f.K8sResourceManagers.PodManager().
				DeleteAndWaitTillPodDeleted(parkingPod)
			Expect(err).ToNot(HaveOccurred())
		})
	})
})

// Validate host networking for the list of pods supplied
func ValidateHostNetworking(testType TestType, podValidationInputString string) {
	testerArgs := []string{fmt.Sprintf("-pod-networking-validation-input=%s",
		podValidationInputString)}

	var shouldTestPodError bool
	if NetworkingSetupSucceeds == testType {
		testerArgs = append(testerArgs, "-test-setup=true")
	} else if NetworkingSetupFails == testType {
		testerArgs = append(testerArgs, "-test-setup=true")
		shouldTestPodError = true
	} else if NetworkingTearDownSucceeds == testType {
		testerArgs = append(testerArgs, "-test-cleanup=true")
	} else if NetworkingTearDownFails == testType {
		testerArgs = append(testerArgs, "-test-cleanup=true")
		shouldTestPodError = true
	}

	testContainer := manifest.NewTestHelperContainer(f.Options.TestImageRegistry).
		Command([]string{"./networking"}).
		Args(testerArgs).
		Build()

	testPod := manifest.NewDefaultPodBuilder().
		Container(testContainer).
		NodeName(primaryNode.Name).
		HostNetwork(true).
		Build()

	By("creating pod to test host networking setup")
	testPod, err := f.K8sResourceManagers.PodManager().CreateAndWaitTillPodCompleted(testPod)
	if shouldTestPodError {
		Expect(err).To(HaveOccurred())
	} else {
		Expect(err).ToNot(HaveOccurred())
	}
	logs, errLogs := f.K8sResourceManagers.PodManager().
		PodLogs(testPod.Namespace, testPod.Name)
	Expect(errLogs).ToNot(HaveOccurred())
	fmt.Fprintln(GinkgoWriter, logs)

	By("deleting the host networking setup pod")
	err = f.K8sResourceManagers.PodManager().
		DeleteAndWaitTillPodDeleted(testPod)
	Expect(err).ToNot(HaveOccurred())
}

// GetIPv6Pods returns list of IPv6 Pods
func GetIPv6Pods(podLabelKey string, podLabelVal string) (podList v1.PodList) {
	podList, err := f.K8sResourceManagers.
		PodManager().
		GetPodsWithLabelSelector(podLabelKey, podLabelVal)
	Expect(err).ToNot(HaveOccurred())

	return podList
}

// GetIPv6PodNetworkingValidationInput returns input string containing the list of pods for which
// the host networking has to be tested
func GetIPv6PodNetworkingValidationInput(podList v1.PodList) input.PodNetworkingValidationInput {
	ip := input.PodNetworkingValidationInput{
		VethPrefix:  "eni",
		PodList:     []input.Pod{},
		ValidateMTU: false,
		IPFamily:    "IPv6",
	}

	for _, pod := range podList.Items {
		ip.PodList = append(ip.PodList, input.Pod{
			PodName:              pod.Name,
			PodNamespace:         pod.Namespace,
			PodIPv6Address:       pod.Status.PodIP,
			IsIPFromSecondaryENI: false,
		})
	}
	return ip
}

func mtuValidationTest(usePodMTU bool, mtuVal int) {
	deployment := manifest.NewBusyBoxDeploymentBuilder(f.Options.TestImageRegistry).
		Replicas(2).
		PodLabel(podLabelKey, podLabelVal).
		NodeName(primaryNode.Name).
		Build()

	if usePodMTU {
		By("Configuring Veth Prefix and Pod MTU value on aws-node daemonset")
		k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace, utils.AwsNodeName, map[string]string{
			AWS_VPC_ENI_MTU:            strconv.Itoa(NEW_MTU_VAL),
			POD_MTU:                    strconv.Itoa(NEW_POD_MTU),
			AWS_VPC_K8S_CNI_VETHPREFIX: NEW_VETH_PREFIX,
		})
	} else {
		By("Configuring Veth Prefix and ENI MTU value on aws-node daemonset")
		k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace, utils.AwsNodeName, map[string]string{
			AWS_VPC_ENI_MTU:            strconv.Itoa(NEW_MTU_VAL),
			AWS_VPC_K8S_CNI_VETHPREFIX: NEW_VETH_PREFIX,
		})
	}
	// After updating daemonset pod, we must wait until conflist is updated so that container-runtime calls CNI ADD with the new VETH prefix and MTU.
	time.Sleep(utils.PollIntervalMedium)

	By("creating a deployment to launch pods")
	deployment, err = f.K8sResourceManagers.DeploymentManager().
		CreateAndWaitTillDeploymentIsReady(deployment, utils.DefaultDeploymentReadyTimeout)
	Expect(err).ToNot(HaveOccurred())

	By("getting the list of pods using IP from primary and secondary ENI")
	interfaceTypeToPodList :=
		GetIPv6Pods(podLabelKey, podLabelVal)

	By("generating the pod networking validation input to be passed to tester")
	podNetworkingValidationInput := GetIPv6PodNetworkingValidationInput(interfaceTypeToPodList)
	podNetworkingValidationInput.VethPrefix = NEW_VETH_PREFIX
	podNetworkingValidationInput.ValidateMTU = true
	podNetworkingValidationInput.MTU = mtuVal
	input, err := podNetworkingValidationInput.Serialize()
	Expect(err).NotTo(HaveOccurred())

	By("validating host networking setup is setup correctly with MTU check as well")
	ValidateHostNetworking(NetworkingSetupSucceeds, input)

	By("deleting the deployment to test teardown")
	err = f.K8sResourceManagers.DeploymentManager().
		DeleteAndWaitTillDeploymentIsDeleted(deployment)
	Expect(err).ToNot(HaveOccurred())

	By("waiting to allow CNI to tear down networking for terminated pods")
	time.Sleep(time.Second * 60)

	By("validating host networking is teared down correctly")
	ValidateHostNetworking(NetworkingTearDownSucceeds, input)
}
