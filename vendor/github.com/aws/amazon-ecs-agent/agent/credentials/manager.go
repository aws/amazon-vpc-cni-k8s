// Copyright 2014-2016 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

package credentials

import (
	"fmt"
	"sync"

	"github.com/aws/amazon-ecs-agent/agent/acs/model/ecsacs"
	"github.com/aws/aws-sdk-go/aws"
)

const (
	// CredentialsIDQueryParameterName is the name of GET query parameter for the task ID.
	CredentialsIDQueryParameterName = "id"

	// CredentialsPath is the path to the credentials handler.
	CredentialsPath = V2CredentialsPath

	V1CredentialsPath = "/v1/credentials"
	V2CredentialsPath = "/v2/credentials"

	// credentialsEndpointRelativeURIFormat defines the relative URI format
	// for the credentials endpoint. The place holders are the API Path and
	// credentials ID
	credentialsEndpointRelativeURIFormat = v2CredentialsEndpointRelativeURIFormat

	v1CredentialsEndpointRelativeURIFormat = "%s?" + CredentialsIDQueryParameterName + "=%s"
	v2CredentialsEndpointRelativeURIFormat = "%s/%s"

	// ApplicationRoleType specifies the credentials that are to be used by the
	// task itself
	ApplicationRoleType = "TaskApplication"

	// ExecutionRoleType specifies the credentials used for non task application
	// uses
	ExecutionRoleType = "TaskExecution"
)

// IAMRoleCredentials is used to save credentials sent by ACS
type IAMRoleCredentials struct {
	CredentialsID   string `json:"-"`
	RoleArn         string `json:"RoleArn"`
	AccessKeyID     string `json:"AccessKeyId"`
	SecretAccessKey string `json:"SecretAccessKey"`
	SessionToken    string `json:"Token"`
	// Expiration is a string instead of a timestamp. This is to avoid any loss of context
	// while marshalling/unmarshalling this field in the agent. The agent just echo's
	// whatever is sent by the backend.
	Expiration string `json:"Expiration"`
	// RoleType distinguishes between TaskRole and ExecutionRole for the
	// credentials that are sent from the backend
	RoleType string `json:"-"`
}

// TaskIAMRoleCredentials wraps the task arn and the credentials object for the same
type TaskIAMRoleCredentials struct {
	ARN                string
	IAMRoleCredentials IAMRoleCredentials
	lock               sync.RWMutex
}

// GetIAMRoleCredentials returns the IAM role credentials in the task IAM role struct
func (role *TaskIAMRoleCredentials) GetIAMRoleCredentials() IAMRoleCredentials {
	role.lock.RLock()
	defer role.lock.RUnlock()

	return role.IAMRoleCredentials
}

// GenerateCredentialsEndpointRelativeURI generates the relative URI for the
// credentials endpoint, for a given task id.
func (roleCredentials *IAMRoleCredentials) GenerateCredentialsEndpointRelativeURI() string {
	return fmt.Sprintf(credentialsEndpointRelativeURIFormat, CredentialsPath, roleCredentials.CredentialsID)
}

// credentialsManager implements the Manager interface. It is used to
// save credentials sent from ACS and to retrieve credentials from
// the credentials endpoint
type credentialsManager struct {
	// idToTaskCredentials maps credentials id to its corresponding TaskIAMRoleCredentials object
	idToTaskCredentials map[string]*TaskIAMRoleCredentials
	taskCredentialsLock sync.RWMutex
}

// IAMRoleCredentialsFromACS translates ecsacs.IAMRoleCredentials object to
// api.IAMRoleCredentials
func IAMRoleCredentialsFromACS(roleCredentials *ecsacs.IAMRoleCredentials, roleType string) IAMRoleCredentials {
	return IAMRoleCredentials{
		CredentialsID:   aws.StringValue(roleCredentials.CredentialsId),
		SessionToken:    aws.StringValue(roleCredentials.SessionToken),
		RoleArn:         aws.StringValue(roleCredentials.RoleArn),
		AccessKeyID:     aws.StringValue(roleCredentials.AccessKeyId),
		SecretAccessKey: aws.StringValue(roleCredentials.SecretAccessKey),
		Expiration:      aws.StringValue(roleCredentials.Expiration),
		RoleType:        roleType,
	}
}

// NewManager creates a new credentials manager object
func NewManager() Manager {
	return &credentialsManager{
		idToTaskCredentials: make(map[string]*TaskIAMRoleCredentials),
	}
}

// SetTaskCredentials adds or updates credentials in the credentials manager
func (manager *credentialsManager) SetTaskCredentials(taskCredentials TaskIAMRoleCredentials) error {
	manager.taskCredentialsLock.Lock()
	defer manager.taskCredentialsLock.Unlock()

	credentials := taskCredentials.IAMRoleCredentials
	// Validate that credentials id is not empty
	if credentials.CredentialsID == "" {
		return fmt.Errorf("CredentialsId is empty")
	}

	// Validate that task arn is not empty
	if taskCredentials.ARN == "" {
		return fmt.Errorf("task ARN is empty")
	}

	// Check if credentials exists for the given credentials id
	taskCredentialsInMap, ok := manager.idToTaskCredentials[credentials.CredentialsID]
	if !ok {
		// No existing credentials, create a new one
		taskCredentialsInMap = &TaskIAMRoleCredentials{}
	}
	*taskCredentialsInMap = taskCredentials
	manager.idToTaskCredentials[credentials.CredentialsID] = taskCredentialsInMap

	return nil
}

// GetTaskCredentials retrieves credentials for a given credentials id
func (manager *credentialsManager) GetTaskCredentials(id string) (TaskIAMRoleCredentials, bool) {
	manager.taskCredentialsLock.RLock()
	defer manager.taskCredentialsLock.RUnlock()

	taskCredentials, ok := manager.idToTaskCredentials[id]

	if !ok {
		return TaskIAMRoleCredentials{}, ok
	}
	return *taskCredentials, ok
}

// RemoveCredentials removes credentials from the credentials manager
func (manager *credentialsManager) RemoveCredentials(id string) {
	manager.taskCredentialsLock.Lock()
	defer manager.taskCredentialsLock.Unlock()

	delete(manager.idToTaskCredentials, id)
}
