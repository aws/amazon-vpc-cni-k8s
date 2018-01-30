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

package wsclient

import "reflect"

// TypeDecoder interface defines methods to decode ecs types.
type TypeDecoder interface {
	// NewOfType returns an object of a recognized type for a given type name.
	// It additionally returns a boolean value which is set to false for an
	// unrecognized type.
	NewOfType(string) (interface{}, bool)

	// GetRecognizedTypes returns a map of type-strings (as passed in acs/tcs messages as
	// the 'type' field) to a pointer to the corresponding struct type this type should
	// be marshalled/unmarshalled to/from.
	GetRecognizedTypes() map[string]reflect.Type
}

// TypeDecoderImpl is an implementation for general use between ACS and
// TCS clients
type TypeDecoderImpl struct {
	typeMappings map[string]reflect.Type
}

// BuildTypeDecoder takes a list of interfaces and stores them internally as a
// list of typeMappings in the format below.
// "MyMessage": TypeOf(ecstcs.MyMessage)
func BuildTypeDecoder(recognizedTypes []interface{}) TypeDecoder {
	typeMappings := make(map[string]reflect.Type)
	for _, recognizedType := range recognizedTypes {
		typeMappings[reflect.TypeOf(recognizedType).Name()] = reflect.TypeOf(recognizedType)
	}

	return &TypeDecoderImpl{typeMappings: typeMappings}
}

func (d *TypeDecoderImpl) NewOfType(typeString string) (interface{}, bool) {
	rtype, ok := d.typeMappings[typeString]
	if !ok {
		return nil, false
	}
	return reflect.New(rtype).Interface(), true
}

func (d *TypeDecoderImpl) GetRecognizedTypes() map[string]reflect.Type {
	return d.typeMappings
}
