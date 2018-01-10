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
	"errors"
	"reflect"
	"strings"
)

// JsonKeys takes an arbitrary byte array representing a json stringified object
// and returns all the keys of that object.
func JsonKeys(b []byte) ([]string, error) {
	var keyMap map[string]interface{}

	err := json.Unmarshal(b, &keyMap)
	if err != nil {
		return []string{}, err
	}

	keys := make([]string, len(keyMap))
	ndx := 0
	for k := range keyMap {
		keys[ndx] = k
		ndx++
	}
	return keys, nil
}

// CompleteJsonUnmarshal determines if a given struct has members corresponding
// to every key of a json object (passed as the json string).
// By default, Go ignores fields in an object which have no corresponding struct
// member and this can be used to determine if this ignoring has occured
// Errors will result in "false" as a return value
func CompleteJsonUnmarshal(b []byte, iface interface{}) error {
	keys, err := JsonKeys(b)
	if err != nil {
		return err
	}

	structType := reflect.ValueOf(iface).Type()

	for _, key := range keys {
		_, found := structType.FieldByNameFunc(func(name string) bool {
			structField, _ := structType.FieldByName(name)
			jsonTag := structField.Tag.Get("json")
			jsonName := strings.Split(jsonTag, ",")[0]
			if jsonName == key {
				return true
			}
			if strings.EqualFold(key, structField.Name) {
				return true
			}
			return false
		})
		if !found {
			// We encountered a json key that there was no corresponding struct
			// field for; this is not complete
			return errors.New("Unrecognized key: " + key)
		}
	}
	return nil
}
