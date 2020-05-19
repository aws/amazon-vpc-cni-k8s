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
	"errors"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"

	mock_ec2metadata "github.com/aws/amazon-vpc-cni-k8s/pkg/ec2metadata/mocks"
	mock_ec2wrapper "github.com/aws/amazon-vpc-cni-k8s/pkg/ec2wrapper/mocks"
)

const (
	az            = "us-east-1a"
	localIP       = "10.0.0.10"
	instanceID    = "i-0e1f3b9eb950e4980"
	instanceType  = "c1.medium"
	primaryMAC    = "12:ef:2a:98:e5:5a"
	eni2MAC       = "12:ef:2a:98:e5:5b"
	sg1           = "sg-2e080f50"
	sg2           = "sg-2e080f51"
	sgs           = sg1 + " " + sg2
	subnetID      = "subnet-6b245523"
	vpcCIDR       = "10.0.0.0/16"
	subnetCIDR    = "10.0.1.0/24"
	accountID     = "694065802095"
	primaryeniID  = "eni-00000000"
	eniID         = "eni-5731da78"
	eniAttachID   = "eni-attach-beb21856"
	eni1Device    = "0"
	eni1PrivateIP = "10.0.0.1"
	eni2Device    = "1"
	eni2PrivateIP = "10.0.0.2"
	eni2AttachID  = "eni-attach-fafdfafd"
	eni2ID        = "eni-12341234"
	ownerID       = "i-0946d8a24922d2852"
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
	mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataDeviceNum).Return(eni1Device, nil)
	mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataOwnerID).Return("1234", nil)
	mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataInterface).Return(primaryMAC, nil)
	mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataSGs).Return(sgs, nil)
	mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataSubnetID).Return(subnetID, nil)
	mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataVPCcidr).Return(vpcCIDR, nil)
	mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataVPCcidrs).Return(vpcCIDR, nil)

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
	mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataDeviceNum).Return(eni1Device, nil)
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
	mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataDeviceNum).Return(eni1Device, nil)
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
	mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataDeviceNum).Return(eni1Device, nil)
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
	mockMetadata.EXPECT().GetMetadata(metadataMACPath).Return("", errors.New("err on ENIs"))

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
	mockMetadata.EXPECT().GetMetadata(metadataMAC).Return(primaryMAC, errors.New("error on MAC"))

	ins := &EC2InstanceMetadataCache{ec2Metadata: mockMetadata}
	err := ins.initWithEC2Metadata()
	assert.Error(t, err)
}

func TestInitWithEC2metadataLocalIPErr(t *testing.T) {
	ctrl, mockMetadata, _ := setup(t)
	defer ctrl.Finish()

	mockMetadata.EXPECT().GetMetadata(metadataAZ).Return(az, nil)
	mockMetadata.EXPECT().GetMetadata(metadataLocalIP).Return(localIP, errors.New("error on localIP"))

	ins := &EC2InstanceMetadataCache{ec2Metadata: mockMetadata}
	err := ins.initWithEC2Metadata()
	assert.Error(t, err)
}

func TestInitWithEC2metadataInstanceErr(t *testing.T) {
	ctrl, mockMetadata, _ := setup(t)
	defer ctrl.Finish()

	mockMetadata.EXPECT().GetMetadata(metadataAZ).Return(az, nil)
	mockMetadata.EXPECT().GetMetadata(metadataLocalIP).Return(localIP, nil)
	mockMetadata.EXPECT().GetMetadata(metadataInstanceID).Return(instanceID, errors.New("error on instanceID"))

	ins := &EC2InstanceMetadataCache{ec2Metadata: mockMetadata}
	err := ins.initWithEC2Metadata()
	assert.Error(t, err)
}

