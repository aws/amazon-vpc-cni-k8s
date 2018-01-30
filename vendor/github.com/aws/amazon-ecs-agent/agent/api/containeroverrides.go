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

package api

import (
	"encoding/json"
	"errors"

	"github.com/aws/amazon-ecs-agent/agent/utils"
)

// ContainerOverrides are overrides applied to the container
type ContainerOverrides struct {
	Command *[]string `json:"command"`
}

// ContainerOverridesCopy is a  type alias that doesn't have a custom unmarshaller so we
// can unmarshal ContainerOverrides data into something without recursing
type ContainerOverridesCopy ContainerOverrides

// UnmarshalJSON overrides the logic for parsing the JSON-encoded
// ContainerOverrides data
// This custom unmarshaller is needed because the json sent to us as a string
// rather than a fully typed object. We support both formats in the hopes that
// one day everything will be fully typed
// Note: the `json:",string"` tag DOES NOT apply here; it DOES NOT work with
// struct types, only ints/floats/etc. We're basically doing that though
// We also intentionally fail if there are any keys we were unable to unmarshal
// into our struct
func (overrides *ContainerOverrides) UnmarshalJSON(b []byte) error {
	regular := ContainerOverridesCopy{}

	// Try to do it the strongly typed way first
	err := json.Unmarshal(b, &regular)
	if err == nil {
		err = utils.CompleteJsonUnmarshal(b, regular)
		if err == nil {
			*overrides = ContainerOverrides(regular)
			return nil
		}
		err = utils.NewMultiError(errors.New("Error unmarshalling ContainerOverrides"), err)
	}

	// Now the strongly typed way
	var str string
	err2 := json.Unmarshal(b, &str)
	if err2 != nil {
		return utils.NewMultiError(errors.New("Could not unmarshal ContainerOverrides into either an object or string respectively"), err, err2)
	}

	// We have a string, let's try to unmarshal that into a typed object
	err3 := json.Unmarshal([]byte(str), &regular)
	if err3 == nil {
		err3 = utils.CompleteJsonUnmarshal([]byte(str), regular)
		if err3 == nil {
			*overrides = ContainerOverrides(regular)
			return nil
		} else {
			err3 = utils.NewMultiError(errors.New("Error unmarshalling ContainerOverrides"), err3)
		}
	}

	return utils.NewMultiError(errors.New("Could not unmarshal ContainerOverrides in any supported way"), err, err2, err3)
}
