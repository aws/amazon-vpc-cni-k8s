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
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/integration/common"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/apps/v1"
	coreV1 "k8s.io/api/core/v1"
)

// Ensures Pods are launched on both Primary and Secondary Network Interfaces on two nodes.
// and the test verifies network connectivity across pods launched on these interfaces.

// The total test will take 1 hour of constantly exercising pod launch on primary and secondary interfaces.
// running connectivity tests, and deleting the pods, and repeating the process.

var _ = Describe("SOAK Test pod networking", Ordered, func() {

	var (
		err                               error
		serverListenCmd                   []string
		serverListenCmdArgs               []string
		testConnectionCommandFunc         func(serverPod coreV1.Pod, port int) []string
		testFailedConnectionCommandFunc   func(serverPod coreV1.Pod, port int) []string
		testerExpectedStdOut              string
		testerExpectedStdErr              string
		serverPort                        int
		protocol                          string
		primaryNodeDeployment             *v1.Deployment
		secondaryNodeDeployment           *v1.Deployment
		primaryNodeService                *coreV1.Service
		secondaryNodeService              *coreV1.Service
		interfaceToPodListOnPrimaryNode   common.InterfaceTypeToPodList
		interfaceToPodListOnSecondaryNode common.InterfaceTypeToPodList
		timesToRunTheTest                 = 12
		waitDuringInMinutes               = time.Duration(5) * time.Minute
	)

	// External probe is opt-in so it only runs where we have outbound
	// internet access.
	externalProbeEnabled := os.Getenv("RUN_EXTERNAL_PROBE") == "true"
	externalProbeHost := os.Getenv("EXTERNAL_PROBE_HOST")
	if externalProbeHost == "" {
		externalProbeHost = "checkip.amazonaws.com"
	}

	BeforeAll(func() {
		fmt.Println("Starting SOAK test")

		protocol = "tcp"
		serverPort = 2273

		By("Authorize Security Group Ingress on EC2 instance.")
		err = f.CloudServices.EC2().
			AuthorizeSecurityGroupIngress(context.TODO(), instanceSecurityGroupID, protocol, serverPort, serverPort, "0.0.0.0/0", false)
		Expect(err).ToNot(HaveOccurred())

		By("Authorize Security Group Egress on EC2 instance.")
		err = f.CloudServices.EC2().
			AuthorizeSecurityGroupEgress(context.TODO(), instanceSecurityGroupID, protocol, serverPort, serverPort, "0.0.0.0/0")
		Expect(err).ToNot(HaveOccurred())
	})

	AfterAll(func() {
		fmt.Println("Cleaning SOAK test")

		By("Revoke Security Group Ingress.")
		err = f.CloudServices.EC2().
			RevokeSecurityGroupIngress(context.TODO(), instanceSecurityGroupID, protocol, serverPort, serverPort, "0.0.0.0/0", false)
		Expect(err).ToNot(HaveOccurred())

		By("Revoke Security Group Egress.")
		err = f.CloudServices.EC2().
			RevokeSecurityGroupEgress(context.TODO(), instanceSecurityGroupID, protocol, serverPort, serverPort, "0.0.0.0/0")
		Expect(err).ToNot(HaveOccurred())

		By("SOAK test completed")
	})

	Context("[SOAK_TEST] Establish TCP connection from tester to server on both Primary and Secondary ENI", func() {
		BeforeEach(func() {
			serverListenCmd = []string{"nc"}
			// The nc flag "-l" for listen mode, "-k" to keep server up and not close connection after each connection
			serverListenCmdArgs = []string{"-k", "-l", strconv.Itoa(serverPort)}

			// netcat verbose output is being redirected to stderr instead of stdout
			// The nc flag "-v" for verbose output and "-wn" for timing out in n seconds
			testConnectionCommandFunc = func(receiverPod coreV1.Pod, port int) []string {
				return []string{"nc", "-v", "-w5", receiverPod.Status.PodIP, strconv.Itoa(port)}
			}

			// Create a negative test case with the wrong port number. This is to reinforce the
			// positive test case work by verifying negative cases do throw error
			testFailedConnectionCommandFunc = func(receiverPod coreV1.Pod, port int) []string {
				return []string{"nc", "-v", "-w5", receiverPod.Status.PodIP, strconv.Itoa(port + 1)}
			}

			serverContainer := manifest.
				NewNetCatAlpineContainer(f.Options.TestImageRegistry).
				Command(serverListenCmd).
				Args(serverListenCmdArgs).
				Build()

			By("Creating Pods on Primary and Secondary ENI on Primary and Secondary Node")
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

			Expect(len(interfaceToPodListOnSecondaryNode.PodsOnPrimaryENI)).
				Should(BeNumerically(">", 1))

			Expect(len(interfaceToPodListOnSecondaryNode.PodsOnSecondaryENI)).
				Should(BeNumerically(">", 1))

			By("Creating ClusterIP services in front of each deployment")
			primaryNodeService, err = f.K8sResourceManagers.ServiceManager().
				CreateService(context.TODO(), manifest.NewHTTPService().
					ServiceType(coreV1.ServiceTypeClusterIP).
					Name("primary-node-svc").
					Port(int32(serverPort)).
					Protocol(coreV1.ProtocolTCP).
					Selector("node", "primary").
					Build())
			Expect(err).ToNot(HaveOccurred())

			secondaryNodeService, err = f.K8sResourceManagers.ServiceManager().
				CreateService(context.TODO(), manifest.NewHTTPService().
					ServiceType(coreV1.ServiceTypeClusterIP).
					Name("secondary-node-svc").
					Port(int32(serverPort)).
					Protocol(coreV1.ProtocolTCP).
					Selector("node", "secondary").
					Build())
			Expect(err).ToNot(HaveOccurred())
		})

		AfterEach(func() {
			By("TearDown services")
			if primaryNodeService != nil {
				err = f.K8sResourceManagers.ServiceManager().
					DeleteAndWaitTillServiceDeleted(context.TODO(), primaryNodeService)
				Expect(err).ToNot(HaveOccurred())
			}
			if secondaryNodeService != nil {
				err = f.K8sResourceManagers.ServiceManager().
					DeleteAndWaitTillServiceDeleted(context.TODO(), secondaryNodeService)
				Expect(err).ToNot(HaveOccurred())
			}

			By("TearDown Pods")
			err = f.K8sResourceManagers.DeploymentManager().
				DeleteAndWaitTillDeploymentIsDeleted(primaryNodeDeployment)
			Expect(err).ToNot(HaveOccurred())

			err = f.K8sResourceManagers.DeploymentManager().
				DeleteAndWaitTillDeploymentIsDeleted(secondaryNodeDeployment)
			Expect(err).ToNot(HaveOccurred())

		})

		for i := 0; i < timesToRunTheTest; i++ {
			It("assert connectivity across nodes and across interface types", func() {

				testerExpectedStdErr = "succeeded!"
				testerExpectedStdOut = ""

				CheckConnectivityForMultiplePodPlacement(
					interfaceToPodListOnPrimaryNode, interfaceToPodListOnSecondaryNode,
					serverPort, testerExpectedStdOut, testerExpectedStdErr, testConnectionCommandFunc)

				By("verifying connection fails for unreachable port")

				VerifyConnectivityFailsForNegativeCase(interfaceToPodListOnPrimaryNode.PodsOnPrimaryENI[0],
					interfaceToPodListOnPrimaryNode.PodsOnPrimaryENI[1], serverPort,
					testFailedConnectionCommandFunc)

				By("verifying ClusterIP service connectivity from secondary-ENI pods")
				verifyClusterIPConnectivity(
					interfaceToPodListOnPrimaryNode.PodsOnSecondaryENI[0],
					secondaryNodeService.Spec.ClusterIP, serverPort)
				verifyClusterIPConnectivity(
					interfaceToPodListOnSecondaryNode.PodsOnSecondaryENI[0],
					primaryNodeService.Spec.ClusterIP, serverPort)

				if externalProbeEnabled {
					By(fmt.Sprintf("verifying external connectivity to %s from secondary-ENI pod", externalProbeHost))
					verifySoakExternalConnectivity(
						interfaceToPodListOnPrimaryNode.PodsOnSecondaryENI[0],
						externalProbeHost)
				}

				time.Sleep(waitDuringInMinutes)
			})
		}
	})
})

