// Copyright 2015 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

package config

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConditional(t *testing.T) {
	x := DefaultEnabled
	y := ExplicitlyEnabled
	z := ExplicitlyDisabled

	assert.True(t, x.Enabled(), "DefaultEnabled is enabled")
	assert.True(t, y.Enabled(), "ExplicitlyEnabled is enabled")
	assert.False(t, z.Enabled(), "ExplicitlyDisabled is not enabled")
}

func TestConditionalImplements(t *testing.T) {
	assert.Implements(t, (*json.Marshaler)(nil), (Conditional)(1))
	assert.Implements(t, (*json.Unmarshaler)(nil), (*Conditional)(nil))
}

// main conversion cases for Conditional marshalling
var cases = []struct {
	defaultBool Conditional
	bytes       []byte
}{
	{ExplicitlyEnabled, []byte("true")},
	{ExplicitlyDisabled, []byte("false")},
	{DefaultEnabled, []byte("null")},
}

func TestConditionalMarshal(t *testing.T) {
	for _, tc := range cases {
		t.Run(string(tc.bytes), func(t *testing.T) {
			m, err := json.Marshal(tc.defaultBool)
			assert.NoError(t, err)
			assert.Equal(t, tc.bytes, m)
		})
	}
}

func TestConditionalUnmarshal(t *testing.T) {
	for _, tc := range cases {
		t.Run(string(tc.bytes), func(t *testing.T) {
			var target Conditional
			err := json.Unmarshal(tc.bytes, &target)
			assert.NoError(t, err)
			assert.Equal(t, tc.defaultBool, target)
		})
	}
}
