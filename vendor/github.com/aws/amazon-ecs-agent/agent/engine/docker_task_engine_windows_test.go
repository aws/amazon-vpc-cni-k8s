// +build windows,!integration

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
package engine

import (
	"testing"

	"github.com/aws/amazon-ecs-agent/agent/api"
	"github.com/aws/amazon-ecs-agent/agent/config"
	"github.com/aws/amazon-ecs-agent/agent/engine/emptyvolume"
	"github.com/aws/amazon-ecs-agent/agent/statemanager/mocks"

	"github.com/stretchr/testify/assert"
)

func TestPullEmptyVolumeImage(t *testing.T) {
	ctrl, client, _, privateTaskEngine, _, _, _ := mocks(t, &config.Config{})
	defer ctrl.Finish()
	taskEngine, _ := privateTaskEngine.(*DockerTaskEngine)
	saver := mock_statemanager.NewMockStateManager(ctrl)
	taskEngine.SetSaver(saver)
	taskEngine._time = nil

	imageName := "image"
	container := &api.Container{
		Type:  api.ContainerEmptyHostVolume,
		Image: imageName,
	}
	task := &api.Task{
		Containers: []*api.Container{container},
	}

	assert.False(t, emptyvolume.LocalImage, "Windows empty volume image is not local")
	client.EXPECT().PullImage(imageName, nil)

	metadata := taskEngine.pullContainer(task, container)
	assert.Equal(t, DockerContainerMetadata{}, metadata, "expected empty metadata")
}
