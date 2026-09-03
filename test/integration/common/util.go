package common

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"

	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/aws/amazon-vpc-cni-k8s/test/agent/pkg/input"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsV1 "k8s.io/api/apps/v1"
	coreV1 "k8s.io/api/core/v1"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/agent"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
)

type TestType int

var (
	// The Pod labels for client and server in order to retrieve the
	// client and server Pods belonging to a Deployment/Jobs
	labelKey          = "app"
	serverPodLabelVal = "server-pod"
	clientPodLabelVal = "client-pod"
)

const (
	NetworkingTearDownSucceeds TestType = iota
	NetworkingTearDownFails
	NetworkingSetupSucceeds
	NetworkingSetupFails
)

type InterfaceTypeToPodList struct {
	PodsOnPrimaryENI   []coreV1.Pod
	PodsOnSecondaryENI []coreV1.Pod
}

func GetPodNetworkingValidationInput(interfaceTypeToPodList InterfaceTypeToPodList, vpcCIDRs []string) input.PodNetworkingValidationInput {
	ip := input.PodNetworkingValidationInput{
		VPCCidrRange: vpcCIDRs,
		VethPrefix:   "eni",
		PodList:      []input.Pod{},
		ValidateMTU:  false,
	}

	for _, primaryENIPod := range interfaceTypeToPodList.PodsOnPrimaryENI {
		ip.PodList = append(ip.PodList, input.Pod{
			PodName:              primaryENIPod.Name,
			PodNamespace:         primaryENIPod.Namespace,
			PodIPv4Address:       primaryENIPod.Status.PodIP,
			IsIPFromSecondaryENI: false,
		})
	}

	for _, secondaryENIPod := range interfaceTypeToPodList.PodsOnSecondaryENI {
		ip.PodList = append(ip.PodList, input.Pod{
			PodName:              secondaryENIPod.Name,
			PodNamespace:         secondaryENIPod.Namespace,
			PodIPv4Address:       secondaryENIPod.Status.PodIP,
			IsIPFromSecondaryENI: true,
		})
	}
	return ip
}

// Validate host networking for the list of pods supplied
func ValidateHostNetworking(testType TestType, podValidationInputString string, nodeName string, f *framework.Framework) {
	testerArgs := []string{fmt.Sprintf("-pod-networking-validation-input=%s",
		podValidationInputString)}

	var shouldTestPodError bool
	if NetworkingSetupSucceeds == testType {
		testerArgs = append(testerArgs, "-test-setup=true")
	} else if NetworkingSetupFails == testType {
		testerArgs = append(testerArgs, "-test-setup=true")
		shouldTestPodError = true
	} else if NetworkingTearDownSucceeds == testType {
		testerArgs = append(testerArgs, "-test-cleanup=true")
	} else if NetworkingTearDownFails == testType {
		testerArgs = append(testerArgs, "-test-cleanup=true")
		shouldTestPodError = true
	}

	testContainer := manifest.NewTestHelperContainer(f.Options.TestImageRegistry).
		Command([]string{"./networking"}).
		Args(testerArgs).
		Build()

	testPod := manifest.NewDefaultPodBuilder().
		Container(testContainer).
		NodeName(nodeName).
		HostNetwork(true).
		Build()

	By("creating pod to test host networking setup")
	testPod, err := f.K8sResourceManagers.PodManager().
		CreateAndWaitTillPodCompleted(testPod)
	logs, errLogs := f.K8sResourceManagers.PodManager().
		PodLogs(testPod.Namespace, testPod.Name)
	Expect(errLogs).ToNot(HaveOccurred())

	_, _ = fmt.Fprintln(GinkgoWriter, logs)

	if shouldTestPodError {
		Expect(err).To(HaveOccurred())
	} else {
		Expect(err).ToNot(HaveOccurred())
	}

	By("deleting the host networking setup pod")
	err = f.K8sResourceManagers.PodManager().
		DeleteAndWaitTillPodDeleted(testPod)
	Expect(err).ToNot(HaveOccurred())
}

