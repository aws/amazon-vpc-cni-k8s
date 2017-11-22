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
	"net"
	"syscall"

	"github.com/aws/amazon-ecs-cni-plugins/pkg/cniipamwrapper"
	"github.com/aws/amazon-ecs-cni-plugins/pkg/cniipwrapper"
	"github.com/aws/amazon-ecs-cni-plugins/pkg/cninswrapper"
	"github.com/aws/amazon-ecs-cni-plugins/pkg/netlinkwrapper"
	"github.com/containernetworking/cni/pkg/types/current"
	"github.com/pkg/errors"
	"github.com/vishvananda/netlink"
)

// zeroLengthIPString is what we expect net.IP.String() to return if the
// ip has length 0. We use this to determing if an IP is empty.
// Refer https://golang.org/pkg/net/#IP.String
const zeroLengthIPString = "<nil>"

// Engine represents the execution engine for the ECS Bridge plugin.
// It defines all the operations performed during the execution of the
// plugin
type Engine interface {
	CreateBridge(bridgeName string, mtu int) (*netlink.Bridge, error)
	CreateVethPair(netnsName string, mtu int, interfaceName string) (*current.Interface, string, error)
	AttachHostVethInterfaceToBridge(hostVethName string, bridge *netlink.Bridge) (*current.Interface, error)
	RunIPAMPluginAdd(plugin string, netConf []byte) (*current.Result, error)
	ConfigureContainerVethInterface(netnsName string, result *current.Result, interfaceName string) error
	ConfigureBridge(result *current.Result, bridge *netlink.Bridge) error
	GetInterfaceIPV4Address(netnsName string, interfaceName string) (string, error)
	RunIPAMPluginDel(plugin string, netconf []byte) error
	DeleteVeth(netnsName string, interfaceName string) error
}

type engine struct {
	netLink netlinkwrapper.NetLink
	ns      cninswrapper.NS
	ip      cniipwrapper.IP
	ipam    cniipamwrapper.IPAM
}

// New creates a new Engine object
func New() Engine {
	return &engine{
		netLink: netlinkwrapper.NewNetLink(),
		ns:      cninswrapper.NewNS(),
		ip:      cniipwrapper.New(),
		ipam:    cniipamwrapper.New(),
	}
}

// CreateBridge creates the bridge if needed
func (engine *engine) CreateBridge(bridgeName string, mtu int) (*netlink.Bridge, error) {
	bridge, err := engine.lookupBridge(bridgeName)
	if err != nil {
		return nil, err
	}

	if bridge == nil {
		err = engine.createBridge(bridgeName, mtu)
		if err != nil {
			return nil, err
		}

		// We need to lookup the bridge link again because LinkAdd
		// doesn't return a handle to the link with all the other
		// attributes set
		bridge, err = engine.lookupBridge(bridgeName)
		if err != nil {
			return nil, err
		}
	}

	if err := engine.netLink.LinkSetUp(bridge); err != nil {
		return nil, errors.Wrapf(err,
			"bridge create: unable to bring up the bridge interface %s", bridgeName)
	}

	return bridge, nil
}

// lookupBridge tries to get the link interface for the bridge by its name. If
// it cannot find the bridge it returns nil. If the link device is not of type
// bridge, or if there's an error with LinkByName, it returns an error
func (engine *engine) lookupBridge(bridgeName string) (*netlink.Bridge, error) {
	bridgeLink, err := engine.netLink.LinkByName(bridgeName)
	if err != nil {
		if _, ok := err.(netlink.LinkNotFoundError); !ok {
			return nil, errors.Wrapf(err,
				"bridge create: error lookup the bridge interface %s", bridgeName)
		}

		return nil, nil
	}

	bridge, ok := bridgeLink.(*netlink.Bridge)
	if !ok {
		return nil, errors.Errorf(
			"bridge create: interface named %s already exists, but is not a bridge",
			bridgeName)
	}

	return bridge, nil
}

// createBridge creates a bridge interface
func (engine *engine) createBridge(bridgeName string, mtu int) error {
	bridgeLinkAttributes := netlink.NewLinkAttrs()
	bridgeLinkAttributes.MTU = mtu
	bridgeLinkAttributes.Name = bridgeName

	bridge := &netlink.Bridge{
		LinkAttrs: bridgeLinkAttributes,
	}

	err := engine.netLink.LinkAdd(bridge)
	if err != nil {
		return errors.Wrapf(err,
			"bridge create: unable to add bridge interface %s", bridgeName)
	}

	return nil
}

// CreateVethPair creates the veth pair to attach the container to the bridge
func (engine *engine) CreateVethPair(netnsName string, mtu int, interfaceName string) (*current.Interface, string, error) {
	createVethContext := newCreateVethPairContext(
		interfaceName, mtu, engine.ip)

	err := engine.ns.WithNetNSPath(netnsName, createVethContext.run)

	if err != nil {
		return nil, "", err
	}

	return createVethContext.containerInterfaceResult, createVethContext.hostVethName, nil
}

