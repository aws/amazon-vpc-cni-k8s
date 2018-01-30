// Copyright 2014-2015 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

package utils

import (
	"encoding/json"
	"testing"
)

func TestJsonKeys(t *testing.T) {
	keys, err := JsonKeys([]byte(`{"key1": 1, "key2": "val", "key3": {}}`))
	if err != nil {
		t.Error(err)
	}
	if !SlicesDeepEqual(keys, []string{"key1", "key2", "key3"}) {
		t.Errorf("Incorrect json keys: Got %v", keys)
	}

	_, err = JsonKeys([]byte(`Invalid json }{`))
	if err == nil {
		t.Error("Could find keys for invalid json")
	}

	keys, err = JsonKeys([]byte(`{}`))
	if err != nil {
		t.Error(err)
	}
	if len(keys) != 0 {
		t.Error("Keys for empty objet should be empty")
	}

	_, err = JsonKeys([]byte(`[]`))
	if err == nil {
		t.Error("Trying to get keys of a non-object wasn't an error")
	}
}

type testStruct struct {
	Field1 string
	Field2 int
	Field3 int `json:"f3"`
	Field4 int `json:"f4,omitempty,string"`
}

type futureTestStruct struct {
	Field1 string
	Field2 int
	Field3 int `json:"f3"`
	Field4 int `json:"f4,omitempty,string"`
	Field5 int `json:"f5,omitempty"`
}

func TestCompleteJsonUnmarshal(t *testing.T) {
	test := testStruct{Field1: "str", Field2: 1, Field3: 1, Field4: 1}
	jsonString, _ := json.Marshal(test)

	var unmarshaledTest testStruct
	err := json.Unmarshal(jsonString, &unmarshaledTest)
	if err != nil {
		t.Error(err)
	}

	if err = CompleteJsonUnmarshal(jsonString, unmarshaledTest); err != nil {
		t.Errorf("Unmarshal should have been complete: %v", err)
	}

	future := futureTestStruct{Field1: "str", Field2: 1, Field5: 1}
	futureStr, _ := json.Marshal(future)

	var outOfDate testStruct
	err = json.Unmarshal(futureStr, &outOfDate)
	if err != nil {
		t.Error(err)
	}

	if err = CompleteJsonUnmarshal(futureStr, outOfDate); err == nil {
		t.Error("Shouldn't be complete with an unrecognized field5")
	}

	var empty struct{}
	if err = CompleteJsonUnmarshal([]byte(`{}`), empty); err != nil {
		t.Error("The empty object can losslessly unmarshal into anything")
	}
	if err := CompleteJsonUnmarshal([]byte(`{}`), test); err != nil {
		t.Error("The empty object can losslessly unmarshal into anything")
	}

	if err = CompleteJsonUnmarshal([]byte(`{"key":"val"}`), empty); err == nil {
		t.Error("The non-empty object can't unmarshal into the empty struct losslessly")
	}
}
