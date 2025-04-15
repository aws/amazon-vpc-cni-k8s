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
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
)

type EC2 interface {
	DescribeInstanceType(ctx context.Context, instanceType string) ([]types.InstanceTypeInfo, error)
	DescribeInstance(ctx context.Context, instanceID string) (types.Instance, error)
	DescribeVPC(ctx context.Context, vpcID string) (*ec2.DescribeVpcsOutput, error)
	DescribeNetworkInterface(ctx context.Context, interfaceIDs []string) (*ec2.DescribeNetworkInterfacesOutput, error)
	AuthorizeSecurityGroupIngress(ctx context.Context, groupID string, protocol string, fromPort int, toPort int, cidrIP string, sourceSG bool) error
	RevokeSecurityGroupIngress(ctx context.Context, groupID string, protocol string, fromPort int, toPort int, cidrIP string, sourceSG bool) error
	AuthorizeSecurityGroupEgress(ctx context.Context, groupID string, protocol string, fromPort int, toPort int, cidrIP string) error
	RevokeSecurityGroupEgress(ctx context.Context, groupID string, protocol string, fromPort int, toPort int, cidrIP string) error
	AssociateVPCCIDRBlock(ctx context.Context, vpcId string, cidrBlock string) (*ec2.AssociateVpcCidrBlockOutput, error)
	TerminateInstance(ctx context.Context, instanceIDs []string) error
	DisAssociateVPCCIDRBlock(ctx context.Context, associationID string) error
	DescribeSubnets(ctx context.Context, subnetIDs []string) (*ec2.DescribeSubnetsOutput, error)
	CreateSubnet(ctx context.Context, cidrBlock string, vpcID string, az string) (*ec2.CreateSubnetOutput, error)
	DeleteSubnet(ctx context.Context, subnetID string) error
	DescribeRouteTables(ctx context.Context, subnetID string) (*ec2.DescribeRouteTablesOutput, error)
	DescribeRouteTablesWithVPCID(ctx context.Context, vpcID string) (*ec2.DescribeRouteTablesOutput, error)
	CreateSecurityGroup(ctx context.Context, groupName string, description string, vpcID string) (*ec2.CreateSecurityGroupOutput, error)
	DeleteSecurityGroup(ctx context.Context, groupID string) error
	AssociateRouteTableToSubnet(ctx context.Context, routeTableId string, subnetID string) error
	CreateKey(ctx context.Context, keyName string) (*ec2.CreateKeyPairOutput, error)
	DeleteKey(ctx context.Context, keyName string) error
	DescribeKey(ctx context.Context, keyName string) (*ec2.DescribeKeyPairsOutput, error)
	ModifyNetworkInterfaceSecurityGroups(ctx context.Context, securityGroupIds []string, networkInterfaceId *string) (*ec2.ModifyNetworkInterfaceAttributeOutput, error)
	DescribeAvailabilityZones(ctx context.Context) (*ec2.DescribeAvailabilityZonesOutput, error)
	CreateTags(ctx context.Context, resourceIds []string, tags []types.Tag) (*ec2.CreateTagsOutput, error)
	DeleteTags(ctx context.Context, resourceIds []string, tags []types.Tag) (*ec2.DeleteTagsOutput, error)
}

type defaultEC2 struct {
	client *ec2.Client
}

func (d *defaultEC2) DescribeInstanceType(ctx context.Context, instanceType string) ([]types.InstanceTypeInfo, error) {
	describeInstanceTypeIp := &ec2.DescribeInstanceTypesInput{
		InstanceTypes: []types.InstanceType{types.InstanceType(instanceType)},
	}
	describeInstanceOp, err := d.client.DescribeInstanceTypes(ctx, describeInstanceTypeIp)
	if err != nil {
		return nil, err
	}
	if len(describeInstanceOp.InstanceTypes) == 0 {
		return nil, fmt.Errorf("no instance type found in the output %s", instanceType)
	}
	return describeInstanceOp.InstanceTypes, nil
}

func (d *defaultEC2) DescribeAvailabilityZones(ctx context.Context) (*ec2.DescribeAvailabilityZonesOutput, error) {
	describeAvailabilityZonesInput := &ec2.DescribeAvailabilityZonesInput{}
	return d.client.DescribeAvailabilityZones(ctx, describeAvailabilityZonesInput)
}

func (d *defaultEC2) ModifyNetworkInterfaceSecurityGroups(ctx context.Context, securityGroupIds []string, networkInterfaceId *string) (*ec2.ModifyNetworkInterfaceAttributeOutput, error) {
	return d.client.ModifyNetworkInterfaceAttribute(ctx, &ec2.ModifyNetworkInterfaceAttributeInput{
		NetworkInterfaceId: networkInterfaceId,
		Groups:             securityGroupIds,
	})
}

