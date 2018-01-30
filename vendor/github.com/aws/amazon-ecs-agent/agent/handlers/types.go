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

package handlers

import "github.com/aws/amazon-ecs-agent/agent/engine/dockerstate"

type MetadataResponse struct {
	Cluster              string
	ContainerInstanceArn *string
	Version              string
}

type TaskResponse struct {
	Arn           string
	DesiredStatus string `json:",omitempty"`
	KnownStatus   string
	Family        string
	Version       string
	Containers    []ContainerResponse
}

type TasksResponse struct {
	Tasks []*TaskResponse
}

type ContainerResponse struct {
	DockerId   string
	DockerName string
	Name       string
}

type DockerStateResolver interface {
	State() dockerstate.TaskEngineState
}
