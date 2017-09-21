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

	log "github.com/cihub/seelog"
	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/pkg/errors"
)

const defaultMTU = 1500

// NetConf defines the parameters required to configure a bridge and to
// attach the same to the contaner's namespace
type NetConf struct {
	types.NetConf
	BridgeName string `json:"bridge"`
	MTU        int    `json:"mtu"`
}

// NewConf creates a new NetConf object by parsing the arguments supplied
func NewConf(args *skel.CmdArgs) (*NetConf, error) {
	var conf NetConf
	if err := json.Unmarshal(args.StdinData, &conf); err != nil {
		return nil, errors.Wrap(err, "bridge parsing config: failed to parse config")
	}

	if conf.BridgeName == "" {
		return nil, errors.Errorf(
			"bridge parsing config: missing required parameter in config named: '%s'",
			"bridge")
	}

	if conf.MTU == 0 {
		// TODO: Is this really needed?
		conf.MTU = defaultMTU
	}
	log.Debugf("Loaded config: %v", conf)
	return &conf, nil
}
