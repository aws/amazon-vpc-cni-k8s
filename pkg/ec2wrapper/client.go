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

package ec2wrapper

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/session"
	ec2svc "github.com/aws/aws-sdk-go/service/ec2"
)

type EC2 interface {
	CreateNetworkInterface(input *ec2svc.CreateNetworkInterfaceInput) (*ec2svc.CreateNetworkInterfaceOutput, error)
	DescribeInstances(input *ec2svc.DescribeInstancesInput) (*ec2svc.DescribeInstancesOutput, error)
	AttachNetworkInterface(input *ec2svc.AttachNetworkInterfaceInput) (*ec2svc.AttachNetworkInterfaceOutput, error)
	DeleteNetworkInterface(input *ec2svc.DeleteNetworkInterfaceInput) (*ec2svc.DeleteNetworkInterfaceOutput, error)
	DetachNetworkInterface(input *ec2svc.DetachNetworkInterfaceInput) (*ec2svc.DetachNetworkInterfaceOutput, error)
	AssignPrivateIpAddresses(input *ec2svc.AssignPrivateIpAddressesInput) (*ec2svc.AssignPrivateIpAddressesOutput, error)
	UnassignPrivateIpAddressesWithContext(ctx aws.Context, input *ec2svc.UnassignPrivateIpAddressesInput, opts ...request.Option) (*ec2svc.UnassignPrivateIpAddressesOutput, error)
	DescribeNetworkInterfaces(input *ec2svc.DescribeNetworkInterfacesInput) (*ec2svc.DescribeNetworkInterfacesOutput, error)
	ModifyNetworkInterfaceAttribute(input *ec2svc.ModifyNetworkInterfaceAttributeInput) (*ec2svc.ModifyNetworkInterfaceAttributeOutput, error)
	CreateTags(input *ec2svc.CreateTagsInput) (*ec2svc.CreateTagsOutput, error)
}

func New(sess *session.Session) EC2 {
	return ec2svc.New(sess)
}
