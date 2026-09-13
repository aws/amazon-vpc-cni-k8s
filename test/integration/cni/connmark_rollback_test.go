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
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/integration/common"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsV1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
)

// CNI image rollback / static-stability test.
//
// Provide two image URIs by environment:
//
//	STABLE_CNI_IMAGE   the previously released aws-node image (rollback target)
//	TARGET_CNI_IMAGE   this branch's aws-node image (PR build under test)
//
// If either is unset, the test is skipped — that way the spec is a no-op for
// regular CNI suite runs and only fires when the operator opts in by setting
// both env vars.
//
// "Rollback" means swapping the aws-node container image, NOT swapping the
// connmark backend (iptables vs nftables). On AL2023 both images run
// iptables-nft underneath; the only delta is that the PR build additionally
// programs an `aws-cni` nft table at priority -90. After rollback, the older
// image keeps using its default iptables path (whatever the AMI ships) at
// the standard NAT priority — numerically below -90 so it executes first
// and shadows the orphaned -90 chain. Residual nft rules persist but are
// inert; that's the static-stability claim this test guards.
//
// Contract:
//   - same long-lived pods on both primary and secondary ENI must remain
//     reachable across an image swap in either direction
//   - conntrack is flushed after each swap so probes open fresh connections
//     (without the flush a probe could ride a pre-swap entry that already
//     carries a mark from --restore-mark, silently bypassing the new ruleset)
//   - on-node nft + iptables state is dumped to the Ginkgo writer for
//     diagnostics; nothing about rule layout is asserted, since residual
//     rules are an expected post-condition of a rollback.
var _ = Describe("CNI image rollback static stability", func() {
	var (
		stableImage = os.Getenv("STABLE_CNI_IMAGE")
		targetImage = os.Getenv("TARGET_CNI_IMAGE")

		deployment *appsV1.Deployment
		service    *v1.Service
		pods       common.InterfaceTypeToPodList
		err        error
	)

	BeforeEach(func() {
		if stableImage == "" || targetImage == "" {
			Skip("STABLE_CNI_IMAGE and TARGET_CNI_IMAGE must both be set; skipping rollback test")
		}
	})

	createDeploymentAndService := func(label string) {
		serverContainer := manifest.NewBusyBoxContainerBuilder(f.Options.TestImageRegistry).
			Image(utils.GetTestImage(f.Options.TestImageRegistry, utils.NginxImage)).
			Command(nil).
			Port(v1.ContainerPort{ContainerPort: 80, Protocol: "TCP"}).
			Build()

		deployment = manifest.NewDefaultDeploymentBuilder().
			Name(fmt.Sprintf("rollback-test-server-%s", label)).
			Container(serverContainer).
			Replicas(maxIPPerInterface*2).
			NodeName(primaryNode.Name).
			PodLabel("app", "rollback-test").
			Build()

		deployment, err = f.K8sResourceManagers.DeploymentManager().
			CreateAndWaitTillDeploymentIsReady(deployment, utils.DefaultDeploymentReadyTimeout)
		Expect(err).ToNot(HaveOccurred())

		pods = common.GetPodsOnPrimaryAndSecondaryInterface(primaryNode, "app", "rollback-test", f)
		Expect(len(pods.PodsOnPrimaryENI)).Should(BeNumerically(">=", 1),
			"need pod on primary ENI to validate static stability for primary-ENI host networking path")
		Expect(len(pods.PodsOnSecondaryENI)).Should(BeNumerically(">=", 1),
			"need pod on secondary ENI to validate that connmark/SNAT path survives image swap")

		service = manifest.NewHTTPService().
			ServiceType(v1.ServiceTypeClusterIP).
			Name(fmt.Sprintf("rollback-test-svc-%s", label)).
			Selector("app", "rollback-test").
			Build()
		service, err = f.K8sResourceManagers.ServiceManager().CreateService(context.Background(), service)
		Expect(err).ToNot(HaveOccurred())

		// Allow kube-proxy to program service rules before probing.
		time.Sleep(utils.PollIntervalLong)
	}

	cleanup := func() {
		if service != nil {
			f.K8sResourceManagers.ServiceManager().DeleteAndWaitTillServiceDeleted(context.Background(), service)
			service = nil
		}
		if deployment != nil {
			f.K8sResourceManagers.DeploymentManager().DeleteAndWaitTillDeploymentIsDeleted(deployment)
			deployment = nil
		}
	}

	probeAllPods := func(when string) {
		By(fmt.Sprintf("verifying connectivity %s — primary ENI pod %s", when, pods.PodsOnPrimaryENI[0].Name))
		probeServiceFreshConn(pods.PodsOnPrimaryENI[0], service.Spec.ClusterIP)
		probeExternalFreshConn(pods.PodsOnPrimaryENI[0])

		By(fmt.Sprintf("verifying connectivity %s — secondary ENI pod %s", when, pods.PodsOnSecondaryENI[0].Name))
		probeServiceFreshConn(pods.PodsOnSecondaryENI[0], service.Spec.ClusterIP)
		probeExternalFreshConn(pods.PodsOnSecondaryENI[0])
	}

	// switchAwsNodeImage swaps the aws-node container's image and waits for
	// the daemonset rollout. Same approach as
	// scripts/run-mixed-os-snat-kube-proxy-test.sh's update_cni_image, but
	// in-process via the framework's k8s client so we don't shell out.
	switchAwsNodeImage := func(image string) {
		By(fmt.Sprintf("switching aws-node container image to %s", image))
		ds, gerr := f.K8sResourceManagers.DaemonSetManager().
			GetDaemonSet(utils.AwsNodeNamespace, utils.AwsNodeName)
		Expect(gerr).ToNot(HaveOccurred())
		updated := ds.DeepCopy()
		setContainerImage(updated.Spec.Template.Spec.Containers, utils.AwsNodeName, image)

		_, uerr := f.K8sResourceManagers.DaemonSetManager().
			UpdateAndWaitTillDaemonSetReady(ds, updated)
		Expect(uerr).ToNot(HaveOccurred())

		// Brief settle so the new pod's init/reconciliation finishes
		// programming rules before probes start.
		time.Sleep(15 * time.Second)
	}

	// flushConntrack drops every conntrack entry on the node so the next
	// probe forces fresh entry creation. Without this, probes can ride
	// pre-swap entries that already carry a mark from --restore-mark and
	// silently bypass the new ruleset — i.e. the test would pass even if
	// the new image's connmark programming were broken. Reuses
	// checkNodeShellPlugin/execNodeShell from pod_traffic_test.go.
	flushConntrack := func(nodeName string) {
		if err := checkNodeShellPlugin(); err != nil {
			fmt.Fprintf(GinkgoWriter, "node-shell plugin unavailable (%v); cannot flush conntrack — probes may ride pre-swap entries\n", err)
			return
		}
		out, ferr := execNodeShell(nodeName, "conntrack -F 2>&1 || true")
		fmt.Fprintf(GinkgoWriter, "conntrack -F:\n%s\n", string(out))
		Expect(ferr).ToNot(HaveOccurred(), "node-shell conntrack flush failed")
	}

	It("rollback (target -> stable): existing pods stay reachable; residual nft rules are harmless", func() {
		switchAwsNodeImage(targetImage)

		createDeploymentAndService("rollback")
		DeferCleanup(cleanup)
		DeferCleanup(switchAwsNodeImage, stableImage)

		dumpOnNodeState(primaryNode.Name, "after target install")
		probeAllPods("with target image")

		switchAwsNodeImage(stableImage)
		flushConntrack(primaryNode.Name)
		dumpOnNodeState(primaryNode.Name, "after rollback to stable")
		probeAllPods("after rollback to stable image (same pods)")
	})

	It("upgrade (stable -> target): existing pods stay reachable", func() {
		switchAwsNodeImage(stableImage)

		createDeploymentAndService("upgrade")
		DeferCleanup(cleanup)
		DeferCleanup(switchAwsNodeImage, stableImage)

		dumpOnNodeState(primaryNode.Name, "after stable install")
		probeAllPods("with stable image")

		switchAwsNodeImage(targetImage)
		flushConntrack(primaryNode.Name)
		dumpOnNodeState(primaryNode.Name, "after upgrade to target")
		probeAllPods("after upgrade to target image (same pods)")
	})
})

