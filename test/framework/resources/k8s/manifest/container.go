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
	v1 "k8s.io/api/core/v1"
)

type Container struct {
	name            string
	image           string
	imagePullPolicy v1.PullPolicy
	command         []string
	args            []string
}

func NewBusyBoxContainerBuilder() *Container {
	return &Container{
		name:            "busybox",
		image:           "busybox",
		imagePullPolicy: v1.PullIfNotPresent,
		command:         []string{"sleep", "3600"},
		args:            []string{},
	}
}

func NewNetCatAlpineContainer() *Container {
	return &Container{
		name:            "net-cat",
		image:           "public.ecr.aws/k4b6w6v3/vpc-cni-tester:latest", // TODO: Add link to instruction
		imagePullPolicy: v1.PullIfNotPresent,
	}
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

func (w *Container) Args(arg []string) *Container {
	w.args = arg
	return w
}

func (w *Container) Build() v1.Container {
	return v1.Container{
		Name:            w.name,
		Image:           w.image,
		Command:         w.command,
		Args:            w.args,
		ImagePullPolicy: w.imagePullPolicy,
	}
}
