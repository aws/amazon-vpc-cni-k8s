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
	"fmt"
	"strconv"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/test/agent/pkg/input"
	v1 "k8s.io/api/core/v1"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

// TODO: Instead of passing the list of pods to the test helper, have the test helper get the pod on node

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
	NEW_MTU_VAL                = 1300
	NEW_VETH_PREFIX            = "veth"
	DEFAULT_MTU_VAL            = "9001"
	DEFAULT_VETH_PREFIX        = "eni"
)

var _ = Describe("test host networking", func() {
	var err error
	var podLabelKey = "app"
	var podLabelVal = "host-networking-test"

	Context("when pods using IP from primary and secondary ENI are created", func() {
		AfterEach(func() {
			k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace, utils.AwsNodeName, map[string]string{
				AWS_VPC_ENI_MTU:            DEFAULT_MTU_VAL,
				AWS_VPC_K8S_CNI_VETHPREFIX: DEFAULT_VETH_PREFIX,
			})
		})
		It("should have correct host networking setup when running and cleaned up once terminated", func() {
			// Launch enough pods so some pods end up using primary ENI IP and some using secondary
			// ENI IP
			deployment := manifest.NewBusyBoxDeploymentBuilder().
				Replicas(maxIPPerInterface*2).
				PodLabel(podLabelKey, podLabelVal).
				NodeName(primaryNode.Name).
				Build()

			By("creating a deployment to launch pod using primary and secondary ENI IP")
			deployment, err = f.K8sResourceManagers.DeploymentManager().
				CreateAndWaitTillDeploymentIsReady(deployment, utils.DefaultDeploymentReadyTimeout)
			Expect(err).ToNot(HaveOccurred())

			By("getting the list of pods using IP from primary and secondary ENI")
			interfaceTypeToPodList :=
				GetPodsOnPrimaryAndSecondaryInterface(primaryNode, podLabelKey, podLabelVal)

			// Primary ENI and Secondary ENI IPs are handled differently when setting up
			// the host networking rule hence this check
			Expect(len(interfaceTypeToPodList.PodsOnSecondaryENI)).
				Should(BeNumerically(">", 0))
			Expect(len(interfaceTypeToPodList.PodsOnPrimaryENI)).
				Should(BeNumerically(">", 0))

			By("generating the pod networking validation input to be passed to tester")
			input, err := GetPodNetworkingValidationInput(interfaceTypeToPodList).Serialize()
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

		It("Validate Host Networking setup after changing MTU and Veth Prefix", func() {
			deployment := manifest.NewBusyBoxDeploymentBuilder().
				Replicas(6).
				PodLabel(podLabelKey, podLabelVal).
				NodeName(primaryNode.Name).
				Build()

			By("Configuring Veth Prefix and MTU value on aws-node daemonset")
			k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace, utils.AwsNodeName, map[string]string{
				AWS_VPC_ENI_MTU:            strconv.Itoa(NEW_MTU_VAL),
				AWS_VPC_K8S_CNI_VETHPREFIX: NEW_VETH_PREFIX,
			})

			By("creating a deployment to launch pods")
			deployment, err = f.K8sResourceManagers.DeploymentManager().
				CreateAndWaitTillDeploymentIsReady(deployment, utils.DefaultDeploymentReadyTimeout)
			Expect(err).ToNot(HaveOccurred())

			By("getting the list of pods using IP from primary and secondary ENI")
			interfaceTypeToPodList :=
				GetPodsOnPrimaryAndSecondaryInterface(primaryNode, podLabelKey, podLabelVal)

			By("generating the pod networking validation input to be passed to tester")
			podNetworkingValidationInput := GetPodNetworkingValidationInput(interfaceTypeToPodList)
			podNetworkingValidationInput.VethPrefix = NEW_VETH_PREFIX
			podNetworkingValidationInput.ValidateMTU = true
			podNetworkingValidationInput.MTU = NEW_MTU_VAL
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
		})
	})

	Context("when host networking is tested on invalid input", func() {
		It("tester pod should error out", func() {

			By("creating a single pod on the test node")
			parkingPod := manifest.NewDefaultPodBuilder().
				Container(manifest.NewBusyBoxContainerBuilder().Build()).
				Name("parking-pod").
				NodeName(primaryNode.Name).
				Build()

			parkingPod, err = f.K8sResourceManagers.PodManager().
				CreatAndWaitTillRunning(parkingPod)
			Expect(err).ToNot(HaveOccurred())

			validInput, err := GetPodNetworkingValidationInput(InterfaceTypeToPodList{
				PodsOnPrimaryENI: []v1.Pod{*parkingPod},
			}).Serialize()
			Expect(err).NotTo(HaveOccurred())

			By("first validating the tester work on valid input")
			ValidateHostNetworking(NetworkingSetupSucceeds, validInput)

			By("validating tester fails when invalid IP is passed")
			invalidPod := parkingPod.DeepCopy()
			invalidPod.Status.PodIP = "1.1.1.1"

			invalidInput, err := GetPodNetworkingValidationInput(InterfaceTypeToPodList{
				PodsOnPrimaryENI: []v1.Pod{*invalidPod},
			}).Serialize()
			Expect(err).NotTo(HaveOccurred())

			ValidateHostNetworking(NetworkingSetupFails, invalidInput)

			By("validating the tester fails when invalid namespace is passed")
			invalidPod = parkingPod.DeepCopy()
			// veth pair name is generated using namespace+name so the test should fail
			invalidPod.Namespace = "different"

			invalidInput, err = GetPodNetworkingValidationInput(InterfaceTypeToPodList{
				PodsOnPrimaryENI: []v1.Pod{*invalidPod},
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

	testContainer := manifest.NewTestHelperContainer().
		Command([]string{"./networking"}).
		Args(testerArgs).
		Build()

	testPod := manifest.NewDefaultPodBuilder().
		Container(testContainer).
		NodeName(primaryNode.Name).
		HostNetwork(true).
		Build()

	By("creating pod to test host networking setup")
	testPod, err := f.K8sResourceManagers.PodManager().
		CreateAndWaitTillPodCompleted(testPod)
	logs, errLogs := f.K8sResourceManagers.PodManager().
		PodLogs(testPod.Namespace, testPod.Name)
	Expect(errLogs).ToNot(HaveOccurred())

	fmt.Fprintln(GinkgoWriter, logs)

	if shouldTestPodError {
		Expect(err).To(HaveOccurred())
	} else {
		Expect(err).ToNot(HaveOccurred())
	}

	By("deleting the host networking setup pod")
	err = f.K8sResourceManagers.PodManager().
		DeleteAndWaitTillPodDeleted(testPod)
	Expect(err).ToNot(HaveOccurred())
}

// GetPodNetworkingValidationInput returns input string containing the list of pods for which
// the host networking has to be tested
func GetPodNetworkingValidationInput(interfaceTypeToPodList InterfaceTypeToPodList) input.PodNetworkingValidationInput {

	ip := input.PodNetworkingValidationInput{
		VPCCidrRange: vpcCIDRs,
		VethPrefix:   "eni",
		PodList:      []input.Pod{},
		ValidateMTU:  false,
	}

	for _, primaryENIPod := range interfaceTypeToPodList.PodsOnPrimaryENI {
		ip.PodList = append(ip.PodList, input.Pod{
			PodName:              primaryENIPod.Name,
			PodNamespace:         primaryENIPod.Namespace,
			PodIPv4Address:       primaryENIPod.Status.PodIP,
			IsIPFromSecondaryENI: false,
		})
	}

	for _, secondaryENIPod := range interfaceTypeToPodList.PodsOnSecondaryENI {
		ip.PodList = append(ip.PodList, input.Pod{
			PodName:              secondaryENIPod.Name,
			PodNamespace:         secondaryENIPod.Namespace,
			PodIPv4Address:       secondaryENIPod.Status.PodIP,
			IsIPFromSecondaryENI: true,
		})
	}

	return ip
}
