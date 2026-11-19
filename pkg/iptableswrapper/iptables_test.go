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
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"

	mock_iptableswrapper "github.com/aws/amazon-vpc-cni-k8s/pkg/iptableswrapper/mocks"
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

func TestCreateChainIfNotExist(t *testing.T) {
	createErr := errors.New("opaque chain creation failure")
	checkErr := errors.New("chain state unavailable")

	tests := []struct {
		name      string
		createErr error
		exists    bool
		checkErr  error
		wantErrs  []error
	}{
		{name: "created"},
		{name: "create failed but chain exists", createErr: createErr, exists: true},
		{name: "create failed and chain missing", createErr: createErr, wantErrs: []error{createErr}},
		{name: "create and state check failed", createErr: createErr, checkErr: checkErr, wantErrs: []error{createErr, checkErr}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ipt := mock_iptableswrapper.NewMockIPTablesIface(gomock.NewController(t))
			ipt.EXPECT().NewChain("nat", "TEST-CHAIN").Return(tt.createErr)
			if tt.createErr != nil {
				ipt.EXPECT().ChainExists("nat", "TEST-CHAIN").Return(tt.exists, tt.checkErr)
			}

			err := CreateChainIfNotExist(ipt, "nat", "TEST-CHAIN")
			if len(tt.wantErrs) == 0 {
				assert.NoError(t, err)
			}
			for _, wantErr := range tt.wantErrs {
				assert.ErrorIs(t, err, wantErr)
			}
		})
	}
}
