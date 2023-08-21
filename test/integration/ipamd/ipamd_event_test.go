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
	"fmt"
	"io/ioutil"
	"net/url"
	"strings"
	"time"

	k8sUtil "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	"github.com/aws/aws-sdk-go/aws"
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
		var rolePolicyDocumentNode string
		var rolePolicyDocumentMaster string
		var masterPolicyName string
		var nodePolicyName string

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

			masterPolicyName = "masters." + *aws.String(f.Options.ClusterName)
			nodePolicyName = "nodes." + *aws.String(f.Options.ClusterName)
			dummyPolicyDocumentPath := utils.GetProjectRoot() + DummyPolicyDocument
			dummyRolePolicyBytes, err := ioutil.ReadFile(dummyPolicyDocumentPath)
			Expect(err).ToNot(HaveOccurred())

			dummyRolePolicyData := string(dummyRolePolicyBytes)

			// For Kops - clusters have an inline role policy defined and has same role and policy name
			rolePolicy, err := f.CloudServices.IAM().GetRolePolicy(nodePolicyName, nodePolicyName)
			if err == nil {
				By("Detaching the inline role policy for worker instances")
				rolePolicyDocumentNode, err = url.QueryUnescape(*rolePolicy.PolicyDocument)
				err = f.CloudServices.IAM().PutRolePolicy(dummyRolePolicyData, nodePolicyName, nodePolicyName)
				Expect(err).ToNot(HaveOccurred())
			}

			rolePolicy, err = f.CloudServices.IAM().GetRolePolicy(masterPolicyName, masterPolicyName)
			if err == nil {
				By("Detaching the inline role policy for master instances")
				rolePolicyDocumentMaster, err = url.QueryUnescape(*rolePolicy.PolicyDocument)
				err = f.CloudServices.IAM().PutRolePolicy(dummyRolePolicyData, masterPolicyName, masterPolicyName)
				Expect(err).ToNot(HaveOccurred())
			}

			RestartAwsNodePods()

			By("checking aws-node pods not running")
			Eventually(func(g Gomega) {
				podList, err = f.K8sResourceManagers.PodManager().GetPodsWithLabelSelector(AwsNodeLabelKey, utils.AwsNodeName)
				g.Expect(err).ToNot(HaveOccurred())
				for _, cond := range podList.Items[0].Status.Conditions {
					if cond.Type == v1.PodReady {
						g.Expect(cond.Status).To(BeEquivalentTo(v1.ConditionFalse), fmt.Sprintf("%s should not be ready", podList.Items[0].Name))
						break
					}
				}
			}).WithTimeout(utils.PollIntervalLong).WithPolling(utils.PollIntervalLong / 10)
		})

		AfterEach(func() {
			By("attaching VPC_CNI policy")
			err = f.CloudServices.IAM().AttachRolePolicy(EKSCNIPolicyARN, role)
			Expect(err).ToNot(HaveOccurred())

			if rolePolicyDocumentNode != "" {
				By("Attaching the inline role policy for worker Node")
				err = f.CloudServices.IAM().PutRolePolicy(rolePolicyDocumentNode, nodePolicyName, nodePolicyName)
				Expect(err).ToNot(HaveOccurred())
			}

			if rolePolicyDocumentMaster != "" {
				By("Attaching the inline role policy for Master Nodes")
				err = f.CloudServices.IAM().PutRolePolicy(rolePolicyDocumentNode, masterPolicyName, masterPolicyName)
				Expect(err).ToNot(HaveOccurred())
			}

			RestartAwsNodePods()

			By("checking aws-node pods are running")
			Eventually(func(g Gomega) {
				podList, err := f.K8sResourceManagers.PodManager().GetPodsWithLabelSelector(AwsNodeLabelKey, utils.AwsNodeName)
				g.Expect(err).ToNot(HaveOccurred())
				for _, cond := range podList.Items[0].Status.Conditions {
					if cond.Type != v1.PodReady {
						continue
					}
					g.Expect(cond.Status).To(BeEquivalentTo(v1.ConditionTrue))
					break
				}
			}).WithTimeout(utils.PollIntervalLong).WithPolling(utils.PollIntervalLong / 10)
		})

		It("unauthorized event must be raised on aws-node pod", func() {
			By("waiting for event to be generated")
			// The event can take a long time to show up in client queries (I have seen up to 5 minutes)...
			time.Sleep(5 * time.Minute)
			listOpts := client.ListOptions{
				FieldSelector: fields.Set{"reason": "MissingIAMPermissions"}.AsSelector(),
				Namespace:     utils.AwsNodeNamespace,
			}
			eventList, err := f.K8sResourceManagers.EventManager().GetEventsWithOptions(&listOpts)
			Expect(err).ToNot(HaveOccurred())
			Expect(eventList.Items).NotTo(BeEmpty())
		})
	})
})

func RestartAwsNodePods() {
	By("Restarting aws-node pods")
	podList, err := f.K8sResourceManagers.PodManager().GetPodsWithLabelSelector(AwsNodeLabelKey, utils.AwsNodeName)
	Expect(err).ToNot(HaveOccurred())
	for _, pod := range podList.Items {
		f.K8sResourceManagers.PodManager().DeleteAndWaitTillPodDeleted(&pod)
	}
}
