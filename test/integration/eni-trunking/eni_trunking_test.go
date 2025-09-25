package eni_trunking

import (
	"fmt"

	awsUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/aws/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	vpcControllerVpc "github.com/aws/amazon-vpc-resource-controller-k8s/pkg/aws/vpc"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ENI Trunking Suite", func() {
	Context("ENABLE_POD_ENI=true", func() {
		BeforeEach(func() {
			k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace, utils.AwsNodeName, map[string]string{
				"ENABLE_POD_ENI": "true",
			})
		})

		It("Non ENI trunking instance can scale to maxPods", func() {
			spec := vpcControllerVpc.Limits[nonEniTrunkingInstanceType]
			maxPods := spec.Interface*(spec.IPv4PerInterface-1) - 2 // Exclude 2 coredns, 1 aws-node, 1 kube-proxy

			By(fmt.Sprintf("Deploying %d Busybox pods", maxPods))
			deployment := manifest.NewBusyBoxDeploymentBuilder(f.Options.TestImageRegistry).
				Replicas(maxPods).
				NodeSelector(awsUtils.ManagedNodeGroupNameLabelKey, nonEniTrunkingLabel).
				Build()
			_, err := f.K8sResourceManagers.DeploymentManager().CreateAndWaitTillDeploymentIsReady(deployment, utils.DefaultDeploymentReadyTimeout)
			Expect(err).ToNot(HaveOccurred())
		})
	})
})
