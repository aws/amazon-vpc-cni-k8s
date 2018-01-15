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

package app

import (
	"fmt"
	"os"

	"github.com/aws/amazon-ecs-agent/agent/sighandlers/exitcodes"
	"github.com/aws/amazon-ecs-agent/agent/utils"
)

// printLicense prints the Agent's license text
func printLicense() int {
	license := utils.NewLicenseProvider()
	text, err := license.GetText()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitcodes.ExitError
	}
	fmt.Println(text)
	return exitcodes.ExitSuccess
}
