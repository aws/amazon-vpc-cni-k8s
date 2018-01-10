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
	"testing"

	"github.com/aws/amazon-ecs-agent/agent/acs/model/ecsacs"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/stretchr/testify/assert"
)

// TestIAMRoleCredentialsFromACS tests if credentials sent from ACS can be
// represented correctly as IAMRoleCredentials
func TestIAMRoleCredentialsFromACS(t *testing.T) {
	acsCredentials := &ecsacs.IAMRoleCredentials{
		CredentialsId:   aws.String("credsId"),
		AccessKeyId:     aws.String("keyId"),
		Expiration:      aws.String("soon"),
		RoleArn:         aws.String("roleArn"),
		SecretAccessKey: aws.String("OhhSecret"),
		SessionToken:    aws.String("sessionToken"),
	}

	roleType := "roleType"

	credentials := IAMRoleCredentialsFromACS(acsCredentials, roleType)
	expectedCredentials := IAMRoleCredentials{
		CredentialsID:   "credsId",
		AccessKeyID:     "keyId",
		Expiration:      "soon",
		RoleArn:         "roleArn",
		SecretAccessKey: "OhhSecret",
		SessionToken:    "sessionToken",
		RoleType:        "roleType",
	}
	assert.Equal(t, credentials, expectedCredentials, "Mismatch between expected and constructed credentials")
}

// TestGetTaskCredentialsUnknownId tests if GetTaskCredentials returns a false value
// when credentials for a given id are not be found in the engine
func TestGetTaskCredentialsUnknownId(t *testing.T) {
	manager := NewManager()
	_, ok := manager.GetTaskCredentials("id")
	if ok {
		t.Error("GetTaskCredentials should return false for non existing id")
	}
}

// TestSetTaskCredentialsEmptyTaskCredentials tests if credentials manager returns an
// error when invalid credentials are used to set credentials
func TestSetTaskCredentialsEmptyTaskCredentials(t *testing.T) {
	manager := NewManager()
	err := manager.SetTaskCredentials(TaskIAMRoleCredentials{})
	assert.Error(t, err, "Expected error adding empty task credentials")
}

// TestSetTaskCredentialsNoCredentialsId tests if credentials manager returns an
// error when credentials object with no credentials id is used to set credentials
func TestSetTaskCredentialsNoCredentialsId(t *testing.T) {
	manager := NewManager()
	err := manager.SetTaskCredentials(TaskIAMRoleCredentials{ARN: "t1", IAMRoleCredentials: IAMRoleCredentials{}})
	assert.Error(t, err, "Expected error adding credentials payload without credential ID")
}

// TestSetTaskCredentialsNoTaskArn tests if credentials manager returns an
// error when credentials object with no task arn used to set credentials
func TestSetTaskCredentialsNoTaskArn(t *testing.T) {
	manager := NewManager()
	err := manager.SetTaskCredentials(TaskIAMRoleCredentials{IAMRoleCredentials: IAMRoleCredentials{CredentialsID: "id"}})
	assert.Error(t, err, "Expected error adding credentials payload without task ARN")
}

// TestSetAndGetTaskCredentialsHappyPath tests the happy path workflow for setting
// and getting credentials
func TestSetAndGetTaskCredentialsHappyPath(t *testing.T) {
	manager := NewManager()
	credentials := TaskIAMRoleCredentials{
		ARN: "t1",
		IAMRoleCredentials: IAMRoleCredentials{
			RoleArn:         "r1",
			AccessKeyID:     "akid1",
			SecretAccessKey: "skid1",
			SessionToken:    "stkn",
			Expiration:      "ts",
			CredentialsID:   "cid1",
		},
	}

	err := manager.SetTaskCredentials(credentials)
	assert.NoError(t, err, "Error adding credentials")

	credentialsFromManager, ok := manager.GetTaskCredentials("cid1")
	assert.True(t, ok, "GetTaskCredentials returned false for existing credentials")
	assert.Equal(t, credentials, credentialsFromManager, "Mismatch between added and retrieved credentials")

	updatedCredentials := TaskIAMRoleCredentials{
		ARN: "t1",
		IAMRoleCredentials: IAMRoleCredentials{
			RoleArn:         "r1",
			AccessKeyID:     "akid2",
			SecretAccessKey: "skid2",
			SessionToken:    "stkn2",
			Expiration:      "ts2",
			CredentialsID:   "cid1",
		},
	}
	err = manager.SetTaskCredentials(updatedCredentials)
	assert.NoError(t, err, "Error updating credentials")
	credentialsFromManager, ok = manager.GetTaskCredentials("cid1")

	assert.True(t, ok, "GetTaskCredentials returned false for existing credentials")
	assert.Equal(t, updatedCredentials, credentialsFromManager, "Mismatch between added and retrieved credentials")
}

// TestGenerateCredentialsEndpointRelativeURI tests if the relative credentials endpoint
// URI is generated correctly
func TestGenerateCredentialsEndpointRelativeURI(t *testing.T) {
	credentials := IAMRoleCredentials{
		RoleArn:         "r1",
		AccessKeyID:     "akid1",
		SecretAccessKey: "skid1",
		SessionToken:    "stkn",
		Expiration:      "ts",
		CredentialsID:   "cid1",
	}
	generatedURI := credentials.GenerateCredentialsEndpointRelativeURI()
	expectedURI := fmt.Sprintf(credentialsEndpointRelativeURIFormat, CredentialsPath, "cid1")
	assert.Equal(t, expectedURI, generatedURI, "Credentials endpoint mismatch")
}

// TestRemoveExistingCredentials tests that GetTaskCredentials returns false when
// credentials are removed from the credentials manager
func TestRemoveExistingCredentials(t *testing.T) {
	manager := NewManager()
	credentials := TaskIAMRoleCredentials{
		ARN: "t1",
		IAMRoleCredentials: IAMRoleCredentials{
			RoleArn:         "r1",
			AccessKeyID:     "akid1",
			SecretAccessKey: "skid1",
			SessionToken:    "stkn",
			Expiration:      "ts",
			CredentialsID:   "cid1",
		},
	}
	err := manager.SetTaskCredentials(credentials)
	assert.NoError(t, err, "Error adding credentials")

	credentialsFromManager, ok := manager.GetTaskCredentials("cid1")
	assert.True(t, ok, "GetTaskCredentials returned false for existing credentials")
	assert.Equal(t, credentials, credentialsFromManager, "Mismatch between added and retrieved credentials")

	manager.RemoveCredentials("cid1")
	_, ok = manager.GetTaskCredentials("cid1")
	if ok {
		t.Error("Expected GetTaskCredentials to return false for removed credentials")
	}
}
