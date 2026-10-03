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

	"github.com/aws/amazon-vpc-cni-k8s/test/framework"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	"github.com/samber/lo"
	appsV1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// The host-exec DaemonSet keeps one long-lived privileged pod on every Linux
// node so tests can run host commands through the API exec subresource. Exec
// into a running pod returns the command's full stdout, unlike kubectl run -i,
// which can attach after a short command has exited.
const (
	hostExecDaemonSetName = "host-exec"
	hostExecLabelKey      = "app"
	hostExecLabelVal      = "vpc-cni-host-exec"
	hostExecContainerName = "host-exec"
)

// EnsureHostExecDaemonSet creates the host-exec DaemonSet in the default test
// namespace unless it already exists, then waits until it is ready. It lives
// in the test namespace, so deleting that namespace also removes it.
func EnsureHostExecDaemonSet(f *framework.Framework) error {
	dsManager := f.K8sResourceManagers.DaemonSetManager()
	_, err := dsManager.GetDaemonSet(utils.DefaultTestNamespace, hostExecDaemonSetName)
	if err == nil {
		return dsManager.CheckIfDaemonSetIsReady(utils.DefaultTestNamespace, hostExecDaemonSetName)
	}
	if !k8sErrors.IsNotFound(err) {
		return fmt.Errorf("getting host-exec daemonset: %w", err)
	}
	_, err = dsManager.CreateAndWaitTillDaemonSetIsReady(newHostExecDaemonSet(f.Options.TestImageRegistry),
		utils.DefaultDeploymentReadyTimeout)
	if err != nil {
		return fmt.Errorf("creating host-exec daemonset: %w", err)
	}
	return nil
}

// DeleteHostExecDaemonSet deletes the host-exec DaemonSet and waits until it
// is gone. A missing DaemonSet is not an error.
func DeleteHostExecDaemonSet(f *framework.Framework) error {
	ds := &appsV1.DaemonSet{ObjectMeta: metav1.ObjectMeta{
		Name:      hostExecDaemonSetName,
		Namespace: utils.DefaultTestNamespace,
	}}
	return f.K8sResourceManagers.DaemonSetManager().DeleteAndWaitTillDaemonSetIsDeleted(ds,
		utils.DefaultDeploymentReadyTimeout)
}

// HostExec runs command with bash in the host namespaces of nodeName and
// returns its stdout. A non-zero exit status is returned as an error carrying
// the exit code and stderr. The host-exec DaemonSet must already be running;
// see EnsureHostExecDaemonSet.
func HostExec(ctx context.Context, f *framework.Framework, nodeName string, command string) (string, error) {
	pods, err := f.K8sResourceManagers.PodManager().GetPodsWithLabelSelector(hostExecLabelKey, hostExecLabelVal)
	if err != nil {
		return "", fmt.Errorf("listing host-exec pods: %w", err)
	}
	pod, found := lo.Find(pods.Items, func(p v1.Pod) bool {
		return p.Namespace == utils.DefaultTestNamespace && p.Spec.NodeName == nodeName &&
			p.Status.Phase == v1.PodRunning && p.DeletionTimestamp == nil
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

// newHostExecDaemonSet builds the host-exec DaemonSet. busybox supplies
// nsenter and sleep; the command itself runs with the host's own binaries once
// nsenter switches to the host mount namespace.
func newHostExecDaemonSet(testImageRegistry string) *appsV1.DaemonSet {
	container := manifest.NewBusyBoxContainerBuilder(testImageRegistry).
		Name(hostExecContainerName).
		Command([]string{"sleep", "infinity"}).
		Privileged().
		Build()
	return manifest.NewDefaultDaemonsetBuilder().
		Name(hostExecDaemonSetName).
		Labels(map[string]string{hostExecLabelKey: hostExecLabelVal}).
		Container(container).
		HostNetwork(true).
		HostPID(true).
		Tolerations([]v1.Toleration{{Operator: v1.TolerationOpExists}}).
		TerminationGracePeriod(0).
		Build()
}
