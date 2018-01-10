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

package ecscni

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/aws/amazon-ecs-agent/agent/logger"
	"github.com/cihub/seelog"
	"github.com/containernetworking/cni/libcni"
	cnitypes "github.com/containernetworking/cni/pkg/types"
	"github.com/pkg/errors"
)

// CNIClient defines the method of setting/cleaning up container namespace
type CNIClient interface {
	Version(string) (string, error)
	Capabilities(string) ([]string, error)
	SetupNS(*Config) error
	CleanupNS(*Config) error
	ReleaseIPResource(*Config) error
}

// cniClient is the client to call plugin and setup the network
type cniClient struct {
	pluginsPath string
	cniVersion  string
	subnet      string
	libcni      libcni.CNI
}

// NewClient creates a client of ecscni which is used to invoke the plugin
func NewClient(cfg *Config) CNIClient {
	libcniConfig := &libcni.CNIConfig{
		Path: []string{cfg.PluginsPath},
	}

	return &cniClient{
		pluginsPath: cfg.PluginsPath,
		cniVersion:  cfg.MinSupportedCNIVersion,
		subnet:      ecsSubnet,
		libcni:      libcniConfig,
	}
}

// SetupNS will set up the namespace of container, including create the bridge
// and the veth pair, move the eni to container namespace, setup the routes
func (client *cniClient) SetupNS(cfg *Config) error {
	cns := &libcni.RuntimeConf{
		ContainerID: cfg.ContainerID,
		NetNS:       fmt.Sprintf(netnsFormat, cfg.ContainerPID),
		IfName:      defaultEthName,
	}

	networkConfigList, err := client.createNetworkConfig(cfg, client.createBridgeNetworkConfigWithIPAM)
	if err != nil {
		return errors.Wrap(err, "cni invocation: failed to construct network configuration for configuring namespace")
	}

	seelog.Debugf("Starting setup the ENI (%s) in container namespace: %s", cfg.ENIID, cfg.ContainerID)
	os.Setenv("ECS_CNI_LOGLEVEL", logger.GetLevel())
	defer os.Unsetenv("ECS_CNI_LOGLEVEL")
	result, err := client.libcni.AddNetworkList(networkConfigList, cns)
	if err != nil {
		return err
	}

	seelog.Debugf("Set up container namespace done: %s", result.String())
	return nil
}

// CleanupNS will clean up the container namespace, including remove the veth
// pair and stop the dhclient
func (client *cniClient) CleanupNS(cfg *Config) error {
	cns := &libcni.RuntimeConf{
		ContainerID: cfg.ContainerID,
		NetNS:       fmt.Sprintf(netnsFormat, cfg.ContainerPID),
		IfName:      defaultEthName,
	}

	// clean up the network namespace is separate from releasing the IP from IPAM
	networkConfigList, err := client.createNetworkConfig(cfg, client.createBridgeNetworkConfigWithoutIPAM)
	if err != nil {
		return errors.Wrap(err, "cni invocation: failed to construct network configuration for namespace cleanup")
	}

	seelog.Debugf("Starting clean up the container namespace: %s", cfg.ContainerID)
	os.Setenv("ECS_CNI_LOGLEVEL", logger.GetLevel())
	defer os.Unsetenv("ECS_CNI_LOGLEVEL")
	return client.libcni.DelNetworkList(networkConfigList, cns)
}

// ReleaseIPResource marks the ip available in the ipam db
func (client *cniClient) ReleaseIPResource(cfg *Config) error {
	cns := &libcni.RuntimeConf{
		ContainerID: cfg.ContainerID,
		NetNS:       fmt.Sprintf(netnsFormat, cfg.ContainerPID),
		IfName:      defaultEthName,
	}

	ipamConfig, err := client.createIPAMNetworkConfig(cfg)
	if err != nil {
		return err
	}

	networkConfigList := &libcni.NetworkConfigList{
		CNIVersion: client.cniVersion,
		Plugins:    []*libcni.NetworkConfig{ipamConfig},
	}

	seelog.Debugf("Releasing the ip resource from ipam db, id: [%s], ip: [%v]", cfg.ID, cfg.IPAMV4Address)
	os.Setenv("ECS_CNI_LOGLEVEL", logger.GetLevel())
	defer os.Unsetenv("ECS_CNI_LOGLEVEL")
	return client.libcni.DelNetworkList(networkConfigList, cns)
}

