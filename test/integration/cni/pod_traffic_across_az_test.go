package cni

import (
	"fmt"
	"strconv"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/integration/common"
	"github.com/aws/aws-sdk-go/service/ec2"
	coreV1 "k8s.io/api/core/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/apps/v1"
)

// Tests pod networking across AZs. It similar to pod connectivity test, but launches a daemonset, so that
// there is a pod on each node across AZs. It then tests connectivity between pods on different nodes across AZs.
var _ = Describe("[STATIC_CANARY] test pod networking", FlakeAttempts(3), func() {

	var (
		err        error
		serverPort int
		protocol   string

		// The command to run on server pods, to allow incoming
		// connections for different traffic type
		serverListenCmd []string
		// Arguments to the server listen command
		serverListenCmdArgs []string

		// The function that generates command which will be sent from
		// tester pod to receiver pod
		testConnectionCommandFunc func(serverPod coreV1.Pod, port int) []string

		// Expected stdout from the exec command on testing connection
		// from tester to server
		testerExpectedStdOut string
		// Expected stderr from the exec command on testing connection
		// from tester to server
		testerExpectedStdErr string

		// Daemonset to run on node
		testDaemonSet *v1.DaemonSet

		// Map of AZ name, string to pod of testDaemonSet
		azToTestPod map[string]coreV1.Pod
	)

	JustBeforeEach(func() {
		By("authorizing security group ingress on instance security group")
		err = f.CloudServices.EC2().
			AuthorizeSecurityGroupIngress(instanceSecurityGroupID, protocol, serverPort, serverPort, "0.0.0.0/0")
		Expect(err).ToNot(HaveOccurred())

		By("authorizing security group egress on instance security group")
		err = f.CloudServices.EC2().
			AuthorizeSecurityGroupEgress(instanceSecurityGroupID, protocol, serverPort, serverPort, "0.0.0.0/0")
		Expect(err).ToNot(HaveOccurred())

		netcatContainer := manifest.
			NewNetCatAlpineContainer(f.Options.TestImageRegistry).
			Command(serverListenCmd).
			Args(serverListenCmdArgs).
			Build()

		By("creating a server DaemonSet on primary node")

		testDaemonSet = manifest.
			NewDefaultDaemonsetBuilder().
			Container(netcatContainer).
			PodLabel("role", "az-test").
			Name("netcat-daemonset").
			Build()

		_, err = f.K8sResourceManagers.DaemonSetManager().CreateAndWaitTillDaemonSetIsReady(testDaemonSet, utils.DefaultDeploymentReadyTimeout)
		Expect(err).ToNot(HaveOccurred())

		By(fmt.Sprintf("getting the node with the node label key %s and value %s",
			f.Options.NgNameLabelKey, f.Options.NgNameLabelVal))

		nodes, err := f.K8sResourceManagers.NodeManager().GetNodes(f.Options.NgNameLabelKey, f.Options.NgNameLabelVal)

		Expect(err).ToNot(HaveOccurred())

		azToTestPod = GetAZtoPod(nodes)
	})

	JustAfterEach(func() {
		By("revoking security group ingress on instance security group")
		err = f.CloudServices.EC2().
			RevokeSecurityGroupIngress(instanceSecurityGroupID, protocol, serverPort, serverPort, "0.0.0.0/0")
		Expect(err).ToNot(HaveOccurred())

		By("revoking security group egress on instance security group")
		err = f.CloudServices.EC2().
			RevokeSecurityGroupEgress(instanceSecurityGroupID, protocol, serverPort, serverPort, "0.0.0.0/0")
		Expect(err).ToNot(HaveOccurred())

		By("deleting the Daemonset.")
		err = f.K8sResourceManagers.DaemonSetManager().DeleteAndWaitTillDaemonSetDeleted(testDaemonSet, utils.DefaultDeploymentReadyTimeout)
		Expect(err).ToNot(HaveOccurred())
	})

	Context("While testing connectivity across AZ", func() {

		BeforeEach(func() {
			serverPort = 2273
			protocol = ec2.ProtocolTcp
			// Test tcp connection using netcat
			serverListenCmd = []string{"nc"}
			// The nc flag "-l" for listen mode, "-k" to keep server up and not close
			// connection after each connection
			serverListenCmdArgs = []string{"-k", "-l", strconv.Itoa(serverPort)}
			// netcat verbose output is being redirected to stderr instead of stdout
			testerExpectedStdErr = "succeeded!"
			testerExpectedStdOut = ""

			// The nc flag "-v" for verbose output and "-wn" for timing out in n seconds
			testConnectionCommandFunc = func(receiverPod coreV1.Pod, port int) []string {
				return []string{"nc", "-v", "-w2", receiverPod.Status.PodIP, strconv.Itoa(port)}
			}
		})

		It("Should allow TCP traffic across AZs.", func() {
			CheckConnectivityBetweenPods(azToTestPod, serverPort, testerExpectedStdOut, testerExpectedStdErr, testConnectionCommandFunc)
		})
	})
})

