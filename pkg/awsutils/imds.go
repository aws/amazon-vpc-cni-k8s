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
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/aws/smithy-go"

	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/feature/ec2/imds"
	"github.com/pkg/errors"
)

// EC2MetadataIface is a subset of the EC2Metadata API.
type EC2MetadataIface interface {
	GetMetadata(ctx context.Context, params *imds.GetMetadataInput, optFns ...func(*imds.Options)) (*imds.GetMetadataOutput, error)
}

// TypedIMDS is a typed wrapper around raw untyped IMDS SDK API.
type TypedIMDS struct {
	EC2MetadataIface
}

// imdsRequestError to provide the caller on the request status
type imdsRequestError struct {
	requestKey string
	err        error
}

var _ error = &imdsRequestError{}

func newIMDSRequestError(requestKey string, err error) *imdsRequestError {
	return &imdsRequestError{
		requestKey: requestKey,
		err:        err,
	}
}

func (e *imdsRequestError) Error() string {
	return fmt.Sprintf("failed to retrieve %s from instance metadata %v", e.requestKey, e.err)
}

func (typedimds TypedIMDS) getList(ctx context.Context, key string) ([]string, error) {
	output, err := typedimds.GetMetadata(ctx, &imds.GetMetadataInput{
		Path: key,
	})
	if err != nil {
		return nil, err
	}
	if output == nil || output.Content == nil {
		return nil, newIMDSRequestError(key, fmt.Errorf("empty response"))
	}

	// Read the content
	defer output.Content.Close()
	bytes, err := io.ReadAll(output.Content)
	if err != nil {
		return nil, newIMDSRequestError(key, fmt.Errorf("failed to read content: %w", err))
	}

	return strings.Fields(string(bytes)), nil
}

// GetAZ returns the Availability Zone in which the instance launched.
func (typedimds TypedIMDS) GetAZ(ctx context.Context) (string, error) {
	output, err := typedimds.GetMetadata(ctx, &imds.GetMetadataInput{
		Path: "placement/availability-zone"})
	if err != nil {
		return "", err
	}
	if output == nil || output.Content == nil {
		return "", newIMDSRequestError("placement/availability-zone", fmt.Errorf("empty response"))
	}
	// Read the content
	defer output.Content.Close()
	bytes, err := io.ReadAll(output.Content)
	if err != nil {
		return "", newIMDSRequestError("placement/availability-zone", fmt.Errorf("failed to read content: %w", err))
	}
	return strings.TrimSpace(string(bytes)), nil
}

// GetInstanceType returns the type of this instance.
func (typedimds TypedIMDS) GetInstanceType(ctx context.Context) (string, error) {
	output, err := typedimds.GetMetadata(ctx, &imds.GetMetadataInput{
		Path: "instance-type"})
	if err != nil {
		return "", err
	}
	if output == nil || output.Content == nil {
		return "", newIMDSRequestError("instance-type", fmt.Errorf("empty response"))
	}
	// Read the content
	defer output.Content.Close()
	bytes, err := io.ReadAll(output.Content)
	if err != nil {
		return "", newIMDSRequestError("instance-type", fmt.Errorf("failed to read content: %w", err))
	}
	return strings.TrimSpace(string(bytes)), nil
}

// GetLocalIPv4 returns the private (primary) IPv4 address of the instance.
func (typedimds TypedIMDS) GetLocalIPv4(ctx context.Context) (net.IP, error) {
	return typedimds.getIP(ctx, "local-ipv4")
}

// GetInstanceID returns the ID of this instance.
func (typedimds TypedIMDS) GetInstanceID(ctx context.Context) (string, error) {
	output, err := typedimds.GetMetadata(ctx, &imds.GetMetadataInput{
		Path: "instance-id"})
	if err != nil {
		return "", err
	}
	if output == nil || output.Content == nil {
		return "", newIMDSRequestError("instance-id", fmt.Errorf("empty response"))
	}
	// Read the content
	defer output.Content.Close()
	bytes, err := io.ReadAll(output.Content)
	if err != nil {
		return "", newIMDSRequestError("instance-id", fmt.Errorf("failed to read content: %w", err))
	}
	return strings.TrimSpace(string(bytes)), nil
}

