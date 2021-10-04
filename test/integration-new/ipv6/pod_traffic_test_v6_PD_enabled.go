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

package ipv6

import (
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/agent"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Test pod networking with prefix delegation enabled", func() {
	var (
		// The Pod labels for client and server in order to retrieve the
		// client and server Pods belonging to a Deployment/Jobs
		labelKey                = "app"
		serverPodLabelVal       = "server-pod"
		clientPodLabelVal       = "client-pod"
		serverDeploymentBuilder *manifest.DeploymentBuilder
	)

	JustBeforeEach(func() {
		By("creating deployment")
		serverDeploymentBuilder = manifest.NewDefaultDeploymentBuilder().
			Name("traffic-server").
			NodeSelector(f.Options.NgNameLabelKey, f.Options.NgNameLabelVal)
	})

	Context("when testing TCP traffic between client and server pods", func() {
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
				IsV6Enabled:                    true,
			}

			successRate, err := trafficTester.TestTraffic()
			Expect(err).ToNot(HaveOccurred())
			Expect(successRate).Should(BeNumerically(">=", float64(99)))
		})
	})

	Context("when testing UDP traffic between client and server pods", func() {
		It("should have 99+% success rate", func() {
			trafficTester := agent.TrafficTest{
				Framework:                      f,
				TrafficServerDeploymentBuilder: serverDeploymentBuilder,
				ServerPort:                     2273,
				ServerProtocol:                 "udp",
				ClientCount:                    20,
				ServerCount:                    20,
				ServerPodLabelKey:              labelKey,
				ServerPodLabelVal:              serverPodLabelVal,
				ClientPodLabelKey:              labelKey,
				ClientPodLabelVal:              clientPodLabelVal,
                IsV6Enabled:                    true,
			}

			successRate, err := trafficTester.TestTraffic()
			Expect(err).ToNot(HaveOccurred())
			Expect(successRate).Should(BeNumerically(">=", float64(99)))
		})
	})
})
