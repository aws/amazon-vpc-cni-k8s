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

package awsutils

import (
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"

	"github.com/aws/aws-sdk-go/aws"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/ec2metadata/mocks"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/ec2wrapper/mocks"
)

const (
	az           = "us-east-1a"
	localIP      = "10.0.0.10"
	instanceID   = "i-0e1f3b9eb950e4980"
	instanceType = "c1.medium"
	primaryMAC   = "12:ef:2a:98:e5:5a"
	eni2MAC      = "12:ef:2a:98:e5:5b"
	sg1          = "sg-2e080f50"
	sg2          = "sg-2e080f51"
	sgs          = sg1 + " " + sg2
	subnetID     = "subnet-6b245523"
	vpcCIDR      = "10.0.0.0/16"
	subnetCIDR   = "10.0.1.0/24"
	accountID    = "694065802095"
	primaryeniID = "eni-00000000"
	eniID        = "eni-5731da78"
	eniAttachID  = "eni-attach-beb21856"
	eni1Device   = "0"
	eni2Device   = "2"
	ownerID      = "i-0946d8a24922d2852"
)

func setup(t *testing.T) (*gomock.Controller,
	*mock_ec2metadata.MockEC2Metadata,
	*mock_ec2wrapper.MockEC2) {
	ctrl := gomock.NewController(t)
	return ctrl,
		mock_ec2metadata.NewMockEC2Metadata(ctrl),
		mock_ec2wrapper.NewMockEC2(ctrl)
}

func TestInitWithEC2metadata(t *testing.T) {
	ctrl, mockMetadata, _ := setup(t)
	defer ctrl.Finish()

	mockMetadata.EXPECT().GetMetadata(metadataAZ).Return(az, nil)
	mockMetadata.EXPECT().GetMetadata(metadataLocalIP).Return(localIP, nil)
	mockMetadata.EXPECT().GetMetadata(metadataInstanceID).Return(instanceID, nil)
	mockMetadata.EXPECT().GetMetadata(metadataInstanceType).Return(instanceType, nil)
	mockMetadata.EXPECT().GetMetadata(metadataMAC).Return(primaryMAC, nil)
	mockMetadata.EXPECT().GetMetadata(metadataMACPath).Return(primaryMAC, nil)
	mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataDeviceNum).Return("1", nil)
	mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataOwnerID).Return("1234", nil)
	mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataInterface).Return(primaryMAC, nil)
	mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataSGs).Return(sgs, nil)
	mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataSubnetID).Return(subnetID, nil)
	mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataVPCcidr).Return(vpcCIDR, nil)

	ins := &EC2InstanceMetadataCache{ec2Metadata: mockMetadata}

	err := ins.initWithEC2Metadata()

	assert.NoError(t, err)
	assert.Equal(t, az, ins.availabilityZone)
	assert.Equal(t, localIP, ins.localIPv4)
	assert.Equal(t, ins.instanceID, instanceID)
	assert.Equal(t, ins.primaryENImac, primaryMAC)
	assert.Equal(t, len(ins.securityGroups), 2)
	assert.Equal(t, subnetID, ins.subnetID)
	assert.Equal(t, vpcCIDR, ins.vpcIPv4CIDR)
}

func TestInitWithEC2metadataVPCcidrErr(t *testing.T) {
	ctrl, mockMetadata, _ := setup(t)
	defer ctrl.Finish()

	mockMetadata.EXPECT().GetMetadata(metadataAZ).Return(az, nil)
	mockMetadata.EXPECT().GetMetadata(metadataLocalIP).Return(localIP, nil)
	mockMetadata.EXPECT().GetMetadata(metadataInstanceID).Return(instanceID, nil)
	mockMetadata.EXPECT().GetMetadata(metadataInstanceType).Return(instanceType, nil)
	mockMetadata.EXPECT().GetMetadata(metadataMAC).Return(primaryMAC, nil)
	mockMetadata.EXPECT().GetMetadata(metadataMACPath).Return(primaryMAC, nil)
	mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataDeviceNum).Return("1", nil)
	mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataOwnerID).Return("1234", nil)
	mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataInterface).Return(primaryMAC, nil)
	mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataSGs).Return(sgs, nil)
	mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataSubnetID).Return(subnetID, nil)
	mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataVPCcidr).Return(vpcCIDR, errors.New("Error on VPCcidr"))

	ins := &EC2InstanceMetadataCache{ec2Metadata: mockMetadata}

	err := ins.initWithEC2Metadata()

	assert.Error(t, err)
}

