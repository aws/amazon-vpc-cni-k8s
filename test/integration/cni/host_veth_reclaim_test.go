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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/networkutils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// A host veth can outlive its pod, and its name is derived from the pod's
// namespace and name, so a replacement finds the name taken. An unstamped link
// is reclaimed; one stamped for another pod is not. These specs manufacture both
// shapes on the node.
var _ = Describe("test host veth reclaim", func() {
	const (
		peerName    = "reclaim-peer"
		refusalText = "is owned by another pod"
	)

	var (
		targetPodName string
		staleVethName string
	)

	BeforeEach(func() {
		// ExecOnHost needs a host shell: AL2 and AL2023 only.
		if !strings.Contains(primaryNode.Status.NodeInfo.OSImage, "Amazon Linux") {
			Skip(fmt.Sprintf("host-exec needs a host shell, unavailable on %q",
				primaryNode.Status.NodeInfo.OSImage))
		}
		Expect(k8sUtils.EnsureHostExecDaemonSet(f)).To(Succeed())

		targetPodName = "veth-reclaim-target"
		staleVethName = networkutils.GeneratePodHostVethName(DEFAULT_VETH_PREFIX,
			utils.DefaultTestNamespace, targetPodName, 0)
	})

	AfterEach(func() {
		pod, err := f.K8sResourceManagers.PodManager().GetPod(utils.DefaultTestNamespace, targetPodName)
		if err == nil {
			Expect(f.K8sResourceManagers.PodManager().DeleteAndWaitTillPodDeleted(pod)).To(Succeed())
		}
		// Removing one end removes the pair, whichever end survived.
		_, _ = k8sUtils.ExecOnHost(f, primaryNode.Name,
			fmt.Sprintf("ip link del %s 2>/dev/null; ip link del %s 2>/dev/null; true", staleVethName, peerName))
	})

	Context("when an unstamped host veth already holds the name", func() {
		It("reclaims the name, and the pod gets networking", func() {
			By("creating an unstamped veth pair named for a pod that does not exist yet")
			makeVeth(staleVethName, peerName, "")

			By("confirming it carries no ownership alias")
			Expect(ifalias(staleVethName)).To(BeEmpty(),
				"the manufactured link must be unstamped, or this spec proves nothing")

			By("scheduling the pod that computes that same host veth name")
			_, err := f.K8sResourceManagers.PodManager().CreateAndWaitTillRunning(targetPod(targetPodName))
			Expect(err).ToNot(HaveOccurred(),
				"an unstamped leftover must be reclaimed, otherwise the pod never gets networking")

			By("confirming the link now belongs to this pod")
			Expect(ifalias(staleVethName)).To(Equal(ownershipAlias(utils.DefaultTestNamespace, targetPodName)),
				"the reclaimed link must carry this pod's ownership alias")
		})
	})

	// Slowest spec in the suite: it waits for the sandbox to keep failing.
	Context("when the name is held by a veth stamped for another pod", func() {
		It("refuses to delete it, and that interface survives", func() {
			foreign := ownershipAlias("other-namespace", "other-pod")

			By("creating a veth pair stamped for a different pod")
			makeVeth(staleVethName, peerName, foreign)

			By("creating the pod that computes that same host veth name")
			// Created, not waited on: waiting for Running would pass on any
			// failure, including one where no ADD ran.
			pod := targetPod(targetPodName)
			Expect(f.K8sClient.Create(context.Background(), pod)).To(Succeed())

			By("waiting for the sandbox to fail with the ownership refusal")
			Eventually(func(g Gomega) {
				events, err := f.K8sResourceManagers.EventManager().GetEventsWithOptions(&client.ListOptions{
					Namespace: utils.DefaultTestNamespace,
				})
				g.Expect(err).ToNot(HaveOccurred())
				// Matched on UID, not name: events outlive pods and this spec reuses
				// the name, so a stale event could otherwise satisfy it.
				g.Expect(events.Items).To(ContainElement(SatisfyAll(
					HaveField("Regarding.UID", pod.UID),
					HaveField("Reason", "FailedCreatePodSandBox"),
					HaveField("Note", ContainSubstring(refusalText)),
				)), "the CNI must refuse the ADD because the link belongs to another pod")
			}).WithTimeout(3 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())

			By("confirming the pod never started")
			running, err := f.K8sResourceManagers.PodManager().GetPod(utils.DefaultTestNamespace, pod.Name)
			Expect(err).ToNot(HaveOccurred())
			Expect(running.Status.Phase).ToNot(Equal(v1.PodRunning))

			By("confirming the other pod's interface was left untouched")
			Expect(ifalias(staleVethName)).To(Equal(foreign),
				"the foreign interface must survive the refused ADD")
		})
	})
})

// targetPod is scheduled where the stale veth was made.
func targetPod(name string) *v1.Pod {
	return manifest.NewDefaultPodBuilder().
		Name(name).
		Namespace(utils.DefaultTestNamespace).
		NodeName(primaryNode.Name).
		Container(manifest.NewBusyBoxContainerBuilder(f.Options.TestImageRegistry).Build()).
		Build()
}

// makeVeth creates a veth pair on primaryNode, both ends in the host namespace so
// the pair outlives the command that made it.
func makeVeth(hostEnd, peerEnd, alias string) {
	cmd := fmt.Sprintf("ip link add %s type veth peer name %s && ip link set %s up && ip link set %s up",
		hostEnd, peerEnd, hostEnd, peerEnd)
	if alias != "" {
		cmd += fmt.Sprintf(" && ip link set %s alias %s", hostEnd, alias)
	}
	_, err := k8sUtils.ExecOnHost(f, primaryNode.Name, cmd)
	Expect(err).ToNot(HaveOccurred(), "manufacturing the host veth must succeed")
}

// ifalias is empty when the link carries no alias.
func ifalias(linkName string) string {
	out, err := k8sUtils.ExecOnHost(f, primaryNode.Name,
		fmt.Sprintf("cat /sys/class/net/%s/ifalias 2>/dev/null || true", linkName))
	Expect(err).ToNot(HaveOccurred())
	return strings.TrimSpace(out)
}

// ownershipAlias is spelled out rather than taken from the plugin, so the specs
// pin the on-wire value.
func ownershipAlias(podNamespace, podName string) string {
	sum := sha256.Sum256([]byte(podNamespace + "/" + podName))
	return "aws-vpc-cni:" + hex.EncodeToString(sum[:])
}
