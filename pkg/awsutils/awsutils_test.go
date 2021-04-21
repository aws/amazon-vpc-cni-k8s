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
	"errors"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"

	mock_ec2wrapper "github.com/aws/amazon-vpc-cni-k8s/pkg/ec2wrapper/mocks"
)

const (
	metadataMACPath      = "network/interfaces/macs/"
	metadataMAC          = "mac"
	metadataAZ           = "placement/availability-zone"
	metadataLocalIP      = "local-ipv4"
	metadataInstanceID   = "instance-id"
	metadataInstanceType = "instance-type"
	metadataSGs          = "/security-group-ids"
	metadataSubnetID     = "/subnet-id"
	metadataVPCcidrs     = "/vpc-ipv4-cidr-blocks"
	metadataDeviceNum    = "/device-number"
	metadataInterface    = "/interface-id"
	metadataSubnetCIDR   = "/subnet-ipv4-cidr-block"
	metadataIPv4s        = "/local-ipv4s"
	metadataIPv4Prefixes = "/ipv4-prefix"

	az                   = "us-east-1a"
	localIP              = "10.0.0.10"
	instanceID           = "i-0e1f3b9eb950e4980"
	instanceType         = "c1.medium"
	primaryMAC           = "12:ef:2a:98:e5:5a"
	eni2MAC              = "12:ef:2a:98:e5:5b"
	sg1                  = "sg-2e080f50"
	sg2                  = "sg-2e080f51"
	sgs                  = sg1 + " " + sg2
	subnetID             = "subnet-6b245523"
	subnetCIDR           = "10.0.1.0/24"
	primaryeniID         = "eni-00000000"
	eniID                = primaryeniID
	eniAttachID          = "eni-attach-beb21856"
	eni1Device           = "0"
	eni1PrivateIP        = "10.0.0.1"
	eni2Device           = "1"
	eni2PrivateIP        = "10.0.0.2"
	eni2ID               = "eni-12341234"
	metadataVPCIPv4CIDRs = "192.168.0.0/16	100.66.0.0/1"
)

func testMetadata(overrides map[string]interface{}) FakeIMDS {
	data := map[string]interface{}{
		metadataAZ:           az,
		metadataLocalIP:      localIP,
		metadataInstanceID:   instanceID,
		metadataInstanceType: instanceType,
		metadataMAC:          primaryMAC,
		metadataMACPath:      primaryMAC,
		metadataMACPath + primaryMAC + metadataDeviceNum:  eni1Device,
		metadataMACPath + primaryMAC + metadataInterface:  primaryeniID,
		metadataMACPath + primaryMAC + metadataSGs:        sgs,
		metadataMACPath + primaryMAC + metadataIPv4s:      eni1PrivateIP,
		metadataMACPath + primaryMAC + metadataSubnetID:   subnetID,
		metadataMACPath + primaryMAC + metadataSubnetCIDR: subnetCIDR,
		metadataMACPath + primaryMAC + metadataVPCcidrs:   metadataVPCIPv4CIDRs,
	}

	for k, v := range overrides {
		data[k] = v
	}

	return FakeIMDS(data)
}

func setup(t *testing.T) (*gomock.Controller,
	*mock_ec2wrapper.MockEC2) {
	ctrl := gomock.NewController(t)
	return ctrl,
		mock_ec2wrapper.NewMockEC2(ctrl)
}

func TestInitWithEC2metadata(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	ctrl, mockEC2 := setup(t)
	defer ctrl.Finish()
	mockMetadata := testMetadata(nil)

	ins := &EC2InstanceMetadataCache{imds: TypedIMDS{mockMetadata}, ec2SVC: mockEC2}
	err := ins.initWithEC2Metadata(ctx)
	if assert.NoError(t, err) {
		assert.Equal(t, az, ins.availabilityZone)
		assert.Equal(t, localIP, ins.localIPv4.String())
		assert.Equal(t, ins.instanceID, instanceID)
		assert.Equal(t, ins.primaryENImac, primaryMAC)
		assert.Equal(t, ins.primaryENI, primaryeniID)
		assert.Equal(t, subnetID, ins.subnetID)
	}
}

