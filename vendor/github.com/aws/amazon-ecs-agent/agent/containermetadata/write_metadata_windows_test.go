// +build !integration, windows
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
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
)

// TestWriteOpenFileFail checks case where open file fails and does not return a NotExist error
func TestWriteOpenFileFail(t *testing.T) {
	_, mockOS, _, done := writeSetup(t)
	defer done()

	mockData := []byte("")
	mockTaskARN := validTaskARN
	mockContainerName := containerName
	mockDataDir := dataDir
	mockOpenErr := errors.New("does exist")

	gomock.InOrder(
		mockOS.EXPECT().OpenFile(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, mockOpenErr),
	)

	err := writeToMetadataFile(mockOS, nil, mockData, mockTaskARN, mockContainerName, mockDataDir)

	expectErrorMessage := "does exist"

	assert.Error(t, err)
	assert.Equal(t, expectErrorMessage, err.Error())
}

// TestWriteFileWrtieFail checks case where we fail to write to file
func TestWriteFileWriteFail(t *testing.T) {
	_, mockOS, mockFile, done := writeSetup(t)
	defer done()

	mockData := []byte("")
	mockTaskARN := validTaskARN
	mockContainerName := containerName
	mockDataDir := dataDir

	gomock.InOrder(
		mockOS.EXPECT().OpenFile(gomock.Any(), gomock.Any(), gomock.Any()).Return(mockFile, nil),
		mockFile.EXPECT().Write(mockData).Return(0, errors.New("write fail")),
		mockFile.EXPECT().Close(),
	)

	err := writeToMetadataFile(mockOS, nil, mockData, mockTaskARN, mockContainerName, mockDataDir)

	expectErrorMessage := "write fail"

	assert.Error(t, err)
	assert.Equal(t, expectErrorMessage, err.Error())
}
