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

package api

import (
	"sync"

	"github.com/aws/amazon-ecs-agent/agent/credentials"
)

// RegistryAuthenticationData is the authentication data sent by the ECS backend.  Currently, the only supported
// authentication data is for ECR.
type RegistryAuthenticationData struct {
	Type        string       `json:"type"`
	ECRAuthData *ECRAuthData `json:"ecrAuthData"`
}

// ECRAuthData is the authentication details for ECR specifying the region, registryID, and possible endpoint override
type ECRAuthData struct {
	EndpointOverride string `json:"endpointOverride"`
	Region           string `json:"region"`
	RegistryID       string `json:"registryId"`
	UseExecutionRole bool   `json:"useExecutionRole"`
	pullCredentials  credentials.IAMRoleCredentials
	lock             sync.RWMutex
}

// GetPullCredentials returns the pull credentials in the auth
func (auth *ECRAuthData) GetPullCredentials() credentials.IAMRoleCredentials {
	auth.lock.RLock()
	defer auth.lock.RUnlock()

	return auth.pullCredentials
}

// SetPullCredentials sets the credentials to pull from ECR in the auth
func (auth *ECRAuthData) SetPullCredentials(creds credentials.IAMRoleCredentials) {
	auth.lock.Lock()
	defer auth.lock.Unlock()

	auth.pullCredentials = creds
}