func TestInitWithEC2metadataErr(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	ctrl, mockEC2 := setup(t)
	defer ctrl.Finish()

	var keys []string
	for k := range testMetadata(nil) {
		keys = append(keys, k)
	}

	for _, key := range keys {
		mockMetadata := testMetadata(map[string]interface{}{
			key: fmt.Errorf("An error with %s", key),
		})

		ins := &EC2InstanceMetadataCache{imds: TypedIMDS{mockMetadata}, ec2SVC: mockEC2}

		// This test is a bit silly.  We expect broken metadata to result in an err return here.  But if the code is resilient and _succeeds_, then of course that's ok too.  Mostly we just want it not to panic.
		assert.NotPanics(t, func() {
			_ = ins.initWithEC2Metadata(ctx)
		}, "Broken metadata %s resulted in panic", key)
	}
}

func TestGetAttachedENIs(t *testing.T) {
	mockMetadata := testMetadata(map[string]interface{}{
		metadataMACPath: primaryMAC + " " + eni2MAC,
		metadataMACPath + eni2MAC + metadataDeviceNum:    eni2Device,
		metadataMACPath + eni2MAC + metadataInterface:    eni2ID,
		metadataMACPath + eni2MAC + metadataSubnetCIDR:   subnetCIDR,
		metadataMACPath + eni2MAC + metadataIPv4s:        eni2PrivateIP,
		metadataMACPath + eni2MAC + metadataIPv4Prefixes: nil,
	})

	ins := &EC2InstanceMetadataCache{imds: TypedIMDS{mockMetadata}}
	ens, err := ins.GetAttachedENIs()
	if assert.NoError(t, err) {
		assert.Equal(t, len(ens), 2)
	}
}

func TestAWSGetFreeDeviceNumberOnErr(t *testing.T) {
	ctrl, mockEC2 := setup(t)
	defer ctrl.Finish()

	// test error handling
	mockEC2.EXPECT().DescribeInstancesWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("error on DescribeInstancesWithContext"))

	ins := &EC2InstanceMetadataCache{ec2SVC: mockEC2}
	_, err := ins.awsGetFreeDeviceNumber()
	assert.Error(t, err)
}

