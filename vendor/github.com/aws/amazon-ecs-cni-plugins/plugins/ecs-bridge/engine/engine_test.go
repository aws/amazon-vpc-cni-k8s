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

package engine

import (
	"errors"
	"net"
	"syscall"
	"testing"

	"github.com/aws/amazon-ecs-cni-plugins/pkg/cniipamwrapper/mocks"
	"github.com/aws/amazon-ecs-cni-plugins/pkg/cniipamwrapper/mocks_types"
	"github.com/aws/amazon-ecs-cni-plugins/pkg/cniipwrapper/mocks"
	"github.com/aws/amazon-ecs-cni-plugins/pkg/cninswrapper/mocks"
	"github.com/aws/amazon-ecs-cni-plugins/pkg/cninswrapper/mocks_netns"
	"github.com/aws/amazon-ecs-cni-plugins/pkg/netlinkwrapper/mocks"
	"github.com/aws/amazon-ecs-cni-plugins/pkg/netlinkwrapper/mocks_link"
	"github.com/containernetworking/cni/pkg/ip"
	"github.com/containernetworking/cni/pkg/types/current"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/vishvananda/netlink"
)

const (
	bridgeName    = "br0"
	mtu           = 9100
	netns         = "ns1"
	interfaceName = "ecs-eth0"
	mac           = "01:23:45:67:89:ab"
	ipamType      = "ecs-ipam"
	gatewayIPCIDR = "192.168.1.1/31"
	gatewayIP     = "192.168.1.1"
)

var macHWAddr net.HardwareAddr

func init() {
	macHWAddr, _ = net.ParseMAC(mac)
}

func setup(t *testing.T) (*gomock.Controller,
	*mock_cninswrapper.MockNS,
	*mock_netlinkwrapper.MockNetLink,
	*mock_cniipwrapper.MockIP,
	*mock_cniipamwrapper.MockIPAM,
	*mock_cninswrapper.MockNS) {
	ctrl := gomock.NewController(t)
	return ctrl,
		mock_cninswrapper.NewMockNS(ctrl),
		mock_netlinkwrapper.NewMockNetLink(ctrl),
		mock_cniipwrapper.NewMockIP(ctrl),
		mock_cniipamwrapper.NewMockIPAM(ctrl),
		mock_cninswrapper.NewMockNS(ctrl)
}

func TestLookupBridgeLinkByNameError(t *testing.T) {
	ctrl, _, mockNetLink, _, _, _ := setup(t)
	defer ctrl.Finish()

	mockNetLink.EXPECT().LinkByName(bridgeName).Return(nil, errors.New("error"))
	engine := &engine{netLink: mockNetLink}
	_, err := engine.lookupBridge(bridgeName)
	assert.Error(t, err)
}

func TestLookupBridgeNotABridgeError(t *testing.T) {
	ctrl, _, mockNetLink, _, _, _ := setup(t)
	defer ctrl.Finish()

	mockNetLink.EXPECT().LinkByName(bridgeName).Return(&netlink.Dummy{}, nil)
	engine := &engine{netLink: mockNetLink}
	_, err := engine.lookupBridge(bridgeName)
	assert.Error(t, err)
}

func TestLookupBridgeLinkNotFound(t *testing.T) {
	ctrl, _, mockNetLink, _, _, _ := setup(t)
	defer ctrl.Finish()

	mockNetLink.EXPECT().LinkByName(bridgeName).Return(nil, netlink.LinkNotFoundError{})
	engine := &engine{netLink: mockNetLink}
	bridge, err := engine.lookupBridge(bridgeName)
	assert.NoError(t, err)
	assert.Nil(t, bridge)
}

func TestLookupBridgeLinkFound(t *testing.T) {
	ctrl, _, mockNetLink, _, _, _ := setup(t)
	defer ctrl.Finish()

	mockNetLink.EXPECT().LinkByName(bridgeName).Return(&netlink.Bridge{}, nil)
	engine := &engine{netLink: mockNetLink}
	bridge, err := engine.lookupBridge(bridgeName)
	assert.NoError(t, err)
	assert.NotNil(t, bridge)
}

