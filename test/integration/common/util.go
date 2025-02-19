package common

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"

	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/aws/amazon-vpc-cni-k8s/test/agent/pkg/input"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	coreV1 "k8s.io/api/core/v1"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/agent"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
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
