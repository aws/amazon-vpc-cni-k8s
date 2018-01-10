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
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aws/amazon-ecs-agent/agent/config"
	"github.com/aws/amazon-ecs-agent/agent/credentials"
	"github.com/aws/amazon-ecs-agent/agent/handlers"
	"github.com/aws/amazon-ecs-agent/agent/logger/audit"
	"github.com/aws/amazon-ecs-agent/agent/logger/audit/request"
	"github.com/aws/amazon-ecs-agent/agent/utils"
	log "github.com/cihub/seelog"
)

const (
	// readTimeout specifies the maximum duration before timing out read of the request.
	// The value is set to 5 seconds as per AWS SDK defaults.
	readTimeout = 5 * time.Second
	// writeTimeout specifies the maximum duration before timing out write of the response.
	// The value is set to 5 seconds as per AWS SDK defaults.
	writeTimeout = 5 * time.Second

	// Credentials API versions
	apiVersion1 = 1
	apiVersion2 = 2

	// Error Types

	// NoIDInRequest is the error code indicating that no ID was specified
	NoIDInRequest = "NoIdInRequest"
	// InvalidIDInRequest is the error code indicating that the ID was invalid
	InvalidIDInRequest = "InvalidIdInRequest"
	// NoCredentialsAssociated is the error code indicating no credentials are
	// associated with the specified ID
	NoCredentialsAssociated = "NoCredentialsAssociated"
	// CredentialsUninitialized is the error code indicating that credentials were
	// not properly initialized.  This may happen immediately after the agent is
	// started, before it has completed state reconciliation.
	CredentialsUninitialized = "CredentialsUninitialized"
	// InternalServerError is the error indicating something generic went wrong
	InternalServerError = "InternalServerError"
)

// errorMessage is used to store the human-readable error Code and a descriptive Message
//  that describes the error. This struct is marshalled and returned in the HTTP response.
type errorMessage struct {
	Code          string `json:"code"`
	Message       string `json:"message"`
	httpErrorCode int
}

// ServeHTTP serves IAM Role Credentials for Tasks being managed by the agent.
func ServeHTTP(credentialsManager credentials.Manager, containerInstanceArn string, cfg *config.Config) {
	// Create and initialize the audit log
	// TODO Use seelog's programmatic configuration instead of xml.
	logger, err := log.LoggerFromConfigAsString(audit.AuditLoggerConfig(cfg))
	if err != nil {
		log.Errorf("Error initializing the audit log: %v", err)
		// If the logger cannot be initialized, use the provided dummy seelog.LoggerInterface, seelog.Disabled.
		logger = log.Disabled
	}

	auditLogger := audit.NewAuditLog(containerInstanceArn, cfg, logger)

	server := setupServer(credentialsManager, auditLogger)

	for {
		utils.RetryWithBackoff(utils.NewSimpleBackoff(time.Second, time.Minute, 0.2, 2), func() error {
			// TODO, make this cancellable and use the passed in context;
			err := server.ListenAndServe()
			if err != nil {
				log.Errorf("Error running http api: %v", err)
			}
			return err
		})
	}
}

// setupServer starts the HTTP server for serving IAM Role Credentials for Tasks.
func setupServer(credentialsManager credentials.Manager, auditLogger audit.AuditLogger) *http.Server {
	serverMux := http.NewServeMux()
	serverMux.HandleFunc(credentials.V1CredentialsPath, credentialsV1V2RequestHandler(credentialsManager, auditLogger, getV1CredentialsID, apiVersion1))
	serverMux.HandleFunc(credentials.V2CredentialsPath+"/", credentialsV1V2RequestHandler(credentialsManager, auditLogger, getV2CredentialsID, apiVersion2))

	// Log all requests and then pass through to serverMux
	loggingServeMux := http.NewServeMux()
	loggingServeMux.Handle("/", handlers.NewLoggingHandler(serverMux))

	server := http.Server{
		Addr:         config.AgentCredentialsAddress + ":" + strconv.Itoa(config.AgentCredentialsPort),
		Handler:      loggingServeMux,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
	}

	return &server
}

