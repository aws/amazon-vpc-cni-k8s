// Copyright 2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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
	"strconv"
)

// Conditional makes it possible to understand if a variable was set explicitly or relies on a default setting
type Conditional int

const (
	_ Conditional = iota
	ExplicitlyEnabled
	ExplicitlyDisabled
	DefaultEnabled
)

// Enabled is a convenience function for when consumers don't care if the value is implicit or explicit
func (b Conditional) Enabled() bool {
	return b == ExplicitlyEnabled || b == DefaultEnabled
}

// MarshalJSON is used to serialize the type to json, per the Marshaller interface
func (b Conditional) MarshalJSON() ([]byte, error) {
	switch b {
	case ExplicitlyEnabled:
		return json.Marshal(true)
	case ExplicitlyDisabled:
		return json.Marshal(false)
	default:
		return json.Marshal(nil)
	}
}

// UnmarshalJSON is used to deserialize json types into Conditional, per the Unmarshaller interface
func (b *Conditional) UnmarshalJSON(jsonData []byte) error {
	jsonString := string(jsonData)
	jsonBool, err := strconv.ParseBool(jsonString)
	if err != nil && jsonString != "null" {
		return err
	}

	if jsonString == "" || jsonString == "null" {
		*b = DefaultEnabled
	} else if jsonBool {
		*b = ExplicitlyEnabled
	} else {
		*b = ExplicitlyDisabled
	}

	return nil
}
