// +build windows

// Copyright 2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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
	"github.com/cihub/seelog"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc/eventlog"
)

/*
TODO: Make this whole thing better

What's here right now is a stub just so that agent logs can appear in the Event Log.  Longer term, we should do a few
things to make this better:

1) Don't use init() and a package-global variable
2) Conform to the MSDN guidelines about event log data.

References to MSDN guidelines can be found here:
* https://msdn.microsoft.com/en-us/library/windows/desktop/aa363632(v=vs.85).aspx
* https://msdn.microsoft.com/en-us/library/windows/desktop/aa363648(v=vs.85).aspx
* https://msdn.microsoft.com/en-us/library/windows/desktop/aa363661(v=vs.85).aspx
* https://msdn.microsoft.com/en-us/library/windows/desktop/aa363669(v=vs.85).aspx
*/

const (
	eventLogName = "AmazonECSAgent"
	eventLogID   = 999
)

// eventLogReceiver fulfills the seelog.CustomReceiver interface
type eventLogReceiver struct{}

var eventLog *eventlog.Log

func init() {
	eventlog.InstallAsEventCreate(eventLogName, windows.EVENTLOG_INFORMATION_TYPE|windows.EVENTLOG_WARNING_TYPE|windows.EVENTLOG_ERROR_TYPE)
	var err error
	eventLog, err = eventlog.Open(eventLogName)
	if err != nil {
		panic(err)
	}
}

// registerPlatformLogger registers the eventLogReceiver
func registerPlatformLogger() {
	seelog.RegisterReceiver("wineventlog", &eventLogReceiver{})
}

// platformLogConfig exposes log configuration for the event log receiver
func platformLogConfig() string {
	return `<custom name="wineventlog" formatid="windows" />`
}

// ReceiveMessage receives a log line from seelog and emits it to the Windows event log
func (r *eventLogReceiver) ReceiveMessage(message string, level seelog.LogLevel, context seelog.LogContextInterface) error {
	switch level {
	case seelog.DebugLvl, seelog.InfoLvl:
		return eventLog.Info(eventLogID, message)
	case seelog.WarnLvl:
		return eventLog.Warning(eventLogID, message)
	case seelog.ErrorLvl, seelog.CriticalLvl:
		return eventLog.Error(eventLogID, message)
	}
	return nil
}

func (r *eventLogReceiver) AfterParse(initArgs seelog.CustomReceiverInitArgs) error { return nil }
func (r *eventLogReceiver) Flush()                                                  {}
func (r *eventLogReceiver) Close() error                                            { return nil }
