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

package commands

import (
	"errors"
	"io/ioutil"
	"os"
	"testing"

	"github.com/aws/amazon-ecs-cni-plugins/plugins/eni/engine/mocks"
	"github.com/cihub/seelog"
	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types/current"
	"github.com/containernetworking/cni/pkg/version"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	eniIPV4Address               = "10.11.12.13"
	eniIPV6Address               = "2001:db8::68"
	eniID                        = "eni1"
	deviceName                   = "eth1"
	nsName                       = "ns1"
	macAddressSanitized          = "mac1"
	eniIPV4Gateway               = "10.10.10.10"
	eniIPV6Gateway               = "2001:db9::68"
	eniIPV4SubnetMask            = "20"
	eniIPV6SubnetMask            = "32"
	mac                          = "01:23:45:67:89:ab"
	eniIPV4AddressWithSubnetMask = "10.11.12.13/20"
	eniIPV6AddressWithSubnetMask = "2001:db8::68/32"
)

var eniArgs = &skel.CmdArgs{
	StdinData: []byte(`{"cniVersion": "0.3.0",` +
		`"eni":"` + eniID +
		`", "ipv4-address":"` + eniIPV4Address +
		`", "mac":"` + mac +
		`", "ipv6-address":"` + eniIPV6Address +
		`"}`),
	Netns: nsName,
}

var eniArgsNoIPV6 = &skel.CmdArgs{
	StdinData: []byte(`{"cniVersion": "0.3.0",` +
		`"eni":"` + eniID +
		`", "ipv4-address":"` + eniIPV4Address +
		`", "mac":"` + mac +
		`"}`),
	Netns: nsName,
}

// TODO: Add integration tests for command.Add commands.Del

func TestAddWithInvalidConfig(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockEngine := mock_engine.NewMockEngine(ctrl)
	mockDHClient := mock_engine.NewMockDHClient(ctrl)

	err := add(&skel.CmdArgs{}, mockEngine, mockDHClient)
	assert.Error(t, err)
}

func TestAddIsDHClientInPathFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockEngine := mock_engine.NewMockEngine(ctrl)
	mockDHClient := mock_engine.NewMockDHClient(ctrl)

	mockDHClient.EXPECT().IsExecutableInPath().Return(false)

	err := add(eniArgs, mockEngine, mockDHClient)
	assert.Error(t, err)
	assert.Equal(t, err, dhclientNotFoundError)
}

func TestAddGetAllMACAddressesFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockEngine := mock_engine.NewMockEngine(ctrl)
	mockDHClient := mock_engine.NewMockDHClient(ctrl)

	gomock.InOrder(
		mockDHClient.EXPECT().IsExecutableInPath().Return(true),
		mockEngine.EXPECT().GetAllMACAddresses().Return([]string{}, errors.New("error")),
	)

	err := add(eniArgs, mockEngine, mockDHClient)
	assert.Error(t, err)
}

func TestAddGetMACAddressesForENIFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockEngine := mock_engine.NewMockEngine(ctrl)
	mockDHClient := mock_engine.NewMockDHClient(ctrl)
	macAddresses := []string{macAddressSanitized}
	gomock.InOrder(
		mockDHClient.EXPECT().IsExecutableInPath().Return(true),
		mockEngine.EXPECT().GetAllMACAddresses().Return(macAddresses, nil),
		mockEngine.EXPECT().GetMACAddressOfENI(macAddresses, eniID).Return("", errors.New("error")),
	)

	err := add(eniArgs, mockEngine, mockDHClient)
	assert.Error(t, err)
}

func TestAddDoesMACAddressMapToIPV4AddressFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockEngine := mock_engine.NewMockEngine(ctrl)
	mockDHClient := mock_engine.NewMockDHClient(ctrl)
	macAddress := macAddressSanitized
	macAddresses := []string{macAddress}
	gomock.InOrder(
		mockDHClient.EXPECT().IsExecutableInPath().Return(true),
		mockEngine.EXPECT().GetAllMACAddresses().Return(macAddresses, nil),
		mockEngine.EXPECT().GetMACAddressOfENI(macAddresses, eniID).Return(macAddress, nil),
		mockEngine.EXPECT().DoesMACAddressMapToIPV4Address(macAddress, eniIPV4Address).Return(false, errors.New("error")),
	)

	err := add(eniArgs, mockEngine, mockDHClient)
	assert.Error(t, err)
	assert.NotEqual(t, err, unmappedIPV4AddressError)
}

