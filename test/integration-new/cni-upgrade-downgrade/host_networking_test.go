package cni_upgrade_downgrade

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/integration-new/common"
	"github.com/go-errors/errors"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/apps/v1"
)

var _ = Describe("test host networking", func() {
	var err error
	var podLabelKey = "app"
	var podLabelVal = "host-networking-test"
	var deployment *v1.Deployment
	var podInput string
	var initialManifest string
	var targetManifest string

	Context("when pods using IP from primary and secondary ENI are created", func() {
		It("should have correct host networking setup when pods are running and cleaned up when pods are terminated", func() {
			initialManifest = f.Options.InitialManifest
			targetManifest = f.Options.TargetManifest
			if len(targetManifest) == 0 {
				err = errors.Errorf("Target Manifest file must be specified")
			}
			Expect(err).NotTo(HaveOccurred())

			if len(initialManifest) != 0 {
				ApplyCNIManifest(initialManifest)
			} else {
				By("Using existing cni manifest")
			}

			// Launch enough pods so some pods end up using primary ENI IP and some using secondary
			// ENI IP
			deployment = manifest.NewBusyBoxDeploymentBuilder().
				Replicas(maxIPPerInterface*2).
				PodLabel(podLabelKey, podLabelVal).
				NodeName(primaryNode.Name).
				Build()

			By("creating a deployment to launch pod using primary and secondary ENI IP")
			deployment, err = f.K8sResourceManagers.DeploymentManager().
				CreateAndWaitTillDeploymentIsReady(deployment, utils.DefaultDeploymentReadyTimeout)
			Expect(err).ToNot(HaveOccurred())

			By("getting the list of pods using IP from primary and secondary ENI")
			interfaceTypeToPodList := common.GetPodsOnPrimaryAndSecondaryInterface(primaryNode, podLabelKey, podLabelVal, f)

			// Primary ENI and Secondary ENI IPs are handled differently when setting up
			// the host networking rule hence this check
			Expect(len(interfaceTypeToPodList.PodsOnSecondaryENI)).
				Should(BeNumerically(">", 0))
			Expect(len(interfaceTypeToPodList.PodsOnPrimaryENI)).
				Should(BeNumerically(">", 0))

			By("generating the pod networking validation input to be passed to tester")
			podInput, err = common.GetPodNetworkingValidationInput(interfaceTypeToPodList, vpcCIDRs).Serialize()
			Expect(err).NotTo(HaveOccurred())

			By("validating host networking setup is setup correctly")
			common.ValidateHostNetworking(common.NetworkingSetupSucceeds, podInput, primaryNode.Name, f)

			By("update cni to target manifest")
			ApplyCNIManifest(targetManifest)

			By("deleting the deployment to test teardown")
			err = f.K8sResourceManagers.DeploymentManager().
				DeleteAndWaitTillDeploymentIsDeleted(deployment)
			Expect(err).ToNot(HaveOccurred())

			By("waiting to allow CNI to tear down networking for terminated pods")
			time.Sleep(time.Second * 60)

			By("validating host networking is teared down correctly")
			common.ValidateHostNetworking(common.NetworkingTearDownSucceeds, podInput, primaryNode.Name, f)
		})
	})
})

func ApplyCNIManifest(filepath string) {
	var stdoutBuf, stderrBuf bytes.Buffer
	By(fmt.Sprintf("Applying manifest: %s", filepath))
	cmd := exec.Command("kubectl", "apply", "-f", filepath)
	cmd.Stdout = io.MultiWriter(os.Stdout, &stdoutBuf)
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)
	err := cmd.Run()
	Expect(err).NotTo(HaveOccurred())
}
