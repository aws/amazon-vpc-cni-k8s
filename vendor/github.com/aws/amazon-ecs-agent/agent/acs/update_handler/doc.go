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

// Package updater handles requests to update the agent.
//
// Requests are assumed to be two-phase where the phases consist of a 'download'
// followed by an 'update'. Either of these steps may fail.
// The agent is responsible for, on download, correctly downloading the
// referenced file and validating it against the provided checksum. However,
// the actual 'update' component is handled by signaling a watching process via
// exit code.
package updater