func TestCreateBridgeInternalLinkAddError(t *testing.T) {
	ctrl, _, mockNetLink, _, _, _ := setup(t)
	defer ctrl.Finish()

	mockNetLink.EXPECT().LinkAdd(gomock.Any()).Do(func(link netlink.Link) {
		assert.Equal(t, bridgeName, link.Attrs().Name)
		assert.Equal(t, mtu, link.Attrs().MTU)
		assert.Equal(t, -1, link.Attrs().TxQLen)
	}).Return(errors.New("error"))
	engine := &engine{netLink: mockNetLink}
	err := engine.createBridge(bridgeName, mtu)
	assert.Error(t, err)
}

func TestCreateBridgeLookupBridgeError(t *testing.T) {
	ctrl, _, mockNetLink, _, _, _ := setup(t)
	defer ctrl.Finish()

	mockNetLink.EXPECT().LinkByName(bridgeName).Return(nil, errors.New("error"))
	engine := &engine{netLink: mockNetLink}
	_, err := engine.CreateBridge(bridgeName, mtu)
	assert.Error(t, err)
}

func TestCreateBridgeLinkAddError(t *testing.T) {
	ctrl, _, mockNetLink, _, _, _ := setup(t)
	defer ctrl.Finish()

	gomock.InOrder(
		mockNetLink.EXPECT().LinkByName(bridgeName).Return(nil, netlink.LinkNotFoundError{}),
		mockNetLink.EXPECT().LinkAdd(gomock.Any()).Return(errors.New("error")),
	)
	engine := &engine{netLink: mockNetLink}
	_, err := engine.CreateBridge(bridgeName, mtu)
	assert.Error(t, err)
}

func TestCreateBridgeLinkSetupError(t *testing.T) {
	ctrl, _, mockNetLink, _, _, _ := setup(t)
	defer ctrl.Finish()

	bridgeLink := &netlink.Bridge{}
	gomock.InOrder(
		mockNetLink.EXPECT().LinkByName(bridgeName).Return(nil, netlink.LinkNotFoundError{}),
		mockNetLink.EXPECT().LinkAdd(gomock.Any()).Return(nil),
		mockNetLink.EXPECT().LinkByName(bridgeName).Return(bridgeLink, nil),
		mockNetLink.EXPECT().LinkSetUp(bridgeLink).Return(errors.New("error")),
	)
	engine := &engine{netLink: mockNetLink}
	_, err := engine.CreateBridge(bridgeName, mtu)
	assert.Error(t, err)
}

func TestCreateBridgeSuccess(t *testing.T) {
	ctrl, _, mockNetLink, _, _, _ := setup(t)
	defer ctrl.Finish()

	bridgeLink := &netlink.Bridge{}
	gomock.InOrder(
		mockNetLink.EXPECT().LinkByName(bridgeName).Return(nil, netlink.LinkNotFoundError{}),
		mockNetLink.EXPECT().LinkAdd(gomock.Any()).Return(nil),
		mockNetLink.EXPECT().LinkByName(bridgeName).Return(bridgeLink, nil),
		mockNetLink.EXPECT().LinkSetUp(bridgeLink).Return(nil),
	)
	engine := &engine{netLink: mockNetLink}
	createdBridge, err := engine.CreateBridge(bridgeName, mtu)
	assert.NoError(t, err)
	assert.Equal(t, bridgeLink, createdBridge)
}

func TestCreateVethPairSetupVethError(t *testing.T) {
	ctrl, _, _, mockIP, _, _ := setup(t)
	defer ctrl.Finish()

	mockNetNS := mock_ns.NewMockNetNS(ctrl)
	mockIP.EXPECT().SetupVeth(interfaceName, mtu, mockNetNS).Return(
		net.Interface{}, net.Interface{}, errors.New("error"))
	createVethContext := newCreateVethPairContext(
		interfaceName, mtu, mockIP)
	err := createVethContext.run(mockNetNS)
	assert.Error(t, err)

}

