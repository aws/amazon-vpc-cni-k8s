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

	v1 "k8s.io/api/core/v1"
)

type Container struct {
	name            string
	image           string
	imagePullPolicy v1.PullPolicy
	command         []string
	args            []string
	probe           *v1.Probe
	ports           []v1.ContainerPort
	securityContext *v1.SecurityContext
	Env             []v1.EnvVar
}

func NewBusyBoxContainerBuilder(testImageRegistry string) *Container {
	return &Container{
		name:            "busybox",
		image:           utils.GetTestImage(testImageRegistry, utils.BusyBoxImage),
		imagePullPolicy: v1.PullIfNotPresent,
		command:         []string{"sleep", "3600"},
		args:            []string{},
	}
}

func NewCurlContainer() *Container {
	return &Container{
		name:            "curl",
		image:           "curlimages/curl:latest",
		imagePullPolicy: v1.PullIfNotPresent,
	}
}

// See test/agent/README.md in this repository for more details
func NewTestHelperContainer(testImageRegistry string) *Container {
	return &Container{
		name:            "test-helper",
		image:           utils.GetTestImage(testImageRegistry, utils.TestAgentImage),
		imagePullPolicy: v1.PullIfNotPresent,
	}
}

func NewNetCatAlpineContainer(testImageRegistry string) *Container {
	return &Container{
		name: "net-cat",
		// simple netcat OpenBSD version with alpine as the base image
		// compatible with arm64 and amd64
		image:           utils.GetTestImage(testImageRegistry, utils.NetCatImage),
		imagePullPolicy: v1.PullIfNotPresent,
	}
}

func NewBaseContainer() *Container {
	return &Container{}
}

func (w *Container) CapabilitiesForSecurityContext(add []v1.Capability, drop []v1.Capability) *Container {
	w.securityContext = &v1.SecurityContext{
		Capabilities: &v1.Capabilities{
			Add:  add,
			Drop: drop,
		},
	}
	return w
}

func (w *Container) Name(name string) *Container {
	w.name = name
	return w
}

func (w *Container) Image(image string) *Container {
	w.image = image
	return w
}

func (w *Container) ImagePullPolicy(policy v1.PullPolicy) *Container {
	w.imagePullPolicy = policy
	return w
}

func (w *Container) Command(cmd []string) *Container {
	w.command = cmd
	return w
}

func (w *Container) EnvVar(env []v1.EnvVar) *Container {
	w.Env = env
	return w
}

func (w *Container) Args(arg []string) *Container {
	w.args = arg
	return w
}

func (w *Container) LivenessProbe(probe *v1.Probe) *Container {
	w.probe = probe
	return w
}

func (w *Container) Port(port v1.ContainerPort) *Container {
	w.ports = append(w.ports, port)
	return w
}

func (w *Container) Build() v1.Container {
	return v1.Container{
		Name:            w.name,
		Image:           w.image,
		Command:         w.command,
		Args:            w.args,
		ImagePullPolicy: w.imagePullPolicy,
		LivenessProbe:   w.probe,
		Ports:           w.ports,
		SecurityContext: w.securityContext,
		Env:             w.Env,
	}
}
