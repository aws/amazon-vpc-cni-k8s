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
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSensitiveRawMessageImplements(t *testing.T) {
	assert.Implements(t, (*fmt.Stringer)(nil), SensitiveRawMessage{})
	assert.Implements(t, (*fmt.GoStringer)(nil), SensitiveRawMessage{})
	assert.Implements(t, (*json.Marshaler)(nil), SensitiveRawMessage{})
	assert.Implements(t, (*json.Unmarshaler)(nil), &SensitiveRawMessage{})
}

func TestSensitiveRawMessage(t *testing.T) {
	sensitive := NewSensitiveRawMessage(json.RawMessage("secret"))

	for _, str := range []string{
		sensitive.String(),
		sensitive.GoString(),
		fmt.Sprintf("%v", sensitive),
		fmt.Sprintf("%#v", sensitive),
		fmt.Sprint(sensitive),
	} {
		assert.Equal(t, "[redacted]", str, "expected redacted")
	}
}

// TestEmptySensitiveRawMessage tests the message content is empty
func TestEmptySensitiveRawMessage(t *testing.T) {
	sensitive := NewSensitiveRawMessage(json.RawMessage(""))

	assert.Nil(t, sensitive, "empty message should return nil")
}