func (d *defaultEC2) DescribeInstance(ctx context.Context, instanceID string) (types.Instance, error) {
	describeInstanceInput := &ec2.DescribeInstancesInput{
		InstanceIds: []string{instanceID},
	}
	describeInstanceOutput, err := d.client.DescribeInstances(ctx, describeInstanceInput)

	if err != nil {
		return types.Instance{}, err
	}
	if describeInstanceOutput == nil || len(describeInstanceOutput.Reservations) == 0 ||
		len(describeInstanceOutput.Reservations[0].Instances) == 0 {
		return types.Instance{}, fmt.Errorf("failed to find instance %s", instanceID)
	}
	return describeInstanceOutput.Reservations[0].Instances[0], nil
}

func (d *defaultEC2) AuthorizeSecurityGroupIngress(ctx context.Context, groupID string, protocol string, fromPort int, toPort int, cidrIP string, sourceSG bool) error {
	var ipv4Ranges []types.IpRange
	var ipv6Ranges []types.Ipv6Range
	var ipPermissions types.IpPermission
	if !sourceSG {
		if strings.Contains(cidrIP, ":") {
			ipv6Ranges = []types.Ipv6Range{
				{
					CidrIpv6: aws.String(cidrIP),
				},
			}
		} else {
			ipv4Ranges = []types.IpRange{
				{
					CidrIp: aws.String(cidrIP),
				},
			}
		}

		ipPermissions = types.IpPermission{
			FromPort:   aws.Int32(int32(fromPort)),
			ToPort:     aws.Int32(int32(toPort)),
			IpProtocol: aws.String(protocol),
			IpRanges:   ipv4Ranges,
			Ipv6Ranges: ipv6Ranges,
		}
	} else {
		ipPermissions = types.IpPermission{
			FromPort:   aws.Int32(int32(fromPort)),
			ToPort:     aws.Int32(int32(toPort)),
			IpProtocol: aws.String(protocol),
			UserIdGroupPairs: []types.UserIdGroupPair{
				{
					GroupId: aws.String(cidrIP),
				},
			},
		}
	}
	authorizeSecurityGroupIngressInput := &ec2.AuthorizeSecurityGroupIngressInput{
		GroupId:       aws.String(groupID),
		IpPermissions: []types.IpPermission{ipPermissions},
	}
	_, err := d.client.AuthorizeSecurityGroupIngress(ctx, authorizeSecurityGroupIngressInput)
	return err
}

func (d *defaultEC2) RevokeSecurityGroupIngress(ctx context.Context, groupID string, protocol string, fromPort int, toPort int, cidrIP string, sourceSG bool) error {
	var ipv4Ranges []types.IpRange
	var ipv6Ranges []types.Ipv6Range
	var ipPermissions types.IpPermission
	if !sourceSG {
		if strings.Contains(cidrIP, ":") {
			ipv6Ranges = []types.Ipv6Range{
				{
					CidrIpv6: aws.String(cidrIP),
				},
			}
		} else {
			ipv4Ranges = []types.IpRange{
				{
					CidrIp: aws.String(cidrIP),
				},
			}
		}

		ipPermissions = types.IpPermission{
			FromPort:   aws.Int32(int32(fromPort)),
			ToPort:     aws.Int32(int32(toPort)),
			IpProtocol: aws.String(protocol),
			IpRanges:   ipv4Ranges,
			Ipv6Ranges: ipv6Ranges,
		}
	} else {
		ipPermissions = types.IpPermission{
			FromPort:   aws.Int32(int32(fromPort)),
			ToPort:     aws.Int32(int32(toPort)),
			IpProtocol: aws.String(protocol),
			UserIdGroupPairs: []types.UserIdGroupPair{
				{
					GroupId: aws.String(cidrIP),
				},
			},
		}
	}
	revokeSecurityGroupIngressInput := &ec2.RevokeSecurityGroupIngressInput{
		GroupId:       aws.String(groupID),
		IpPermissions: []types.IpPermission{ipPermissions},
	}
	_, err := d.client.RevokeSecurityGroupIngress(ctx, revokeSecurityGroupIngressInput)
	return err
}

