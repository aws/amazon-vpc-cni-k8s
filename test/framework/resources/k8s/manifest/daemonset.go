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

package manifest

import (
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	"github.com/aws/aws-sdk-go/aws"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type DaemonsetBuilder struct {
	namespace              string
	name                   string
	container              corev1.Container
	labels                 map[string]string
	nodeSelector           map[string]string
	terminationGracePeriod int
	hostNetwork            bool
	volume                 []corev1.Volume
	volumeMount            []corev1.VolumeMount
}

func NewDefaultDaemonsetBuilder() *DaemonsetBuilder {
	return &DaemonsetBuilder{
		namespace:              utils.DefaultTestNamespace,
		terminationGracePeriod: 1,
		labels:                 map[string]string{"role": "test"},
		nodeSelector:           map[string]string{"kubernetes.io/os": "linux"},
	}
}

func (d *DaemonsetBuilder) Labels(labels map[string]string) *DaemonsetBuilder {
	d.labels = labels
	return d
}

func (d *DaemonsetBuilder) NodeSelector(labelKey string, labelVal string) *DaemonsetBuilder {
	if labelKey != "" {
		d.nodeSelector[labelKey] = labelVal
	}
	return d
}

func (d *DaemonsetBuilder) Namespace(namespace string) *DaemonsetBuilder {
	d.namespace = namespace
	return d
}

func (d *DaemonsetBuilder) TerminationGracePeriod(tg int) *DaemonsetBuilder {
	d.terminationGracePeriod = tg
	return d
}

func (d *DaemonsetBuilder) Name(name string) *DaemonsetBuilder {
	d.name = name
	return d
}

func (d *DaemonsetBuilder) Container(container corev1.Container) *DaemonsetBuilder {
	d.container = container
	return d
}

func (d *DaemonsetBuilder) PodLabel(labelKey string, labelValue string) *DaemonsetBuilder {
	d.labels[labelKey] = labelValue
	return d
}

func (d *DaemonsetBuilder) HostNetwork(hostNetwork bool) *DaemonsetBuilder {
	d.hostNetwork = hostNetwork
	return d
}

func (d *DaemonsetBuilder) MountVolume(volume []corev1.Volume, volumeMount []corev1.VolumeMount) *DaemonsetBuilder {
	d.volume = volume
	d.volumeMount = volumeMount
	return d
}

func (d *DaemonsetBuilder) Build() *v1.DaemonSet {
	deploymentSpec := &v1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      d.name,
			Namespace: d.namespace,
			Labels:    d.labels,
		},
		Spec: v1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: d.labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: d.labels,
				},
				Spec: corev1.PodSpec{
					HostNetwork:                   d.hostNetwork,
					NodeSelector:                  d.nodeSelector,
					Containers:                    []corev1.Container{d.container},
					TerminationGracePeriodSeconds: aws.Int64(int64(d.terminationGracePeriod)),
				},
			},
		},
	}

	if len(d.volume) > 0 && len(d.volumeMount) > 0 {
		deploymentSpec.Spec.Template.Spec.Volumes = d.volume
		deploymentSpec.Spec.Template.Spec.Containers[0].VolumeMounts = d.volumeMount
	}
	return deploymentSpec
}
