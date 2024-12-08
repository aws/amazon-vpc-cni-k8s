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

	"github.com/aws/aws-sdk-go-v2/aws"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type DeploymentBuilder struct {
	namespace              string
	name                   string
	replicas               int
	container              corev1.Container
	labels                 map[string]string
	nodeSelector           map[string]string
	terminationGracePeriod int
	nodeName               string
	hostNetwork            bool
	volume                 []corev1.Volume
	volumeMount            []corev1.VolumeMount
}

func NewBusyBoxDeploymentBuilder(testImageRegistry string) *DeploymentBuilder {
	return &DeploymentBuilder{
		namespace:              utils.DefaultTestNamespace,
		name:                   "deployment-test",
		replicas:               10,
		container:              NewBusyBoxContainerBuilder(testImageRegistry).Build(),
		labels:                 map[string]string{"role": "test"},
		nodeSelector:           map[string]string{"kubernetes.io/os": "linux"},
		terminationGracePeriod: 1,
	}
}

func NewDefaultDeploymentBuilder() *DeploymentBuilder {
	return &DeploymentBuilder{
		namespace:              utils.DefaultTestNamespace,
		terminationGracePeriod: 1,
		labels:                 map[string]string{"role": "test"},
		nodeSelector:           map[string]string{"kubernetes.io/os": "linux"},
	}
}

func NewCalicoStarDeploymentBuilder() *DeploymentBuilder {
	return &DeploymentBuilder{
		labels:       map[string]string{},
		nodeSelector: map[string]string{},
	}
}

func (d *DeploymentBuilder) Labels(labels map[string]string) *DeploymentBuilder {
	d.labels = labels
	return d
}

func (d *DeploymentBuilder) NodeSelector(labelKey string, labelVal string) *DeploymentBuilder {
	if labelKey != "" {
		d.nodeSelector[labelKey] = labelVal
	}
	return d
}

func (d *DeploymentBuilder) Namespace(namespace string) *DeploymentBuilder {
	d.namespace = namespace
	return d
}

func (d *DeploymentBuilder) TerminationGracePeriod(tg int) *DeploymentBuilder {
	d.terminationGracePeriod = tg
	return d
}

func (d *DeploymentBuilder) Name(name string) *DeploymentBuilder {
	d.name = name
	return d
}

func (d *DeploymentBuilder) NodeName(nodeName string) *DeploymentBuilder {
	d.nodeName = nodeName
	return d
}

func (d *DeploymentBuilder) Replicas(replicas int) *DeploymentBuilder {
	d.replicas = replicas
	return d
}

func (d *DeploymentBuilder) Container(container corev1.Container) *DeploymentBuilder {
	d.container = container
	return d
}

func (d *DeploymentBuilder) PodLabel(labelKey string, labelValue string) *DeploymentBuilder {
	d.labels[labelKey] = labelValue
	return d
}

func (d *DeploymentBuilder) HostNetwork(hostNetwork bool) *DeploymentBuilder {
	d.hostNetwork = hostNetwork
	return d
}

func (d *DeploymentBuilder) MountVolume(volume []corev1.Volume, volumeMount []corev1.VolumeMount) *DeploymentBuilder {
	d.volume = volume
	d.volumeMount = volumeMount
	return d
}

func (d *DeploymentBuilder) Build() *v1.Deployment {
	deploymentSpec := &v1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      d.name,
			Namespace: d.namespace,
			Labels:    d.labels,
		},
		Spec: v1.DeploymentSpec{
			Replicas: aws.Int32(int32(d.replicas)),
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
					NodeName:                      d.nodeName,
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
