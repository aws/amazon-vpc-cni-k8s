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
	"net"

	"github.com/aws/amazon-ecs-cni-plugins/plugins/ipam/config"
	"github.com/aws/amazon-ecs-cni-plugins/plugins/ipam/ipstore"
	"github.com/cihub/seelog"
	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/cni/pkg/types/current"
	"github.com/pkg/errors"
)

// Add will return ip, gateway, routes which can be
// used in bridge plugin to configure veth pair and bridge
func Add(args *skel.CmdArgs) error {
	defer seelog.Flush()
	ipamConf, cniVersion, err := config.LoadIPAMConfig(args.StdinData, args.Args)
	if err != nil {
		return err
	}

	dbConf, err := config.LoadDBConfig()
	if err != nil {
		return err
	}

	// Create the ip manager
	ipManager, err := ipstore.NewIPAllocator(dbConf, net.IPNet{
		IP:   ipamConf.IPV4Subnet.IP,
		Mask: ipamConf.IPV4Subnet.Mask})
	if err != nil {
		return err
	}
	defer ipManager.Close()

	return add(ipManager, ipamConf, cniVersion)
}

func add(ipManager ipstore.IPAllocator, ipamConf *config.IPAMConfig, cniVersion string) error {
	err := verifyGateway(ipamConf.IPV4Gateway, ipManager)
	if err != nil {
		return err
	}

	nextIP, err := getIPV4Address(ipManager, ipamConf)
	if err != nil {
		return err
	}

	err = ipManager.Update(config.LastKnownIPKey, nextIP.IP.String())
	if err != nil {
		// This error will only impact how the next ip will be find, it shouldn't cause
		// the command to fail
		seelog.Warnf("Add commands: update the last known ip failed: %v", err)
	}

	result, err := constructResults(ipamConf, *nextIP)
	if err != nil {
		return err
	}
	return types.PrintResult(result, cniVersion)
}

// Del will release one ip address and update the last known ip
func Del(args *skel.CmdArgs) error {
	defer seelog.Flush()
	ipamConf, _, err := config.LoadIPAMConfig(args.StdinData, args.Args)
	if err != nil {
		return err
	}

	if err := validateDelConfiguration(ipamConf); err != nil {
		return err
	}

	dbConf, err := config.LoadDBConfig()
	if err != nil {
		return err
	}
	// Create the ip manager
	ipManager, err := ipstore.NewIPAllocator(dbConf, net.IPNet{
		IP:   ipamConf.IPV4Subnet.IP,
		Mask: ipamConf.IPV4Subnet.Mask,
	})
	if err != nil {
		return err
	}
	defer ipManager.Close()

	return del(ipManager, ipamConf)
}

// validateDelConfiguration checks the configuration for ipam del
func validateDelConfiguration(ipamConf *config.IPAMConfig) error {
	if ipamConf.ID == "" && (ipamConf.IPV4Address.IP == nil || net.ParseIP(ipamConf.IPV4Address.IP.String()) == nil) {
		return errors.New("del commands: ip address and id can not both be empty for deletion")
	}
	return nil
}

func del(ipManager ipstore.IPAllocator, ipamConf *config.IPAMConfig) error {
	var releasedIP string

	if ipamConf.IPV4Address.IP != nil && net.ParseIP(ipamConf.IPV4Address.IP.String()) != nil {
		// Release the ip by ip address
		err := ipManager.Release(ipamConf.IPV4Address.IP.String())
		if err != nil {
			return err
		}
		releasedIP = ipamConf.IPV4Address.IP.String()
	} else {
		// Release the ip by unique id associated with the ip
		ip, err := ipManager.ReleaseByID(ipamConf.ID)
		if err != nil {
			return nil
		}
		releasedIP = ip
	}

	// Update the last known ip
	err := ipManager.Update("lastKnownIP", releasedIP)
	if err != nil {
		// This error will only impact how the next ip will be find, it shouldn't cause
		// the command to fail
		seelog.Warnf("Del commands: update the last known ip failed: %v", err)
	}

	return nil
}

// getIPV4Address return the available ip address from configuration if specified or from the
// db if not explicitly specified in the configuration
func getIPV4Address(ipManager ipstore.IPAllocator, conf *config.IPAMConfig) (*net.IPNet, error) {
	assignedAddress := &net.IPNet{
		IP:   conf.IPV4Address.IP,
		Mask: conf.IPV4Address.Mask,
	}
	if assignedAddress.IP != nil {
		// IP was specifed in the configuration, try to assign this ip as used
		// if this ip has already been used, it will return an error
		err := ipManager.Assign(assignedAddress.IP.String(), conf.ID)
		if err != nil {
			return nil, errors.Wrapf(err, "getIPV4Address commands: failed to mark this ip %v as used", assignedAddress)
		}
	} else {
		// Get the next ip from db based on the last used ip
		nextIP, err := getIPV4AddressFromDB(ipManager, conf)
		if err != nil {
			return nil, err
		}
		assignedAddress.IP = net.ParseIP(nextIP)
		assignedAddress.Mask = conf.IPV4Subnet.Mask
	}
	return assignedAddress, nil
}

// getIPV4AddressFromDB will try to get an ipv4 address from the ipmanager
func getIPV4AddressFromDB(ipManager ipstore.IPAllocator, conf *config.IPAMConfig) (string, error) {
	startIP := conf.IPV4Subnet.IP.Mask(conf.IPV4Subnet.Mask)
	ok, err := ipManager.Exists(config.LastKnownIPKey)
	if err != nil {
		return "", errors.Wrap(err, "getIPV4AddressFromDB commands: failed to read the db")
	}
	if ok {
		lastKnownIPStr, err := ipManager.Get(config.LastKnownIPKey)
		if err != nil {
			return "", errors.Wrap(err, "getIPV4AddressFromDB commands: failed to get lask known ip from the db")
		}
		startIP = net.ParseIP(lastKnownIPStr)
	}

	ipManager.SetLastKnownIP(startIP)
	nextIP, err := ipManager.GetAvailableIP(conf.ID)
	if err != nil {
		return "", errors.Wrap(err, "getIPV4AddressFromDB commands: failed to get available ip from the db")
	}

	return nextIP, nil
}

// constructResults construct the struct from IPAM configuration to be used
// by bridge plugin
func constructResults(conf *config.IPAMConfig, ipv4 net.IPNet) (*current.Result, error) {
	result := &current.Result{}
	ipversion := "4"

	// Currently only ipv4 is supported
	if ipv4.IP.To4() == nil {
		return nil, errors.New("constructResults commands: invalid ipv4 address")
	}

	ipConfig := &current.IPConfig{
		Version: ipversion,
		Address: ipv4,
		Gateway: conf.IPV4Gateway,
	}

	result.IPs = []*current.IPConfig{ipConfig}
	result.Routes = conf.IPV4Routes

	return result, nil
}

// verifyGateway checks if this gateway address is the default gateway or used by other container
func verifyGateway(gw net.IP, ipManager ipstore.IPAllocator) error {
	// Check if gateway address has already been used and if it's used by gateway
	value, err := ipManager.Get(gw.String())
	if err != nil {
		return errors.Wrap(err, "verifyGateway commands: failed to get the value of gateway")
	}
	if value == "" {
		// Address not used, mark it as used
		err := ipManager.Assign(gw.String(), config.GatewayValue)
		if err != nil {
			return errors.Wrap(err, "verifyGateway commands: failed to update gateway into the db")
		}
	} else if value == config.GatewayValue {
		// Address is used by gateway
		return nil
	} else {
		return errors.New("verifyGateway commands: ip of gateway has already been used")
	}

	return nil
}