func TestInitWithEC2metadataAZErr(t *testing.T) {
	ctrl, mockMetadata, _ := setup(t)
	defer ctrl.Finish()

	mockMetadata.EXPECT().GetMetadata(metadataAZ).Return(az, errors.New("error on metadata AZ"))

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
		mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataInterface).Return(eniID, nil),
		mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataSubnetCIDR).Return(subnetCIDR, nil),
		mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataIPv4s).Return("", nil),
		mockMetadata.EXPECT().GetMetadata(metadataMACPath+eni2MAC+metadataDeviceNum).Return(eni2Device, nil),
		mockMetadata.EXPECT().GetMetadata(metadataMACPath+eni2MAC+metadataInterface).Return(eni2ID, nil),
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
	mockEC2.EXPECT().DescribeInstancesWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("error on DescribeInstancesWithContext"))

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

	mockEC2.EXPECT().DescribeInstancesWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(result, nil)

	ins := &EC2InstanceMetadataCache{ec2SVC: mockEC2}
	_, err := ins.awsGetFreeDeviceNumber()
	assert.Error(t, err)
}

func TestGetENIAttachmentID(t *testing.T) {
	ctrl, _, mockEC2 := setup(t)
	defer ctrl.Finish()

	attachmentID := aws.String("foo-attach")
	testCases := []struct {
		name   string
		output *ec2.DescribeNetworkInterfacesOutput
		awsErr error
		expID  *string
		expErr error
	}{
		{
			"success with attachment",
			&ec2.DescribeNetworkInterfacesOutput{
				NetworkInterfaces: []*ec2.NetworkInterface{{
					Attachment: &ec2.NetworkInterfaceAttachment{
						AttachmentId: attachmentID,
					},
				}},
			},
			nil,
			attachmentID,
			nil,
		},
		{
			"success no Attachment",
			&ec2.DescribeNetworkInterfacesOutput{
				NetworkInterfaces: []*ec2.NetworkInterface{{}},
			},
			nil,
			nil,
			nil,
		},
		{
			"error empty net ifaces",
			&ec2.DescribeNetworkInterfacesOutput{
				NetworkInterfaces: []*ec2.NetworkInterface{},
			},
			nil,
			nil,
			ErrNoNetworkInterfaces,
		},
		{
			"not found error",
			nil,
			awserr.New("InvalidNetworkInterfaceID.NotFound", "", nil),
			nil,
			ErrENINotFound,
		},
	}

	for _, tc := range testCases {
		mockEC2.EXPECT().DescribeNetworkInterfacesWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(tc.output, tc.awsErr)

		ins := &EC2InstanceMetadataCache{ec2SVC: mockEC2}
		id, err := ins.getENIAttachmentID("test-eni")
		assert.Equal(t, tc.expErr, err)
		assert.Equal(t, tc.expID, id)
	}
}

func TestDescribeAllENIs(t *testing.T) {
	ctrl, mockMetadata, mockEC2 := setup(t)
	defer ctrl.Finish()

	mockMetadata.EXPECT().GetMetadata(metadataMACPath).Times(2).Return(primaryMAC, nil)
	mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataDeviceNum).Times(2).Return(eni1Device, nil)
	mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataInterface).Times(2).Return(eniID, nil)
	mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataSubnetCIDR).Times(2).Return(subnetCIDR, nil)
	mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataIPv4s).Times(2).Return("", nil)

	result := &ec2.DescribeNetworkInterfacesOutput{
		NetworkInterfaces: []*ec2.NetworkInterface{{
			TagSet: []*ec2.Tag{
				{Key: aws.String("foo"), Value: aws.String("foo-value")},
			},
		}},
	}

	testCases := []struct {
		name    string
		exptags map[string]TagMap
		awsErr  error
		expErr  error
	}{
		{"success DescribeENI", map[string]TagMap{"": {"foo": "foo-value"}}, nil, nil},
		{"not found error", nil, awserr.New("InvalidNetworkInterfaceID.NotFound", "", nil), ErrENINotFound},
	}

	for _, tc := range testCases {
		mockEC2.EXPECT().DescribeNetworkInterfacesWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(result, tc.awsErr)
		ins := &EC2InstanceMetadataCache{ec2Metadata: mockMetadata, ec2SVC: mockEC2}
		_, tags, err := ins.DescribeAllENIs()
		assert.Equal(t, tc.expErr, err, tc.name)
		assert.Equal(t, tc.exptags, tags, tc.name)
	}
}

