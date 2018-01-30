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
	"testing"
)

func TestMarshalUnmarshalTaskVolumes(t *testing.T) {
	task := &Task{
		Arn: "test",
		Volumes: []TaskVolume{
			TaskVolume{Name: "1", Volume: &EmptyHostVolume{}},
			TaskVolume{Name: "2", Volume: &FSHostVolume{FSSourcePath: "/path"}},
		},
	}

	marshal, err := json.Marshal(task)
	if err != nil {
		t.Fatal("Could not marshal: ", err)
	}

	var out Task
	err = json.Unmarshal(marshal, &out)
	if err != nil {
		t.Fatal("Could not unmarshal: ", err)
	}

	if len(out.Volumes) != 2 {
		t.Fatal("Incorrect number of volumes")
	}

	var v1, v2 TaskVolume

	for _, v := range out.Volumes {
		if v.Name == "1" {
			v1 = v
		} else {
			v2 = v
		}
	}

	if _, ok := v1.Volume.(*EmptyHostVolume); !ok {
		t.Error("Expected v1 to be an empty volume")
	}

	if v2.Volume.SourcePath() != "/path" {
		t.Error("Expected v2 to have 'sourcepath' work correctly")
	}
	fs, ok := v2.Volume.(*FSHostVolume)
	if !ok || fs.FSSourcePath != "/path" {
		t.Error("Unmarshaled v2 didn't match marshalled v2")
	}
}
