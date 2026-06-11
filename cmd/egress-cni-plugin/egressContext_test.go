// Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//     http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package main

import (
	"errors"
	"fmt"
	"net"
	"syscall"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/vishvananda/netlink"

	mock_netlink "github.com/aws/amazon-vpc-cni-k8s/pkg/netlinkwrapper/mocks"
)

func testRoute() *netlink.Route {
	return &netlink.Route{
		LinkIndex: 100,
		Scope:     netlink.SCOPE_LINK,
		Dst: &net.IPNet{
			IP:   net.ParseIP("169.254.172.10"),
			Mask: net.CIDRMask(32, 32),
		},
	}
}

func TestAddRouteWithRetry_SuccessFirstAttempt(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockLink := mock_netlink.NewMockNetLink(ctrl)
	ec := egressContext{Link: mockLink}

	mockLink.EXPECT().RouteAdd(gomock.Any()).Return(nil)

	err := ec.addRouteWithRetry(testRoute())
	assert.NoError(t, err)
}

func TestAddRouteWithRetry_SuccessAfterEEXIST(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockLink := mock_netlink.NewMockNetLink(ctrl)
	ec := egressContext{Link: mockLink}

	gomock.InOrder(
		mockLink.EXPECT().RouteAdd(gomock.Any()).Return(syscall.EEXIST),
		mockLink.EXPECT().RouteAdd(gomock.Any()).Return(syscall.EEXIST),
		mockLink.EXPECT().RouteAdd(gomock.Any()).Return(nil),
	)

	err := ec.addRouteWithRetry(testRoute())
	assert.NoError(t, err)
}

func TestAddRouteWithRetry_NonEEXISTError(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockLink := mock_netlink.NewMockNetLink(ctrl)
	ec := egressContext{Link: mockLink}

	mockLink.EXPECT().RouteAdd(gomock.Any()).Return(fmt.Errorf("permission denied"))

	err := ec.addRouteWithRetry(testRoute())
	assert.EqualError(t, err, "permission denied")
}

func TestAddRouteWithRetry_ExhaustedRetries(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockLink := mock_netlink.NewMockNetLink(ctrl)
	ec := egressContext{Link: mockLink}

	mockLink.EXPECT().RouteAdd(gomock.Any()).Return(syscall.EEXIST).Times(5)

	err := ec.addRouteWithRetry(testRoute())
	assert.ErrorContains(t, err, "stale route still exists after 5 attempts")
	assert.True(t, errors.Is(err, syscall.EEXIST))
}