// curl flags used for every probe:
//
//	-s -o /dev/null -w '%{http_code}'  : quiet, only print the HTTP code
//	--max-time 5                       : per-request timeout
//	--no-keepalive + Connection: close : explicit fresh-TCP-5-tuple intent.
//	  Each PodExec already spawns a fresh curl process (no reuse across
//	  iterations), but these flags survive any future change that batches
//	  probes into a single curl invocation.
var curlNewConnFlags = []string{
	"-s", "-o", "/dev/null",
	"-w", "%{http_code}",
	"--max-time", "5",
	"--no-keepalive",
	"-H", "Connection: close",
}

func probeServiceFreshConn(pod v1.Pod, serviceIP string) {
	for i := 0; i < 5; i++ {
		cmd := append([]string{"curl"}, curlNewConnFlags...)
		cmd = append(cmd, fmt.Sprintf("http://%s:80", serviceIP))
		stdout, stderr, perr := f.K8sResourceManagers.PodManager().PodExec(pod.Namespace, pod.Name, cmd)
		fmt.Fprintf(GinkgoWriter, "service [%s] attempt %d: stdout=%s stderr=%s\n", pod.Name, i, stdout, stderr)
		Expect(perr).ToNot(HaveOccurred(), "service connectivity failed from pod %s", pod.Name)
	}
}

