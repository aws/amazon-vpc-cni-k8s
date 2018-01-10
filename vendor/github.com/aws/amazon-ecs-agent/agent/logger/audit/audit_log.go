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

	"github.com/aws/amazon-ecs-agent/agent/config"
	"github.com/aws/amazon-ecs-agent/agent/logger/audit/request"
)

type auditLog struct {
	containerInstanceArn string
	cluster              string
	logger               InfoLogger
	cfg                  *config.Config
}

func NewAuditLog(containerInstanceArn string, cfg *config.Config, logger InfoLogger) AuditLogger {
	return &auditLog{
		cluster:              cfg.Cluster,
		containerInstanceArn: containerInstanceArn,
		logger:               logger,
		cfg:                  cfg,
	}
}

// Log will construct an audit log entry log and log that entry to the audit log
// using the underlying logger (which implements the audit.InfoLogger interface).
func (a *auditLog) Log(r request.LogRequest, httpResponseCode int, eventType string) {
	if !a.cfg.CredentialsAuditLogDisabled {
		auditLogEntry := constructAuditLogEntry(r, httpResponseCode, eventType, a.GetCluster(),
			a.GetContainerInstanceArn())

		a.logger.Info(auditLogEntry)
	}
}

func constructAuditLogEntry(r request.LogRequest, httpResponseCode int, eventType string,
	cluster string, containerInstanceArn string) string {
	commonAuditLogFields := constructCommonAuditLogEntryFields(r, httpResponseCode)
	auditLogTypeFields := constructAuditLogEntryByType(eventType, cluster, containerInstanceArn)

	return fmt.Sprintf("%s %s", commonAuditLogFields, auditLogTypeFields)
}

func (a *auditLog) GetCluster() string {
	return a.cluster
}

func (a *auditLog) GetContainerInstanceArn() string {
	return a.containerInstanceArn
}
