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

// Package testutils contains files that are used in tests but not elsewhere and thus can
// be excluded from the final executable. Including them in a different package
// allows this to happen
package testutils

import "github.com/aws/amazon-ecs-agent/agent/engine"
import state_testutil "github.com/aws/amazon-ecs-agent/agent/engine/dockerstate/testutils"

// DockerTaskEnginesEqual determines if the lhs and rhs task engines given are
// equal (as defined by their state being equal)
func DockerTaskEnginesEqual(lhs, rhs *engine.DockerTaskEngine) bool {
	return state_testutil.DockerStatesEqual(lhs.State(), rhs.State())
}