func TestAWSGetFreeDeviceNumberNoDevice(t *testing.T) {
	ctrl, mockEC2 := setup(t)
	defer ctrl.Finish()

	// test no free index
	ec2ENIs := make([]*ec2.InstanceNetworkInterface, 0)

	for i := 0; i < maxENIs; i++ {
		var deviceNums [maxENIs]int64
		deviceNums[i] = int64(i)
		ec2ENI := &ec2.InstanceNetworkInterface{Attachment: &ec2.InstanceNetworkInterfaceAttachment{DeviceIndex: &deviceNums[i]}}
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
	ctrl, mockEC2 := setup(t)
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
	ctrl, mockEC2 := setup(t)
	defer ctrl.Finish()

	result := &ec2.DescribeNetworkInterfacesOutput{
		NetworkInterfaces: []*ec2.NetworkInterface{{
			TagSet: []*ec2.Tag{
				{Key: aws.String("foo"), Value: aws.String("foo-value")},
			},
			Attachment: &ec2.NetworkInterfaceAttachment{
				NetworkCardIndex: aws.Int64(0),
			},
		}},
	}

	expectedError := awserr.New("InvalidNetworkInterfaceID.NotFound", "no 'eni-xxx'", nil)
	noMessageError := awserr.New("InvalidNetworkInterfaceID.NotFound", "no message", nil)
	err := errors.New("other Error")

	testCases := []struct {
		name    string
		exptags map[string]TagMap
		n       int
		awsErr  error
		expErr  error
	}{
		{"Success DescribeENI", map[string]TagMap{"": {"foo": "foo-value"}}, 1, nil, nil},
		{"Not found error", nil, maxENIEC2APIRetries, awserr.New("InvalidNetworkInterfaceID.NotFound", "no 'eni-xxx'", nil), expectedError},
		{"Not found, no message", nil, maxENIEC2APIRetries, awserr.New("InvalidNetworkInterfaceID.NotFound", "no message", nil), noMessageError},
		{"Other error", nil, maxENIEC2APIRetries, err, err},
	}

	mockMetadata := testMetadata(nil)

	for _, tc := range testCases {
		mockEC2.EXPECT().DescribeNetworkInterfacesWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Times(tc.n).Return(result, tc.awsErr)
		ins := &EC2InstanceMetadataCache{imds: TypedIMDS{mockMetadata}, ec2SVC: mockEC2}
		metaData, err := ins.DescribeAllENIs()
		assert.Equal(t, tc.expErr, err, tc.name)
		assert.Equal(t, tc.exptags, metaData.TagMap, tc.name)
	}
}

func TestTagEni(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()
	ctrl, mockEC2 := setup(t)
	defer ctrl.Finish()
	mockMetadata := testMetadata(nil)

	ins := &EC2InstanceMetadataCache{imds: TypedIMDS{mockMetadata}, ec2SVC: mockEC2}

	err := ins.initWithEC2Metadata(ctx)
	assert.NoError(t, err)
	mockEC2.EXPECT().CreateTagsWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("tagging failed"))
	mockEC2.EXPECT().CreateTagsWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("tagging failed"))
	mockEC2.EXPECT().CreateTagsWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("tagging failed"))
	mockEC2.EXPECT().CreateTagsWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("tagging failed"))
	mockEC2.EXPECT().CreateTagsWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)

	ins.tagENI(eniID, time.Millisecond)
	assert.NoError(t, err)
}

func TestClusterNameTag(t *testing.T) {
	ctrl, mockEC2 := setup(t)
	defer ctrl.Finish()
	_ = os.Setenv(clusterNameEnvVar, "cni-test")
	tagKey1 := eniClusterTagKey
	tagValue1 := "cni-test"
	additionalEniTags := ec2.Tag{
		Key:   &tagKey1,
		Value: &tagValue1,
	}
	tags := []*ec2.Tag{
		{
			Key:   aws.String(eniNodeTagKey),
			Value: aws.String(instanceID),
		},
	}
	tags = append(tags, &additionalEniTags)
	input := &ec2.CreateTagsInput{
		Resources: []*string{
			aws.String(eniID),
		},
		Tags: tags,
	}

	ins := &EC2InstanceMetadataCache{instanceID: instanceID, ec2SVC: mockEC2}
	mockEC2.EXPECT().CreateTagsWithContext(gomock.Any(), input, gomock.Any()).Return(nil, nil)
	ins.tagENI(eniID, time.Millisecond)
	_ = os.Unsetenv(clusterNameEnvVar)
}

