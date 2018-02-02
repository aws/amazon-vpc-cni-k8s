// Copyright 2014-2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

package api

import (
	"strconv"

	"github.com/fsouza/go-dockerclient"
)

const (
	UnrecognizedTransportProtocolErrorName = "UnrecognizedTransportProtocol"
	UnparseablePortErrorName               = "UnparsablePort"
)

// PortBinding represents a port binding for a container
type PortBinding struct {
	// ContainerPort is the port inside the container
	ContainerPort uint16
	// HostPort is the port exposed on the host
	HostPort uint16
	// BindIP is the IP address to which the port is bound
	BindIP string `json:"BindIp"`
	// Protocol is the protocol of the port
	Protocol TransportProtocol
}

// PortBindingFromDockerPortBinding constructs a PortBinding slice from a docker
// NetworkSettings.Ports map.
func PortBindingFromDockerPortBinding(dockerPortBindings map[docker.Port][]docker.PortBinding) ([]PortBinding, NamedError) {
	portBindings := make([]PortBinding, 0, len(dockerPortBindings))

	for port, bindings := range dockerPortBindings {
		containerPort, err := strconv.Atoi(port.Port())
		if err != nil {
			return nil, &DefaultNamedError{Name: UnparseablePortErrorName, Err: "Error parsing docker port as int " + err.Error()}
		}
		protocol, err := NewTransportProtocol(port.Proto())
		if err != nil {
			return nil, &DefaultNamedError{Name: UnrecognizedTransportProtocolErrorName, Err: err.Error()}
		}

		for _, binding := range bindings {
			hostPort, err := strconv.Atoi(binding.HostPort)
			if err != nil {
				return nil, &DefaultNamedError{Name: UnparseablePortErrorName, Err: "Error parsing port binding as int " + err.Error()}
			}
			portBindings = append(portBindings, PortBinding{
				ContainerPort: uint16(containerPort),
				HostPort:      uint16(hostPort),
				BindIP:        binding.HostIP,
				Protocol:      protocol,
			})
		}
	}
	return portBindings, nil
}
