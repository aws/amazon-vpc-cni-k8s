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
	"net"
	"testing"

	"github.com/aws/amazon-ecs-cni-plugins/plugins/ecs-bridge/engine/mocks"
	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types/current"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/vishvananda/netlink"
)

const (
	bridgeName    = "ecs-br0"
	defaultMTU    = 1500
	nsName        = "ecs-eni"
	interfaceName = "ecs-veth0"
	hostVethName  = "ecs-host-veth0"
	ipamType      = "ecs-ipam"
	mac           = "01:23:45:67:89:ab"
)

var conf = &skel.CmdArgs{
	StdinData: []byte(`{"bridge":"` + bridgeName +
		`", "ipam":{"type": "` + ipamType + `"}}`),
	Netns:  nsName,
	IfName: interfaceName,
}

var emptyConf = &skel.CmdArgs{
	StdinData: []byte(""),
	Netns:     nsName,
}

var macHWAddr net.HardwareAddr

func init() {
	macHWAddr, _ = net.ParseMAC(mac)
}

// TODO: Add integration tests for command.Add commands.Del

func TestAddConfError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockEngine := mock_engine.NewMockEngine(ctrl)

	err := add(emptyConf, mockEngine)
	assert.Error(t, err)
}

func TestAddCreateBridgeError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockEngine := mock_engine.NewMockEngine(ctrl)
	mockEngine.EXPECT().CreateBridge(bridgeName, defaultMTU).Return(nil, errors.New("error"))
	err := add(conf, mockEngine)
	assert.Error(t, err)
}

func TestAddCreateVethPairError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockEngine := mock_engine.NewMockEngine(ctrl)
	gomock.InOrder(
		mockEngine.EXPECT().CreateBridge(bridgeName, defaultMTU).Return(nil, nil),
		mockEngine.EXPECT().CreateVethPair(nsName, defaultMTU, interfaceName).Return(nil, "", errors.New("error")),
	)
	err := add(conf, mockEngine)
	assert.Error(t, err)
}

func TestAddAttachHostVethInterfaceToBridgeError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockEngine := mock_engine.NewMockEngine(ctrl)
	bridgeLink := &netlink.Bridge{}
	gomock.InOrder(
		mockEngine.EXPECT().CreateBridge(bridgeName, defaultMTU).Return(bridgeLink, nil),
		mockEngine.EXPECT().CreateVethPair(nsName, defaultMTU, interfaceName).Return(nil, hostVethName, nil),
		mockEngine.EXPECT().AttachHostVethInterfaceToBridge(hostVethName, bridgeLink).Return(nil, errors.New("error")),
	)
	err := add(conf, mockEngine)
	assert.Error(t, err)
}

func TestAddRunIPAMPlginAddError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockEngine := mock_engine.NewMockEngine(ctrl)
	bridgeLink := &netlink.Bridge{}
	gomock.InOrder(
		mockEngine.EXPECT().CreateBridge(bridgeName, defaultMTU).Return(bridgeLink, nil),
		mockEngine.EXPECT().CreateVethPair(nsName, defaultMTU, interfaceName).Return(nil, hostVethName, nil),
		mockEngine.EXPECT().AttachHostVethInterfaceToBridge(hostVethName, bridgeLink).Return(nil, nil),
		mockEngine.EXPECT().RunIPAMPluginAdd(ipamType, conf.StdinData).Return(nil, errors.New("error")),
	)
	err := add(conf, mockEngine)
	assert.Error(t, err)
}

func TestAddConfigureContainerVethInterfaceError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockEngine := mock_engine.NewMockEngine(ctrl)
	bridgeLink := &netlink.Bridge{
		LinkAttrs: netlink.LinkAttrs{
			Name:         bridgeName,
			HardwareAddr: macHWAddr,
		},
	}

	result := &current.Result{
		IPs: []*current.IPConfig{
			&current.IPConfig{},
		},
	}
	containerVethInterface := &current.Interface{}
	hostVethInterface := &current.Interface{}
	gomock.InOrder(
		mockEngine.EXPECT().CreateBridge(bridgeName, defaultMTU).Return(bridgeLink, nil),
		mockEngine.EXPECT().CreateVethPair(nsName, defaultMTU, interfaceName).Return(containerVethInterface, hostVethName, nil),
		mockEngine.EXPECT().AttachHostVethInterfaceToBridge(hostVethName, bridgeLink).Return(hostVethInterface, nil),
		mockEngine.EXPECT().RunIPAMPluginAdd(ipamType, conf.StdinData).Return(result, nil),
		mockEngine.EXPECT().ConfigureContainerVethInterface(nsName, result, interfaceName).Do(
			func(netns string, res *current.Result, ifName string) {
				assert.NotEmpty(t, res)
				assert.Equal(t, 3, len(res.Interfaces))
				assert.Equal(t, 2, res.IPs[0].Interface)
				bridge := res.Interfaces[0]
				assert.Equal(t, bridgeName, bridge.Name)
				assert.Equal(t, mac, bridge.Mac)
			}).Return(errors.New("error")),
	)
	err := add(conf, mockEngine)
	assert.Error(t, err)
}

