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

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
)

func TestInitHappyCase(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockCgroup := mock_cgroups.NewMockCgroup(ctrl)
	mockCgroupFactory := mock_factory.NewMockCgroupFactory(ctrl)

	mockCgroupFactory.EXPECT().New(gomock.Any(), gomock.Any(), gomock.Any()).Return(mockCgroup, nil)

	control := newControl(mockCgroupFactory)

	assert.NoError(t, control.Init())
}

func TestInitErrorCase(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockCgroupFactory := mock_factory.NewMockCgroupFactory(ctrl)

	mockCgroupFactory.EXPECT().New(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("cgroup error"))

	control := newControl(mockCgroupFactory)

	assert.Error(t, control.Init())
}
