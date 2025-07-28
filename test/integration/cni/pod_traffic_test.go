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

package cni

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/integration/common"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/apps/v1"
	coreV1 "k8s.io/api/core/v1"
)

var (
	None       string = "none"
	Persistent string = "persistent"
)

// Verifies network connectivity across Pods placed on different combination of
// primary and second Elastic Networking Interface on two nodes. The test verifies
// different traffic type for instance TCP, UDP, ICMP
var _ = Describe("test pod networking", func() {

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
		interfaceToPodListOnPrimaryNode common.InterfaceTypeToPodList
		// Map of Pods placed on primary/secondary ENI IP on secondary node
		interfaceToPodListOnSecondaryNode common.InterfaceTypeToPodList
	)

	JustBeforeEach(func() {
		By("authorizing security group ingress on instance security group")
		err = f.CloudServices.EC2().
			AuthorizeSecurityGroupIngress(context.TODO(), instanceSecurityGroupID, protocol, serverPort, serverPort, "0.0.0.0/0", false)
		Expect(err).ToNot(HaveOccurred())

		By("authorizing security group egress on instance security group")
		err = f.CloudServices.EC2().
			AuthorizeSecurityGroupEgress(context.TODO(), instanceSecurityGroupID, protocol, serverPort, serverPort, "0.0.0.0/0")
		Expect(err).ToNot(HaveOccurred())

		serverContainer := manifest.
			NewNetCatAlpineContainer(f.Options.TestImageRegistry).
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
			common.GetPodsOnPrimaryAndSecondaryInterface(primaryNode, "node", "primary", f)

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
			common.GetPodsOnPrimaryAndSecondaryInterface(secondaryNode, "node", "secondary", f)

		// Same reason as mentioned above
		Expect(len(interfaceToPodListOnSecondaryNode.PodsOnPrimaryENI)).
			Should(BeNumerically(">", 1))
		Expect(len(interfaceToPodListOnSecondaryNode.PodsOnSecondaryENI)).
			Should(BeNumerically(">", 1))
	})

	JustAfterEach(func() {
		By("revoking security group ingress on instance security group")
		err = f.CloudServices.EC2().
			RevokeSecurityGroupIngress(context.TODO(), instanceSecurityGroupID, protocol, serverPort, serverPort, "0.0.0.0/0", false)
		Expect(err).ToNot(HaveOccurred())

		By("revoking security group egress on instance security group")
		err = f.CloudServices.EC2().
			RevokeSecurityGroupEgress(context.TODO(), instanceSecurityGroupID, protocol, serverPort, serverPort, "0.0.0.0/0")
		Expect(err).ToNot(HaveOccurred())

		By("deleting the primary node server deployment")
		err = f.K8sResourceManagers.DeploymentManager().
			DeleteAndWaitTillDeploymentIsDeleted(primaryNodeDeployment)
		Expect(err).ToNot(HaveOccurred())

		By("deleting the secondary node server deployment")
		err = f.K8sResourceManagers.DeploymentManager().
			DeleteAndWaitTillDeploymentIsDeleted(secondaryNodeDeployment)
		Expect(err).ToNot(HaveOccurred())
	})

	Context("when testing ICMP traffic", func() {
		BeforeEach(func() {
			// The number of packets to be sent
			packetCount := 5
			// Protocol needs to be set allow ICMP traffic on the EC2 SG
			protocol = "ICMP"
			// ICMP doesn't need any port to be opened on the SG
			serverPort = 0
			// Since ping doesn't need any server, just sleep on the server pod
			serverListenCmd = []string{"sleep"}
			serverListenCmdArgs = []string{"1000"}

			// Verify all the packets were transmitted and received successfully
			testerExpectedStdOut = fmt.Sprintf("%d packets transmitted, "+
				"%d packets received", packetCount, packetCount)
			testerExpectedStdErr = ""

			testConnectionCommandFunc = func(receiverPod coreV1.Pod, port int) []string {
				return []string{"ping", "-c", strconv.Itoa(packetCount), receiverPod.Status.PodIP}
			}
		})

		It("should allow ICMP traffic", func() {
			CheckConnectivityForMultiplePodPlacement(
				interfaceToPodListOnPrimaryNode, interfaceToPodListOnSecondaryNode,
				serverPort, testerExpectedStdOut, testerExpectedStdErr, testConnectionCommandFunc)
		})
	})

	Context("[CANARY][SMOKE] when establishing UDP connection from tester to server", func() {
		BeforeEach(func() {
			serverPort = 2273
			protocol = "udp"
			serverListenCmd = []string{"nc"}
			// The nc flag "-l" for listen mode, "-k" to keep server up and not close
			// connection after each connection, "-u" for udp
			serverListenCmdArgs = []string{"-u", "-l", "-k", strconv.Itoa(serverPort)}

			// Verbose output from nc is being redirected to stderr instead of stdout
			testerExpectedStdErr = "succeeded!"
			testerExpectedStdOut = ""

			// The nc flag "-u" for UDP traffic, "-v" for verbose output and "-wn" for timing out
			// in n seconds
			testConnectionCommandFunc = func(receiverPod coreV1.Pod, port int) []string {
				return []string{"nc", "-u", "-v", "-w2", receiverPod.Status.PodIP, strconv.Itoa(port)}
			}

			// Create a negative test case with the wrong port number. This is to reinforce the
			// positive test case work by verifying negative cases do throw error
			testFailedConnectionCommandFunc = func(receiverPod coreV1.Pod, port int) []string {
				return []string{"nc", "-u", "-v", "-w2", receiverPod.Status.PodIP, strconv.Itoa(port + 1)}
			}
		})

		It("connection should be established", func() {
			CheckConnectivityForMultiplePodPlacement(
				interfaceToPodListOnPrimaryNode, interfaceToPodListOnSecondaryNode,
				serverPort, testerExpectedStdOut, testerExpectedStdErr, testConnectionCommandFunc)

			By("verifying connection fails for unreachable port")
			VerifyConnectivityFailsForNegativeCase(interfaceToPodListOnPrimaryNode.PodsOnPrimaryENI[0],
				interfaceToPodListOnPrimaryNode.PodsOnPrimaryENI[1], serverPort,
				testFailedConnectionCommandFunc)
		})
	})

	Context("[CANARY][SMOKE] when establishing TCP connection from tester to server", func() {

		BeforeEach(func() {
			serverPort = 2273
			protocol = "tcp"
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

			// Create a negative test case with the wrong port number. This is to reinforce the
			// positive test case work by verifying negative cases do throw error
			testFailedConnectionCommandFunc = func(receiverPod coreV1.Pod, port int) []string {
				return []string{"nc", "-v", "-w2", receiverPod.Status.PodIP, strconv.Itoa(port + 1)}
			}
		})

		It("should allow connection across nodes and across interface types", func() {
			CheckConnectivityForMultiplePodPlacement(
				interfaceToPodListOnPrimaryNode, interfaceToPodListOnSecondaryNode,
				serverPort, testerExpectedStdOut, testerExpectedStdErr, testConnectionCommandFunc)

			By("verifying connection fails for unreachable port")
			VerifyConnectivityFailsForNegativeCase(interfaceToPodListOnPrimaryNode.PodsOnPrimaryENI[0],
				interfaceToPodListOnPrimaryNode.PodsOnPrimaryENI[1], serverPort,
				testFailedConnectionCommandFunc)
		})
	})
})

