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
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/aws/amazon-ecs-agent/agent/credentials"
	mock_credentials "github.com/aws/amazon-ecs-agent/agent/credentials/mocks"
	mock_audit "github.com/aws/amazon-ecs-agent/agent/logger/audit/mocks"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
)

const (
	roleArn         = "r1"
	accessKeyID     = "ak"
	secretAccessKey = "sk"
	credentialsID   = "credentialsId"
)

// TestInvalidPath tests if HTTP status code 404 is returned when invalid path is queried.
func TestInvalidPath(t *testing.T) {
	testErrorResponsesFromServer(t, "/", nil)
}

// TestCredentialsV1RequestWithNoArguments tests if HTTP status code 400 is returned when
// query parameters are not specified for the credentials endpoint.
func TestCredentialsV1RequestWithNoArguments(t *testing.T) {
	msg := &errorMessage{
		Code:          NoIDInRequest,
		Message:       "CredentialsV1Request: No ID in the request",
		httpErrorCode: http.StatusBadRequest,
	}
	testErrorResponsesFromServer(t, credentials.V1CredentialsPath, msg)
}

// TestCredentialsV2RequestWithNoArguments tests if HTTP status code 400 is returned when
// query parameters are not specified for the credentials endpoint.
func TestCredentialsV2RequestWithNoArguments(t *testing.T) {
	msg := &errorMessage{
		Code:          NoIDInRequest,
		Message:       "CredentialsV2Request: No ID in the request",
		httpErrorCode: http.StatusBadRequest,
	}
	testErrorResponsesFromServer(t, credentials.V2CredentialsPath+"/", msg)
}

// TestCredentialsV1RequestWhenCredentialsIdNotFound tests if HTTP status code 400 is returned when
// the credentials manager does not contain the credentials id specified in the query.
func TestCredentialsV1RequestWhenCredentialsIdNotFound(t *testing.T) {
	expectedErrorMessage := &errorMessage{
		Code:          InvalidIDInRequest,
		Message:       fmt.Sprintf("CredentialsV1Request: ID not found"),
		httpErrorCode: http.StatusBadRequest,
	}
	path := credentials.V1CredentialsPath + "?id=" + credentialsID
	_, err := getResponseForCredentialsRequest(t, expectedErrorMessage.httpErrorCode,
		expectedErrorMessage, path, func() (credentials.TaskIAMRoleCredentials, bool) { return credentials.TaskIAMRoleCredentials{}, false })
	assert.NoError(t, err, "Error getting response body")
}

// TestCredentialsV2RequestWhenCredentialsIdNotFound tests if HTTP status code 400 is returned when
// the credentials manager does not contain the credentials id specified in the query.
func TestCredentialsV2RequestWhenCredentialsIdNotFound(t *testing.T) {
	expectedErrorMessage := &errorMessage{
		Code:          InvalidIDInRequest,
		Message:       fmt.Sprintf("CredentialsV2Request: ID not found"),
		httpErrorCode: http.StatusBadRequest,
	}
	path := credentials.V2CredentialsPath + "/" + credentialsID
	_, err := getResponseForCredentialsRequest(t, expectedErrorMessage.httpErrorCode,
		expectedErrorMessage, path, func() (credentials.TaskIAMRoleCredentials, bool) { return credentials.TaskIAMRoleCredentials{}, false })
	assert.NoError(t, err, "Error getting response body")
}

// TestCredentialsV1RequestWhenCredentialsUninitialized tests if HTTP status code 500 is returned when
// the credentials manager returns empty credentials.
func TestCredentialsV1RequestWhenCredentialsUninitialized(t *testing.T) {
	expectedErrorMessage := &errorMessage{
		Code:          CredentialsUninitialized,
		Message:       fmt.Sprintf("CredentialsV1Request: Credentials uninitialized for ID"),
		httpErrorCode: http.StatusServiceUnavailable,
	}
	path := credentials.V1CredentialsPath + "?id=" + credentialsID
	_, err := getResponseForCredentialsRequest(t, expectedErrorMessage.httpErrorCode,
		expectedErrorMessage, path, func() (credentials.TaskIAMRoleCredentials, bool) { return credentials.TaskIAMRoleCredentials{}, true })
	assert.NoError(t, err, "Error getting response body")
}

// TestCredentialsV2RequestWhenCredentialsUninitialized tests if HTTP status code 500 is returned when
// the credentials manager returns empty credentials.
func TestCredentialsV2RequestWhenCredentialsUninitialized(t *testing.T) {
	expectedErrorMessage := &errorMessage{
		Code:          CredentialsUninitialized,
		Message:       fmt.Sprintf("CredentialsV2Request: Credentials uninitialized for ID"),
		httpErrorCode: http.StatusServiceUnavailable,
	}
	path := credentials.V2CredentialsPath + "/" + credentialsID
	_, err := getResponseForCredentialsRequest(t, expectedErrorMessage.httpErrorCode,
		expectedErrorMessage, path, func() (credentials.TaskIAMRoleCredentials, bool) { return credentials.TaskIAMRoleCredentials{}, true })
	assert.NoError(t, err, "Error getting response body")
}