func probeExternalFreshConn(pod v1.Pod) {
	for i := 0; i < 5; i++ {
		cmd := append([]string{"curl"}, curlNewConnFlags...)
		cmd = append(cmd, "http://checkip.amazonaws.com")
		stdout, stderr, perr := f.K8sResourceManagers.PodManager().PodExec(pod.Namespace, pod.Name, cmd)
		fmt.Fprintf(GinkgoWriter, "external [%s] attempt %d: stdout=%s stderr=%s\n", pod.Name, i, stdout, stderr)
		Expect(perr).ToNot(HaveOccurred(), "external connectivity failed from pod %s", pod.Name)
	}
}

func setContainerImage(containers []v1.Container, name, image string) {
	for i := range containers {
		if containers[i].Name == name {
			containers[i].Image = image
			return
		}
	}
	Fail(fmt.Sprintf("container %q not found in daemonset", name))
}

// dumpOnNodeState writes nft + iptables state to the Ginkgo writer so a
// failing run shows whether residual `aws-cni` rules are present, what the
// active iptables prerouting chain looks like, etc. Diagnostic only — the
// expected post-rollback state legitimately contains stale nft rules.
// Reuses checkNodeShellPlugin/execNodeShell from pod_traffic_test.go.
func dumpOnNodeState(nodeName, when string) {
	if err := checkNodeShellPlugin(); err != nil {
		fmt.Fprintf(GinkgoWriter, "[%s] node-shell unavailable (%v); skipping on-node state dump\n", when, err)
		return
	}
	if out, ferr := execNodeShell(nodeName, "nft list table ip aws-cni 2>&1 || true"); ferr == nil {
		fmt.Fprintf(GinkgoWriter, "[%s] nft list table ip aws-cni:\n%s\n", when, string(out))
	}
	if out, ferr := execNodeShell(nodeName, "iptables -t nat -S | grep -E 'AWS|CONNMARK' || true"); ferr == nil {
		fmt.Fprintf(GinkgoWriter, "[%s] iptables nat AWS rules:\n%s\n", when, string(out))
	}
}