func TestCreateVethPairSuccess(t *testing.T) {
	ctrl, _, _, mockIP, _, _ := setup(t)
	defer ctrl.Finish()

	hostVeth := net.Interface{
		Name: "host-veth0",
	}
	containerVeth := net.Interface{
		Name:         "ctr-veth0",
		HardwareAddr: macHWAddr,
	}
	mockNetNS := mock_ns.NewMockNetNS(ctrl)
	mockIP.EXPECT().SetupVeth(interfaceName, mtu, mockNetNS).Return(
		hostVeth, containerVeth, nil)
	createVethContext := newCreateVethPairContext(
		interfaceName, mtu, mockIP)
	err := createVethContext.run(mockNetNS)
	assert.NoError(t, err)
	assert.Equal(t, hostVeth.Name, createVethContext.hostVethName)
	assert.Equal(t, containerVeth.Name, createVethContext.containerInterfaceResult.Name)
	assert.Equal(t, mac, createVethContext.containerInterfaceResult.Mac)
}

func TestCreateVethPairWithNetNSPathError(t *testing.T) {
	ctrl, _, _, _, _, mockNS := setup(t)
	defer ctrl.Finish()

	mockNS.EXPECT().WithNetNSPath(netns, gomock.Any()).Return(errors.New("error"))
	engine := &engine{ns: mockNS}
	_, _, err := engine.CreateVethPair(netns, mtu, interfaceName)
	assert.Error(t, err)
}

func TestAttachHostVethInterfaceToBridgeLinkByNameError(t *testing.T) {
	ctrl, _, mockNetLink, _, _, _ := setup(t)
	defer ctrl.Finish()

	hostVethName := "host-veth0"
	mockNetLink.EXPECT().LinkByName(hostVethName).Return(nil, errors.New("error"))
	engine := &engine{netLink: mockNetLink}
	_, err := engine.AttachHostVethInterfaceToBridge(hostVethName, nil)
	assert.Error(t, err)
}

func TestAttachHostVethInterfaceToBridgeLinkSetMasterError(t *testing.T) {
	ctrl, _, mockNetLink, _, _, _ := setup(t)
	defer ctrl.Finish()

	hostVethName := "host-veth0"
	hostVethInterface := &netlink.Dummy{}
	bridgeLink := &netlink.Bridge{}
	gomock.InOrder(
		mockNetLink.EXPECT().LinkByName(hostVethName).Return(hostVethInterface, nil),
		mockNetLink.EXPECT().LinkSetMaster(hostVethInterface, bridgeLink).Return(errors.New("error")),
	)
	engine := &engine{netLink: mockNetLink}
	_, err := engine.AttachHostVethInterfaceToBridge(hostVethName, bridgeLink)
	assert.Error(t, err)
}

func TestAttachHostVethInterfaceToBridgeSuccess(t *testing.T) {
	ctrl, _, mockNetLink, _, _, _ := setup(t)
	defer ctrl.Finish()

	hostVethName := "host-veth0"
	hostVethInterface := mock_netlink.NewMockLink(ctrl)
	bridgeLink := &netlink.Bridge{}
	gomock.InOrder(
		mockNetLink.EXPECT().LinkByName(hostVethName).Return(hostVethInterface, nil),
		mockNetLink.EXPECT().LinkSetMaster(hostVethInterface, bridgeLink).Return(nil),
		hostVethInterface.EXPECT().Attrs().Return(&netlink.LinkAttrs{HardwareAddr: macHWAddr}),
	)
	engine := &engine{netLink: mockNetLink}
	hostVethResult, err := engine.AttachHostVethInterfaceToBridge(hostVethName, bridgeLink)
	assert.NoError(t, err)
	assert.Equal(t, hostVethName, hostVethResult.Name)
	assert.Equal(t, mac, hostVethResult.Mac)
}

func TestRunIPAMPluginAddExecAddError(t *testing.T) {
	ctrl, _, _, _, mockIPAM, _ := setup(t)
	defer ctrl.Finish()

	netConf := []byte{}
	mockIPAM.EXPECT().ExecAdd(ipamType, netConf).Return(nil, errors.New("error"))
	engine := &engine{ipam: mockIPAM}
	_, err := engine.RunIPAMPluginAdd(ipamType, netConf)
	assert.Error(t, err)
}

func TestRunIPAMPluginAddResultConversionError(t *testing.T) {
	ctrl, _, _, _, mockIPAM, _ := setup(t)
	defer ctrl.Finish()

	netConf := []byte{}
	result := mock_types.NewMockResult(ctrl)
	gomock.InOrder(
		mockIPAM.EXPECT().ExecAdd(ipamType, netConf).Return(result, nil),
		// Return an unsupported CNI version, which should cause the
		// current.NewResultFromResult to return an error, thus
		// simulating a "parse error"
		result.EXPECT().Version().Return("a.b.c").MinTimes(1),
		result.EXPECT().String().Return(""),
	)
	engine := &engine{ipam: mockIPAM}
	_, err := engine.RunIPAMPluginAdd(ipamType, netConf)
	assert.Error(t, err)
}

