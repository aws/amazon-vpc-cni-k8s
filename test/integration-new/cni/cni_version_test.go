package cni

import (
	"fmt"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/aws/services"
	"github.com/pkg/errors"
	"time"

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

	Context("when testing host networking on initial version", func() {
		HostNetworkingTest()
		PodTrafficTest()
		ServiceConnectivityTest()
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

	Context("when testing host networking on final version", func() {
		HostNetworkingTest()
		PodTrafficTest()
		ServiceConnectivityTest()
	})

})

func ApplyAddOn(versionName string) {

	By("getting the current addon")
	describeAddonOutput, err = f.CloudServices.EKS().DescribeAddon("vpc-cni", f.Options.ClusterName)
	if err == nil {
		fmt.Printf("By applying addon %s\n", versionName)
		By("checking if the current addon is same as addon to be applied")
		if *describeAddonOutput.Addon.AddonVersion != versionName {

			By("deleting the current vpc cni addon ")
			_, err = f.CloudServices.EKS().DeleteAddon("vpc-cni", f.Options.ClusterName)
			Expect(err).ToNot(HaveOccurred())

			_, err = f.CloudServices.EKS().DescribeAddon("vpc-cni", f.Options.ClusterName)
			for err == nil {
				time.Sleep(5 * time.Second)
				_, err = f.CloudServices.EKS().DescribeAddon("vpc-cni", f.Options.ClusterName)

			}

			By("apply addon version")
			_, err = f.CloudServices.EKS().CreateAddon(services.CreateAddOnParams{AddonName: "vpc-cni", ClusterName: f.Options.ClusterName, AddonVersion: versionName})
			Expect(err).ToNot(HaveOccurred())

		}
	} else {
		fmt.Printf("By applying addon %s\n", versionName)
		By("apply addon version")
		_, err = f.CloudServices.EKS().CreateAddon(services.CreateAddOnParams{AddonName: "vpc-cni", ClusterName: f.Options.ClusterName, AddonVersion: versionName})
		Expect(err).ToNot(HaveOccurred())
	}

	var status = ""

	By("waiting for  addon to be ACTIVE")
	for status != "ACTIVE" {
		describeAddonOutput, err = f.CloudServices.EKS().DescribeAddon("vpc-cni", f.Options.ClusterName)
		Expect(err).ToNot(HaveOccurred())
		status = *describeAddonOutput.Addon.Status
		time.Sleep(5 * time.Second)
	}

	//Set the WARM_ENI_TARGET to 0 to prevent all pods being scheduled on secondary ENI
	k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, "aws-node", "kube-system",
		"aws-node", map[string]string{"WARM_IP_TARGET": "3", "WARM_ENI_TARGET": "0"})
}
