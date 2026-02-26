// Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//     http://aws.amazon.com/apache2.0/

package cni

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/integration/common"
	"github.com/samber/lo"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsV1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
)

const (
	kubeProxyConfigMapName = "kube-proxy-config"
	kubeProxyNamespace     = "kube-system"
)

// Tests VPC CNI SNAT/connmark rules work with all kube-proxy modes
var _ = Describe("test SNAT with kube-proxy modes", func() {
	var (
		deployment         *appsV1.Deployment
		service            *v1.Service
		interfaceToPodList common.InterfaceTypeToPodList
		err                error
	)

	BeforeEach(func() {
		Expect(checkNodeShellPlugin()).To(BeNil())
		serverContainer := manifest.NewBusyBoxContainerBuilder(f.Options.TestImageRegistry).
			Image(utils.GetTestImage(f.Options.TestImageRegistry, utils.NginxImage)).
			Command(nil).
			Port(v1.ContainerPort{ContainerPort: 80, Protocol: "TCP"}).
			Build()

		deployment = manifest.NewDefaultDeploymentBuilder().
			Name("snat-test-server").
			Container(serverContainer).
			Replicas(maxIPPerInterface*2).
			NodeName(primaryNode.Name).
			PodLabel("app", "snat-test").
			Build()

		deployment, err = f.K8sResourceManagers.DeploymentManager().
			CreateAndWaitTillDeploymentIsReady(deployment, utils.DefaultDeploymentReadyTimeout)
		Expect(err).ToNot(HaveOccurred())

		interfaceToPodList = common.GetPodsOnPrimaryAndSecondaryInterface(primaryNode, "app", "snat-test", f)
		Expect(len(interfaceToPodList.PodsOnPrimaryENI)).Should(BeNumerically(">=", 1))
		Expect(len(interfaceToPodList.PodsOnSecondaryENI)).Should(BeNumerically(">=", 1))

		service = manifest.NewHTTPService().
			ServiceType(v1.ServiceTypeClusterIP).
			Name("snat-test-svc").
			Selector("app", "snat-test").
			Build()

		service, err = f.K8sResourceManagers.ServiceManager().CreateService(context.Background(), service)
		Expect(err).ToNot(HaveOccurred())

		time.Sleep(utils.PollIntervalLong)
	})

	AfterEach(func() {
		if service != nil {
			f.K8sResourceManagers.ServiceManager().DeleteAndWaitTillServiceDeleted(context.Background(), service)
		}
		if deployment != nil {
			f.K8sResourceManagers.DeploymentManager().DeleteAndWaitTillDeploymentIsDeleted(deployment)
		}
	})

	DescribeTable("verifies VPC CNI is kube-proxy mode agnostic",
		func(mode string) {

			ver, err := f.DiscoveryClient.ServerVersion()
			Expect(err).ToNot(HaveOccurred())
			semVer, err := semver.NewVersion(ver.String())
			Expect(err).ToNot(HaveOccurred())
			if semVer.Minor() <= 32 && mode == "nftables" {
				return
			}

			originalMode, err := getKubeProxyMode()
			Expect(err).ToNot(HaveOccurred())

			DeferCleanup(func() {
				By(fmt.Sprintf("restoring kube-proxy mode to %s", originalMode))
				Expect(setKubeProxyMode(originalMode)).To(Succeed())
			})

			By(fmt.Sprintf("switching kube-proxy to %s mode", mode))
			Expect(setKubeProxyMode(mode)).To(Succeed())

			By("detecting iptables backend on node")
			backend := detectIptablesBackend(primaryNode.Name)
			fmt.Fprintf(GinkgoWriter, "detected iptables backend: %s\n", backend)

			By("verifying CNI connmark rules exist")
			verifyConnmarkRules(primaryNode.Name, backend)

			By("testing pod on primary ENI")
			primaryPod := interfaceToPodList.PodsOnPrimaryENI[0]
			secondaryPod := interfaceToPodList.PodsOnSecondaryENI[0]
			verifyServiceConnectivity(primaryPod, service.Spec.ClusterIP)
			verifyAPIServerConnectivity(primaryPod)
			verifyExternalConnectivity(primaryPod)

			By("testing pod on secondary ENI")
			verifyServiceConnectivity(secondaryPod, service.Spec.ClusterIP)
			verifyAPIServerConnectivity(secondaryPod)
			verifyExternalConnectivity(secondaryPod)

		},
		Entry("iptables", "iptables"),
		Entry("nftables", "nftables"),
		Entry("ipvs", "ipvs"),
	)
})

func verifyServiceConnectivity(pod v1.Pod, serviceIP string) {
	cmd := []string{"curl", "-s", "-o", "/dev/null", "-w", "%{http_code}", "--max-time", "5", fmt.Sprintf("http://%s:80", serviceIP)}
	stdout, stderr, err := f.K8sResourceManagers.PodManager().PodExec(pod.Namespace, pod.Name, cmd)
	fmt.Fprintf(GinkgoWriter, "service [%s]: stdout=%s stderr=%s\n", pod.Name, stdout, stderr)
	Expect(err).ToNot(HaveOccurred(), "service connectivity failed from pod %s", pod.Name)
}

