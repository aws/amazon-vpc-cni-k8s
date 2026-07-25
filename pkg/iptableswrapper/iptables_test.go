// Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
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

package iptableswrapper

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestParseIptablesMode covers parseIptablesMode against captured outputs
// from real `iptables --version` runs across the AMIs vpc-cni supports.
func TestParseIptablesMode(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   IptablesMode
	}{
		{
			// AL2023 default, Bottlerocket >= 1.34, Ubuntu 24.04
			name:   "nf_tables backend",
			output: "iptables v1.8.10 (nf_tables)\n",
			want:   IptablesModeNFT,
		},
		{
			// AL2 default, AL2023 with iptables-legacy alternative
			name:   "legacy backend",
			output: "iptables v1.8.7 (legacy)\n",
			want:   IptablesModeLegacy,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseIptablesMode(tt.output)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIptablesModeIsNFTables(t *testing.T) {
	assert.True(t, IptablesModeNFT.IsNFTables())
	assert.False(t, IptablesModeLegacy.IsNFTables())
}