func TestAddDoesMACAddressMapToIPV4AddressReturnsFalse(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockEngine := mock_engine.NewMockEngine(ctrl)
	mockDHClient := mock_engine.NewMockDHClient(ctrl)
	macAddress := macAddressSanitized
	macAddresses := []string{macAddress}
	gomock.InOrder(
		mockDHClient.EXPECT().IsExecutableInPath().Return(true),
		mockEngine.EXPECT().GetAllMACAddresses().Return(macAddresses, nil),
		mockEngine.EXPECT().GetMACAddressOfENI(macAddresses, eniID).Return(macAddress, nil),
		mockEngine.EXPECT().DoesMACAddressMapToIPV4Address(macAddress, eniIPV4Address).Return(false, nil),
	)

	err := add(eniArgs, mockEngine, mockDHClient)
	assert.Error(t, err)
	assert.Equal(t, err, unmappedIPV4AddressError)
}

func TestAddDoesMACAddressMapToIPV6AddressFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockEngine := mock_engine.NewMockEngine(ctrl)
	mockDHClient := mock_engine.NewMockDHClient(ctrl)
	macAddress := macAddressSanitized
	macAddresses := []string{macAddress}
	gomock.InOrder(
		mockDHClient.EXPECT().IsExecutableInPath().Return(true),
		mockEngine.EXPECT().GetAllMACAddresses().Return(macAddresses, nil),
		mockEngine.EXPECT().GetMACAddressOfENI(macAddresses, eniID).Return(macAddress, nil),
		mockEngine.EXPECT().DoesMACAddressMapToIPV4Address(macAddress, eniIPV4Address).Return(true, nil),
		mockEngine.EXPECT().DoesMACAddressMapToIPV6Address(macAddress, eniIPV6Address).Return(false, errors.New("error")),
	)

	err := add(eniArgs, mockEngine, mockDHClient)
	assert.Error(t, err)
}

func TestAddDoesMACAddressMapToIPV6AddressReturnsFalse(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockEngine := mock_engine.NewMockEngine(ctrl)
	mockDHClient := mock_engine.NewMockDHClient(ctrl)
	macAddress := macAddressSanitized
	macAddresses := []string{macAddress}
	gomock.InOrder(
		mockDHClient.EXPECT().IsExecutableInPath().Return(true),
		mockEngine.EXPECT().GetAllMACAddresses().Return(macAddresses, nil),
		mockEngine.EXPECT().GetMACAddressOfENI(macAddresses, eniID).Return(macAddress, nil),
		mockEngine.EXPECT().DoesMACAddressMapToIPV4Address(macAddress, eniIPV4Address).Return(true, nil),
		mockEngine.EXPECT().DoesMACAddressMapToIPV6Address(macAddress, eniIPV6Address).Return(false, nil),
	)

	err := add(eniArgs, mockEngine, mockDHClient)
	assert.Error(t, err)
	assert.Equal(t, err, unmappedIPV6AddressError)
}

func TestAddGetInterfaceDeviceNameFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockEngine := mock_engine.NewMockEngine(ctrl)
	mockDHClient := mock_engine.NewMockDHClient(ctrl)
	macAddress := macAddressSanitized
	macAddresses := []string{macAddress}
	gomock.InOrder(
		mockDHClient.EXPECT().IsExecutableInPath().Return(true),
		mockEngine.EXPECT().GetAllMACAddresses().Return(macAddresses, nil),
		mockEngine.EXPECT().GetMACAddressOfENI(macAddresses, eniID).Return(macAddress, nil),
		mockEngine.EXPECT().DoesMACAddressMapToIPV4Address(macAddress, eniIPV4Address).Return(true, nil),
		mockEngine.EXPECT().DoesMACAddressMapToIPV6Address(macAddress, eniIPV6Address).Return(true, nil),
		mockEngine.EXPECT().GetInterfaceDeviceName(macAddress).Return("", errors.New("error")),
	)

	err := add(eniArgs, mockEngine, mockDHClient)
	assert.Error(t, err)
}

