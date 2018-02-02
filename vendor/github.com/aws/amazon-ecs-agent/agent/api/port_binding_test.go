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
	"reflect"
	"testing"

	docker "github.com/fsouza/go-dockerclient"
)

func TestPortBindingFromDockerPortBinding(t *testing.T) {
	pairs := []struct {
		dockerPortBindings map[docker.Port][]docker.PortBinding
		ecsPortBindings    []PortBinding
	}{
		{
			map[docker.Port][]docker.PortBinding{
				"53/udp": {{HostIP: "1.2.3.4", HostPort: "55"}},
			},
			[]PortBinding{
				{
					BindIP:        "1.2.3.4",
					HostPort:      55,
					ContainerPort: 53,
					Protocol:      TransportProtocolUDP,
				},
			},
		},
		{
			map[docker.Port][]docker.PortBinding{
				"80/tcp": {
					{HostIP: "2.3.4.5", HostPort: "8080"},
					{HostIP: "5.6.7.8", HostPort: "80"},
				},
			},
			[]PortBinding{
				{
					BindIP:        "2.3.4.5",
					HostPort:      8080,
					ContainerPort: 80,
					Protocol:      TransportProtocolTCP,
				},
				{
					BindIP:        "5.6.7.8",
					HostPort:      80,
					ContainerPort: 80,
					Protocol:      TransportProtocolTCP,
				},
			},
		},
	}

	for i, pair := range pairs {
		converted, err := PortBindingFromDockerPortBinding(pair.dockerPortBindings)
		if err != nil {
			t.Errorf("Error converting port binding pair #%v: %v", i, err)
		}
		if !reflect.DeepEqual(pair.ecsPortBindings, converted) {
			t.Errorf("Converted bindings didn't match expected for #%v: expected %+v, actual %+v", i, pair.ecsPortBindings, converted)
		}
	}
}

func TestPortBindingErrors(t *testing.T) {
	badInputs := []struct {
		dockerPortBindings map[docker.Port][]docker.PortBinding
		errorName          string
	}{
		{
			map[docker.Port][]docker.PortBinding{
				"woof/tcp": {
					{HostIP: "2.3.4.5", HostPort: "8080"},
					{HostIP: "5.6.7.8", HostPort: "80"},
				},
			},
			UnparseablePortErrorName,
		},
		{
			map[docker.Port][]docker.PortBinding{
				"80/tcp": {
					{HostIP: "2.3.4.5", HostPort: "8080"},
					{HostIP: "5.6.7.8", HostPort: "bark"},
				},
			},
			UnparseablePortErrorName,
		},
		{
			map[docker.Port][]docker.PortBinding{
				"80/bark": {
					{HostIP: "2.3.4.5", HostPort: "8080"},
					{HostIP: "5.6.7.8", HostPort: "80"},
				},
			},
			UnrecognizedTransportProtocolErrorName,
		},
	}

	for i, pair := range badInputs {
		_, err := PortBindingFromDockerPortBinding(pair.dockerPortBindings)
		if err == nil {
			t.Errorf("Expected error converting port binding pair #%v", i)
		}
		namedErr, ok := err.(NamedError)
		if !ok {
			t.Errorf("Expected err to implement NamedError")
		}
		if namedErr.ErrorName() != pair.errorName {
			t.Errorf("Expected %s but was %s", pair.errorName, namedErr.ErrorName())
		}
	}
}
