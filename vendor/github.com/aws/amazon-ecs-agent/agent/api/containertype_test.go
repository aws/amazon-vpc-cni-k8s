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

package api

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

type containerTypeWrapper struct {
	Type ContainerType `json:"IsInternal"`
}

func TestMarshalContainerType(t *testing.T) {
	testCases := []struct {
		containerType containerTypeWrapper
		encodedString string
	}{
		{containerTypeWrapper{ContainerNormal}, `{"IsInternal":"NORMAL"}`},
		{containerTypeWrapper{ContainerEmptyHostVolume}, `{"IsInternal":"EMPTY_HOST_VOLUME"}`},
		{containerTypeWrapper{ContainerCNIPause}, `{"IsInternal":"CNI_PAUSE"}`},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%s marshalled as %s", tc.containerType.Type.String(), tc.encodedString),
			func(t *testing.T) {
				contTypeWrapper := &tc.containerType
				contTypeJSON, err := json.Marshal(contTypeWrapper)
				assert.NoError(t, err)
				assert.Equal(t, tc.encodedString, string(contTypeJSON))
			})
	}
}

func TestUnmarhsalContainerType(t *testing.T) {
	testCases := []struct {
		containerType containerTypeWrapper
		encodedString string
	}{
		{containerTypeWrapper{ContainerNormal}, `{"IsInternal":"NORMAL"}`},
		{containerTypeWrapper{ContainerEmptyHostVolume}, `{"IsInternal":"EMPTY_HOST_VOLUME"}`},
		{containerTypeWrapper{ContainerCNIPause}, `{"IsInternal":"CNI_PAUSE"}`},
		{containerTypeWrapper{ContainerNormal}, `{"IsInternal":null}`},
		{containerTypeWrapper{ContainerNormal}, `{"IsInternal":false}`},
		{containerTypeWrapper{ContainerEmptyHostVolume}, `{"IsInternal":true}`},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%s unmarshalled as %s", tc.encodedString, tc.containerType.Type.String()),
			func(t *testing.T) {
				var contTypeWrapper containerTypeWrapper
				err := json.Unmarshal([]byte(tc.encodedString), &contTypeWrapper)
				assert.NoError(t, err)
				assert.Equal(t, tc.containerType.Type, contTypeWrapper.Type)
			})
	}
}

func TestUnmarhsalContainerTypeFailures(t *testing.T) {
	testCases := []struct {
		encodedString string
	}{
		{`{"IsInternal":"foo"}`},
		{`{"IsInternal":""}`},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%s unmarshalling", tc.encodedString), func(t *testing.T) {
			var contTypeWrapper containerTypeWrapper
			err := json.Unmarshal([]byte(tc.encodedString), &contTypeWrapper)
			assert.Error(t, err)
			assert.Equal(t, ContainerNormal, contTypeWrapper.Type)
		})
	}
}
