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
	"fmt"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/agent"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Test pod networking with prefix delegation enabled <-> disabled", func() {
	var (
		// The Pod labels for client and server in order to retrieve the
		// client and server Pods belonging to a Deployment/Jobs
		labelKey                = "app"
		serverPodLabelVal       = "server-pod"
		clientPodLabelVal       = "client-pod"
		serverDeploymentBuilder *manifest.DeploymentBuilder
		// Value for the Environment variable ENABLE_PREFIX_DELEGATION
		enableIPv4PrefixDelegation string
		firstRun                   bool
		lastRun                    bool
	)

	JustBeforeEach(func() {
		// TODO Gingko doesnt support beforeAll so while adding upgrades/downgrades will move this to a suite
		if firstRun {

			By("creating deployment")
			serverDeploymentBuilder = manifest.NewDefaultDeploymentBuilder().
				Name("traffic-server").
				NodeSelector(f.Options.NgNameLabelKey, f.Options.NgNameLabelVal)
		}

		By(fmt.Sprintf("Setting PD - %v", enableIPv4PrefixDelegation))
		k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName,
			utils.AwsNodeNamespace, utils.AwsNodeName,
			map[string]string{"ENABLE_PREFIX_DELEGATION": enableIPv4PrefixDelegation})

	})

	JustAfterEach(func() {
		if lastRun {

			k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName,
				utils.AwsNodeNamespace, utils.AwsNodeName,
				map[string]string{"ENABLE_PREFIX_DELEGATION": "false"})
		}
	})

	Context("when testing TCP traffic between client and server pods on enabling PD", func() {
		BeforeEach(func() {
			enableIPv4PrefixDelegation = "true"
			firstRun = true
			lastRun = false
		})

		//TODO : Add pod IP validation if IP belongs to prefix
		//TODO : remove hardcoding from client/server count
		It("should have 99+% success rate", func() {
			trafficTester := agent.TrafficTest{
				Framework:                      f,
				TrafficServerDeploymentBuilder: serverDeploymentBuilder,
				ServerPort:                     2273,
				ServerProtocol:                 "tcp",
				ClientCount:                    20,
				ServerCount:                    20,
				ServerPodLabelKey:              labelKey,
				ServerPodLabelVal:              serverPodLabelVal,
				ClientPodLabelKey:              labelKey,
				ClientPodLabelVal:              clientPodLabelVal,
			}

			successRate, err := trafficTester.TestTraffic()
			Expect(err).ToNot(HaveOccurred())
			Expect(successRate).Should(BeNumerically(">=", float64(99)))
		})
	})

	Context("when testing TCP traffic between client and server pods on disabling PD", func() {
		BeforeEach(func() {
			enableIPv4PrefixDelegation = "false"
			firstRun = false
			lastRun = true
		})

		//TODO : Add pod IP validation if IP belongs to SIP
		//TODO : remove hardcoding from client/server count
		It("should have 99+% success rate", func() {
			trafficTester := agent.TrafficTest{
				Framework:                      f,
				TrafficServerDeploymentBuilder: serverDeploymentBuilder,
				ServerPort:                     2273,
				ServerProtocol:                 "tcp",
				ClientCount:                    20,
				ServerCount:                    20,
				ServerPodLabelKey:              labelKey,
				ServerPodLabelVal:              serverPodLabelVal,
				ClientPodLabelKey:              labelKey,
				ClientPodLabelVal:              clientPodLabelVal,
			}

			successRate, err := trafficTester.TestTraffic()
			Expect(err).ToNot(HaveOccurred())
			Expect(successRate).Should(BeNumerically(">=", float64(99)))
		})
	})
})
