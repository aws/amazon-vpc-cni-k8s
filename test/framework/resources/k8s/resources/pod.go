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

package resources

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/wait"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type PodManager interface {
	PodExec(namespace string, name string, command []string) (string, string, error)
	PodLogs(namespace string, name string) (string, error)
	GetPodsWithLabelSelector(labelKey string, labelVal string) (v1.PodList, error)
	GetPodsWithLabelSelectorMap(labels map[string]string) (v1.PodList, error)
	GetPod(podNamespace string, podName string) (*v1.Pod, error)
	CreateAndWaitTillRunning(pod *v1.Pod) (*v1.Pod, error)
	CreateAndWaitTillPodCompleted(pod *v1.Pod) (*v1.Pod, error)
	DeleteAndWaitTillPodDeleted(pod *v1.Pod) error

	WaitUntilPodRunning(ctx context.Context, pod *v1.Pod) error
	WaitUntilPodDeleted(ctx context.Context, pod *v1.Pod) error
}

type defaultPodManager struct {
	k8sClient client.Client
	// client-go Clientset is used instead of controller-runtime Client to access subresources, such as logs and exec
	k8sClientset *kubernetes.Clientset
	k8sSchema    *runtime.Scheme
	config       *rest.Config
}

func NewDefaultPodManager(k8sClient client.Client, k8sClientset *kubernetes.Clientset, k8sSchema *runtime.Scheme, config *rest.Config) PodManager {
	return &defaultPodManager{
		k8sClient:    k8sClient,
		k8sClientset: k8sClientset,
		k8sSchema:    k8sSchema,
		config:       config,
	}
}

func (d *defaultPodManager) CreateAndWaitTillRunning(pod *v1.Pod) (*v1.Pod, error) {
	err := d.k8sClient.Create(context.Background(), pod)
	if err != nil {
		return nil, fmt.Errorf("failed to create pod: %v", err)
	}
	// Allow the cache to sync
	time.Sleep(utils.PollIntervalShort)

	observedPod := &v1.Pod{}
	err = wait.PollImmediate(utils.PollIntervalShort, time.Second*120, func() (done bool, err error) {
		err = d.k8sClient.Get(context.Background(), utils.NamespacedName(pod), observedPod)
		if err != nil {
			return true, err
		}
		if observedPod.Status.Phase == v1.PodRunning {
			return true, nil
		}
		return false, nil
	})

	return observedPod, err
}

func (d *defaultPodManager) GetPod(podNamespace string, podName string) (*v1.Pod, error) {
	pod := &v1.Pod{}
	return pod, d.k8sClient.Get(context.Background(),
		types.NamespacedName{Name: podName, Namespace: podNamespace}, pod)
}

func (d *defaultPodManager) CreateAndWaitTillPodCompleted(pod *v1.Pod) (*v1.Pod, error) {
	err := d.k8sClient.Create(context.Background(), pod)
	if err != nil {
		return nil, fmt.Errorf("failed to create pod: %v", err)
	}
	// Allow the cache to sync
	time.Sleep(utils.PollIntervalShort)

	observedPod := &v1.Pod{}
	err = wait.PollImmediate(utils.PollIntervalShort, time.Second*120, func() (done bool, err error) {
		err = d.k8sClient.Get(context.Background(), utils.NamespacedName(pod), observedPod)
		if err != nil {
			return true, err
		}
		if observedPod.Status.Phase == v1.PodSucceeded {
			return true, nil
		} else if observedPod.Status.Phase == v1.PodFailed {
			return false, fmt.Errorf("pod failed to run")
		}
		return false, nil
	})

	return observedPod, err
}

func (d *defaultPodManager) DeleteAndWaitTillPodDeleted(pod *v1.Pod) error {
	err := d.k8sClient.Delete(context.Background(), pod)
	if err != nil {
		return err
	}
	observedPod := &v1.Pod{}
	return wait.PollImmediate(utils.PollIntervalShort, time.Second*120, func() (done bool, err error) {
		err = d.k8sClient.Get(context.Background(), utils.NamespacedName(pod), observedPod)
		if err != nil {
			if errors.IsNotFound(err) {
				return true, nil
			}
			return true, err
		}
		return false, nil
	})
}

func (d *defaultPodManager) WaitUntilPodRunning(ctx context.Context, pod *v1.Pod) error {
	observedPod := &v1.Pod{}
	return wait.PollImmediateUntil(utils.PollIntervalShort, func() (done bool, err error) {
		if err := d.k8sClient.Get(ctx, utils.NamespacedName(pod), observedPod); err != nil {
			return false, err
		}
		if observedPod.Status.Phase == v1.PodRunning {
			return true, nil
		}
		return false, nil
	}, ctx.Done())
}

func (d *defaultPodManager) WaitUntilPodDeleted(ctx context.Context, pod *v1.Pod) error {
	observedPod := &v1.Pod{}
	return wait.PollImmediateUntil(utils.PollIntervalShort, func() (bool, error) {
		if err := d.k8sClient.Get(ctx, utils.NamespacedName(pod), observedPod); err != nil {
			if errors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}
		return false, nil
	}, ctx.Done())
}

// Use K8SClientset to return pod logs as a string
func (d *defaultPodManager) PodLogs(namespace string, name string) (string, error) {
	podLogOpts := v1.PodLogOptions{}
	req := d.k8sClientset.CoreV1().Pods(namespace).GetLogs(name, &podLogOpts)

	podLogs, err := req.Stream(context.Background())
	defer podLogs.Close()

	buf := new(bytes.Buffer)
	buf.ReadFrom(podLogs)
	return string(buf.Bytes()), err
}

func (d *defaultPodManager) PodExec(namespace string, name string, command []string) (string, string, error) {
	execOptions := &v1.PodExecOptions{
		Stdout:  true,
		Stderr:  true,
		Command: command,
	}

	req := d.k8sClientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(name).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(execOptions, runtime.NewParameterCodec(d.k8sSchema))

	exec, err := remotecommand.NewSPDYExecutor(d.config, http.MethodPost, req.URL())
	if err != nil {
		return "", "", err
	}

	var stdout, stderr bytes.Buffer
	err = exec.Stream(remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})
	return stdout.String(), stderr.String(), err
}

func (d *defaultPodManager) GetPodsWithLabelSelector(labelKey string, labelVal string) (v1.PodList, error) {
	ctx := context.Background()
	podList := v1.PodList{}
	err := d.k8sClient.List(ctx, &podList, client.MatchingLabels{
		labelKey: labelVal,
	})
	return podList, err
}

func (d *defaultPodManager) GetPodsWithLabelSelectorMap(labels map[string]string) (v1.PodList, error) {
	ctx := context.Background()
	podList := v1.PodList{}
	err := d.k8sClient.List(ctx, &podList, client.MatchingLabels(labels))
	return podList, err
}
