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
	"context"
	"flag"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework"
	k8sUtil "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"

	. "github.com/onsi/ginkgo/v2"
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

	clusterIDKeys []string
)

const (
	DEFAULT_CLUSTER_ID = "k8s-cluster"
)

// Parse optional flags for setting the cni metrics helper image
func init() {
	flag.StringVar(&imageRepository, "cni-metrics-helper-image-repo", "602401143452.dkr.ecr.us-west-2.amazonaws.com/cni-metrics-helper", "CNI Metrics Helper Image Repository")
	flag.StringVar(&imageTag, "cni-metrics-helper-image-tag", "v1.11.2", "CNI Metrics Helper Image Tag")

	// Order in which we try fetch the keys and use it as CLUSTER_ID dimension
	clusterIDKeys = []string{
		"eks:cluster-name",
		"CLUSTER_ID",
		"Name",
	}
}

func TestCNIMetricsHelper(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "CNI Metrics Helper Test Suite")
}

var _ = BeforeSuite(func() {
	f = framework.New(framework.GlobalOptions)

	By("creating test namespace")
	_ = f.K8sResourceManagers.NamespaceManager().CreateNamespace(utils.DefaultTestNamespace)

	By("getting the node list")
	nodeList, err := f.K8sResourceManagers.NodeManager().GetNodes(f.Options.NgNameLabelKey, f.Options.NgNameLabelVal)
	Expect(err).ToNot(HaveOccurred())
	Expect(len(nodeList.Items)).To(BeNumerically(">", 0))

	// pick a non-tainted node
	var instanceID string
	for i := range nodeList.Items {
		n := nodeList.Items[i]
		if len(n.Spec.Taints) == 0 {
			instanceID = k8sUtil.GetInstanceIDFromNode(n)
			nodeName = n.Name
			break
		}
	}
	Expect(instanceID).ToNot(Equal(""), "expected to find a non-tainted node")

	By("getting the nodegroup name and instance profile")
	instance, err := f.CloudServices.EC2().DescribeInstance(context.TODO(), instanceID)
	Expect(err).ToNot(HaveOccurred())

	instanceTagKeyValuePair := map[string]string{
		"eks:cluster-name": "",
		"CLUSTER_ID":       "",
		"Name":             "",
	}

	for _, instanceTag := range instance.Tags {
		if _, ok := instanceTagKeyValuePair[*instanceTag.Key]; ok {
			instanceTagKeyValuePair[*instanceTag.Key] = *instanceTag.Value
		}
	}

	for _, k := range clusterIDKeys {
		if tagVal, ok := instanceTagKeyValuePair[k]; ok && tagVal != "" {
			ngName = tagVal
			break
		}
	}

	if ngName == "" {
		ngName = DEFAULT_CLUSTER_ID
	}
	_, _ = fmt.Fprintf(GinkgoWriter, "cluster name: %s\n", ngName)

	By("getting the node instance role")
	instanceProfileRoleName := strings.Split(*instance.IamInstanceProfile.Arn, "instance-profile/")[1]
	instanceProfileOutput, err := f.CloudServices.IAM().GetInstanceProfile(context.TODO(), instanceProfileRoleName)
	Expect(err).ToNot(HaveOccurred())

	ngRoleName = *instanceProfileOutput.InstanceProfile.Roles[0].RoleName
	By("attaching CNIMetricsHelperPolicy to the node IAM role")

	// We should ideally use the PathPrefix argument to list the policy, but this is returning an empty list. So workaround by listing local policies & filter
	// SO issue: https://stackoverflow.com/questions/66287626/aws-cli-list-policies-to-find-a-policy-with-a-specific-name
	policyList, err := f.CloudServices.IAM().ListPolicies(context.TODO(), "Local")
	Expect(err).ToNot(HaveOccurred())

	for _, item := range policyList.Policies {
		if strings.Contains(*item.PolicyName, "CNIMetricsHelperPolicy") {
			policyARN = *item.Arn
			break
		}
	}

	err = f.CloudServices.IAM().AttachRolePolicy(context.TODO(), policyARN, ngRoleName)
	Expect(err).ToNot(HaveOccurred(), fmt.Sprintf("unable to attach arn: %s role: %s", policyARN, ngRoleName))

	By("updating the aws-nodes to restart the metric count")
	k8sUtil.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace,
		utils.AwsNodeName, map[string]string{"SOME_NON_EXISTENT_VAR": "0"})

	By("installing cni-metrics-helper using helm")
	err = f.InstallationManager.InstallCNIMetricsHelper(imageRepository, imageTag, ngName)
	Expect(err).ToNot(HaveOccurred())

	By("waiting for the metrics helper to publish initial metrics")
	time.Sleep(time.Minute * 3)
})

var _ = AfterSuite(func() {
	By("uninstalling cni-metrics-helper using helm")
	err := f.InstallationManager.UnInstallCNIMetricsHelper()
	Expect(err).ToNot(HaveOccurred())

	By("detaching role policy from the node IAM Role")
	err = f.CloudServices.IAM().DetachRolePolicy(context.TODO(), policyARN, ngRoleName)
	Expect(err).ToNot(HaveOccurred(), fmt.Sprintf("unable to detach %s %s", policyARN, ngRoleName))

	k8sUtil.RemoveVarFromDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace,
		utils.AwsNodeName, map[string]struct{}{"SOME_NON_EXISTENT_VAR": {}})

	By("deleting test namespace")
	_ = f.K8sResourceManagers.NamespaceManager().DeleteAndWaitTillNamespaceDeleted(utils.DefaultTestNamespace)
})