func TestAddGetIPV4GatewayNetmaskFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockEngine := mock_engine.NewMockEngine(ctrl)
	mockDHClient := mock_engine.NewMockDHClient(ctrl)
	macAddress := macAddressSanitized
	macAddresses := []string{macAddress}
	gomock.InOrder(
		mockDHClient.EXPECT().IsExecutableInPath().Return(true),
		mockEngine.EXPECT().GetAllMACAddresses().Return(macAddresses, nil),
		mockEngine.EXPECT().GetMACAddressOfENI(macAddresses, eniID).Return(macAddress, nil),
		mockEngine.EXPECT().DoesMACAddressMapToIPV4Address(macAddress, eniIPV4Address).Return(true, nil),
		mockEngine.EXPECT().DoesMACAddressMapToIPV6Address(macAddress, eniIPV6Address).Return(true, nil),
		mockEngine.EXPECT().GetInterfaceDeviceName(macAddress).Return(deviceName, nil),
		mockEngine.EXPECT().GetIPV4GatewayNetmask(macAddress).Return("", "", errors.New("error")),
	)

	err := add(eniArgs, mockEngine, mockDHClient)
	assert.Error(t, err)
}

func TestAddGetIPV4GatewayNetmaskFailsNoIPV6(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockEngine := mock_engine.NewMockEngine(ctrl)
	mockDHClient := mock_engine.NewMockDHClient(ctrl)
	macAddress := macAddressSanitized
	macAddresses := []string{macAddress}
	gomock.InOrder(
		mockDHClient.EXPECT().IsExecutableInPath().Return(true),
		mockEngine.EXPECT().GetAllMACAddresses().Return(macAddresses, nil),
		mockEngine.EXPECT().GetMACAddressOfENI(macAddresses, eniID).Return(macAddress, nil),
		mockEngine.EXPECT().DoesMACAddressMapToIPV4Address(macAddress, eniIPV4Address).Return(true, nil),
		mockEngine.EXPECT().GetInterfaceDeviceName(macAddress).Return(deviceName, nil),
		mockEngine.EXPECT().GetIPV4GatewayNetmask(macAddress).Return("", "", errors.New("error")),
	)

	err := add(eniArgsNoIPV6, mockEngine, mockDHClient)
	assert.Error(t, err)
}

func TestAddGetIPV6SubnetMaskFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockEngine := mock_engine.NewMockEngine(ctrl)
	mockDHClient := mock_engine.NewMockDHClient(ctrl)
	macAddress := macAddressSanitized
	macAddresses := []string{macAddress}
	gomock.InOrder(
		mockDHClient.EXPECT().IsExecutableInPath().Return(true),
		mockEngine.EXPECT().GetAllMACAddresses().Return(macAddresses, nil),
		mockEngine.EXPECT().GetMACAddressOfENI(macAddresses, eniID).Return(macAddress, nil),
		mockEngine.EXPECT().DoesMACAddressMapToIPV4Address(macAddress, eniIPV4Address).Return(true, nil),
		mockEngine.EXPECT().DoesMACAddressMapToIPV6Address(macAddress, eniIPV6Address).Return(true, nil),
		mockEngine.EXPECT().GetInterfaceDeviceName(macAddress).Return(deviceName, nil),
		mockEngine.EXPECT().GetIPV4GatewayNetmask(macAddress).Return(eniIPV4Gateway, eniIPV4SubnetMask, nil),
		mockEngine.EXPECT().GetIPV6PrefixLength(macAddress).Return("", errors.New("error")),
	)

	err := add(eniArgs, mockEngine, mockDHClient)
	assert.Error(t, err)
}

func TestAddGetIPV6GatewayFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockEngine := mock_engine.NewMockEngine(ctrl)
	mockDHClient := mock_engine.NewMockDHClient(ctrl)
	macAddress := macAddressSanitized
	macAddresses := []string{macAddress}
	gomock.InOrder(
		mockDHClient.EXPECT().IsExecutableInPath().Return(true),
		mockEngine.EXPECT().GetAllMACAddresses().Return(macAddresses, nil),
		mockEngine.EXPECT().GetMACAddressOfENI(macAddresses, eniID).Return(macAddress, nil),
		mockEngine.EXPECT().DoesMACAddressMapToIPV4Address(macAddress, eniIPV4Address).Return(true, nil),
		mockEngine.EXPECT().DoesMACAddressMapToIPV6Address(macAddress, eniIPV6Address).Return(true, nil),
		mockEngine.EXPECT().GetInterfaceDeviceName(macAddress).Return(deviceName, nil),
		mockEngine.EXPECT().GetIPV4GatewayNetmask(macAddress).Return(eniIPV4Gateway, eniIPV4SubnetMask, nil),
		mockEngine.EXPECT().GetIPV6PrefixLength(macAddress).Return(eniIPV6SubnetMask, nil),
		mockEngine.EXPECT().GetIPV6Gateway(deviceName).Return("", errors.New("error")),
	)

	err := add(eniArgs, mockEngine, mockDHClient)
	assert.Error(t, err)
}

func TestAddSetupContainerNamespaceFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockEngine := mock_engine.NewMockEngine(ctrl)
	mockDHClient := mock_engine.NewMockDHClient(ctrl)
	macAddress := macAddressSanitized
	macAddresses := []string{macAddress}
	gomock.InOrder(
		mockDHClient.EXPECT().IsExecutableInPath().Return(true),
		mockEngine.EXPECT().GetAllMACAddresses().Return(macAddresses, nil),
		mockEngine.EXPECT().GetMACAddressOfENI(macAddresses, eniID).Return(macAddress, nil),
		mockEngine.EXPECT().DoesMACAddressMapToIPV4Address(macAddress, eniIPV4Address).Return(true, nil),
		mockEngine.EXPECT().DoesMACAddressMapToIPV6Address(macAddress, eniIPV6Address).Return(true, nil),
		mockEngine.EXPECT().GetInterfaceDeviceName(macAddress).Return(deviceName, nil),
		mockEngine.EXPECT().GetIPV4GatewayNetmask(macAddress).Return(eniIPV4Gateway, eniIPV4SubnetMask, nil),
		mockEngine.EXPECT().GetIPV6PrefixLength(macAddress).Return(eniIPV6SubnetMask, nil),
		mockEngine.EXPECT().GetIPV6Gateway(deviceName).Return(eniIPV6Gateway, nil),
		mockEngine.EXPECT().SetupContainerNamespace(nsName, deviceName, eniIPV4AddressWithSubnetMask,
			eniIPV6AddressWithSubnetMask, eniIPV4Gateway, eniIPV6Gateway, mockDHClient, false).Return(errors.New("error")),
	)

	err := add(eniArgs, mockEngine, mockDHClient)
	assert.Error(t, err)
}

func TestAddSetupContainerNamespaceFailsNoIPV6(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockEngine := mock_engine.NewMockEngine(ctrl)
	mockDHClient := mock_engine.NewMockDHClient(ctrl)
	macAddress := macAddressSanitized
	macAddresses := []string{macAddress}
	gomock.InOrder(
		mockDHClient.EXPECT().IsExecutableInPath().Return(true),
		mockEngine.EXPECT().GetAllMACAddresses().Return(macAddresses, nil),
		mockEngine.EXPECT().GetMACAddressOfENI(macAddresses, eniID).Return(macAddress, nil),
		mockEngine.EXPECT().DoesMACAddressMapToIPV4Address(macAddress, eniIPV4Address).Return(true, nil),
		mockEngine.EXPECT().GetInterfaceDeviceName(macAddress).Return(deviceName, nil),
		mockEngine.EXPECT().GetIPV4GatewayNetmask(macAddress).Return(eniIPV4Gateway, eniIPV4SubnetMask, nil),
		mockEngine.EXPECT().SetupContainerNamespace(nsName, deviceName, eniIPV4AddressWithSubnetMask, "",
			eniIPV4Gateway, "", mockDHClient, false).Return(errors.New("error")),
	)

	err := add(eniArgsNoIPV6, mockEngine, mockDHClient)
	assert.Error(t, err)
}

func TestAddNoError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockEngine := mock_engine.NewMockEngine(ctrl)
	mockDHClient := mock_engine.NewMockDHClient(ctrl)
	macAddress := macAddressSanitized
	macAddresses := []string{macAddress}
	gomock.InOrder(
		mockDHClient.EXPECT().IsExecutableInPath().Return(true),
		mockEngine.EXPECT().GetAllMACAddresses().Return(macAddresses, nil),
		mockEngine.EXPECT().GetMACAddressOfENI(macAddresses, eniID).Return(macAddress, nil),
		mockEngine.EXPECT().DoesMACAddressMapToIPV4Address(macAddress, eniIPV4Address).Return(true, nil),
		mockEngine.EXPECT().DoesMACAddressMapToIPV6Address(macAddress, eniIPV6Address).Return(true, nil),
		mockEngine.EXPECT().GetInterfaceDeviceName(macAddress).Return(deviceName, nil),
		mockEngine.EXPECT().GetIPV4GatewayNetmask(macAddress).Return(eniIPV4Gateway, eniIPV4SubnetMask, nil),
		mockEngine.EXPECT().GetIPV6PrefixLength(macAddress).Return(eniIPV6SubnetMask, nil),
		mockEngine.EXPECT().GetIPV6Gateway(deviceName).Return(eniIPV6Gateway, nil),
		mockEngine.EXPECT().SetupContainerNamespace(nsName, deviceName, eniIPV4AddressWithSubnetMask,
			eniIPV6AddressWithSubnetMask, eniIPV4Gateway, eniIPV6Gateway, mockDHClient, false).Return(nil),
	)

	err := add(eniArgs, mockEngine, mockDHClient)
	assert.NoError(t, err)
}

