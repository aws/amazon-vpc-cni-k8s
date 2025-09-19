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

package ec2wrapper

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
)

// EC2 is the EC2 wrapper interface
type EC2 interface {
	CreateNetworkInterface(ctx context.Context, input *ec2.CreateNetworkInterfaceInput, opts ...func(*ec2.Options)) (*ec2.CreateNetworkInterfaceOutput, error)
	DescribeInstances(ctx context.Context, input *ec2.DescribeInstancesInput, opts ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error)
	DescribeInstanceTypes(ctx context.Context, input *ec2.DescribeInstanceTypesInput, opts ...func(*ec2.Options)) (*ec2.DescribeInstanceTypesOutput, error)
	AttachNetworkInterface(ctx context.Context, input *ec2.AttachNetworkInterfaceInput, opts ...func(*ec2.Options)) (*ec2.AttachNetworkInterfaceOutput, error)
	DeleteNetworkInterface(ctx context.Context, input *ec2.DeleteNetworkInterfaceInput, opts ...func(*ec2.Options)) (*ec2.DeleteNetworkInterfaceOutput, error)
	DetachNetworkInterface(ctx context.Context, input *ec2.DetachNetworkInterfaceInput, opts ...func(*ec2.Options)) (*ec2.DetachNetworkInterfaceOutput, error)
	AssignPrivateIpAddresses(ctx context.Context, input *ec2.AssignPrivateIpAddressesInput, opts ...func(*ec2.Options)) (*ec2.AssignPrivateIpAddressesOutput, error)
	UnassignPrivateIpAddresses(ctx context.Context, input *ec2.UnassignPrivateIpAddressesInput, opts ...func(*ec2.Options)) (*ec2.UnassignPrivateIpAddressesOutput, error)
	AssignIpv6Addresses(ctx context.Context, input *ec2.AssignIpv6AddressesInput, opts ...func(*ec2.Options)) (*ec2.AssignIpv6AddressesOutput, error)
	UnassignIpv6Addresses(ctx context.Context, input *ec2.UnassignIpv6AddressesInput, opts ...func(*ec2.Options)) (*ec2.UnassignIpv6AddressesOutput, error)
	DescribeNetworkInterfaces(ctx context.Context, input *ec2.DescribeNetworkInterfacesInput, opts ...func(*ec2.Options)) (*ec2.DescribeNetworkInterfacesOutput, error)
	ModifyNetworkInterfaceAttribute(ctx context.Context, input *ec2.ModifyNetworkInterfaceAttributeInput, opts ...func(*ec2.Options)) (*ec2.ModifyNetworkInterfaceAttributeOutput, error)
	CreateTags(ctx context.Context, input *ec2.CreateTagsInput, opts ...func(*ec2.Options)) (*ec2.CreateTagsOutput, error)
	DescribeSubnets(ctx context.Context, input *ec2.DescribeSubnetsInput, opts ...func(*ec2.Options)) (*ec2.DescribeSubnetsOutput, error)
	DescribeSecurityGroups(ctx context.Context, input *ec2.DescribeSecurityGroupsInput, opts ...func(*ec2.Options)) (*ec2.DescribeSecurityGroupsOutput, error)
}

// New creates a new EC2 wrapper
func New(cfg aws.Config) *ec2.Client {
	return ec2.NewFromConfig(cfg)
}
