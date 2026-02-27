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
	"strings"
)

const (
	defaultLogFilePath     = "/host/var/log/aws-routed-eni/ipamd.log"
	defaultLogLevel        = "Debug"
	envLogLevel            = "AWS_VPC_K8S_CNI_LOGLEVEL"
	envLogFilePath         = "AWS_VPC_K8S_CNI_LOG_FILE"
	envAdditionalLogFile   = "AWS_VPC_K8S_CNI_ADDITIONAL_LOG_FILE"
)

// Configuration stores the config for the logger
type Configuration struct {
	LogLevel               string
	LogLocation            string
	AdditionalLogLocations []string
}

// LoadLogConfig returns the log configuration
func LoadLogConfig() *Configuration {
	return &Configuration{
		LogLevel:               GetLogLevel(),
		LogLocation:            GetLogLocation(),
		AdditionalLogLocations: GetAdditionalLogLocations(),
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

// GetAdditionalLogLocations returns a list of additional log destinations
// parsed from a comma-separated environment variable.
func GetAdditionalLogLocations() []string {
	return ParseAdditionalLogLocations(os.Getenv(envAdditionalLogFile))
}

// ParseAdditionalLogLocations parses a comma-separated string of log destinations
// into a slice of trimmed, non-empty strings.
func ParseAdditionalLogLocations(val string) []string {
	if val == "" {
		return nil
	}
	var locations []string
	for _, s := range strings.Split(val, ",") {
		s = strings.TrimSpace(s)
		if s != "" {
			locations = append(locations, s)
		}
	}
	return locations
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