func TestInitWithEC2metadataSubnetErr(t *testing.T) {
	ctrl, mockMetadata, _ := setup(t)
	defer ctrl.Finish()

	mockMetadata.EXPECT().GetMetadata(metadataAZ).Return(az, nil)
	mockMetadata.EXPECT().GetMetadata(metadataLocalIP).Return(localIP, nil)
	mockMetadata.EXPECT().GetMetadata(metadataInstanceID).Return(instanceID, nil)
	mockMetadata.EXPECT().GetMetadata(metadataInstanceType).Return(instanceType, nil)
	mockMetadata.EXPECT().GetMetadata(metadataMAC).Return(primaryMAC, nil)
	mockMetadata.EXPECT().GetMetadata(metadataMACPath).Return(primaryMAC, nil)
	mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataDeviceNum).Return("1", nil)
	mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataOwnerID).Return("1234", nil)
	mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataInterface).Return(primaryMAC, nil)
	mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataSGs).Return(sgs, nil)
	mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataSubnetID).Return(subnetID, errors.New("Error on Subnet"))
	ins := &EC2InstanceMetadataCache{ec2Metadata: mockMetadata}

	err := ins.initWithEC2Metadata()

	assert.Error(t, err)
}

func TestInitWithEC2metadataSGErr(t *testing.T) {
	ctrl, mockMetadata, _ := setup(t)
	defer ctrl.Finish()

	mockMetadata.EXPECT().GetMetadata(metadataAZ).Return(az, nil)
	mockMetadata.EXPECT().GetMetadata(metadataLocalIP).Return(localIP, nil)
	mockMetadata.EXPECT().GetMetadata(metadataInstanceID).Return(instanceID, nil)
	mockMetadata.EXPECT().GetMetadata(metadataInstanceType).Return(instanceType, nil)
	mockMetadata.EXPECT().GetMetadata(metadataMAC).Return(primaryMAC, nil)
	mockMetadata.EXPECT().GetMetadata(metadataMACPath).Return(primaryMAC, nil)
	mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataDeviceNum).Return("1", nil)
	mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataOwnerID).Return("1234", nil)
	mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataInterface).Return(primaryMAC, nil)
	mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataSGs).Return(sgs, errors.New("Error on SG"))
	ins := &EC2InstanceMetadataCache{ec2Metadata: mockMetadata}

	err := ins.initWithEC2Metadata()

	assert.Error(t, err)
}

func TestInitWithEC2metadataENIErrs(t *testing.T) {
	ctrl, mockMetadata, _ := setup(t)
	defer ctrl.Finish()

	mockMetadata.EXPECT().GetMetadata(metadataAZ).Return(az, nil)
	mockMetadata.EXPECT().GetMetadata(metadataLocalIP).Return(localIP, nil)
	mockMetadata.EXPECT().GetMetadata(metadataInstanceID).Return(instanceID, nil)
	mockMetadata.EXPECT().GetMetadata(metadataInstanceType).Return(instanceType, nil)
	mockMetadata.EXPECT().GetMetadata(metadataMAC).Return(primaryMAC, nil)
	mockMetadata.EXPECT().GetMetadata(metadataMACPath).Return("", errors.New("Err on ENIs"))

	ins := &EC2InstanceMetadataCache{ec2Metadata: mockMetadata}

	err := ins.initWithEC2Metadata()

	assert.Error(t, err)
}