// TestCredentialsV1RequestWhenCredentialsFound tests if HTTP status code 200 is returned when
// the credentials manager contains the credentials id specified in the query.
func TestCredentialsV1RequestWhenCredentialsFound(t *testing.T) {
	creds := credentials.TaskIAMRoleCredentials{
		ARN: "arn",
		IAMRoleCredentials: credentials.IAMRoleCredentials{
			RoleArn:         roleArn,
			AccessKeyID:     accessKeyID,
			SecretAccessKey: secretAccessKey,
		},
	}
	path := credentials.V1CredentialsPath + "?id=" + credentialsID
	body, err := getResponseForCredentialsRequest(t, http.StatusOK, nil, path, func() (credentials.TaskIAMRoleCredentials, bool) { return creds, true })
	assert.NoError(t, err)

	credentials, err := parseResponseBody(body)
	assert.NoError(t, err, "Error retrieving credentials")

	assert.Equal(t, roleArn, credentials.RoleArn, "Incorrect credentials received: role ARN")
	assert.Equal(t, accessKeyID, credentials.AccessKeyID, "Incorrect credentials received: access key ID")
	assert.Equal(t, secretAccessKey, credentials.SecretAccessKey, "Incorrect credentials received: secret access key")
}

// TestCredentialsV2RequestWhenCredentialsFound tests if HTTP status code 200 is returned when
// the credentials manager contains the credentials id specified in the query.
func TestCredentialsV2RequestWhenCredentialsFound(t *testing.T) {
	creds := credentials.TaskIAMRoleCredentials{
		ARN: "arn",
		IAMRoleCredentials: credentials.IAMRoleCredentials{
			RoleArn:         roleArn,
			AccessKeyID:     accessKeyID,
			SecretAccessKey: secretAccessKey,
		},
	}
	path := credentials.V2CredentialsPath + "/" + credentialsID
	body, err := getResponseForCredentialsRequest(t, http.StatusOK, nil, path, func() (credentials.TaskIAMRoleCredentials, bool) { return creds, true })
	if err != nil {
		t.Fatalf("Error retrieving credentials response: %v", err)
	}

	credentials, err := parseResponseBody(body)
	assert.NoError(t, err, "Error retrieving credentials")

	assert.Equal(t, roleArn, credentials.RoleArn, "Incorrect credentials received: role ARN")
	assert.Equal(t, accessKeyID, credentials.AccessKeyID, "Incorrect credentials received: access key ID")
	assert.Equal(t, secretAccessKey, credentials.SecretAccessKey, "Incorrect credentials received: secret access key")
}

func testErrorResponsesFromServer(t *testing.T, path string, expectedErrorMessage *errorMessage) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	credentialsManager := mock_credentials.NewMockManager(ctrl)
	auditLog := mock_audit.NewMockAuditLogger(ctrl)
	server := setupServer(credentialsManager, auditLog)

	recorder := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", path, nil)
	if path == credentials.V1CredentialsPath || path == credentials.V2CredentialsPath+"/" {
		auditLog.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any())
	}

	server.Handler.ServeHTTP(recorder, req)
	httpErrorCode := http.StatusNotFound
	if expectedErrorMessage != nil {
		httpErrorCode = expectedErrorMessage.httpErrorCode
	}
	assert.Equal(t, httpErrorCode, recorder.Code, "Incorrect return code")

	// Only paths that are equal to /v1/credentials will return valid error responses.
	if path == credentials.CredentialsPath {
		errorMessage := &errorMessage{}
		json.Unmarshal(recorder.Body.Bytes(), errorMessage)
		assert.Equal(t, expectedErrorMessage.Code, errorMessage.Code, "Incorrect error code")
		assert.Equal(t, expectedErrorMessage.Message, errorMessage.Message, "Incorrect error message")
	}
}

// getResponseForCredentialsRequestWithParameters queries credentials for the
// given id. The getCredentials function is used to simulate getting the
// credentials object from the CredentialsManager
func getResponseForCredentialsRequest(t *testing.T, expectedStatus int,
	expectedErrorMessage *errorMessage, path string, getCredentials func() (credentials.TaskIAMRoleCredentials, bool)) (*bytes.Buffer, error) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	credentialsManager := mock_credentials.NewMockManager(ctrl)
	auditLog := mock_audit.NewMockAuditLogger(ctrl)
	server := setupServer(credentialsManager, auditLog)
	recorder := httptest.NewRecorder()

	creds, ok := getCredentials()
	credentialsManager.EXPECT().GetTaskCredentials(gomock.Any()).Return(creds, ok)
	auditLog.EXPECT().Log(gomock.Any(), gomock.Any(), gomock.Any())

	params := make(url.Values)
	params[credentials.CredentialsIDQueryParameterName] = []string{credentialsID}

	req, _ := http.NewRequest("GET", path, nil)
	server.Handler.ServeHTTP(recorder, req)

	assert.Equal(t, expectedStatus, recorder.Code, "Incorrect return code")

	if recorder.Code != http.StatusOK {
		errorMessage := &errorMessage{}
		json.Unmarshal(recorder.Body.Bytes(), errorMessage)

		assert.Equal(t, expectedErrorMessage.Code, errorMessage.Code, "Incorrect error code")
		assert.Equal(t, expectedErrorMessage.Message, errorMessage.Message, "Incorrect error message")
	}

	return recorder.Body, nil
}

// parseResponseBody parses the HTTP response body into an IAMRoleCredentials object.
func parseResponseBody(body *bytes.Buffer) (*credentials.IAMRoleCredentials, error) {
	response, err := ioutil.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("Error reading response body: %v", err)
	}
	var creds credentials.IAMRoleCredentials
	err = json.Unmarshal(response, &creds)
	if err != nil {
		return nil, fmt.Errorf("Error unmarshaling: %v", err)
	}
	return &creds, nil
}
