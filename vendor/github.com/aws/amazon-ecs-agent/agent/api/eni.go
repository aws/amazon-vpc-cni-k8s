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
	"fmt"
	"strings"

	"github.com/aws/amazon-ecs-agent/agent/acs/model/ecsacs"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/pkg/errors"
)

// ENI contains information of the eni
type ENI struct {
	// ID is the id of eni
	ID string `json:"ec2Id"`
	// IPV4Addresses is the ipv4 address associated with the eni
	IPV4Addresses []*ENIIPV4Address
	// IPV6Addresses is the ipv6 address associated with the eni
	IPV6Addresses []*ENIIPV6Address
	// MacAddress is the mac address of the eni
	MacAddress string
	// DomainNameServers specifies the nameserver IP addresses for
	// the eni
	DomainNameServers []string `json:"omitempty"`
	// DomainNameSearchList specifies the search list for the domain
	// name lookup, for the eni
	DomainNameSearchList []string `json:"omitempty"`
}

// String returns a human readable version of the ENI object
func (eni *ENI) String() string {
	var ipv4Addresses []string
	for _, addr := range eni.IPV4Addresses {
		ipv4Addresses = append(ipv4Addresses, addr.Address)
	}
	var ipv6Addresses []string
	for _, addr := range eni.IPV6Addresses {
		ipv6Addresses = append(ipv6Addresses, addr.Address)
	}
	return fmt.Sprintf(
		"eni id:%s, mac: %s, ipv4addresses: [%s], ipv6addresses: [%s], dns: [%s], dns search: [%s]",
		eni.ID, eni.MacAddress, strings.Join(ipv4Addresses, ","), strings.Join(ipv6Addresses, ","),
		strings.Join(eni.DomainNameServers, ","), strings.Join(eni.DomainNameSearchList, ","))
}

// ENIIPV4Address is the ipv4 information of the eni
type ENIIPV4Address struct {
	// Primary indicates whether the ip address is primary
	Primary bool
	// Address is the ipv4 address associated with eni
	Address string
}

// ENIIPV6Address is the ipv6 information of the eni
type ENIIPV6Address struct {
	// Address is the ipv6 address associated with eni
	Address string
}

// ENIFromACS validates the information from acs message and create the ENI object
func ENIFromACS(acsenis []*ecsacs.ElasticNetworkInterface) (*ENI, error) {
	err := ValidateTaskENI(acsenis)
	if err != nil {
		return nil, err
	}

	var ipv4 []*ENIIPV4Address
	var ipv6 []*ENIIPV6Address

	// Read ipv4 address information of the eni
	for _, ec2Ipv4 := range acsenis[0].Ipv4Addresses {
		ipv4 = append(ipv4, &ENIIPV4Address{
			Primary: aws.BoolValue(ec2Ipv4.Primary),
			Address: aws.StringValue(ec2Ipv4.PrivateAddress),
		})
	}

	// Read ipv6 address information of the eni
	for _, ec2Ipv6 := range acsenis[0].Ipv6Addresses {
		ipv6 = append(ipv6, &ENIIPV6Address{
			Address: aws.StringValue(ec2Ipv6.Address),
		})
	}

	eni := &ENI{
		ID:            aws.StringValue(acsenis[0].Ec2Id),
		IPV4Addresses: ipv4,
		IPV6Addresses: ipv6,
		MacAddress:    aws.StringValue(acsenis[0].MacAddress),
	}
	for _, nameserverIP := range acsenis[0].DomainNameServers {
		eni.DomainNameServers = append(eni.DomainNameServers, aws.StringValue(nameserverIP))
	}
	for _, nameserverDomain := range acsenis[0].DomainName {
		eni.DomainNameSearchList = append(eni.DomainNameSearchList, aws.StringValue(nameserverDomain))
	}

	return eni, nil
}

// ValidateTaskENI validates the eni informaiton sent from acs
func ValidateTaskENI(acsenis []*ecsacs.ElasticNetworkInterface) error {
	// Only one eni should be associated with the task
	// Only one ipv4 should be associated with the eni
	// No more than one ipv6 should be associated with the eni
	if len(acsenis) != 1 {
		return errors.Errorf("eni message validation: more than one ENIs in the message(%d)", len(acsenis))
	} else if len(acsenis[0].Ipv4Addresses) != 1 {
		return errors.Errorf("eni message validation: more than one ipv4 addresses in the message(%d)", len(acsenis[0].Ipv4Addresses))
	} else if len(acsenis[0].Ipv6Addresses) > 1 {
		return errors.Errorf("eni message validation: more than one ipv6 addresses in the message(%d)", len(acsenis[0].Ipv6Addresses))
	}

	if acsenis[0].MacAddress == nil {
		return errors.Errorf("eni message validation: empty eni mac address in the message")
	}

	if acsenis[0].Ec2Id == nil {
		return errors.Errorf("eni message validation: empty eni id in the message")
	}

	return nil
}