func TestRunIPAMPluginAddResultParseErrorInvalidNumIPs(t *testing.T) {
	ctrl, _, _, _, mockIPAM, _ := setup(t)
	defer ctrl.Finish()

	netConf := []byte{}
	result := &current.Result{}
	mockIPAM.EXPECT().ExecAdd(ipamType, netConf).Return(result, nil)
	engine := &engine{ipam: mockIPAM}
	_, err := engine.RunIPAMPluginAdd(ipamType, netConf)
	assert.Error(t, err)
}

func TestRunIPAMPluginAddResultParseErrorNoGateway(t *testing.T) {
	ctrl, _, _, _, mockIPAM, _ := setup(t)
	defer ctrl.Finish()

	netConf := []byte{}
	result := &current.Result{
		IPs: []*current.IPConfig{
			{},
		},
	}
	mockIPAM.EXPECT().ExecAdd(ipamType, netConf).Return(result, nil)
	engine := &engine{ipam: mockIPAM}
	_, err := engine.RunIPAMPluginAdd(ipamType, netConf)
	assert.Error(t, err)
}

func TestRunIPAMPluginAddMaskNotSet(t *testing.T) {
	ctrl, _, _, _, mockIPAM, _ := setup(t)
	defer ctrl.Finish()

	netConf := []byte{}
	result := &current.Result{
		IPs: []*current.IPConfig{
			{
				Gateway: net.ParseIP(gatewayIPCIDR),
			},
		},
	}
	mockIPAM.EXPECT().ExecAdd(ipamType, netConf).Return(result, nil)
	engine := &engine{ipam: mockIPAM}
	result, err := engine.RunIPAMPluginAdd(ipamType, netConf)
	assert.Error(t, err)
}

func TestRunIPAMPluginAddSuccess(t *testing.T) {
	ctrl, _, _, _, mockIPAM, _ := setup(t)
	defer ctrl.Finish()

	netConf := []byte{}
	result := &current.Result{
		IPs: []*current.IPConfig{
			{
				Address: net.IPNet{
					Mask: net.CIDRMask(31, 32),
				},
				Gateway: net.ParseIP("192.168.1.1"),
			},
		},
	}
	mockIPAM.EXPECT().ExecAdd(ipamType, netConf).Return(result, nil)
	engine := &engine{ipam: mockIPAM}
	result, err := engine.RunIPAMPluginAdd(ipamType, netConf)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(result.IPs))
	assert.Equal(t, gatewayIP, result.IPs[0].Gateway.String())
}

func TestConfigureContainerVethInterfaceConfigureIfaceError(t *testing.T) {
	ctrl, _, mockNetLink, mockIP, mockIPAM, _ := setup(t)
	defer ctrl.Finish()

	result := &current.Result{}
	mockIPAM.EXPECT().ConfigureIface(interfaceName, result).Return(errors.New("error"))
	configContext := newConfigureVethContext(
		interfaceName, result, mockIP, mockIPAM, mockNetLink)
	err := configContext.run(nil)
	assert.Error(t, err)
}

func TestConfigureContainerVethInterfaceSetHWAddrByIPError(t *testing.T) {
	ctrl, _, mockNetLink, mockIP, mockIPAM, _ := setup(t)
	defer ctrl.Finish()

	gatewayIPAddr := net.ParseIP(gatewayIPCIDR)
	ipConfig := &current.IPConfig{
		Address: net.IPNet{
			IP: gatewayIPAddr,
		},
	}

	result := &current.Result{
		IPs: []*current.IPConfig{ipConfig},
	}

	gomock.InOrder(
		mockIPAM.EXPECT().ConfigureIface(interfaceName, result).Return(nil),
		mockIP.EXPECT().SetHWAddrByIP(interfaceName, gatewayIPAddr, nil).Return(errors.New("error")),
	)
	configContext := newConfigureVethContext(
		interfaceName, result, mockIP, mockIPAM, mockNetLink)
	err := configContext.run(nil)
	assert.Error(t, err)
}

