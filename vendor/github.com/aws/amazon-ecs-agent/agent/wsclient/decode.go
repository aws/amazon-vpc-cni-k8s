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

// Package wsclient wraps the generated aws-sdk-go client to provide marshalling
// and unmarshalling of data over a websocket connection in the format expected
// by backend. It allows for bidirectional communication and acts as both a
// client-and-server in terms of requests, but only as a client in terms of
// connecting.
package wsclient

import (
	"bytes"
	"encoding/json"
	"reflect"

	"github.com/aws/aws-sdk-go/private/protocol/json/jsonutil"
)

// DecodeData decodes a raw message into its type. E.g. An ACS message of the
// form {"type":"FooMessage","message":{"foo":1}} will be decoded into the
// corresponding *ecsacs.FooMessage type. The type string, "FooMessage", will
// also be returned as a convenience.
func DecodeData(data []byte, dec TypeDecoder) (interface{}, string, error) {
	raw := &ReceivedMessage{}
	// Delay unmarshal until we know the type
	err := json.Unmarshal(data, raw)
	if err != nil || raw.Type == "" {
		// Unframed messages can be of the {"Type":"Message"} form as well, try
		// that.
		connErr, connErrType, decodeErr := DecodeConnectionError(data, dec)
		if decodeErr == nil && connErrType != "" {
			return connErr, connErrType, nil
		}
		return nil, "", decodeErr
	}

	reqMessage, ok := dec.NewOfType(raw.Type)
	if !ok {
		return nil, raw.Type, &UnrecognizedWSRequestType{raw.Type}
	}
	err = jsonutil.UnmarshalJSON(reqMessage, bytes.NewReader(raw.Message))
	return reqMessage, raw.Type, err
}

// DecodeConnectionError decodes some of the connection errors returned by the
// backend. Some differ from the usual ones in that they do not have a 'type'
// and 'message' field, but rather are of the form {"ErrorType":"ErrorMessage"}
func DecodeConnectionError(data []byte, dec TypeDecoder) (interface{}, string, error) {
	var acsErr map[string]string
	err := json.Unmarshal(data, &acsErr)
	if err != nil {
		return nil, "", &UndecodableMessage{string(data)}
	}
	if len(acsErr) != 1 {
		return nil, "", &UndecodableMessage{string(data)}
	}
	var typeStr string
	for key := range acsErr {
		typeStr = key
	}
	errType, ok := dec.NewOfType(typeStr)
	if !ok {
		return nil, typeStr, &UnrecognizedWSRequestType{}
	}

	val := reflect.ValueOf(errType)
	if val.Kind() != reflect.Ptr {
		return nil, typeStr, &UnrecognizedWSRequestType{"Non-pointer kind: " + val.Kind().String()}
	}
	ret := reflect.New(val.Elem().Type())
	retObj := ret.Elem()

	if retObj.Kind() != reflect.Struct {
		return nil, typeStr, &UnrecognizedWSRequestType{"Pointer to non-struct kind: " + retObj.Kind().String()}
	}

	msgField := retObj.FieldByName("Message")
	if !msgField.IsValid() {
		return nil, typeStr, &UnrecognizedWSRequestType{"Expected error type to have 'Message' field"}
	}
	if msgField.IsValid() && msgField.CanSet() {
		msgStr := acsErr[typeStr]
		msgStrVal := reflect.ValueOf(&msgStr)
		if !msgStrVal.Type().AssignableTo(msgField.Type()) {
			return nil, typeStr, &UnrecognizedWSRequestType{"Type mismatch; 'Message' field must be a *string"}
		}
		msgField.Set(msgStrVal)
		return ret.Interface(), typeStr, nil
	}
	return nil, typeStr, &UnrecognizedWSRequestType{"Invalid message field; must not be nil"}
}
