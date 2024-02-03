// Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
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
package vpc

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetENILimit(t *testing.T) {
	eniLimit, err := GetENILimit("large")
	assert.Equal(t, eniLimit, -1)
	assert.Equal(t, err, ErrInstanceTypeNotExist)

	eniLimit, err = GetENILimit("a1.2xlarge")
	assert.Equal(t, eniLimit, 4)
	assert.NoError(t, err)

	eniLimit, err = GetENILimit("a1.4xlarge")
	assert.Equal(t, eniLimit, 8)
	assert.NoError(t, err)

	eniLimit, err = GetENILimit("p5.48xlarge")
	assert.Equal(t, eniLimit, 64)
	assert.NoError(t, err)
}

func TestGetIPv4Limit(t *testing.T) {
	ipv4Limit, err := GetIPv4Limit("large")
	assert.Equal(t, ipv4Limit, -1)
	assert.Equal(t, err, ErrInstanceTypeNotExist)

	ipv4Limit, err = GetIPv4Limit("a1.2xlarge")
	assert.Equal(t, ipv4Limit, 15)
	assert.NoError(t, err)

	ipv4Limit, err = GetIPv4Limit("a1.4xlarge")
	assert.Equal(t, ipv4Limit, 30)
	assert.NoError(t, err)
}

func TestGetDefaultNetworkIndex(t *testing.T) {
	defaultNetworkIndex, err := GetDefaultNetworkCardIndex("large")
	assert.Equal(t, defaultNetworkIndex, -1)
	assert.Equal(t, err, ErrInstanceTypeNotExist)

	defaultNetworkIndex, err = GetDefaultNetworkCardIndex("a1.2xlarge")
	assert.Equal(t, defaultNetworkIndex, 0)
	assert.NoError(t, err)

	defaultNetworkIndex, err = GetDefaultNetworkCardIndex("a1.4xlarge")
	assert.Equal(t, defaultNetworkIndex, 0)
	assert.NoError(t, err)
}

func TestGetHypervisorType(t *testing.T) {
	hypervisorType, err := GetHypervisorType("large")
	assert.Equal(t, hypervisorType, "")
	assert.Equal(t, err, ErrInstanceTypeNotExist)

	hypervisorType, err = GetHypervisorType("a1.2xlarge")
	assert.Equal(t, hypervisorType, "nitro")
	assert.NoError(t, err)

	hypervisorType, err = GetHypervisorType("a1.metal")
	assert.Equal(t, hypervisorType, "unknown")
	assert.NoError(t, err)

	hypervisorType, err = GetHypervisorType("c3.large")
	assert.Equal(t, hypervisorType, "xen")
	assert.NoError(t, err)
}

func TestGetIsBareMetal(t *testing.T) {
	isBaremetal, err := GetIsBareMetal("large")
	assert.False(t, isBaremetal)
	assert.Equal(t, err, ErrInstanceTypeNotExist)

	isBaremetal, err = GetIsBareMetal("a1.2xlarge")
	assert.False(t, isBaremetal)
	assert.NoError(t, err)

	isBaremetal, err = GetIsBareMetal("a1.metal")
	assert.True(t, isBaremetal)
	assert.NoError(t, err)
}

func TestGetNetworkCards(t *testing.T) {
	networkCards, err := GetNetworkCards("large")
	assert.Equal(t, networkCards, []NetworkCard(nil))
	assert.Equal(t, err, ErrInstanceTypeNotExist)

	networkCards, err = GetNetworkCards("a1.2xlarge")
	assert.Equal(t, networkCards, []NetworkCard{
		{
			MaximumNetworkInterfaces: 4,
			NetworkCardIndex:         0,
		},
	})
	assert.NoError(t, err)

	networkCards, err = GetNetworkCards("a1.4xlarge")
	assert.Equal(t, networkCards, []NetworkCard{
		{
			MaximumNetworkInterfaces: 8,
			NetworkCardIndex:         0,
		},
	})
	assert.NoError(t, err)

	networkCards, err = GetNetworkCards("c5a.metal")
	assert.Equal(t, networkCards, []NetworkCard(nil))
	assert.Equal(t, err, ErrNoInfo)
}

func TestGetInstance(t *testing.T) {
	instance, ok := GetInstance("large")
	assert.False(t, ok)

	instance, ok = GetInstance("a1.2xlarge")
	assert.Equal(t, instance, InstanceTypeLimits{
		ENILimit:                4,
		IPv4Limit:               15,
		DefaultNetworkCardIndex: 0,
		NetworkCards: []NetworkCard{
			{
				MaximumNetworkInterfaces: 4,
				NetworkCardIndex:         0,
			},
		},
		HypervisorType: "nitro",
		IsBareMetal:    false,
	})
	assert.True(t, ok)

	instance, ok = GetInstance("a1.4xlarge")
	assert.Equal(t, instance, InstanceTypeLimits{
		ENILimit:                8,
		IPv4Limit:               30,
		DefaultNetworkCardIndex: 0,
		NetworkCards: []NetworkCard{
			{
				MaximumNetworkInterfaces: 8,
				NetworkCardIndex:         0,
			},
		},
		HypervisorType: "nitro",
		IsBareMetal:    false,
	})
	assert.True(t, ok)
}

func TestSetInstance(t *testing.T) {
	SetInstance("a1.2xlarge", 7, 21, 0, []NetworkCard{{MaximumNetworkInterfaces: 7, NetworkCardIndex: 0}}, "xen", true)
	instance, ok := GetInstance("a1.2xlarge")
	assert.Equal(t, instance, InstanceTypeLimits{
		ENILimit:                7,
		IPv4Limit:               21,
		DefaultNetworkCardIndex: 0,
		NetworkCards: []NetworkCard{
			{
				MaximumNetworkInterfaces: 7,
				NetworkCardIndex:         0,
			},
		},
		HypervisorType: "xen",
		IsBareMetal:    true,
	})
	assert.True(t, ok)

	SetInstance("a1.2xlarge", 4, 15, 0, []NetworkCard{{MaximumNetworkInterfaces: 4, NetworkCardIndex: 0}}, "nitro", false)
	instance, ok = GetInstance("a1.2xlarge")
	assert.Equal(t, instance, InstanceTypeLimits{
		ENILimit:                4,
		IPv4Limit:               15,
		DefaultNetworkCardIndex: 0,
		NetworkCards: []NetworkCard{
			{
				MaximumNetworkInterfaces: 4,
				NetworkCardIndex:         0,
			},
		},
		HypervisorType: "nitro",
		IsBareMetal:    false,
	})
	assert.True(t, ok)
}
