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

package ipamd

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Verifies that additional ENI tags are added on new Secondary Elastic Network Interface created
// by IPAMD
var _ = Describe("test tags are created on Secondary ENI", func() {
	// Subset of tags expected on new Secondary ENI
	var expectedTags map[string]string
	// List of ENIs created after updating the environment variables
	var newENIs []string
	// Environment variables to be updated for testing new tags on Secondary ENI
	var environmentVariables map[string]string

	// Sets the WARM_ENI_TARGET to 0 to allow for unused ENIs to be deleted by IPAMD and then
	// sets the desired environment variables and gets the list of new ENIs created after setting
	// the environment variables
	JustBeforeEach(func() {
		// To re-initialize for each test case
		newENIs = []string{}

		By("try detaching all ENIs by setting WARM_ENI_TARGET to 0")
		k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace,
			utils.AwsNodeName, map[string]string{"WARM_ENI_TARGET": "0"})

		By("sleeping to allow CNI Plugin delete unused ENIs")
		time.Sleep(time.Second * 90)

		By("getting the list of ENIs before setting ADDITIONAL_ENI_TAGS")
		instance, err := f.CloudServices.EC2().DescribeInstance(context.TODO(), *primaryInstance.InstanceId)
		Expect(err).ToNot(HaveOccurred())

		existingENIs := make(map[string]bool)
		for _, nwInterface := range instance.NetworkInterfaces {
			existingENIs[*nwInterface.NetworkInterfaceId] = true
		}

		By(fmt.Sprintf("adding environment variable: %+v", environmentVariables))
		k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace,
			utils.AwsNodeName, environmentVariables)

		By("sleeping to allow CNI Plugin to create new ENI with additional tags")
		time.Sleep(time.Second * 90)

		By("getting the list of current ENIs by describing the instance")
		instance, err = f.CloudServices.EC2().DescribeInstance(context.TODO(), *primaryInstance.InstanceId)
		Expect(err).ToNot(HaveOccurred())

		for _, nwInterface := range instance.NetworkInterfaces {
			if _, ok := existingENIs[*nwInterface.NetworkInterfaceId]; !ok {
				newENIs = append(newENIs, *nwInterface.NetworkInterfaceId)
			}
		}

		By("verifying at least one new Secondary ENI is created")
		Expect(len(newENIs)).Should(BeNumerically(">", 0))
	})

	JustAfterEach(func() {

		envVarToRemove := map[string]struct{}{}
		for key := range environmentVariables {
			envVarToRemove[key] = struct{}{}
		}

		By("removing environment variables set by the test")
		k8sUtils.RemoveVarFromDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace,
			utils.AwsNodeName, envVarToRemove)
	})

	Context("when additional ENI tags are added using ADDITIONAL_ENI_TAGS", func() {
		BeforeEach(func() {
			suppliedTags := map[string]string{
				"tag_owner":                   "cni_automation_test",
				"k8s.amazonaws.com/tag_owner": "cni_automation_test",
			}
			tagBytes, err := json.Marshal(suppliedTags)
			Expect(err).ToNot(HaveOccurred())

			environmentVariables = map[string]string{
				"ADDITIONAL_ENI_TAGS": string(tagBytes),
				"WARM_ENI_TARGET":     "2",
			}

			expectedTags = map[string]string{
				"tag_owner": "cni_automation_test",
			}
		})

		It("new secondary ENI should get new tags and block reserved tags", func() {
			VerifyTagIsPresentOnENIs(newENIs, expectedTags)
		})
	})

	Context("when additional secondary ENI are created after setting CLUSTER_NAME", func() {
		BeforeEach(func() {
			clusterName := "dummy_cluster_name"
			expectedTags = map[string]string{
				"cluster.k8s.amazonaws.com/name": clusterName,
			}

			environmentVariables = map[string]string{
				"CLUSTER_NAME":    clusterName,
				"WARM_ENI_TARGET": "2",
			}
		})

		It("new secondary ENI should have cluster name tags", func() {
			VerifyTagIsPresentOnENIs(newENIs, expectedTags)
		})
	})
})

// VerifyTagIsPresentOnENIs verifies that the list of ENIs have expected tag key-val pair
func VerifyTagIsPresentOnENIs(newENIIds []string, expectedTags map[string]string) {
	By(fmt.Sprintf("Describing the list of new ENI created after seeting env variable %v", newENIIds))
	describeNetworkInterfaceOutput, err := f.CloudServices.EC2().DescribeNetworkInterface(context.TODO(), newENIIds)
	Expect(err).ToNot(HaveOccurred())

	By("verifying the new tags are present on new ENIs")

	// Initially expected tag should not be 0
	Expect(len(expectedTags)).ShouldNot(Equal(0))

	// Each time there's a match found, remove the entry from expected tags
	for _, nwInterface := range describeNetworkInterfaceOutput.NetworkInterfaces {
		for _, tag := range nwInterface.TagSet {
			if val, ok := expectedTags[*tag.Key]; ok && *tag.Value == val {
				delete(expectedTags, *tag.Key)
			}
		}
	}

	// The expected tags map should be empty indicating that all matches were found
	Expect(len(expectedTags)).Should(Equal(0))
}
