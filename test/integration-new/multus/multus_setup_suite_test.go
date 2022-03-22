package multus

import (
	"fmt"
	"testing"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var f *framework.Framework

func TestMultusSetup(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Multus Setup Suite")
}

var _ = BeforeSuite(func() {
	f = framework.New(framework.GlobalOptions)

	By("Check if Multus Daemonset is Ready")
	_, err := f.K8sResourceManagers.DaemonSetManager().GetDaemonSet(utils.AwsNodeNamespace, utils.MultusNodeName)
	Expect(err).NotTo(HaveOccurred())

	err = f.K8sResourceManagers.DaemonSetManager().CheckIfDaemonSetIsReady(utils.AwsNodeNamespace, utils.MultusNodeName)
	Expect(err).NotTo(HaveOccurred())

	By(fmt.Sprintf("getting the node with the node label key %s and value %s if specified, else get all nodes",
		f.Options.NgNameLabelKey, f.Options.NgNameLabelVal))
	nodes, err := f.K8sResourceManagers.NodeManager().GetNodes(f.Options.NgNameLabelKey, f.Options.NgNameLabelVal)
	Expect(err).ToNot(HaveOccurred())

	By("verifying that atleast 1 node is present for the test")
	Expect(len(nodes.Items)).Should(BeNumerically(">", 0))
})
