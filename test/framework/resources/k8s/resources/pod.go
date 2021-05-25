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
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

type PodManager interface {
	PodExec(namespace string, name string, command []string) (string, string, error)
	PodLogs(namespace string, name string) (string, error)
	GetPodsWithLabelSelector(labelKey string, labelVal string) (v1.PodList, error)
	GetPod(podNamespace string, podName string) (*v1.Pod, error)
	CreatAndWaitTillRunning(pod *v1.Pod) (*v1.Pod, error)
	CreateAndWaitTillPodCompleted(pod *v1.Pod) (*v1.Pod, error)
	DeleteAndWaitTillPodDeleted(pod *v1.Pod) error
}

type defaultPodManager struct {
	k8sClient client.DelegatingClient
	k8sSchema *runtime.Scheme
	config    *rest.Config
}

func NewDefaultPodManager(k8sClient client.DelegatingClient, k8sSchema *runtime.Scheme,
	config *rest.Config) PodManager {

	return &defaultPodManager{
		k8sClient: k8sClient,
		k8sSchema: k8sSchema,
		config:    config,
	}
}

func (d *defaultPodManager) CreatAndWaitTillRunning(pod *v1.Pod) (*v1.Pod, error) {
	err := d.k8sClient.Create(context.Background(), pod)
	if err != nil {
		return nil, fmt.Errorf("faield to create pod: %v", err)
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
		return nil, fmt.Errorf("faield to create pod: %v", err)
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

func (d *defaultPodManager) PodLogs(namespace string, name string) (string, error) {
	restClient, err := d.getRestClientForPod(namespace, name)
	if err != nil {
		return "", err
	}

	req := restClient.Get().
		Resource("pods").
		Namespace(namespace).
		Name(name).
		SubResource("log")

	readCloser, err := req.Stream(context.Background())
	if err != nil {
		return "", err
	}
	defer readCloser.Close()
	buf := new(bytes.Buffer)
	buf.ReadFrom(readCloser)

	return string(buf.Bytes()), err
}

func (d *defaultPodManager) PodExec(namespace string, name string, command []string) (string, string, error) {
	restClient, err := d.getRestClientForPod(namespace, name)
	if err != nil {
		return "", "", err
	}

	execOptions := &v1.PodExecOptions{
		Stdout:  true,
		Stderr:  true,
		Command: command,
	}

	restClient.Get()
	req := restClient.Post().
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

func (d *defaultPodManager) getRestClientForPod(namespace string, name string) (rest.Interface, error) {
	pod := &v1.Pod{}
	err := d.k8sClient.Get(context.Background(), types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}, pod)
	if err != nil {
		return nil, err
	}

	gkv, err := apiutil.GVKForObject(pod, d.k8sSchema)
	if err != nil {
		return nil, err
	}
	return apiutil.RESTClientForGVK(gkv, d.config, serializer.NewCodecFactory(d.k8sSchema))
}
