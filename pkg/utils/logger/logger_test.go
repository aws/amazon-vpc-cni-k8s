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
	"gopkg.in/natefinch/lumberjack.v2"
)

func TestEnvLogFilePath(t *testing.T) {
	path := "/var/log/test.log"
	_ = os.Setenv(envLogFilePath, path)
	defer os.Unsetenv(envLogFilePath)

	assert.Equal(t, path, GetLogLocation())
}

func TestLoggerGetSameInstance(t *testing.T) {
	log1 := Get()
	log2 := Get()

	assert.True(t, log1 == log2)
}

func TestLoggerNewAndGetSameInstance(t *testing.T) {
	logConfig := LoadLogConfig()
	log1 := New(logConfig)
	log2 := Get()

	assert.True(t, log1 == log2)
}

func TestGetLogFileLocationReturnsDefaultPath(t *testing.T) {
	defaultPath := "/host/var/log/aws-routed-eni/ipamd.log"
	assert.Equal(t, defaultPath, GetLogLocation())
}

func TestLogLevelReturnsOverriddenLevel(t *testing.T) {
	_ = os.Setenv(envLogLevel, "INFO")
	defer os.Unsetenv(envLogLevel)

	expectedLogLevel := zapcore.InfoLevel
	inputLogLevel := GetLogLevel()
	assert.Equal(t, expectedLogLevel, getZapLevel(inputLogLevel))
}

func TestLogLevelReturnsDefaultLevelWhenEnvNotSet(t *testing.T) {
	expectedLogLevel := zapcore.DebugLevel
	inputLogLevel := GetLogLevel()
	assert.Equal(t, expectedLogLevel, getZapLevel(inputLogLevel))
}

func TestLogLevelReturnsDefaultLevelWhenEnvSetToInvalidValue(t *testing.T) {
	_ = os.Setenv(envLogLevel, "EVERYTHING")
	defer os.Unsetenv(envLogLevel)

	var expectedLogLevel zapcore.Level
	inputLogLevel := GetLogLevel()
	expectedLogLevel = zapcore.DebugLevel
	assert.Equal(t, expectedLogLevel, getZapLevel(inputLogLevel))
}

func TestGetPluginLogFilePathEmpty(t *testing.T) {
	expectedWriter := zapcore.Lock(os.Stderr)
	inputPluginLogFile := ""
	assert.Equal(t, expectedWriter, getPluginLogFilePath(inputPluginLogFile))
}

func TestGetPluginLogFilePathStdout(t *testing.T) {
	expectedWriter := zapcore.Lock(os.Stdout)
	inputPluginLogFile := "stdout"
	assert.Equal(t, expectedWriter, getPluginLogFilePath(inputPluginLogFile))
}

func TestGetPluginLogFilePath(t *testing.T) {
	inputPluginLogFile := "/var/log/aws-routed-eni/plugin.log"
	expectedLumberJackLogger := &lumberjack.Logger{
		Filename:   "/var/log/aws-routed-eni/plugin.log",
		MaxSize:    100,
		MaxBackups: 5,
		MaxAge:     30,
		Compress:   true,
	}
	assert.Equal(t, zapcore.AddSync(expectedLumberJackLogger), getPluginLogFilePath(inputPluginLogFile))
}

func TestParseAdditionalLogLocationsEmpty(t *testing.T) {
	assert.Nil(t, ParseAdditionalLogLocations(""))
}

func TestParseAdditionalLogLocationsSingle(t *testing.T) {
	assert.Equal(t, []string{"stdout"}, ParseAdditionalLogLocations("stdout"))
}

func TestParseAdditionalLogLocationsMultiple(t *testing.T) {
	result := ParseAdditionalLogLocations("stdout, /var/log/extra.log, stderr")
	assert.Equal(t, []string{"stdout", "/var/log/extra.log", "stderr"}, result)
}

func TestParseAdditionalLogLocationsTrimsWhitespace(t *testing.T) {
	result := ParseAdditionalLogLocations("  stdout , , /tmp/test.log  ")
	assert.Equal(t, []string{"stdout", "/tmp/test.log"}, result)
}

func TestGetAdditionalLogLocationsFromEnv(t *testing.T) {
	_ = os.Setenv(envAdditionalLogFile, "stdout,/var/log/extra.log")
	defer os.Unsetenv(envAdditionalLogFile)

	result := GetAdditionalLogLocations()
	assert.Equal(t, []string{"stdout", "/var/log/extra.log"}, result)
}

func TestGetAdditionalLogLocationsNotSet(t *testing.T) {
	assert.Nil(t, GetAdditionalLogLocations())
}

func TestMultiSinkLoggerFileAndStdout(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "cni-log-test-*.log")
	assert.NoError(t, err)
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	logConfig := &Configuration{
		LogLevel:               "Debug",
		LogLocation:            tmpFile.Name(),
		AdditionalLogLocations: []string{"stdout"},
	}
	l := New(logConfig)
	assert.NotNil(t, l)

	// Write a log line and verify it appears in the file
	l.Info("multi-sink test message")

	content, err := os.ReadFile(tmpFile.Name())
	assert.NoError(t, err)
	assert.Contains(t, string(content), "multi-sink test message")
}

func TestMultiSinkLoggerTwoFiles(t *testing.T) {
	tmpFile1, err := os.CreateTemp("", "cni-log-test1-*.log")
	assert.NoError(t, err)
	defer os.Remove(tmpFile1.Name())
	tmpFile1.Close()

	tmpFile2, err := os.CreateTemp("", "cni-log-test2-*.log")
	assert.NoError(t, err)
	defer os.Remove(tmpFile2.Name())
	tmpFile2.Close()

	logConfig := &Configuration{
		LogLevel:               "Debug",
		LogLocation:            tmpFile1.Name(),
		AdditionalLogLocations: []string{tmpFile2.Name()},
	}
	l := New(logConfig)
	l.Info("dual-file test message")

	content1, err := os.ReadFile(tmpFile1.Name())
	assert.NoError(t, err)
	assert.Contains(t, string(content1), "dual-file test message")

	content2, err := os.ReadFile(tmpFile2.Name())
	assert.NoError(t, err)
	assert.Contains(t, string(content2), "dual-file test message")
}

func TestSingleSinkBackwardCompat(t *testing.T) {
	// When AdditionalLogLocations is nil, behavior is unchanged
	logConfig := &Configuration{
		LogLevel:    "Debug",
		LogLocation: "stdout",
	}
	l := New(logConfig)
	assert.NotNil(t, l)
}

func TestLoadLogConfigIncludesAdditionalLogLocations(t *testing.T) {
	_ = os.Setenv(envAdditionalLogFile, "stdout")
	defer os.Unsetenv(envAdditionalLogFile)

	config := LoadLogConfig()
	assert.Equal(t, []string{"stdout"}, config.AdditionalLogLocations)
}