// GetMAC returns the first/primary network interface mac address.
func (typedimds TypedIMDS) GetMAC(ctx context.Context) (string, error) {
	output, err := typedimds.GetMetadata(ctx, &imds.GetMetadataInput{
		Path: "mac"})
	if err != nil {
		return "", err
	}
	if output == nil || output.Content == nil {
		return "", newIMDSRequestError("mac", fmt.Errorf("empty response"))
	}
	// Read the content
	defer output.Content.Close()
	bytes, err := io.ReadAll(output.Content)
	if err != nil {
		return "", newIMDSRequestError("mac", fmt.Errorf("failed to read content: %w", err))
	}
	return string(bytes), nil
}

// GetMACs returns the interface addresses attached to the instance.
func (typedimds TypedIMDS) GetMACs(ctx context.Context) ([]string, error) {
	list, err := typedimds.getList(ctx, "network/interfaces/macs")
	if err != nil {
		if imdsErr, ok := err.(*imdsRequestError); ok {
			log.Warnf("%v", err)
			return nil, imdsErr.err
		}
		return nil, err
	}
	// Remove trailing /
	for i, item := range list {
		list[i] = strings.TrimSuffix(item, "/")
	}
	return list, err
}

// GetMACImdsFields returns the imds fields present for a MAC
func (typedimds TypedIMDS) GetMACImdsFields(ctx context.Context, mac string) ([]string, error) {
	key := fmt.Sprintf("network/interfaces/macs/%s", mac)
	list, err := typedimds.getList(ctx, key)
	if err != nil {
		if imdsErr, ok := err.(*imdsRequestError); ok {
			log.Warnf("%v", err)
			return nil, imdsErr.err
		}
		return nil, err
	}
	// Remove trailing /
	for i, item := range list {
		list[i] = strings.TrimSuffix(item, "/")
	}
	return list, err
}

// GetInterfaceID returns the ID of the network interface.
func (typedimds TypedIMDS) GetInterfaceID(ctx context.Context, mac string) (string, error) {
	key := fmt.Sprintf("network/interfaces/macs/%s/interface-id", mac)
	output, err := typedimds.GetMetadata(ctx, &imds.GetMetadataInput{
		Path: key})
	if err != nil {
		return "", err
	}
	if output == nil || output.Content == nil {
		return "", newIMDSRequestError(key, fmt.Errorf("empty response"))
	}
	// Read the content
	defer output.Content.Close()
	bytes, err := io.ReadAll(output.Content)
	if err != nil {
		return "", newIMDSRequestError(key, fmt.Errorf("failed to read content: %w", err))
	}
	return string(bytes), nil
}

func (typedimds TypedIMDS) getInt(ctx context.Context, key string) (int, error) {
	output, err := typedimds.GetMetadata(ctx, &imds.GetMetadataInput{
		Path: key})
	if err != nil {
		return 0, err
	}
	if output == nil || output.Content == nil {
		return 0, newIMDSRequestError(key, fmt.Errorf("empty response"))
	}
	// Read the content
	defer output.Content.Close()
	bytes, err := io.ReadAll(output.Content)
	if err != nil {
		return 0, newIMDSRequestError(key, fmt.Errorf("failed to read content: %w", err))
	}
	dataInt, err := strconv.Atoi(strings.TrimSpace(string(bytes)))
	if err != nil {
		return 0, err
	}
	return dataInt, err
}

// GetDeviceNumber returns the unique device number associated with an interface.  The primary interface is 0.
func (typedimds TypedIMDS) GetDeviceNumber(ctx context.Context, mac string) (int, error) {
	key := fmt.Sprintf("network/interfaces/macs/%s/device-number", mac)
	return typedimds.getInt(ctx, key)
}

