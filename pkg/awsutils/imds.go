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

package awsutils

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/pkg/errors"
)

// EC2MetadataIface is a subset of the EC2Metadata API.
type EC2MetadataIface interface {
	GetMetadataWithContext(ctx context.Context, p string) (string, error)
}

// TypedIMDS is a typed wrapper around raw untyped IMDS SDK API.
type TypedIMDS struct {
	EC2MetadataIface
}

func (imds TypedIMDS) getList(ctx context.Context, key string) ([]string, error) {
	data, err := imds.GetMetadataWithContext(ctx, key)
	if err != nil {
		return nil, err
	}

	return strings.Fields(data), nil
}

// GetAZ returns the Availability Zone in which the instance launched.
func (imds TypedIMDS) GetAZ(ctx context.Context) (string, error) {
	return imds.GetMetadataWithContext(ctx, "placement/availability-zone")
}

// GetInstanceType returns the type of this instance.
func (imds TypedIMDS) GetInstanceType(ctx context.Context) (string, error) {
	return imds.GetMetadataWithContext(ctx, "instance-type")
}

// GetLocalIPv4 returns the private (primary) IPv4 address of the instance.
func (imds TypedIMDS) GetLocalIPv4(ctx context.Context) (net.IP, error) {
	return imds.getIP(ctx, "local-ipv4")
}

// GetInstanceID returns the ID of this instance.
func (imds TypedIMDS) GetInstanceID(ctx context.Context) (string, error) {
	return imds.GetMetadataWithContext(ctx, "instance-id")
}

// GetMAC returns the first/primary network interface mac address.
func (imds TypedIMDS) GetMAC(ctx context.Context) (string, error) {
	return imds.GetMetadataWithContext(ctx, "mac")
}

// GetMACs returns the interface addresses attached to the instance.
func (imds TypedIMDS) GetMACs(ctx context.Context) ([]string, error) {
	list, err := imds.getList(ctx, "network/interfaces/macs")
	if err != nil {
		return nil, err
	}
	// Remove trailing /
	for i, item := range list {
		list[i] = strings.TrimSuffix(item, "/")
	}
	return list, nil
}

// GetInterfaceID returns the ID of the network interface.
func (imds TypedIMDS) GetInterfaceID(ctx context.Context, mac string) (string, error) {
	key := fmt.Sprintf("network/interfaces/macs/%s/interface-id", mac)
	return imds.GetMetadataWithContext(ctx, key)
}

func (imds TypedIMDS) getInt(ctx context.Context, key string) (int, error) {
	data, err := imds.GetMetadataWithContext(ctx, key)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(data)
}

// GetDeviceNumber returns the unique device number associated with an interface.  The primary interface is 0.
func (imds TypedIMDS) GetDeviceNumber(ctx context.Context, mac string) (int, error) {
	key := fmt.Sprintf("network/interfaces/macs/%s/device-number", mac)
	return imds.getInt(ctx, key)
}

// GetSubnetID returns the ID of the subnet in which the interface resides.
func (imds TypedIMDS) GetSubnetID(ctx context.Context, mac string) (string, error) {
	key := fmt.Sprintf("network/interfaces/macs/%s/subnet-id", mac)
	return imds.GetMetadataWithContext(ctx, key)
}

// GetSecurityGroupIDs returns the IDs of the security groups to which the network interface belongs.
func (imds TypedIMDS) GetSecurityGroupIDs(ctx context.Context, mac string) ([]string, error) {
	key := fmt.Sprintf("network/interfaces/macs/%s/security-group-ids", mac)
	return imds.getList(ctx, key)
}

func (imds TypedIMDS) getIP(ctx context.Context, key string) (net.IP, error) {
	data, err := imds.GetMetadataWithContext(ctx, key)
	if err != nil {
		return nil, err
	}

	ip := net.ParseIP(data)
	if ip == nil {
		return nil, &net.ParseError{Type: "IP address", Text: data}
	}
	return ip, nil
}

func (imds TypedIMDS) getIPs(ctx context.Context, key string) ([]net.IP, error) {
	list, err := imds.getList(ctx, key)
	if err != nil {
		return nil, err
	}

	ips := make([]net.IP, len(list))
	for i, item := range list {
		ip := net.ParseIP(item)
		if ip == nil {
			return nil, &net.ParseError{Type: "IP address", Text: item}
		}
		ips[i] = ip
	}
	return ips, nil
}

