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

package cni_egress

import (
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/apps/v1"
	coreV1 "k8s.io/api/core/v1"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/integration/common"
)

const (
	cmdForPodEgressIpv6       = `ip -f inet6 address show dev v6if0 | sed -n "s/.*inet6 \(fd00::ac:.*\)\/118.*/\1/p"`
	cmdForPodEgressIpv4       = `ip -f inet address show dev v4if0 | sed -n "s/.*inet \(169.254..*\)\/22.*/\1/p"`
	PublicUrlForEgressTesting = "icanhazip.com"
	podNumberToCheck          = 2 // to save integration test time, we only choose 2 pods to test
	pingTimes                 = 3 // we ping 3 times to test connectivity
)

var _ = Describe("[CANARY] test cluster egress connectivity", func() {
	var (
		err error
		// test container that verifies connectivity to an external IPv6 using IPv6 only
		testerContainer coreV1.Container

		// Primary node busybox deployment
		primaryNodeDeployment *v1.Deployment

		v6ClusterVars v4EgressVars // in v4 cluster, ipv6 egress needs to be tested
		v4ClusterVars v6EgressVars // in v6 cluster, ipv4 egress needs to be tested
	)

	BeforeEach(func() {
		// initialize vars
		err = nil

		// initialize curl container for testing later
		testerContainer = manifest.NewCurlContainer(f.Options.TestImageRegistry).
			Command([]string{"sleep", "3600"}).Build()

		testerContainer.SecurityContext = &coreV1.SecurityContext{
			RunAsUser: aws.Int64(0)} // ping (inside busybox) needs root to run, normal ping does not need root to run

		// in IPv6 cluster, only need 2 pods for testing
		var replicas = 2
		if isIPv4Cluster {
			// v4 cluster needs more pods to trigger secondary ENI
			// extra pods will not trigger secondary ENI in v6 cluster
			replicas = maxIPPerInterface * 2
			v4ClusterVars = v6EgressVars{}
		} else {
			v6ClusterVars = v4EgressVars{}
		}

		By(fmt.Sprintf("creating test deployment on primary node: %s", primaryNode.Name))
		primaryNodeDeployment = manifest.
			NewDefaultDeploymentBuilder().
			Container(testerContainer).
			Replicas(replicas).
			NodeName(primaryNode.Name).
			PodLabel("node", "primary").
			Name(fmt.Sprintf("primarynode-egress-tester")).
			Build()
		primaryNodeDeployment, err = f.K8sResourceManagers.
			DeploymentManager().
			CreateAndWaitTillDeploymentIsReady(primaryNodeDeployment, utils.DefaultDeploymentReadyTimeout)
		Expect(err).ToNot(HaveOccurred())

		if isIPv4Cluster {
			// extra check for v4 cluster to assert pods are using both primary and secondary ENI
			interfaceToPodListOnPrimaryNode :=
				common.GetPodsOnPrimaryAndSecondaryInterface(primaryNode, "node", "primary", f)
			// At least two Pods should be placed on the Primary and Secondary Interface
			// on the Primary and Secondary Node in order to test all possible scenarios
			v4ClusterVars.podsInPrimaryENI = interfaceToPodListOnPrimaryNode.PodsOnPrimaryENI
			Expect(len(v4ClusterVars.podsInPrimaryENI)).Should(BeNumerically(">", 1))
			v4ClusterVars.podsInSecondaryENI = interfaceToPodListOnPrimaryNode.PodsOnSecondaryENI
			Expect(len(v4ClusterVars.podsInSecondaryENI)).Should(BeNumerically(">", 1))

			v4ClusterVars.podsInPrimaryENIIPs = getPodIpsFromPodList(v4ClusterVars.podsInPrimaryENI, isIPv4Cluster, podNumberToCheck)
			Expect(len(v4ClusterVars.podsInPrimaryENIIPs)).Should(BeNumerically(">", 1))

			v4ClusterVars.podsInSecondaryENIIPs = getPodIpsFromPodList(v4ClusterVars.podsInSecondaryENI, isIPv4Cluster, podNumberToCheck)
			Expect(len(v4ClusterVars.podsInSecondaryENIIPs)).Should(BeNumerically(">", 1))
		} else {
			v6ClusterVars.pods, err = f.K8sResourceManagers.PodManager().
				GetPodsWithLabelSelector("node", "primary")
			Expect(err).ToNot(HaveOccurred())
			Expect(len(v6ClusterVars.pods.Items)).Should(BeNumerically(">", 1))

			// we only need IPs form 2 PODs running in primary node for egress blocking testing cross PODs
			v6ClusterVars.podsIPs = getPodIpsFromPodList(v6ClusterVars.pods.Items, isIPv4Cluster, podNumberToCheck)
			Expect(len(v6ClusterVars.podsIPs)).Should(BeNumerically(">", 1))
		}

	})

	JustAfterEach(func() {
		if primaryNodeDeployment != nil {
			By("deleting the primary node egress-tester deployment")
			err = f.K8sResourceManagers.DeploymentManager().
				DeleteAndWaitTillDeploymentIsDeleted(primaryNodeDeployment)
			Expect(err).ToNot(HaveOccurred())
		}
	})

	It("container can access off-cluster service using egress interface", func() {
		egressIpFamily := "IPv4"
		if isIPv4Cluster {
			egressIpFamily = "IPv6"
			By(fmt.Sprintf("testing pods in primary ENI %s egress running in primary node: %s", egressIpFamily, primaryNode.Name))
			testPodsEgress(v4ClusterVars.podsInPrimaryENI, PublicUrlForEgressTesting, *primaryNodeIp, isIPv4Cluster)
			By(fmt.Sprintf("testing pods in secondary ENI %s egress running in primary node: %s", egressIpFamily, primaryNode.Name))
			testPodsEgress(v4ClusterVars.podsInSecondaryENI, PublicUrlForEgressTesting, *primaryNodeIp, isIPv4Cluster)

			By(fmt.Sprintf("testing %s ping between PODs within same ENI using egress interface is blocked", egressIpFamily))
			testEgressTrafficBlockedBetweenPods(v4ClusterVars.podsInPrimaryENI, v4ClusterVars.podsInPrimaryENIIPs, isIPv4Cluster, podNumberToCheck)
			testEgressTrafficBlockedBetweenPods(v4ClusterVars.podsInSecondaryENI, v4ClusterVars.podsInSecondaryENIIPs, isIPv4Cluster, podNumberToCheck)

			By(fmt.Sprintf("testing %s ping between PODs within different ENIs using egress interface is blocked", egressIpFamily))
			testEgressTrafficBlockedBetweenPods(v4ClusterVars.podsInPrimaryENI, v4ClusterVars.podsInSecondaryENIIPs, isIPv4Cluster, podNumberToCheck)
			testEgressTrafficBlockedBetweenPods(v4ClusterVars.podsInSecondaryENI, v4ClusterVars.podsInPrimaryENIIPs, isIPv4Cluster, podNumberToCheck)
		} else {
			By(fmt.Sprintf("testing pods %s egress", egressIpFamily))
			testPodsEgress(v6ClusterVars.pods.Items, PublicUrlForEgressTesting, *primaryNodeIp, isIPv4Cluster)

			By(fmt.Sprintf("testing %s ping between PODs using egress interface is blocked", egressIpFamily))
			testEgressTrafficBlockedBetweenPods(v6ClusterVars.pods.Items, v6ClusterVars.podsIPs, isIPv4Cluster, podNumberToCheck)
		}
	})
})

