// Copyright 2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//     http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package engine

// _error contains the common error fields for the error types defined in this file.
// It implements the 'error' interface.
type _error struct {
	operation string
	origin    string
	message   string
}

func (err *_error) Error() string {
	return err.operation + " " + err.origin + ": " + err.message
}

// unmappedMACAddressError is used to indicate that the MAC address of the ENI
// cannot be mapped to any of the network interfaces attached to the host as
// determined by the instance metadata
type unmappedMACAddressError struct {
	err *_error
}

func (macErr *unmappedMACAddressError) Error() string {
	return macErr.err.Error()
}

func newUnmappedMACAddressError(operation string, origin string, message string) error {
	return &unmappedMACAddressError{
		err: &_error{
			operation: operation,
			origin:    origin,
			message:   message,
		},
	}
}

// parseIPV4GatewayNetmaskError is used to indicate any error with parsing the
// IPV4 address and the netmask of the ENI
type parseIPV4GatewayNetmaskError struct {
	err *_error
}

func (parseErr *parseIPV4GatewayNetmaskError) Error() string {
	return parseErr.err.Error()
}

func newParseIPV4GatewayNetmaskError(operation string, origin string, message string) error {
	return &parseIPV4GatewayNetmaskError{
		err: &_error{
			operation: operation,
			origin:    origin,
			message:   message,
		},
	}
}
