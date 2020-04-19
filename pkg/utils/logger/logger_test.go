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
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap/zapcore"
)

func TestEnvLogFilePath(t *testing.T) {
	path := "/var/log/test.log"
	_ = os.Setenv(envLogFilePath, path)
	defer os.Unsetenv(envLogFilePath)

	assert.Equal(t, path, getLogFileLocation())
}

func TestGetLogFileLocationReturnsDefaultPath(t *testing.T) {
	defaultPath := "/host/var/log/aws-routed-eni/ipamd.log"
	assert.Equal(t, defaultPath, getLogFileLocation())
}

func TestLogLevelReturnsOverriddenLevel(t *testing.T) {
	_ = os.Setenv(envLogLevel, "INFO")
	defer os.Unsetenv(envLogLevel)

	var expectedLogLevel zapcore.Level
	expectedLogLevel = zapcore.InfoLevel
	inputLogLevel := getLogLevel()
	assert.Equal(t, expectedLogLevel, getZapLevel(inputLogLevel))
}

func TestLogLevelReturnsDefaultLevelWhenEnvNotSet(t *testing.T) {
	var expectedLogLevel zapcore.Level
	expectedLogLevel = zapcore.DebugLevel
	inputLogLevel := getLogLevel()
	assert.Equal(t, expectedLogLevel, getZapLevel(inputLogLevel))
}

func TestLogLevelReturnsDefaultLevelWhenEnvSetToInvalidValue(t *testing.T) {
	_ = os.Setenv(envLogLevel, "EVERYTHING")
	defer os.Unsetenv(envLogLevel)

	var expectedLogLevel zapcore.Level
	inputLogLevel := getLogLevel()
	expectedLogLevel = zapcore.DebugLevel
	assert.Equal(t, expectedLogLevel, getZapLevel(inputLogLevel))
}
