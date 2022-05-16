package addon_tests

import (
	"fmt"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/aws/services"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/integration/common"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/apps/v1"
)

var (
	err         error
	podLabelKey = "app"
	podLabelVal = "host-networking-test"
	deployment  *v1.Deployment
)

const (
	// Using v1.9.x as the default since v1.7.5 has following issue:https://github.com/aws/amazon-vpc-cni-k8s/pull/1341
	DEFAULT_VERSION = "v1.9.1-eksbuild.1"
)

var _ = Describe("cni addon upgrade/downgrade test", func() {
	Context("test host networking", func() {
		It("create test deployment with initial addon and verify deletion with target addon", func() {
			targetAddonVersion := f.Options.TargetAddon
			initialAddonVersion := f.Options.InitialAddon
			if len(f.Options.TargetAddon) == 0 {
				targetAddonVersion = latestAddonVersion
			}
			By(fmt.Sprintf("using target addon version as %s", targetAddonVersion))

			if len(f.Options.InitialAddon) == 0 {
				initialAddonVersion = DEFAULT_VERSION
			}
			By(fmt.Sprintf("using initial addon version as %s", initialAddonVersion))

			ApplyAddOn(initialAddonVersion)

			interfaceTypeToPodList := getTestPodList()
			By("generating the pod networking validation input to be passed to tester")
			input, err := common.GetPodNetworkingValidationInput(interfaceTypeToPodList, vpcCIDRs).Serialize()
			Expect(err).NotTo(HaveOccurred())

			By("validating host networking is setup correctly with initial addon")
			common.ValidateHostNetworking(common.NetworkingSetupSucceeds, input, primaryNode.Name, f)

			By("upgrading to target addon version")
			ApplyAddOn(targetAddonVersion)

			deleteTestDeployment()

			By("validating host networking is teared down correctly with target addon")
			common.ValidateHostNetworking(common.NetworkingTearDownSucceeds, input, primaryNode.Name, f)
		})

		It("create test deployment with target addon and verify deletion with initial addon", func() {
			targetAddonVersion := f.Options.TargetAddon
			initialAddonVersion := f.Options.InitialAddon
			if len(f.Options.TargetAddon) == 0 {
				targetAddonVersion = latestAddonVersion
			}
			By(fmt.Sprintf("using target addon version as %s", targetAddonVersion))

			if len(f.Options.InitialAddon) == 0 {
				initialAddonVersion = DEFAULT_VERSION
			}
			By(fmt.Sprintf("using initial addon version as %s", initialAddonVersion))

			ApplyAddOn(targetAddonVersion)

			interfaceTypeToPodList := getTestPodList()
			By("generating the pod networking validation input to be passed to tester")
			input, err := common.GetPodNetworkingValidationInput(interfaceTypeToPodList, vpcCIDRs).Serialize()
			Expect(err).NotTo(HaveOccurred())

			By("validating host networking is setup correctly with target addon")
			common.ValidateHostNetworking(common.NetworkingSetupSucceeds, input, primaryNode.Name, f)

			By("downgrading to initial addon version")
			ApplyAddOn(initialAddonVersion)

			deleteTestDeployment()

			By("validating host networking is teared down correctly with initial addon")
			common.ValidateHostNetworking(common.NetworkingTearDownSucceeds, input, primaryNode.Name, f)
		})
	})
})

func deleteTestDeployment() {
	By("deleting the deployment to test teardown")
	err = f.K8sResourceManagers.DeploymentManager().
		DeleteAndWaitTillDeploymentIsDeleted(deployment)
	Expect(err).ToNot(HaveOccurred())

	By("waiting to allow CNI to tear down networking for terminated pods")
	time.Sleep(time.Second * 60)
}

func getTestPodList() common.InterfaceTypeToPodList {
	deployment = manifest.NewBusyBoxDeploymentBuilder().
		Replicas(maxIPPerInterface*2).
		PodLabel(podLabelKey, podLabelVal).
		NodeName(primaryNode.Name).
		Build()

	By("creating a deployment to launch pod using primary and secondary ENI IP")
	_, err := f.K8sResourceManagers.DeploymentManager().
		CreateAndWaitTillDeploymentIsReady(deployment, utils.DefaultDeploymentReadyTimeout)
	Expect(err).ToNot(HaveOccurred())

	By("getting the list of pods using IP from primary and secondary ENI")
	interfaceTypeToPodList :=
		common.GetPodsOnPrimaryAndSecondaryInterface(primaryNode, podLabelKey, podLabelVal, f)

	return interfaceTypeToPodList
}

func ApplyAddOn(version string) {
	By("delete existing cni addon if any")
	_, err := f.CloudServices.EKS().DescribeAddon(&services.AddonInput{
		AddonName:   "vpc-cni",
		ClusterName: f.Options.ClusterName,
	})
	if err == nil {
		_, err := f.CloudServices.EKS().DeleteAddon(&services.AddonInput{
			AddonName:   "vpc-cni",
			ClusterName: f.Options.ClusterName,
		})
		Expect(err).NotTo(HaveOccurred())

		By("wait till the old addon is deleted")
		err = k8sUtils.WaitTillAddonIsDeleted(f.CloudServices.EKS(), "vpc-cni", f.Options.ClusterName)
		Expect(err).To(HaveOccurred())
	}
	By(fmt.Sprintf("install the new addon with version: %s", version))
	_, err = f.CloudServices.EKS().CreateAddon(&services.AddonInput{
		AddonName:    "vpc-cni",
		ClusterName:  f.Options.ClusterName,
		AddonVersion: version,
	})
	Expect(err).NotTo(HaveOccurred())

	By("wait till the addon is active")
	err = k8sUtils.WaitTillAddonIsActive(f.CloudServices.EKS(), "vpc-cni", f.Options.ClusterName)
	Expect(err).NotTo(HaveOccurred())

	By("Check if aws-node pods are Running")
	podList, err := f.K8sResourceManagers.PodManager().GetPodsWithLabelSelector("k8s-app", "aws-node")
	Expect(err).NotTo(HaveOccurred())

	for _, pod := range podList.Items {
		for _, status := range pod.Status.ContainerStatuses {
			Expect(status.State.Running).ToNot(BeNil())
		}
	}
}
