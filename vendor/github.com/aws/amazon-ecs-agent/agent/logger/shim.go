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

package logger

import (
	"fmt"

	log "github.com/cihub/seelog"
)

type OldLogger interface {
	// New returns an OldLogger instance with the passed ctx plus existing ctx
	New(ctx ...interface{}) OldLogger

	// Log a message at the given level with context key/value pairs
	Debug(msg string, ctx ...interface{})
	Info(msg string, ctx ...interface{})
	Warn(msg string, ctx ...interface{})
	Error(msg string, ctx ...interface{})
	Crit(msg string, ctx ...interface{})
}

type Shim struct {
	ctx []interface{}
}

func (s *Shim) New(ctx ...interface{}) OldLogger {
	if len(ctx)%2 != 0 {
		log.Warnf("New ctx log with uneven ctx length. ctx length: %n", len(ctx))
		ctx = nil
	}
	return &Shim{
		ctx: append(s.ctx, ctx...),
	}
}

func (s *Shim) Debug(msg string, ctx ...interface{}) {
	log.Debug(s.formatMessage(msg, ctx...))
}

func (s *Shim) Info(msg string, ctx ...interface{}) {
	log.Info(s.formatMessage(msg, ctx...))
}

func (s *Shim) Warn(msg string, ctx ...interface{}) {
	log.Warn(s.formatMessage(msg, ctx...))
}

func (s *Shim) Error(msg string, ctx ...interface{}) {
	log.Error(s.formatMessage(msg, ctx...))
}

func (s *Shim) Crit(msg string, ctx ...interface{}) {
	log.Critical(s.formatMessage(msg, ctx...))
}

func (s *Shim) formatMessage(msg string, ctx ...interface{}) string {
	if len(ctx)%2 != 0 {
		log.Warnf("Log message with uneven ctx length. msg: %s, ctx length: %n", msg, len(ctx))
		return msg + " [malformed ctx omitted]"
	}
	fullCtx := append(s.ctx, ctx...)
	var retval string
	for i := 0; i < len(fullCtx); i += 2 {
		retval += fmt.Sprintf(" %v=\"%+v\"", fullCtx[i], fullCtx[i+1])
	}
	return msg + retval
}
