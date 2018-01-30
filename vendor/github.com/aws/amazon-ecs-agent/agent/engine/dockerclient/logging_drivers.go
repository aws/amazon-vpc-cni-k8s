// Copyright 2014-2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

package dockerclient

type LoggingDriver string

const (
	JSONFileDriver   LoggingDriver = "json-file"
	SyslogDriver     LoggingDriver = "syslog"
	JournaldDriver   LoggingDriver = "journald"
	GelfDriver       LoggingDriver = "gelf"
	FluentdDriver    LoggingDriver = "fluentd"
	AWSLogsDriver    LoggingDriver = "awslogs"
	SplunklogsDriver LoggingDriver = "splunk"
	LogentriesDriver LoggingDriver = "logentries"
	SumoLogicDriver  LoggingDriver = "sumologic"
	NoneDriver       LoggingDriver = "none"
)

var LoggingDriverMinimumVersion = map[LoggingDriver]DockerVersion{
	JSONFileDriver:   Version_1_18,
	SyslogDriver:     Version_1_18,
	JournaldDriver:   Version_1_19,
	GelfDriver:       Version_1_20,
	FluentdDriver:    Version_1_20,
	AWSLogsDriver:    Version_1_21,
	SplunklogsDriver: Version_1_22,
	LogentriesDriver: Version_1_25,
	SumoLogicDriver:  Version_1_29,
	NoneDriver:       Version_1_19,
}
