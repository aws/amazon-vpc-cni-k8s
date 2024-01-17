package cni

import (
	"fmt"
	"strconv"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudwatch"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/integration/common"
	"github.com/aws/aws-sdk-go/service/ec2"
	coreV1 "k8s.io/api/core/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/apps/v1"
)

var (
	retries = 3
)

const MetricNamespace = "NetworkingAZConnectivity"

// Tests pod networking across AZs. It similar to pod connectivity test, but launches a daemonset, so that
// there is a pod on each node across AZs. It then tests connectivity between pods on different nodes across AZs.
var _ = Describe("[STATIC_CANARY] test pod networking", FlakeAttempts(retries), func() {

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

		// Map of AZ name, string to AZ ID for the account.
		azToazID map[string]string
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

		azToTestPod, azToazID = GetAZMappings(nodes)
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
		err = f.K8sResourceManagers.DaemonSetManager().DeleteAndWaitTillDaemonSetIsDeleted(testDaemonSet, utils.DefaultDeploymentReadyTimeout)
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
			CheckConnectivityBetweenPods(azToTestPod, azToazID, serverPort, testerExpectedStdOut, testerExpectedStdErr, testConnectionCommandFunc)
		})
	})
})

// Functio to Az to Pod mapping and Az to AZ ID mapping
func GetAZMappings(nodes coreV1.NodeList) (map[string]coreV1.Pod, map[string]string) {
	// Map of AZ name to Pod from Daemonset running on nodes
	azToPod := make(map[string]coreV1.Pod)
	// Map of AZ name to AZ ID
	azToazID := make(map[string]string)

	describeAZOutput, err := f.CloudServices.EC2().DescribeAvailabilityZones()

	if err != nil {
		// Don't fail the test if we can't describe AZs. The failure will be caught by the test
		// We use describe AZs to get the AZ ID for metrics.
		fmt.Println("Error while describing AZs", err)
	}

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

		azToazID[azName] = *describeAZOutput.AvailabilityZones[i].ZoneId
	}
	return azToPod, azToazID
}

var _ = Describe("[STATIC_CANARY] API Server Connectivity from AZs", FlakeAttempts(retries), func() {

	var (
		err           error
		testDaemonSet *v1.DaemonSet

		// Map of AZ name to Pod of testDaemonSet running on nodes
		azToPod  map[string]coreV1.Pod
		azToazID map[string]string
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

		azToPod, azToazID = GetAZMappings(nodes)

	})

	JustAfterEach(func() {
		By("Deleting the Daemonset.")
		err = f.K8sResourceManagers.DaemonSetManager().DeleteAndWaitTillDaemonSetIsDeleted(testDaemonSet, utils.DefaultDeploymentReadyTimeout)
		Expect(err).ToNot(HaveOccurred())

	})

	Context("While testing API Server Connectivity", func() {

		It("Should connect to the API Server", func() {
			describeClusterOutput, err := f.CloudServices.EKS().DescribeCluster(f.Options.ClusterName)
			Expect(err).ToNot(HaveOccurred(), fmt.Sprintf("Error while Describing the cluster to find APIServer NLB endpoint. %s", f.Options.ClusterName))
			APIServerNLBEndpoint := fmt.Sprintf("%s/api", *describeClusterOutput.Cluster.Endpoint)
			APIServerInternalEndpoint := "https://kubernetes.default.svc/api"

			CheckAPIServerConnectivityFromPods(azToPod, azToazID, APIServerInternalEndpoint)

			CheckAPIServerConnectivityFromPods(azToPod, azToazID, APIServerNLBEndpoint)
		})

	})
})

func CheckAPIServerConnectivityFromPods(azToPod map[string]coreV1.Pod, azToazId map[string]string, api_server_url string) {
	// Standard paths for SA token, CA cert and API Server URL
	token_path := "/var/run/secrets/kubernetes.io/serviceaccount/token"
	cacert := "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
	const MetricName = "APIServerConnectivity"

	for az := range azToPod {
		fmt.Printf("Testing API Server %s Connectivity from AZ %s  AZID %s \n", api_server_url, az, azToazId[az])
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
		fmt.Printf("API Server %s Connectivity from AZ %s was successful.\n", api_server_url, az)

		if f.Options.PublicCWMetrics {
			putmetricData := cloudwatch.PutMetricDataInput{
				Namespace: aws.String(MetricNamespace),
				MetricData: []*cloudwatch.MetricDatum{
					{
						MetricName: aws.String(MetricName),
						Unit:       aws.String("Count"),
						Value:      aws.Float64(1),
						Dimensions: []*cloudwatch.Dimension{
							{
								Name:  aws.String("AZID"),
								Value: aws.String(azToazId[az]),
							},
						},
					},
				},
			}

			_, err = f.CloudServices.CloudWatch().PutMetricData(&putmetricData)
			Expect(err).ToNot(HaveOccurred(), fmt.Sprintf("Error while putting metric data for API Server Connectivity from %s", az))
		}
	}
}

func CheckConnectivityBetweenPods(azToPod map[string]coreV1.Pod, azToazId map[string]string, port int, testerExpectedStdOut string, testerExpectedStdErr string, getTestCommandFunc func(serverPod coreV1.Pod, port int) []string) {
	const MetricName = "InterAZConnectivity"

	By("checking connection on same node, primary to primary")

	for az1 := range azToPod {
		for az2 := range azToPod {
			if az1 != az2 {
				fmt.Printf("Testing Connectivity from Pod IP1 %s (%s, %s) to Pod IP2 %s (%s, %s) \n",
					azToPod[az1].Status.PodIP, az1, azToazId[az1], azToPod[az2].Status.PodIP, az2, azToazId[az2])
				testConnectivity(azToPod[az1], azToPod[az2], testerExpectedStdOut, testerExpectedStdErr, port, getTestCommandFunc)

				if f.Options.PublicCWMetrics {
					putmetricData := cloudwatch.PutMetricDataInput{
						Namespace: aws.String(MetricNamespace),
						MetricData: []*cloudwatch.MetricDatum{
							{
								MetricName: aws.String(MetricName),
								Unit:       aws.String("Count"),
								Value:      aws.Float64(1),
								Dimensions: []*cloudwatch.Dimension{
									{
										Name:  aws.String("AZID"),
										Value: aws.String(azToazId[az1]),
									},
								},
							},
						},
					}
					_, err := f.CloudServices.CloudWatch().PutMetricData(&putmetricData)
					Expect(err).ToNot(HaveOccurred(), fmt.Sprintf("Error while putting metric data for API Server Connectivity from %s", azToazId[az1]))
				}
			}
		}
	}
}

func RunCommandOnPod(receiverPod coreV1.Pod, command []string) (string, string, error) {
	count := retries
	for {
		stdout, stdrr, err := f.K8sResourceManagers.PodManager().
			PodExec(receiverPod.Namespace, receiverPod.Name, command)
		count -= 1
		if count == 0 || err == nil {
			return stdout, stdrr, err
		}
	}
}
