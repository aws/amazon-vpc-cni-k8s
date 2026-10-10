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

// Package logger is the CNI Logger interface, using zap
package logger

import "sync"

var (
	// logInstance is the process-wide singleton logger.
	logInstance Logger
	logOnce     sync.Once
)

// Fields Type to pass when we want to call WithFields for structured logging
type Fields map[string]interface{}

// Logger is our contract for the logger
type Logger interface {
	Debugf(format string, args ...interface{})

	Debug(format string)

	Infof(format string, args ...interface{})

	Info(format string)

	Warnf(format string, args ...interface{})

	Warn(format string)

	Errorf(format string, args ...interface{})

	Error(format string)

	Fatalf(format string, args ...interface{})

	Panicf(format string, args ...interface{})

	WithFields(keyValues Fields) Logger
}

// Get returns the process-wide singleton logger, initializing it on the first call.
// Safe for concurrent use.
func Get() Logger {
	logOnce.Do(func() {
		logConfig := LoadLogConfig()
		logInstance = logConfig.newZapLogger()
		logInstance.Info("Constructed new logger instance")
	})
	return logInstance
}

// New creates a logger with the given configuration and returns it.
// It does not replace the singleton returned by Get; use Get for the shared
// process logger. New is intended for short-lived processes (CNI plugins) that
// need an independently configured logger.
func New(inputLogConfig *Configuration) Logger {
	l := inputLogConfig.newZapLogger()
	l.Info("Constructed new logger instance")
	return l
}
