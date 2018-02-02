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

package acsclient

import (
	"github.com/aws/amazon-ecs-agent/agent/acs/model/ecsacs"
	"github.com/aws/amazon-ecs-agent/agent/wsclient"
)

const errType = "ACSError"

// ACSUnretriableErrors wraps all the typed errors that ACS may return
type ACSUnretriableErrors struct{}

// Get gets the list of unretriable error types.
func (err *ACSUnretriableErrors) Get() []interface{} {
	return unretriableErrors
}

// acsError implements wsclient.ServiceError interface.
type acsError struct{}

// NewError returns an error corresponding to a typed error returned from
// ACS. It is expected that the passed in interface{} is really a struct which
// has a 'Message' field of type *string. In that case, the Message will be
// conveyed as part of the Error string as well as the type. It is safe to pass
// anything into this constructor and it will also work reasonably well with
// anything fulfilling the 'error' interface.
func (ae *acsError) NewError(err interface{}) *wsclient.WSError {
	return &wsclient.WSError{ErrObj: err, Type: errType, WSUnretriableErrors: &ACSUnretriableErrors{}}
}

// These errors are all fatal and there's nothing we can do about them.
// AccessDeniedException is actually potentially fixable because you can change
// credentials at runtime, but still close to unretriable.
var unretriableErrors = []interface{}{
	&ecsacs.InvalidInstanceException{},
	&ecsacs.InvalidClusterException{},
	&ecsacs.InactiveInstanceException{},
	&ecsacs.AccessDeniedException{},
}