func TestAddNoErrorWhenIPV6AddressNotSpecifiedInConfig(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockEngine := mock_engine.NewMockEngine(ctrl)
	mockDHClient := mock_engine.NewMockDHClient(ctrl)
	macAddress := macAddressSanitized
	macAddresses := []string{macAddress}
	gomock.InOrder(
		mockDHClient.EXPECT().IsExecutableInPath().Return(true),
		mockEngine.EXPECT().GetAllMACAddresses().Return(macAddresses, nil),
		mockEngine.EXPECT().GetMACAddressOfENI(macAddresses, eniID).Return(macAddress, nil),
		mockEngine.EXPECT().DoesMACAddressMapToIPV4Address(macAddress, eniIPV4Address).Return(true, nil),
		mockEngine.EXPECT().GetInterfaceDeviceName(macAddress).Return(deviceName, nil),
		mockEngine.EXPECT().GetIPV4GatewayNetmask(macAddress).Return(eniIPV4Gateway, eniIPV4SubnetMask, nil),
		mockEngine.EXPECT().SetupContainerNamespace(nsName, deviceName, eniIPV4AddressWithSubnetMask, "",
			eniIPV4Gateway, "", mockDHClient, false).Return(nil),
	)

	err := add(eniArgsNoIPV6, mockEngine, mockDHClient)
	assert.NoError(t, err)
}

// TestAddPrintResult tests the add command return compatiable result as cni
func TestAddPrintResult(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Turn off the log for test, as the test needs to read the result returned by the plugin from stdout
	logger, err := seelog.LoggerFromConfigAsString(`
	<seelog minlevel="off"></seelog>
	`)
	assert.NoError(t, err, "create new logger failed")
	err = seelog.ReplaceLogger(logger)
	assert.NoError(t, err, "turn off the logger failed")

	mockEngine := mock_engine.NewMockEngine(ctrl)
	mockDHClient := mock_engine.NewMockDHClient(ctrl)
	macAddress := macAddressSanitized
	macAddresses := []string{macAddress}

	gomock.InOrder(
		mockDHClient.EXPECT().IsExecutableInPath().Return(true),
		mockEngine.EXPECT().GetAllMACAddresses().Return(macAddresses, nil),
		mockEngine.EXPECT().GetMACAddressOfENI(macAddresses, eniID).Return(macAddress, nil),
		mockEngine.EXPECT().DoesMACAddressMapToIPV4Address(macAddress, eniIPV4Address).Return(true, nil),
		mockEngine.EXPECT().DoesMACAddressMapToIPV6Address(macAddress, eniIPV6Address).Return(true, nil),
		mockEngine.EXPECT().GetInterfaceDeviceName(macAddress).Return(deviceName, nil),
		mockEngine.EXPECT().GetIPV4GatewayNetmask(macAddress).Return(eniIPV4Gateway, eniIPV4SubnetMask, nil),
		mockEngine.EXPECT().GetIPV6PrefixLength(macAddress).Return(eniIPV6SubnetMask, nil),
		mockEngine.EXPECT().GetIPV6Gateway(deviceName).Return(eniIPV6Gateway, nil),
		mockEngine.EXPECT().SetupContainerNamespace(nsName, deviceName, eniIPV4AddressWithSubnetMask,
			eniIPV6AddressWithSubnetMask, eniIPV4Gateway, eniIPV6Gateway, mockDHClient, false).Return(nil),
	)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err, "redirect os.stdin succeed")

	os.Stdout = w
	err = add(eniArgs, mockEngine, mockDHClient)
	assert.NoError(t, err)

	w.Close()
	output, err := ioutil.ReadAll(r)
	require.NoError(t, err, "read from stdin failed")
	os.Stdout = oldStdout

	res, err := version.NewResult("0.3.0", output)
	assert.NoError(t, err, "construct result from stdin failed")
	result, err := current.GetResult(res)
	assert.NoError(t, err, "convert result to current version failed")
	assert.Equal(t, deviceName, result.Interfaces[0].Name)
	assert.Equal(t, macAddressSanitized, result.Interfaces[0].Mac)
	assert.Equal(t, 2, len(result.IPs), "result should contains information of both ipv4 and ipv6")
}

