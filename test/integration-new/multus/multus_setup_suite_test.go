package multus

import (
	"fmt"
	"testing"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	rbacV1 "k8s.io/api/rbac/v1"
)

var f *framework.Framework

func TestMultusSetup(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Multus Setup Suite")
}

var _ = BeforeSuite(func() {
	f = framework.New(framework.GlobalOptions)

	By("Creating network-attachment-definitions CRD")
	err := f.CrdManager.CreateNetworkAttachmentDefinitionCRD()
	Expect(err).NotTo(HaveOccurred())

	By("Creating ServiceAccount")
	err = f.K8sResourceManagers.ServiceAccountManager().CreateServiceAccount(utils.MultusResourceName, utils.AwsNodeNamespace)
	Expect(err).NotTo(HaveOccurred())

	rules := getRulesForMultusClusterRole()
	By("Creating ClusterRole")
	err = f.K8sResourceManagers.ClusterRoleManager().CreateClusterRole(utils.MultusResourceName, rules)
	Expect(err).NotTo(HaveOccurred())

	subjects := getSubjectsForMultusClusterRoleBinding()
	By("Creating ClusterRoleBinding")
	err = f.K8sResourceManagers.ClusterRoleManager().CreateClusterRoleBinding(utils.MultusResourceName, utils.MultusContainerName, subjects)
	Expect(err).NotTo(HaveOccurred())

	By("Creating And Wait till Multus Daemonset is Ready")
	err = f.K8sResourceManagers.DaemonSetManager().CreateAndWaitTillMultusDaemonSetReady(utils.MultusNodeName, utils.AwsNodeNamespace, utils.MultusImage)
	Expect(err).NotTo(HaveOccurred())

	By(fmt.Sprintf("getting the node with the node label key %s and value %s if specified, else get all nodes",
		f.Options.NgNameLabelKey, f.Options.NgNameLabelVal))
	nodes, err := f.K8sResourceManagers.NodeManager().GetNodes(f.Options.NgNameLabelKey, f.Options.NgNameLabelVal)
	Expect(err).ToNot(HaveOccurred())

	By("verifying that atleast 1 node is present for the test")
	Expect(len(nodes.Items)).Should(BeNumerically(">", 0))
})

var _ = AfterSuite(func() {
	By("Delete network-attachment-defintions CRD")
	err := f.CrdManager.DeleteCRD(utils.NetworkAttachmentDefinitionName)
	Expect(err).NotTo(HaveOccurred())

	By("Deleting ServiceAccount")
	err = f.K8sResourceManagers.ServiceAccountManager().DeleteServiceAccount(utils.MultusResourceName, utils.AwsNodeNamespace)
	Expect(err).NotTo(HaveOccurred())

	By("Deleting ClusterRole")
	err = f.K8sResourceManagers.ClusterRoleManager().DeleteClusterRole(utils.MultusResourceName)
	Expect(err).NotTo(HaveOccurred())

	By("Deleting ClusterRoleBinding")
	err = f.K8sResourceManagers.ClusterRoleManager().DeleteClusterRoleBinding(utils.MultusResourceName)
	Expect(err).NotTo(HaveOccurred())

	By("Deleting Multus Daemonset")
	err = f.K8sResourceManagers.DaemonSetManager().DeleteDaemonSet(utils.MultusNodeName, utils.AwsNodeNamespace)
	Expect(err).NotTo(HaveOccurred())
})

func getRulesForMultusClusterRole() []rbacV1.PolicyRule {
	clusterRoleRules := []rbacV1.PolicyRule{
		{
			APIGroups: []string{
				"k8s.cni.cncf.io",
			},
			Resources: []string{
				"*",
			},
			Verbs: []string{
				"*",
			},
		},
		{
			APIGroups: []string{
				"",
			},
			Resources: []string{
				"pods",
				"pods/status",
			},
			Verbs: []string{
				"get",
				"update",
			},
		},
		{
			APIGroups: []string{
				"",
				"events.k8s.io",
			},
			Resources: []string{
				"events",
			},
			Verbs: []string{
				"create",
				"patch",
				"update",
			},
		},
	}
	return clusterRoleRules
}

func getSubjectsForMultusClusterRoleBinding() []rbacV1.Subject {
	return []rbacV1.Subject{
		{
			Kind:      "ServiceAccount",
			Name:      "multus",
			Namespace: "kube-system",
		},
	}
}
