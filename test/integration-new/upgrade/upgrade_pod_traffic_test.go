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

package upgrade

import (
	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	"github.com/aws/aws-sdk-go/service/eks"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("test applying  older version", func() {

	var (
		describeAddonOutput *eks.DescribeAddonOutput
		err                 error
	)

	BeforeEach(func() {
		By("getting the current addon")
		describeAddonOutput, err = f.CloudServices.EKS().DescribeAddon("vpc-cni", f.Options.ClusterName)
		if err == nil {

			By("deleting the current vpc cni addon ")
			_, err = f.CloudServices.EKS().DeleteAddon("vpc-cni", f.Options.ClusterName)
			Expect(err).ToNot(HaveOccurred())

		}
	})

	It("should successfully run on initial addon version", func() {
		By("apply initial addon version")
		_, err = f.CloudServices.EKS().CreateAddonWithVersion("vpc-cni", f.Options.ClusterName, initialCNIVersion)
		Expect(err).ToNot(HaveOccurred())

		var status string = ""

		By("getting the initial addon...")
		for status != "ACTIVE" {
			describeAddonOutput, err = f.CloudServices.EKS().DescribeAddon("vpc-cni", f.Options.ClusterName)
			Expect(err).ToNot(HaveOccurred())
			status = *describeAddonOutput.Addon.Status
		}
		//Set the WARM_ENI_TARGET to 0 to prevent all pods being scheduled on secondary ENI
		k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, "aws-node", "kube-system",
			"aws-node", map[string]string{"WARM_IP_TARGET": "3", "WARM_ENI_TARGET": "0"})

		testPodNetworking()

	})

	It("should successfully run on final addon version", func() {
		By("apply final addon version")
		_, err = f.CloudServices.EKS().CreateAddonWithVersion("vpc-cni", f.Options.ClusterName, finalCNIVersion)
		Expect(err).ToNot(HaveOccurred())

		var status string = ""

		By("getting the final addon...")
		for status != "ACTIVE" {
			describeAddonOutput, err = f.CloudServices.EKS().DescribeAddon("vpc-cni", f.Options.ClusterName)
			Expect(err).ToNot(HaveOccurred())
			status = *describeAddonOutput.Addon.Status
		}
		//Set the WARM_ENI_TARGET to 0 to prevent all pods being scheduled on secondary ENI
		k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, "aws-node", "kube-system",
			"aws-node", map[string]string{"WARM_IP_TARGET": "3", "WARM_ENI_TARGET": "0"})

		testPodNetworking()

	})

})

func testPodNetworking() {

}
