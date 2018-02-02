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
	"strings"
	"time"

	"github.com/aws/amazon-ecs-agent/agent/credentials"
	"github.com/aws/amazon-ecs-agent/agent/logger/audit/request"
	log "github.com/cihub/seelog"
)

const (
	getCredentialsEventType                = "GetCredentials"
	getCredentialsTaskExecutionEventType   = "GetCredentialsExecutionRole"
	getCredentialsInvalidRoleTypeEventType = "GetCredentialsInvalidRoleType"

	// getCredentialsAuditLogVersion is the version of the audit log
	// Version '1', the fields are:
	// 1. event time
	// 2. response code
	// 3. source ip address
	// 4. url
	// 5. user agent
	// 6. arn for the entity associated with credentials
	// 7. event type ('GetCredentials')
	// 8. version
	// 9. cluster
	// 10. container instance arn

	// Version '2', following fields were modified
	// 7. event type ('GetCredentials, GetCredentialsExecutionRole')

	getCredentialsAuditLogVersion = 2
)

type commonAuditLogEntryFields struct {
	eventTime    string
	responseCode int
	srcAddr      string
	theURL       string
	userAgent    string
	arn          string
}

// GetCredentialsEventType is the type for a GetCredentials request
func GetCredentialsEventType(roleType string) string {
	switch roleType {
	case credentials.ApplicationRoleType:
		return getCredentialsEventType
	case credentials.ExecutionRoleType:
		return getCredentialsTaskExecutionEventType
	default:
		return getCredentialsInvalidRoleTypeEventType
	}
}

func (c *commonAuditLogEntryFields) string() string {
	return fmt.Sprintf("%s %d %s %s %s %s", c.eventTime, c.responseCode, c.srcAddr, c.theURL, c.userAgent, c.arn)
}

type getCredentialsAuditLogEntryFields struct {
	eventType            string
	version              int
	cluster              string
	containerInstanceArn string
}

func (g *getCredentialsAuditLogEntryFields) string() string {
	return fmt.Sprintf("%s %d %s %s", g.eventType, g.version, g.cluster, g.containerInstanceArn)
}

func constructCommonAuditLogEntryFields(r request.LogRequest, httpResponseCode int) string {
	httpRequest := r.Request
	url := httpRequest.URL.Path
	// V2CredentialsPath contains the credentials ID, which should not be logged
	if strings.HasPrefix(url, credentials.V2CredentialsPath+"/") {
		url = credentials.V2CredentialsPath
	}
	fields := &commonAuditLogEntryFields{
		eventTime:    time.Now().UTC().Format(time.RFC3339),
		responseCode: httpResponseCode,
		srcAddr:      populateField(httpRequest.RemoteAddr),
		theURL:       populateField(fmt.Sprintf(`"%s"`, url)),
		userAgent:    populateField(fmt.Sprintf(`"%s"`, httpRequest.UserAgent())),
		arn:          populateField(r.ARN),
	}
	return fields.string()
}

func constructAuditLogEntryByType(eventType string, cluster string, containerInstanceArn string) string {
	switch eventType {
	case getCredentialsEventType:
		fields := &getCredentialsAuditLogEntryFields{
			eventType:            eventType,
			version:              getCredentialsAuditLogVersion,
			cluster:              populateField(cluster),
			containerInstanceArn: populateField(containerInstanceArn),
		}
		return fields.string()
	case getCredentialsTaskExecutionEventType:
		fields := &getCredentialsAuditLogEntryFields{
			eventType:            eventType,
			version:              getCredentialsAuditLogVersion,
			cluster:              populateField(cluster),
			containerInstanceArn: populateField(containerInstanceArn),
		}
		return fields.string()
	default:
		log.Warn(fmt.Sprintf("Unknown eventType: %s", eventType))
		return ""
	}
}

func populateField(logField string) string {
	if logField == "" {
		logField = "-"
	}
	return logField
}
