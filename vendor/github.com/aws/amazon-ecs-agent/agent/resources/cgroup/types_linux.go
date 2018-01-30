// +build linux

// Copyright 2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//	http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package cgroup

import (
	"github.com/containerd/cgroups"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

// Spec captures the abstraction for a creating a new
// cgroup based on root and the runtime specifications
type Spec struct {
	// Root is the cgroup path
	Root string
	// Specs are for all the linux resources including cpu, memory, etc...
	Specs *specs.LinuxResources
}

type Control interface {
	Create(cgroupSpec *Spec) (cgroups.Cgroup, error)
	Remove(cgroupPath string) error
	Exists(cgroupPath string) bool
	Init() error
}