// createNetworkConfig creates the configuration for invoking plugins
func (client *cniClient) createNetworkConfig(cfg *Config, bridgeConfigFunc func(*Config) (*libcni.NetworkConfig, error)) (*libcni.NetworkConfigList, error) {
	bridgeConfig, err := bridgeConfigFunc(cfg)
	if err != nil {
		return nil, err
	}

	eniConfig, err := client.createENINetworkConfig(cfg)
	if err != nil {
		return nil, err
	}

	pluginConfigs := []*libcni.NetworkConfig{bridgeConfig, eniConfig}
	networkConfigList := &libcni.NetworkConfigList{
		CNIVersion: client.cniVersion,
		Plugins:    pluginConfigs,
	}

	return networkConfigList, nil
}

// createBridgeNetworkConfigWithIPAM creates the config of bridge for ADD command, where
// bridge plugin acquires the IP and route information from IPAM
func (client *cniClient) createBridgeNetworkConfigWithIPAM(cfg *Config) (*libcni.NetworkConfig, error) {
	// Create the bridge config first
	bridgeConfig := client.createBridgeConfig(cfg)

	// Create the ipam config
	ipamConfig, err := client.createIPAMConfig(cfg)
	if err != nil {
		return nil, err
	}

	bridgeConfig.IPAM = ipamConfig

	return client.constructNetworkConfig(bridgeConfig, ECSBridgePluginName)
}

// createBridgeNetworkConfigWithoutIPAM creates the config of the bridge for removal
func (client *cniClient) createBridgeNetworkConfigWithoutIPAM(cfg *Config) (*libcni.NetworkConfig, error) {
	return client.constructNetworkConfig(client.createBridgeConfig(cfg), ECSBridgePluginName)
}

func (client *cniClient) createBridgeConfig(cfg *Config) BridgeConfig {
	bridgeName := defaultBridgeName
	if len(cfg.BridgeName) != 0 {
		bridgeName = cfg.BridgeName
	}

	bridgeConfig := BridgeConfig{
		Type:       ECSBridgePluginName,
		CNIVersion: client.cniVersion,
		BridgeName: bridgeName,
	}

	return bridgeConfig
}

// constructNetworkConfig takes in the config from agent and construct the configuration
// that's accepted by the libcni
func (client *cniClient) constructNetworkConfig(cfg interface{}, plugin string) (*libcni.NetworkConfig, error) {
	configBytes, err := json.Marshal(cfg)
	if err != nil {
		seelog.Errorf("Marshal configuration for plugin %s failed, error: %v", plugin, err)
		return nil, err
	}
	networkConfig := &libcni.NetworkConfig{
		Network: &cnitypes.NetConf{
			Type: plugin,
		},
		Bytes: configBytes,
	}

	return networkConfig, nil
}

func (client *cniClient) createENINetworkConfig(cfg *Config) (*libcni.NetworkConfig, error) {
	eniConf := ENIConfig{
		Type:                 ECSENIPluginName,
		CNIVersion:           client.cniVersion,
		ENIID:                cfg.ENIID,
		IPV4Address:          cfg.ENIIPV4Address,
		MACAddress:           cfg.ENIMACAddress,
		IPV6Address:          cfg.ENIIPV6Address,
		BlockInstanceMetdata: cfg.BlockInstanceMetdata,
	}

	return client.constructNetworkConfig(eniConf, ECSENIPluginName)
}