func TestInitWithEC2metadataMACErr(t *testing.T) {
	ctrl, mockMetadata, _ := setup(t)
	defer ctrl.Finish()

	mockMetadata.EXPECT().GetMetadata(metadataAZ).Return(az, nil)
	mockMetadata.EXPECT().GetMetadata(metadataLocalIP).Return(localIP, nil)
	mockMetadata.EXPECT().GetMetadata(metadataInstanceID).Return(instanceID, nil)
	mockMetadata.EXPECT().GetMetadata(metadataInstanceType).Return(instanceType, nil)
	mockMetadata.EXPECT().GetMetadata(metadataMAC).Return(primaryMAC, errors.New("Error on MAC"))

	ins := &EC2InstanceMetadataCache{ec2Metadata: mockMetadata}

	err := ins.initWithEC2Metadata()

	assert.Error(t, err)
}

func TestInitWithEC2metadataLocalIPErr(t *testing.T) {
	ctrl, mockMetadata, _ := setup(t)
	defer ctrl.Finish()

	mockMetadata.EXPECT().GetMetadata(metadataAZ).Return(az, nil)
	mockMetadata.EXPECT().GetMetadata(metadataLocalIP).Return(localIP, errors.New("Error on localIP"))
	ins := &EC2InstanceMetadataCache{ec2Metadata: mockMetadata}

	err := ins.initWithEC2Metadata()

	assert.Error(t, err)

}

func TestInitWithEC2metadataInstanceErr(t *testing.T) {
	ctrl, mockMetadata, _ := setup(t)
	defer ctrl.Finish()

	mockMetadata.EXPECT().GetMetadata(metadataAZ).Return(az, nil)
	mockMetadata.EXPECT().GetMetadata(metadataLocalIP).Return(localIP, nil)
	mockMetadata.EXPECT().GetMetadata(metadataInstanceID).Return(instanceID, errors.New("Error on instanceID"))

	ins := &EC2InstanceMetadataCache{ec2Metadata: mockMetadata}

	err := ins.initWithEC2Metadata()

	assert.Error(t, err)
}

func TestInitWithEC2metadataAZErr(t *testing.T) {
	ctrl, mockMetadata, _ := setup(t)
	defer ctrl.Finish()

	mockMetadata.EXPECT().GetMetadata(metadataAZ).Return(az, errors.New("Error on metadata AZ"))
	ins := &EC2InstanceMetadataCache{ec2Metadata: mockMetadata}

	err := ins.initWithEC2Metadata()

	assert.Error(t, err)
}

func TestSetPrimaryENs(t *testing.T) {
	ctrl, mockMetadata, _ := setup(t)
	defer ctrl.Finish()

	mockMetadata.EXPECT().GetMetadata(metadataMACPath).Return(primaryMAC+" "+eni2MAC, nil)

	gomock.InOrder(
		mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataDeviceNum).Return(eni1Device, nil),
		mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataOwnerID).Return(ownerID, nil),
		mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataInterface).Return(primaryeniID, nil),
	)
	ins := &EC2InstanceMetadataCache{ec2Metadata: mockMetadata}
	ins.primaryENImac = primaryMAC
	err := ins.setPrimaryENI()

	assert.NoError(t, err)
	assert.Equal(t, ins.primaryENI, primaryeniID)
}

func TestGetAttachedENIs(t *testing.T) {
	ctrl, mockMetadata, _ := setup(t)
	defer ctrl.Finish()

	mockMetadata.EXPECT().GetMetadata(metadataMACPath).Return(primaryMAC+" "+eni2MAC, nil)

	gomock.InOrder(
		mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataDeviceNum).Return(eni1Device, nil),
		mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataInterface).Return(primaryMAC, nil),
		mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataSubnetCIDR).Return(subnetCIDR, nil),
		mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataIPv4s).Return("", nil),
		mockMetadata.EXPECT().GetMetadata(metadataMACPath+eni2MAC+metadataDeviceNum).Return(eni2Device, nil),
		mockMetadata.EXPECT().GetMetadata(metadataMACPath+eni2MAC+metadataInterface).Return(eni2MAC, nil),
		mockMetadata.EXPECT().GetMetadata(metadataMACPath+eni2MAC+metadataSubnetCIDR).Return(subnetCIDR, nil),
		mockMetadata.EXPECT().GetMetadata(metadataMACPath+eni2MAC+metadataIPv4s).Return("", nil),
	)
	ins := &EC2InstanceMetadataCache{ec2Metadata: mockMetadata}
	ens, err := ins.GetAttachedENIs()

	assert.NoError(t, err)
	assert.Equal(t, len(ens), 2)
}

