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
	"sync"
)

const pluginBinaryName = "aws-cni"

var once sync.Once

//Log is global variable so that log functions can be directly accessed
var log Logger

//Fields Type to pass when we want to call WithFields for structured logging
type Fields map[string]interface{}

//Logger is our contract for the logger
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

// Get returns an default instance of the zap logger
func Get() Logger {
	var logf = &structuredLogger{}
	if logf.isEmpty() {
		logConfig := LoadLogConfig()
		log = New(logConfig)
		return log
	}
	log = logf
	return log
}

func (logf *structuredLogger) isEmpty() bool {
	return logf.zapLogger == nil
}

//New logger initializes logger
func New(inputLogConfig *Configuration) Logger {
	if inputLogConfig.BinaryName != pluginBinaryName {
		logConfig := LoadLogConfig()
		log = logConfig.newZapLogger()
		return log
	}

	log = inputLogConfig.newZapLogger()
	return log
}