// GetPodsOnPrimaryAndSecondaryInterface returns the list of Pods on Primary Networking
// Interface and Secondary Network Interface on a given Node
func GetPodsOnPrimaryAndSecondaryInterface(node coreV1.Node,
	podLabelKey string, podLabelVal string, f *framework.Framework) InterfaceTypeToPodList {
	podList, err := f.K8sResourceManagers.PodManager().
		GetPodsWithLabelSelector(podLabelKey, podLabelVal)
	Expect(err).ToNot(HaveOccurred())

	instance, err := f.CloudServices.EC2().
		DescribeInstance(context.TODO(), k8sUtils.GetInstanceIDFromNode(node))
	Expect(err).ToNot(HaveOccurred())

	interfaceToPodList := InterfaceTypeToPodList{
		PodsOnPrimaryENI:   []coreV1.Pod{},
		PodsOnSecondaryENI: []coreV1.Pod{},
	}

	ipToPod := map[string]coreV1.Pod{}
	for _, pod := range podList.Items {
		ipToPod[pod.Status.PodIP] = pod
	}

	for _, nwInterface := range instance.NetworkInterfaces {
		isPrimary := IsPrimaryENI(nwInterface, instance.PrivateIpAddress)
		for _, ip := range nwInterface.PrivateIpAddresses {
			if pod, found := ipToPod[*ip.PrivateIpAddress]; found {
				if isPrimary {
					interfaceToPodList.PodsOnPrimaryENI =
						append(interfaceToPodList.PodsOnPrimaryENI, pod)
				} else {
					interfaceToPodList.PodsOnSecondaryENI =
						append(interfaceToPodList.PodsOnSecondaryENI, pod)
				}
			}
		}
	}
	return interfaceToPodList
}

// SpanningENIsReplicaCount returns the number of pods needed so that placement
// must occupy the primary ENI and at least one secondary ENI. ipamd assigns pod
// IPs by ranging over a Go map of ENIs, so placement order is not deterministic;
// requesting two more pods than every secondary ENI can hold forces at least two
// onto the primary ENI regardless of iteration order. It returns an error when
// the instance limits are missing or cannot support that guarantee.
func SpanningENIsReplicaCount(netInfo ec2types.NetworkInfo) (int, error) {
	if netInfo.MaximumNetworkInterfaces == nil {
		return 0, fmt.Errorf("instance type network info is missing maximum network interfaces")
	}
	if netInfo.Ipv4AddressesPerInterface == nil {
		return 0, fmt.Errorf("instance type network info is missing IPv4 addresses per interface")
	}

	maxENIs := int(*netInfo.MaximumNetworkInterfaces)
	ipsPerENI := int(*netInfo.Ipv4AddressesPerInterface)
	if maxENIs < 2 {
		return 0, fmt.Errorf("instance type supports %d ENI(s): at least 2 are required", maxENIs)
	}
	if ipsPerENI < 3 {
		return 0, fmt.Errorf("instance type supports %d IPv4 address(es) per ENI: at least 3 are required to place two pods on the primary ENI", ipsPerENI)
	}
	return (maxENIs-1)*(ipsPerENI-1) + 2, nil
}

// NetworkInfoForNode returns the ENI/IP limits for the node's own instance type,
// read from its instance-type label. Resolving per node keeps the replica count
// and preconditions correct on a heterogeneous node group.
func NetworkInfoForNode(f *framework.Framework, node coreV1.Node) ec2types.NetworkInfo {
	instanceType := node.Labels["node.kubernetes.io/instance-type"]
	if instanceType == "" {
		instanceType = node.Labels["beta.kubernetes.io/instance-type"]
	}
	Expect(instanceType).ToNot(BeEmpty(), "node %s has no instance-type label", node.Name)

	instanceTypeInfo, err := f.CloudServices.EC2().DescribeInstanceType(context.TODO(), instanceType)
	Expect(err).ToNot(HaveOccurred())
	Expect(instanceTypeInfo).ToNot(BeEmpty())
	Expect(instanceTypeInfo[0].NetworkInfo).ToNot(BeNil(),
		"instance type %s has no network information", instanceType)
	return *instanceTypeInfo[0].NetworkInfo
}

