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
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/aws/amazon-ecs-cni-plugins/pkg/cninswrapper"
	"github.com/aws/amazon-ecs-cni-plugins/pkg/ec2metadata"
	"github.com/aws/amazon-ecs-cni-plugins/pkg/execwrapper"
	"github.com/aws/amazon-ecs-cni-plugins/pkg/ioutilwrapper"
	"github.com/aws/amazon-ecs-cni-plugins/pkg/netlinkwrapper"
	"github.com/aws/amazon-ecs-cni-plugins/pkg/oswrapper"
	log "github.com/cihub/seelog"
	"github.com/pkg/errors"
	"github.com/vishvananda/netlink"
)

const (
	metadataNetworkInterfacesPath               = "network/interfaces/macs/"
	metadataNetworkInterfaceIDPathSuffix        = "interface-id"
	metadataNetworkInterfaceIPV4CIDRPathSuffix  = "/subnet-ipv4-cidr-block"
	metadataNetworkInterfaceIPV4AddressesSuffix = "/local-ipv4s"
	metadataNetworkInterfaceIPV6AddressesSuffix = "/ipv6s"
	metadataNetworkInterfaceIPV6CIDRPathSuffix  = "/subnet-ipv6-cidr-blocks"
	ipv6GatewayTickDuration                     = 1 * time.Second
	// zeroLengthIPString is what we expect net.IP.String() to return if the
	// ip has length 0. We use this to determing if an IP is empty.
	// Refer https://golang.org/pkg/net/#IP.String
	zeroLengthIPString = "<nil>"
	// maxTicksForRetrievingIPV6Gateway is the maximum number of ticks to wait
	// for retrieving the ipv6 gateway ip from the routing table. We give up
	// after 10 ticks, which corresponds to 10 seconds
	maxTicksForRetrievingIPV6Gateway = 10

	checkDHClientStateInteval = 50 * time.Millisecond
	maxDHClientStopWait       = 1 * time.Second

	minIPV4CIDRBlockSize = 28
	maxIPV4CIDRBlockSize = 16

	instanceMetadataMaxRetryCount          = 20
	instanceMetadataDurationBetweenRetries = 1 * time.Second
)

// Engine represents the execution engine for the ENI plugin. It defines all the
// operations performed by the plugin
type Engine interface {
	GetAllMACAddresses() ([]string, error)
	GetMACAddressOfENI(macAddresses []string, eniID string) (string, error)
	GetInterfaceDeviceName(macAddress string) (string, error)
	GetIPV4GatewayNetmask(macAddress string) (string, string, error)
	GetIPV6PrefixLength(macAddress string) (string, error)
	GetIPV6Gateway(deviceName string) (string, error)
	DoesMACAddressMapToIPV4Address(macAddress string, ipv4Address string) (bool, error)
	DoesMACAddressMapToIPV6Address(macAddress string, ipv4Address string) (bool, error)
	SetupContainerNamespace(netns string, deviceName string, ipv4Address string, ipv6Address string, ipv4Gateway string, ipv6Gateway string, dhclient DHClient, blockIMDS bool) error
	TeardownContainerNamespace(netns string, macAddress string, stopDHClient6 bool, dhclient DHClient) error
}

type engine struct {
	metadata                         ec2metadata.EC2Metadata
	ioutil                           ioutilwrapper.IOUtil
	netLink                          netlinkwrapper.NetLink
	ns                               cninswrapper.NS
	exec                             execwrapper.Exec
	os                               oswrapper.OS
	ipv6GatewayTickDuration          time.Duration
	maxTicksForRetrievingIPV6Gateway int
	metadataMaxRetryCount            int
	metadataDurationBetweenRetries   time.Duration
}

// New creates a new Engine object
func New() Engine {
	return create(
		ec2metadata.NewEC2Metadata(),
		ioutilwrapper.NewIOUtil(),
		netlinkwrapper.NewNetLink(),
		cninswrapper.NewNS(),
		execwrapper.NewExec(),
		oswrapper.NewOS())
}

func create(metadata ec2metadata.EC2Metadata,
	ioutil ioutilwrapper.IOUtil,
	netLink netlinkwrapper.NetLink,
	ns cninswrapper.NS,
	exec execwrapper.Exec,
	os oswrapper.OS,
) Engine {
	return &engine{
		metadata: metadata,
		ioutil:   ioutil,
		netLink:  netLink,
		ns:       ns,
		exec:     exec,
		os:       os,
		ipv6GatewayTickDuration:          ipv6GatewayTickDuration,
		maxTicksForRetrievingIPV6Gateway: maxTicksForRetrievingIPV6Gateway,
		metadataMaxRetryCount:            instanceMetadataMaxRetryCount,
		metadataDurationBetweenRetries:   instanceMetadataDurationBetweenRetries,
	}
}

