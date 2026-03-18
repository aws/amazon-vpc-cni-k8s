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

package core_plugins

import (
	"fmt"
	"strings"

	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	awsNodeLabelKey = "k8s-app"
	cniBinPath      = "/host/opt/cni/bin"
)

// expectedCorePlugins lists all containernetworking/plugins binaries shipped
// in the amazon-k8s-cni-init image via core-plugins/binaries/.
var expectedCorePlugins = []string{
	"bandwidth",
	"bridge",
	"dhcp",
	"dummy",
	"firewall",
	"host-device",
	"host-local",
	"ipvlan",
	"loopback",
	"macvlan",
	"portmap",
	"ptp",
	"sbr",
	"static",
	"tap",
	"tuning",
	"vlan",
	"vrf",
}

var _ = Describe("CNI init container core plugins", func() {
	It("all core plugins should be present with executable permissions", func() {
		By("getting an aws-node pod")
		podList, err := f.K8sResourceManagers.PodManager().
			GetPodsWithLabelSelector(awsNodeLabelKey, utils.AwsNodeName)
		Expect(err).ToNot(HaveOccurred())
		Expect(podList.Items).ToNot(BeEmpty(), "expected at least one aws-node pod")

		pod := podList.Items[0]
		By(fmt.Sprintf("checking core plugin executability on node %s via pod %s", pod.Spec.NodeName, pod.Name))

		var failures []string
		for _, plugin := range expectedCorePlugins {
			p := fmt.Sprintf("%s/%s", cniBinPath, plugin)
			// Invoke the binary directly; CNI plugins print version info on
			// stdin-EOF and exit 0 or 1, but the exec itself will fail with a
			// clear error if the file is missing or not executable.
			cmd := []string{p}
			_, _, err := f.K8sResourceManagers.PodManager().
				PodExecWithContainer(pod.Namespace, pod.Name, "aws-node", cmd)
			if err != nil {
				errMsg := err.Error()
				if strings.Contains(errMsg, "not found") || strings.Contains(errMsg, "no such file") {
					failures = append(failures, fmt.Sprintf("MISSING: %s", plugin))
				} else if strings.Contains(errMsg, "permission denied") {
					failures = append(failures, fmt.Sprintf("NOT_EXEC: %s", plugin))
				}
				// Other errors (e.g. non-zero exit) are expected — the binary
				// exists and is executable but needs CNI_COMMAND env, so ignore.
			}
		}
		Expect(failures).To(BeEmpty(),
			fmt.Sprintf("core plugin issues found:\n%s", strings.Join(failures, "\n")))
	})

	Context("when ENABLE_BANDWIDTH_PLUGIN is enabled", func() {
		AfterEach(func() {
			By("disabling ENABLE_BANDWIDTH_PLUGIN")
			k8sUtils.RemoveVarFromDaemonSetAndWaitTillUpdated(f,
				utils.AwsNodeName, utils.AwsNodeNamespace, utils.AwsNodeName,
				map[string]struct{}{
					"ENABLE_BANDWIDTH_PLUGIN": {},
				})
		})

		It("pods should reach Running state", func() {
			By("enabling ENABLE_BANDWIDTH_PLUGIN on aws-node daemonset")
			k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f,
				utils.AwsNodeName, utils.AwsNodeNamespace, utils.AwsNodeName,
				map[string]string{
					"ENABLE_BANDWIDTH_PLUGIN": "true",
				})

			By("creating a deployment to verify pods can start")
			deployment := manifest.NewBusyBoxDeploymentBuilder(f.Options.TestImageRegistry).
				Replicas(3).
				PodLabel("app", "bandwidth-test").
				Build()

			deployment, err := f.K8sResourceManagers.DeploymentManager().
				CreateAndWaitTillDeploymentIsReady(deployment, utils.DefaultDeploymentReadyTimeout)
			Expect(err).ToNot(HaveOccurred(),
				"pods failed to reach Running state with ENABLE_BANDWIDTH_PLUGIN=true")

			By("deleting the test deployment")
			err = f.K8sResourceManagers.DeploymentManager().
				DeleteAndWaitTillDeploymentIsDeleted(deployment)
			Expect(err).ToNot(HaveOccurred())
		})
	})
})
