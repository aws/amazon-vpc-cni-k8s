// Copyright 2014-2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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
)

// SensitiveRawMessage is a struct to store some data that should not be logged
// or printed.
// This struct is a Stringer which will not print its contents with 'String'.
// It is a json.Marshaler and json.Unmarshaler and will present its actual
// contents in plaintext when read/written from/to json.
type SensitiveRawMessage struct {
	contents json.RawMessage
}

// NewSensitiveRawMessage returns a new encapsulated json.RawMessage or nil if
// the data is empty. It cannot be accidentally logged via .String/.GoString/%v/%#v
func NewSensitiveRawMessage(data json.RawMessage) *SensitiveRawMessage {
	if len(data) == 0 {
		return nil
	}
	return &SensitiveRawMessage{contents: data}
}

func (data SensitiveRawMessage) String() string {
	return "[redacted]"
}

func (data SensitiveRawMessage) GoString() string {
	return "[redacted]"
}

func (data SensitiveRawMessage) Contents() json.RawMessage {
	return data.contents
}

func (data SensitiveRawMessage) MarshalJSON() ([]byte, error) {
	return data.contents, nil
}

func (data *SensitiveRawMessage) UnmarshalJSON(jsonData []byte) error {
	data.contents = json.RawMessage(jsonData)
	return nil
}