func (imds TypedIMDS) getCIDR(ctx context.Context, key string) (net.IPNet, error) {
	data, err := imds.GetMetadataWithContext(ctx, key)
	if err != nil {
		return net.IPNet{}, err
	}

	ip, network, err := net.ParseCIDR(data)
	if err != nil {
		return net.IPNet{}, err
	}
	// Why doesn't net.ParseCIDR just return values in this form?
	cidr := net.IPNet{IP: ip, Mask: network.Mask}
	return cidr, nil
}

func (imds TypedIMDS) getCIDRs(ctx context.Context, key string) ([]net.IPNet, error) {
	list, err := imds.getList(ctx, key)
	if err != nil {
		return nil, err
	}

	cidrs := make([]net.IPNet, len(list))
	for i, item := range list {
		ip, network, err := net.ParseCIDR(item)
		if err != nil {
			return nil, err
		}
		// Why doesn't net.ParseCIDR just return values in this form?
		cidrs[i] = net.IPNet{IP: ip, Mask: network.Mask}
	}
	return cidrs, nil
}

// GetLocalIPv4s returns the private IPv4 addresses associated with the interface.  First returned address is the primary address.
func (imds TypedIMDS) GetLocalIPv4s(ctx context.Context, mac string) ([]net.IP, error) {
	key := fmt.Sprintf("network/interfaces/macs/%s/local-ipv4s", mac)
	return imds.getIPs(ctx, key)
}

// GetIPv6s returns the IPv6 addresses associated with the interface.
func (imds TypedIMDS) GetIPv6s(ctx context.Context, mac string) ([]net.IP, error) {
	key := fmt.Sprintf("network/interfaces/macs/%s/ipv6s", mac)
	ips, err := imds.getIPs(ctx, key)
	if IsNotFound(err) {
		// No IPv6.  Not an error, just a disappointment :(
		return nil, nil
	}
	return ips, err
}

// GetSubnetIPv4CIDRBlock returns the IPv4 CIDR block for the subnet in which the interface resides.
func (imds TypedIMDS) GetSubnetIPv4CIDRBlock(ctx context.Context, mac string) (net.IPNet, error) {
	key := fmt.Sprintf("network/interfaces/macs/%s/subnet-ipv4-cidr-block", mac)
	return imds.getCIDR(ctx, key)
}

// GetVPCIPv4CIDRBlocks returns the IPv4 CIDR blocks for the VPC.
func (imds TypedIMDS) GetVPCIPv4CIDRBlocks(ctx context.Context, mac string) ([]net.IPNet, error) {
	key := fmt.Sprintf("network/interfaces/macs/%s/vpc-ipv4-cidr-blocks", mac)
	return imds.getCIDRs(ctx, key)
}

// GetVPCIPv6CIDRBlocks returns the IPv6 CIDR blocks for the VPC.
func (imds TypedIMDS) GetVPCIPv6CIDRBlocks(ctx context.Context, mac string) ([]net.IPNet, error) {
	key := fmt.Sprintf("network/interfaces/macs/%s/vpc-ipv6-cidr-blocks", mac)
	ipnets, err := imds.getCIDRs(ctx, key)
	if IsNotFound(err) {
		// No IPv6.  Not an error, just a disappointment :(
		return nil, nil
	}
	return ipnets, err
}

// IsNotFound returns true if the error was caused by an AWS API 404 response.
func IsNotFound(err error) bool {
	if err != nil {
		var aerr awserr.RequestFailure
		if errors.As(err, &aerr) {
			return aerr.StatusCode() == http.StatusNotFound
		}
	}
	return false
}

// FakeIMDS is a trivial implementation of EC2MetadataIface using an in-memory map - for testing.
type FakeIMDS map[string]interface{}

// GetMetadataWithContext implements the EC2MetadataIface interface.
func (f FakeIMDS) GetMetadataWithContext(ctx context.Context, p string) (string, error) {
	result, ok := f[p]
	if !ok {
		result, ok = f[p+"/"] // Metadata API treats foo/ as foo
	}
	if !ok {
		notFoundErr := awserr.NewRequestFailure(awserr.New("NotFound", "not found", nil), http.StatusNotFound, "dummy-reqid")
		return "", fmt.Errorf("no test data for metadata path %s: %w", p, notFoundErr)
	}
	switch v := result.(type) {
	case string:
		return v, nil
	case error:
		return "", v
	default:
		panic(fmt.Sprintf("unknown test metadata value type %T for %s", result, p))
	}
}
