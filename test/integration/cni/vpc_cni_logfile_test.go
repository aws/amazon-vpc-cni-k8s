package cni

import (
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsV1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
)

const (
	AWS_VPC_K8S_CNI_LOG_FILE = "AWS_VPC_K8S_CNI_LOG_FILE"
	POD_VOL_LABEL_KEY        = "MountVolume"
	POD_VOL_LABEL_VAL        = "true"
	VOLUME_NAME              = "ipamd-logs"
	VOLUME_MOUNT_PATH        = "/var/log/aws-routed-eni/"
)

var _ = Describe("aws-node env test", func() {
	var (
		deploymentSpecWithVol *appsV1.Deployment
	)

	var _ = JustBeforeEach(func() {
		By("Deploying a host network deployment with Volume mount")
		container := manifest.NewBusyBoxContainerBuilder(f.Options.TestImageRegistry).Build()

		volume := []v1.Volume{
			{
				Name: VOLUME_NAME,
				VolumeSource: v1.VolumeSource{
					HostPath: &v1.HostPathVolumeSource{
						Path: VOLUME_MOUNT_PATH,
					},
				},
			},
		}

		volumeMount := []v1.VolumeMount{
			{
				Name:      VOLUME_NAME,
				MountPath: VOLUME_NAME,
			},
		}

		deploymentSpecWithVol = manifest.NewDefaultDeploymentBuilder().
			Namespace("default").
			Name("host-network").
			Replicas(1).
			HostNetwork(true).
			Container(container).
			PodLabel(POD_VOL_LABEL_KEY, POD_VOL_LABEL_VAL).
			MountVolume(volume, volumeMount).
			NodeName(primaryNode.Name).
			Build()

		_, err := f.K8sResourceManagers.
			DeploymentManager().
			CreateAndWaitTillDeploymentIsReady(deploymentSpecWithVol, utils.DefaultDeploymentReadyTimeout)
		Expect(err).ToNot(HaveOccurred())
	})

	It("aws-node environment variable AWS_VPC_K8S_CNI_LOG_FILE test", func() {
		By("Checking for pod with volume mount")
		pods, err := f.K8sResourceManagers.PodManager().GetPodsWithLabelSelector(POD_VOL_LABEL_KEY, POD_VOL_LABEL_VAL)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(pods.Items)).Should(BeNumerically(">", 0))

		podWithVol := pods.Items[0]

		newLogFile := "ipamd_test.log"
		k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace, utils.AwsNodeName, map[string]string{
			AWS_VPC_K8S_CNI_LOG_FILE: "/host/var/log/aws-routed-eni/" + newLogFile,
		})

		stdout, _, err := f.K8sResourceManagers.PodManager().PodExec("default", podWithVol.Name, []string{"tail", "-n", "5", "ipamd-logs/ipamd_test.log"})
		Expect(err).NotTo(HaveOccurred())
		Expect(stdout).NotTo(Equal(""))

	})

	var _ = JustAfterEach(func() {
		By("Restoring old value on daemonset")
		k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace, utils.AwsNodeName, map[string]string{
			AWS_VPC_K8S_CNI_LOG_FILE: "/host/var/log/aws-routed-eni/ipamd.log",
		})

		By("Deleing deployment with Volume Mount")
		err := f.K8sResourceManagers.DeploymentManager().DeleteAndWaitTillDeploymentIsDeleted(deploymentSpecWithVol)
		Expect(err).NotTo(HaveOccurred())
	})
})