func TestAWSGetFreeDeviceNumberOnErr(t *testing.T) {
	ctrl, _, mockEC2 := setup(t)
	defer ctrl.Finish()

	// test error handling
	mockEC2.EXPECT().DescribeInstances(gomock.Any()).Return(nil, errors.New("Error on DescribeInstances"))
	ins := &EC2InstanceMetadataCache{ec2SVC: mockEC2}

	_, err := ins.awsGetFreeDeviceNumber()

	assert.Error(t, err)
}

func TestAWSGetFreeDeviceNumberNoDevice(t *testing.T) {
	ctrl, _, mockEC2 := setup(t)
	defer ctrl.Finish()

	// test no free index
	ec2ENIs := make([]*ec2.InstanceNetworkInterface, 0)
	ownerID := accountID

	for i := 0; i < maxENIs; i++ {
		var deviceNums [maxENIs]int64
		deviceNums[i] = int64(i)
		ec2ENI := &ec2.InstanceNetworkInterface{Attachment: &ec2.InstanceNetworkInterfaceAttachment{DeviceIndex: &deviceNums[i]},
			OwnerId: &ownerID}
		ec2ENIs = append(ec2ENIs, ec2ENI)
	}
	result := &ec2.DescribeInstancesOutput{
		Reservations: []*ec2.Reservation{{Instances: []*ec2.Instance{{NetworkInterfaces: ec2ENIs}}}}}

	mockEC2.EXPECT().DescribeInstances(gomock.Any()).Return(result, nil)
	ins := &EC2InstanceMetadataCache{ec2SVC: mockEC2}

	_, err := ins.awsGetFreeDeviceNumber()

	assert.Error(t, err)

}

func TestAllocENI(t *testing.T) {
	ctrl, _, mockEC2 := setup(t)
	defer ctrl.Finish()

	cureniID := eniID
	eni := ec2.CreateNetworkInterfaceOutput{NetworkInterface: &ec2.NetworkInterface{NetworkInterfaceId: &cureniID}}
	mockEC2.EXPECT().CreateNetworkInterface(gomock.Any()).Return(&eni, nil)
	mockEC2.EXPECT().CreateTags(gomock.Any()).Return(nil, nil)

	// 2 ENIs, uses device number 0 3, expect to find free at 1
	ec2ENIs := make([]*ec2.InstanceNetworkInterface, 0)
	deviceNum1 := int64(0)
	ownerID := accountID
	ec2ENI := &ec2.InstanceNetworkInterface{Attachment: &ec2.InstanceNetworkInterfaceAttachment{DeviceIndex: &deviceNum1},
		OwnerId: &ownerID}
	ec2ENIs = append(ec2ENIs, ec2ENI)

	deviceNum2 := int64(3)
	ownerID = accountID
	ec2ENI = &ec2.InstanceNetworkInterface{Attachment: &ec2.InstanceNetworkInterfaceAttachment{DeviceIndex: &deviceNum2},
		OwnerId: &ownerID}
	ec2ENIs = append(ec2ENIs, ec2ENI)

	result := &ec2.DescribeInstancesOutput{
		Reservations: []*ec2.Reservation{{Instances: []*ec2.Instance{{NetworkInterfaces: ec2ENIs}}}}}

	mockEC2.EXPECT().DescribeInstances(gomock.Any()).Return(result, nil)
	attachmentID := "eni-attach-58ddda9d"
	attachResult := &ec2.AttachNetworkInterfaceOutput{
		AttachmentId: &attachmentID}
	mockEC2.EXPECT().AttachNetworkInterface(gomock.Any()).Return(attachResult, nil)

	mockEC2.EXPECT().ModifyNetworkInterfaceAttribute(gomock.Any()).Return(nil, nil)

	ins := &EC2InstanceMetadataCache{ec2SVC: mockEC2}
	_, err := ins.AllocENI(false, nil, "")
	assert.NoError(t, err)
}

