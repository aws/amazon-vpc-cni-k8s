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

package audit

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/aws/amazon-ecs-agent/agent/config"
	"github.com/aws/amazon-ecs-agent/agent/credentials"
	mock_infologger "github.com/aws/amazon-ecs-agent/agent/logger/audit/mocks"
	"github.com/aws/amazon-ecs-agent/agent/logger/audit/request"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
)

const (
	dummyContainerInstanceArn = "containerInstanceArn"
	dummyCluster              = "cluster"
	dummyEventType            = "someEvent"
	dummyRemoteAddress        = "rAddr"
	dummyURL                  = "http://foo.com" + dummyURLPath + "?id=foo"
	dummyURLPath              = "/urlPath"
	dummyURLV2                = "http://foo.com" + credentials.V2CredentialsPath + "/" + taskARN
	dummyUserAgent            = "userAgent"
	dummyResponseCode         = 400
	dummyRoleType             = "TaskExecution"
	taskARN                   = "task-arn-1"

	commonAuditLogEntryFieldCount = 6
	getCredentialsEntryFieldCount = 4
)

func TestWritingToAuditLog(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockInfoLogger := mock_infologger.NewMockInfoLogger(ctrl)

	req, _ := http.NewRequest("GET", "foo", nil)
	req.RemoteAddr = dummyRemoteAddress
	parsedURL, err := url.Parse(dummyURL)
	if err != nil {
		t.Fatal("error parsing dummyUrl")
	}
	req.URL = parsedURL
	req.Header.Set("User-Agent", dummyUserAgent)

	cfg := &config.Config{
		Cluster:                 dummyCluster,
		CredentialsAuditLogFile: "foo.txt",
	}

	auditLogger := NewAuditLog(dummyContainerInstanceArn, cfg, mockInfoLogger)
	assert.Equal(t, dummyCluster, auditLogger.GetCluster(), "Cluster is not initialized properly")
	assert.Equal(t, dummyContainerInstanceArn, auditLogger.GetContainerInstanceArn(), "ContainerInstanceArn is not initialized properly")

	mockInfoLogger.EXPECT().Info(gomock.Any()).Do(func(logLine string) {
		verifyAuditLogEntryResult(logLine, taskARN, dummyURLPath, t)
	})

	auditLogger.Log(request.LogRequest{Request: req, ARN: taskARN}, dummyResponseCode, GetCredentialsEventType(dummyRoleType))
}

func TestWritingToAuditLogV2(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockInfoLogger := mock_infologger.NewMockInfoLogger(ctrl)

	req, _ := http.NewRequest("GET", "foo", nil)
	req.RemoteAddr = dummyRemoteAddress
	parsedURL, err := url.Parse(dummyURLV2)
	if err != nil {
		t.Fatal("error parsing dummyUrl")
	}
	req.URL = parsedURL
	req.Header.Set("User-Agent", dummyUserAgent)

	cfg := &config.Config{
		Cluster:                 dummyCluster,
		CredentialsAuditLogFile: "foo.txt",
	}

	auditLogger := NewAuditLog(dummyContainerInstanceArn, cfg, mockInfoLogger)
	assert.Equal(t, dummyCluster, auditLogger.GetCluster(), "Cluster is not initialized properly")
	assert.Equal(t, dummyContainerInstanceArn, auditLogger.GetContainerInstanceArn(), "ContainerInstanceArn is not initialized properly")

	mockInfoLogger.EXPECT().Info(gomock.Any()).Do(func(logLine string) {
		verifyAuditLogEntryResult(logLine, taskARN, credentials.V2CredentialsPath, t)
	})

	auditLogger.Log(request.LogRequest{Request: req, ARN: taskARN}, dummyResponseCode, GetCredentialsEventType(dummyRoleType))
}

func TestWritingErrorsToAuditLog(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockInfoLogger := mock_infologger.NewMockInfoLogger(ctrl)

	req, _ := http.NewRequest("GET", "foo", nil)
	req.RemoteAddr = dummyRemoteAddress
	parsedURL, err := url.Parse(dummyURL)
	if err != nil {
		t.Fatal("error parsing dummyUrl")
	}
	req.URL = parsedURL
	req.Header.Set("User-Agent", dummyUserAgent)

	cfg := &config.Config{
		Cluster:                 dummyCluster,
		CredentialsAuditLogFile: "foo.txt",
	}

	auditLogger := NewAuditLog(dummyContainerInstanceArn, cfg, mockInfoLogger)
	assert.Equal(t, dummyCluster, auditLogger.GetCluster(), "Cluster is not initialized properly")
	assert.Equal(t, dummyContainerInstanceArn, auditLogger.GetContainerInstanceArn(), "ContainerInstanceArn is not initialized properly")

	mockInfoLogger.EXPECT().Info(gomock.Any()).Do(func(logLine string) {
		verifyAuditLogEntryResult(logLine, "-", dummyURLPath, t)
	})

	auditLogger.Log(request.LogRequest{Request: req, ARN: ""}, dummyResponseCode, GetCredentialsEventType(dummyRoleType))
}

