// +build !windows
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

package dockerclient

import (
	"github.com/aws/amazon-ecs-agent/agent/engine/dockeriface"
)

const (
	// minDockerAPIVersion is the min Docker API version supported by agent
	minDockerAPIVersion = Version_1_17
)

// GetClient on linux will simply return the cached client from the map
func (f *factory) GetClient(version DockerVersion) (dockeriface.Client, error) {
	return f.getClient(version)
}

// getAgentVersions returns a list of supported agent-supported versions for linux
func getAgentVersions() []DockerVersion {
	return []DockerVersion{
		Version_1_17,
		Version_1_18,
		Version_1_19,
		Version_1_20,
		Version_1_21,
		Version_1_22,
		Version_1_23,
		Version_1_24,
		Version_1_25,
		Version_1_30,
	}
}

// getDefaultVersion will return the default Docker API version for linux
func getDefaultVersion() DockerVersion {
	return Version_1_17
}