func TestAllocENINoFreeDevice(t *testing.T) {
	ctrl, _, mockEC2 := setup(t)
	defer ctrl.Finish()

	cureniID := eniID
	eni := ec2.CreateNetworkInterfaceOutput{NetworkInterface: &ec2.NetworkInterface{NetworkInterfaceId: &cureniID}}
	mockEC2.EXPECT().CreateNetworkInterface(gomock.Any()).Return(&eni, nil)
	mockEC2.EXPECT().CreateTags(gomock.Any()).Return(nil, nil)

	// test no free index
	ec2ENIs := make([]*ec2.InstanceNetworkInterface, 0)
	ownerID := accountID

	for i := 0; i < maxENIs; i++ {
		var deviceNums [maxENIs]int64
		deviceNums[i] = int64(i)
		ec2ENI := &ec2.InstanceNetworkInterface{Attachment: &ec2.InstanceNetworkInterfaceAttachment{DeviceIndex: &deviceNums[i]},
			OwnerId: &ownerID}
		ec2ENIs = append(ec2ENIs, ec2ENI)
	}
	result := &ec2.DescribeInstancesOutput{
		Reservations: []*ec2.Reservation{{Instances: []*ec2.Instance{{NetworkInterfaces: ec2ENIs}}}}}

	mockEC2.EXPECT().DescribeInstances(gomock.Any()).Return(result, nil)

	mockEC2.EXPECT().DeleteNetworkInterface(gomock.Any()).Return(nil, nil)

	ins := &EC2InstanceMetadataCache{ec2SVC: mockEC2}
	_, err := ins.AllocENI(false, nil, "")
	assert.Error(t, err)

}

func TestAllocENIMaxReached(t *testing.T) {
	ctrl, _, mockEC2 := setup(t)
	defer ctrl.Finish()

	cureniID := eniID
	eni := ec2.CreateNetworkInterfaceOutput{NetworkInterface: &ec2.NetworkInterface{NetworkInterfaceId: &cureniID}}
	mockEC2.EXPECT().CreateNetworkInterface(gomock.Any()).Return(&eni, nil)
	mockEC2.EXPECT().CreateTags(gomock.Any()).Return(nil, nil)

	// 2 ENIs, uses device number 0 3, expect to find free at 1
	ec2ENIs := make([]*ec2.InstanceNetworkInterface, 0)
	deviceNum1 := int64(0)
	ownerID := accountID
	ec2ENI := &ec2.InstanceNetworkInterface{Attachment: &ec2.InstanceNetworkInterfaceAttachment{DeviceIndex: &deviceNum1},
		OwnerId: &ownerID}
	ec2ENIs = append(ec2ENIs, ec2ENI)

	deviceNum2 := int64(3)
	ownerID = accountID
	ec2ENI = &ec2.InstanceNetworkInterface{Attachment: &ec2.InstanceNetworkInterfaceAttachment{DeviceIndex: &deviceNum2},
		OwnerId: &ownerID}
	ec2ENIs = append(ec2ENIs, ec2ENI)

	result := &ec2.DescribeInstancesOutput{
		Reservations: []*ec2.Reservation{{Instances: []*ec2.Instance{{NetworkInterfaces: ec2ENIs}}}}}

	mockEC2.EXPECT().DescribeInstances(gomock.Any()).Return(result, nil)
	mockEC2.EXPECT().AttachNetworkInterface(gomock.Any()).Return(nil, errors.New("AttachmentLimitExceeded"))
	mockEC2.EXPECT().DeleteNetworkInterface(gomock.Any()).Return(nil, nil)

	ins := &EC2InstanceMetadataCache{ec2SVC: mockEC2}
	_, err := ins.AllocENI(false, nil, "")
	assert.Error(t, err)
}

