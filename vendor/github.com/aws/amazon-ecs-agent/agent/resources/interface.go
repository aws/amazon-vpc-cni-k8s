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

package resources

import (
	"github.com/aws/amazon-ecs-agent/agent/api"
)

// Resource interface to interact with platform level resource constructs
// Resource is exposed as a platform agnostic abstraction
type Resource interface {
	// Init is used to initialize the resource
	Init() error
	// Setup sets up the resource
	Setup(task *api.Task) error
	// Cleanup removes the resource
	Cleanup(task *api.Task) error
}
