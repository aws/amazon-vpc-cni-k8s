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
	"github.com/aws/amazon-ecs-agent/agent/resources/cgroup/factory"

	"github.com/cihub/seelog"
	"github.com/containerd/cgroups"
	"github.com/pkg/errors"
)

// control is used to implement the cgroup Control interface
type control struct {
	factory.CgroupFactory
}

// New is used to obtain a new cgroup control object
func New() Control {
	return newControl(&factory.GlobalCgroupFactory{})
}

// newControl helps setup the cgroup controller
func newControl(cgroupFact factory.CgroupFactory) Control {
	return &control{
		cgroupFact,
	}
}

// Create creates a new cgroup based off the spec post validation
func (c *control) Create(cgroupSpec *Spec) (cgroups.Cgroup, error) {
	// Validate incoming spec
	err := validateCgroupSpec(cgroupSpec)
	if err != nil {
		return nil, errors.Wrapf(err, "cgroup create: failed to validate spec")
	}

	// Create cgroup
	seelog.Infof("Creating cgroup %s", cgroupSpec.Root)
	controller, err := c.New(cgroups.V1, cgroups.StaticPath(cgroupSpec.Root), cgroupSpec.Specs)

	if err != nil {
		return nil, errors.Wrapf(err, "cgroup create: unable to create controller")
	}

	return controller, nil
}

// Remove is used to delete the cgroup
func (c *control) Remove(cgroupPath string) error {
	seelog.Debugf("Removing cgroup %s", cgroupPath)

	controller, err := c.Load(cgroups.V1, cgroups.StaticPath(cgroupPath))
	if err != nil {
		return errors.Wrapf(err, "cgroup remove: unable to obtain controller")
	}

	// Delete cgroup
	err = controller.Delete()
	if err != nil {
		return errors.Wrapf(err, "cgroup remove: unable to delete cgroup")
	}
	return nil
}

// Exists is used to verify the existence of a cgroup
func (c *control) Exists(cgroupPath string) bool {
	seelog.Debugf("Checking existence of cgroup: %s", cgroupPath)

	controller, err := c.Load(cgroups.V1, cgroups.StaticPath(cgroupPath))
	if err != nil || controller == nil {
		return false
	}

	return true
}

// validateCgroupSpec checks the cgroup spec for valid path and specifications
func validateCgroupSpec(cgroupSpec *Spec) error {
	if cgroupSpec == nil {
		return errors.New("cgroup spec validator: empty cgroup spec")
	}

	if cgroupSpec.Root == "" {
		return errors.New("cgroup spec validator: invalid cgroup root")
	}

	// Validate the linux resource specs
	if cgroupSpec.Specs == nil {
		return errors.New("cgroup spec validator: empty linux resource spec")
	}
	return nil
}
