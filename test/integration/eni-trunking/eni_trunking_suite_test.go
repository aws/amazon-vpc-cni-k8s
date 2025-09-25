package eni_trunking

import (
	"fmt"
	"testing"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/aws/utils"
	testUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	nonEniTrunkingLabel = "non-eni-trunking"

	nonEniTrunkingInstanceType = "t3.medium"
)

var (
	f               *framework.Framework
	props           utils.NodeGroupProperties
	privateSubnetId string
)

func TestEniTrunking(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "ENI Trunking Suite")
}

var _ = BeforeSuite(func() {
	f = framework.New(framework.GlobalOptions)

	By("creating test namespace")
	f.K8sResourceManagers.NamespaceManager().
		CreateNamespace(testUtils.DefaultTestNamespace)

	By("Getting Private subnets")
	vpcConfig, err := utils.GetClusterVPCConfig(f)
	Expect(err).ToNot(HaveOccurred())

	Expect(len(vpcConfig.PrivateSubnetList)).To(BeNumerically(">", 0))

	privateSubnetId = vpcConfig.PrivateSubnetList[0]

	msg := fmt.Sprintf("Deploying non-eni-trunking %s managed nodegroup of size 1", nonEniTrunkingInstanceType)
	By(msg)
	props = utils.NodeGroupProperties{
		AsgSize:       1,
		NodeGroupName: nonEniTrunkingLabel,
		Subnet: []string{
			privateSubnetId,
		},
		InstanceType: nonEniTrunkingInstanceType,
	}

	err = utils.CreateAndWaitTillManagedNGReady(f, props)
	Expect(err).NotTo(HaveOccurred())
})

var _ = AfterSuite(func() {
	By("Deleting test namespace")
	f.K8sResourceManagers.NamespaceManager().
		DeleteAndWaitTillNamespaceDeleted(testUtils.DefaultTestNamespace)

	By("Deleting Managed Nodegroup")
	err := utils.DeleteAndWaitTillSelfManagedNGStackDeleted(f, props)
	Expect(err).NotTo(HaveOccurred())
})
