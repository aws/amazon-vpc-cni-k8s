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

package oswrapper

import "os"

// OS wraps methods from the stdlib's 'os' package. This is mostly to help with
// testing where responses from these methods can be mocked
type OS interface {
	// Getpid returns the process id of the caller
	Getpid() int
}

// New creates a new stdlib OS object
func New() OS {
	return &stdlibOS{}
}

type stdlibOS struct{}

func (*stdlibOS) Getpid() int {
	return os.Getpid()
}