func getPodIpsFromPodList(pods []coreV1.Pod, isIpv4Cluster bool, topN int) map[string]string {
	var podIps = map[string]string{}
	for _, pod := range pods {
		By(fmt.Sprintf("fetching pod %s egress address ...", pod.Name))
		podIps[pod.Name] = getPodIp(pod, isIpv4Cluster)
		if len(podIps) >= topN {
			break
		}
	}
	return podIps
}

func getPodIp(pod coreV1.Pod, isIpv4Cluster bool) string {
	cmd := []string{"/bin/sh", "-c"}
	if isIpv4Cluster {
		cmd = append(cmd, cmdForPodEgressIpv6)
	} else {
		cmd = append(cmd, cmdForPodEgressIpv4)
	}
	stdout, stderr, err := f.K8sResourceManagers.PodManager().PodExec(
		pod.Namespace,
		pod.Name,
		cmd)
	Expect(stderr).To(BeEmpty())
	Expect(stdout).ToNot(BeEmpty())
	Expect(err).ToNot(HaveOccurred())
	return strings.TrimSpace(stdout)
}

func testPodsEgress(pods []coreV1.Pod, publicUrl string, expectedAddress string, isIpv4Cluster bool) {
	ipVersionParameter := "-4"
	if isIpv4Cluster {
		ipVersionParameter = "-6" // testing ipv6 egress for ipv4 cluster
	}
	for _, pod := range pods {
		stdout, stderr, err := f.K8sResourceManagers.PodManager().PodExec(
			pod.Namespace,
			pod.Name,
			[]string{"curl", ipVersionParameter, "--silent", "--connect-timeout", "5", publicUrl})
		Expect(err).ToNot(HaveOccurred())
		Expect(stderr).Should(BeEmpty())
		Expect(stdout).Should(Equal(expectedAddress + "\n"))
	}
}

func testEgressTrafficBlockedBetweenPods(pods []coreV1.Pod, iPs map[string]string, isIpv4Cluster bool, testPodCount int) {
	var pingCmd = "ping"
	if isIpv4Cluster {
		pingCmd = "ping6"
	}
	for i, testPod := range pods {
		if i > testPodCount {
			break
		}

		for destPodName, destPodIp := range iPs {
			_, _, err := f.K8sResourceManagers.PodManager().PodExec(
				testPod.Namespace,
				testPod.Name,
				[]string{pingCmd, "-c", fmt.Sprintf("%d", pingTimes), destPodIp})
			if testPod.Name == destPodName {
				// ping its own egress address should not be blocked
				Expect(err).ToNot(HaveOccurred())
				By(fmt.Sprintf("in pod %s ping its own IPv6 egress address - ok", testPod.Name))
			} else {
				// ping other pods' egress address should be blocked
				Expect(err).To(HaveOccurred())
				By(fmt.Sprintf("in pod %s ping pod %s IPv6 egress address - blocked", testPod.Name, destPodName))
			}
		}
	}
}
