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

// Package exitcodes defines agent exit codes
package exitcodes

const (
	// ExitSuccess indicates there's no need to restart the agent and everything
	// is okay
	ExitSuccess = 0
	// ExitError (as well as unspecified exit codes) indicate a fatal error
	// occured, but the agent should be restarted
	ExitError = 1
	// ExitTerminal indicates the agent has exited unsuccessfully, but should
	// not be restarted
	ExitTerminal = 5
	// ExitUpdate indicates that the agent has written an update file to the
	// configured location and this file should be used instead when restarting
	// the agent
	ExitUpdate = 42
)