// CreateDeploymentSpanningENIs creates a deployment on the node sized so its pods
// occupy the primary ENI and at least one secondary ENI, waits for readiness, and
// returns the pods bucketed by ENI. Requires secondary-IP mode, no trunk ENI, and
// no custom networking; each precondition is asserted. On an empty bucket it
// prints the node ENI/IP layout.
func CreateDeploymentSpanningENIs(f *framework.Framework, node coreV1.Node,
	name, podLabelKey, podLabelVal string,
	container coreV1.Container) (InterfaceTypeToPodList, *appsV1.Deployment) {

	replicas := AssertSpanningENIsPreconditions(f, node)

	deployment := manifest.NewDefaultDeploymentBuilder().
		Name(name).
		Container(container).
		Replicas(replicas).
		NodeName(node.Name).
		PodLabel(podLabelKey, podLabelVal).
		Build()

	deployment, err := f.K8sResourceManagers.DeploymentManager().
		CreateAndWaitTillDeploymentIsReady(deployment, utils.DefaultDeploymentReadyTimeout)
	if err != nil {
		// Print pod phases so a readiness timeout is diagnosable.
		pods, listErr := f.K8sResourceManagers.PodManager().
			GetPodsWithLabelSelector(podLabelKey, podLabelVal)
		if listErr == nil {
			phases := map[string]int{}
			for _, pod := range pods.Items {
				phases[string(pod.Status.Phase)]++
			}
			fmt.Fprintf(GinkgoWriter, "spanning deployment not ready after %v: %d replicas requested, pod phases: %v\n",
				utils.DefaultDeploymentReadyTimeout, replicas, phases)
		}
	}
	Expect(err).ToNot(HaveOccurred())

	interfaceToPodList := GetPodsOnPrimaryAndSecondaryInterface(node, podLabelKey, podLabelVal, f)
	if len(interfaceToPodList.PodsOnPrimaryENI) < 2 || len(interfaceToPodList.PodsOnSecondaryENI) < 2 {
		DumpENIPlacement(f, node, interfaceToPodList, replicas)
	}
	return interfaceToPodList, deployment
}

// AssertSpanningENIsPreconditions fails the spec if the node configuration would
// break the pigeonhole guarantee that a SpanningENIsReplicaCount-sized deployment
// occupies both the primary and a secondary ENI, and returns the validated replica
// count for callers that build the deployment directly.
func AssertSpanningENIsPreconditions(f *framework.Framework, node coreV1.Node) int {
	netInfo := NetworkInfoForNode(f, node)
	replicas, err := SpanningENIsReplicaCount(netInfo)
	Expect(err).ToNot(HaveOccurred(), "instance type cannot support a spanning ENI deployment")
	awsNodeEnv := GetAWSNodeEnv(f)

	Expect(parseBoolEnv(awsNodeEnv["ENABLE_PREFIX_DELEGATION"])).To(BeFalse(),
		"prefix delegation is enabled: per-ENI capacity is prefix-based, so the "+
			"secondary-IP replica count under-provisions and pods span every ENI by chance, not by pigeonhole")
	Expect(parseBoolEnv(awsNodeEnv["AWS_VPC_K8S_CNI_CUSTOM_NETWORK_CFG"])).To(BeFalse(),
		"custom networking is enabled: the primary ENI is excluded from pod IPs, so the primary bucket is always empty")

	// A trunk ENI (security groups for pods) consumes an ENI slot without hosting
	// pod IPs, so real data-ENI capacity drops below the pigeonhole count and pods
	// stay Pending until the readiness wait times out.
	_, hasTrunk := node.Labels["vpc.amazonaws.com/has-trunk-attached"]
	Expect(hasTrunk).To(BeFalse(),
		"security-groups-for-pods is enabled on this node: the trunk ENI consumes an ENI slot without hosting pod IPs, so %d replicas exceed data-ENI capacity and pods stay Pending", replicas)

	return replicas
}

// GetAWSNodeEnv reads the environment variables on the live aws-node container so
// preconditions reflect the current node state, including mid-suite toggles.
func GetAWSNodeEnv(f *framework.Framework) map[string]string {
	ds, err := f.K8sResourceManagers.DaemonSetManager().
		GetDaemonSet(utils.AwsNodeNamespace, utils.AwsNodeName)
	Expect(err).ToNot(HaveOccurred())

	env := map[string]string{}
	found := false
	for _, container := range ds.Spec.Template.Spec.Containers {
		if container.Name != utils.AwsNodeName {
			continue
		}
		found = true
		for _, e := range container.Env {
			env[e.Name] = e.Value
		}
		break
	}
	Expect(found).To(BeTrue(), "daemonset %s/%s has no %q container",
		utils.AwsNodeNamespace, utils.AwsNodeName, utils.AwsNodeName)
	return env
}

