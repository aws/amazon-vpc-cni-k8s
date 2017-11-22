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
	"encoding/json"
	"net"

	log "github.com/cihub/seelog"
	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/pkg/errors"
)

// NetConf defines the parameters required to configure a contaner's namespace
// with an ENI
type NetConf struct {
	types.NetConf
	ENIID       string `json:"eni"`
	IPV4Address string `json:"ipv4-address"`
	MACAddress  string `json:"mac"`
	IPV6Address string `json:"ipv6-address"`
	BlockIMDS   bool   `json:"block-instance-metadata"`
}

// NewConf creates a new NetConf object by parsing the arguments supplied
func NewConf(args *skel.CmdArgs) (*NetConf, error) {
	var conf NetConf
	if err := json.Unmarshal(args.StdinData, &conf); err != nil {
		return nil, errors.Wrap(err, "newconf types: failed to parse config")
	}

	// Validate if all the required fields are present
	if conf.ENIID == "" {
		return nil, errors.Errorf("newconf types: missing required parameter in config: '%s'", "eni")
	}
	if conf.IPV4Address == "" {
		return nil, errors.Errorf("newconf types: missing required parameter in config: '%s'", "ipv4-address")
	}
	if conf.MACAddress == "" {
		return nil, errors.Errorf("newconf types: missing required parameter in config: '%s'", "mac")
	}

	// Validate if the ipv4 address in the config is valid
	if err := isValidIPV4Address(conf.IPV4Address); err != nil {
		return nil, err
	}

	// Validate if the mac address in the config is valid
	if _, err := net.ParseMAC(conf.MACAddress); err != nil {
		return nil, errors.Wrapf(err, "newconf types: malformatted mac address specified")
	}

	// Validate if the ipv6 address in the config is valid, when supplied
	if conf.IPV6Address != "" {
		if err := isValidIPV6Address(conf.IPV6Address); err != nil {
			return nil, err
		}
	}

	// Validation complete. Return the parsed config object
	log.Debugf("Loaded config: %v", conf)
	return &conf, nil
}

func isValidIPV4Address(address string) error {
	ip := net.ParseIP(address)
	if ip == nil {
		return errors.Errorf("newconf types: malformed IPv4 address specified")
	}
	if ip.To4() == nil {
		return errors.Errorf("newconf types: invalid IPv4 address specified")
	}

	return nil
}

func isValidIPV6Address(address string) error {
	ip := net.ParseIP(address)
	if ip == nil {
		return errors.Errorf("newconf types: malformed IPv6 address specified")
	}
	// There's no To6() method in the `net` package. Instead, just check that
	// it's not a valid `v4` IP, but is a valid IP
	if ip.To4() != nil {
		return errors.Errorf("newconf types: invalid IPv6 address specified")
	}

	return nil
}