// GetSubnetID returns the ID of the subnet in which the interface resides.
func (typedimds TypedIMDS) GetSubnetID(ctx context.Context, mac string) (string, error) {
	key := fmt.Sprintf("network/interfaces/macs/%s/subnet-id", mac)
	output, err := typedimds.GetMetadata(ctx, &imds.GetMetadataInput{
		Path: key,
	})

	// Read the content first, even if there's an error
	var subnetID string
	if output != nil && output.Content != nil {
		defer output.Content.Close()
		bytes, readErr := io.ReadAll(output.Content)
		if readErr == nil {
			subnetID = string(bytes)
		}
	}

	// Now handle any errors, but return subnetID if it was read
	if err != nil {
		if imdsErr, ok := err.(*imdsRequestError); ok {
			log.Warnf("Warning: %v", err)
			return subnetID, imdsErr.err
		}
		return "", err
	}

	return subnetID, nil
}

func (typedimds TypedIMDS) GetVpcID(ctx context.Context, mac string) (string, error) {
	key := fmt.Sprintf("network/interfaces/macs/%s/vpc-id", mac)
	output, err := typedimds.GetMetadata(ctx, &imds.GetMetadataInput{
		Path: key,
	})

	// Read the content first, even if there's an error
	var vpcID string
	if output != nil && output.Content != nil {
		defer output.Content.Close()
		bytes, readErr := io.ReadAll(output.Content)
		if readErr == nil {
			vpcID = string(bytes)
		}
	}

	// Handle errors but preserve any partial vpcID data
	if err != nil {
		if imdsErr, ok := err.(*imdsRequestError); ok {
			log.Warnf("Warning: %v", err)
			return vpcID, imdsErr.err
		}
		return "", err
	}

	return vpcID, nil
}

// GetSecurityGroupIDs returns the IDs of the security groups to which the network interface belongs.
func (typedimds TypedIMDS) GetSecurityGroupIDs(ctx context.Context, mac string) ([]string, error) {
	key := fmt.Sprintf("network/interfaces/macs/%s/security-group-ids", mac)
	sgs, err := typedimds.getList(ctx, key)
	if err != nil {
		if imdsErr, ok := err.(*imdsRequestError); ok {
			log.Warnf("%v", err)
			return sgs, imdsErr.err
		}
		return nil, err
	}
	return sgs, err
}

func (typedimds TypedIMDS) getIP(ctx context.Context, key string) (net.IP, error) {
	output, err := typedimds.GetMetadata(ctx, &imds.GetMetadataInput{
		Path: key})
	if err != nil {
		return nil, err
	}
	if output == nil || output.Content == nil {
		return nil, newIMDSRequestError(key, fmt.Errorf("empty response"))
	}
	// Read the content
	defer output.Content.Close()
	bytes, err := io.ReadAll(output.Content)
	if err != nil {
		return nil, newIMDSRequestError(key, fmt.Errorf("failed to read content: %w", err))
	}
	ip := net.ParseIP(strings.TrimSpace(string(bytes)))
	if ip == nil {
		err = &net.ParseError{Type: "IP address", Text: string(bytes)}
	}
	return ip, err
}

func (typedimds TypedIMDS) getIPs(ctx context.Context, key string) ([]net.IP, error) {
	list, err := typedimds.getList(ctx, key)
	if err != nil {
		return nil, err
	}

	ips := make([]net.IP, len(list))
	for i, item := range list {
		ip := net.ParseIP(item)
		if ip == nil {
			err = &net.ParseError{Type: "IP address", Text: item}
			return nil, err
		}
		ips[i] = ip
	}
	return ips, err
}