func TestConfigureContainerVethInterfaceLinkByNameError(t *testing.T) {
	ctrl, _, mockNetLink, mockIP, mockIPAM, _ := setup(t)
	defer ctrl.Finish()

	gatewayIPAddr := net.ParseIP(gatewayIPCIDR)
	ipConfig := &current.IPConfig{
		Address: net.IPNet{
			IP: gatewayIPAddr,
		},
	}

	result := &current.Result{
		IPs: []*current.IPConfig{ipConfig},
	}

	gomock.InOrder(
		mockIPAM.EXPECT().ConfigureIface(interfaceName, result).Return(nil),
		mockIP.EXPECT().SetHWAddrByIP(interfaceName, gatewayIPAddr, nil).Return(nil),
		mockNetLink.EXPECT().LinkByName(interfaceName).Return(nil, errors.New("error")),
	)
	configContext := newConfigureVethContext(
		interfaceName, result, mockIP, mockIPAM, mockNetLink)
	err := configContext.run(nil)
	assert.Error(t, err)
}

func TestConfigureContainerVethInterfaceRouteListError(t *testing.T) {
	ctrl, _, mockNetLink, mockIP, mockIPAM, _ := setup(t)
	defer ctrl.Finish()

	gatewayIPAddr := net.ParseIP(gatewayIPCIDR)
	ipConfig := &current.IPConfig{
		Address: net.IPNet{
			IP: gatewayIPAddr,
		},
	}

	result := &current.Result{
		IPs: []*current.IPConfig{ipConfig},
	}

	mockLink := mock_netlink.NewMockLink(ctrl)

	gomock.InOrder(
		mockIPAM.EXPECT().ConfigureIface(interfaceName, result).Return(nil),
		mockIP.EXPECT().SetHWAddrByIP(interfaceName, gatewayIPAddr, nil).Return(nil),
		mockNetLink.EXPECT().LinkByName(interfaceName).Return(mockLink, nil),
		mockNetLink.EXPECT().RouteList(mockLink, netlink.FAMILY_ALL).Return(nil, errors.New("error")),
	)
	configContext := newConfigureVethContext(
		interfaceName, result, mockIP, mockIPAM, mockNetLink)
	err := configContext.run(nil)
	assert.Error(t, err)
}

func TestConfigureContainerVethInterfaceRouteDelError(t *testing.T) {
	ctrl, _, mockNetLink, mockIP, mockIPAM, _ := setup(t)
	defer ctrl.Finish()

	gatewayIPAddr := net.ParseIP(gatewayIPCIDR)
	ipConfig := &current.IPConfig{
		Address: net.IPNet{
			IP: gatewayIPAddr,
		},
	}

	result := &current.Result{
		IPs: []*current.IPConfig{ipConfig},
	}

	mockLink := mock_netlink.NewMockLink(ctrl)
	route := netlink.Route{}
	routes := []netlink.Route{route}
	gomock.InOrder(
		mockIPAM.EXPECT().ConfigureIface(interfaceName, result).Return(nil),
		mockIP.EXPECT().SetHWAddrByIP(interfaceName, gatewayIPAddr, nil).Return(nil),
		mockNetLink.EXPECT().LinkByName(interfaceName).Return(mockLink, nil),
		mockNetLink.EXPECT().RouteList(mockLink, netlink.FAMILY_ALL).Return(routes, nil),
		mockNetLink.EXPECT().RouteDel(&route).Return(errors.New("error")),
	)
	configContext := newConfigureVethContext(
		interfaceName, result, mockIP, mockIPAM, mockNetLink)
	err := configContext.run(nil)
	assert.Error(t, err)
}

