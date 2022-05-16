package common

import (
	"fmt"

	"github.com/aws/amazon-vpc-cni-k8s/test/agent/pkg/input"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	"github.com/aws/aws-sdk-go/service/ec2"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	coreV1 "k8s.io/api/core/v1"
)

type TestType int

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

	testContainer := manifest.NewTestHelperContainer().
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

	fmt.Fprintln(GinkgoWriter, logs)

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
		DescribeInstance(k8sUtils.GetInstanceIDFromNode(node))
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

func IsPrimaryENI(nwInterface *ec2.InstanceNetworkInterface, instanceIPAddr *string) bool {
	for _, privateIPAddress := range nwInterface.PrivateIpAddresses {
		if *privateIPAddress.PrivateIpAddress == *instanceIPAddr {
			return true
		}
	}
	return false
}
