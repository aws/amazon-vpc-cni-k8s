// +build linux

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
	"testing"

	"github.com/aws/amazon-ecs-agent/agent/acs/update_handler/os/mock"
	"github.com/aws/amazon-ecs-agent/agent/config"
	"github.com/aws/amazon-ecs-agent/agent/engine"
	"github.com/aws/amazon-ecs-agent/agent/engine/dockerclient/mocks"
	"github.com/aws/amazon-ecs-agent/agent/engine/dockeriface/mocks"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
)

const (
	pauseTarballPath = "/path/to/pause.tar"
	pauseName        = "pause"
	pauseTag         = "tag"
)

var defaultConfig = config.DefaultConfig()

// TestLoadFromFileWithReaderError tests loadFromFile with reader error
func TestLoadFromFileWithReaderError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDocker := mock_dockeriface.NewMockClient(ctrl)
	mockDocker.EXPECT().Ping().AnyTimes().Return(nil)
	factory := mock_dockerclient.NewMockFactory(ctrl)
	factory.EXPECT().GetDefaultClient().AnyTimes().Return(mockDocker, nil)
	client, err := engine.NewDockerGoClient(factory, &defaultConfig)
	assert.NoError(t, err)

	mockfs := mock_os.NewMockFileSystem(ctrl)
	mockfs.EXPECT().Open(pauseTarballPath).Return(nil, errors.New("Dummy Reader Error"))

	err = loadFromFile(pauseTarballPath, client, mockfs)
	assert.Error(t, err)
}

// TestLoadFromFileHappyPath tests loadFromFile against happy path
func TestLoadFromFileHappyPath(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDocker := mock_dockeriface.NewMockClient(ctrl)
	mockDocker.EXPECT().Ping().AnyTimes().Return(nil)
	factory := mock_dockerclient.NewMockFactory(ctrl)
	factory.EXPECT().GetDefaultClient().AnyTimes().Return(mockDocker, nil)
	client, err := engine.NewDockerGoClient(factory, &defaultConfig)
	assert.NoError(t, err)

	mockDocker.EXPECT().LoadImage(gomock.Any()).Return(nil)

	mockfs := mock_os.NewMockFileSystem(ctrl)
	mockfs.EXPECT().Open(pauseTarballPath).Return(nil, nil)

	err = loadFromFile(pauseTarballPath, client, mockfs)
	assert.NoError(t, err)
}

// TestLoadFromFileDockerLoadImageError tests loadFromFile against error
// from Docker clients LoadImage
func TestLoadFromFileDockerLoadImageError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDocker := mock_dockeriface.NewMockClient(ctrl)
	mockDocker.EXPECT().Ping().AnyTimes().Return(nil)
	factory := mock_dockerclient.NewMockFactory(ctrl)
	factory.EXPECT().GetDefaultClient().AnyTimes().Return(mockDocker, nil)
	client, err := engine.NewDockerGoClient(factory, &defaultConfig)
	assert.NoError(t, err)

	mockDocker.EXPECT().LoadImage(gomock.Any()).Return(
		errors.New("Dummy Load Image Error"))

	mockfs := mock_os.NewMockFileSystem(ctrl)
	mockfs.EXPECT().Open(pauseTarballPath).Return(nil, nil)

	err = loadFromFile(pauseTarballPath, client, mockfs)
	assert.Error(t, err)
}

func TestGetPauseContainerImageInspectImageError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDocker := mock_dockeriface.NewMockClient(ctrl)
	mockDocker.EXPECT().Ping().AnyTimes().Return(nil)
	factory := mock_dockerclient.NewMockFactory(ctrl)
	factory.EXPECT().GetDefaultClient().AnyTimes().Return(mockDocker, nil)
	client, err := engine.NewDockerGoClient(factory, &defaultConfig)
	assert.NoError(t, err)

	mockDocker.EXPECT().InspectImage(pauseName+":"+pauseTag).Return(
		nil, errors.New("error"))

	_, err = getPauseContainerImage(pauseName, pauseTag, client)
	assert.Error(t, err)
}

func TestGetPauseContainerHappyPath(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDocker := mock_dockeriface.NewMockClient(ctrl)
	mockDocker.EXPECT().Ping().AnyTimes().Return(nil)
	factory := mock_dockerclient.NewMockFactory(ctrl)
	factory.EXPECT().GetDefaultClient().AnyTimes().Return(mockDocker, nil)
	client, err := engine.NewDockerGoClient(factory, &defaultConfig)
	assert.NoError(t, err)

	mockDocker.EXPECT().InspectImage(pauseName+":"+pauseTag).Return(nil, nil)

	_, err = getPauseContainerImage(pauseName, pauseTag, client)
	assert.NoError(t, err)
}
