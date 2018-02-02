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

func loggerConfig() string {
	config := `
	<seelog type="asyncloop" minlevel="` + level + `">
		<outputs formatid="main">
			<console />`
	config += platformLogConfig()
	if logfile != "" {
		config += `<rollingfile filename="` + logfile + `" type="date"
			 datepattern="2006-01-02-15" archivetype="none" maxrolls="24" />`
	}
	config += `
		</outputs>
		<formats>
			<format id="main" format="%UTCDate(2006-01-02T15:04:05Z07:00) [%LEVEL] %Msg%n" />
			<format id="windows" format="%Msg" />
		</formats>
	</seelog>
`
	return config
}
