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

package networkutils

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/vishvananda/netlink"

	"github.com/aws/amazon-ecs-agent/agent/eni/netlinkwrapper/mocks"
)

const (
	randomDevice     = "eth1"
	validMAC         = "00:0a:95:9d:68:16"
	pciDevPath       = " ../../devices/pci0000:00/0000:00:03.0/net/eth1"
	virtualDevPath   = "../../devices/virtual/net/lo"
	invalidDevPath   = "../../virtual/net/lo"
	incorrectDevPath = "../../devices/totally/wrong/net/path"
)

// TestGetMACAddress checks obtaining MACAddress by using netlinkClient
func TestGetMACAddress(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()
	mockNetlink := mock_netlinkwrapper.NewMockNetLink(mockCtrl)
	pm, _ := net.ParseMAC(validMAC)
	mockNetlink.EXPECT().LinkByName(randomDevice).Return(
		&netlink.Device{
			LinkAttrs: netlink.LinkAttrs{
				HardwareAddr: pm,
				Name:         randomDevice,
			},
		}, nil)
	ctx := context.TODO()
	mac, err := GetMACAddress(ctx, time.Millisecond, randomDevice, mockNetlink)
	assert.Nil(t, err)
	assert.Equal(t, mac, validMAC)
}

// TestGetMACAddressWithNetlinkError attempts to test the netlinkClient
// error code path
func TestGetMACAddressWithNetlinkError(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()
	mockNetlink := mock_netlinkwrapper.NewMockNetLink(mockCtrl)
	// LinkByName returns an error. This will ensure that a non-retriable
	// error is generated and halts the backoff-rety loop
	mockNetlink.EXPECT().LinkByName(randomDevice).Return(
		&netlink.Device{},
		errors.New("Dummy Netlink Error"))
	ctx := context.TODO()
	mac, err := GetMACAddress(ctx, 2*macAddressBackoffMax, randomDevice, mockNetlink)
	assert.Error(t, err)
	assert.Empty(t, mac)
}

// TestGetMACAddressNotFoundRetry tests if netlink.LinkByName gets invoked
// multiple times if the mac address for a device is returned as empty string
func TestGetMACAddressNotFoundRetry(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()
	mockNetlink := mock_netlinkwrapper.NewMockNetLink(mockCtrl)
	pm, _ := net.ParseMAC(validMAC)
	gomock.InOrder(
		// Return empty mac address on first invocation
		mockNetlink.EXPECT().LinkByName(randomDevice).Return(&netlink.Device{}, nil),
		// Return a valid mac address on first invocation. Even though the first
		// invocation did not result in an error, it did return an empty mac address.
		// Hence, we expect it to be retried
		mockNetlink.EXPECT().LinkByName(randomDevice).Return(&netlink.Device{
			LinkAttrs: netlink.LinkAttrs{
				HardwareAddr: pm,
				Name:         randomDevice,
			},
		}, nil),
	)
	ctx := context.TODO()
	// Set max retry duration to twice that of the min backoff to ensure that there's
	// enough time to retry
	mac, err := GetMACAddress(ctx, 2*macAddressBackoffMin, randomDevice, mockNetlink)
	assert.NoError(t, err)
	assert.Equal(t, mac, validMAC)
}

// TestGetMACAddressNotFoundRetryExpires tests if the backoff-retry logic for
// retrieving mac address returns error on timeout
func TestGetMACAddressNotFoundRetryExpires(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()
	mockNetlink := mock_netlinkwrapper.NewMockNetLink(mockCtrl)
	// Return empty mac address everytime
	mockNetlink.EXPECT().LinkByName(randomDevice).Return(&netlink.Device{}, nil).MinTimes(1)
	ctx := context.TODO()
	// Set max retry duration to twice that of the min backoff to ensure that there's
	// enough time to retry
	mac, err := GetMACAddress(ctx, 2*macAddressBackoffMin, randomDevice, mockNetlink)
	assert.Error(t, err)
	assert.Empty(t, mac)
}

// TestIsValidDevicePathTableTest does a table test for device path validity
func TestIsValidDevicePathTableTest(t *testing.T) {
	var table = []struct {
		input  string
		output bool
	}{
		{pciDevPath, true},
		{virtualDevPath, false},
		{invalidDevPath, false},
		{incorrectDevPath, false},
	}

	for _, entry := range table {
		status := IsValidNetworkDevice(entry.input)
		assert.Equal(t, status, entry.output)
	}
}
