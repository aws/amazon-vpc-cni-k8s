package cni

import (
	"fmt"
	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	"github.com/aws/aws-sdk-go/service/eks"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"time"
)

var (
	describeAddonOutput *eks.DescribeAddonOutput
	err                 error
	initialCNIVersion   string
	finalCNIVersion     string
)

var _ = Describe("test cluster upgrade/downgrade", func() {

	initialCNIVersion = f.Options.InitialCNIVersion
	finalCNIVersion = f.Options.FinalCNIVersion

	It("should apply initial addon version successfully", func() {
		ApplyAddOn(initialCNIVersion)
		//Set the WARM_ENI_TARGET to 0 to prevent all pods being scheduled on secondary ENI
		k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, "aws-node", "kube-system",
			"aws-node", map[string]string{"WARM_IP_TARGET": "3", "WARM_ENI_TARGET": "0"})

	})

	Context("when testing host networking on initial version", func() {
		HostNetworkingTest()
		PodTrafficTest()
		ServiceConnectivityTest()
	})

	It("should apply final addon version successfully", func() {
		ApplyAddOn(finalCNIVersion)
		//Set the WARM_ENI_TARGET to 0 to prevent all pods being scheduled on secondary ENI
		k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, "aws-node", "kube-system",
			"aws-node", map[string]string{"WARM_IP_TARGET": "3", "WARM_ENI_TARGET": "0"})

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

			By("apply initial addon version")
			_, err = f.CloudServices.EKS().CreateAddonWithVersion("vpc-cni", f.Options.ClusterName, versionName)
			Expect(err).ToNot(HaveOccurred())

		}
	} else {
		By("apply addon version")
		_, err = f.CloudServices.EKS().CreateAddonWithVersion("vpc-cni", f.Options.ClusterName, versionName)
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
}