func TestAdditionalTagsEni(t *testing.T) {
	ctrl, mockEC2 := setup(t)
	defer ctrl.Finish()
	_ = os.Setenv(additionalEniTagsEnvVar, `{"testKey": "testing"}`)
	tagKey1 := "testKey"
	tagValue1 := "testing"
	additionalEniTags := ec2.Tag{
		Key:   &tagKey1,
		Value: &tagValue1,
	}
	tags := []*ec2.Tag{
		{
			Key:   aws.String(eniNodeTagKey),
			Value: aws.String(instanceID),
		},
	}
	tags = append(tags, &additionalEniTags)
	input := &ec2.CreateTagsInput{
		Resources: []*string{
			aws.String(eniID),
		},
		Tags: tags,
	}

	ins := &EC2InstanceMetadataCache{instanceID: instanceID, ec2SVC: mockEC2}
	mockEC2.EXPECT().CreateTagsWithContext(gomock.Any(), input, gomock.Any()).Return(nil, nil)
	ins.tagENI(eniID, time.Millisecond)
	os.Unsetenv(additionalEniTagsEnvVar)
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
	ctrl, mockEC2 := setup(t)
	defer ctrl.Finish()

	mockMetadata := testMetadata(nil)

	cureniID := eniID
	eni := ec2.CreateNetworkInterfaceOutput{NetworkInterface: &ec2.NetworkInterface{NetworkInterfaceId: &cureniID}}
	mockEC2.EXPECT().CreateNetworkInterfaceWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(&eni, nil)

	// 2 ENIs, uses device number 0 3, expect to find free at 1
	ec2ENIs := make([]*ec2.InstanceNetworkInterface, 0)
	deviceNum1 := int64(0)
	ec2ENI := &ec2.InstanceNetworkInterface{Attachment: &ec2.InstanceNetworkInterfaceAttachment{DeviceIndex: &deviceNum1}}
	ec2ENIs = append(ec2ENIs, ec2ENI)

	deviceNum2 := int64(3)
	ec2ENI = &ec2.InstanceNetworkInterface{Attachment: &ec2.InstanceNetworkInterfaceAttachment{DeviceIndex: &deviceNum2}}
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

	ins := &EC2InstanceMetadataCache{
		ec2SVC: mockEC2,
		imds:   TypedIMDS{mockMetadata},
	}
	_, err := ins.AllocENI(false, nil, "")
	assert.NoError(t, err)
}

func TestAllocENINoFreeDevice(t *testing.T) {
	ctrl, mockEC2 := setup(t)
	defer ctrl.Finish()

	mockMetadata := testMetadata(nil)

	cureniID := eniID
	eni := ec2.CreateNetworkInterfaceOutput{NetworkInterface: &ec2.NetworkInterface{NetworkInterfaceId: &cureniID}}
	mockEC2.EXPECT().CreateNetworkInterfaceWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(&eni, nil)

	// test no free index
	ec2ENIs := make([]*ec2.InstanceNetworkInterface, 0)

	for i := 0; i < maxENIs; i++ {
		var deviceNums [maxENIs]int64
		deviceNums[i] = int64(i)
		ec2ENI := &ec2.InstanceNetworkInterface{Attachment: &ec2.InstanceNetworkInterfaceAttachment{DeviceIndex: &deviceNums[i]}}
		ec2ENIs = append(ec2ENIs, ec2ENI)
	}
	result := &ec2.DescribeInstancesOutput{
		Reservations: []*ec2.Reservation{{Instances: []*ec2.Instance{{NetworkInterfaces: ec2ENIs}}}}}

	mockEC2.EXPECT().DescribeInstancesWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(result, nil)
	mockEC2.EXPECT().DeleteNetworkInterfaceWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)

	ins := &EC2InstanceMetadataCache{
		ec2SVC: mockEC2,
		imds:   TypedIMDS{mockMetadata},
	}
	_, err := ins.AllocENI(false, nil, "")
	assert.Error(t, err)
}