var _ = Describe("pod egress traffic test", Ordered, func() {
	var originalPolicy string
	var pod *coreV1.Pod
	BeforeAll(func() {
		Skip("Skipping pod egress traffic until addon release")
		Expect(checkNodeShellPlugin()).To(BeNil())
		originalPolicy, err = currentMacAddressPolicy(primaryNode.Name)
		Expect(err).ToNot(HaveOccurred())
	})
	AfterEach(func() {
		f.K8sResourceManagers.PodManager().DeleteAndWaitTillPodDeleted(pod)
	})
	AfterAll(func() {
		err := setMACAddressPolicy(primaryNode.Name, originalPolicy)
		Expect(err).ToNot(HaveOccurred())
	})
	Describe("Traffic test under change of MAC address policy Change", func() {

		Context("When MAC address Policy is None", func() {
			// check if current policy is none, if not make it none
			It("can ping to 8.8.8.8", func() {
				Skip("Skipping pod egress traffic until addon release")
				Expect(setMACAddressPolicy(primaryNode.Name, None)).Error().ShouldNot(HaveOccurred())
				// deploy pod on this node.
				pod = manifest.NewDefaultPodBuilder().
					NodeName(primaryNode.Name).
					Container(manifest.NewBusyBoxContainerBuilder(f.Options.TestImageRegistry).Build()).
					Build()
				pod, err = f.K8sResourceManagers.PodManager().CreateAndWaitTillRunning(pod)
				Expect(err).ToNot(HaveOccurred())
				command := []string{"ping", "-c", "1", "8.8.8.8"}
				stdout, stderr, err := f.K8sResourceManagers.PodManager().PodExec("default", pod.Name, command)
				Expect(err).ToNot(HaveOccurred(), stderr, stdout)
				Expect(stderr).To(BeEmpty(), stdout)
				Expect(stdout).ToNot(BeEmpty())
			})
		})
		Context("When MAC address policy is persistent", func() {
			// check if current policy is none, if not make it none
			It("can ping to 8.8.8.8", func() {
				Skip("Skipping pod egress traffic until addon release")
				Expect(setMACAddressPolicy(primaryNode.Name, Persistent)).Error().ShouldNot(HaveOccurred())
				// deploy pod on this node.
				pod := manifest.NewDefaultPodBuilder().
					NodeName(primaryNode.Name).
					Container(manifest.NewBusyBoxContainerBuilder(f.Options.TestImageRegistry).Build()).
					Build()
				pod, err = f.K8sResourceManagers.PodManager().CreateAndWaitTillRunning(pod)
				Expect(err).ToNot(HaveOccurred())

				command := []string{"ping", "-c", "1", "8.8.8.8"}
				stdout, stderr, err := f.K8sResourceManagers.PodManager().PodExec("default", pod.Name, command)
				Expect(err).ToNot(HaveOccurred(), stderr, stdout)
				Expect(stderr).To(BeEmpty(), stdout)
				Expect(stdout).ToNot(BeEmpty())
			})
		})
	})

})