func TestConfigureContainerVethInterfaceContextSuccess(t *testing.T) {
	ctrl, _, mockNetLink, mockIP, mockIPAM, _ := setup(t)
	defer ctrl.Finish()

	gatewayIPAddr := net.ParseIP(gatewayIPCIDR)
	ipConfig := &current.IPConfig{
		Address: net.IPNet{
			IP: gatewayIPAddr,
		},
	}

	result := &current.Result{
		IPs: []*current.IPConfig{ipConfig},
	}

	mockLink := mock_netlink.NewMockLink(ctrl)
	route := netlink.Route{}
	routes := []netlink.Route{route}
	gomock.InOrder(
		mockIPAM.EXPECT().ConfigureIface(interfaceName, result).Return(nil),
		mockIP.EXPECT().SetHWAddrByIP(interfaceName, gatewayIPAddr, nil).Return(nil),
		mockNetLink.EXPECT().LinkByName(interfaceName).Return(mockLink, nil),
		mockNetLink.EXPECT().RouteList(mockLink, netlink.FAMILY_ALL).Return(routes, nil),
		mockNetLink.EXPECT().RouteDel(&route).Return(nil),
	)
	configContext := newConfigureVethContext(
		interfaceName, result, mockIP, mockIPAM, mockNetLink)
	err := configContext.run(nil)
	assert.NoError(t, err)
}

func TestConfigureContainerVethInterfaceWithNetNSPathError(t *testing.T) {
	ctrl, mockNS, mockNetLink, mockIP, mockIPAM, _ := setup(t)
	defer ctrl.Finish()

	mockNS.EXPECT().WithNetNSPath(netns, gomock.Any()).Return(errors.New("error"))
	engine := &engine{
		ns:      mockNS,
		ip:      mockIP,
		ipam:    mockIPAM,
		netLink: mockNetLink,
	}

	err := engine.ConfigureContainerVethInterface(netns, nil, interfaceName)
	assert.Error(t, err)
}

func TestConfigureContainerVethInterfaceSuccess(t *testing.T) {
	ctrl, mockNS, mockNetLink, mockIP, mockIPAM, _ := setup(t)
	defer ctrl.Finish()

	mockNS.EXPECT().WithNetNSPath(netns, gomock.Any()).Return(nil)
	engine := &engine{
		ns:      mockNS,
		ip:      mockIP,
		ipam:    mockIPAM,
		netLink: mockNetLink,
	}

	err := engine.ConfigureContainerVethInterface(netns, nil, interfaceName)
	assert.NoError(t, err)
}

func TestConfigureBridgeAddrListError(t *testing.T) {
	ctrl, _, mockNetLink, _, _, _ := setup(t)
	defer ctrl.Finish()

	bridgeLink := &netlink.Bridge{}
	mockNetLink.EXPECT().AddrList(bridgeLink, syscall.AF_INET).Return(nil, errors.New("error"))

	engine := &engine{netLink: mockNetLink}
	err := engine.ConfigureBridge(nil, bridgeLink)
	assert.Error(t, err)
}

func TestConfigureBridgeAddrListWhenFound(t *testing.T) {
	ctrl, _, mockNetLink, _, _, _ := setup(t)
	defer ctrl.Finish()

	bridgeLink := &netlink.Bridge{}
	gatewayIPAddr := net.ParseIP(gatewayIPCIDR)
	ipConfig := &current.IPConfig{
		Address: net.IPNet{
			Mask: net.CIDRMask(31, 32),
		},
		Gateway: gatewayIPAddr,
	}

	result := &current.Result{
		IPs: []*current.IPConfig{ipConfig},
	}

	addrs := []netlink.Addr{
		{
			IPNet: &net.IPNet{
				IP: gatewayIPAddr,
			},
		},
	}
	gomock.InOrder(
		mockNetLink.EXPECT().AddrList(bridgeLink, syscall.AF_INET).Return(addrs, nil),
	)
	engine := &engine{netLink: mockNetLink}
	err := engine.ConfigureBridge(result, bridgeLink)
	assert.NoError(t, err)
}

func TestConfigureBridgeAddrListWhenNotFound(t *testing.T) {
	ctrl, _, mockNetLink, _, _, _ := setup(t)
	defer ctrl.Finish()

	bridgeLink := &netlink.Bridge{}
	gatewayIPAddr := net.ParseIP(gatewayIP)
	ipConfig := &current.IPConfig{
		Address: net.IPNet{
			Mask: net.CIDRMask(31, 32),
		},
		Gateway: gatewayIPAddr,
	}

	result := &current.Result{
		IPs: []*current.IPConfig{ipConfig},
	}

	addrs := []netlink.Addr{
		{
			IPNet: &net.IPNet{},
		},
	}
	gomock.InOrder(
		mockNetLink.EXPECT().AddrList(bridgeLink, syscall.AF_INET).Return(addrs, nil),
	)
	engine := &engine{netLink: mockNetLink}
	err := engine.ConfigureBridge(result, bridgeLink)
	assert.Error(t, err)
}

