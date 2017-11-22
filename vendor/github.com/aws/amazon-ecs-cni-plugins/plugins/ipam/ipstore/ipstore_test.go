// +build !integration,!e2e

// Copyright 2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

package ipstore

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNextIPNoAvailableIP tests no available IP in the subnet
func TestNextIPNoAvailableIP(t *testing.T) {
	ip := net.ParseIP("10.0.0.1")

	// No available ip in the subnet
	_, subnet, err := net.ParseCIDR("10.0.0.0/31")
	assert.NoError(t, err, "Parsing the subnet failed")

	_, err = NextIP(ip, *subnet)
	assert.Error(t, err, "no avaialble ip in the subnet should cause error")
}

func TestNextIPHappyPath(t *testing.T) {
	ip := net.ParseIP("10.0.0.1")
	_, subnet, err := net.ParseCIDR("10.0.0.0/30")
	assert.NoError(t, err, "Parsing the subnet failed")

	nextIP, err := NextIP(ip, *subnet)
	assert.NoError(t, err, "error is not expected to get next ip")

	assert.Equal(t, "10.0.0.2", nextIP.String(), "next ip should be increase 1 by current ip")
}

// TestNextIPCircle tests it will start from minimum address if reached the maximum one
func TestNextIPCircle(t *testing.T) {
	ip := net.ParseIP("10.0.0.2")
	_, subnet, err := net.ParseCIDR("10.0.0.0/30")
	assert.NoError(t, err, "Parsing the subnet failed")

	nextIP, err := NextIP(ip, *subnet)
	assert.NoError(t, err, "error is not expected to get next ip")
	assert.Equal(t, "10.0.0.1", nextIP.String(), "the minimum one should be returned if reached to the maximum")
}

func TestNextIPInvalidIPV4(t *testing.T) {
	ip := net.ParseIP("10.0.0.3.2.3")
	_, subnet, err := net.ParseCIDR("10.0.0.0/30")
	assert.NoError(t, err, "Parsing the subnet failed")

	_, err = NextIP(ip, *subnet)
	assert.Error(t, err, "invalid ipv4 address should cause error")
}

func TestNextIPNotInSubnet(t *testing.T) {
	ip := net.ParseIP("10.0.0.3")
	_, subnet, err := net.ParseCIDR("10.1.0.0/16")
	assert.NoError(t, err)

	_, err = NextIP(ip, *subnet)
	assert.Error(t, err)
}
