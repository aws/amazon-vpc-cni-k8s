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

package metrics_helper

import (
	"encoding/json"
	"flag"
	"strings"
	"testing"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/aws/services"
	k8sUtil "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var (
	f   *framework.Framework
	err error
	// cni-metrics-repository name to use with helm install
	imageRepository string
	// cni-metrics-repository image tag to use with helm install
	imageTag string
	// CNIMetricsHelper policy ARN
	policyARN string
	// Node Group role where the CNI Metrics Policy is attached
	ngRoleName string
	// CNI Metrics is published with node group name as dimension
	ngName string
	// node name which has CW publish metric privileges
	nodeName string
)

// Parse optional flags for setting the cni metrics helper image
func init() {
	flag.StringVar(&imageRepository, "cni-metrics-helper-image-repo", "602401143452.dkr.ecr.us-west-2.amazonaws.com/cni-metrics-helper", "CNI Metrics Helper Image Repository")
	flag.StringVar(&imageTag, "cni-metrics-helper-image-tag", "v1.7.10", "CNI Metrics Helper Image Tag")
}

func TestCNIMetricsHelper(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "CNI Metrics Helper Test Suite")
}

var _ = BeforeSuite(func() {
	f = framework.New(framework.GlobalOptions)

	// Create a new policy with PutMetric Permission
	policy := services.PolicyDocument{
		Version: "2012-10-17",
		Statement: []services.StatementEntry{
			{
				Effect:   "Allow",
				Action:   []string{"cloudwatch:PutMetricData"},
				Resource: "*",
			},
		},
	}

	b, err := json.Marshal(policy)
	Expect(err).ToNot(HaveOccurred())

	By("creating the CNIMetricsHelperPolicy policy")
	createPolicyOutput, err := f.CloudServices.IAM().
		CreatePolicy("CNIMetricsHelperPolicy", string(b))
	Expect(err).ToNot(HaveOccurred())
	policyARN = *createPolicyOutput.Policy.Arn

	By("getting the node instance profile")
	nodeList, err := f.K8sResourceManagers.NodeManager().GetAllNodes()
	Expect(err).ToNot(HaveOccurred())
	Expect(len(nodeList.Items)).To(BeNumerically(">", 0))

	instanceID := k8sUtil.GetInstanceIDFromNode(nodeList.Items[0])
	nodeName = nodeList.Items[0].Name

	By("getting the nodegroup name and instance profile")
	instance, err := f.CloudServices.EC2().DescribeInstance(instanceID)
	Expect(err).ToNot(HaveOccurred())

	for _, instanceTag := range instance.Tags {
		if *instanceTag.Key == "Name" {
			ngName = *instanceTag.Value
		}
	}
	Expect(ngName).ToNot(BeEmpty())

	By("getting the node instance role")
	instanceProfileRoleName := strings.Split(*instance.IamInstanceProfile.Arn, "instance-profile/")[1]
	instanceProfileOutput, err := f.CloudServices.IAM().GetInstanceProfile(instanceProfileRoleName)
	Expect(err).ToNot(HaveOccurred())

	By("attaching policy to the node IAM role")
	ngRoleName = *instanceProfileOutput.InstanceProfile.Roles[0].RoleName
	By("attaching the node instance role")
	err = f.CloudServices.IAM().AttachRolePolicy(policyARN, ngRoleName)
	Expect(err).ToNot(HaveOccurred())

	By("updating the aws-nodes to restart the metric count")
	k8sUtil.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace,
		utils.AwsNodeName, map[string]string{"SOME_NON_EXISTENT_VAR": "0"})

	By("installing cni-metrics-helper using helm")
	err = f.InstallationManager.InstallCNIMetricsHelper(imageRepository, imageTag)
	Expect(err).ToNot(HaveOccurred())
})

var _ = AfterSuite(func() {
	k8sUtil.RemoveVarFromDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace,
		utils.AwsNodeName, map[string]struct{}{"SOME_NON_EXISTENT_VAR": {}})

	By("detaching role policy from the node IAM Role")
	err = f.CloudServices.IAM().DetachRolePolicy(policyARN, ngRoleName)
	Expect(err).ToNot(HaveOccurred())

	By("deleting the CNIMetricsHelperPolicy policy")
	err = f.CloudServices.IAM().DeletePolicy(policyARN)
	Expect(err).ToNot(HaveOccurred())

	By("uninstalling cni-metrics-helper using helm")
	err := f.InstallationManager.UnInstallCNIMetricsHelper()
	Expect(err).ToNot(HaveOccurred())
})
