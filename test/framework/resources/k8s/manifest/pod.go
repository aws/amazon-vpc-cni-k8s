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
	v1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type PodBuilder struct {
	name                   string
	namespace              string
	hostNetwork            bool
	container              v1.Container
	labels                 map[string]string
	terminationGracePeriod int
	nodeName               string
	restartPolicy          v1.RestartPolicy
	nodeSelector           map[string]string
	volume                 []v1.Volume
	volumeMount            []v1.VolumeMount
}

func NewDefaultPodBuilder() *PodBuilder {
	return &PodBuilder{
		name:                   "test-pod",
		namespace:              utils.DefaultTestNamespace,
		labels:                 map[string]string{},
		terminationGracePeriod: 1,
		restartPolicy:          v1.RestartPolicyNever,
		nodeSelector:           map[string]string{"kubernetes.io/os": "linux"},
	}
}

func (p *PodBuilder) Name(name string) *PodBuilder {
	p.name = name
	return p
}

func (p *PodBuilder) Namespace(namespace string) *PodBuilder {
	p.namespace = namespace
	return p
}

func (p *PodBuilder) HostNetwork(hostNetwork bool) *PodBuilder {
	p.hostNetwork = hostNetwork
	return p
}

func (p *PodBuilder) Container(container v1.Container) *PodBuilder {
	p.container = container
	return p
}

func (p *PodBuilder) PodLabel(labelKey string, labelVal string) *PodBuilder {
	p.labels[labelKey] = labelVal
	return p
}

func (p *PodBuilder) NodeName(nodeName string) *PodBuilder {
	p.nodeName = nodeName
	return p
}

func (p *PodBuilder) NodeSelector(nodeLabelKey string, nodeLabelVal string) *PodBuilder {
	if nodeLabelKey != "" {
		p.nodeSelector[nodeLabelKey] = nodeLabelVal
	}
	return p
}

func (p *PodBuilder) TerminationGracePeriod(period int) *PodBuilder {
	p.terminationGracePeriod = period
	return p
}

func (p *PodBuilder) RestartPolicy(policy v1.RestartPolicy) *PodBuilder {
	p.restartPolicy = policy
	return p
}

func (p *PodBuilder) MountVolume(volume []v1.Volume, volumeMount []v1.VolumeMount) *PodBuilder {
	p.volume = volume
	p.volumeMount = volumeMount
	return p
}

func (p *PodBuilder) Build() *v1.Pod {
	podSpec := &v1.Pod{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      p.name,
			Namespace: p.namespace,
			Labels:    p.labels,
		},
		Spec: v1.PodSpec{
			Containers:                    []v1.Container{p.container},
			RestartPolicy:                 p.restartPolicy,
			TerminationGracePeriodSeconds: aws.Int64(int64(p.terminationGracePeriod)),
			NodeName:                      p.nodeName,
			HostNetwork:                   p.hostNetwork,
			NodeSelector:                  p.nodeSelector,
		},
	}
	if len(p.volume) > 0 && len(p.volumeMount) > 0 {
		podSpec.Spec.Volumes = p.volume
		podSpec.Spec.Containers[0].VolumeMounts = p.volumeMount
	}
	return podSpec
}
