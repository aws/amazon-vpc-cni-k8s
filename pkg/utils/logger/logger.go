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

// Log is global variable so that log functions can be directly accessed
var log Logger

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

// Get returns the current logger instance, building the default one on first
// use if nothing has been configured yet.
//
// Construction no longer emits a log line (see issue #3893): writing at build
// time made lumberjack create the log file eagerly, which fails on a read-only
// root (Bottlerocket) when short-lived CNI plugin binaries inherit the ipamd
// default log path. The first write must come from real log calls only.
func Get() Logger {
	if log == nil {
		logConfig := LoadLogConfig()
		log = New(logConfig)
	}
	return log
}

// New logger initializes logger
func New(inputLogConfig *Configuration) Logger {
	log = inputLogConfig.newZapLogger()
	return log
}

// Returns a Logger proxy that resolves the underlying logger via Get() on
// every call instead of capturing it eagerly.
func Lazy() Logger {
	return &lazyLogger{}
}

// lazyLogger is a stateless proxy. Each method resolves the active logger with
// Get() at call time and forwards to it, so no logger is constructed until a
// log method is actually invoked.
type lazyLogger struct{}

func (l *lazyLogger) Debugf(format string, args ...interface{}) { Get().Debugf(format, args...) }

func (l *lazyLogger) Debug(msg string) { Get().Debug(msg) }

func (l *lazyLogger) Infof(format string, args ...interface{}) { Get().Infof(format, args...) }

func (l *lazyLogger) Info(msg string) { Get().Info(msg) }

func (l *lazyLogger) Warnf(format string, args ...interface{}) { Get().Warnf(format, args...) }

func (l *lazyLogger) Warn(msg string) { Get().Warn(msg) }

func (l *lazyLogger) Errorf(format string, args ...interface{}) { Get().Errorf(format, args...) }

func (l *lazyLogger) Error(msg string) { Get().Error(msg) }

func (l *lazyLogger) Fatalf(format string, args ...interface{}) { Get().Fatalf(format, args...) }

func (l *lazyLogger) Panicf(format string, args ...interface{}) { Get().Panicf(format, args...) }

func (l *lazyLogger) WithFields(keyValues Fields) Logger { return Get().WithFields(keyValues) }
