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
	"fmt"
	"testing"

	"github.com/aws/amazon-ecs-agent/agent/utils/ioutilwrapper/mocks"
	"github.com/aws/amazon-ecs-agent/agent/utils/oswrapper/mocks"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
)

func writeSetup(t *testing.T) (*mock_ioutilwrapper.MockIOUtil, *mock_oswrapper.MockOS, *mock_oswrapper.MockFile, func()) {
	ctrl := gomock.NewController(t)
	mockIOUtil := mock_ioutilwrapper.NewMockIOUtil(ctrl)
	mockOS := mock_oswrapper.NewMockOS(ctrl)
	mockFile := mock_oswrapper.NewMockFile(ctrl)
	return mockIOUtil, mockOS, mockFile, ctrl.Finish
}

// TestWriteInvalidARN checks case where task ARN passed in is invalid
func TestWriteInvalidARN(t *testing.T) {
	_, _, _, done := writeSetup(t)
	defer done()

	mockData := []byte("")
	mockTaskARN := invalidTaskARN
	mockContainerName := containerName
	mockDataDir := dataDir
	expectErrorMessage := fmt.Sprintf("write to metadata file for task %s container %s: get metdata file path of task %s container %s: get task ARN: invalid TaskARN %s", mockTaskARN, mockContainerName, mockTaskARN, mockContainerName, mockTaskARN)

	err := writeToMetadataFile(nil, nil, mockData, mockTaskARN, mockContainerName, mockDataDir)
	assert.Equal(t, expectErrorMessage, err.Error())
}
