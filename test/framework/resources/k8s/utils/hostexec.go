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

package utils

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	. "github.com/onsi/ginkgo/v2"
	"github.com/samber/lo"
	appsV1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// The host-exec DaemonSet keeps one long-lived privileged pod per test node so
// tests can run host commands through the exec subresource, which returns the
// full stdout. kubectl run -i can attach after a short command has exited.
const (
	hostExecDaemonSetName = "host-exec"
	hostExecLabelKey      = "app"
	hostExecLabelVal      = "vpc-cni-host-exec"
	hostExecContainerName = "host-exec"

	// hostExecTimeout bounds a single command run on a node's host.
	hostExecTimeout = 2 * time.Minute
	// hostExecRetryFor and hostExecRetryInterval shape ExecOnHostWithRetries.
	hostExecRetryFor      = 5 * time.Minute
	hostExecRetryInterval = 10 * time.Second
)

// EnsureHostExecDaemonSet creates the host-exec DaemonSet in the test
// namespace if needed and waits until it is ready. Idempotent, so specs that
// run host commands call it from their setup hooks instead of BeforeSuite.
// Deleting the test namespace in AfterSuite removes it.
func EnsureHostExecDaemonSet(f *framework.Framework) error {
	dsManager := f.K8sResourceManagers.DaemonSetManager()
	_, err := dsManager.CreateAndWaitTillDaemonSetIsReady(newHostExecDaemonSet(f), utils.DefaultDeploymentReadyTimeout)
	if k8sErrors.IsAlreadyExists(err) {
		return dsManager.CheckIfDaemonSetIsReady(utils.DefaultTestNamespace, hostExecDaemonSetName)
	}
	if err != nil {
		return fmt.Errorf("creating host-exec daemonset: %w", err)
	}
	return nil
}

// ExecOnHost runs command with the host's bash in the host namespaces of
// nodeName and returns its stdout, bounded by hostExecTimeout. A non-zero exit
// is an error carrying the exit code and stderr. Requires bash on the host
// (AL2, AL2023; not Bottlerocket) and the host-exec DaemonSet; see
// EnsureHostExecDaemonSet.
func ExecOnHost(f *framework.Framework, nodeName string, command string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), hostExecTimeout)
	defer cancel()

	pods := &v1.PodList{}
	// The cached client has no spec.nodeName index, so filter by node in memory.
	err := f.K8sClient.List(ctx, pods, client.InNamespace(utils.DefaultTestNamespace),
		client.MatchingLabels{hostExecLabelKey: hostExecLabelVal})
	if err != nil {
		return "", fmt.Errorf("listing host-exec pods: %w", err)
	}
	pod, found := lo.Find(pods.Items, func(p v1.Pod) bool {
		return p.Spec.NodeName == nodeName && p.Status.Phase == v1.PodRunning && p.DeletionTimestamp == nil
	})
	if !found {
		return "", fmt.Errorf("no running host-exec pod on node %s", nodeName)
	}
	// Enter PID 1's mount, UTS, IPC, network and PID namespaces.
	stdout, stderr, err := f.K8sResourceManagers.PodManager().PodExecInContainerWithContext(ctx,
		pod.Namespace, pod.Name, hostExecContainerName,
		[]string{"nsenter", "-t", "1", "-m", "-u", "-i", "-n", "-p", "--", "bash", "-c", command})
	if err != nil {
		return stdout, fmt.Errorf("host exec on node %s: %w (stderr: %s)", nodeName, err, stderr)
	}
	return stdout, nil
}

// ExecOnHostWithRetries is ExecOnHost plus retries on any failure for up to
// hostExecRetryFor. Commands must be idempotent and expected to succeed; one
// that cannot succeed costs the full retry window.
func ExecOnHostWithRetries(f *framework.Framework, nodeName string, command string) (string, error) {
	deadline := time.Now().Add(hostExecRetryFor)
	for {
		output, err := ExecOnHost(f, nodeName, command)
		if err == nil || time.Now().After(deadline) {
			return output, err
		}
		fmt.Fprintf(GinkgoWriter, "host exec on %s failed, retrying in %s: %v (output: %s)\n",
			nodeName, hostExecRetryInterval, err, output)
		time.Sleep(hostExecRetryInterval)
	}
}

// newHostExecDaemonSet builds the host-exec DaemonSet, scoped to the
// --ng-name-label Linux nodes so unrelated nodes neither block readiness nor
// get a privileged pod. It tolerates all taints. busybox supplies nsenter and
// sleep; commands run with the host's binaries after nsenter.
func newHostExecDaemonSet(f *framework.Framework) *appsV1.DaemonSet {
	container := manifest.NewBusyBoxContainerBuilder(f.Options.TestImageRegistry).
		Name(hostExecContainerName).
		Command([]string{"sleep", "infinity"}).
		Build()
	container.SecurityContext = &v1.SecurityContext{Privileged: new(true)}
	ds := manifest.NewDefaultDaemonsetBuilder().
		Name(hostExecDaemonSetName).
		Labels(map[string]string{hostExecLabelKey: hostExecLabelVal}).
		Container(container).
		NodeSelector(f.Options.NgNameLabelKey, f.Options.NgNameLabelVal).
		HostNetwork(true).
		TerminationGracePeriod(0).
		Build()
	ds.Spec.Template.Spec.HostPID = true
	ds.Spec.Template.Spec.Tolerations = []v1.Toleration{{Operator: v1.TolerationOpExists}}
	return ds
}
