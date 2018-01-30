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

package resources

import (
	"errors"
	"testing"

	"github.com/aws/amazon-ecs-agent/agent/api"
	"github.com/aws/amazon-ecs-agent/agent/engine/testdata"
	"github.com/aws/amazon-ecs-agent/agent/resources/cgroup/factory/mock"
	"github.com/aws/amazon-ecs-agent/agent/resources/cgroup/mock_control"
	"github.com/containerd/cgroups"
	"github.com/stretchr/testify/assert"

	"github.com/golang/mock/gomock"
)

const (
	validTaskArn   = "arn:aws:ecs:region:account-id:task/task-id"
	invalidTaskArn = "invalid:task::arn"
)

func TestInitHappyPath(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockControl := mock_cgroup.NewMockControl(ctrl)

	gomock.InOrder(
		mockControl.EXPECT().Exists(gomock.Any()).Return(false),
		mockControl.EXPECT().Init().Return(nil),
	)

	resource := newResources(mockControl)
	assert.NoError(t, resource.Init())
}

func TestInitCgroupExistsHappyPath(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockControl := mock_cgroup.NewMockControl(ctrl)

	mockControl.EXPECT().Exists(gomock.Any()).Return(true)

	resource := newResources(mockControl)
	assert.NoError(t, resource.Init())
}

func TestInitErrorPath(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockControl := mock_cgroup.NewMockControl(ctrl)

	gomock.InOrder(
		mockControl.EXPECT().Exists(gomock.Any()).Return(false),
		mockControl.EXPECT().Init().Return(errors.New("cgroup init error")),
	)

	resource := newResources(mockControl)
	assert.Error(t, resource.Init())
}

func TestSetupHappyPath(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockControl := mock_cgroup.NewMockControl(ctrl)
	mockCgroup := mock_cgroups.NewMockCgroup(ctrl)

	task := testdata.LoadTask("sleep5TaskCgroup")

	gomock.InOrder(
		mockControl.EXPECT().Exists(gomock.Any()).Return(false),
		mockControl.EXPECT().Create(gomock.Any()).Return(mockCgroup, nil),
	)

	resource := newResources(mockControl)
	assert.NoError(t, resource.Setup(task))
}

func TestSetupInvalidTaskARN(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockControl := mock_cgroup.NewMockControl(ctrl)

	task := &api.Task{
		Arn: invalidTaskArn,
	}

	resource := newResources(mockControl)
	assert.Error(t, resource.Setup(task))
}

func TestSetupCgroupExists(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockControl := mock_cgroup.NewMockControl(ctrl)

	task := testdata.LoadTask("sleep5TaskCgroup")

	mockControl.EXPECT().Exists(gomock.Any()).Return(true)

	resource := newResources(mockControl)
	assert.NoError(t, resource.Setup(task))
}

func TestSetupCgroupInvalidResourceSpec(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockControl := mock_cgroup.NewMockControl(ctrl)

	task := testdata.LoadTask("sleep5TaskCgroup")
	task.CPU = float64(100)

	mockControl.EXPECT().Exists(gomock.Any()).Return(false)

	resource := newResources(mockControl)
	assert.Error(t, resource.Setup(task))
}

func TestSetupCgroupCreateError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockControl := mock_cgroup.NewMockControl(ctrl)
	mockCgroup := mock_cgroups.NewMockCgroup(ctrl)

	task := testdata.LoadTask("sleep5TaskCgroup")

	gomock.InOrder(
		mockControl.EXPECT().Exists(gomock.Any()).Return(false),
		mockControl.EXPECT().Create(gomock.Any()).Return(mockCgroup, errors.New("cgroup create error")),
	)

	resource := newResources(mockControl)
	assert.Error(t, resource.Setup(task))
}

func TestCleanupHappyPath(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockControl := mock_cgroup.NewMockControl(ctrl)

	task := testdata.LoadTask("sleep5TaskCgroup")

	mockControl.EXPECT().Remove(gomock.Any()).Return(nil)

	resource := newResources(mockControl)
	assert.NoError(t, resource.Cleanup(task))
}

func TestCleanupRemoveError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockControl := mock_cgroup.NewMockControl(ctrl)

	task := testdata.LoadTask("sleep5TaskCgroup")

	mockControl.EXPECT().Remove(gomock.Any()).Return(errors.New("cgroup remove error"))

	resource := newResources(mockControl)
	assert.Error(t, resource.Cleanup(task))
}

func TestCleanupInvalidTaskARN(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockControl := mock_cgroup.NewMockControl(ctrl)

	task := &api.Task{
		Arn: invalidTaskArn,
	}

	resource := newResources(mockControl)
	assert.Error(t, resource.Cleanup(task))
}

func TestCleanupCgroupDeletedError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockControl := mock_cgroup.NewMockControl(ctrl)

	task := testdata.LoadTask("sleep5TaskCgroup")

	mockControl.EXPECT().Remove(gomock.Any()).Return(cgroups.ErrCgroupDeleted)

	resource := newResources(mockControl)
	assert.NoError(t, resource.Cleanup(task))
}