func TestTagEni(t *testing.T) {
	ctrl, mockMetadata, mockEC2 := setup(t)
	defer ctrl.Finish()
	mockMetadata.EXPECT().GetMetadata(metadataAZ).Return(az, nil)
	mockMetadata.EXPECT().GetMetadata(metadataLocalIP).Return(localIP, nil)
	mockMetadata.EXPECT().GetMetadata(metadataInstanceID).Return(instanceID, nil)
	mockMetadata.EXPECT().GetMetadata(metadataInstanceType).Return(instanceType, nil)
	mockMetadata.EXPECT().GetMetadata(metadataMAC).Return(primaryMAC, nil)
	mockMetadata.EXPECT().GetMetadata(metadataMACPath).Return(primaryMAC, nil)
	mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataDeviceNum).Return(eni1Device, nil)
	mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataOwnerID).Return("1234", nil)
	mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataInterface).Return(primaryMAC, nil)
	mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataSGs).Return(sgs, nil)
	mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataSubnetID).Return(subnetID, nil)
	mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataVPCcidr).Return(vpcCIDR, nil)
	mockMetadata.EXPECT().GetMetadata(metadataMACPath+primaryMAC+metadataVPCcidrs).Return(vpcCIDR, nil)

	ins := &EC2InstanceMetadataCache{ec2Metadata: mockMetadata, ec2SVC: mockEC2}
	err := ins.initWithEC2Metadata()
	assert.NoError(t, err)
	mockEC2.EXPECT().CreateTagsWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("tagging failed"))
	mockEC2.EXPECT().CreateTagsWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("tagging failed"))
	mockEC2.EXPECT().CreateTagsWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("tagging failed"))
	mockEC2.EXPECT().CreateTagsWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("tagging failed"))
	mockEC2.EXPECT().CreateTagsWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)
	ins.tagENI(eniID, time.Millisecond)
	assert.NoError(t, err)
}

func TestAdditionalTagsEni(t *testing.T) {
	ctrl, _, mockEC2 := setup(t)
	defer ctrl.Finish()
	_ = os.Setenv(additionalEniTagsEnvVar, `{"testKey": "testing"}`)
	currentENIID := eniID
	//result key
	tagKey1 := "testKey"
	//result value
	tagValue1 := "testing"
	tag := ec2.Tag{
		Key:   &tagKey1,
		Value: &tagValue1}
	result := &ec2.DescribeNetworkInterfacesOutput{
		NetworkInterfaces: []*ec2.NetworkInterface{{TagSet: []*ec2.Tag{&tag}}}}

	ins := &EC2InstanceMetadataCache{ec2SVC: mockEC2}
	mockEC2.EXPECT().CreateTagsWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)
	ins.tagENI(currentENIID, time.Millisecond)

	// Verify the tags are registered.
	assert.Equal(t, aws.StringValue(result.NetworkInterfaces[0].TagSet[0].Key), tagKey1)
	assert.Equal(t, aws.StringValue(result.NetworkInterfaces[0].TagSet[0].Value), tagValue1)
}

