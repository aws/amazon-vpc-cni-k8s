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

package calico

import (
	"fmt"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

var _ = Describe("test calico with vpc-cni-k8s", func() {

	JustBeforeEach(func() {
		By("creating test namespace")
		f.K8sResourceManagers.NamespaceManager().
			CreateNamespace(utils.DefaultTestNamespace)
	})

	JustAfterEach(func() {
		By("deleting test namespace")
		f.K8sResourceManagers.NamespaceManager().
			DeleteAndWaitTillNamespaceDeleted(utils.DefaultTestNamespace)
	})

	Context("when deny and allow ingress rule network policy is deployed", func() {
		It("it should deny and allow ingress respectively", func() {

			serverPod := DeployNginxServer()
			serverPodIP := serverPod.Status.PodIP

			By("verifying connection to server succeeds when no policy is deployed")
			VerifyClientToServerConnection(serverPodIP, false)

			By("blocking all ingress traffic")
			DeployBlockNetworkPolicy([]v1.PolicyType{v1.PolicyTypeIngress})

			By("verify connection to server fails when block ingress policy is deployed")
			VerifyClientToServerConnection(serverPodIP, true)

			protocol := corev1.ProtocolTCP
			port := intstr.IntOrString{
				IntVal: int32(openPort),
			}

			By("allowing traffic from client-server")
			allowPolicy := manifest.NewDefaultNetworkPolicyBuilder().
				Name("allow-client-to-server").
				PodSelectorMatchLabel(map[string]string{"app": "server"}).
				PolicyTypes([]v1.PolicyType{v1.PolicyTypeIngress}).
				IngressRules([]v1.NetworkPolicyIngressRule{
					{
						Ports: []v1.NetworkPolicyPort{
							{
								Protocol: &protocol,
								Port:     &port,
							},
						},
						From: []v1.NetworkPolicyPeer{
							{
								PodSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{"app": "client"},
								},
							},
						},
					},
				}).Build()

			err = f.K8sResourceManagers.CustomResourceManager().CreateResource(&allowPolicy)
			Expect(err).ToNot(HaveOccurred())

			By("verify connection to server succeeds when allow ingress policy is deployed")
			VerifyClientToServerConnection(serverPodIP, false)
		})
	})
})

func DeployBlockNetworkPolicy(policyTypes []v1.PolicyType) {
	blockPolicy := manifest.NewDefaultNetworkPolicyBuilder().
		Name("block-policy").
		PolicyTypes(policyTypes).
		Build()

	err = f.K8sResourceManagers.CustomResourceManager().CreateResource(&blockPolicy)
	Expect(err).ToNot(HaveOccurred())
}

func VerifyClientToServerConnection(serverPodIP string, shouldError bool) {
	clientContainer := manifest.NewCurlContainer().
		Name("client").
		Command([]string{"curl"}).
		Args([]string{
			"--fail",
			"--connect-timeout",
			"5", // 5 seconds timeout
			fmt.Sprintf("http://%s:%d", serverPodIP, openPort)}).
		Build()

	clientPod := manifest.NewDefaultPodBuilder().
		Name("client").
		Container(clientContainer).
		NodeName(node.Name).
		RestartPolicy(corev1.RestartPolicyNever).
		PodLabel("app", "client").
		Build()

	_, err := f.K8sResourceManagers.PodManager().
		CreateAndWaitTillPodCompleted(clientPod)
	if shouldError {
		Expect(err).To(HaveOccurred())
	} else {
		Expect(err).ToNot(HaveOccurred())
	}

	By("deleting the client pod")
	err = f.K8sResourceManagers.PodManager().
		DeleteAndWaitTillPodDeleted(clientPod)
	Expect(err).ToNot(HaveOccurred())
}

func DeployNginxServer() *corev1.Pod {
	serverContainer := manifest.NewTestHelperContainer().
		Name("server").
		Image("nginx:1.14.2").
		Build()

	serverPod := manifest.NewDefaultPodBuilder().
		Name("server").
		NodeName(node.Name).
		Container(serverContainer).
		PodLabel("app", "server").
		Build()

	By("deploying the server pod")
	serverPod, err = f.K8sResourceManagers.PodManager().
		CreatAndWaitTillRunning(serverPod)
	Expect(err).ToNot(HaveOccurred())

	return serverPod
}
