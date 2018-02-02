// Copyright 2014-2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

// Package testutils contains files that are used in tests but not elsewhere and thus can
// be excluded from the final executable. Including them in a different package
// allows this to happen
package testutils

import (
	"reflect"

	"github.com/aws/amazon-ecs-agent/agent/api"
	"github.com/aws/amazon-ecs-agent/agent/utils"
)

// ContainersEqual determines if this container is equal to another container.
// This is not exact equality, but logical equality.
// TODO: use reflection along with `equal:"unordered"` annotations on slices to
// replace this verbose code (low priority, but would be fun)
func ContainersEqual(lhs, rhs *api.Container) bool {
	if lhs == rhs {
		return true
	}
	if lhs.Name != rhs.Name || lhs.Image != rhs.Image {
		return false
	}

	if !utils.StrSliceEqual(lhs.Command, rhs.Command) {
		return false
	}
	if lhs.CPU != rhs.CPU || lhs.Memory != rhs.Memory {
		return false
	}
	// Order doesn't matter
	if !utils.SlicesDeepEqual(lhs.Links, rhs.Links) {
		return false
	}
	if !utils.SlicesDeepEqual(lhs.VolumesFrom, rhs.VolumesFrom) {
		return false
	}
	if !utils.SlicesDeepEqual(lhs.MountPoints, rhs.MountPoints) {
		return false
	}
	if !utils.SlicesDeepEqual(lhs.Ports, rhs.Ports) {
		return false
	}
	if !utils.SlicesDeepEqual(lhs.KnownPortBindings, rhs.KnownPortBindings) {
		return false
	}
	if lhs.Essential != rhs.Essential {
		return false
	}
	if lhs.EntryPoint == nil || rhs.EntryPoint == nil {
		if lhs.EntryPoint != rhs.EntryPoint {
			return false
		}
		// both nil
	} else {
		if !utils.StrSliceEqual(*lhs.EntryPoint, *rhs.EntryPoint) {
			return false
		}
	}
	if !reflect.DeepEqual(lhs.Environment, rhs.Environment) {
		return false
	}
	if !ContainerOverridesEqual(lhs.Overrides, rhs.Overrides) {
		return false
	}
	if lhs.DesiredStatusUnsafe != rhs.DesiredStatusUnsafe || lhs.KnownStatusUnsafe != rhs.KnownStatusUnsafe {
		return false
	}
	if lhs.AppliedStatus != rhs.AppliedStatus {
		return false
	}
	if !reflect.DeepEqual(lhs.GetKnownExitCode(), rhs.GetKnownExitCode()) {
		return false
	}

	return true
}

// ContainerOverridesEqual determines if two container overrides are equal
func ContainerOverridesEqual(lhs, rhs api.ContainerOverrides) bool {
	if lhs.Command == nil || rhs.Command == nil {
		if lhs.Command != rhs.Command {
			return false
		}
	} else {
		if !utils.StrSliceEqual(*lhs.Command, *rhs.Command) {
			return false
		}
	}

	return true
}