func TestAddPrintResultNoIPV6(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Turn off the log for test, as the test needs to read the result returned by the plugin from stdout
	logger, err := seelog.LoggerFromConfigAsString(`
	<seelog minlevel="off"></seelog>
	`)
	assert.NoError(t, err, "create new logger failed")
	err = seelog.ReplaceLogger(logger)
	assert.NoError(t, err, "turn off the logger failed")

	mockEngine := mock_engine.NewMockEngine(ctrl)
	mockDHClient := mock_engine.NewMockDHClient(ctrl)
	macAddress := macAddressSanitized
	macAddresses := []string{macAddress}

	gomock.InOrder(
		mockDHClient.EXPECT().IsExecutableInPath().Return(true),
		mockEngine.EXPECT().GetAllMACAddresses().Return(macAddresses, nil),
		mockEngine.EXPECT().GetMACAddressOfENI(macAddresses, eniID).Return(macAddress, nil),
		mockEngine.EXPECT().DoesMACAddressMapToIPV4Address(macAddress, eniIPV4Address).Return(true, nil),
		mockEngine.EXPECT().GetInterfaceDeviceName(macAddress).Return(deviceName, nil),
		mockEngine.EXPECT().GetIPV4GatewayNetmask(macAddress).Return(eniIPV4Gateway, eniIPV4SubnetMask, nil),
		mockEngine.EXPECT().SetupContainerNamespace(nsName, deviceName, eniIPV4AddressWithSubnetMask, "",
			eniIPV4Gateway, "", mockDHClient, false).Return(nil),
	)

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err, "redirect os.stdin succeed")

	os.Stdout = w
	err = add(eniArgsNoIPV6, mockEngine, mockDHClient)
	assert.NoError(t, err)

	w.Close()
	output, err := ioutil.ReadAll(r)
	require.NoError(t, err, "read from stdin failed")
	os.Stdout = oldStdout

	res, err := version.NewResult("0.3.0", output)
	assert.NoError(t, err, "construct result from stdin failed")
	result, err := current.GetResult(res)
	assert.NoError(t, err, "convert result to current version failed")
	assert.Equal(t, deviceName, result.Interfaces[0].Name)
	assert.Equal(t, macAddressSanitized, result.Interfaces[0].Mac)
	assert.Equal(t, 1, len(result.IPs), "result should only contains information of ipv4")
}

func TestDelWithInvalidConfig(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockEngine := mock_engine.NewMockEngine(ctrl)
	mockDHClient := mock_engine.NewMockDHClient(ctrl)

	err := del(&skel.CmdArgs{}, mockEngine, mockDHClient)
	assert.Error(t, err)
}

func TestDelFailsWhenTearDownContainerNamespaceFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockEngine := mock_engine.NewMockEngine(ctrl)
	mockDHClient := mock_engine.NewMockDHClient(ctrl)

	mockEngine.EXPECT().TeardownContainerNamespace(nsName, mac, true, mockDHClient).Return(errors.New("error"))
	err := del(eniArgs, mockEngine, mockDHClient)
	assert.Error(t, err)
}

func TestDel(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockEngine := mock_engine.NewMockEngine(ctrl)
	mockDHClient := mock_engine.NewMockDHClient(ctrl)

	mockEngine.EXPECT().TeardownContainerNamespace(nsName, mac, true, mockDHClient).Return(nil)
	err := del(eniArgs, mockEngine, mockDHClient)
	assert.NoError(t, err)
}

func TestDelNoIPV6(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockEngine := mock_engine.NewMockEngine(ctrl)
	mockDHClient := mock_engine.NewMockDHClient(ctrl)

	mockEngine.EXPECT().TeardownContainerNamespace(nsName, mac, false, mockDHClient).Return(nil)
	err := del(eniArgsNoIPV6, mockEngine, mockDHClient)
	assert.NoError(t, err)
}
