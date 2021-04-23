package env_vars

import (
	"regexp"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

const (
	AWS_VPC_ENI_MTU            = "AWS_VPC_ENI_MTU"
	AWS_VPC_K8S_CNI_LOG_FILE   = "AWS_VPC_K8S_CNI_LOG_FILE"
	AWS_VPC_K8S_CNI_VETHPREFIX = "AWS_VPC_K8S_CNI_VETHPREFIX"
)

var _ = Describe("cni env test", func() {

	Context("CNI Environment Variables", func() {
		It("Verifying that secondary ENI is created", func() {
			nodes, err := f.K8sResourceManagers.NodeManager().GetAllNodes()
			Expect(err).NotTo(HaveOccurred())

			for _, node := range nodes.Items {
				instanceId := k8sUtils.GetInstanceIDFromNode(node)
				instance, err := f.CloudServices.EC2().DescribeInstance(instanceId)
				Expect(err).NotTo(HaveOccurred())

				len := len(instance.NetworkInterfaces)
				Expect(len).To(BeNumerically(">=", 2))
			}
		})

		It("Changing AWS_VPC_ENI_MTU and AWS_VPC_K8S_CNI_VETHPREFIX", func() {
			currMTUVal := getEnvValueForKey(AWS_VPC_ENI_MTU)
			Expect(currMTUVal).NotTo(Equal(""))

			currVETHPrefix := getEnvValueForKey(AWS_VPC_K8S_CNI_VETHPREFIX)
			Expect(currVETHPrefix).NotTo(Equal(""))

			k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, DAEMONSET, NAMESPACE, DAEMONSET, map[string]string{
				AWS_VPC_ENI_MTU:            "1300",
				AWS_VPC_K8S_CNI_VETHPREFIX: "veth",
			})

			By("Deploying a BusyBox deployment")
			{
				deploymentSpec := manifest.NewBusyBoxDeploymentBuilder().
					Namespace("default").
					Name("busybox").
					Replicas(1).
					NodeName(primaryNode.Name).
					Build()

				_, err := f.K8sResourceManagers.
					DeploymentManager().
					CreateAndWaitTillDeploymentIsReady(deploymentSpec)

				Expect(err).ToNot(HaveOccurred())

				stdout, _, err := f.K8sResourceManagers.PodManager().PodExec("default", hostNetworkPod.Name, []string{"ifconfig"})
				Expect(err).NotTo(HaveOccurred())

				re := regexp.MustCompile(`\n`)
				input := re.ReplaceAllString(stdout, "")

				re = regexp.MustCompile(`eth.*lo`)
				eth := re.FindStringSubmatch(input)[0]

				re = regexp.MustCompile(`MTU:[0-9]*`)
				mtus := re.FindAllStringSubmatch(eth, -1)

				By("Validating new MTU value")
				{
					// Validate MTU
					for _, m := range mtus {
						Expect(m[0]).To(Equal("MTU:1300"))
					}
				}

				By("Validating new VETH Prefix")
				{
					// Validate VETH Prefix
					// Adding the new MTU value to below regex ensures that we are checking the recently created
					// veth and not any older entries
					re = regexp.MustCompile(`veth.*MTU:1300`)
					veth := re.FindAllString(input, -1)

					Expect(len(veth)).NotTo(Equal(0))
				}

				By("Deleting BusyBox Deployment")
				{
					err = f.K8sResourceManagers.DeploymentManager().DeleteAndWaitTillDeploymentIsDeleted(deploymentSpec)
					Expect(err).NotTo(HaveOccurred())
				}
			}
			By("Restoring old value on daemonset")
			{
				restoreOldValues(map[string]string{
					AWS_VPC_ENI_MTU:            currMTUVal,
					AWS_VPC_K8S_CNI_VETHPREFIX: currVETHPrefix,
				})
			}
		})

		It("Changing AWS_VPC_K8S_CNI_LOG_FILE", func() {
			currLogFilepath := getEnvValueForKey(AWS_VPC_K8S_CNI_LOG_FILE)
			Expect(currLogFilepath).NotTo(Equal(""))

			newLogFile := "ipamd_test.log"
			k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, DAEMONSET, NAMESPACE, DAEMONSET, map[string]string{
				AWS_VPC_K8S_CNI_LOG_FILE: "/host/var/log/aws-routed-eni/" + newLogFile,
			})

			stdout, _, err := f.K8sResourceManagers.PodManager().PodExec("default", hostNetworkPod.Name, []string{"tail", "-n", "5", "ipamd-logs/ipamd_test.log"})
			Expect(err).NotTo(HaveOccurred())

			Expect(stdout).NotTo(Equal(""))

			By("Restoring old value on daemonset")
			{
				restoreOldValues(map[string]string{
					AWS_VPC_K8S_CNI_LOG_FILE: currLogFilepath,
				})
			}
		})
	})
})

func getEnvValueForKey(key string) string {
	envVar := ds.Spec.Template.Spec.Containers[0].Env
	for _, env := range envVar {
		if env.Name == key {
			return env.Value
		}
	}
	return ""
}

func restoreOldValues(oldVals map[string]string) {
	k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, DAEMONSET, NAMESPACE, DAEMONSET, oldVals)
}