func TestConfigureBridgeAddrAddError(t *testing.T) {
	ctrl, _, mockNetLink, _, _, _ := setup(t)
	defer ctrl.Finish()

	bridgeLink := &netlink.Bridge{}
	gatewayIPAddr := net.ParseIP(gatewayIPCIDR)
	ipConfig := &current.IPConfig{
		Address: net.IPNet{
			Mask: net.CIDRMask(31, 32),
		},
		Gateway: gatewayIPAddr,
	}

	result := &current.Result{
		IPs: []*current.IPConfig{ipConfig},
	}

	bridgeAddr := &netlink.Addr{
		IPNet: &net.IPNet{
			IP:   gatewayIPAddr,
			Mask: net.CIDRMask(31, 32),
		},
	}
	gomock.InOrder(
		mockNetLink.EXPECT().AddrList(bridgeLink, syscall.AF_INET).Return(nil, nil),
		mockNetLink.EXPECT().AddrAdd(bridgeLink, bridgeAddr).Return(errors.New("error")),
	)
	engine := &engine{netLink: mockNetLink}
	err := engine.ConfigureBridge(result, bridgeLink)
	assert.Error(t, err)
}

func TestConfigureBridgeAddrAddSuccess(t *testing.T) {
	ctrl, _, mockNetLink, _, _, _ := setup(t)
	defer ctrl.Finish()

	bridgeLink := &netlink.Bridge{}
	gatewayIPAddr := net.ParseIP(gatewayIPCIDR)
	ipConfig := &current.IPConfig{
		Address: net.IPNet{
			Mask: net.CIDRMask(31, 32),
		},
		Gateway: gatewayIPAddr,
	}

	result := &current.Result{
		IPs: []*current.IPConfig{ipConfig},
	}

	bridgeAddr := &netlink.Addr{
		IPNet: &net.IPNet{
			IP:   gatewayIPAddr,
			Mask: net.CIDRMask(31, 32),
		},
	}
	gomock.InOrder(
		mockNetLink.EXPECT().AddrList(bridgeLink, syscall.AF_INET).Return(nil, nil),
		mockNetLink.EXPECT().AddrAdd(bridgeLink, bridgeAddr).Return(nil),
	)
	engine := &engine{netLink: mockNetLink}
	err := engine.ConfigureBridge(result, bridgeLink)
	assert.NoError(t, err)
}

func TestGetInterfaceIPV4AddressContextLinkByNameError(t *testing.T) {
	ctrl, _, mockNetLink, _, _, _ := setup(t)
	defer ctrl.Finish()

	mockNetLink.EXPECT().LinkByName(interfaceName).Return(nil, errors.New("error"))
	ipv4Context := newGetContainerIPV4Context(interfaceName, mockNetLink)
	err := ipv4Context.run(nil)
	assert.Error(t, err)
}

func TestGetInterfaceIPV4AddressContextAddrListError(t *testing.T) {
	ctrl, _, mockNetLink, _, _, _ := setup(t)
	defer ctrl.Finish()

	mockLink := mock_netlink.NewMockLink(ctrl)
	gomock.InOrder(
		mockNetLink.EXPECT().LinkByName(interfaceName).Return(mockLink, nil),
		mockNetLink.EXPECT().AddrList(mockLink, netlink.FAMILY_V4).Return(nil, errors.New("error")),
	)
	ipv4Context := newGetContainerIPV4Context(interfaceName, mockNetLink)
	err := ipv4Context.run(nil)
	assert.Error(t, err)
}

func TestGetInterfaceIPV4AddressContextAddrListEmpty(t *testing.T) {
	ctrl, _, mockNetLink, _, _, _ := setup(t)
	defer ctrl.Finish()

	mockLink := mock_netlink.NewMockLink(ctrl)
	gomock.InOrder(
		mockNetLink.EXPECT().LinkByName(interfaceName).Return(mockLink, nil),
		mockNetLink.EXPECT().AddrList(mockLink, netlink.FAMILY_V4).Return(nil, nil),
	)
	ipv4Context := newGetContainerIPV4Context(interfaceName, mockNetLink)
	err := ipv4Context.run(nil)
	assert.Error(t, err)
}

