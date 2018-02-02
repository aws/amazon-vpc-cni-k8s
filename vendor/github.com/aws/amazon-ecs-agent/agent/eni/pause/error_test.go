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

package pause

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUnsupportedPlatform(t *testing.T) {
	testCases := map[error]bool{
		errors.New("error"):                              false,
		NewUnsupportedPlatformError(errors.New("error")): true,
	}

	for err, expected := range testCases {
		t.Run(fmt.Sprintf("returns %t for type %s", expected, reflect.TypeOf(err)), func(t *testing.T) {
			assert.Equal(t, expected, UnsupportedPlatform(err))
		})
	}
}

func TestIsNoSuchFileError(t *testing.T) {
	testCases := map[error]bool{
		errors.New("error"):                            false,
		NewNoSuchFileError(errors.New("No such file")): true,
	}

	for err, expected := range testCases {
		t.Run(fmt.Sprintf("return %t for type %s", expected, reflect.TypeOf(err)), func(t *testing.T) {
			assert.Equal(t, expected, IsNoSuchFileError(err))
		})
	}
}