func TestAllocENIMaxReached(t *testing.T) {
	ctrl, mockEC2 := setup(t)
	defer ctrl.Finish()

	mockMetadata := testMetadata(nil)

	cureniID := eniID
	eni := ec2.CreateNetworkInterfaceOutput{NetworkInterface: &ec2.NetworkInterface{NetworkInterfaceId: &cureniID}}
	mockEC2.EXPECT().CreateNetworkInterfaceWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(&eni, nil)

	// 2 ENIs, uses device number 0 3, expect to find free at 1
	ec2ENIs := make([]*ec2.InstanceNetworkInterface, 0)
	deviceNum1 := int64(0)
	ec2ENI := &ec2.InstanceNetworkInterface{Attachment: &ec2.InstanceNetworkInterfaceAttachment{DeviceIndex: &deviceNum1}}
	ec2ENIs = append(ec2ENIs, ec2ENI)

	deviceNum2 := int64(3)
	ec2ENI = &ec2.InstanceNetworkInterface{Attachment: &ec2.InstanceNetworkInterfaceAttachment{DeviceIndex: &deviceNum2}}
	ec2ENIs = append(ec2ENIs, ec2ENI)

	result := &ec2.DescribeInstancesOutput{
		Reservations: []*ec2.Reservation{{Instances: []*ec2.Instance{{NetworkInterfaces: ec2ENIs}}}}}

	mockEC2.EXPECT().DescribeInstancesWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(result, nil)
	mockEC2.EXPECT().AttachNetworkInterfaceWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("AttachmentLimitExceeded"))
	mockEC2.EXPECT().DeleteNetworkInterfaceWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)

	ins := &EC2InstanceMetadataCache{
		ec2SVC: mockEC2,
		imds:   TypedIMDS{mockMetadata},
	}
	_, err := ins.AllocENI(false, nil, "")
	assert.Error(t, err)
}

func TestFreeENI(t *testing.T) {
	ctrl, mockEC2 := setup(t)
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
	ctrl, mockEC2 := setup(t)
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
	ctrl, mockEC2 := setup(t)
	defer ctrl.Finish()

	attachmentID := eniAttachID
	attachment := &ec2.NetworkInterfaceAttachment{AttachmentId: &attachmentID}
	result := &ec2.DescribeNetworkInterfacesOutput{
		NetworkInterfaces: []*ec2.NetworkInterface{{Attachment: attachment}}}
	mockEC2.EXPECT().DescribeNetworkInterfacesWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(result, nil)
	mockEC2.EXPECT().DetachNetworkInterfaceWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)

	for i := 0; i < maxENIEC2APIRetries; i++ {
		mockEC2.EXPECT().DeleteNetworkInterfaceWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("testing retrying delete"))
	}

	ins := &EC2InstanceMetadataCache{ec2SVC: mockEC2}
	err := ins.freeENI("test-eni", time.Millisecond, time.Millisecond)
	assert.Error(t, err)
}

func TestFreeENIDescribeErr(t *testing.T) {
	ctrl, mockEC2 := setup(t)
	defer ctrl.Finish()

	mockEC2.EXPECT().DescribeNetworkInterfacesWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("Error on DescribeNetworkInterfacesWithContext"))

	ins := &EC2InstanceMetadataCache{ec2SVC: mockEC2}
	err := ins.FreeENI("test-eni")
	assert.Error(t, err)
}

func TestDescribeInstanceTypes(t *testing.T) {
	ctrl, mockEC2 := setup(t)
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
	pv4Limit, err := ins.GetENIIPv4Limit()
	assert.NoError(t, err)
	assert.Equal(t, 98, pv4Limit)
}

func TestAllocIPAddress(t *testing.T) {
	ctrl, mockEC2 := setup(t)
	defer ctrl.Finish()

	mockEC2.EXPECT().AssignPrivateIpAddressesWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(&ec2.AssignPrivateIpAddressesOutput{}, nil)

	ins := &EC2InstanceMetadataCache{ec2SVC: mockEC2}
	err := ins.AllocIPAddress("eni-id")
	assert.NoError(t, err)
}

func TestAllocIPAddressOnErr(t *testing.T) {
	ctrl, mockEC2 := setup(t)
	defer ctrl.Finish()

	mockEC2.EXPECT().AssignPrivateIpAddressesWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("Error on AssignPrivateIpAddressesWithContext"))

	ins := &EC2InstanceMetadataCache{ec2SVC: mockEC2}
	err := ins.AllocIPAddress("eni-id")
	assert.Error(t, err)
}

