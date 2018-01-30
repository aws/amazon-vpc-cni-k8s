// Copyright 2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

package ec2

import "fmt"

// MetadataError is used to encapsulate the error returned because of non OK HTTP
// status codes in the response when querying the Instance Metadata Service
type MetadataError struct {
	error
	statusCode int
}

// NewMetadataError returns a new MetadataError object
func NewMetadataError(statusCode int) *MetadataError {
	return &MetadataError{
		statusCode: statusCode,
	}
}

// Error returns the error string
func (err *MetadataError) Error() string {
	return fmt.Sprintf("ec2 metadata client: unsuccessful response from Metadata service: %v", err.statusCode)
}

// GetStatusCode returns the http status code for the error from metadata
func (err *MetadataError) GetStatusCode() int {
	return err.statusCode
}