func (d *defaultEC2) AuthorizeSecurityGroupEgress(ctx context.Context, groupID string, protocol string, fromPort int, toPort int, cidrIP string) error {
	var ipv4Ranges []types.IpRange
	var ipv6Ranges []types.Ipv6Range
	if strings.Contains(cidrIP, ":") {
		ipv6Ranges = []types.Ipv6Range{
			{
				CidrIpv6: aws.String(cidrIP),
			},
		}
	} else {
		ipv4Ranges = []types.IpRange{
			{
				CidrIp: aws.String(cidrIP),
			},
		}
	}

	ipPermissions := types.IpPermission{
		FromPort:   aws.Int32(int32(fromPort)),
		ToPort:     aws.Int32(int32(toPort)),
		IpProtocol: aws.String(protocol),
		IpRanges:   ipv4Ranges,
		Ipv6Ranges: ipv6Ranges,
	}
	authorizeSecurityGroupEgressInput := &ec2.AuthorizeSecurityGroupEgressInput{
		GroupId:       aws.String(groupID),
		IpPermissions: []types.IpPermission{ipPermissions},
	}
	_, err := d.client.AuthorizeSecurityGroupEgress(ctx, authorizeSecurityGroupEgressInput)
	return err
}

func (d *defaultEC2) RevokeSecurityGroupEgress(ctx context.Context, groupID string, protocol string, fromPort int, toPort int, cidrIP string) error {
	var ipv4Ranges []types.IpRange
	var ipv6Ranges []types.Ipv6Range
	if strings.Contains(cidrIP, ":") {
		ipv6Ranges = []types.Ipv6Range{
			{
				CidrIpv6: aws.String(cidrIP),
			},
		}
	} else {
		ipv4Ranges = []types.IpRange{
			{
				CidrIp: aws.String(cidrIP),
			},
		}
	}

	ipPermissions := types.IpPermission{
		FromPort:   aws.Int32(int32(fromPort)),
		ToPort:     aws.Int32(int32(toPort)),
		IpProtocol: aws.String(protocol),
		IpRanges:   ipv4Ranges,
		Ipv6Ranges: ipv6Ranges,
	}
	revokeSecurityGroupEgressInput := &ec2.RevokeSecurityGroupEgressInput{
		GroupId:       aws.String(groupID),
		IpPermissions: []types.IpPermission{ipPermissions},
	}
	_, err := d.client.RevokeSecurityGroupEgress(ctx, revokeSecurityGroupEgressInput)
	return err
}

func (d *defaultEC2) DescribeNetworkInterface(ctx context.Context, interfaceIDs []string) (*ec2.DescribeNetworkInterfacesOutput, error) {
	describeNetworkInterfaceInput := &ec2.DescribeNetworkInterfacesInput{
		NetworkInterfaceIds: interfaceIDs,
	}

	return d.client.DescribeNetworkInterfaces(ctx, describeNetworkInterfaceInput)
}

func (d *defaultEC2) AssociateVPCCIDRBlock(ctx context.Context, vpcId string, cidrBlock string) (*ec2.AssociateVpcCidrBlockOutput, error) {
	associateVPCCidrBlockInput := &ec2.AssociateVpcCidrBlockInput{
		CidrBlock: aws.String(cidrBlock),
		VpcId:     aws.String(vpcId),
	}

	return d.client.AssociateVpcCidrBlock(ctx, associateVPCCidrBlockInput)
}

func (d *defaultEC2) DisAssociateVPCCIDRBlock(ctx context.Context, associationID string) error {
	disassociateVPCCidrBlockInput := &ec2.DisassociateVpcCidrBlockInput{
		AssociationId: aws.String(associationID),
	}

	_, err := d.client.DisassociateVpcCidrBlock(ctx, disassociateVPCCidrBlockInput)
	return err
}

func (d *defaultEC2) CreateSubnet(ctx context.Context, cidrBlock string, vpcID string, az string) (*ec2.CreateSubnetOutput, error) {
	createSubnetInput := &ec2.CreateSubnetInput{
		AvailabilityZone: aws.String(az),
		CidrBlock:        aws.String(cidrBlock),
		VpcId:            aws.String(vpcID),
	}
	return d.client.CreateSubnet(ctx, createSubnetInput)
}

func (d *defaultEC2) DescribeSubnets(ctx context.Context, subnetIDs []string) (*ec2.DescribeSubnetsOutput, error) {
	describeSubnetInput := &ec2.DescribeSubnetsInput{
		SubnetIds: subnetIDs,
	}
	return d.client.DescribeSubnets(ctx, describeSubnetInput)
}

func (d *defaultEC2) DescribeRouteTablesWithVPCID(ctx context.Context, vpcID string) (*ec2.DescribeRouteTablesOutput, error) {
	describeRouteTableInput := &ec2.DescribeRouteTablesInput{
		Filters: []types.Filter{
			{
				Name:   aws.String("vpc-id"),
				Values: []string{vpcID},
			},
		},
	}
	return d.client.DescribeRouteTables(ctx, describeRouteTableInput)
}

