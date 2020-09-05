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

package logger

import (
	"os"
)

const (
	defaultLogFilePath = "/host/var/log/aws-routed-eni/ipamd.log"
	defaultLogLevel    = "Debug"
	envLogLevel        = "AWS_VPC_K8S_CNI_LOGLEVEL"
	envLogFilePath     = "AWS_VPC_K8S_CNI_LOG_FILE"
)

// Configuration stores the config for the logger
type Configuration struct {
	LogLevel    string
	LogLocation string
}

// LoadLogConfig returns the log configuration
func LoadLogConfig() *Configuration {
	return &Configuration{
		LogLevel:    GetLogLevel(),
		LogLocation: GetLogLocation(),
	}
}

// GetLogLocation returns the log file path
func GetLogLocation() string {
	logFilePath := os.Getenv(envLogFilePath)
	if logFilePath == "" {
		logFilePath = defaultLogFilePath
	}
	return logFilePath
}

// GetLogLevel returns the log level
func GetLogLevel() string {
	logLevel := os.Getenv(envLogLevel)
	switch logLevel {
	case "":
		logLevel = defaultLogLevel
		return logLevel
	default:
		return logLevel
	}
}