// credentialsV1V2RequestHandler creates response for the 'v1/credentials' and 'v2/credentials' APIs. It returns a JSON response
// containing credentials when found. The HTTP status code of 400 is returned otherwise.
func credentialsV1V2RequestHandler(credentialsManager credentials.Manager, auditLogger audit.AuditLogger, idFunc func(*http.Request) string, apiVersion int) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		credentialsID := idFunc(r)
		jsonResponse, arn, roleType, errorMessage, err := processCredentialsV1V2Request(credentialsManager, r, credentialsID, apiVersion)
		if err != nil {
			jsonMsg, _ := json.Marshal(errorMessage)
			writeCredentialsV1V2RequestResponse(w, r, errorMessage.httpErrorCode, audit.GetCredentialsEventType(roleType), arn, auditLogger, jsonMsg)
			return
		}

		writeCredentialsV1V2RequestResponse(w, r, http.StatusOK, audit.GetCredentialsEventType(roleType), arn, auditLogger, jsonResponse)
	}
}

func getV1CredentialsID(r *http.Request) string {
	credentialsID, ok := handlers.ValueFromRequest(r, credentials.CredentialsIDQueryParameterName)
	if !ok {
		return ""
	}
	return credentialsID
}

func getV2CredentialsID(r *http.Request) string {
	if strings.HasPrefix(r.URL.Path, credentials.CredentialsPath+"/") {
		return r.URL.String()[len(credentials.V2CredentialsPath+"/"):]
	}
	return ""
}

func writeCredentialsV1V2RequestResponse(w http.ResponseWriter, r *http.Request, httpStatusCode int, eventType string, arn string, auditLogger audit.AuditLogger, message []byte) {
	auditLogger.Log(request.LogRequest{Request: r, ARN: arn}, httpStatusCode, eventType)

	writeJSONToResponse(w, httpStatusCode, message)
}

// processCredentialsV1V2Request returns the response json containing credentials for the credentials id in the request
func processCredentialsV1V2Request(credentialsManager credentials.Manager, r *http.Request, credentialsID string, apiVersion int) ([]byte, string, string, *errorMessage, error) {
	errPrefix := fmt.Sprintf("CredentialsV%dRequest: ", apiVersion)
	if credentialsID == "" {
		errText := errPrefix + "No ID in the request"
		log.Infof("%s. Request IP Address: %s", errText, r.RemoteAddr)
		msg := &errorMessage{
			Code:          NoIDInRequest,
			Message:       errText,
			httpErrorCode: http.StatusBadRequest,
		}
		return nil, "", "", msg, errors.New(errText)
	}

	credentials, ok := credentialsManager.GetTaskCredentials(credentialsID)
	if !ok {
		errText := errPrefix + "ID not found"
		log.Infof("%s. Request IP Address: %s", errText, r.RemoteAddr)
		msg := &errorMessage{
			Code:          InvalidIDInRequest,
			Message:       errText,
			httpErrorCode: http.StatusBadRequest,
		}
		return nil, "", "", msg, errors.New(errText)
	}

	if utils.ZeroOrNil(credentials) {
		// This can happen when the agent is restarted and is reconciling its state.
		errText := errPrefix + "Credentials uninitialized for ID"
		log.Infof("%s. Request IP Address: %s", errText, r.RemoteAddr)
		msg := &errorMessage{
			Code:          CredentialsUninitialized,
			Message:       errText,
			httpErrorCode: http.StatusServiceUnavailable,
		}
		return nil, "", "", msg, errors.New(errText)
	}

	credentialsJSON, err := json.Marshal(credentials.IAMRoleCredentials)
	if err != nil {
		errText := errPrefix + "Error marshaling credentials"
		log.Errorf("%s. Request IP Address: %s", errText, r.RemoteAddr)
		msg := &errorMessage{
			Code:          InternalServerError,
			Message:       "Internal server error",
			httpErrorCode: http.StatusInternalServerError,
		}
		return nil, "", "", msg, errors.New(errText)
	}

	//Success
	return credentialsJSON, credentials.ARN, credentials.IAMRoleCredentials.RoleType, nil, nil
}

func writeJSONToResponse(w http.ResponseWriter, httpStatusCode int, jsonMessage []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpStatusCode)
	_, err := w.Write(jsonMessage)
	if err != nil {
		log.Error("handlers/credentials: Error writing json error message to ResponseWriter")
	}
}
