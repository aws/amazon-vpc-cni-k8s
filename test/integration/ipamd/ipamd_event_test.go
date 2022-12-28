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
	"io/ioutil"
	"net/url"
	"strings"
	"time"

	k8sUtil "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const EKSCNIPolicyARN = "arn:aws:iam::aws:policy/AmazonEKS_CNI_Policy"
const AwsNodeLabelKey = "k8s-app"
const DummyPolicyDocument = "/testdata/dummy-role-policy.json"

var _ = Describe("test aws-node pod event", func() {

	// Verifies aws-node pod events works as expected
	Context("when iam role is missing VPC_CNI policy", func() {
		var role string
		var rolePolicyDocument string
		var policyName string

		BeforeEach(func() {
			// To get the role assumed by CNI, first check the ENV "AWS_ROLE_ARN" on aws-node to get the service account role
			// If not found, get the node instance role
			By("getting the iam role")
			podList, err := f.K8sResourceManagers.PodManager().GetPodsWithLabelSelector(AwsNodeLabelKey, utils.AwsNodeName)
			Expect(err).ToNot(HaveOccurred())
			for _, env := range podList.Items[0].Spec.Containers[0].Env {
				if env.Name == "AWS_ROLE_ARN" {
					role = strings.Split(env.Value, "/")[1]
				}
			}

			if role == "" { // get the node instance role
				nodeList, err := f.K8sResourceManagers.NodeManager().
					GetNodes(f.Options.NgNameLabelKey, f.Options.NgNameLabelVal)
				Expect(err).ToNot(HaveOccurred())

				instanceID := k8sUtil.GetInstanceIDFromNode(nodeList.Items[0])
				instance, err := f.CloudServices.EC2().DescribeInstance(instanceID)
				Expect(err).ToNot(HaveOccurred())

				By("getting the node instance role")
				instanceProfileRoleName := strings.Split(*instance.IamInstanceProfile.Arn, "instance-profile/")[1]
				instanceProfileOutput, err := f.CloudServices.IAM().GetInstanceProfile(instanceProfileRoleName)
				Expect(err).ToNot(HaveOccurred())
				role = *instanceProfileOutput.InstanceProfile.Roles[0].RoleName
			}

			By("Detaching VPC_CNI policy")
			err = f.CloudServices.IAM().DetachRolePolicy(EKSCNIPolicyARN, role)
			Expect(err).ToNot(HaveOccurred())

			dummyPolicyDocumentPath := utils.GetProjectRoot() + DummyPolicyDocument
			dummyRolePolicyBytes, err := ioutil.ReadFile(dummyPolicyDocumentPath)
			Expect(err).ToNot(HaveOccurred())

			dummyRolePolicyData := string(dummyRolePolicyBytes)

			// For Kops - clusters have an inline role policy defined which has the same name as role name
			policyName = role
			rolePolicy, err := f.CloudServices.IAM().GetRolePolicy(policyName, role)
			if err == nil {
				By("Detaching the inline role policy")
				rolePolicyDocument, err = url.QueryUnescape(*rolePolicy.PolicyDocument)
				err = f.CloudServices.IAM().PutRolePolicy(dummyRolePolicyData, policyName, role)
				Expect(err).ToNot(HaveOccurred())
			}

			RestartAwsNodePods()

			By("checking aws-node pods not running")
			time.Sleep(utils.PollIntervalMedium) // allow time for aws-node to restart
			podList, err = f.K8sResourceManagers.PodManager().GetPodsWithLabelSelector(AwsNodeLabelKey, utils.AwsNodeName)
			Expect(err).ToNot(HaveOccurred())

			for _, cond := range podList.Items[0].Status.Conditions {
				if cond.Type == v1.PodReady {
					Expect(cond.Status).To(BeEquivalentTo(v1.ConditionFalse))
					break
				}
			}

		})

		AfterEach(func() {
			By("attaching VPC_CNI policy")
			err = f.CloudServices.IAM().AttachRolePolicy(EKSCNIPolicyARN, role)
			Expect(err).ToNot(HaveOccurred())

			if rolePolicyDocument != "" {
				By("Attaching the inline role policy")
				err = f.CloudServices.IAM().PutRolePolicy(rolePolicyDocument, policyName, role)
				Expect(err).ToNot(HaveOccurred())
			}

			RestartAwsNodePods()

			By("checking aws-node pods are running")
			time.Sleep(utils.PollIntervalMedium * 3) // sleep to allow aws-node to restart
			podList, err := f.K8sResourceManagers.PodManager().GetPodsWithLabelSelector(AwsNodeLabelKey, utils.AwsNodeName)
			Expect(err).ToNot(HaveOccurred())
			for _, cond := range podList.Items[0].Status.Conditions {
				if cond.Type != v1.PodReady {
					continue
				}
				Expect(cond.Status).To(BeEquivalentTo(v1.ConditionTrue))
				break
			}
		})

		It("unauthorized event must be raised on aws-node pod", func() {
			listOpts := client.ListOptions{
				FieldSelector: fields.SelectorFromSet(fields.Set{"reason": "MissingIAMPermissions"}),
				Namespace:     utils.AwsNodeNamespace,
			}
			eventList, err := f.K8sResourceManagers.EventManager().GetEventsWithOptions(&listOpts)
			Expect(err).ToNot(HaveOccurred())
			Expect(eventList.Items).NotTo(BeEmpty())

		})
	})
})

func RestartAwsNodePods() {
	By("Restart aws-node pods")
	podList, err := f.K8sResourceManagers.PodManager().GetPodsWithLabelSelector("k8s-app", utils.AwsNodeName)
	Expect(err).ToNot(HaveOccurred())
	for _, pod := range podList.Items {
		f.K8sResourceManagers.PodManager().DeleteAndWaitTillPodDeleted(&pod)
	}
}
