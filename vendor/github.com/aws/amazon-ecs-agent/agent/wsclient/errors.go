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

// UnrecognizedWSRequestType specifies that a given type is not recognized.
// This error is not retriable.
type UnrecognizedWSRequestType struct {
	Type string
}

// Error implements error
func (u *UnrecognizedWSRequestType) Error() string {
	return "Could not recognize given argument as a valid type: " + u.Type
}

// Retry implements Retriable
func (u *UnrecognizedWSRequestType) Retry() bool {
	return false
}

// NotMarshallableWSRequest represents that the given request input could not be
// marshalled
type NotMarshallableWSRequest struct {
	Type string

	Err error
}

// Retry implementes Retriable
func (u *NotMarshallableWSRequest) Retry() bool {
	return false
}

// Error implements error
func (u *NotMarshallableWSRequest) Error() string {
	ret := "Could not marshal Request"
	if u.Type != "" {
		ret += " (" + u.Type + ")"
	}
	return ret + ": " + u.Err.Error()
}

// UndecodableMessage indicates that a message from the backend could not be decoded
type UndecodableMessage struct {
	Msg string
}

func (u *UndecodableMessage) Error() string {
	return "Could not decode message into any expected format: " + u.Msg
}
