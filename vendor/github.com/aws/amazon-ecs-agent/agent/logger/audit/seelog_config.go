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

package audit

import "github.com/aws/amazon-ecs-agent/agent/config"

func AuditLoggerConfig(cfg *config.Config) string {
	config := `
	<seelog type="asyncloop" minlevel="info">
		<outputs formatid="main">
			<console />`
	if cfg.CredentialsAuditLogFile != "" {
		config += `<rollingfile filename="` + cfg.CredentialsAuditLogFile + `" type="date"
			 datepattern="2006-01-02-15" archivetype="none" maxrolls="24" />`
	}
	config += `
		</outputs>
		<formats>
			<format id="main" format="%Msg%n" />
		</formats>
	</seelog>
`
	return config
}
