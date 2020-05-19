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
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/session"
	ec2svc "github.com/aws/aws-sdk-go/service/ec2"
)

type EC2 interface {
	CreateNetworkInterfaceWithContext(ctx aws.Context, input *ec2svc.CreateNetworkInterfaceInput, opts ...request.Option) (*ec2svc.CreateNetworkInterfaceOutput, error)
	DescribeInstancesWithContext(ctx aws.Context, input *ec2svc.DescribeInstancesInput, opts ...request.Option) (*ec2svc.DescribeInstancesOutput, error)
	DescribeInstanceTypesWithContext(ctx aws.Context, input *ec2svc.DescribeInstanceTypesInput, opts ...request.Option) (*ec2svc.DescribeInstanceTypesOutput, error)
	AttachNetworkInterfaceWithContext(ctx aws.Context, input *ec2svc.AttachNetworkInterfaceInput, opts ...request.Option) (*ec2svc.AttachNetworkInterfaceOutput, error)
	DeleteNetworkInterfaceWithContext(ctx aws.Context, input *ec2svc.DeleteNetworkInterfaceInput, opts ...request.Option) (*ec2svc.DeleteNetworkInterfaceOutput, error)
	DetachNetworkInterfaceWithContext(ctx aws.Context, input *ec2svc.DetachNetworkInterfaceInput, opts ...request.Option) (*ec2svc.DetachNetworkInterfaceOutput, error)
	AssignPrivateIpAddressesWithContext(ctx aws.Context, input *ec2svc.AssignPrivateIpAddressesInput, opts ...request.Option) (*ec2svc.AssignPrivateIpAddressesOutput, error)
	UnassignPrivateIpAddressesWithContext(ctx aws.Context, input *ec2svc.UnassignPrivateIpAddressesInput, opts ...request.Option) (*ec2svc.UnassignPrivateIpAddressesOutput, error)
	DescribeNetworkInterfacesWithContext(ctx aws.Context, input *ec2svc.DescribeNetworkInterfacesInput, opts ...request.Option) (*ec2svc.DescribeNetworkInterfacesOutput, error)
	ModifyNetworkInterfaceAttributeWithContext(ctx aws.Context, input *ec2svc.ModifyNetworkInterfaceAttributeInput, opts ...request.Option) (*ec2svc.ModifyNetworkInterfaceAttributeOutput, error)
	CreateTagsWithContext(ctx aws.Context, input *ec2svc.CreateTagsInput, opts ...request.Option) (*ec2svc.CreateTagsOutput, error)
}

func New(sess *session.Session) EC2 {
	return ec2svc.New(sess)
}