func TestAllocIPAddresses(t *testing.T) {
	ctrl, mockEC2 := setup(t)
	defer ctrl.Finish()

	// when required IP numbers(5) is below ENI's limit(30)
	input := &ec2.AssignPrivateIpAddressesInput{
		NetworkInterfaceId:             aws.String(eniID),
		SecondaryPrivateIpAddressCount: aws.Int64(5),
	}
	mockEC2.EXPECT().AssignPrivateIpAddressesWithContext(gomock.Any(), input, gomock.Any()).Return(nil, nil)

	ins := &EC2InstanceMetadataCache{ec2SVC: mockEC2, instanceType: "c5n.18xlarge"}
	err := ins.AllocIPAddresses(eniID, 5)
	assert.NoError(t, err)

	// when required IP numbers(50) is higher than ENI's limit(49)
	input = &ec2.AssignPrivateIpAddressesInput{
		NetworkInterfaceId:             aws.String(eniID),
		SecondaryPrivateIpAddressCount: aws.Int64(49),
	}
	addresses := make([]*ec2.AssignedPrivateIpAddress, 49)
	output := ec2.AssignPrivateIpAddressesOutput{
		AssignedPrivateIpAddresses: addresses,
		NetworkInterfaceId:         aws.String(eniID),
	}
	mockEC2.EXPECT().AssignPrivateIpAddressesWithContext(gomock.Any(), input, gomock.Any()).Return(&output, nil)

	ins = &EC2InstanceMetadataCache{ec2SVC: mockEC2, instanceType: "c5n.18xlarge"}
	err = ins.AllocIPAddresses(eniID, 50)
	assert.NoError(t, err)

	// Adding 0 should do nothing
	err = ins.AllocIPAddresses(eniID, 0)
	assert.NoError(t, err)
}

func TestAllocIPAddressesAlreadyFull(t *testing.T) {
	ctrl, mockEC2 := setup(t)
	defer ctrl.Finish()
	// The required IP numbers(14) is the ENI's limit(14)
	input := &ec2.AssignPrivateIpAddressesInput{
		NetworkInterfaceId:             aws.String(eniID),
		SecondaryPrivateIpAddressCount: aws.Int64(14),
	}
	ins := &EC2InstanceMetadataCache{ec2SVC: mockEC2, instanceType: "t3.xlarge"}

	retErr := awserr.New("PrivateIpAddressLimitExceeded", "Too many IPs already allocated", nil)
	mockEC2.EXPECT().AssignPrivateIpAddressesWithContext(gomock.Any(), input, gomock.Any()).Return(nil, retErr)
	// If EC2 says that all IPs are already attached, we do nothing
	err := ins.AllocIPAddresses(eniID, 14)
	assert.NoError(t, err)
}

func TestEC2InstanceMetadataCache_getFilteredListOfNetworkInterfaces_OneResult(t *testing.T) {
	ctrl, mockEC2 := setup(t)
	defer ctrl.Finish()

	attachmentID := eniAttachID
	description := eniDescriptionPrefix + "test"
	status := "available"

	tag := []*ec2.Tag{
		{
			Key:   aws.String(eniNodeTagKey),
			Value: aws.String("test"),
		},
	}

	timein := time.Now().Local().Add(time.Minute * time.Duration(-10))

	tag = append(tag, &ec2.Tag{
		Key:   aws.String(eniCreatedAtTagKey),
		Value: aws.String(timein.Format(time.RFC3339)),
	})
	attachment := &ec2.NetworkInterfaceAttachment{AttachmentId: &attachmentID}
	cureniID := eniID

	interfaces := []*ec2.NetworkInterface{{Attachment: attachment, Status: &status, TagSet: tag, Description: &description, NetworkInterfaceId: &cureniID}}
	setupDescribeNetworkInterfacesPagesWithContextMock(t, mockEC2, interfaces, nil, 1)
	ins := &EC2InstanceMetadataCache{ec2SVC: mockEC2}
	got, err := ins.getFilteredListOfNetworkInterfaces()
	assert.NotNil(t, got)
	assert.NoError(t, err)
}