func TestMapToTags(t *testing.T) {
	tagKey1 := "tagKey1"
	tagKey2 := "tagKey2"
	tagValue1 := "tagValue1"
	tagValue2 := "tagValue2"
	tagKey3 := "cluster.k8s.amazonaws.com/name"
	tagValue3 := "clusterName"
	tagsMap := map[string]string{
		tagKey1: tagValue1,
		tagKey2: tagValue2,
		tagKey3: tagValue3,
	}
	tags := make([]*ec2.Tag, 0)
	tags = mapToTags(tagsMap, tags)
	assert.Equal(t, 2, len(tags))
	sort.Slice(tags, func(i, j int) bool {
		return aws.StringValue(tags[i].Key) < aws.StringValue(tags[j].Key)
	})

	assert.Equal(t, aws.StringValue(tags[0].Key), tagKey1)
	assert.Equal(t, aws.StringValue(tags[0].Value), tagValue1)
	assert.Equal(t, aws.StringValue(tags[1].Key), tagKey2)
	assert.Equal(t, aws.StringValue(tags[1].Value), tagValue2)
}

func TestAllocENI(t *testing.T) {
	ctrl, _, mockEC2 := setup(t)
	defer ctrl.Finish()

	cureniID := eniID
	eni := ec2.CreateNetworkInterfaceOutput{NetworkInterface: &ec2.NetworkInterface{NetworkInterfaceId: &cureniID}}
	mockEC2.EXPECT().CreateNetworkInterfaceWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(&eni, nil)

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

	mockEC2.EXPECT().DescribeInstancesWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(result, nil)
	attachmentID := "eni-attach-58ddda9d"
	attachResult := &ec2.AttachNetworkInterfaceOutput{
		AttachmentId: &attachmentID}
	mockEC2.EXPECT().AttachNetworkInterfaceWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(attachResult, nil)
	mockEC2.EXPECT().CreateTagsWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)
	mockEC2.EXPECT().ModifyNetworkInterfaceAttributeWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)

	ins := &EC2InstanceMetadataCache{ec2SVC: mockEC2}
	_, err := ins.AllocENI(false, nil, "")
	assert.NoError(t, err)
}

func TestAllocENINoFreeDevice(t *testing.T) {
	ctrl, _, mockEC2 := setup(t)
	defer ctrl.Finish()

	cureniID := eniID
	eni := ec2.CreateNetworkInterfaceOutput{NetworkInterface: &ec2.NetworkInterface{NetworkInterfaceId: &cureniID}}
	mockEC2.EXPECT().CreateNetworkInterfaceWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(&eni, nil)

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

	mockEC2.EXPECT().DescribeInstancesWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(result, nil)
	mockEC2.EXPECT().DeleteNetworkInterfaceWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)

	ins := &EC2InstanceMetadataCache{ec2SVC: mockEC2}
	_, err := ins.AllocENI(false, nil, "")
	assert.Error(t, err)
}

func TestAllocENIMaxReached(t *testing.T) {
	ctrl, _, mockEC2 := setup(t)
	defer ctrl.Finish()

	cureniID := eniID
	eni := ec2.CreateNetworkInterfaceOutput{NetworkInterface: &ec2.NetworkInterface{NetworkInterfaceId: &cureniID}}
	mockEC2.EXPECT().CreateNetworkInterfaceWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(&eni, nil)

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

	mockEC2.EXPECT().DescribeInstancesWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(result, nil)
	mockEC2.EXPECT().AttachNetworkInterfaceWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("AttachmentLimitExceeded"))
	mockEC2.EXPECT().DeleteNetworkInterfaceWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)

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
	mockEC2.EXPECT().DescribeNetworkInterfacesWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(result, nil)
	mockEC2.EXPECT().DetachNetworkInterfaceWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)
	mockEC2.EXPECT().DeleteNetworkInterfaceWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)

	ins := &EC2InstanceMetadataCache{ec2SVC: mockEC2}
	err := ins.freeENI("test-eni", time.Millisecond, time.Millisecond)
	assert.NoError(t, err)
}