// verifyClusterIPConnectivity exercises the kube-proxy dstnat → aws-cni
// nat-prerouting chain ordering. ClusterIP traffic stays within the VPC CIDR
// so it returns early in the snat-mark chain — this validates chain priority,
// not the mark-set side.
func verifyClusterIPConnectivity(senderPod coreV1.Pod, clusterIP string, port int) {
	cmd := []string{"nc", "-v", "-w5", clusterIP, strconv.Itoa(port)}
	stdout, stderr, err := f.K8sResourceManagers.PodManager().
		PodExec(senderPod.Namespace, senderPod.Name, cmd)
	fmt.Fprintf(GinkgoWriter, "clusterIP probe [%s → %s:%d]: stdout=%s stderr=%s\n",
		senderPod.Name, clusterIP, port, stdout, stderr)
	Expect(err).ToNot(HaveOccurred(), "ClusterIP probe failed from pod %s to %s", senderPod.Name, clusterIP)
	Expect(stderr).To(ContainSubstring("succeeded!"))
}

// verifySoakExternalConnectivity exercises the mark-set side of the snat-mark
// chain by opening a TCP connection to a non-VPC destination. Opt-in via
// RUN_EXTERNAL_PROBE=true; only enable where outbound internet access is
// available.
func verifySoakExternalConnectivity(senderPod coreV1.Pod, host string) {
	cmd := []string{"nc", "-v", "-w5", host, "80"}
	stdout, stderr, err := f.K8sResourceManagers.PodManager().
		PodExec(senderPod.Namespace, senderPod.Name, cmd)
	fmt.Fprintf(GinkgoWriter, "external probe [%s → %s:80]: stdout=%s stderr=%s\n",
		senderPod.Name, host, stdout, stderr)
	Expect(err).ToNot(HaveOccurred(), "external probe failed from pod %s to %s", senderPod.Name, host)
	Expect(stderr).To(ContainSubstring("succeeded!"))
}
