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

package cgroup

import (
	"errors"
	"testing"

	"github.com/aws/amazon-ecs-agent/agent/resources/cgroup/factory/mock"
	"github.com/aws/amazon-ecs-agent/agent/resources/cgroup/factory/mock_factory"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/assert"

	"github.com/golang/mock/gomock"
)

const (
	testCgroupRoot = "/ecs/foo"
)

func TestCreateHappyCase(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockCgroup := mock_cgroups.NewMockCgroup(ctrl)
	mockCgroupFactory := mock_factory.NewMockCgroupFactory(ctrl)

	mockCgroupFactory.EXPECT().New(gomock.Any(), gomock.Any(), gomock.Any()).Return(mockCgroup, nil)

	testSpecs := &specs.LinuxResources{}

	control := newControl(mockCgroupFactory)

	res, err := control.Create(&Spec{testCgroupRoot, testSpecs})
	assert.Equal(t, mockCgroup, res)
	assert.NoError(t, err)
}

func TestCreateErrorCase(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockCgroupFactory := mock_factory.NewMockCgroupFactory(ctrl)

	mockCgroupFactory.EXPECT().New(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("cgroup error"))

	testSpecs := &specs.LinuxResources{}

	control := newControl(mockCgroupFactory)

	res, err := control.Create(&Spec{testCgroupRoot, testSpecs})
	assert.Nil(t, res)
	assert.Error(t, err)
}

func TestCreateWithBadSpecs(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockCgroupFactory := mock_factory.NewMockCgroupFactory(ctrl)

	cg := newControl(mockCgroupFactory)

	testCases := []struct {
		spec *Spec
		name string
	}{
		{&Spec{"", nil}, "empty root and nil spec"},
		{&Spec{"/ecs/foo", nil}, "root with nil spec"},
		{&Spec{"", &specs.LinuxResources{}}, "empty root with spec"},
		{&Spec{}, "empty spec"},
		{nil, "nil spec"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			control, err := cg.Create(tc.spec)
			assert.Error(t, err, "Create should return an error")
			assert.Nil(t, control, "Create call should not return a controller")
		})
	}
}

func TestRemoveHappyCase(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockCgroup := mock_cgroups.NewMockCgroup(ctrl)
	mockCgroupFactory := mock_factory.NewMockCgroupFactory(ctrl)

	gomock.InOrder(
		mockCgroupFactory.EXPECT().Load(gomock.Any(), gomock.Any()).Return(mockCgroup, nil),
		mockCgroup.EXPECT().Delete().Return(nil),
	)

	control := newControl(mockCgroupFactory)

	assert.NoError(t, control.Remove(testCgroupRoot))
}

func TestRemoveDoingErrorCase(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockCgroupFactory := mock_factory.NewMockCgroupFactory(ctrl)

	mockCgroupFactory.EXPECT().Load(gomock.Any(), gomock.Any()).Return(nil, errors.New("unable to load"))

	control := newControl(mockCgroupFactory)

	assert.Error(t, control.Remove(testCgroupRoot))
}

func TestRemoveErrorCase(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockCgroup := mock_cgroups.NewMockCgroup(ctrl)
	mockCgroupFactory := mock_factory.NewMockCgroupFactory(ctrl)

	gomock.InOrder(
		mockCgroupFactory.EXPECT().Load(gomock.Any(), gomock.Any()).Return(mockCgroup, nil),
		mockCgroup.EXPECT().Delete().Return(errors.New("cgroup error")),
	)

	control := newControl(mockCgroupFactory)

	assert.Error(t, control.Remove(testCgroupRoot))
}

func TestExistsHappyPath(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockCgroup := mock_cgroups.NewMockCgroup(ctrl)
	mockCgroupFactory := mock_factory.NewMockCgroupFactory(ctrl)

	mockCgroupFactory.EXPECT().Load(gomock.Any(), gomock.Any()).Return(mockCgroup, nil)

	control := newControl(mockCgroupFactory)

	assert.True(t, control.Exists(testCgroupRoot))
}

func TestExistsErrorPath(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockCgroupFactory := mock_factory.NewMockCgroupFactory(ctrl)

	mockCgroupFactory.EXPECT().Load(gomock.Any(), gomock.Any()).Return(nil, errors.New("cgroups error"))

	control := newControl(mockCgroupFactory)

	assert.False(t, control.Exists(testCgroupRoot))
}

func TestExistsErrorPathWithLoadError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockCgroupFactory := mock_factory.NewMockCgroupFactory(ctrl)

	mockCgroupFactory.EXPECT().Load(gomock.Any(), gomock.Any()).Return(nil, nil)

	control := newControl(mockCgroupFactory)

	assert.False(t, control.Exists(testCgroupRoot))
}