func TestEC2InstanceMetadataCache_getFilteredListOfNetworkInterfaces_NoResult(t *testing.T) {
	ctrl, mockEC2 := setup(t)
	defer ctrl.Finish()

	setupDescribeNetworkInterfacesPagesWithContextMock(t, mockEC2, []*ec2.NetworkInterface{}, nil, 1)
	ins := &EC2InstanceMetadataCache{ec2SVC: mockEC2}
	got, err := ins.getFilteredListOfNetworkInterfaces()
	assert.Nil(t, got)
	assert.NoError(t, err)
}

func TestEC2InstanceMetadataCache_getFilteredListOfNetworkInterfaces_Error(t *testing.T) {
	ctrl, mockEC2 := setup(t)
	defer ctrl.Finish()

	interfaces := []*ec2.NetworkInterface{{
		TagSet: []*ec2.Tag{
			{Key: aws.String("foo"), Value: aws.String("foo-value")},
		},
	}}
	setupDescribeNetworkInterfacesPagesWithContextMock(t, mockEC2, interfaces, errors.New("dummy error"), 1)

	ins := &EC2InstanceMetadataCache{ec2SVC: mockEC2}
	got, err := ins.getFilteredListOfNetworkInterfaces()
	assert.Nil(t, got)
	assert.Error(t, err)
}