// GetAllMACAddresses gets a list of mac addresses for all interfaces from the instance
// metadata service
func (engine *engine) GetAllMACAddresses() ([]string, error) {
	macs, err := engine.metadata.GetMetadata(metadataNetworkInterfacesPath)
	if err != nil {
		return nil, errors.Wrap(err,
			"getAllMACAddresses engine: unable to get all mac addresses on the instance from instance metadata")
	}
	return strings.Split(macs, "\n"), nil
}

// GetMACAddressOfENI gets the mac address for a given ENI ID
func (engine *engine) GetMACAddressOfENI(macAddresses []string, eniID string) (string, error) {
	for _, macAddress := range macAddresses {
		// TODO Use fmt.Sprintf and wrap that in a method
		interfaceID, err := engine.metadata.GetMetadata(metadataNetworkInterfacesPath + macAddress + metadataNetworkInterfaceIDPathSuffix)
		if err != nil {
			log.Warnf("Error getting interface id for mac address '%s': %v", macAddress, err)
			continue
		}
		if interfaceID == eniID {
			// MAC addresses retrieved from the metadata service end with the '/' character. Strip it off.
			return strings.Split(macAddress, "/")[0], nil
		}
	}

	return "", newUnmappedMACAddressError("getMACAddressOfENI", "engine",
		fmt.Sprintf("mac address of ENI '%s' not found", eniID))
}

// GetInterfaceDeviceName gets the device name on the host, given a mac address
func (engine *engine) GetInterfaceDeviceName(macAddress string) (string, error) {
	hardwareAddr, err := net.ParseMAC(macAddress)
	if err != nil {
		return "", errors.Wrapf(err, "getInterfaceDeviceName engine: malformatted mac address specified")
	}

	link, err := getLinkByHardwareAddress(engine.netLink, hardwareAddr)
	if err != nil {
		return "", errors.Wrapf(err,
			"getInterfaceDeviceName engine: unable to get device with hardware address '%s'", macAddress)
	}

	return link.Attrs().Name, nil
}

// GetIPV4GatewayNetmask gets the ipv4 gateway and the netmask from the instance
// metadata, given a mac address
func (engine *engine) GetIPV4GatewayNetmask(macAddress string) (string, string, error) {
	// TODO Use fmt.Sprintf and wrap that in a method
	cidrBlock, err := engine.metadata.GetMetadata(metadataNetworkInterfacesPath + macAddress + metadataNetworkInterfaceIPV4CIDRPathSuffix)
	if err != nil {
		return "", "", errors.Wrapf(err,
			"getIPV4GatewayNetmask engine: unable to get ipv4 subnet and cidr block for '%s' from instance metadata", macAddress)
	}

	return getIPV4GatewayNetmask(cidrBlock)
}

func getIPV4GatewayNetmask(cidrBlock string) (string, string, error) {
	// The IPV4 CIDR block is of the format ip-addr/netmask
	ip, ipNet, err := net.ParseCIDR(cidrBlock)
	if err != nil {
		return "", "", errors.Wrapf(err,
			"getIPV4GatewayNetmask engine: unable to parse response for ipv4 cidr: '%s' from instance metadata", cidrBlock)
	}

	ip4 := ip.To4()
	if ip4 == nil {
		return "", "", newParseIPV4GatewayNetmaskError("getIPV4GatewayNetmask", "engine",
			fmt.Sprintf("unable to parse ipv4 gateway from cidr block '%s'", cidrBlock))
	}

	maskOnes, _ := ipNet.Mask.Size()
	// As per
	// http://docs.aws.amazon.com/AmazonVPC/latest/UserGuide/VPC_Subnets.html#VPC_Sizing
	// You can assign a single CIDR block to a VPC. The allowed block size
	// is between a /16 netmask and /28 netmask. Verify that
	if maskOnes > minIPV4CIDRBlockSize {
		return "", "", errors.Errorf("eni ipv4 netmask: invalid ipv4 cidr block, %d > 28", maskOnes)
	}
	if maskOnes < maxIPV4CIDRBlockSize {
		return "", "", errors.Errorf("eni ipv4 netmask: invalid ipv4 cidr block, %d <= 16", maskOnes)
	}

	// ipv4 gateway is the first available IP address in the subnet
	ip4[3] = ip4[3] + 1
	return ip4.String(), fmt.Sprintf("%d", maskOnes), nil
}