func (typedimds TypedIMDS) getCIDR(ctx context.Context, key string) (net.IPNet, error) {
	output, err := typedimds.GetMetadata(ctx, &imds.GetMetadataInput{
		Path: key})
	if err != nil {
		return net.IPNet{}, err
	}
	if output == nil || output.Content == nil {
		return net.IPNet{}, newIMDSRequestError(key, fmt.Errorf("empty response"))
	}
	// Read the content
	defer output.Content.Close()
	bytes, err := io.ReadAll(output.Content)
	if err != nil {
		return net.IPNet{}, newIMDSRequestError(key, fmt.Errorf("failed to read content: %w", err))
	}

	data := strings.TrimSpace(string(bytes))
	ip, network, err := net.ParseCIDR(data)
	if err != nil {
		return net.IPNet{}, err
	}
	// Why doesn't net.ParseCIDR just return values in this form?
	cidr := net.IPNet{IP: ip, Mask: network.Mask}
	return cidr, err
}

func (typedimds TypedIMDS) getCIDRs(ctx context.Context, key string) ([]net.IPNet, error) {
	list, err := typedimds.getList(ctx, key)
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
func (typedimds TypedIMDS) GetLocalIPv4s(ctx context.Context, mac string) ([]net.IP, error) {
	key := fmt.Sprintf("network/interfaces/macs/%s/local-ipv4s", mac)
	ips, err := typedimds.getIPs(ctx, key)
	if err != nil {
		if imdsErr, ok := err.(*imdsRequestError); ok {
			log.Warnf("%v", err)
			return nil, imdsErr.err
		}
		return nil, err
	}
	return ips, err
}

// GetIPv4Prefixes returns the IPv4 prefixes delegated to this interface
func (typedimds TypedIMDS) GetIPv4Prefixes(ctx context.Context, mac string) ([]net.IPNet, error) {
	key := fmt.Sprintf("network/interfaces/macs/%s/ipv4-prefix", mac)
	prefixes, err := typedimds.getCIDRs(ctx, key)

	if err != nil {
		if imdsErr, ok := err.(*imdsRequestError); ok {
			if IsNotFound(imdsErr.err) {
				return nil, nil
			}
			log.Warnf("%v", err)
			return nil, imdsErr.err
		}
		return nil, err
	}
	return prefixes, err
}

// GetIPv6Prefixes returns the IPv6 prefixes delegated to this interface
func (typedimds TypedIMDS) GetIPv6Prefixes(ctx context.Context, mac string) ([]net.IPNet, error) {
	key := fmt.Sprintf("network/interfaces/macs/%s/ipv6-prefix", mac)
	prefixes, err := typedimds.getCIDRs(ctx, key)
	if err != nil {
		if imdsErr, ok := err.(*imdsRequestError); ok {
			if IsNotFound(imdsErr.err) {
				return nil, nil
			}
			log.Warnf("%v", err)
			return nil, imdsErr.err
		}
		return nil, err
	}
	return prefixes, err
}

// GetIPv6s returns the IPv6 addresses associated with the interface.
func (typedimds TypedIMDS) GetIPv6s(ctx context.Context, mac string) ([]net.IP, error) {
	key := fmt.Sprintf("network/interfaces/macs/%s/ipv6s", mac)
	ips, err := typedimds.getIPs(ctx, key)
	if err != nil {
		if imdsErr, ok := err.(*imdsRequestError); ok {
			if IsNotFound(imdsErr.err) {
				// No IPv6.  Not an error, just a disappointment :(
				return nil, nil
			}
			log.Warnf("%v", err)
			return nil, imdsErr.err
		}
		return nil, err
	}
	return ips, err
}

// GetSubnetIPv4CIDRBlock returns the IPv4 CIDR block for the subnet in which the interface resides.
func (typedimds TypedIMDS) GetSubnetIPv4CIDRBlock(ctx context.Context, mac string) (net.IPNet, error) {
	key := fmt.Sprintf("network/interfaces/macs/%s/subnet-ipv4-cidr-block", mac)
	return typedimds.getCIDR(ctx, key)
}

// GetVPCIPv4CIDRBlocks returns the IPv4 CIDR blocks for the VPC.
func (typedimds TypedIMDS) GetVPCIPv4CIDRBlocks(ctx context.Context, mac string) ([]net.IPNet, error) {
	key := fmt.Sprintf("network/interfaces/macs/%s/vpc-ipv4-cidr-blocks", mac)
	cidrs, err := typedimds.getCIDRs(ctx, key)
	if err != nil {
		if imdsErr, ok := err.(*imdsRequestError); ok {
			log.Warnf("%v", err)
			return cidrs, imdsErr.err
		}
		return nil, err
	}
	return cidrs, err
}

// GetVPCIPv6CIDRBlocks returns the IPv6 CIDR blocks for the VPC.
func (typedimds TypedIMDS) GetVPCIPv6CIDRBlocks(ctx context.Context, mac string) ([]net.IPNet, error) {
	key := fmt.Sprintf("network/interfaces/macs/%s/vpc-ipv6-cidr-blocks", mac)
	ipnets, err := typedimds.getCIDRs(ctx, key)
	if err != nil {
		if imdsErr, ok := err.(*imdsRequestError); ok {
			if IsNotFound(imdsErr.err) {
				// No IPv6.  Not an error, just a disappointment :(
				return nil, nil
			}
			log.Warnf("%v", err)
			return nil, imdsErr.err
		}
		return nil, nil
	}
	return ipnets, err
}

// GetSubnetIPv6CIDRBlocks returns the IPv6 CIDR block for the subnet in which the interface resides.
func (typedimds TypedIMDS) GetSubnetIPv6CIDRBlocks(ctx context.Context, mac string) (net.IPNet, error) {
	key := fmt.Sprintf("network/interfaces/macs/%s/subnet-ipv6-cidr-blocks", mac)
	return typedimds.getCIDR(ctx, key)
}

// IsNotFound returns true if the error was caused by an AWS API 404 response.
// We implement a Custom IMDS Error, so need to use APIError instead of HTTP Response Error
func IsNotFound(err error) bool {
	log.Warnf("IsNotFound 1: %v", err)
	if err == nil {
		return false
	}

	var re *awshttp.ResponseError
	var oe *smithy.OperationError
	var ae smithy.APIError
	if errors.As(err, &oe) {
		log.Warnf("IsNotFound 2: failed to call service: %s, operation: %s, error: %v", oe.Service(), oe.Operation(), oe.Unwrap())
	}
	if errors.As(err, &ae) {
		log.Warnf("IsNotFound 3: code: %s, message: %s, fault: %s", ae.ErrorCode(), ae.ErrorMessage(), ae.ErrorFault().String())
	}

	if errors.As(err, &re) {
		log.Warnf("IsNotFound: %v %d", re, re.Response.StatusCode)
		return re.Response.StatusCode == http.StatusNotFound
	}

	return false
}

// FakeIMDS is a trivial implementation of EC2MetadataIface using an in-memory map - for testing.
type FakeIMDS map[string]interface{}

// Custom error type
type CustomRequestFailure struct {
	code       string
	message    string
	fault      smithy.ErrorFault
	statusCode int
	requestID  string
}

func (e *CustomRequestFailure) Error() string {
	return fmt.Sprintf("%s: %s", e.code, e.message)
}

func (e *CustomRequestFailure) ErrorCode() string {
	return e.code
}

func (e *CustomRequestFailure) ErrorMessage() string {
	return e.message
}

func (e *CustomRequestFailure) ErrorFault() smithy.ErrorFault {
	return e.fault
}

func (e *CustomRequestFailure) HTTPStatusCode() int {
	return e.statusCode
}

func (e *CustomRequestFailure) RequestID() string {
	return e.requestID
}

// GetMetadataWithContext implements the EC2MetadataIface interface.
func (f FakeIMDS) GetMetadataWithContext(ctx context.Context, p string) (string, error) {
	result, ok := f[p]
	if !ok {
		result, ok = f[p+"/"] // Metadata API treats foo/ as foo
	}
	if !ok {
		notFoundErr := &CustomRequestFailure{
			code:       "NotFound",
			message:    "not found",
			fault:      smithy.FaultUnknown,
			statusCode: http.StatusNotFound,
			requestID:  "dummy-reqid",
		}
		return "", newIMDSRequestError(p, notFoundErr)
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