func TestFreeENIRetry(t *testing.T) {
	ctrl, _, mockEC2 := setup(t)
	defer ctrl.Finish()

	attachmentID := eniAttachID
	attachment := &ec2.NetworkInterfaceAttachment{AttachmentId: &attachmentID}
	result := &ec2.DescribeNetworkInterfacesOutput{
		NetworkInterfaces: []*ec2.NetworkInterface{{Attachment: attachment}}}
	mockEC2.EXPECT().DescribeNetworkInterfacesWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(result, nil)

	// retry 2 times
	mockEC2.EXPECT().DetachNetworkInterfaceWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)
	mockEC2.EXPECT().DeleteNetworkInterfaceWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("testing retrying delete"))
	mockEC2.EXPECT().DeleteNetworkInterfaceWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)

	ins := &EC2InstanceMetadataCache{ec2SVC: mockEC2}
	err := ins.freeENI("test-eni", time.Millisecond, time.Millisecond)
	assert.NoError(t, err)
}

func TestFreeENIRetryMax(t *testing.T) {
	ctrl, _, mockEC2 := setup(t)
	defer ctrl.Finish()

	attachmentID := eniAttachID
	attachment := &ec2.NetworkInterfaceAttachment{AttachmentId: &attachmentID}
	result := &ec2.DescribeNetworkInterfacesOutput{
		NetworkInterfaces: []*ec2.NetworkInterface{{Attachment: attachment}}}
	mockEC2.EXPECT().DescribeNetworkInterfacesWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(result, nil)
	mockEC2.EXPECT().DetachNetworkInterfaceWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)

	for i := 0; i < maxENIDeleteRetries; i++ {
		mockEC2.EXPECT().DeleteNetworkInterfaceWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("testing retrying delete"))
	}

	ins := &EC2InstanceMetadataCache{ec2SVC: mockEC2}
	err := ins.freeENI("test-eni", time.Millisecond, time.Millisecond)
	assert.Error(t, err)
}

func TestFreeENIDescribeErr(t *testing.T) {
	ctrl, _, mockEC2 := setup(t)
	defer ctrl.Finish()

	mockEC2.EXPECT().DescribeNetworkInterfacesWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("Error on DescribeNetworkInterfacesWithContext"))

	ins := &EC2InstanceMetadataCache{ec2SVC: mockEC2}
	err := ins.FreeENI("test-eni")
	assert.Error(t, err)
}

func TestDescribeInstanceTypes(t *testing.T) {
	ctrl, _, mockEC2 := setup(t)
	defer ctrl.Finish()
	mockEC2.EXPECT().DescribeInstanceTypesWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(&ec2.DescribeInstanceTypesOutput{
		InstanceTypes: []*ec2.InstanceTypeInfo{
			{InstanceType: aws.String("not-there"), NetworkInfo: &ec2.NetworkInfo{
				MaximumNetworkInterfaces:  aws.Int64(9),
				Ipv4AddressesPerInterface: aws.Int64(99)},
			},
		},
		NextToken: nil,
	}, nil)

	ins := &EC2InstanceMetadataCache{ec2SVC: mockEC2}
	ins.instanceType = "not-there"
	value, err := ins.GetENILimit()
	assert.NoError(t, err)
	assert.Equal(t, 9, value)
	assert.Equal(t, 99, InstanceIPsAvailable[ins.instanceType])
}

func TestAllocIPAddress(t *testing.T) {
	ctrl, _, mockEC2 := setup(t)
	defer ctrl.Finish()

	mockEC2.EXPECT().AssignPrivateIpAddressesWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(&ec2.AssignPrivateIpAddressesOutput{}, nil)

	ins := &EC2InstanceMetadataCache{ec2SVC: mockEC2}
	err := ins.AllocIPAddress("eni-id")
	assert.NoError(t, err)
}

func TestAllocIPAddressOnErr(t *testing.T) {
	ctrl, _, mockEC2 := setup(t)
	defer ctrl.Finish()

	mockEC2.EXPECT().AssignPrivateIpAddressesWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("Error on AssignPrivateIpAddressesWithContext"))

	ins := &EC2InstanceMetadataCache{ec2SVC: mockEC2}
	err := ins.AllocIPAddress("eni-id")
	assert.Error(t, err)
}

