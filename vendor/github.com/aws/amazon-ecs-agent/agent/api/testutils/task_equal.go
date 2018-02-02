// Copyright 2014-2015 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

package testutils

import "github.com/aws/amazon-ecs-agent/agent/api"

// TasksEqual determines if the lhs and rhs tasks are equal for the definition
// of having the same family, version, statuses, and equal containers.
func TasksEqual(lhs, rhs *api.Task) bool {
	if lhs == rhs {
		return true
	}
	if lhs.Arn != rhs.Arn {
		return false
	}

	if lhs.Family != rhs.Family || lhs.Version != rhs.Version {
		return false
	}

	for _, left := range lhs.Containers {
		right, ok := rhs.ContainerByName(left.Name)
		if !ok {
			return false
		}
		if !ContainersEqual(left, right) {
			return false
		}
	}

	if lhs.DesiredStatusUnsafe != rhs.DesiredStatusUnsafe {
		return false
	}
	if lhs.GetKnownStatus() != rhs.GetKnownStatus() {
		return false
	}
	return true
}
