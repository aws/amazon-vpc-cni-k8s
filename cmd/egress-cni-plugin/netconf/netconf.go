// Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
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

package netconf

import (
	"encoding/json"
	"fmt"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
	"github.com/containernetworking/cni/pkg/types"
	cniversion "github.com/containernetworking/cni/pkg/version"
	"net"
)

const (
	EGRESS_IPV4_INTERFACE_NAME = "v4if0"
	EGRESS_IPV6_INTERFACE_NAME = "v6if0"
)
// NetConf is our CNI config structure
type NetConf struct {
	types.NetConf

	// Interface inside container to create
	IfName string `json:"ifName"`

	// MTU for Egress v4 interface
	MTU string `json:"mtu"`

	Enabled string `json:"enabled"`

	RandomizeSNAT string `json:"randomizeSNAT"`

	// IP to use as SNAT target
	NodeIP net.IP `json:"nodeIP"`

	PluginLogFile  string `json:"pluginLogFile"`
	PluginLogLevel string `json:"pluginLogLevel"`
}

func LoadConf(bytes []byte) (*NetConf, logger.Logger, error) {
	conf := &NetConf{}

	if err := json.Unmarshal(bytes, conf); err != nil {
		return nil, nil, err
	}

	if conf.RawPrevResult != nil {
		if err := cniversion.ParsePrevResult(&conf.NetConf); err != nil {
			return nil, nil, fmt.Errorf("could not parse prevResult: %v", err)
		}
	}

	logConfig := logger.Configuration{
		LogLevel:    conf.PluginLogLevel,
		LogLocation: conf.PluginLogFile,
	}
	log := logger.New(&logConfig)
	return conf, log, nil
}