func TestGetInterfaceIPV4AddressWithNetNSPathError(t *testing.T) {
	ctrl, mockNS, _, _, _, _ := setup(t)
	defer ctrl.Finish()

	mockNS.EXPECT().WithNetNSPath(netns, gomock.Any()).Return(errors.New("error"))
	engine := &engine{
		ns: mockNS,
	}

	_, err := engine.GetInterfaceIPV4Address(netns, interfaceName)
	assert.Error(t, err)
}

func TestGetInterfaceIPV4AddressWithNetNSPathSuccess(t *testing.T) {
	ctrl, mockNS, _, _, _, _ := setup(t)
	defer ctrl.Finish()

	mockNS.EXPECT().WithNetNSPath(netns, gomock.Any()).Return(nil)
	engine := &engine{
		ns: mockNS,
	}

	_, err := engine.GetInterfaceIPV4Address(netns, interfaceName)
	assert.NoError(t, err)
}

func TestRunIPAMPluginDelExecDelError(t *testing.T) {
	ctrl, _, _, _, mockIPAM, _ := setup(t)
	defer ctrl.Finish()

	netConf := []byte{}
	mockIPAM.EXPECT().ExecDel(ipamType, netConf).Return(errors.New("error"))
	engine := &engine{ipam: mockIPAM}
	err := engine.RunIPAMPluginDel(ipamType, netConf)
	assert.Error(t, err)
}

func TestRunIPAMPluginDelExecDelSuccess(t *testing.T) {
	ctrl, _, _, _, mockIPAM, _ := setup(t)
	defer ctrl.Finish()

	netConf := []byte{}
	mockIPAM.EXPECT().ExecDel(ipamType, netConf).Return(nil)
	engine := &engine{ipam: mockIPAM}
	err := engine.RunIPAMPluginDel(ipamType, netConf)
	assert.NoError(t, err)
}

func TestDeleteVethContextDelLinkByNameAddrError(t *testing.T) {
	ctrl, _, _, mockIP, _, _ := setup(t)
	defer ctrl.Finish()

	mockIP.EXPECT().DelLinkByNameAddr(interfaceName, netlink.FAMILY_V4).Return(nil, errors.New("error"))
	delContext := newDeleteLinkContext(interfaceName, mockIP)
	err := delContext.run(nil)
	assert.Error(t, err)
}

func TestDeleteVethContextDelLinkByNameAddrErrorNotFound(t *testing.T) {
	ctrl, _, _, mockIP, _, _ := setup(t)
	defer ctrl.Finish()

	mockIP.EXPECT().DelLinkByNameAddr(interfaceName, netlink.FAMILY_V4).Return(nil, ip.ErrLinkNotFound)
	delContext := newDeleteLinkContext(interfaceName, mockIP)
	err := delContext.run(nil)
	assert.NoError(t, err)
}

func TestDeleteVethContextDelLinkByNameAddrSuccess(t *testing.T) {
	ctrl, _, _, mockIP, _, _ := setup(t)
	defer ctrl.Finish()

	mockIP.EXPECT().DelLinkByNameAddr(interfaceName, netlink.FAMILY_V4).Return(nil, nil)
	delContext := newDeleteLinkContext(interfaceName, mockIP)
	err := delContext.run(nil)
	assert.NoError(t, err)
}

func TestDeleteVethWithNetNSPathError(t *testing.T) {
	ctrl, mockNS, _, _, _, _ := setup(t)
	defer ctrl.Finish()

	mockNS.EXPECT().WithNetNSPath(netns, gomock.Any()).Return(errors.New("error"))
	engine := &engine{
		ns: mockNS,
	}

	err := engine.DeleteVeth(netns, interfaceName)
	assert.Error(t, err)
}

func TestDeleteVethWithNetNSPathSuccess(t *testing.T) {
	ctrl, mockNS, _, _, _, _ := setup(t)
	defer ctrl.Finish()

	mockNS.EXPECT().WithNetNSPath(netns, gomock.Any()).Return(nil)
	engine := &engine{
		ns: mockNS,
	}

	err := engine.DeleteVeth(netns, interfaceName)
	assert.NoError(t, err)
}
