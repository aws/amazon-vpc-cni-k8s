// Copyright 2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

package version

import (
	"encoding/json"
	"strings"

	"github.com/pkg/errors"
)

// Command is the option for plugin to print the version
const Command = "version"

// Version is the version number of the repository
var Version string

// GitPorcelain indicates the output of the git status --porcelain command to
// determine the cleanliness of the git repo when this plugin was built
var GitPorcelain string

// GitShortHash is the short hash of this repository build
var GitShortHash string

type versionInfo struct {
	Version      string `json:"version"`
	Dirty        bool   `json:"dirty"`
	GitShortHash string `json:"gitShortHash"`
}

// String returns a JSON version string from the versionInfo type
func String() (string, error) {
	dirty := true
	if strings.TrimSpace(GitPorcelain) == "0" {
		dirty = false
	}

	verInfo := versionInfo{
		Version:      Version,
		Dirty:        dirty,
		GitShortHash: GitShortHash,
	}

	verInfoJSON, err := json.Marshal(verInfo)
	if err != nil {
		return "", errors.Wrapf(err, "version: failed to marshal version info: %v", verInfo)
	}

	return string(verInfoJSON), nil
}