func verifyAPIServerConnectivity(pod v1.Pod) {
	cmd := []string{"curl", "-s", "-o", "/dev/null", "-w", "%{http_code}", "--max-time", "5", "-k", "https://kubernetes.default.svc:443/healthz"}
	stdout, stderr, err := f.K8sResourceManagers.PodManager().PodExec(pod.Namespace, pod.Name, cmd)
	fmt.Fprintf(GinkgoWriter, "api-server [%s]: stdout=%s stderr=%s\n", pod.Name, stdout, stderr)
	Expect(err).ToNot(HaveOccurred(), "API server connectivity failed from pod %s", pod.Name)
}

func verifyExternalConnectivity(pod v1.Pod) {
	cmd := []string{"curl", "-s", "-o", "/dev/null", "-w", "%{http_code}", "--max-time", "5", "http://checkip.amazonaws.com"}
	stdout, stderr, err := f.K8sResourceManagers.PodManager().PodExec(pod.Namespace, pod.Name, cmd)
	fmt.Fprintf(GinkgoWriter, "external [%s]: stdout=%s stderr=%s\n", pod.Name, stdout, stderr)
	Expect(err).ToNot(HaveOccurred(), "external connectivity failed from pod %s", pod.Name)
}

func getKubeProxyMode() (string, error) {
	cm, err := f.K8sResourceManagers.ConfigMapManager().GetConfigMap(kubeProxyNamespace, kubeProxyConfigMapName)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(cm.Data["config"], "\n") {
		if strings.Contains(line, "mode:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1]), nil
			}
		}
	}
	return "iptables", nil
}

func setKubeProxyMode(mode string) error {
	cm, err := f.K8sResourceManagers.ConfigMapManager().GetConfigMap(kubeProxyNamespace, kubeProxyConfigMapName)
	if err != nil {
		return err
	}

	newCm := cm.DeepCopy()
	var lines []string
	modeSet := false
	for _, line := range strings.Split(newCm.Data["config"], "\n") {
		if strings.Contains(line, "mode:") {
			lines = append(lines, fmt.Sprintf("mode: %s", mode))
			modeSet = true
		} else {
			lines = append(lines, line)
		}
	}
	if !modeSet {
		lines = append(lines, fmt.Sprintf("mode: %s", mode))
	}
	newCm.Data["config"] = strings.Join(lines, "\n")

	if err := f.K8sResourceManagers.ConfigMapManager().UpdateConfigMap(cm, newCm); err != nil {
		return err
	}
	return restartKubeProxyPods()
}

func restartKubeProxyPods() error {
	pods, err := f.K8sResourceManagers.PodManager().GetPodsWithLabelSelector("k8s-app", "kube-proxy")
	if err != nil {
		return err
	}
	for _, pod := range pods.Items {
		f.K8sResourceManagers.PodManager().DeleteAndWaitTillPodDeleted(&pod)
	}
	time.Sleep(30 * time.Second)
	return nil
}

// detectIptablesBackend determines if the node uses iptables-legacy or nftables
func detectIptablesBackend(nodeName string) string {
	pods, err := f.K8sResourceManagers.PodManager().GetPodsWithLabelSelector("k8s-app", "aws-node")
	if err != nil {
		fmt.Fprintf(GinkgoWriter, "Failed to find aws-node pod: %v\n", err)
		return ""
	}
	pod, found := lo.Find(pods.Items, func(p v1.Pod) bool {
		return p.Spec.NodeName == nodeName
	})
	if !found {
		fmt.Fprintf(GinkgoWriter, "Failed to find aws-node pod on node %s\n", nodeName)
		return ""
	}
	stdout, _, err := f.K8sResourceManagers.PodManager().PodExecInContainer(pod.Namespace, pod.Name, "aws-node", []string{"iptables", "--version"})
	if err != nil {
		fmt.Fprintf(GinkgoWriter, "Failed to run iptables --version: %v\n", err)
		return ""
	}
	if strings.Contains(stdout, "nf_tables") {
		return "nftables"
	} else if strings.Contains(stdout, "legacy") {
		return "legacy"
	}
	return ""
}

// verifyConnmarkRules checks that CNI connmark rules exist ONLY in the appropriate backend
func verifyConnmarkRules(nodeName, backend string) {
	if backend == "nftables" {
		out, err := execNodeShell(nodeName, "nft list table ip aws-cni")
		fmt.Fprintf(GinkgoWriter, "nftables rules:\n%s\n", string(out))
		Expect(err).ToNot(HaveOccurred())
		Expect(string(out)).To(ContainSubstring("chain nat-prerouting"))
		Expect(string(out)).To(ContainSubstring("chain snat-mark"))

		out, _ = execNodeShell(nodeName, "iptables-legacy -t nat -L PREROUTING -n")
		Expect(string(out)).ToNot(ContainSubstring("AWS-CONNMARK"))
	} else {
		out, err := execNodeShell(nodeName, "iptables-legacy -t nat -L PREROUTING -n")
		fmt.Fprintf(GinkgoWriter, "iptables-legacy:\n%s\n", string(out))
		Expect(err).ToNot(HaveOccurred())
		Expect(string(out)).To(ContainSubstring("AWS-CONNMARK"))

		_, err = execNodeShell(nodeName, "nft list table ip aws-cni")
		Expect(err).To(HaveOccurred())
	}
}