// GetIPV6PrefixLength gets the ipv6 subnet mask from the instance
// metadata, given a mac address
func (engine *engine) GetIPV6PrefixLength(macAddress string) (string, error) {
	// TODO Use fmt.Sprintf and wrap that in a method
	cidrBlock, err := engine.metadata.GetMetadata(metadataNetworkInterfacesPath + macAddress + metadataNetworkInterfaceIPV6CIDRPathSuffix)
	if err != nil {
		return "", errors.Wrapf(err,
			"getIPV6Netmask engine: unable to get ipv6 subnet and cidr block for '%s' from instance metadata", macAddress)
	}

	return getIPV6PrefixLength(cidrBlock)
}

func getIPV6PrefixLength(cidrBlock string) (string, error) {
	// The IPV6 CIDR block is of the format ip-addr/netmask
	_, ipNet, err := net.ParseCIDR(cidrBlock)
	if err != nil {
		return "", errors.Wrapf(err,
			"getIPV6Netmask engine: unable to parse response for ipv6 cidr: '%s' from instance metadata", cidrBlock)
	}

	maskOnes, _ := ipNet.Mask.Size()
	return fmt.Sprintf("%d", maskOnes), nil
}

// GetIPV6Gateway gets the ipv6 address of the subnet gateway
func (engine *engine) GetIPV6Gateway(deviceName string) (string, error) {
	// Get the device link for the ENI
	eniLink, err := engine.netLink.LinkByName(deviceName)
	if err != nil {
		return "", errors.Wrapf(err,
			"getIPV6Gateway engine: unable to get link for device '%s'", deviceName)
	}

	return engine.getIPV6GatewayIPFromRoutes(eniLink, deviceName,
		engine.maxTicksForRetrievingIPV6Gateway, engine.ipv6GatewayTickDuration)
}

func (engine *engine) getIPV6GatewayIPFromRoutes(link netlink.Link,
	deviceName string,
	maxTicks int,
	durationBetweenTicks time.Duration) (string, error) {
	// In rare cases, it is possible that there's a delay in the kernel updating
	// its routing table for non-primary ENIs attached to the instance. Retry querying
	// the routing table for such scenarios.
	for numTicks := 0; numTicks < maxTicks; numTicks++ {
		log.Infof("Trying to get IPV6 Gateway from route table (device=%s), attempt: %d/%d",
			deviceName, numTicks+1, maxTicks)
		gateway, ok, err := engine.getIPV6GatewayIPFromRoutesOnce(link, deviceName)
		if err != nil {
			return "", err
		}
		if ok {
			return gateway, nil
		}

		time.Sleep(durationBetweenTicks)
	}

	return "", errors.Errorf(
		"getIPV6Gateway engine: unable to get gateway from route table for '%s'", deviceName)
}

func (engine *engine) getIPV6GatewayIPFromRoutesOnce(link netlink.Link, deviceName string) (string, bool, error) {
	routes, err := engine.netLink.RouteList(link, netlink.FAMILY_V6)
	if err != nil {
		return "", false, errors.Wrapf(err,
			"getIPV6Gateway engine: unable to get ipv6 routes for device '%s'", deviceName)
	}

	for _, route := range routes {
		// Search for "default" route. A "default" route has no source and
		// destination ip addresses, but has the gateway set to a non-emtpty string
		if route.Dst != nil {
			continue
		}

		if (route.Dst == nil || route.Dst.String() == zeroLengthIPString) && // Dst is not set
			route.Src.String() == zeroLengthIPString && // Src is not set
			route.Gw.String() != zeroLengthIPString { // Gw is set
			log.Debugf("Found ipv6 gateway (device=%s): %s", deviceName, route.Gw.String())
			return route.Gw.String(), true, nil
		}
	}

	return "", false, nil
}

// DoesMACAddressMapToIPV4Address validates in the MAC Address for the ENI maps to the
// IPV4 Address specified
func (engine *engine) DoesMACAddressMapToIPV4Address(macAddress string, ipv4Address string) (bool, error) {
	ok, err := engine.doesMACAddressMapToIPAddress(macAddress, ipv4Address, metadataNetworkInterfaceIPV4AddressesSuffix)
	if err != nil {
		return false, errors.Wrap(err,
			"doesMACAddressMapToIPV4Address engine: unable to get ipv4 addresses from instance metadata")
	}

	return ok, nil
}

