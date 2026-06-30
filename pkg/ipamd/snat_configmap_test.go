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

package ipamd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseSNATCIDRs(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "newline separated",
			input:    "10.0.0.0/16\n10.1.0.0/16\n10.2.0.0/16",
			expected: []string{"10.0.0.0/16", "10.1.0.0/16", "10.2.0.0/16"},
		},
		{
			name:     "comma separated",
			input:    "10.0.0.0/16,10.1.0.0/16,10.2.0.0/16",
			expected: []string{"10.0.0.0/16", "10.1.0.0/16", "10.2.0.0/16"},
		},
		{
			name:     "mixed separators",
			input:    "10.0.0.0/16\n10.1.0.0/16,10.2.0.0/16",
			expected: []string{"10.0.0.0/16", "10.1.0.0/16", "10.2.0.0/16"},
		},
		{
			name:     "with whitespace and empty lines",
			input:    "  10.0.0.0/16  \n\n  10.1.0.0/16\n",
			expected: []string{"10.0.0.0/16", "10.1.0.0/16"},
		},
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
		{
			name:     "only whitespace",
			input:    "  \n  \n  ",
			expected: nil,
		},
		{
			name:     "invalid CIDR skipped",
			input:    "10.0.0.0/16\nnot-a-cidr\n10.1.0.0/16",
			expected: []string{"10.0.0.0/16", "10.1.0.0/16"},
		},
		{
			name:     "IPv6 CIDR skipped",
			input:    "10.0.0.0/16\nfd00::/64\n10.1.0.0/16",
			expected: []string{"10.0.0.0/16", "10.1.0.0/16"},
		},
		{
			name:     "normalizes CIDR notation",
			input:    "10.0.1.5/16",
			expected: []string{"10.0.0.0/16"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseSNATCIDRs(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
