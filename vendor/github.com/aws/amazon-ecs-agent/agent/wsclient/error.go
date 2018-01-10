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

package wsclient

import "reflect"

// WSUnretriableErrors defines methods to retrieve the list of unretriable
// errors.
type WSUnretriableErrors interface {
	Get() []interface{}
}

// ServiceError defines methods to return new backend specific error objects.
type ServiceError interface {
	NewError(err interface{}) *WSError
}

// WSError wraps all the typed errors that the backend may return
// This will not be needed once the aws-sdk-go generation handles error types
// more cleanly
type WSError struct {
	ErrObj interface{}
	Type   string
	WSUnretriableErrors
}

// Error returns an error string
func (err *WSError) Error() string {
	val := reflect.ValueOf(err.ErrObj)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	var typeStr = "Unknown type"
	if val.IsValid() {
		typeStr = val.Type().Name()
		msg := val.FieldByName("Message")
		if msg.IsValid() && msg.CanInterface() {
			str, ok := msg.Interface().(*string)
			if ok {
				if str == nil {
					return typeStr + ": null"
				}
				return typeStr + ": " + *str
			}
		}
	}

	if asErr, ok := err.ErrObj.(error); ok {
		return err.Type + ": " + asErr.Error()
	}
	return err.Type + ": Unknown error (" + typeStr + ")"
}

// Retry returns true if this error should be considered retriable
func (err *WSError) Retry() bool {
	for _, unretriable := range err.Get() {
		if reflect.TypeOf(err.ErrObj) == reflect.TypeOf(unretriable) {
			return false
		}
	}
	return true
}