func TestFreeENI(t *testing.T) {
	ctrl, _, mockEC2 := setup(t)
	defer ctrl.Finish()

	attachmentID := eniAttachID
	attachment := &ec2.NetworkInterfaceAttachment{AttachmentId: &attachmentID}
	result := &ec2.DescribeNetworkInterfacesOutput{
		NetworkInterfaces: []*ec2.NetworkInterface{{Attachment: attachment}}}
	mockEC2.EXPECT().DescribeNetworkInterfaces(gomock.Any()).Return(result, nil)
	mockEC2.EXPECT().DetachNetworkInterface(gomock.Any()).Return(nil, nil)
	mockEC2.EXPECT().DeleteNetworkInterface(gomock.Any()).Return(nil, nil)
	ins := &EC2InstanceMetadataCache{ec2SVC: mockEC2}
	err := ins.FreeENI("test-eni")
	assert.NoError(t, err)

}

func TestFreeENIRetry(t *testing.T) {
	ctrl, _, mockEC2 := setup(t)
	defer ctrl.Finish()

	attachmentID := eniAttachID
	attachment := &ec2.NetworkInterfaceAttachment{AttachmentId: &attachmentID}
	result := &ec2.DescribeNetworkInterfacesOutput{
		NetworkInterfaces: []*ec2.NetworkInterface{{Attachment: attachment}}}
	mockEC2.EXPECT().DescribeNetworkInterfaces(gomock.Any()).Return(result, nil)

	// retry 2 times
	mockEC2.EXPECT().DetachNetworkInterface(gomock.Any()).Return(nil, nil)
	mockEC2.EXPECT().DeleteNetworkInterface(gomock.Any()).Return(nil, errors.New("testing retrying delete"))
	mockEC2.EXPECT().DeleteNetworkInterface(gomock.Any()).Return(nil, nil)
	ins := &EC2InstanceMetadataCache{ec2SVC: mockEC2}
	err := ins.FreeENI("test-eni")
	assert.NoError(t, err)

}

func TestFreeENIRetryMax(t *testing.T) {
	ctrl, _, mockEC2 := setup(t)
	defer ctrl.Finish()

	attachmentID := eniAttachID
	attachment := &ec2.NetworkInterfaceAttachment{AttachmentId: &attachmentID}
	result := &ec2.DescribeNetworkInterfacesOutput{
		NetworkInterfaces: []*ec2.NetworkInterface{{Attachment: attachment}}}
	mockEC2.EXPECT().DescribeNetworkInterfaces(gomock.Any()).Return(result, nil)

	mockEC2.EXPECT().DetachNetworkInterface(gomock.Any()).Return(nil, nil)

	for i := 0; i < maxENIDeleteRetries; i++ {
		mockEC2.EXPECT().DeleteNetworkInterface(gomock.Any()).Return(nil, errors.New("testing retrying delete"))
	}

	ins := &EC2InstanceMetadataCache{ec2SVC: mockEC2}
	err := ins.FreeENI("test-eni")
	assert.Error(t, err)

}

func TestFreeENIDescribeErr(t *testing.T) {
	ctrl, _, mockEC2 := setup(t)
	defer ctrl.Finish()

	mockEC2.EXPECT().DescribeNetworkInterfaces(gomock.Any()).Return(nil, errors.New("Error on DescribeNetworkInterfaces"))
	ins := &EC2InstanceMetadataCache{ec2SVC: mockEC2}
	err := ins.FreeENI("test-eni")
	assert.Error(t, err)

}

