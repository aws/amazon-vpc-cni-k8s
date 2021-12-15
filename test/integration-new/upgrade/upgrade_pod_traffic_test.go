// Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//     http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package upgrade

import (
	"fmt"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	"github.com/aws/aws-sdk-go/service/eks"
	"strconv"

	"github.com/aws/aws-sdk-go/service/ec2"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/apps/v1"
	coreV1 "k8s.io/api/core/v1"
)

var (
	err error
	// The command to run on server pods, to allow incoming
	// connections for different traffic type
	serverListenCmd []string
	// Arguments to the server listen command
	serverListenCmdArgs []string
	// The function that generates command which will be sent from
	// tester pod to receiver pod
	testConnectionCommandFunc func(serverPod coreV1.Pod, port int) []string
	// The functions reinforces that the positive test is working as
	// expected by creating a negative test command that should fail
	testFailedConnectionCommandFunc func(serverPod coreV1.Pod, port int) []string
	// Expected stdout from the exec command on testing connection
	// from tester to server
	testerExpectedStdOut string
	// Expected stderr from the exec command on testing connection
	// from tester to server
	testerExpectedStdErr string
	// The port on which server is listening for new connections
	serverPort int
	// Protocol for establishing connection to server
	protocol string

	// Primary node server deployment
	primaryNodeDeployment *v1.Deployment
	// Secondary node Server deployment
	secondaryNodeDeployment *v1.Deployment

	// Map of Pods placed on primary/secondary ENI IP on primary node
	interfaceToPodListOnPrimaryNode InterfaceTypeToPodList
	// Map of Pods placed on primary/secondary ENI IP on secondary node
	interfaceToPodListOnSecondaryNode InterfaceTypeToPodList
)

var _ = Describe("test applying  older version", func() {

	var (
		describeAddonOutput *eks.DescribeAddonOutput
		err                 error
	)

	BeforeEach(func() {
		By("getting the current addon")
		describeAddonOutput, err = f.CloudServices.EKS().DescribeAddon("vpc-cni", f.Options.ClusterName)
		if err == nil {

			By("deleting the current vpc cni addon ")
			_, err = f.CloudServices.EKS().DeleteAddon("vpc-cni", f.Options.ClusterName)
			Expect(err).ToNot(HaveOccurred())

		}
	})

	It("should successfully run on initial addon version", func() {
		By("apply initial addon version")
		_, err = f.CloudServices.EKS().CreateAddonWithVersion("vpc-cni", f.Options.ClusterName, initialCNIVersion)
		Expect(err).ToNot(HaveOccurred())

		var status string = ""

		By("getting the initial addon...")
		for status != "ACTIVE" {
			describeAddonOutput, err = f.CloudServices.EKS().DescribeAddon("vpc-cni", f.Options.ClusterName)
			Expect(err).ToNot(HaveOccurred())
			status = *describeAddonOutput.Addon.Status
		}
		//Set the WARM_ENI_TARGET to 0 to prevent all pods being scheduled on secondary ENI
		k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, "aws-node", "kube-system",
			"aws-node", map[string]string{"WARM_IP_TARGET": "3", "WARM_ENI_TARGET": "0"})

		testPodNetworking()

	})

	It("should successfully run on final addon version", func() {
		By("apply final addon version")
		_, err = f.CloudServices.EKS().CreateAddonWithVersion("vpc-cni", f.Options.ClusterName, finalCNIVersion)
		Expect(err).ToNot(HaveOccurred())

		var status string = ""

		By("getting the final addon...")
		for status != "ACTIVE" {
			describeAddonOutput, err = f.CloudServices.EKS().DescribeAddon("vpc-cni", f.Options.ClusterName)
			Expect(err).ToNot(HaveOccurred())
			status = *describeAddonOutput.Addon.Status
		}
		//Set the WARM_ENI_TARGET to 0 to prevent all pods being scheduled on secondary ENI
		k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, "aws-node", "kube-system",
			"aws-node", map[string]string{"WARM_IP_TARGET": "3", "WARM_ENI_TARGET": "0"})

		testPodNetworking()

	})

})

type TestStack struct {
	protocol            string
	serverPort          int
	serverListenCmd     []string
	serverListenCmdArgs []string
}

func (s TestStack) Deploy() {
	DeployStack(s.protocol, s.serverPort, s.serverListenCmd, s.serverListenCmdArgs)
}

func (s TestStack) ConnectivityPerStack() {

	protocolVal := s.protocol
	switch {
	case protocolVal == "icmp":
		ConnectivityPerStack(s.serverPort, fmt.Sprintf("%d packets transmitted, "+
			"%d packets received", 5, 5), "", func(receiverPod coreV1.Pod, port int) []string {
			return []string{"ping", "-c", strconv.Itoa(5), receiverPod.Status.PodIP}
		})
	case protocolVal == "tcp":
		ConnectivityPerStack(s.serverPort, "", "succeeded!", func(receiverPod coreV1.Pod, port int) []string {
			return []string{"nc", "-v", "-w2", receiverPod.Status.PodIP, strconv.Itoa(port)}
		})
	case protocolVal == "udp":
		ConnectivityPerStack(s.serverPort, "", "succeeded!", func(receiverPod coreV1.Pod, port int) []string {
			return []string{"nc", "-u", "-v", "-w2", receiverPod.Status.PodIP, strconv.Itoa(port)}
		})
	default:
		fmt.Println("Invalid")
	}

}

