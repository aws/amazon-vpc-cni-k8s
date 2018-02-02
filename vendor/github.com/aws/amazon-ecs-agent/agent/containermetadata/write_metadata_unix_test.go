// +build !integration, !windows
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

// TestWriteTempFileFail checks case where temp file cannot be made
func TestWriteTempFileFail(t *testing.T) {
	mockIOUtil, _, _, done := writeSetup(t)
	defer done()

	mockData := []byte("")
	mockTaskARN := validTaskARN
	mockContainerName := containerName
	mockDataDir := dataDir

	gomock.InOrder(
		mockIOUtil.EXPECT().TempFile(gomock.Any(), gomock.Any()).Return(nil, errors.New("temp file fail")),
	)

	err := writeToMetadataFile(nil, mockIOUtil, mockData, mockTaskARN, mockContainerName, mockDataDir)
	expectErrorMessage := "temp file fail"

	assert.Error(t, err)
	assert.Equal(t, expectErrorMessage, err.Error())
}

// TestWriteFileWriteFail checks case where write to file fails
func TestWriteFileWriteFail(t *testing.T) {
	mockIOUtil, _, mockFile, done := writeSetup(t)
	defer done()

	mockData := []byte("")
	mockTaskARN := validTaskARN
	mockContainerName := containerName
	mockDataDir := dataDir

	gomock.InOrder(
		mockIOUtil.EXPECT().TempFile(gomock.Any(), gomock.Any()).Return(mockFile, nil),
		mockFile.EXPECT().Write(mockData).Return(0, errors.New("write fail")),
		mockFile.EXPECT().Close(),
	)

	err := writeToMetadataFile(nil, mockIOUtil, mockData, mockTaskARN, mockContainerName, mockDataDir)
	expectErrorMessage := "write fail"

	assert.Error(t, err)
	assert.Equal(t, expectErrorMessage, err.Error())
}

// TestWriteChmodFail checks case where chmod fails
func TestWriteChmodFail(t *testing.T) {
	mockIOUtil, _, mockFile, done := writeSetup(t)
	defer done()

	mockData := []byte("")
	mockTaskARN := validTaskARN
	mockContainerName := containerName
	mockDataDir := dataDir

	gomock.InOrder(
		mockIOUtil.EXPECT().TempFile(gomock.Any(), gomock.Any()).Return(mockFile, nil),
		mockFile.EXPECT().Write(mockData).Return(0, nil),
		mockFile.EXPECT().Chmod(gomock.Any()).Return(errors.New("chmod fail")),
		mockFile.EXPECT().Close(),
	)

	err := writeToMetadataFile(nil, mockIOUtil, mockData, mockTaskARN, mockContainerName, mockDataDir)
	expectErrorMessage := "chmod fail"

	assert.Error(t, err)
	assert.Equal(t, expectErrorMessage, err.Error())
}
