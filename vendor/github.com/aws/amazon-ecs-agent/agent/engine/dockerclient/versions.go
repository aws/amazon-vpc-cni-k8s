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

type DockerVersion string

const (
	Version_1_17 DockerVersion = "1.17"
	Version_1_18 DockerVersion = "1.18"
	Version_1_19 DockerVersion = "1.19"
	Version_1_20 DockerVersion = "1.20"
	Version_1_21 DockerVersion = "1.21"
	Version_1_22 DockerVersion = "1.22"
	Version_1_23 DockerVersion = "1.23"
	Version_1_24 DockerVersion = "1.24"
	Version_1_25 DockerVersion = "1.25"
	Version_1_26 DockerVersion = "1.26"
	Version_1_27 DockerVersion = "1.27"
	Version_1_28 DockerVersion = "1.28"
	Version_1_29 DockerVersion = "1.29"
	Version_1_30 DockerVersion = "1.30"
)

// getKnownAPIVersions returns all of the API versions that we know about.
// It doesn't care if the version is supported by Docker or ECS agent
func getKnownAPIVersions() []DockerVersion {
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
		Version_1_26,
		Version_1_27,
		Version_1_28,
		Version_1_29,
		Version_1_30,
	}
}
