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

	"github.com/cihub/seelog"
)

// TaskVolume is a definition of all the volumes available for containers to
// reference within a task. It must be named.
type TaskVolume struct {
	Name   string `json:"name"`
	Volume HostVolume
}

// UnmarshalJSON for TaskVolume determines the name and volume type, and
// unmarshals it into the appropriate HostVolume fulfilling interfaces
func (tv *TaskVolume) UnmarshalJSON(b []byte) error {
	// Format: {name: volumeName, host: emptyVolumeOrHostVolume}
	intermediate := make(map[string]json.RawMessage)
	if err := json.Unmarshal(b, &intermediate); err != nil {
		return err
	}
	name, ok := intermediate["name"]
	if !ok {
		return errors.New("invalid Volume; must include a name")
	}
	if err := json.Unmarshal(name, &tv.Name); err != nil {
		return err
	}

	if rawhostdata, ok := intermediate["host"]; ok {
		// Default to trying to unmarshal it as a FSHostVolume
		var hostvolume FSHostVolume
		err := json.Unmarshal(rawhostdata, &hostvolume)
		if err != nil {
			return err
		}
		if hostvolume.FSSourcePath == "" {
			// If the FSSourcePath is empty, that must mean it was not an
			// FSHostVolume (empty path is invalid for that type). The only other
			// type is an empty volume, so unmarshal it as such.
			emptyVolume := &EmptyHostVolume{}
			json.Unmarshal(rawhostdata, emptyVolume)
			tv.Volume = emptyVolume
		} else {
			tv.Volume = &hostvolume
		}
		return nil
	}

	return errors.New("unrecognized volume type; try updating me")
}

// MarshalJSON overrides the logic for JSON-encoding a  TaskVolume object
func (tv *TaskVolume) MarshalJSON() ([]byte, error) {
	result := make(map[string]interface{})

	result["name"] = tv.Name

	switch v := tv.Volume.(type) {
	case *FSHostVolume:
		result["host"] = v
	case *EmptyHostVolume:
		result["host"] = v
	default:
		seelog.Critical("Unknown task volume type in marshal")
	}
	return json.Marshal(result)
}
