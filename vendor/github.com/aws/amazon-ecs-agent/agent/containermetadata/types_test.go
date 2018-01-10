// +build !integration
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

package containermetadata

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestUnMarshalText(t *testing.T) {

	var nilMs MetadataStatus

	testCases := []struct {
		param          []byte
		expectedReturn MetadataStatus
		name           string
	}{
		{[]byte(""), nilMs, "unmarshal MetadataStatus from empty string"},
		{[]byte("Malformed"), nilMs, "unmarshal MetadataStatus from malformed string"},
		{[]byte(MetadataInitialText), MetadataInitial, "unmarshal MetadataInitial from string"},
		{[]byte(MetadataReadyText), MetadataReady, "unmarshal MetadataReady from string"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var ms MetadataStatus
			ms.UnmarshalText(tc.param)
			assert.Equal(t, tc.expectedReturn, ms, tc.name)
		})
	}
}

func TestMarshalText(t *testing.T) {

	testCases := []struct {
		param          MetadataStatus
		expectedReturn []byte
		name           string
	}{
		{MetadataInitial, []byte(MetadataInitialText), "marshal MetadataInitial to text"},
		{MetadataReady, []byte(MetadataReadyText), "marshal MetadataReady to text"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ret, _ := tc.param.MarshalText()
			assert.Equal(t, tc.expectedReturn, ret, tc.name)
		})
	}
}