func (s TestStack) Cleanup() {
	CleanupStack(s.protocol, s.serverPort)
}

func testPodNetworking() {
	icmpStack :=
		TestStack{"icmp", 0, []string{"sleep"}, []string{"1000"}}
	tcpStack :=
		TestStack{ec2.ProtocolTcp, 2273, []string{"nc"}, []string{"-k", "-l", strconv.Itoa(2273)}}
	udpStack :=
		TestStack{ec2.ProtocolUdp, 2273, []string{"nc"}, []string{"-u", "-l", "-k", strconv.Itoa(2273)}}

	testStacks := []TestStack{icmpStack, tcpStack, udpStack}

	for _, testStackVal := range testStacks {
		testStackVal.Deploy()
		testStackVal.ConnectivityPerStack()
		testStackVal.Cleanup()
	}

}

func DeployStack(protocol string, serverPort int, serverListenCmd []string, serverListenCmdArgs []string) {
	By("authorizing security group ingress on instance security group")
	err = f.CloudServices.EC2().
		AuthorizeSecurityGroupIngress(instanceSecurityGroupID, protocol, serverPort, serverPort, "0.0.0.0/0")
	Expect(err).ToNot(HaveOccurred())

	By("authorizing security group egress on instance security group")
	err = f.CloudServices.EC2().
		AuthorizeSecurityGroupEgress(instanceSecurityGroupID, protocol, serverPort, serverPort, "0.0.0.0/0")
	Expect(err).ToNot(HaveOccurred())

	serverContainer := manifest.
		NewNetCatAlpineContainer().
		Command(serverListenCmd).
		Args(serverListenCmdArgs).
		Build()

	By("creating server deployment on the primary node")
	primaryNodeDeployment = manifest.
		NewDefaultDeploymentBuilder().
		Container(serverContainer).
		Replicas(maxIPPerInterface*2). // X2 so Pods are created on secondary ENI too
		NodeName(primaryNode.Name).
		PodLabel("node", "primary").
		Name("primary-node-server").
		Build()

	primaryNodeDeployment, err = f.K8sResourceManagers.
		DeploymentManager().
		CreateAndWaitTillDeploymentIsReady(primaryNodeDeployment, utils.DefaultDeploymentReadyTimeout)
	Expect(err).ToNot(HaveOccurred())

	interfaceToPodListOnPrimaryNode =
		GetPodsOnPrimaryAndSecondaryInterface(primaryNode, "node", "primary")

	// At least two Pods should be placed on the Primary and Secondary Interface
	// on the Primary and Secondary Node in order to test all possible scenarios
	Expect(len(interfaceToPodListOnPrimaryNode.PodsOnPrimaryENI)).
		Should(BeNumerically(">", 1))
	Expect(len(interfaceToPodListOnPrimaryNode.PodsOnSecondaryENI)).
		Should(BeNumerically(">", 1))

	By("creating server deployment on secondary node")
	secondaryNodeDeployment = manifest.
		NewDefaultDeploymentBuilder().
		Container(serverContainer).
		Replicas(maxIPPerInterface*2). // X2 so Pods are created on secondary ENI too
		NodeName(secondaryNode.Name).
		PodLabel("node", "secondary").
		Name("secondary-node-server").
		Build()

	secondaryNodeDeployment, err = f.K8sResourceManagers.
		DeploymentManager().
		CreateAndWaitTillDeploymentIsReady(secondaryNodeDeployment, utils.DefaultDeploymentReadyTimeout)
	Expect(err).ToNot(HaveOccurred())

	interfaceToPodListOnSecondaryNode =
		GetPodsOnPrimaryAndSecondaryInterface(secondaryNode, "node", "secondary")

	// Same reason as mentioned above
	Expect(len(interfaceToPodListOnSecondaryNode.PodsOnPrimaryENI)).
		Should(BeNumerically(">", 1))
	Expect(len(interfaceToPodListOnSecondaryNode.PodsOnSecondaryENI)).
		Should(BeNumerically(">", 1))
}

func CleanupStack(protocol string, serverPort int) {
	By("revoking security group ingress on instance security group")
	err = f.CloudServices.EC2().
		RevokeSecurityGroupIngress(instanceSecurityGroupID, protocol, serverPort, serverPort, "0.0.0.0/0")
	Expect(err).ToNot(HaveOccurred())

	By("revoking security group egress on instance security group")
	err = f.CloudServices.EC2().
		RevokeSecurityGroupEgress(instanceSecurityGroupID, protocol, serverPort, serverPort, "0.0.0.0/0")
	Expect(err).ToNot(HaveOccurred())

	By("deleting the primary node server deployment")
	err = f.K8sResourceManagers.DeploymentManager().
		DeleteAndWaitTillDeploymentIsDeleted(primaryNodeDeployment)
	Expect(err).ToNot(HaveOccurred())

	By("deleting the secondary node server deployment")
	err = f.K8sResourceManagers.DeploymentManager().
		DeleteAndWaitTillDeploymentIsDeleted(secondaryNodeDeployment)
	Expect(err).ToNot(HaveOccurred())
}