func GetAZtoPod(nodes coreV1.NodeList) map[string]coreV1.Pod {
	// Map of AZ name to Pod from Daemonset running on nodes
	azToPod := make(map[string]coreV1.Pod)
	for i := range nodes.Items {
		// node label key "topology.kubernetes.io/zone" is well known label populated by cloud controller manager
		// guaranteed to be present and represent the AZ name
		// Ref https://kubernetes.io/docs/reference/labels-annotations-taints/#topologykubernetesiozone
		azName := nodes.Items[i].ObjectMeta.Labels["topology.kubernetes.io/zone"]
		interfaceToPodList := common.GetPodsOnPrimaryAndSecondaryInterface(nodes.Items[i], "role", "az-test", f)
		// It doesn't matter which ENI the pod is on, as long as it is on the node
		if len(interfaceToPodList.PodsOnSecondaryENI) > 0 {
			azToPod[azName] = interfaceToPodList.PodsOnSecondaryENI[0]
		}
		if len(interfaceToPodList.PodsOnPrimaryENI) > 0 {
			azToPod[azName] = interfaceToPodList.PodsOnPrimaryENI[0]
		}
	}
	return azToPod
}

var _ = Describe("[STATIC_CANARY] API Server Connectivity from AZs", FlakeAttempts(3), func() {

	var (
		err           error
		testDaemonSet *v1.DaemonSet

		// Map of AZ name to Pod of testDaemonSet running on nodes
		azToPod map[string]coreV1.Pod
	)

	JustBeforeEach(func() {
		serverContainer := manifest.
			NewCurlContainer().
			Command([]string{
				"sleep",
				"3600",
			}).
			Build()

		By("creating a server DaemonSet on primary node")

		testDaemonSet = manifest.
			NewDefaultDaemonsetBuilder().
			Container(serverContainer).
			PodLabel("role", "az-test").
			Name("api-server-connectivity-daemonset").
			Build()

		_, err = f.K8sResourceManagers.DaemonSetManager().CreateAndWaitTillDaemonSetIsReady(testDaemonSet, utils.DefaultDeploymentReadyTimeout)
		Expect(err).ToNot(HaveOccurred())

		By(fmt.Sprintf("getting the node with the node label key %s and value %s",
			f.Options.NgNameLabelKey, f.Options.NgNameLabelVal))

		nodes, err := f.K8sResourceManagers.NodeManager().GetNodes(f.Options.NgNameLabelKey, f.Options.NgNameLabelVal)
		Expect(err).ToNot(HaveOccurred())

		azToPod = GetAZtoPod(nodes)
	})

	JustAfterEach(func() {
		By("Deleting the Daemonset.")
		err = f.K8sResourceManagers.DaemonSetManager().DeleteAndWaitTillDaemonSetDeleted(testDaemonSet, utils.DefaultDeploymentReadyTimeout)
		Expect(err).ToNot(HaveOccurred())

	})

	Context("While testing API Server Connectivity", func() {
		It("Should connect to the API Server", func() {
			// Standard paths for SA token, CA cert and API Server URL
			token_path := "/var/run/secrets/kubernetes.io/serviceaccount/token"
			cacert := "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
			api_server_url := "https://kubernetes.default.svc/api"

			for az := range azToPod {
				fmt.Printf("Testing API Server Connectivity from AZ %s \n", az)
				sa_token := []string{"cat", token_path}
				token_value, _, err := RunCommandOnPod(azToPod[az], sa_token)
				Expect(err).ToNot(HaveOccurred(), fmt.Sprintf("failed to get SA token for pod in %s", az))
				bearer := fmt.Sprintf("Authorization: Bearer %s", token_value)
				test_api_server_connectivity := []string{"curl", "--cacert", cacert, "--header", bearer, "-X", "GET",
					api_server_url,
				}

				api_server_stdout, _, err := RunCommandOnPod(azToPod[az], test_api_server_connectivity)
				// Descriptive error message on failure to connect to API Server from particular AZ.
				Expect(err).ToNot(HaveOccurred(), fmt.Sprintf("Error while connecting to API Server from %s", az))
				Expect(api_server_stdout).ToNot(BeEmpty())
				Expect(api_server_stdout).To(ContainSubstring("APIVersions"))
			}
		})
	})
})

func CheckConnectivityBetweenPods(azToPod map[string]coreV1.Pod, port int, testerExpectedStdOut string, testerExpectedStdErr string, getTestCommandFunc func(serverPod coreV1.Pod, port int) []string) {

	By("checking connection on same node, primary to primary")

	for az1 := range azToPod {
		for az2 := range azToPod {
			if az1 != az2 {
				fmt.Printf("Testing Connectivity from Pod IP1 %s (%s) to Pod IP2 %s (%s) \n",
					azToPod[az1].Status.PodIP, az1, azToPod[az2].Status.PodIP, az2)
				testConnectivity(azToPod[az1], azToPod[az2], testerExpectedStdOut, testerExpectedStdErr, port, getTestCommandFunc)
			}
		}
	}
}

func RunCommandOnPod(receiverPod coreV1.Pod, command []string) (string, string, error) {
	stdout, stderr, err := f.K8sResourceManagers.PodManager().
		PodExec(receiverPod.Namespace, receiverPod.Name, command)
	return stdout, stderr, err
}
