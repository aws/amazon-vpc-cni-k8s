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

package factory

import (
	"github.com/containerd/cgroups"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

// CgroupFactory wraps around the global functions exposed by the cgroups library
type CgroupFactory interface {
	// New is used to create a new cgroup hierarchy
	New(hierarchy cgroups.Hierarchy, path cgroups.Path, specs *specs.LinuxResources) (cgroups.Cgroup, error)
	// Load is used to load the cgroup based off the cgroup path
	Load(hierarchy cgroups.Hierarchy, path cgroups.Path) (cgroups.Cgroup, error)
}

// GlobalCgroupFactory calls the cgroups library global functions
type GlobalCgroupFactory struct{}

// Load is used to load the cgroup hierarchy based off the cgroup path
func (c *GlobalCgroupFactory) Load(hierarchy cgroups.Hierarchy, path cgroups.Path) (cgroups.Cgroup, error) {
	return cgroups.Load(hierarchy, path)
}

// New is used to create a new cgroup hierarchy
func (c *GlobalCgroupFactory) New(hierarchy cgroups.Hierarchy, path cgroups.Path, specs *specs.LinuxResources) (cgroups.Cgroup, error) {
	return cgroups.New(hierarchy, path, specs)
}
