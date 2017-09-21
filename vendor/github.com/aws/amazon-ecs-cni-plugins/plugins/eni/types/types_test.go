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

package types

import (
	"testing"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/stretchr/testify/assert"
)

var validConfigNoIPV6 = `{"eni":"eni1", "ipv4-address":"10.11.12.13", "mac":"01:23:45:67:89:ab"}`
var configNoENIID = `{"ipv4-address":"10.11.12.13", "mac":"01:23:45:67:89:ab"}`
var configNoIPV4Address = `{"eni":"eni1", "mac":"01:23:45:67:89:ab"}`
var configNoMAC = `{"eni":"eni1", "ipv4-address":"10.11.12.13"}`
var configInvalidIPV4AddressMalformed = `{"eni":"eni1", "ipv4-address":"1", "mac":"01:23:45:67:89:ab"}`
var configInvalidIPV4AddressIPV6 = `{"eni":"eni1", "ipv4-address":"2001:db8::68", "mac":"01:23:45:67:89:ab"}`
var configMalformedMAC = `{"eni":"eni1", "ipv4-address":"10.11.12.13", "mac":"01:23:45:67:89"}`
var validConfigWithIPV6 = `{
	"eni":"eni1",
	"ipv4-address":"10.11.12.13",
	"mac":"01:23:45:67:89:ab",
	"ipv6-address":"2001:db8::68"
}`
var configInvalidIPV6AddressMalformed = `{
	"eni":"eni1",
	"ipv4-address":"10.11.12.13",
	"mac":"01:23:45:67:89:ab",
	"ipv6-address":"2001:::68"
}`
var configInvalidIPV6AddressIPV4 = `{
	"eni":"eni1",
	"ipv4-address":"10.11.12.13",
	"mac":"01:23:45:67:89:ab",
	"ipv6-address":"10.11.12.13"
}`

func TestNewConfWithValidConfig(t *testing.T) {
	args := &skel.CmdArgs{
		StdinData: []byte(validConfigWithIPV6),
	}
	_, err := NewConf(args)
	assert.NoError(t, err)
}

func TestNewConfWithEmptyConfig(t *testing.T) {
	args := &skel.CmdArgs{
		StdinData: []byte(""),
	}
	_, err := NewConf(args)
	assert.Error(t, err)
}

func TestNewConfWithInvalidConfig(t *testing.T) {
	args := &skel.CmdArgs{
		StdinData: []byte(`{"foo":"eni1"}`),
	}
	_, err := NewConf(args)
	assert.Error(t, err)
}

func TestNewConfWithMissingENIIDConfig(t *testing.T) {
	args := &skel.CmdArgs{
		StdinData: []byte(configNoENIID),
	}
	_, err := NewConf(args)
	assert.Error(t, err)
}

func TestNewConfWithMissingIPV4AddressConfig(t *testing.T) {
	args := &skel.CmdArgs{
		StdinData: []byte(configNoIPV4Address),
	}
	_, err := NewConf(args)
	assert.Error(t, err)
}

func TestNewConfWithMissingMACConfig(t *testing.T) {
	args := &skel.CmdArgs{
		StdinData: []byte(configNoMAC),
	}
	_, err := NewConf(args)
	assert.Error(t, err)
}

func TestNewConfWithInvalidMalformedIPV4AddressConfig(t *testing.T) {
	args := &skel.CmdArgs{
		StdinData: []byte(configInvalidIPV4AddressMalformed),
	}
	_, err := NewConf(args)
	assert.Error(t, err)
}

func TestNewConfWithInvalidIPV4AddressIPV6Config(t *testing.T) {
	args := &skel.CmdArgs{
		StdinData: []byte(configInvalidIPV4AddressIPV6),
	}
	_, err := NewConf(args)
	assert.Error(t, err)
}

func TestNewConfWithInvalidMACConfig(t *testing.T) {
	args := &skel.CmdArgs{
		StdinData: []byte(configMalformedMAC),
	}
	_, err := NewConf(args)
	assert.Error(t, err)
}

func TestNewConfWithInvalidIPV4AddressMalformed(t *testing.T) {
	args := &skel.CmdArgs{
		StdinData: []byte(configInvalidIPV6AddressMalformed),
	}
	_, err := NewConf(args)
	assert.Error(t, err)
}

func TestNewConfWithInvalidIPV4AddressIPv4(t *testing.T) {
	args := &skel.CmdArgs{
		StdinData: []byte(configInvalidIPV6AddressIPV4),
	}
	_, err := NewConf(args)
	assert.Error(t, err)
}