func (d *defaultEC2) DeleteSubnet(ctx context.Context, subnetID string) error {
	deleteSubnetInput := &ec2.DeleteSubnetInput{
		SubnetId: aws.String(subnetID),
	}
	_, err := d.client.DeleteSubnet(ctx, deleteSubnetInput)
	return err
}

func (d *defaultEC2) DescribeRouteTables(ctx context.Context, subnetID string) (*ec2.DescribeRouteTablesOutput, error) {
	describeRouteTableInput := &ec2.DescribeRouteTablesInput{
		Filters: []types.Filter{
			{
				Name:   aws.String("association.subnet-id"),
				Values: []string{subnetID},
			},
		},
	}
	return d.client.DescribeRouteTables(ctx, describeRouteTableInput)
}

func (d *defaultEC2) AssociateRouteTableToSubnet(ctx context.Context, routeTableId string, subnetID string) error {
	associateRouteTableInput := &ec2.AssociateRouteTableInput{
		RouteTableId: aws.String(routeTableId),
		SubnetId:     aws.String(subnetID),
	}
	_, err := d.client.AssociateRouteTable(ctx, associateRouteTableInput)
	return err
}

func (d *defaultEC2) DeleteSecurityGroup(ctx context.Context, groupID string) error {
	deleteSecurityGroupInput := &ec2.DeleteSecurityGroupInput{
		GroupId: aws.String(groupID),
	}

	_, err := d.client.DeleteSecurityGroup(ctx, deleteSecurityGroupInput)
	return err
}

func (d *defaultEC2) CreateSecurityGroup(ctx context.Context, groupName string, description string, vpcID string) (*ec2.CreateSecurityGroupOutput, error) {
	createSecurityGroupInput := &ec2.CreateSecurityGroupInput{
		Description: aws.String(description),
		GroupName:   aws.String(groupName),
		VpcId:       aws.String(vpcID),
	}

	return d.client.CreateSecurityGroup(ctx, createSecurityGroupInput)
}

func (d *defaultEC2) CreateKey(ctx context.Context, keyName string) (*ec2.CreateKeyPairOutput, error) {
	createKeyInput := &ec2.CreateKeyPairInput{
		KeyName: aws.String(keyName),
	}
	return d.client.CreateKeyPair(ctx, createKeyInput)
}

func (d *defaultEC2) DeleteKey(ctx context.Context, keyName string) error {
	deleteKeyPairInput := &ec2.DeleteKeyPairInput{
		KeyName: aws.String(keyName),
	}
	_, err := d.client.DeleteKeyPair(ctx, deleteKeyPairInput)
	return err
}

func (d *defaultEC2) DescribeKey(ctx context.Context, keyName string) (*ec2.DescribeKeyPairsOutput, error) {
	keyPairInput := &ec2.DescribeKeyPairsInput{
		KeyNames: []string{
			keyName,
		},
	}
	return d.client.DescribeKeyPairs(ctx, keyPairInput)
}

func (d *defaultEC2) TerminateInstance(ctx context.Context, instanceIDs []string) error {
	terminateInstanceInput := &ec2.TerminateInstancesInput{
		DryRun:      nil,
		InstanceIds: instanceIDs,
	}
	_, err := d.client.TerminateInstances(ctx, terminateInstanceInput)
	return err
}

func (d *defaultEC2) DescribeVPC(ctx context.Context, vpcID string) (*ec2.DescribeVpcsOutput, error) {
	describeVPCInput := &ec2.DescribeVpcsInput{
		VpcIds: []string{vpcID},
	}
	return d.client.DescribeVpcs(ctx, describeVPCInput)
}

func (d *defaultEC2) CreateTags(ctx context.Context, resourceIds []string, tags []types.Tag) (*ec2.CreateTagsOutput, error) {
	input := &ec2.CreateTagsInput{
		Resources: resourceIds,
		Tags:      tags,
	}
	return d.client.CreateTags(ctx, input)
}

func (d *defaultEC2) DeleteTags(ctx context.Context, resourceIds []string, tags []types.Tag) (*ec2.DeleteTagsOutput, error) {
	input := &ec2.DeleteTagsInput{
		Resources: resourceIds,
		Tags:      tags,
	}
	return d.client.DeleteTags(ctx, input)
}

func NewEC2(cfg aws.Config) EC2 {
	return &defaultEC2{
		client: ec2.NewFromConfig(cfg),
	}
}
