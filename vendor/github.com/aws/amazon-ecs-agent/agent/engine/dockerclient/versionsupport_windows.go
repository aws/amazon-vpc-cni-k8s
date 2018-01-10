// +build windows
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

const minDockerAPIVersion = Version_1_24

// GetClient will replace some versions of Docker on Windows. We need this because
// agent assumes that it can always call older versions of the docker API.
func (f *factory) GetClient(version DockerVersion) (dockeriface.Client, error) {
	for _, v := range getWindowsReplaceableVersions() {
		if v == version {
			version = minDockerAPIVersion
			break
		}
	}
	return f.getClient(version)
}

// getWindowsReplaceableVersions returns the set of versions that agent will report
// as Docker 1.24
func getWindowsReplaceableVersions() []DockerVersion {
	return []DockerVersion{
		Version_1_17,
		Version_1_18,
		Version_1_19,
		Version_1_20,
		Version_1_21,
		Version_1_22,
		Version_1_23,
	}
}

// getAgentVersions for Windows should return all of the replaceable versions plus additional versions
func getAgentVersions() []DockerVersion {
	return append(getWindowsReplaceableVersions(), minDockerAPIVersion)
}

// getDefaultVersion returns agent's default version of the Docker API
func getDefaultVersion() DockerVersion {
	return minDockerAPIVersion
}
