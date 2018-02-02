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

package engine

import (
	"encoding/json"

	"github.com/aws/amazon-ecs-agent/agent/api"
	"github.com/aws/amazon-ecs-agent/agent/statechange"
	"github.com/aws/amazon-ecs-agent/agent/statemanager"
	"golang.org/x/net/context"
)

// TaskEngine is an interface for the DockerTaskEngine
type TaskEngine interface {
	Init(context.Context) error
	MustInit(context.Context)
	// Disable *must* only be called when this engine will no longer be used
	// (e.g. right before exiting down the process). It will irreversably stop
	// this task engine from processing new tasks
	Disable()

	// StateChangeEvents will provide information about tasks that have been previously
	// executed. Specifically, it will provide information when they reach
	// running or stopped, as well as providing portbinding and other metadata
	StateChangeEvents() chan statechange.Event
	SetSaver(statemanager.Saver)

	// AddTask adds a new task to the task engine and manages its container's
	// lifecycle. If it returns an error, the task was not added.
	AddTask(*api.Task) error

	// ListTasks lists all the tasks being managed by the TaskEngine.
	ListTasks() ([]*api.Task, error)

	// GetTaskByArn gets a managed task, given a task arn.
	GetTaskByArn(string) (*api.Task, bool)

	Version() (string, error)

	json.Marshaler
	json.Unmarshaler
}
