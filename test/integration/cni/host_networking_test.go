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
	"strconv"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/integration/common"
	v1 "k8s.io/api/core/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// TODO: Instead of passing the list of pods to the test helper, have the test helper get the pod on node
const (
	NEW_MTU_VAL     = 1300
	NEW_POD_MTU     = 1280
	NEW_VETH_PREFIX = "veth"
	podLabelKey     = "app"
	podLabelVal     = "host-networking-test"
)

var err error

var _ = Describe("test host networking", func() {

	// For host networking tests, increase WARM_IP_TARGET to prevent long IPAMD warmup.
	BeforeEach(func() {
		k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace, utils.AwsNodeName, map[string]string{
			"WARM_IP_TARGET": strconv.Itoa(maxIPPerInterface - 1),
		})
	})
	AfterEach(func() {
		k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace, utils.AwsNodeName, map[string]string{
			"WARM_IP_TARGET": DEFAULT_WARM_IP_TARGET,
		})
	})

	Context("when pods using IP from primary and secondary ENI are created", func() {
		AfterEach(func() {
			k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace, utils.AwsNodeName, map[string]string{
				"AWS_VPC_ENI_MTU":            DEFAULT_MTU_VAL,
				"AWS_VPC_K8S_CNI_VETHPREFIX": DEFAULT_VETH_PREFIX,
			})
			k8sUtils.RemoveVarFromDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName,
				utils.AwsNodeNamespace, utils.AwsNodeName, map[string]struct{}{
					"POD_MTU": {},
				})
			// After updating daemonset pod, we must wait until conflist is updated so that container-runtime calls CNI ADD with the latest VETH prefix and MTU.
			// Otherwise, the stale value can cause failures in future test cases.
			time.Sleep(utils.PollIntervalMedium)
		})
		It("should have correct host networking setup when running and cleaned up once terminated", func() {
			// Launch enough pods so some pods end up using primary ENI IP and some using secondary
			// ENI IP
			deployment := manifest.NewBusyBoxDeploymentBuilder(f.Options.TestImageRegistry).
				Replicas(maxIPPerInterface*2).
				PodLabel(podLabelKey, podLabelVal).
				NodeName(primaryNode.Name).
				Build()

			By("creating a deployment to launch pod using primary and secondary ENI IP")
			deployment, err = f.K8sResourceManagers.DeploymentManager().
				CreateAndWaitTillDeploymentIsReady(deployment, utils.DefaultDeploymentReadyTimeout)
			Expect(err).ToNot(HaveOccurred())

			By("getting the list of pods using IP from primary and secondary ENI")
			interfaceTypeToPodList := common.GetPodsOnPrimaryAndSecondaryInterface(primaryNode, podLabelKey, podLabelVal, f)

			// Primary ENI and Secondary ENI IPs are handled differently when setting up
			// the host networking rule hence this check
			Expect(len(interfaceTypeToPodList.PodsOnSecondaryENI)).
				Should(BeNumerically(">", 0))
			Expect(len(interfaceTypeToPodList.PodsOnPrimaryENI)).
				Should(BeNumerically(">", 0))

			By("generating the pod networking validation input to be passed to tester")
			input, err := common.GetPodNetworkingValidationInput(interfaceTypeToPodList, vpcCIDRs).Serialize()
			Expect(err).NotTo(HaveOccurred())

			By("validating host networking setup is setup correctly")
			common.ValidateHostNetworking(common.NetworkingSetupSucceeds, input, primaryNode.Name, f)

			By("deleting the deployment to test teardown")
			err = f.K8sResourceManagers.DeploymentManager().
				DeleteAndWaitTillDeploymentIsDeleted(deployment)
			Expect(err).ToNot(HaveOccurred())

			By("waiting to allow CNI to tear down networking for terminated pods")
			time.Sleep(time.Second * 60)

			By("validating host networking is teared down correctly")
			common.ValidateHostNetworking(common.NetworkingTearDownSucceeds, input, primaryNode.Name, f)
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

	Context("when host networking is tested on invalid input", func() {
		It("tester pod should error out", func() {
			By("creating a single pod on the test node")
			parkingPod := manifest.NewDefaultPodBuilder().
				Container(manifest.NewBusyBoxContainerBuilder(f.Options.TestImageRegistry).Build()).
				Name("parking-pod").
				NodeName(primaryNode.Name).
				Build()

			parkingPod, err = f.K8sResourceManagers.PodManager().CreateAndWaitTillRunning(parkingPod)
			Expect(err).ToNot(HaveOccurred())

			validInput, err := common.GetPodNetworkingValidationInput(common.InterfaceTypeToPodList{
				PodsOnPrimaryENI: []v1.Pod{*parkingPod},
			}, vpcCIDRs).Serialize()
			Expect(err).NotTo(HaveOccurred())

			By("first validating the tester work on valid input")
			common.ValidateHostNetworking(common.NetworkingSetupSucceeds, validInput, primaryNode.Name, f)

			By("validating tester fails when invalid IP is passed")
			invalidPod := parkingPod.DeepCopy()
			invalidPod.Status.PodIP = "1.1.1.1"

			invalidInput, err := common.GetPodNetworkingValidationInput(common.InterfaceTypeToPodList{
				PodsOnPrimaryENI: []v1.Pod{*invalidPod},
			}, vpcCIDRs).Serialize()
			Expect(err).NotTo(HaveOccurred())

			common.ValidateHostNetworking(common.NetworkingSetupFails, invalidInput, primaryNode.Name, f)

			By("validating the tester fails when invalid namespace is passed")
			invalidPod = parkingPod.DeepCopy()
			// veth pair name is generated using namespace+name so the test should fail
			invalidPod.Namespace = "different"

			invalidInput, err = common.GetPodNetworkingValidationInput(common.InterfaceTypeToPodList{
				PodsOnPrimaryENI: []v1.Pod{*invalidPod},
			}, vpcCIDRs).Serialize()
			Expect(err).NotTo(HaveOccurred())

			common.ValidateHostNetworking(common.NetworkingSetupFails, invalidInput, primaryNode.Name, f)

			By("validating the tester fails when tear down check is run on running pod")
			common.ValidateHostNetworking(common.NetworkingTearDownFails, validInput, primaryNode.Name, f)

			By("deleting the parking pod")
			err = f.K8sResourceManagers.PodManager().
				DeleteAndWaitTillPodDeleted(parkingPod)
			Expect(err).ToNot(HaveOccurred())
		})
	})
})

