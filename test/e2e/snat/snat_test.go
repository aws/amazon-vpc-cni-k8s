package snat

import (
	"fmt"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	"github.com/aws/aws-sdk-go/aws"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
)

const (
	TEST_POD_LABEL_KEY   = "test-pod-label-key"
	TEST_POD_LABEL_VALUE = "test-pod-label-val"
	EXTERNAL_DOMAIN      = "https://aws.amazon.com/"
)

var _ = Describe("SNAT test", func() {
	It("Pod in private subnet should have Internet access with External SNAT disabled", func() {
		By("Setting External SNAT to false")
		k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace, utils.AwsNodeName, map[string]string{
			"AWS_VPC_K8S_CNI_EXTERNALSNAT": "false",
		})

		By("Checking External Domain Connectivity")
		ValidateExternalDomainConnectivity(EXTERNAL_DOMAIN)
	})

	It("Pod in private subnet should have Internet access with External SNAT enabled", func() {
		By("Setting External SNAT to true")
		k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace, utils.AwsNodeName, map[string]string{
			"AWS_VPC_K8S_CNI_EXTERNALSNAT": "true",
		})

		By("Checking External Domain Connectivity")
		ValidateExternalDomainConnectivity(EXTERNAL_DOMAIN)
	})

	It("Verify SNAT IP table rule by changing AWS_VPC_K8S_CNI_RANDOMIZESNAT", func() {
		vpcOutput, err := f.CloudServices.EC2().DescribeVPC(f.Options.AWSVPCID)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(vpcOutput.Vpcs)).To(BeNumerically(">", 0))

		numOfCidrs := len(vpcOutput.Vpcs[0].CidrBlockAssociationSet)

		By("Check whether SNAT IP table has random-fully with AWS_VPC_K8S_CNI_RANDOMIZESNAT set to default value of prng")
		ValidateIPTableRules("prng", numOfCidrs)

		By("Setting AWS_VPC_K8S_CNI_RANDOMIZESNAT to none")
		k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace, utils.AwsNodeName, map[string]string{
			"AWS_VPC_K8S_CNI_RANDOMIZESNAT": "none",
		})

		By("Check where SNAT IP table rule is updated and it doesn't contain random port allocation")
		ValidateIPTableRules("none", numOfCidrs)
	})

	It("Verify External Domain Connectivity by modifying AWS_VPC_K8S_CNI_EXCLUDE_SNAT_CIDRS", func() {
		By("Getting CIDR for primary node's private subnet")
		out, err := f.CloudServices.EC2().DescribeSubnet(privateSubnetId)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(out.Subnets)).To(BeNumerically(">", 0))

		cidrBlock := out.Subnets[0].CidrBlock
		By("Updating AWS_VPC_K8S_CNI_EXCLUDE_SNAT_CIDRS with private subnet CIDR")
		k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace, utils.AwsNodeName, map[string]string{
			"AWS_VPC_K8S_CNI_EXCLUDE_SNAT_CIDRS": aws.StringValue(cidrBlock),
		})

		By("Check External domain connectivity from this private subnet CIDR block")
		ValidateExternalDomainConnectivity(EXTERNAL_DOMAIN)
	})

	AfterEach(func() {
		By("Reverting aws-node env variables to default values")
		k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace, utils.AwsNodeName, map[string]string{
			"AWS_VPC_K8S_CNI_EXTERNALSNAT":  "false",
			"AWS_VPC_K8S_CNI_RANDOMIZESNAT": "prng",
		})
		k8sUtils.RemoveVarFromDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace, utils.AwsNodeName, map[string]struct{}{
			"AWS_VPC_K8S_CNI_EXCLUDE_SNAT_CIDRS": {},
		})
	})
})

func ValidateExternalDomainConnectivity(url string) {
	testerArgs := []string{
		"-testExternalDomainConnectivity=true",
		fmt.Sprintf("-url=%s", url),
	}

	testContainer := manifest.NewTestHelperContainer().
		Command([]string{"./snat-utils"}).
		Args(testerArgs).
		Build()

	testPodManifest := manifest.NewDefaultPodBuilder().
		Container(testContainer).
		NodeName(primaryNodeInPrivateSubnet.Name).
		Name("snat-test-pod").
		Build()

	By("Deploying a test pod to check External domain access")
	testPod, err := f.K8sResourceManagers.PodManager().
		CreateAndWaitTillPodCompleted(testPodManifest)
	Expect(err).NotTo(HaveOccurred())

	logs, errLogs := f.K8sResourceManagers.PodManager().
		PodLogs(testPod.Namespace, testPod.Name)
	Expect(errLogs).ToNot(HaveOccurred())

	fmt.Fprintln(GinkgoWriter, logs)

	By("deleting the test pod")
	err = f.K8sResourceManagers.PodManager().
		DeleteAndWaitTillPodDeleted(testPod)
	Expect(err).ToNot(HaveOccurred())
}

func ValidateIPTableRules(randomizedSNATValue string, numOfCidrs int) {
	testerArgs := []string{
		"-testIPTableRules=true",
		fmt.Sprintf("-randomizedSNATValue=%s", randomizedSNATValue),
		fmt.Sprintf("-numOfCidrs=%d", numOfCidrs),
	}

	hostNetworkContainer := manifest.NewTestHelperContainer().
		Command([]string{"./snat-utils"}).
		CapabilitiesForSecurityContext([]corev1.Capability{
			"NET_ADMIN",
		}, nil).
		Args(testerArgs).
		Build()

	hostNetworkPodManifest := manifest.NewDefaultPodBuilder().
		Container(hostNetworkContainer).
		NodeName(primaryNodeInPublicSubnet.Name).
		Name("host-network").
		HostNetwork(true).
		Build()

	By("creating pod to check iptable SNAT rules on the host")
	hostNetworkPod, err := f.K8sResourceManagers.PodManager().
		CreateAndWaitTillPodCompleted(hostNetworkPodManifest)
	Expect(err).NotTo(HaveOccurred())

	logs, errLogs := f.K8sResourceManagers.PodManager().
		PodLogs(hostNetworkPod.Namespace, hostNetworkPod.Name)
	Expect(errLogs).ToNot(HaveOccurred())

	fmt.Fprintln(GinkgoWriter, logs)

	By("deleting the host networking setup pod")
	err = f.K8sResourceManagers.PodManager().
		DeleteAndWaitTillPodDeleted(hostNetworkPod)
	Expect(err).ToNot(HaveOccurred())
}
