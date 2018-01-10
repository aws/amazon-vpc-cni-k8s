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

package api

import (
	"testing"

	"github.com/aws/amazon-ecs-agent/agent/acs/model/ecsacs"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/stretchr/testify/assert"
)

const (
	defaultDNS         = "169.254.169.253"
	customDNS          = "10.0.0.2"
	customSearchDomain = "us-west-2.compute.internal"
)

// TestENIFromACS tests the eni information was correctly read from the acs
func TestENIFromACS(t *testing.T) {
	acsenis := []*ecsacs.ElasticNetworkInterface{
		{
			AttachmentArn: aws.String("arn"),
			Ec2Id:         aws.String("ec2id"),
			Ipv4Addresses: []*ecsacs.IPv4AddressAssignment{
				{
					Primary:        aws.Bool(true),
					PrivateAddress: aws.String("ipv4"),
				},
			},
			Ipv6Addresses: []*ecsacs.IPv6AddressAssignment{
				{
					Address: aws.String("ipv6")},
			},
			MacAddress:        aws.String("mac"),
			DomainNameServers: []*string{aws.String(defaultDNS), aws.String(customDNS)},
			DomainName:        []*string{aws.String(customSearchDomain)},
		},
	}

	eni, err := ENIFromACS(acsenis)
	assert.NoError(t, err)
	assert.NotNil(t, eni)
	assert.Equal(t, aws.StringValue(acsenis[0].Ec2Id), eni.ID)
	assert.Equal(t, 1, len(acsenis[0].Ipv4Addresses))
	assert.Equal(t, aws.StringValue(acsenis[0].Ipv4Addresses[0].PrivateAddress), eni.IPV4Addresses[0].Address)
	assert.Equal(t, aws.BoolValue(acsenis[0].Ipv4Addresses[0].Primary), eni.IPV4Addresses[0].Primary)
	assert.Equal(t, aws.StringValue(acsenis[0].MacAddress), eni.MacAddress)
	assert.Equal(t, 1, len(acsenis[0].Ipv6Addresses))
	assert.Equal(t, aws.StringValue(acsenis[0].Ipv6Addresses[0].Address), eni.IPV6Addresses[0].Address)
	assert.Len(t, eni.DomainNameServers, 2)
	assert.Equal(t, defaultDNS, eni.DomainNameServers[0])
	assert.Equal(t, customDNS, eni.DomainNameServers[1])
	assert.Len(t, eni.DomainNameSearchList, 1)
	assert.Equal(t, customSearchDomain, eni.DomainNameSearchList[0])
}

// TestValidateENIFromACS tests the validation of enis from acs
func TestValidateENIFromACS(t *testing.T) {
	acsenis := []*ecsacs.ElasticNetworkInterface{
		{
			AttachmentArn: aws.String("arn"),
			Ec2Id:         aws.String("ec2id"),
			Ipv4Addresses: []*ecsacs.IPv4AddressAssignment{
				{
					Primary:        aws.Bool(true),
					PrivateAddress: aws.String("ipv4"),
				},
			},
			Ipv6Addresses: []*ecsacs.IPv6AddressAssignment{
				{
					Address: aws.String("ipv6")},
			},
			MacAddress: aws.String("mac"),
		},
	}

	err := ValidateTaskENI(acsenis)
	assert.NoError(t, err)

	twoenis := append(acsenis, acsenis[0])
	err = ValidateTaskENI(twoenis)
	assert.Error(t, err, "More than one eni for a task should cause error")

	acsenis[0].Ipv6Addresses = nil
	err = ValidateTaskENI(acsenis)
	assert.NoError(t, err)

	acsenis[0].Ipv4Addresses = nil
	err = ValidateTaskENI(acsenis)
	assert.Error(t, err)
}
