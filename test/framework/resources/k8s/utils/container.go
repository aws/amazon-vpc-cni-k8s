// Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//     http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package utils

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
)

// AddOrUpdateEnvironmentVariable adds or updates existing Environment variable to the
// specified container name
func AddOrUpdateEnvironmentVariable(containers []v1.Container, containerName string,
	envVars map[string]string) error {

	containerIndex := -1
	// Update existing environment variable first
	for i, container := range containers {
		if container.Name != containerName {
			continue
		}
		containerIndex = i
		for j, env := range container.Env {
			if val, alreadyPresent := envVars[env.Name]; alreadyPresent {
				container.Env[j].Value = val
				// Delete, so we don't add the environment variable multiple times
				delete(envVars, env.Name)
			}
		}
	}

	if containerIndex < 0 {
		return fmt.Errorf("failed to find container %s in the passed containers",
			containerName)
	}

	// Add the environment variables that were not already present
	for key, val := range envVars {
		containers[containerIndex].Env = append(containers[containerIndex].Env,
			v1.EnvVar{
				Name:  key,
				Value: val,
			})
	}

	return nil
}

// RemoveEnvironmentVariables removes the environment variable from the specified container
func RemoveEnvironmentVariables(containers []v1.Container, containerName string,
	envVars map[string]struct{}) error {
	var updatedEnvVar []v1.EnvVar
	containerIndex := -1
	for i, container := range containers {
		if container.Name != containerName {
			continue
		}
		containerIndex = i
		for j := 0; j < len(container.Env); j++ {
			if _, ok := envVars[container.Env[j].Name]; !ok {
				updatedEnvVar = append(updatedEnvVar, container.Env[j])
			}
		}
	}

	if containerIndex < 0 {
		return fmt.Errorf("failed to find cotnainer %s in list of containers",
			containerName)
	}

	containers[containerIndex].Env = updatedEnvVar

	return nil
}
