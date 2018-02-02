// Copyright 2014-2015 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

package logger

import (
	"os"
	"strings"
	"sync"

	log "github.com/cihub/seelog"
)

const (
	LOGLEVEL_ENV_VAR = "ECS_LOGLEVEL"
	LOGFILE_ENV_VAR  = "ECS_LOGFILE"

	DEFAULT_LOGLEVEL = "info"
)

var logfile string
var level string
var levelLock sync.RWMutex
var levels map[string]string
var logger OldLogger

// Initialize this logger once
var once sync.Once

func init() {
	once.Do(initLogger)
}

func initLogger() {
	levels = map[string]string{
		"debug": "debug",
		"info":  "info",
		"warn":  "warn",
		"error": "error",
		"crit":  "critical",
		"none":  "off",
	}

	level = DEFAULT_LOGLEVEL

	logger = &Shim{}

	envLevel := os.Getenv(LOGLEVEL_ENV_VAR)

	logfile = os.Getenv(LOGFILE_ENV_VAR)
	SetLevel(envLevel)
	registerPlatformLogger()
	reloadConfig()
}

func reloadConfig() {
	logger, err := log.LoggerFromConfigAsString(loggerConfig())
	if err == nil {
		log.ReplaceLogger(logger)
	} else {
		log.Error(err)
	}
}

// SetLevel sets the log level for logging
func SetLevel(logLevel string) {
	parsedLevel, ok := levels[strings.ToLower(logLevel)]

	if ok {
		levelLock.Lock()
		defer levelLock.Unlock()
		level = parsedLevel
		reloadConfig()
	}
}

// GetLevel gets the log level
func GetLevel() string {
	levelLock.RLock()
	defer levelLock.RUnlock()

	return level
}

// ForModule returns an OldLogger instance.  OldLogger is deprecated and kept
// for compatibility reasons.  Prefer using Seelog directly.
func ForModule(module string) OldLogger {
	once.Do(initLogger)
	return logger.New("module", module)
}