func TestAddConfigureBridgeError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockEngine := mock_engine.NewMockEngine(ctrl)
	bridgeLink := &netlink.Bridge{
		LinkAttrs: netlink.LinkAttrs{
			Name:         bridgeName,
			HardwareAddr: macHWAddr,
		},
	}

	result := &current.Result{
		IPs: []*current.IPConfig{
			&current.IPConfig{},
		},
	}
	containerVethInterface := &current.Interface{}
	hostVethInterface := &current.Interface{}
	gomock.InOrder(
		mockEngine.EXPECT().CreateBridge(bridgeName, defaultMTU).Return(bridgeLink, nil),
		mockEngine.EXPECT().CreateVethPair(nsName, defaultMTU, interfaceName).Return(containerVethInterface, hostVethName, nil),
		mockEngine.EXPECT().AttachHostVethInterfaceToBridge(hostVethName, bridgeLink).Return(hostVethInterface, nil),
		mockEngine.EXPECT().RunIPAMPluginAdd(ipamType, conf.StdinData).Return(result, nil),
		mockEngine.EXPECT().ConfigureContainerVethInterface(nsName, result, interfaceName).Return(nil),
		mockEngine.EXPECT().ConfigureBridge(result, bridgeLink).Return(errors.New("error")),
	)
	err := add(conf, mockEngine)
	assert.Error(t, err)

}

func TestAddSuccess(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockEngine := mock_engine.NewMockEngine(ctrl)
	bridgeLink := &netlink.Bridge{
		LinkAttrs: netlink.LinkAttrs{
			Name:         bridgeName,
			HardwareAddr: macHWAddr,
		},
	}

	result := &current.Result{
		IPs: []*current.IPConfig{
			&current.IPConfig{},
		},
	}
	containerVethInterface := &current.Interface{}
	hostVethInterface := &current.Interface{}
	gomock.InOrder(
		mockEngine.EXPECT().CreateBridge(bridgeName, defaultMTU).Return(bridgeLink, nil),
		mockEngine.EXPECT().CreateVethPair(nsName, defaultMTU, interfaceName).Return(containerVethInterface, hostVethName, nil),
		mockEngine.EXPECT().AttachHostVethInterfaceToBridge(hostVethName, bridgeLink).Return(hostVethInterface, nil),
		mockEngine.EXPECT().RunIPAMPluginAdd(ipamType, conf.StdinData).Return(result, nil),
		mockEngine.EXPECT().ConfigureContainerVethInterface(nsName, result, interfaceName).Do(
			func(netns string, res *current.Result, ifName string) {
				assert.NotEmpty(t, res)
				assert.Equal(t, 3, len(res.Interfaces))
				assert.Equal(t, 2, res.IPs[0].Interface)
				bridge := res.Interfaces[0]
				assert.Equal(t, bridgeName, bridge.Name)
				assert.Equal(t, mac, bridge.Mac)
			}).Return(nil),
		mockEngine.EXPECT().ConfigureBridge(result, bridgeLink).Return(nil),
	)
	err := add(conf, mockEngine)
	assert.NoError(t, err)
}

func TestDelNewConfError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockEngine := mock_engine.NewMockEngine(ctrl)
	err := del(emptyConf, mockEngine)
	assert.Error(t, err)
}

func TestDelRunIPAMPluginDelError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockEngine := mock_engine.NewMockEngine(ctrl)
	mockEngine.EXPECT().RunIPAMPluginDel(ipamType, conf.StdinData).Return(errors.New("error"))
	err := del(conf, mockEngine)
	assert.Error(t, err)
}

func TestDelDeleteVethError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockEngine := mock_engine.NewMockEngine(ctrl)
	gomock.InOrder(
		mockEngine.EXPECT().RunIPAMPluginDel(ipamType, conf.StdinData).Return(nil),
		mockEngine.EXPECT().DeleteVeth(nsName, interfaceName).Return(errors.New("error")),
	)
	err := del(conf, mockEngine)
	assert.Error(t, err)
}

func TestDelSuccess(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockEngine := mock_engine.NewMockEngine(ctrl)
	gomock.InOrder(
		mockEngine.EXPECT().RunIPAMPluginDel(ipamType, conf.StdinData).Return(nil),
		mockEngine.EXPECT().DeleteVeth(nsName, interfaceName).Return(nil),
	)
	err := del(conf, mockEngine)
	assert.NoError(t, err)
}