func TestAllocIPAddresses(t *testing.T) {
	ctrl, _, mockEC2 := setup(t)
	defer ctrl.Finish()

	// when required IP numbers(5) is below ENI's limit(30)
	input := &ec2.AssignPrivateIpAddressesInput{
		NetworkInterfaceId:             aws.String("eni-id"),
		SecondaryPrivateIpAddressCount: aws.Int64(5),
	}
	mockEC2.EXPECT().AssignPrivateIpAddressesWithContext(gomock.Any(), input, gomock.Any()).Return(nil, nil)

	ins := &EC2InstanceMetadataCache{ec2SVC: mockEC2, instanceType: "c5n.18xlarge"}
	err := ins.AllocIPAddresses("eni-id", 5)
	assert.NoError(t, err)

	// when required IP numbers(50) is higher than ENI's limit(49)
	input = &ec2.AssignPrivateIpAddressesInput{
		NetworkInterfaceId:             aws.String("eni-id"),
		SecondaryPrivateIpAddressCount: aws.Int64(49),
	}
	mockEC2.EXPECT().AssignPrivateIpAddressesWithContext(gomock.Any(), input, gomock.Any()).Return(nil, nil)

	ins = &EC2InstanceMetadataCache{ec2SVC: mockEC2, instanceType: "c5n.18xlarge"}
	err = ins.AllocIPAddresses("eni-id", 50)
	assert.NoError(t, err)

	// Adding 0 should do nothing
	err = ins.AllocIPAddresses("eni-id", 0)
	assert.NoError(t, err)
}

func TestEC2InstanceMetadataCache_getFilteredListOfNetworkInterfaces_OneResult(t *testing.T) {
	ctrl, _, mockEC2 := setup(t)
	defer ctrl.Finish()

	attachmentID := eniAttachID
	description := eniDescriptionPrefix + "test"
	status := "available"
	tagKey := eniNodeTagKey
	tag := ec2.Tag{Key: &tagKey}
	attachment := &ec2.NetworkInterfaceAttachment{AttachmentId: &attachmentID}
	result := &ec2.DescribeNetworkInterfacesOutput{
		NetworkInterfaces: []*ec2.NetworkInterface{{Attachment: attachment, Status: &status, TagSet: []*ec2.Tag{&tag}, Description: &description}}}
	mockEC2.EXPECT().DescribeNetworkInterfacesWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(result, nil)

	ins := &EC2InstanceMetadataCache{ec2SVC: mockEC2}
	got, err := ins.getFilteredListOfNetworkInterfaces()
	assert.NotNil(t, got)
	assert.NoError(t, err)
}

func TestEC2InstanceMetadataCache_getFilteredListOfNetworkInterfaces_NoResult(t *testing.T) {
	ctrl, _, mockEC2 := setup(t)
	defer ctrl.Finish()

	result := &ec2.DescribeNetworkInterfacesOutput{
		NetworkInterfaces: []*ec2.NetworkInterface{}}
	mockEC2.EXPECT().DescribeNetworkInterfacesWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(result, nil)

	ins := &EC2InstanceMetadataCache{ec2SVC: mockEC2}
	got, err := ins.getFilteredListOfNetworkInterfaces()
	assert.Nil(t, got)
	assert.NoError(t, err)
}

func TestEC2InstanceMetadataCache_getFilteredListOfNetworkInterfaces_Error(t *testing.T) {
	ctrl, _, mockEC2 := setup(t)
	defer ctrl.Finish()

	mockEC2.EXPECT().DescribeNetworkInterfacesWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("dummy error"))

	ins := &EC2InstanceMetadataCache{ec2SVC: mockEC2}
	got, err := ins.getFilteredListOfNetworkInterfaces()
	assert.Nil(t, got)
	assert.Error(t, err)
}
