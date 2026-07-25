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

	"github.com/aws/aws-sdk-go-v2/service/eks"
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
	// IRSA role name created for the metrics helper service account
	irsaRoleName string
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
	flag.StringVar(&imageTag, "cni-metrics-helper-image-tag", "v1.22.3", "CNI Metrics Helper Image Tag")

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

	By("getting the nodegroup name from instance tags")
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

	By("getting the cluster OIDC issuer URL")
	describeClusterOutput, err := f.CloudServices.EKS().DescribeCluster(context.TODO(), f.Options.ClusterName)
	Expect(err).ToNot(HaveOccurred())
	oidcIssuer := getOIDCIssuer(describeClusterOutput)
	// Strip the https:// prefix to get the OIDC provider host path
	oidcProvider := strings.TrimPrefix(oidcIssuer, "https://")

	By("looking up the CNIMetricsHelperPolicy ARN")
	policyList, err := f.CloudServices.IAM().ListPolicies(context.TODO(), "Local")
	Expect(err).ToNot(HaveOccurred())

	for _, item := range policyList.Policies {
		if strings.Contains(*item.PolicyName, "CNIMetricsHelperPolicy") {
			policyARN = *item.Arn
			break
		}
	}
	Expect(policyARN).ToNot(Equal(""), "CNIMetricsHelperPolicy not found")

	By("creating an IRSA role for cni-metrics-helper")
	// Extract partition and account ID from the policy ARN (arn:<PARTITION>:iam::<ACCOUNT_ID>:policy/...)
	arnParts := strings.Split(policyARN, ":")
	Expect(len(arnParts)).To(BeNumerically(">=", 5), fmt.Sprintf("unexpected ARN format: %s", policyARN))
	partition := arnParts[1]
	accountID := arnParts[4]
	Expect(partition).ToNot(BeEmpty(), fmt.Sprintf("partition is empty in ARN: %s", policyARN))
	Expect(accountID).ToNot(BeEmpty(), fmt.Sprintf("account ID is empty in ARN: %s", policyARN))
	irsaRoleName = "cni-metrics-helper-irsa-" + f.Options.ClusterName
	// IAM role names have a 64-character limit
	if len(irsaRoleName) > 64 {
		irsaRoleName = irsaRoleName[:64]
	}

	trustPolicy := fmt.Sprintf(`{
		"Version": "2012-10-17",
		"Statement": [{
			"Effect": "Allow",
			"Principal": {
				"Federated": "arn:%s:iam::%s:oidc-provider/%s"
			},
			"Action": "sts:AssumeRoleWithWebIdentity",
			"Condition": {
				"StringEquals": {
					"%s:sub": "system:serviceaccount:kube-system:cni-metrics-helper",
					"%s:aud": "sts.amazonaws.com"
				}
			}
		}]
	}`, partition, accountID, oidcProvider, oidcProvider, oidcProvider)

	createRoleOutput, err := f.CloudServices.IAM().CreateRole(context.TODO(), irsaRoleName, trustPolicy)
	Expect(err).ToNot(HaveOccurred())
	roleARN := *createRoleOutput.Role.Arn
	_, _ = fmt.Fprintf(GinkgoWriter, "created IRSA role: %s\n", roleARN)

	By("attaching CNIMetricsHelperPolicy to the IRSA role")
	err = f.CloudServices.IAM().AttachRolePolicy(context.TODO(), policyARN, irsaRoleName)
	Expect(err).ToNot(HaveOccurred())

	By("updating the aws-nodes to restart the metric count")
	k8sUtil.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace,
		utils.AwsNodeName, map[string]string{"SOME_NON_EXISTENT_VAR": "0"})

	By("installing cni-metrics-helper using helm with IRSA")
	err = f.InstallationManager.InstallCNIMetricsHelper(imageRepository, imageTag, ngName, f.Options.AWSRegion, roleARN)
	Expect(err).ToNot(HaveOccurred())

	By("waiting for the metrics helper to publish initial metrics")
	time.Sleep(time.Minute * 1)
})

var _ = AfterSuite(func() {
	By("uninstalling cni-metrics-helper using helm")
	err := f.InstallationManager.UnInstallCNIMetricsHelper()
	Expect(err).ToNot(HaveOccurred())

	if irsaRoleName != "" && policyARN != "" {
		By("detaching policy from the IRSA role")
		if err := f.CloudServices.IAM().DetachRolePolicy(context.TODO(), policyARN, irsaRoleName); err != nil {
			_, _ = fmt.Fprintf(GinkgoWriter, "warning: failed to detach policy: %v\n", err)
		}
	}

	if irsaRoleName != "" {
		By("deleting the IRSA role")
		if err := f.CloudServices.IAM().DeleteRole(context.TODO(), irsaRoleName); err != nil {
			_, _ = fmt.Fprintf(GinkgoWriter, "warning: failed to delete IRSA role: %v\n", err)
		}
	}

	k8sUtil.RemoveVarFromDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace,
		utils.AwsNodeName, map[string]struct{}{"SOME_NON_EXISTENT_VAR": {}})

	By("deleting test namespace")
	_ = f.K8sResourceManagers.NamespaceManager().DeleteAndWaitTillNamespaceDeleted(utils.DefaultTestNamespace)
})

// getOIDCIssuer extracts the OIDC issuer URL from DescribeCluster output,
// failing the test with a clear message if OIDC is not configured.
func getOIDCIssuer(output *eks.DescribeClusterOutput) string {
	Expect(output.Cluster).ToNot(BeNil(), "DescribeCluster returned nil Cluster")
	Expect(output.Cluster.Identity).ToNot(BeNil(), "Cluster.Identity is nil")
	Expect(output.Cluster.Identity.Oidc).ToNot(BeNil(), "Cluster.Identity.Oidc is nil")
	Expect(output.Cluster.Identity.Oidc.Issuer).ToNot(BeNil(),
		"OIDC Issuer is nil - ensure OIDC is enabled on the cluster")
	return *output.Cluster.Identity.Oidc.Issuer
}
