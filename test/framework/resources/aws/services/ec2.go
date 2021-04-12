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

package services

import (
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"
)

type EC2 interface {
	DescribeInstanceType(instanceType string) ([]*ec2.InstanceTypeInfo, error)
	DescribeInstance(instanceID string) (*ec2.Instance, error)
	AuthorizeSecurityGroupIngress(groupID string, protocol string, fromPort int, toPort int, cidrIP string) error
	RevokeSecurityGroupIngress(groupID string, protocol string, fromPort int, toPort int, cidrIP string) error
	AuthorizeSecurityGroupEgress(groupID string, protocol string, fromPort int, toPort int, cidrIP string) error
	RevokeSecurityGroupEgress(groupID string, protocol string, fromPort int, toPort int, cidrIP string) error
}

type defaultEC2 struct {
	ec2iface.EC2API
}

func (d *defaultEC2) DescribeInstanceType(instanceType string) ([]*ec2.InstanceTypeInfo, error) {
	describeInstanceTypeIp := &ec2.DescribeInstanceTypesInput{
		InstanceTypes: aws.StringSlice([]string{instanceType}),
	}
	describeInstanceOp, err := d.EC2API.DescribeInstanceTypes(describeInstanceTypeIp)
	if err != nil {
		return nil, err
	}
	if len(describeInstanceOp.InstanceTypes) == 0 {
		return nil, fmt.Errorf("no instance type found in the output %s", instanceType)
	}
	return describeInstanceOp.InstanceTypes, nil
}

func (d *defaultEC2) DescribeInstance(instanceID string) (*ec2.Instance, error) {
	describeInstanceInput := &ec2.DescribeInstancesInput{
		InstanceIds: aws.StringSlice([]string{instanceID}),
	}
	describeInstanceOutput, err := d.EC2API.DescribeInstances(describeInstanceInput)
	if err != nil {
		return nil, err
	}
	if describeInstanceOutput == nil || len(describeInstanceOutput.Reservations) == 0 ||
		len(describeInstanceOutput.Reservations[0].Instances) == 0 {
		return nil, fmt.Errorf("failed to find instance %s", instanceID)
	}
	return describeInstanceOutput.Reservations[0].Instances[0], nil
}

func (d *defaultEC2) AuthorizeSecurityGroupIngress(groupID string, protocol string,
	fromPort int, toPort int, cidrIP string) error {
	ipPermissions := &ec2.IpPermission{
		FromPort:   aws.Int64(int64(fromPort)),
		ToPort:     aws.Int64(int64(toPort)),
		IpProtocol: aws.String(protocol),
		IpRanges: []*ec2.IpRange{
			{
				CidrIp: aws.String(cidrIP),
			},
		},
	}
	authorizeSecurityGroupIngressInput := &ec2.AuthorizeSecurityGroupIngressInput{
		GroupId:       aws.String(groupID),
		IpPermissions: []*ec2.IpPermission{ipPermissions},
	}
	_, err := d.EC2API.AuthorizeSecurityGroupIngress(authorizeSecurityGroupIngressInput)
	return err
}

func (d *defaultEC2) RevokeSecurityGroupIngress(groupID string, protocol string, fromPort int, toPort int, cidrIP string) error {
	ipPermissions := &ec2.IpPermission{
		FromPort:   aws.Int64(int64(fromPort)),
		ToPort:     aws.Int64(int64(toPort)),
		IpProtocol: aws.String(protocol),
		IpRanges: []*ec2.IpRange{
			{
				CidrIp: aws.String(cidrIP),
			},
		},
	}
	revokeSecurityGroupIngressInput := &ec2.RevokeSecurityGroupIngressInput{
		GroupId:       aws.String(groupID),
		IpPermissions: []*ec2.IpPermission{ipPermissions},
	}
	_, err := d.EC2API.RevokeSecurityGroupIngress(revokeSecurityGroupIngressInput)
	return err
}

func (d *defaultEC2) AuthorizeSecurityGroupEgress(groupID string, protocol string, fromPort int, toPort int, cidrIP string) error {
	ipPermissions := &ec2.IpPermission{
		FromPort:   aws.Int64(int64(fromPort)),
		ToPort:     aws.Int64(int64(toPort)),
		IpProtocol: aws.String(protocol),
		IpRanges: []*ec2.IpRange{
			{
				CidrIp: aws.String(cidrIP),
			},
		},
	}
	authorizeSecurityGroupEgressInput := &ec2.AuthorizeSecurityGroupEgressInput{
		GroupId:       aws.String(groupID),
		IpPermissions: []*ec2.IpPermission{ipPermissions},
	}
	_, err := d.EC2API.AuthorizeSecurityGroupEgress(authorizeSecurityGroupEgressInput)
	return err
}

func (d *defaultEC2) RevokeSecurityGroupEgress(groupID string, protocol string, fromPort int, toPort int, cidrIP string) error {
	ipPermissions := &ec2.IpPermission{
		FromPort:   aws.Int64(int64(fromPort)),
		ToPort:     aws.Int64(int64(toPort)),
		IpProtocol: aws.String(protocol),
		IpRanges: []*ec2.IpRange{
			{
				CidrIp: aws.String(cidrIP),
			},
		},
	}
	revokeSecurityGroupEgressInput := &ec2.RevokeSecurityGroupEgressInput{
		GroupId:       aws.String(groupID),
		IpPermissions: []*ec2.IpPermission{ipPermissions},
	}
	_, err := d.EC2API.RevokeSecurityGroupEgress(revokeSecurityGroupEgressInput)
	return err
}

func NewEC2(session *session.Session) EC2 {
	return &defaultEC2{
		EC2API: ec2.New(session),
	}
}
