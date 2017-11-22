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

package types

import (
	"testing"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/stretchr/testify/assert"
)

func TestNetConf(t *testing.T) {
	testCases := []struct {
		name               string
		rawJSON            string
		expectedError      bool
		expectedBridgeName string
		expectedMTU        int
	}{
		{"empty config returns error", "", true, "", 0},
		{"empty config JSON returns error", "{}", true, "", 0},
		{"config with no bridge name returns error", `{"mtu":9100}`, true, "", 0},
		{"config with empty bridge name returns error", `{"bridge":"","mtu":9100}`, true, "", 0},
		{"valid config is parsed correctly", `{"bridge":"br0","mtu":9100}`, false, "br0", 9100},
		{"config with no mtu is parsed correctly", `{"bridge":"br0"}`, false, "br0", defaultMTU},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			args := &skel.CmdArgs{
				StdinData: []byte(tc.rawJSON),
			}
			conf, err := NewConf(args)
			if tc.expectedError {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tc.expectedBridgeName, conf.BridgeName)
			assert.Equal(t, tc.expectedMTU, conf.MTU)
		})
	}
}
