// +build !integration,!e2e

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

package cnispec

import (
	"testing"

	"github.com/containernetworking/cni/pkg/version"
	"github.com/stretchr/testify/assert"
)

func TestGetSpecVersionsSupported(t *testing.T) {
	specVersionSupported = version.PluginSupports("0.2.0")
	pluginInfo := GetSpecVersionSupported()
	supportedVersions := pluginInfo.SupportedVersions()
	assert.NotEmpty(t, supportedVersions)
	assert.Len(t, supportedVersions, 1)
	assert.Contains(t, supportedVersions, "0.2.0")
}