// AttachHostVethInterfaceToBridge moves the host end of the veth pair to the bridge
func (engine *engine) AttachHostVethInterfaceToBridge(hostVethName string, bridge *netlink.Bridge) (*current.Interface, error) {
	hostVethInterface, err := engine.netLink.LinkByName(hostVethName)
	if err != nil {
		return nil, errors.Wrapf(err,
			"bridge create veth pair: unable to look up host veth interface %s", hostVethName)
	}

	err = engine.netLink.LinkSetMaster(hostVethInterface, bridge)
	if err != nil {
		return nil, errors.Wrapf(err,
			"bridge create veth pair: unable to attach the veth interface %s to bridge", hostVethName)
	}

	return &current.Interface{
		Name: hostVethName,
		Mac:  hostVethInterface.Attrs().HardwareAddr.String(),
	}, nil
}

// RunIPAMPluginAdd invokes the IPAM plugin with the ADD command
func (engine *engine) RunIPAMPluginAdd(plugin string, netConf []byte) (*current.Result, error) {
	ipamResult, err := engine.ipam.ExecAdd(plugin, netConf)
	if err != nil {
		return nil, errors.Wrapf(err,
			"bridge ipam ADD: failed to execute plugin: %s", plugin)
	}

	result, err := current.NewResultFromResult(ipamResult)
	if err != nil {
		return nil, errors.Wrapf(err,
			"bridge IPAM ADD: unable to parse result '%s'", ipamResult.String())
	}

	// This version of the bridge plugin only considers the first IP Address
	// returned by the ipam plugin as we only expect one ip address to be
	// allocated by the IPAM interface
	if len(result.IPs) != 1 {
		return nil, errors.New("bridge IPAM ADD: Missing IP config in result")
	}

	if result.IPs[0].Address.Mask == nil || result.IPs[0].Address.Mask.String() == zeroLengthIPString {
		return nil, errors.New("bridge IPAM ADD: IP address mask not set in result")
	}

	if result.IPs[0].Gateway == nil || result.IPs[0].Gateway.String() == zeroLengthIPString {
		return nil, errors.New("bridge IPAM ADD: Gateway not set in result")
	}

	return result, nil
}

// ConfigureContainerVethInterface configures the container's veth interface,
// including setting up routes within the container
func (engine *engine) ConfigureContainerVethInterface(netnsName string, result *current.Result, interfaceName string) error {
	configureContext := newConfigureVethContext(
		interfaceName,
		result,
		engine.ip,
		engine.ipam,
		engine.netLink)

	return engine.ns.WithNetNSPath(netnsName, configureContext.run)
}

// ConfigureBridge configures the IP address of the bridge if needed
func (engine *engine) ConfigureBridge(result *current.Result, bridge *netlink.Bridge) error {
	addrs, err := engine.netLink.AddrList(bridge, syscall.AF_INET)
	if err != nil && err != syscall.ENOENT {
		return errors.Wrapf(err,
			"bridge configure: unable to list addresses for bridge %s",
			bridge.Attrs().Name)
	}

	resultBridgeNetwork := &net.IPNet{
		IP:   result.IPs[0].Gateway,
		Mask: result.IPs[0].Address.Mask,
	}
	resultBridgeCIDR := resultBridgeNetwork.String()
	if len(addrs) > 0 {
		for _, addr := range addrs {
			if addr.IPNet.String() == resultBridgeCIDR {
				return nil
			}
		}
		return errors.New("bridge configure: mismatch in bridge ip address")
	}

	bridgeAddr := &netlink.Addr{
		IPNet: resultBridgeNetwork,
	}
	err = engine.netLink.AddrAdd(bridge, bridgeAddr)
	if err != nil {
		return errors.Wrapf(err,
			"bridge configure: unable to assign ip address to bridge %s",
			bridge.Attrs().Name)
	}

	return nil
}

// GetInterfaceIPV4Address gets the ipv4 address of a given interface
// in the container
func (engine *engine) GetInterfaceIPV4Address(netnsName string, interfaceName string) (string, error) {
	ipv4Context := newGetContainerIPV4Context(interfaceName, engine.netLink)
	err := engine.ns.WithNetNSPath(netnsName, ipv4Context.run)
	if err != nil {
		return "", err
	}

	return ipv4Context.ipv4Addr, nil
}

// RunIPAMPluginDel invokes the IPAM plugin with the DEL command
func (engine *engine) RunIPAMPluginDel(plugin string, netconf []byte) error {
	err := engine.ipam.ExecDel(plugin, netconf)
	if err != nil {
		return errors.Wrapf(err,
			"bridge ipam DEL: failed to execute the plugin: %s", plugin)
	}

	return nil
}

// DeleteVeth deletes the veth interface in container
func (engine *engine) DeleteVeth(netnsName string, interfaceName string) error {
	delContext := newDeleteLinkContext(interfaceName, engine.ip)
	return engine.ns.WithNetNSPath(netnsName, delContext.run)
}