func Test_badENIID(t *testing.T) {
	tests := []struct {
		name   string
		errMsg string
		want   string
	}{
		{"Just a regular string", "Just a string", ""},
		{"Actual error message", "The networkInterface ID 'eni-00000088' does not exist", "eni-00000088"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := badENIID(tt.errMsg); got != tt.want {
				t.Errorf("badENIID() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEC2InstanceMetadataCache_waitForENIAndIPsAttached(t *testing.T) {
	type args struct {
		eni                        string
		foundSecondaryIPs          int
		wantedSecondaryIPs         int
		maxBackoffDelay            time.Duration
		times                      int
		enableIpv4PrefixDelegation bool
	}
	eni1Metadata := ENIMetadata{
		ENIID:         eniID,
		IPv4Addresses: nil,
		IPv4Prefixes:  nil,
	}
	isPrimary := true
	notPrimary := false
	primaryIP := eni2PrivateIP
	secondaryIP1 := primaryIP + "0"
	secondaryIP2 := primaryIP + "1"
	eni2Metadata := ENIMetadata{
		ENIID:          eni2ID,
		MAC:            eni2MAC,
		DeviceNumber:   1,
		SubnetIPv4CIDR: subnetCIDR,
		IPv4Addresses: []*ec2.NetworkInterfacePrivateIpAddress{
			{
				Primary:          &isPrimary,
				PrivateIpAddress: &primaryIP,
			}, {
				Primary:          &notPrimary,
				PrivateIpAddress: &secondaryIP1,
			}, {
				Primary:          &notPrimary,
				PrivateIpAddress: &secondaryIP2,
			},
		},
		IPv4Prefixes: make([]*ec2.Ipv4PrefixSpecification, 0),
	}
	eniList := []ENIMetadata{eni1Metadata, eni2Metadata}
	tests := []struct {
		name            string
		args            args
		wantEniMetadata ENIMetadata
		wantErr         bool
	}{
		{"Test wait success", args{eni: eni2ID, foundSecondaryIPs: 2, wantedSecondaryIPs: 2, maxBackoffDelay: 5 * time.Millisecond, times: 1}, eniList[1], false},
		{"Test partial success", args{eni: eni2ID, foundSecondaryIPs: 2, wantedSecondaryIPs: 12, maxBackoffDelay: 5 * time.Millisecond, times: maxENIEC2APIRetries}, eniList[1], false},
		{"Test wait fail", args{eni: eni2ID, foundSecondaryIPs: 0, wantedSecondaryIPs: 12, maxBackoffDelay: 5 * time.Millisecond, times: maxENIEC2APIRetries}, ENIMetadata{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl, mockEC2 := setup(t)
			defer ctrl.Finish()
			eniIPs := eni2PrivateIP
			for i := 0; i < tt.args.foundSecondaryIPs; i++ {
				eniIPs += " " + eni2PrivateIP + strconv.Itoa(i)
			}
			fmt.Println("eniips", eniIPs)
			mockMetadata := testMetadata(map[string]interface{}{
				metadataMACPath: primaryMAC + " " + eni2MAC,
				metadataMACPath + eni2MAC + metadataDeviceNum:  eni2Device,
				metadataMACPath + eni2MAC + metadataInterface:  eni2ID,
				metadataMACPath + eni2MAC + metadataSubnetCIDR: subnetCIDR,
				metadataMACPath + eni2MAC + metadataIPv4s:      eniIPs,
			})
			cache := &EC2InstanceMetadataCache{imds: TypedIMDS{mockMetadata}, ec2SVC: mockEC2}
			gotEniMetadata, err := cache.waitForENIAndIPsAttached(tt.args.eni, tt.args.wantedSecondaryIPs, tt.args.maxBackoffDelay)
			if (err != nil) != tt.wantErr {
				t.Errorf("waitForENIAndIPsAttached() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(gotEniMetadata, tt.wantEniMetadata) {
				t.Errorf("waitForENIAndIPsAttached() gotEniMetadata = %v, want %v", gotEniMetadata, tt.wantEniMetadata)
			}
		})
	}
}

func TestEC2InstanceMetadataCache_SetUnmanagedENIs(t *testing.T) {
	mockMetadata := testMetadata(nil)
	ins := &EC2InstanceMetadataCache{imds: TypedIMDS{mockMetadata}}
	ins.SetUnmanagedENIs(nil)
	assert.False(t, ins.IsUnmanagedENI("eni-1"))
	ins.SetUnmanagedENIs([]string{"eni-1", "eni-2"})
	assert.True(t, ins.IsUnmanagedENI("eni-1"))
	assert.False(t, ins.IsUnmanagedENI("eni-99"))
	ins.SetUnmanagedENIs(nil)
	assert.False(t, ins.IsUnmanagedENI("eni-1"))
}

func TestEC2InstanceMetadataCache_cleanUpLeakedENIsInternal(t *testing.T) {
	ctrl, mockEC2 := setup(t)
	defer ctrl.Finish()

	description := eniDescriptionPrefix + "test"
	interfaces := []*ec2.NetworkInterface{{
		Description: &description,
		TagSet: []*ec2.Tag{
			{Key: aws.String(eniNodeTagKey), Value: aws.String("test-value")},
		},
	}}

	setupDescribeNetworkInterfacesPagesWithContextMock(t, mockEC2, interfaces, nil, 1)
	mockEC2.EXPECT().CreateTagsWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)

	ins := &EC2InstanceMetadataCache{ec2SVC: mockEC2}
	// Test checks that both mocks gets called.
	ins.cleanUpLeakedENIsInternal(time.Millisecond)
}

func setupDescribeNetworkInterfacesPagesWithContextMock(
	t *testing.T, mockEC2 *mock_ec2wrapper.MockEC2, interfaces []*ec2.NetworkInterface, err error, times int) {
	mockEC2.EXPECT().
		DescribeNetworkInterfacesPagesWithContext(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(times).
		DoAndReturn(func(_ context.Context, _ *ec2.DescribeNetworkInterfacesInput,
			fn func(*ec2.DescribeNetworkInterfacesOutput, bool) bool) error {
			assert.Equal(t, true, fn(&ec2.DescribeNetworkInterfacesOutput{
				NetworkInterfaces: interfaces,
			}, true))
			return err
		})
}