// DoesMACAddressMapToIPV6Address validates in the MAC Address for the ENI maps to the
// IPV6 Address specified
func (engine *engine) DoesMACAddressMapToIPV6Address(macAddress string, ipv6Address string) (bool, error) {
	ok, err := engine.doesMACAddressMapToIPAddress(macAddress, ipv6Address, metadataNetworkInterfaceIPV6AddressesSuffix)
	if err != nil {
		return false, errors.Wrap(err,
			"doesMACAddressMapToIPv6Address engine: unable to get ipv6 addresses from instance metadata")
	}

	return ok, nil
}

func (engine *engine) doesMACAddressMapToIPAddress(macAddress string, addressToFind string, metatdataPathSuffix string) (bool, error) {
	// TODO Use fmt.Sprintf and wrap that in a method
	var addressesResponse string
	var err error

	attempts := 1
	for {
		addressesResponse, err = engine.metadata.GetMetadata(
			metadataNetworkInterfacesPath + macAddress + metatdataPathSuffix)
		if err == nil {
			break
		}
		log.Warnf("Error querying metadata path (attempt %d/%d) : '%s': %v",
			attempts, engine.metadataMaxRetryCount,
			metadataNetworkInterfacesPath+macAddress+metatdataPathSuffix, err)
		// It could take few seconds for the ENI's MAC address to show up in
		// instance metdata
		// Retry a few times before giving up
		if attempts >= engine.metadataMaxRetryCount {
			break
		}
		attempts++
		time.Sleep(engine.metadataDurationBetweenRetries)
	}
	if err != nil {
		return false, errors.Wrapf(err,
			"querying metadata path: '%s'",
			metadataNetworkInterfacesPath+macAddress+metatdataPathSuffix)
	}
	for _, address := range strings.Split(addressesResponse, "\n") {
		if address == addressToFind {
			return true, nil
		}
	}
	return false, nil
}

// SetupContainerNamespace configures the network namespace of the container with
// the ipv4 address and routes to use the ENI interface. The ipv4 address is of the
// ipv4-address/netmask format
func (engine *engine) SetupContainerNamespace(netns string,
	deviceName string,
	ipv4Address string,
	ipv6Address string,
	ipv4Gateway string,
	ipv6Gateway string,
	dhclient DHClient,
	blockIMDS bool) error {
	// Get the device link for the ENI
	eniLink, err := engine.netLink.LinkByName(deviceName)
	if err != nil {
		return errors.Wrapf(err,
			"setupContainerNamespace engine: unable to get link for device '%s'", deviceName)
	}

	// Get the handle for the container's network namespace
	containerNS, err := engine.ns.GetNS(netns)
	if err != nil {
		return errors.Wrapf(err,
			"setupContainerNamespace engine: unable to get network namespace for '%s'", netns)
	}

	// Assign the ENI device to container's network namespace
	err = engine.netLink.LinkSetNsFd(eniLink, int(containerNS.Fd()))
	if err != nil {
		return errors.Wrapf(err,
			"setupContainerNamespace engine: unable to move device '%s' to container namespace '%s'", deviceName, netns)
	}

	// Generate the closure to execute within the container's namespace
	toRun, err := newSetupNamespaceClosureContext(engine.netLink, dhclient, deviceName,
		ipv4Address, ipv6Address, ipv4Gateway, ipv6Gateway, blockIMDS)
	if err != nil {
		return errors.Wrap(err,
			"setupContainerNamespace engine: unable to create closure to execute in container namespace")
	}

	// Execute the closure within the container's namespace
	err = engine.ns.WithNetNSPath(netns, toRun.run)
	if err != nil {
		return errors.Wrapf(err,
			"setupContainerNamespace engine: unable to setup device '%s' in namespace '%s'", deviceName, netns)
	}
	return nil
}

// TeardownContainerNamespace brings down the ENI device in the container's namespace
func (engine *engine) TeardownContainerNamespace(netns string, macAddress string, stopDHClient6 bool, dhclient DHClient) error {
	// Generate the closure to execute within the container's namespace
	toRun, err := newTeardownNamespaceClosureContext(engine.netLink, dhclient,
		macAddress, stopDHClient6, checkDHClientStateInteval, maxDHClientStopWait)
	if err != nil {
		return errors.Wrap(err,
			"teardownContainerNamespace engine: unable to create closure to execute in container namespace")
	}

	// Execute the closure within the container's namespace
	err = engine.ns.WithNetNSPath(netns, toRun.run)
	if err != nil {
		return errors.Wrap(err,
			"teardownContainerNamespace engine: unable to teardown container namespace")
	}
	return nil
}