func mtuValidationTest(usePodMTU bool, mtuVal int) {
	deployment := manifest.NewBusyBoxDeploymentBuilder(f.Options.TestImageRegistry).
		Replicas(maxIPPerInterface*2).
		PodLabel(podLabelKey, podLabelVal).
		NodeName(primaryNode.Name).
		Build()

	if usePodMTU {
		By("Configuring Veth Prefix and Pod MTU value on aws-node daemonset")
		k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace, utils.AwsNodeName, map[string]string{
			"AWS_VPC_ENI_MTU":            strconv.Itoa(NEW_MTU_VAL),
			"POD_MTU":                    strconv.Itoa(NEW_POD_MTU),
			"AWS_VPC_K8S_CNI_VETHPREFIX": NEW_VETH_PREFIX,
		})
	} else {
		By("Configuring Veth Prefix and ENI MTU value on aws-node daemonset")
		k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace, utils.AwsNodeName, map[string]string{
			"AWS_VPC_ENI_MTU":            strconv.Itoa(NEW_MTU_VAL),
			"AWS_VPC_K8S_CNI_VETHPREFIX": NEW_VETH_PREFIX,
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
		common.GetPodsOnPrimaryAndSecondaryInterface(primaryNode, podLabelKey, podLabelVal, f)

	By("generating the pod networking validation input to be passed to tester")
	podNetworkingValidationInput := common.GetPodNetworkingValidationInput(interfaceTypeToPodList, vpcCIDRs)
	podNetworkingValidationInput.VethPrefix = NEW_VETH_PREFIX
	podNetworkingValidationInput.ValidateMTU = true
	podNetworkingValidationInput.MTU = mtuVal
	input, err := podNetworkingValidationInput.Serialize()
	Expect(err).NotTo(HaveOccurred())

	By("validating host networking setup is setup correctly with MTU check as well")
	common.ValidateHostNetworking(common.NetworkingSetupSucceeds, input, primaryNode.Name, f)

	By("deleting the deployment to test teardown")
	err = f.K8sResourceManagers.DeploymentManager().
		DeleteAndWaitTillDeploymentIsDeleted(deployment)
	Expect(err).ToNot(HaveOccurred())

	By("waiting to allow CNI to tear down networking for terminated pods")
	time.Sleep(time.Second * 60)

	By("validating host networking is teared down correctly")
	common.ValidateHostNetworking(common.NetworkingTearDownSucceeds, input, primaryNode.Name, f)
}
