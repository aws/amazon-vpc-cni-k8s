package cni

import (
	"context"
	"fmt"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/aws/services"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/util/wait"

	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	"github.com/aws/aws-sdk-go/service/eks"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var (
	describeAddonOutput *eks.DescribeAddonOutput
	err                 error
	initialCNIVersion   string
	finalCNIVersion     string
)

var _ = Describe("test cluster upgrade/downgrade", func() {

	It("should apply initial addon version successfully", func() {
		By("getting initial cni version")
		if len(f.Options.InitialCNIVersion) == 0 {
			err = errors.Errorf("%s must be set!", "initial-version")
		}
		Expect(err).ToNot(HaveOccurred())
		initialCNIVersion = f.Options.InitialCNIVersion
		ApplyAddOn(initialCNIVersion)

	})

	Context("when testing pod networking on initial version", func() {
		HostNetworkingTest()
		//PodTrafficTest()
		//ServiceConnectivityTest()
	})

	It("should apply final addon version successfully", func() {
		By("getting final cni version")
		if len(f.Options.FinalCNIVersion) == 0 {
			err = errors.Errorf("%s must be set!", "final-version")
		}
		Expect(err).ToNot(HaveOccurred())
		finalCNIVersion = f.Options.FinalCNIVersion
		ApplyAddOn(finalCNIVersion)

	})

	Context("when testing pod networking on final version", func() {
		HostNetworkingTest()
		//PodTrafficTest()
		//ServiceConnectivityTest()
	})

})

func ApplyAddOn(versionName string) {

	By("getting the current addon")
	describeAddonOutput, err = f.CloudServices.EKS().DescribeAddon("vpc-cni", f.Options.ClusterName)
	if err == nil {
		By("checking if the current addon is same as addon to be applied")
		if *describeAddonOutput.Addon.AddonVersion != versionName {

			By("deleting the current vpc cni addon ")
			_, err = f.CloudServices.EKS().DeleteAddon("vpc-cni", f.Options.ClusterName)
			Expect(err).ToNot(HaveOccurred())

			By("waiting for addon to be deleted")
			err = DeleteAddOnAndWaitTillReady("vpc-cni", f.Options.ClusterName)
			Expect(err).ToNot(HaveOccurred())

			By(fmt.Sprintf("By applying addon %s\n", versionName))
			_, err = f.CloudServices.EKS().CreateAddon(services.AddOnInput{AddonName: "vpc-cni", ClusterName: f.Options.ClusterName, AddonVersion: versionName})
			Expect(err).ToNot(HaveOccurred())

		}
	} else {
		By(fmt.Sprintf("By applying addon %s\n", versionName))
		_, err = f.CloudServices.EKS().CreateAddon(services.AddOnInput{AddonName: "vpc-cni", ClusterName: f.Options.ClusterName, AddonVersion: versionName})
		Expect(err).ToNot(HaveOccurred())
	}

	By("waiting for addon to be ACTIVE")
	err = UpdateAddOnAndWaitTillReady("vpc-cni", f.Options.ClusterName)
	Expect(err).ToNot(HaveOccurred())

	//Set the WARM_ENI_TARGET to 0 to prevent all pods being scheduled on secondary ENI
	k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, "aws-node", "kube-system",
		"aws-node", map[string]string{"WARM_IP_TARGET": "3", "WARM_ENI_TARGET": "0"})
}

func DeleteAddOnAndWaitTillReady(addOnName string, clusterName string) error {

	ctx := context.Background()
	return wait.PollImmediateUntil(utils.PollIntervalShort, func() (bool, error) {
		_, err = f.CloudServices.EKS().DescribeAddon(addOnName, clusterName)
		if err != nil {
			return true, nil
		}
		return false, nil
	}, ctx.Done())

}

func UpdateAddOnAndWaitTillReady(addOnName string, clusterName string) error {

	ctx := context.Background()
	var status = ""
	return wait.PollImmediateUntil(utils.PollIntervalShort, func() (bool, error) {
		describeAddonOutput, err = f.CloudServices.EKS().DescribeAddon(addOnName, clusterName)
		if err != nil {
			return false, err
		}
		status = *describeAddonOutput.Addon.Status
		if status == "ACTIVE" {
			return true, nil
		}
		return false, nil
	}, ctx.Done())
}