func checkNodeShellPlugin() error {
	cmd := exec.Command("kubectl", "plugin", "list")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("kubectl node-shell plugin not present")
	}
	if !strings.Contains(string(out), "node_shell") {
		return fmt.Errorf("node-shell not part of supported plugin %s", string(out))
	}
	return nil
}

func execNodeShell(nodeName string, command string) ([]byte, error) {

	cmd := exec.Command("kubectl", "node-shell", nodeName, "--", "bash", "-c", command)
	output, err := cmd.Output()
	return output, err
}

// sets requested policy in drop file and restarts udev
func setMACAddressPolicy(nodeName string, value string) error {

	if val, err := currentMacAddressPolicy(nodeName); err == nil && val == value {
		return nil
	} else if err != nil {
		return fmt.Errorf("error while reading mac address policy: %s", err)
	}

	script := fmt.Sprintf(`set -euo pipefail
dir=/etc/systemd/network/99-default.link.d
mkdir -p "$dir"
# Pick existing drop‑in if any; else fallback to 01-custom-policy.conf
file=$(ls -1 "$dir" 2>/dev/null | head -n1)
[ -z "$file" ] && file=01-custom-policy.conf
file="$dir/$file"
cat > "$file" <<'EOF'
[Link]
MACAddressPolicy=%s
EOF
udevadm control --reload
`, value)

	out, err := execNodeShell(nodeName, script)
	fmt.Println(string(out))

	return err
}

func currentMacAddressPolicy(nodeName string) (string, error) {
	out, err := execNodeShell(nodeName, `systemd-analyze cat-config systemd/network/99-default.link`)
	if err != nil {
		return "", err
	}
	var policy string
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "MACAddressPolicy") {
			policy = strings.SplitAfter(line, "=")[1]
		}
	}
	fmt.Println("extracted current mac address policy", policy)
	return policy, nil
}

func VerifyConnectivityFailsForNegativeCase(senderPod coreV1.Pod, receiverPod coreV1.Pod, port int,
	getTestCommandFunc func(receiverPod coreV1.Pod, port int) []string) {

	testerCommand := getTestCommandFunc(receiverPod, port)

	_, _ = fmt.Fprintf(GinkgoWriter, "verifying connectivity fails from pod %s on node %s with IP %s to pod"+
		" %s on node %s with IP %s\n", senderPod.Name, senderPod.Spec.NodeName, senderPod.Status.PodIP,
		receiverPod.Name, receiverPod.Spec.NodeName, receiverPod.Status.PodIP)

	_, _, err := f.K8sResourceManagers.PodManager().
		PodExec(senderPod.Namespace, senderPod.Name, testerCommand)
	Expect(err).To(HaveOccurred())
}

// CheckConnectivityForMultiplePodPlacement checks connectivity for various scenarios, an example
// connection from Pod on Node 1 having IP from Primary Network Interface to Pod on Node 2 having
// IP from Secondary Network Interface
func CheckConnectivityForMultiplePodPlacement(interfaceToPodListOnPrimaryNode common.InterfaceTypeToPodList,
	interfaceToPodListOnSecondaryNode common.InterfaceTypeToPodList, port int,
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

	_, _ = fmt.Fprintf(GinkgoWriter, "verifying connectivity from pod %s on node %s with IP %s to pod"+
		" %s on node %s with IP %s\n", senderPod.Name, senderPod.Spec.NodeName, senderPod.Status.PodIP,
		receiverPod.Name, receiverPod.Spec.NodeName, receiverPod.Status.PodIP)

	stdOut, stdErr, err := f.K8sResourceManagers.PodManager().
		PodExec(senderPod.Namespace, senderPod.Name, testerCommand)
	Expect(err).ToNot(HaveOccurred())

	_, _ = fmt.Fprintf(GinkgoWriter, "stdout: %s and stderr: %s\n", stdOut, stdErr)

	Expect(stdErr).To(ContainSubstring(expectedStderr))
	Expect(stdOut).To(ContainSubstring(expectedStdout))
}
