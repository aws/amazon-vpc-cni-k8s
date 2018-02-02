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

// Package engine contains code for interacting with container-running backends and handling events from them.
// It supports Docker as the sole task engine type.
package engine

import (
	"github.com/aws/amazon-ecs-agent/agent/config"
	"github.com/aws/amazon-ecs-agent/agent/containermetadata"
	"github.com/aws/amazon-ecs-agent/agent/credentials"
	"github.com/aws/amazon-ecs-agent/agent/engine/dockerstate"
	"github.com/aws/amazon-ecs-agent/agent/eventstream"
	"github.com/aws/amazon-ecs-agent/agent/logger"
)

var log = logger.ForModule("TaskEngine")

// NewTaskEngine returns a default TaskEngine
func NewTaskEngine(cfg *config.Config, client DockerClient,
	credentialsManager credentials.Manager,
	containerChangeEventStream *eventstream.EventStream,
	imageManager ImageManager, state dockerstate.TaskEngineState,
	metadataManager containermetadata.Manager) TaskEngine {
	return NewDockerTaskEngine(cfg, client, credentialsManager, containerChangeEventStream, imageManager, state, metadataManager)
}