func TestWritingToAuditLogWhenDisabled(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockInfoLogger := mock_infologger.NewMockInfoLogger(ctrl)

	req, _ := http.NewRequest("GET", "foo", nil)

	cfg := &config.Config{
		Cluster:                     dummyCluster,
		CredentialsAuditLogFile:     "foo.txt",
		CredentialsAuditLogDisabled: true,
	}

	auditLogger := NewAuditLog(dummyContainerInstanceArn, cfg, mockInfoLogger)
	assert.Equal(t, dummyCluster, auditLogger.GetCluster(), "Cluster is not initialized properly")
	assert.Equal(t, dummyContainerInstanceArn, auditLogger.GetContainerInstanceArn(), "ContainerInstanceArn is not initialized properly")

	mockInfoLogger.EXPECT().Info(gomock.Any()).Times(0)

	auditLogger.Log(request.LogRequest{Request: req, ARN: taskARN}, dummyResponseCode, GetCredentialsEventType(dummyRoleType))
}

func TestConstructCommonAuditLogEntryFields(t *testing.T) {
	req, _ := http.NewRequest("GET", "foo", nil)
	req.RemoteAddr = dummyRemoteAddress
	parsedURL, err := url.Parse(dummyURL)
	if err != nil {
		t.Fatal("error parsing dummyUrl")
	}
	req.URL = parsedURL
	req.Header.Set("User-Agent", dummyUserAgent)

	result := constructCommonAuditLogEntryFields(request.LogRequest{Request: req, ARN: taskARN}, dummyResponseCode)

	verifyCommonAuditLogEntryFieldResult(result, taskARN, dummyURLPath, t)
}

func TestConstructAuditLogEntryByTypeGetCredentials(t *testing.T) {
	result := constructAuditLogEntryByType(GetCredentialsEventType(dummyRoleType), dummyCluster,
		dummyContainerInstanceArn)
	verifyConstructAuditLogEntryGetCredentialsResult(result, t)
}

func verifyAuditLogEntryResult(logLine string, expectedTaskArn string, expectedURLPath string, t *testing.T) {
	tokens := strings.Split(logLine, " ")
	assert.Equal(t, commonAuditLogEntryFieldCount+getCredentialsEntryFieldCount, len(tokens), "Incorrect number of tokens in audit log entry")
	verifyCommonAuditLogEntryFieldResult(strings.Join(tokens[:commonAuditLogEntryFieldCount], " "), expectedTaskArn, expectedURLPath, t)
	verifyConstructAuditLogEntryGetCredentialsResult(strings.Join(tokens[commonAuditLogEntryFieldCount:], " "), t)
}

func verifyCommonAuditLogEntryFieldResult(result string, expectedTaskArn string, expectedURLPath string, t *testing.T) {
	tokens := strings.Split(result, " ")
	assert.Equal(t, commonAuditLogEntryFieldCount, len(tokens), "Incorrect number of tokens in common audit log entry")

	respCode, _ := strconv.Atoi(tokens[1])
	assert.Equal(t, dummyResponseCode, respCode, "response code does not match")
	assert.Equal(t, dummyRemoteAddress, tokens[2], "remoted address does not match")
	assert.Equal(t, fmt.Sprintf(`"%s"`, expectedURLPath), tokens[3], "URL path does not match")
	assert.Equal(t, fmt.Sprintf(`"%s"`, dummyUserAgent), tokens[4], "User Agent does not match")
	assert.Equal(t, expectedTaskArn, tokens[5], "ARN for credentials does not match")
}

func verifyConstructAuditLogEntryGetCredentialsResult(result string, t *testing.T) {
	tokens := strings.Split(result, " ")

	assert.Equal(t, getCredentialsEntryFieldCount, len(tokens), "Incorrect number of tokens in GetCredentials audit log entry")
	assert.Equal(t, GetCredentialsEventType(dummyRoleType), tokens[0], "event type does not match")

	auditLogVersion, _ := strconv.Atoi(tokens[1])
	assert.Equal(t, getCredentialsAuditLogVersion, auditLogVersion, "version does not match")
	assert.Equal(t, dummyCluster, tokens[2], "cluster does not match")
	assert.Equal(t, dummyContainerInstanceArn, tokens[3], "containerInstanceArn does not match")
}

func TestConstructAuditLogEntryByTypeUnknownType(t *testing.T) {
	result := constructAuditLogEntryByType("unknownEvent", dummyCluster, dummyContainerInstanceArn)
	assert.Equal(t, "", result, "unknown event type should not return an entry")
}
