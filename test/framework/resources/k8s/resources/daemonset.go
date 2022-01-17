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
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type DaemonSetManager interface {
	MultusDaemonSetManager
	GetDaemonSet(string, string) (*v1.DaemonSet, error)
	UpdateAndWaitTillDaemonSetReady(*v1.DaemonSet, *v1.DaemonSet) (*v1.DaemonSet, error)
	DeleteDaemonSet(string, string) error
}

type MultusDaemonSetManager interface {
	CreateAndWaitTillMultusDaemonSetReady(string, string, string) error
}

type defaultDaemonSetManager struct {
	k8sClient client.DelegatingClient
}

func NewDefaultDaemonSetManager(k8sClient client.DelegatingClient) DaemonSetManager {
	return &defaultDaemonSetManager{k8sClient: k8sClient}
}

func (d *defaultDaemonSetManager) CreateAndWaitTillMultusDaemonSetReady(name string, namespace string, multusImage string) error {
	daemonSet := &v1.DaemonSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "DaemonSet",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"tier": "node",
				"app":  "multus",
				"name": "multus",
			},
		},
		Spec: v1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"name": "multus",
				},
			},
			UpdateStrategy: v1.DaemonSetUpdateStrategy{
				Type: v1.RollingUpdateDaemonSetStrategyType,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"tier": "node",
						"app":  "multus",
						"name": "multus",
					},
				},
				Spec: corev1.PodSpec{
					Affinity: &corev1.Affinity{
						NodeAffinity: &corev1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
								NodeSelectorTerms: []corev1.NodeSelectorTerm{
									{
										MatchExpressions: []corev1.NodeSelectorRequirement{
											{
												Key:      "kubernetes.io/os",
												Operator: corev1.NodeSelectorOpIn,
												Values: []string{
													"linux",
												},
											},
											{
												Key:      "eks.amazonaws.com/compute-type",
												Operator: corev1.NodeSelectorOpNotIn,
												Values: []string{
													"fargate",
												},
											},
										},
									},
								},
							},
						},
					},
					HostNetwork: true,
					Tolerations: []corev1.Toleration{
						{
							Operator: corev1.TolerationOpExists,
							Effect:   corev1.TaintEffectNoSchedule,
						},
					},
					ServiceAccountName: "multus",
					Containers: []corev1.Container{
						manifest.NewMultusContainer(namespace, multusImage),
					},
					TerminationGracePeriodSeconds: &[]int64{10}[0],
					Volumes: []corev1.Volume{
						{
							Name: "cni",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/etc/cni/net.d",
								},
							},
						},
						{
							Name: "cnibin",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/opt/cni/bin",
								},
							},
						},
					},
				},
			},
		},
	}
	if err := d.k8sClient.Create(context.Background(), daemonSet); err != nil {
		return err
	}
	return waitTillDaemonSetReady(name, namespace, d)
}

func (d *defaultDaemonSetManager) DeleteDaemonSet(name string, namespace string) error {
	ds, err := d.GetDaemonSet(namespace, name)
	if err != nil {
		return err
	}
	err = d.k8sClient.Delete(context.Background(), ds)
	if err != nil {
		return err
	}
	return waitTillDaemonSetIsDeleted(name, namespace, d)
}

// Private method to check if the Daemonset is Deleted
// It takes some time so we wait until Daemonset is cleaned up
func waitTillDaemonSetIsDeleted(name string, namespace string, d *defaultDaemonSetManager) error {
	attempts := 0
	for {
		_, err := d.GetDaemonSet(namespace, name)
		attempts += 1
		// If we get DaemonSet not found error
		// means DS is deleted and we return
		notFoundError := fmt.Sprintf("DaemonSet.apps \"%v\" not found", name)
		if err != nil && strings.Contains(err.Error(), notFoundError) {
			return nil
		}
		if attempts > 5 {
			break
		}
		time.Sleep(10 * time.Second)
	}
	return errors.New("DaemonSet taking too long to delete")
}

func (d *defaultDaemonSetManager) GetDaemonSet(namespace string, name string) (*v1.DaemonSet, error) {
	ctx := context.Background()
	daemonSet := &v1.DaemonSet{}
	err := d.k8sClient.Get(ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}, daemonSet)
	return daemonSet, err
}

func (d *defaultDaemonSetManager) UpdateAndWaitTillDaemonSetReady(old *v1.DaemonSet, new *v1.DaemonSet) (*v1.DaemonSet, error) {
	ctx := context.Background()
	err := d.k8sClient.Patch(ctx, new, client.MergeFrom(old))
	if err != nil {
		return nil, err
	}

	observed := &v1.DaemonSet{}
	return observed, wait.PollImmediateUntil(utils.PollIntervalShort, func() (bool, error) {
		if err := d.k8sClient.Get(ctx, utils.NamespacedName(new), observed); err != nil {
			return false, err
		}
		if observed.Status.NumberReady == (new.Status.DesiredNumberScheduled) &&
			observed.Status.NumberAvailable == (new.Status.DesiredNumberScheduled) &&
			observed.Status.UpdatedNumberScheduled == (new.Status.DesiredNumberScheduled) &&
			observed.Status.ObservedGeneration >= new.Generation {
			return true, nil
		}
		return false, nil
	}, ctx.Done())
}

func waitTillDaemonSetReady(name string, namespace string, d *defaultDaemonSetManager) error {
	ds, err := d.GetDaemonSet(namespace, name)
	if err != nil {
		return err
	}
	ctx := context.Background()
	return wait.PollImmediateUntil(utils.PollIntervalMedium, func() (bool, error) {
		if err := d.k8sClient.Get(ctx, utils.NamespacedName(ds), ds); err != nil {
			return false, err
		}
		// Need to ensure the DesiredNumberScheduled is not 0 as it may happen if the DS is still being deleted from previous run
		if ds.Status.DesiredNumberScheduled != 0 && ds.Status.NumberReady == ds.Status.DesiredNumberScheduled {
			return true, nil
		}
		return false, nil
	}, ctx.Done())

}
