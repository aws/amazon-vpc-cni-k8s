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

package ipamd

import (
	"encoding/json"
	"fmt"
	"maps"
	"strconv"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// HyperPod versions of the warm target, ENI/IP leak and ENI tag specs, plus specs for the HyperPod reserved ENI slot.
// HyperPod owns the ENI at device index 0 on network card 0, so the CNI can use one ENI less than the instance limit.
// The node also runs pod-network DaemonSets, so warm targets are counted on top of the IPs those pods hold.
var _ = Describe("[HYPERPOD] test HyperPod nodes", func() {
	BeforeEach(func() {
		if !isHyperPod {
			Skip("primary node is not a HyperPod node")
		}
	})

	// setEnv sets aws-node environment variables and restores their previous values in a single update when the spec
	// ends, so that e.g. ENABLE_PREFIX_DELEGATION and the warm targets never leave aws-node in an invalid combination
	setEnv := func(env map[string]string) {
		ds, err := f.K8sResourceManagers.DaemonSetManager().GetDaemonSet(utils.AwsNodeNamespace, utils.AwsNodeName)
		Expect(err).ToNot(HaveOccurred())
		previous := map[string]string{}
		for _, container := range ds.Spec.Template.Spec.Containers {
			if container.Name != utils.AwsNodeName {
				continue
			}
			for _, envVar := range container.Env {
				if _, ok := env[envVar.Name]; ok {
					previous[envVar.Name] = envVar.Value
				}
			}
		}
		added := map[string]struct{}{}
		for key := range env {
			if _, ok := previous[key]; !ok {
				added[key] = struct{}{}
			}
		}

		// The framework removes already present keys from the map it is given, so pass a copy
		k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace, utils.AwsNodeName,
			maps.Clone(env))
		DeferCleanup(func() {
			k8sUtils.UpdateEnvVarOnDaemonSetAndWaitUntilReady(f, utils.AwsNodeName, utils.AwsNodeNamespace,
				utils.AwsNodeName, maps.Clone(previous), added)
		})
	}

	// deployPods deploys pods on the HyperPod node. A nodeSelector rather than nodeName is used so the scheduler
	// enforces capacity.pods.
	deployPods := func(name string, replicas int) {
		deploymentSpec := manifest.NewBusyBoxDeploymentBuilder(f.Options.TestImageRegistry).
			Name(name).
			NodeSelector("kubernetes.io/hostname", hyperPodNode.Labels["kubernetes.io/hostname"]).
			Replicas(replicas).
			Build()
		_, err := f.K8sResourceManagers.DeploymentManager().
			CreateAndWaitTillDeploymentIsReady(deploymentSpec, utils.DefaultDeploymentReadyTimeout*5)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			Expect(f.K8sResourceManagers.DeploymentManager().
				DeleteAndWaitTillDeploymentIsDeleted(deploymentSpec)).To(Succeed())
		})
	}

	Context("when warm ENI target is used", func() {
		DescribeTable("the node should have the expected ENIs",
			func(warmENITarget, maxENI int) {
				// A negative target means one ENI more than the instance limit, which only the reserved slot could hold
				if warmENITarget < 0 {
					warmENITarget = usableENILimit() + hyperPodReservedENIs + 1
				}

				// MAX_ENI only stops new attachments, so first free the ENIs the pods on the node do not need
				By("freeing unused ENIs by setting WARM_ENI_TARGET to 0")
				setEnv(map[string]string{"WARM_ENI_TARGET": "0"})
				Eventually(func(g Gomega) {
					g.Expect(describeHyperPodInstance().NetworkInterfaces).To(HaveLen(expectedENIs(0, 0)))
				}).WithTimeout(10 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())

				setEnv(map[string]string{
					"WARM_ENI_TARGET": strconv.Itoa(warmENITarget),
					"MAX_ENI":         strconv.Itoa(maxENI),
				})

				Eventually(func(g Gomega) {
					g.Expect(describeHyperPodInstance().NetworkInterfaces).To(HaveLen(expectedENIs(warmENITarget, maxENI)))
				}).WithTimeout(10 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())

				verifyNoFailedENIAllocations(time.Minute)
			},
			// MAX_ENI counts the reserved ENI
			Entry("when WARM_ENI_TARGET = 3 and MAX_ENI = 2", 3, 2),
			Entry("when WARM_ENI_TARGET = 3", 3, 0),
			Entry("when WARM_ENI_TARGET = 2", 2, 0),
			Entry("when WARM_ENI_TARGET exceeds the instance ENI limit", -1, 0),
		)
	})

	Context("when warm IP target is set", func() {
		DescribeTable("the node should have the expected secondary IPv4 addresses",
			func(warmIPTarget, minIPTarget int) {
				setEnv(map[string]string{
					"WARM_IP_TARGET":    strconv.Itoa(warmIPTarget),
					"MINIMUM_IP_TARGET": strconv.Itoa(minIPTarget),
				})

				Eventually(func(g Gomega) {
					ips, _ := countSecondaryIPsAndPrefixes(describeHyperPodInstance())
					g.Expect(ips).To(Equal(expectedIPs(warmIPTarget, minIPTarget)))
				}).WithTimeout(10 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())
			},
			Entry("when WARM_IP_TARGET = 2", 2, 0),
			Entry("when WARM_IP_TARGET = 16", 16, 0),
			Entry("when MINIMUM_IP_TARGET = 2", 0, 2),
			Entry("when MINIMUM_IP_TARGET = 16", 0, 16),
			Entry("when MINIMUM_IP_TARGET = 10 and WARM_IP_TARGET = 6", 6, 10),
		)
	})

	Context("when prefix delegation is enabled", func() {
		DescribeTable("the node should have the expected prefixes",
			func(warmIPTarget, minIPTarget, warmPrefixTarget int) {
				setEnv(map[string]string{
					"ENABLE_PREFIX_DELEGATION": "true",
					"WARM_IP_TARGET":           strconv.Itoa(warmIPTarget),
					"MINIMUM_IP_TARGET":        strconv.Itoa(minIPTarget),
					"WARM_PREFIX_TARGET":       strconv.Itoa(warmPrefixTarget),
				})

				Eventually(func(g Gomega) {
					secondaryIPs, prefixes := countSecondaryIPsAndPrefixes(describeHyperPodInstance())
					g.Expect(prefixes).To(Equal(expectedPrefixes(warmIPTarget, minIPTarget, warmPrefixTarget, secondaryIPs)))
				}).WithTimeout(10 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())
			},
			Entry("when WARM_IP_TARGET = 2", 2, 0, 0),
			Entry("when WARM_IP_TARGET = 16", 16, 0, 0),
			Entry("when MINIMUM_IP_TARGET = 2", 0, 2, 0),
			Entry("when MINIMUM_IP_TARGET = 16", 0, 16, 0),
			Entry("when MINIMUM_IP_TARGET = 10 and WARM_IP_TARGET = 6", 6, 10, 0),
			Entry("when WARM_PREFIX_TARGET = 2", 0, 0, 2),
			Entry("when WARM_IP_TARGET = 2 and WARM_PREFIX_TARGET = 1", 2, 0, 1),
			Entry("when MINIMUM_IP_TARGET = 2 and WARM_PREFIX_TARGET = 2", 0, 2, 2),
			Entry("when MINIMUM_IP_TARGET = 10, WARM_IP_TARGET = 6 and WARM_PREFIX_TARGET = 1", 6, 10, 1),
		)
	})

	Context("when pods are deleted", func() {
		It("should restore the ENI and IP state", func() {
			setEnv(map[string]string{"WARM_IP_TARGET": "3", "WARM_ENI_TARGET": "0"})

			var oldIPs, oldENIs int
			Eventually(func(g Gomega) {
				instance := describeHyperPodInstance()
				ips, _ := countSecondaryIPsAndPrefixes(instance)
				g.Expect(ips).To(Equal(expectedIPs(3, 0)))
				oldIPs, oldENIs = ips, len(instance.NetworkInterfaces)
			}).WithTimeout(10 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())

			// Deploy half of the pods the node can still take
			capacity := min(maxENIsForPods()*secondaryIPsPerENI(), nodeMaxPods()) - len(getPodsOnNode())
			deploymentSpec := manifest.NewBusyBoxDeploymentBuilder(f.Options.TestImageRegistry).
				Name("hyperpod-ip-leak").
				NodeSelector("kubernetes.io/hostname", hyperPodNode.Labels["kubernetes.io/hostname"]).
				Replicas(max(capacity/2, 1)).
				Build()
			_, err := f.K8sResourceManagers.DeploymentManager().
				CreateAndWaitTillDeploymentIsReady(deploymentSpec, utils.DefaultDeploymentReadyTimeout*5)
			Expect(err).ToNot(HaveOccurred())
			Expect(f.K8sResourceManagers.DeploymentManager().DeleteAndWaitTillDeploymentIsDeleted(deploymentSpec)).To(Succeed())

			Eventually(func(g Gomega) {
				instance := describeHyperPodInstance()
				ips, _ := countSecondaryIPsAndPrefixes(instance)
				g.Expect(ips).To(Equal(oldIPs))
				g.Expect(instance.NetworkInterfaces).To(HaveLen(oldENIs))
			}).WithTimeout(6 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())
		})
	})

	Context("when a secondary ENI is created", func() {
		DescribeTable("the new ENI should have the expected tags",
			func(env map[string]string, expectedTags map[string]string) {
				if maxENIsForPods() < 2 {
					Skip(fmt.Sprintf("capacity.pods (%d) fits on one ENI, so ipamd never creates a secondary ENI", nodeMaxPods()))
				}

				By("detaching unused ENIs by setting WARM_ENI_TARGET to 0")
				setEnv(map[string]string{"WARM_ENI_TARGET": "0"})
				time.Sleep(90 * time.Second)
				existingENIs := map[string]bool{}
				for _, ni := range describeHyperPodInstance().NetworkInterfaces {
					existingENIs[*ni.NetworkInterfaceId] = true
				}

				newENIEnv := map[string]string{"WARM_ENI_TARGET": "2"}
				for key, val := range env {
					newENIEnv[key] = val
				}
				setEnv(newENIEnv)

				var newENIs []string
				Eventually(func(g Gomega) {
					newENIs = nil
					for _, ni := range describeHyperPodInstance().NetworkInterfaces {
						if !existingENIs[*ni.NetworkInterfaceId] {
							newENIs = append(newENIs, *ni.NetworkInterfaceId)
						}
					}
					g.Expect(newENIs).ToNot(BeEmpty())
				}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())

				VerifyTagIsPresentOnENIs(newENIs, expectedTags)
			},
			Entry("when additional ENI tags are added using ADDITIONAL_ENI_TAGS",
				map[string]string{"ADDITIONAL_ENI_TAGS": mustMarshal(map[string]string{
					"tag_owner":                   "cni_automation_test",
					"k8s.amazonaws.com/tag_owner": "cni_automation_test",
				})},
				map[string]string{"tag_owner": "cni_automation_test"}),
			Entry("when CLUSTER_NAME is set",
				map[string]string{"CLUSTER_NAME": "dummy_cluster_name"},
				map[string]string{"cluster.k8s.amazonaws.com/name": "dummy_cluster_name"}),
		)
	})

	Context("when the reserved ENI slot is accounted for", func() {
		It("should report the reserved slot in the max ENI metric", func() {
			Expect(int(getIpamdMetric("awscni_eni_max", ""))).To(Equal(usableENILimit()))
		})

		DescribeTable("the node packed to capacity.pods should not attach an ENI to the reserved slot",
			func(warmENITarget int) {
				if !needsReservedSlot() {
					Skip(fmt.Sprintf("capacity.pods (%d) fits in the %d usable ENIs, so ipamd never needs the reserved slot",
						nodeMaxPods(), usableENILimit()))
				}
				setEnv(map[string]string{"WARM_ENI_TARGET": strconv.Itoa(warmENITarget)})

				// Leave one pod slot for the job that reads the ipamd metrics. With WARM_ENI_TARGET = 1 the usable ENIs
				// still have fewer free IPs than one ENI holds, so ipamd still wants another ENI.
				replicas := nodeMaxPods() - len(getPodsOnNode()) - 1
				By(fmt.Sprintf("deploying %d pods to fill %s to %d pods", replicas, hyperPodNode.Name, nodeMaxPods()-1))
				deployPods("hyperpod-reserved-eni", replicas)

				By("verifying all usable ENIs are attached")
				Eventually(func(g Gomega) {
					g.Expect(describeHyperPodInstance().NetworkInterfaces).To(HaveLen(usableENILimit()))
				}).WithTimeout(5 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())

				verifyNoFailedENIAllocations(2 * time.Minute)
				Expect(describeHyperPodInstance().NetworkInterfaces).To(HaveLen(usableENILimit()))
			},
			Entry("when WARM_ENI_TARGET = 1", 1),
			Entry("when WARM_ENI_TARGET = 0", 0),
		)

		// With WARM_ENI_TARGET = 0, packing the node only makes ipamd want another ENI once every IP on the usable ENIs
		// is assigned, which the node's host-network pods may prevent. An IP target above what the usable ENIs hold
		// asks ipamd for another ENI at any pod density.
		DescribeTable("an IP target above the usable ENIs should not attach an ENI to the reserved slot",
			func(useWarmIPTarget bool) {
				if !needsReservedSlot() {
					Skip(fmt.Sprintf("capacity.pods (%d) fits in the %d usable ENIs, so ipamd never needs the reserved slot",
						nodeMaxPods(), usableENILimit()))
				}
				usableIPs := usableENILimit() * secondaryIPsPerENI()
				env := map[string]string{"WARM_ENI_TARGET": "0"}
				if useWarmIPTarget {
					env["WARM_IP_TARGET"] = strconv.Itoa(usableIPs - assignedIPs() + 1)
				} else {
					env["MINIMUM_IP_TARGET"] = strconv.Itoa(usableIPs + 1)
				}
				setEnv(env)

				By("verifying all usable ENIs are attached and full")
				Eventually(func(g Gomega) {
					instance := describeHyperPodInstance()
					g.Expect(instance.NetworkInterfaces).To(HaveLen(usableENILimit()))
					ips, _ := countSecondaryIPsAndPrefixes(instance)
					g.Expect(ips).To(Equal(usableIPs))
				}).WithTimeout(10 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())

				verifyNoFailedENIAllocations(2 * time.Minute)
				Expect(describeHyperPodInstance().NetworkInterfaces).To(HaveLen(usableENILimit()))
			},
			Entry("when WARM_ENI_TARGET = 0 and MINIMUM_IP_TARGET exceeds the usable ENIs", false),
			Entry("when WARM_ENI_TARGET = 0 and WARM_IP_TARGET exceeds the usable ENIs", true),
		)
	})
})

func mustMarshal(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}