// CheckConnectivityForMultiplePodPlacement checks connectivity for various scenarios, an example
// connection from Pod on Node 1 having IP from Primary Network Interface to Pod on Node 2 having
// IP from Secondary Network Interface
func ConnectivityPerStack(port int,
	testerExpectedStdOut string, testerExpectedStdErr string,
	getTestCommandFunc func(receiverPod coreV1.Pod, port int) []string) {
	CheckConnectivityForMultiplePodPlacement(port, testerExpectedStdOut, testerExpectedStdErr, getTestCommandFunc)
}
func CheckConnectivityForMultiplePodPlacement(port int,
	testerExpectedStdOut string, testerExpectedStdErr string,
	getTestCommandFunc func(receiverPod coreV1.Pod, port int) []string) {

	By("checking connection on same node, primary to primary")
	testConnectivity(
		interfaceToPodListOnPrimaryNode.PodsOnPrimaryENI[0],
		interfaceToPodListOnPrimaryNode.PodsOnPrimaryENI[1],
		testerExpectedStdOut, testerExpectedStdErr, port, getTestCommandFunc)

	By("checking connection on same node, primary to secondary")
	testConnectivity(
		interfaceToPodListOnPrimaryNode.PodsOnPrimaryENI[0],
		interfaceToPodListOnPrimaryNode.PodsOnSecondaryENI[0],
		testerExpectedStdOut, testerExpectedStdErr, port, getTestCommandFunc)

	By("checking connection on same node, secondary to secondary")
	testConnectivity(
		interfaceToPodListOnPrimaryNode.PodsOnSecondaryENI[0],
		interfaceToPodListOnPrimaryNode.PodsOnSecondaryENI[1],
		testerExpectedStdOut, testerExpectedStdErr, port, getTestCommandFunc)

	By("checking connection on different node, primary to primary")
	testConnectivity(
		interfaceToPodListOnPrimaryNode.PodsOnPrimaryENI[0],
		interfaceToPodListOnSecondaryNode.PodsOnPrimaryENI[0],
		testerExpectedStdOut, testerExpectedStdErr, port, getTestCommandFunc)

	By("checking connection on different node, primary to secondary")
	testConnectivity(
		interfaceToPodListOnPrimaryNode.PodsOnPrimaryENI[0],
		interfaceToPodListOnSecondaryNode.PodsOnSecondaryENI[0],
		testerExpectedStdOut, testerExpectedStdErr, port, getTestCommandFunc)

	By("checking connection on different node, secondary to secondary")
	testConnectivity(
		interfaceToPodListOnPrimaryNode.PodsOnSecondaryENI[0],
		interfaceToPodListOnSecondaryNode.PodsOnSecondaryENI[0],
		testerExpectedStdOut, testerExpectedStdErr, port, getTestCommandFunc)
}

// testConnectivity verifies connectivity between tester and server
func testConnectivity(senderPod coreV1.Pod, receiverPod coreV1.Pod, expectedStdout string,
	expectedStderr string, port int, getTestCommandFunc func(receiverPod coreV1.Pod, port int) []string) {

	testerCommand := getTestCommandFunc(receiverPod, port)

	fmt.Fprintf(GinkgoWriter, "verifying connectivity from pod %s on node %s with IP %s to pod"+
		" %s on node %s with IP %s\n", senderPod.Name, senderPod.Spec.NodeName, senderPod.Status.PodIP,
		receiverPod.Name, receiverPod.Spec.NodeName, receiverPod.Status.PodIP)

	stdOut, stdErr, err := f.K8sResourceManagers.PodManager().
		PodExec(senderPod.Namespace, senderPod.Name, testerCommand)
	Expect(err).ToNot(HaveOccurred())

	fmt.Fprintf(GinkgoWriter, "stdout: %s and stderr: %s\n", stdOut, stdErr)

	Expect(stdErr).To(ContainSubstring(expectedStderr))
	Expect(stdOut).To(ContainSubstring(expectedStdout))
}

type InterfaceTypeToPodList struct {
	PodsOnPrimaryENI   []coreV1.Pod
	PodsOnSecondaryENI []coreV1.Pod
}

// GetPodsOnPrimaryAndSecondaryInterface returns the list of Pods on Primary Networking
// Interface and Secondary Network Interface on a given Node
func GetPodsOnPrimaryAndSecondaryInterface(node coreV1.Node,
	podLabelKey string, podLabelVal string) InterfaceTypeToPodList {
	podList, err := f.K8sResourceManagers.
		PodManager().
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