func TestAllocIPAddress(t *testing.T) {
	ctrl, _, mockEC2 := setup(t)
	defer ctrl.Finish()

	testIP := ec2.AssignPrivateIpAddressesOutput{}
	mockEC2.EXPECT().AssignPrivateIpAddresses(gomock.Any()).Return(&testIP, nil)

	ins := &EC2InstanceMetadataCache{ec2SVC: mockEC2}

	err := ins.AllocIPAddress("eni-id")

	assert.NoError(t, err)
}

func TestAllocIPAddressOnErr(t *testing.T) {
	ctrl, _, mockEC2 := setup(t)
	defer ctrl.Finish()

	mockEC2.EXPECT().AssignPrivateIpAddresses(gomock.Any()).Return(nil, errors.New("Error on AssignPrivateIpAddresses"))

	ins := &EC2InstanceMetadataCache{ec2SVC: mockEC2}

	err := ins.AllocIPAddress("eni-id")

	assert.Error(t, err)
}

func TestAllocAllIPAddress(t *testing.T) {
	ctrl, _, mockEC2 := setup(t)
	defer ctrl.Finish()

	// the expected addresses for t2.nano
	input := &ec2.AssignPrivateIpAddressesInput{
		NetworkInterfaceId:             aws.String("eni-id"),
		SecondaryPrivateIpAddressCount: aws.Int64(1),
	}
	mockEC2.EXPECT().AssignPrivateIpAddresses(input).Return(nil, nil)

	ins := &EC2InstanceMetadataCache{ec2SVC: mockEC2, instanceType: "t2.nano"}

	err := ins.AllocAllIPAddress("eni-id")

	assert.NoError(t, err)

	// the expected addresses for r4.16xlarge
	input = &ec2.AssignPrivateIpAddressesInput{
		NetworkInterfaceId:             aws.String("eni-id"),
		SecondaryPrivateIpAddressCount: aws.Int64(49),
	}
	mockEC2.EXPECT().AssignPrivateIpAddresses(input).Return(nil, nil)

	ins = &EC2InstanceMetadataCache{ec2SVC: mockEC2, instanceType: "r4.16xlarge"}

	err = ins.AllocAllIPAddress("eni-id")

	assert.NoError(t, err)
}

func TestAllocIPAddresses(t *testing.T) {
	ctrl, _, mockEC2 := setup(t)
	defer ctrl.Finish()

	// when required IP numbers(5) is below ENI's limit(49)
	input := &ec2.AssignPrivateIpAddressesInput{
		NetworkInterfaceId:             aws.String("eni-id"),
		SecondaryPrivateIpAddressCount: aws.Int64(5),
	}
	mockEC2.EXPECT().AssignPrivateIpAddresses(input).Return(nil, nil)

	ins := &EC2InstanceMetadataCache{ec2SVC: mockEC2, instanceType: "r4.16xlarge"}

	err := ins.AllocIPAddresses("eni-id", 5)

	assert.NoError(t, err)

	// when required IP numbers(60) is higher than ENI's limit(49)
	input = &ec2.AssignPrivateIpAddressesInput{
		NetworkInterfaceId:             aws.String("eni-id"),
		SecondaryPrivateIpAddressCount: aws.Int64(49),
	}
	mockEC2.EXPECT().AssignPrivateIpAddresses(input).Return(nil, nil)

	ins = &EC2InstanceMetadataCache{ec2SVC: mockEC2, instanceType: "r4.16xlarge"}

	err = ins.AllocIPAddresses("eni-id", 49)

	assert.NoError(t, err)
}

func TestAllocAllIPAddressOnErr(t *testing.T) {
	ctrl, _, mockEC2 := setup(t)
	defer ctrl.Finish()

	// 2 addresses
	mockEC2.EXPECT().AssignPrivateIpAddresses(gomock.Any()).Return(nil, nil)
	mockEC2.EXPECT().AssignPrivateIpAddresses(gomock.Any()).Return(nil, errors.New("Error on AssignPrivateIpAddresses"))

	ins := &EC2InstanceMetadataCache{ec2SVC: mockEC2}

	err := ins.AllocAllIPAddress("eni-id")

	assert.Error(t, err)
}