// createIPAMNetworkConfig constructs the ipam configuration accepted by libcni
func (client *cniClient) createIPAMNetworkConfig(cfg *Config) (*libcni.NetworkConfig, error) {
	ipamConfig, err := client.createIPAMConfig(cfg)
	if err != nil {
		return nil, err
	}

	ipamNetworkConfig := IPAMNetworkConfig{
		Name:       ECSIPAMPluginName,
		CNIVersion: client.cniVersion,
		IPAM:       ipamConfig,
	}
	return client.constructNetworkConfig(ipamNetworkConfig, ECSIPAMPluginName)
}

func (client *cniClient) createIPAMConfig(cfg *Config) (IPAMConfig, error) {
	_, dst, err := net.ParseCIDR(TaskIAMRoleEndpoint)
	if err != nil {
		return IPAMConfig{}, err
	}

	routes := []*cnitypes.Route{
		{
			Dst: *dst,
		},
	}
	for _, route := range cfg.AdditionalLocalRoutes {
		seelog.Debugf("Adding an additional route for %s", route)
		ipNetRoute := (net.IPNet)(route)
		routes = append(routes, &cnitypes.Route{Dst: ipNetRoute})
	}

	ipamConfig := IPAMConfig{
		Type:        ECSIPAMPluginName,
		CNIVersion:  client.cniVersion,
		IPV4Subnet:  client.subnet,
		IPV4Address: cfg.IPAMV4Address,
		ID:          cfg.ID,
		IPV4Routes:  routes,
	}

	return ipamConfig, nil
}

// Version returns the version of the plugin
func (client *cniClient) Version(name string) (string, error) {
	file := filepath.Join(client.pluginsPath, name)

	// Check if the plugin file exists before executing it
	_, err := os.Stat(file)
	if err != nil {
		return "", err
	}

	cmd := exec.Command(file, versionCommand)
	versionInfo, err := cmd.Output()
	if err != nil {
		return "", err
	}

	version := &cniPluginVersion{}
	// versionInfo is of the format
	// {"version":"2017.06.0","dirty":true,"gitShortHash":"226db36"}
	// Unmarshal this
	err = json.Unmarshal(versionInfo, version)
	if err != nil {
		return "", errors.Wrapf(err, "ecscni: unmarshal version from string: %s", versionInfo)
	}

	return version.str(), nil
}

// cniPluginVersion is used to convert the JSON output of the
// '--version' command into a string
type cniPluginVersion struct {
	Version string `json:"version"`
	Dirty   bool   `json:"dirty"`
	Hash    string `json:"gitShortHash"`
}

// str generates a string version of the CNI plugin version
// Example:
// {"version":"2017.06.0","dirty":true,"gitShortHash":"226db36"} => @226db36-2017.06.0
// {"version":"2017.06.0","dirty":false,"gitShortHash":"326db36"} => 326db36-2017.06.0
func (version *cniPluginVersion) str() string {
	ver := ""
	if version.Dirty {
		ver = "@"
	}
	return ver + version.Hash + "-" + version.Version
}

// Capabilities returns the capabilities supported by a plugin
func (client *cniClient) Capabilities(name string) ([]string, error) {
	file := filepath.Join(client.pluginsPath, name)

	// Check if the plugin file exists before executing it
	_, err := os.Stat(file)
	if err != nil {
		return nil, errors.Wrapf(err, "ecscni: unable to describe file info for '%s'", file)
	}

	cmd := exec.Command(file, capabilitiesCommand)
	capabilitiesInfo, err := cmd.Output()
	if err != nil {
		return nil, errors.Wrapf(err, "ecscni: failed invoking capabilities command for '%s'", name)
	}

	capabilities := &struct {
		Capabilities []string `json:"capabilities"`
	}{}
	err = json.Unmarshal(capabilitiesInfo, capabilities)
	if err != nil {
		return nil, errors.Wrapf(err, "ecscni: failed to unmarshal capabilities for '%s' from string: %s", name, capabilitiesInfo)
	}

	return capabilities.Capabilities, nil
}
