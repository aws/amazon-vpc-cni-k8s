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

package config

import (
	"fmt"
	"net"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultDBPath(t *testing.T) {
	os.Unsetenv(EnvDBPath)
	conf, err := LoadDBConfig()
	assert.NoError(t, err, "loading db config failed")
	assert.Equal(t, DefaultDBPath, conf.DB, "the default DB path will be used if not set by IPAM_DB_PATH")
}

func TestDBPathFromEnv(t *testing.T) {
	os.Setenv(EnvDBPath, "/tmp/test")
	defer os.Unsetenv(EnvDBPath)

	conf, err := LoadDBConfig()
	assert.NoError(t, err, "loading db config failed")
	assert.Equal(t, conf.DB, "/tmp/test")
}

// TestInvalidIPV4Address tests invalid IP address will cause error
func TestInvalidIPV4Address(t *testing.T) {
	conf := `{
			"name": "testnet",
			"cniVersion": "0.3.0",
			"ipam": {
				"type": "ipam",
				"ipv4-subnet": "10.0.0.0/24",
				"ipv4-address": "%s"
			}
		}`

	_, _, err := LoadIPAMConfig([]byte(fmt.Sprintf(conf, "10.0.0")), "")
	assert.Error(t, err, "expect error for invalid ip address")

	_, _, err = LoadIPAMConfig([]byte(fmt.Sprintf(conf, "10.0.0.1")), "")
	assert.Error(t, err, "expect error for missing mask in ipv4-address")

	_, _, err = LoadIPAMConfig([]byte(fmt.Sprintf(conf, "10.0.0.2/24")), "")
	assert.NoError(t, err, "valid ip address should not cause loading configuration error")

	_, _, err = LoadIPAMConfig([]byte(fmt.Sprintf(conf, "")), "")
	assert.Error(t, err, "expect error for missing IP address in the configuration")
}

// TestIPNotINSubnet tests if the specified ip is not in the subnet
func TestIPNotINSubnet(t *testing.T) {
	conf := `{
			"name": "testnet",
			"cniVersion": "0.3.0",
			"ipam": {
				"type": "ipam",
				"ipv4-gateway": "10.0.0.1",
				"ipv4-subnet": '10.0.0.0/24'
				"ipv4-address": "10.0.1.2/24"
			}
		}`

	_, _, err := LoadIPAMConfig([]byte(conf), "")
	assert.Error(t, err, "Specified IP not in the subnet should cause error")
}

// TestGatewayNotINSubnet tests if the specified ip is not in the subnet
func TestGatewayNotINSubnet(t *testing.T) {
	conf := `{
			"name": "testnet",
			"cniVersion": "0.3.0",
			"ipam": {
				"type": "ipam",
				"ipv4-gateway": "10.0.1.1",
				"ipv4-subnet": '10.0.0.0/24'
				"ipv4-address": "10.0.0.2/24"
			}
		}`

	_, _, err := LoadIPAMConfig([]byte(conf), "")
	assert.Error(t, err, "Specified gateway not in the subnet should cause error")
}

// TestIPIsNetworkAddress tests use network address should cause error
func TestIPIsNetworkAddress(t *testing.T) {
	conf := `{
			"name": "testnet",
			"cniVersion": "0.3.0",
			"ipam": {
				"type": "ipam",
				"ipv4-gateway": "10.0.0.1",
				"ipv4-subnet": "10.0.0.0/24",
				"ipv4-address": "10.0.0.0/24"
			}
		}`

	_, _, err := LoadIPAMConfig([]byte(conf), "")
	assert.Error(t, err, "Specified IP is the network address should cause error")
}

// TestIPIsBroadcastAddress tests use broadcast address should cause error
func TestIPIsBroadcastAddress(t *testing.T) {
	conf := `{
			"name": "testnet",
			"cniVersion": "0.3.0",
			"ipam": {
				"type": "ipam",
				"ipv4-gateway": "10.0.0.1",
				"ipv4-subnet": "10.0.0.0/24",
				"ipv4-address": "10.0.0.255/24"
			}
		}`

	_, _, err := LoadIPAMConfig([]byte(conf), "")
	assert.Error(t, err, "Specified IP is the broadcast address should cause error")
}

// TestGWIsNetworkAddress tests use network address should cause error
func TestGWIsNetworkAddress(t *testing.T) {
	conf := `{
			"name": "testnet",
			"cniVersion": "0.3.0",
			"ipam": {
				"type": "ipam",
				"ipv4-gateway": "10.0.0.0",
				"ipv4-subnet": "10.0.0.0/24",
				"ipv4-address": "10.0.0.2/24"
			}
		}`

	_, _, err := LoadIPAMConfig([]byte(conf), "")
	assert.Error(t, err, "Specified gateway is the broadcast address should cause error")
}

// TestGWIsBroadcastAddress tests use broadcast address should cause error
func TestGWIsBroadcastAddress(t *testing.T) {
	conf := `{
			"name": "testnet",
			"cniVersion": "0.3.0",
			"ipam": {
				"type": "ipam",
				"ipv4-gateway": "10.0.1.255",
				"ipv4-subnet": "10.0.0.0/24",
				"ipv4-address": "10.0.0.2/24"
			}
		}`

	_, _, err := LoadIPAMConfig([]byte(conf), "")
	assert.Error(t, err, "Specified gateway is the broadcast address should cause error")
}

// TestEmptySubnet tests missing subnet will cause error
func TestEmptySubnet(t *testing.T) {
	conf := `{
			"name": "testnet",
			"cniVersion": "0.3.0",
			"ipam": {
				"type": "ipam",
				"ipv4-gateway": "10.0.0.1",
				"ipv4-address": "10.0.0.2/24"
			}
		}`

	_, _, err := LoadIPAMConfig([]byte(conf), "")
	assert.Error(t, err, "expect error for missing subnet")
}

// TestDefaultGateway tests the default gateway will be given if gateway is not specified
func TestDefaultGateway(t *testing.T) {
	conf := `{
			"name": "testnet",
			"cniVersion": "0.3.0",
			"ipam": {
				"type": "ipam",
				"ipv4-address": "10.0.0.2/24",
				"ipv4-subnet": "10.0.0.0/24"
			}
		}`

	ipamConf, _, err := LoadIPAMConfig([]byte(conf), "")
	require.NoError(t, err, "valid configuration should not cause error")

	assert.Equal(t, ipamConf.IPV4Gateway.To4(), net.ParseIP("10.0.0.1").To4(), "expect to set the first address as default gateway")
}

func TestIPv4HappyPath(t *testing.T) {
	conf := `{
			"name": "testnet",
			"cniVersion": "0.3.0",
			"ipam": {
				"type": "ipam",
				"ipv4-address": "10.0.0.2/16",
				"ipv4-subnet": "10.0.0.0/16",
				"ipv4-gateway": "10.0.0.8",
				"ipv4-routes": [
				{"dst": "192.168.2.3/32"}
				]
			}
		}`

	ipamConf, _, err := LoadIPAMConfig([]byte(conf), "")
	require.NoError(t, err, "valid configuration should not cause error")

	assert.Equal(t, ipamConf.IPV4Gateway, net.ParseIP("10.0.0.8"), "result should be same as configured")
	assert.Equal(t, ipamConf.IPV4Address.IP, net.ParseIP("10.0.0.2"), "result should be same as configured")
	assert.Equal(t, ipamConf.IPV4Routes[0].Dst.String(), "192.168.2.3/32", "result should be same as configured")
}

func TestIsNetwokOrBroadcast(t *testing.T) {
	_, subnet, err := net.ParseCIDR("10.0.0.2/29")
	assert.NoError(t, err)

	result := isNetworkOrBroadcast(*subnet, net.ParseIP("10.0.0.0"))
	assert.True(t, result, "all 0 should be the network address of subnet")

	result = isNetworkOrBroadcast(*subnet, net.ParseIP("10.0.0.2"))
	assert.False(t, result, "regular ip is not broadcast or network address")

	result = isNetworkOrBroadcast(*subnet, net.ParseIP("10.0.0.7"))
	assert.True(t, result, "all 1 should be the broadcast address of subnet")
}