// CurrentSGPPTestConfig reads and validates the live aws-node SGPP configuration.
func CurrentSGPPTestConfig(f *framework.Framework) SGPPTestConfig {
	testConfig, err := ResolveSGPPTestConfig(GetAWSNodeEnv(f))
	Expect(err).ToNot(HaveOccurred())
	fmt.Fprintf(GinkgoWriter, "SGPP enforcing mode is %s; expected host veth prefix is %s\n",
		testConfig.EnforcingMode, testConfig.HostVethPrefix)
	return testConfig
}

func parseBoolEnv(val string) bool {
	parsed, err := strconv.ParseBool(val)
	return err == nil && parsed
}

// DumpENIPlacement prints the node's attached ENIs and each pod's IP-to-ENI
// mapping so a placement assertion failure carries the state that explains it.
func DumpENIPlacement(f *framework.Framework, node coreV1.Node,
	interfaceToPodList InterfaceTypeToPodList, replicas int) {

	fmt.Fprintf(GinkgoWriter, "ENI placement dump for node %s (requested %d replicas): "+
		"%d pods on primary ENI, %d pods on secondary ENIs\n",
		node.Name, replicas, len(interfaceToPodList.PodsOnPrimaryENI),
		len(interfaceToPodList.PodsOnSecondaryENI))

	for _, pod := range interfaceToPodList.PodsOnPrimaryENI {
		fmt.Fprintf(GinkgoWriter, "  primary-bucket pod %s -> %s\n", pod.Name, pod.Status.PodIP)
	}
	for _, pod := range interfaceToPodList.PodsOnSecondaryENI {
		fmt.Fprintf(GinkgoWriter, "  secondary-bucket pod %s -> %s\n", pod.Name, pod.Status.PodIP)
	}

	instance, err := f.CloudServices.EC2().
		DescribeInstance(context.TODO(), k8sUtils.GetInstanceIDFromNode(node))
	if err != nil {
		fmt.Fprintf(GinkgoWriter, "could not describe instance for dump: %v\n", err)
		return
	}
	for _, nwInterface := range instance.NetworkInterfaces {
		role := "secondary"
		if IsPrimaryENI(nwInterface, instance.PrivateIpAddress) {
			role = "primary"
		}
		ips := make([]string, 0, len(nwInterface.PrivateIpAddresses))
		for _, ip := range nwInterface.PrivateIpAddresses {
			ips = append(ips, *ip.PrivateIpAddress)
		}
		fmt.Fprintf(GinkgoWriter, "  ENI %s (%s): %v\n", *nwInterface.NetworkInterfaceId, role, ips)
	}
}

func GetTrafficTestConfig(f *framework.Framework, protocol string, serverDeploymentBuilder *manifest.DeploymentBuilder, clientCount int, serverCount int) agent.TrafficTest {
	return agent.TrafficTest{
		Framework:                      f,
		TrafficServerDeploymentBuilder: serverDeploymentBuilder,
		ServerPort:                     2273,
		ServerProtocol:                 protocol,
		ClientCount:                    clientCount,
		ServerCount:                    serverCount,
		ServerPodLabelKey:              labelKey,
		ServerPodLabelVal:              serverPodLabelVal,
		ClientPodLabelKey:              labelKey,
		ClientPodLabelVal:              clientPodLabelVal,
	}
}

func IsPrimaryENI(nwInterface ec2types.InstanceNetworkInterface, instanceIPAddr *string) bool {
	for _, privateIPAddress := range nwInterface.PrivateIpAddresses {
		if *privateIPAddress.PrivateIpAddress == *instanceIPAddr {
			return true
		}
	}
	return false
}

func ApplyCNIManifest(filepath string) {
	var stdoutBuf, stderrBuf bytes.Buffer
	By(fmt.Sprintf("applying manifest: %s", filepath))
	cmd := exec.Command("kubectl", "apply", "-f", filepath)
	cmd.Stdout = io.MultiWriter(os.Stdout, &stdoutBuf)
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)
	err := cmd.Run()
	Expect(err).NotTo(HaveOccurred())
}

func ValidateTraffic(f *framework.Framework, serverDeploymentBuilder *manifest.DeploymentBuilder, succesRate float64, protocol string) {
	trafficTester := GetTrafficTestConfig(f, protocol, serverDeploymentBuilder, 20, 20)
	successRate, err := trafficTester.TestTraffic()
	Expect(err).ToNot(HaveOccurred())
	Expect(successRate).Should(BeNumerically(">=", succesRate))
}
