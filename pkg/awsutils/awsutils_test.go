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
	"strconv"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"

	mock_ec2wrapper "github.com/aws/amazon-vpc-cni-k8s/pkg/ec2wrapper/mocks"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/eventrecorder"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	metadataVpcID        = "/vpc-id"
	metadataVPCcidrs     = "/vpc-ipv4-cidr-blocks"
	metadataDeviceNum    = "/device-number"
	metadataInterface    = "/interface-id"
	metadataSubnetCIDR   = "/subnet-ipv4-cidr-block"
	metadataIPv4s        = "/local-ipv4s"
	metadataIPv4Prefixes = "/ipv4-prefix"
	metadataIPv6Prefixes = "/ipv6-prefix"

	az                   = "us-east-1a"
	localIP              = "10.0.0.10"
	instanceID           = "i-0e1f3b9eb950e4980"
	instanceType         = "c1.medium"
	primaryMAC           = "12:ef:2a:98:e5:5a"
	eni2MAC              = "12:ef:2a:98:e5:5b"
	eni3MAC              = "12:ef:2a:98:e5:5c"
	sg1                  = "sg-2e080f50"
	sg2                  = "sg-2e080f51"
	sgs                  = sg1 + " " + sg2
	subnetID             = "subnet-6b245523"
	subnetCIDR           = "10.0.1.0/24"
	vpcID                = "vpc-3c133421"
	primaryeniID         = "eni-00000000"
	eniID                = primaryeniID
	eniAttachID          = "eni-attach-beb21856"
	eni1Device           = "0"
	eni1PrivateIP        = "10.0.0.1"
	eni1Prefix           = "10.0.1.0/28"
	eni2Device           = "1"
	eni2PrivateIP        = "10.0.0.2"
	eni2Prefix           = "10.0.2.0/28"
	eni2v6Prefix         = "2001:db8::/64"
	eni2ID               = "eni-12341234"
	metadataVPCIPv4CIDRs = "192.168.0.0/16	100.66.0.0/1"
	myNodeName           = "testNodeName"
	imdsMACFields        = "security-group-ids subnet-id vpc-id vpc-ipv4-cidr-blocks device-number interface-id subnet-ipv4-cidr-block local-ipv4s ipv4-prefix ipv6-prefix"
	imdsMACFieldsEfaOnly = "security-group-ids subnet-id vpc-id vpc-ipv4-cidr-blocks device-number interface-id subnet-ipv4-cidr-block ipv4-prefix ipv6-prefix"
)

func testMetadata(overrides map[string]interface{}) FakeIMDS {
	data := map[string]interface{}{
		metadataAZ:                   az,
		metadataLocalIP:              localIP,
		metadataInstanceID:           instanceID,
		metadataInstanceType:         instanceType,
		metadataMAC:                  primaryMAC,
		metadataMACPath:              primaryMAC,
		metadataMACPath + primaryMAC: imdsMACFields,
		metadataMACPath + primaryMAC + metadataDeviceNum:  eni1Device,
		metadataMACPath + primaryMAC + metadataInterface:  primaryeniID,
		metadataMACPath + primaryMAC + metadataSGs:        sgs,
		metadataMACPath + primaryMAC + metadataIPv4s:      eni1PrivateIP,
		metadataMACPath + primaryMAC + metadataSubnetID:   subnetID,
		metadataMACPath + primaryMAC + metadataVpcID:      vpcID,
		metadataMACPath + primaryMAC + metadataSubnetCIDR: subnetCIDR,
		metadataMACPath + primaryMAC + metadataVPCcidrs:   metadataVPCIPv4CIDRs,
	}

	for k, v := range overrides {
		data[k] = v
	}

	return FakeIMDS(data)
}

func testMetadataWithPrefixes(overrides map[string]interface{}) FakeIMDS {
	data := map[string]interface{}{
		metadataAZ:                   az,
		metadataLocalIP:              localIP,
		metadataInstanceID:           instanceID,
		metadataInstanceType:         instanceType,
		metadataMAC:                  primaryMAC,
		metadataMACPath:              primaryMAC,
		metadataMACPath + primaryMAC: imdsMACFields,
		metadataMACPath + primaryMAC + metadataDeviceNum:    eni1Device,
		metadataMACPath + primaryMAC + metadataInterface:    primaryeniID,
		metadataMACPath + primaryMAC + metadataSGs:          sgs,
		metadataMACPath + primaryMAC + metadataIPv4s:        eni1PrivateIP,
		metadataMACPath + primaryMAC + metadataIPv4Prefixes: eni1Prefix,
		metadataMACPath + primaryMAC + metadataSubnetID:     subnetID,
		metadataMACPath + primaryMAC + metadataVpcID:        vpcID,
		metadataMACPath + primaryMAC + metadataSubnetCIDR:   subnetCIDR,
		metadataMACPath + primaryMAC + metadataVPCcidrs:     metadataVPCIPv4CIDRs,
	}

	for k, v := range overrides {
		data[k] = v
	}

	return FakeIMDS(data)
}

func setup(t *testing.T) (*gomock.Controller,
	*mock_ec2wrapper.MockEC2) {
	ctrl := gomock.NewController(t)
	setupEventRecorder(t)
	return ctrl,
		mock_ec2wrapper.NewMockEC2(ctrl)
}

func setupEventRecorder(t *testing.T) {
	eventrecorder.InitMockEventRecorder()
	mockEventRecorder := eventrecorder.Get()

	fakeNode := v1.Node{
		TypeMeta:   metav1.TypeMeta{Kind: "Node"},
		ObjectMeta: metav1.ObjectMeta{Name: myNodeName},
		Spec:       v1.NodeSpec{},
		Status:     v1.NodeStatus{},
	}
	ctx := context.Background()
	mockEventRecorder.K8sClient.Create(ctx, &fakeNode)
}

func TestInitWithEC2metadata(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	ctrl, mockEC2 := setup(t)
	defer ctrl.Finish()
	mockMetadata := testMetadata(nil)

	cache := &EC2InstanceMetadataCache{imds: TypedIMDS{mockMetadata}, ec2SVC: mockEC2}
	err := cache.initWithEC2Metadata(ctx)
	if assert.NoError(t, err) {
		assert.Equal(t, az, cache.availabilityZone)
		assert.Equal(t, localIP, cache.localIPv4.String())
		assert.Equal(t, cache.instanceID, instanceID)
		assert.Equal(t, cache.primaryENImac, primaryMAC)
		assert.Equal(t, cache.primaryENI, primaryeniID)
		assert.Equal(t, cache.vpcID, vpcID)
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

		cache := &EC2InstanceMetadataCache{imds: TypedIMDS{mockMetadata}, ec2SVC: mockEC2}

		// This test is a bit silly.  We expect broken metadata to result in an err return here.  But if the code is resilient and _succeeds_, then of course that's ok too.  Mostly we just want it not to panic.
		assert.NotPanics(t, func() {
			_ = cache.initWithEC2Metadata(ctx)
		}, "Broken metadata %s resulted in panic", key)
	}
}

func TestGetAttachedENIs(t *testing.T) {
	mockMetadata := testMetadata(map[string]interface{}{
		metadataMACPath:                                primaryMAC + " " + eni2MAC,
		metadataMACPath + eni2MAC:                      imdsMACFields,
		metadataMACPath + eni2MAC + metadataDeviceNum:  eni2Device,
		metadataMACPath + eni2MAC + metadataInterface:  eni2ID,
		metadataMACPath + eni2MAC + metadataSubnetCIDR: subnetCIDR,
		metadataMACPath + eni2MAC + metadataIPv4s:      eni2PrivateIP,
	})

	cache := &EC2InstanceMetadataCache{imds: TypedIMDS{mockMetadata}}
	ens, err := cache.GetAttachedENIs()
	if assert.NoError(t, err) {
		assert.Equal(t, len(ens), 2)
	}
}

func TestGetAttachedENIsWithEfaOnly(t *testing.T) {
	mockMetadata := testMetadata(map[string]interface{}{
		metadataMACPath:                                primaryMAC + " " + eni2MAC,
		metadataMACPath + eni2MAC:                      imdsMACFieldsEfaOnly,
		metadataMACPath + eni2MAC + metadataDeviceNum:  eni2Device,
		metadataMACPath + eni2MAC + metadataInterface:  eni2ID,
		metadataMACPath + eni2MAC + metadataSubnetCIDR: subnetCIDR,
	})

	cache := &EC2InstanceMetadataCache{imds: TypedIMDS{mockMetadata}}
	ens, err := cache.GetAttachedENIs()
	if assert.NoError(t, err) {
		assert.Equal(t, len(ens), 2)
	}
}

func TestGetAttachedENIsWithPrefixes(t *testing.T) {
	mockMetadata := testMetadata(map[string]interface{}{
		metadataMACPath:                                  primaryMAC + " " + eni2MAC,
		metadataMACPath + eni2MAC:                        imdsMACFields,
		metadataMACPath + eni2MAC + metadataDeviceNum:    eni2Device,
		metadataMACPath + eni2MAC + metadataInterface:    eni2ID,
		metadataMACPath + eni2MAC + metadataSubnetCIDR:   subnetCIDR,
		metadataMACPath + eni2MAC + metadataIPv4s:        eni2PrivateIP,
		metadataMACPath + eni2MAC + metadataIPv4Prefixes: eni2Prefix,
	})

	cache := &EC2InstanceMetadataCache{imds: TypedIMDS{mockMetadata}}
	ens, err := cache.GetAttachedENIs()
	if assert.NoError(t, err) {
		assert.Equal(t, len(ens), 2)
	}
}

func TestAWSGetFreeDeviceNumberOnErr(t *testing.T) {
	ctrl, mockEC2 := setup(t)
	defer ctrl.Finish()

	// test error handling
	mockEC2.EXPECT().DescribeInstancesWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("error on DescribeInstancesWithContext"))

	cache := &EC2InstanceMetadataCache{ec2SVC: mockEC2}
	_, err := cache.awsGetFreeDeviceNumber()
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
		ec2ENI := &ec2.InstanceNetworkInterface{Attachment: &ec2.InstanceNetworkInterfaceAttachment{DeviceIndex: &deviceNums[i], NetworkCardIndex: aws.Int64(0)}}
		ec2ENIs = append(ec2ENIs, ec2ENI)
	}
	result := &ec2.DescribeInstancesOutput{
		Reservations: []*ec2.Reservation{{Instances: []*ec2.Instance{{NetworkInterfaces: ec2ENIs}}}}}

	mockEC2.EXPECT().DescribeInstancesWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(result, nil)

	cache := &EC2InstanceMetadataCache{ec2SVC: mockEC2}
	_, err := cache.awsGetFreeDeviceNumber()
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

		cache := &EC2InstanceMetadataCache{ec2SVC: mockEC2}
		id, err := cache.getENIAttachmentID("test-eni")
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
		cache := &EC2InstanceMetadataCache{imds: TypedIMDS{mockMetadata}, ec2SVC: mockEC2}
		metaData, err := cache.DescribeAllENIs()
		assert.Equal(t, tc.expErr, err, tc.name)
		assert.Equal(t, tc.exptags, metaData.TagMap, tc.name)
	}
}

func TestAllocENI(t *testing.T) {
	ctrl, mockEC2 := setup(t)
	defer ctrl.Finish()

	mockMetadata := testMetadata(nil)

	ipAddressCount := int64(100)
	subnetResult := &ec2.DescribeSubnetsOutput{
		Subnets: []*ec2.Subnet{{
			AvailableIpAddressCount: aws.Int64(ipAddressCount),
			SubnetId:                aws.String(subnetID),
			Tags: []*ec2.Tag{
				{
					Key:   aws.String("kubernetes.io/role/cni"),
					Value: aws.String("1"),
				},
			},
		}},
	}
	mockEC2.EXPECT().DescribeSubnetsWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(subnetResult, nil)

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
	mockEC2.EXPECT().ModifyNetworkInterfaceAttributeWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)

	cache := &EC2InstanceMetadataCache{
		ec2SVC:             mockEC2,
		imds:               TypedIMDS{mockMetadata},
		instanceType:       "c5n.18xlarge",
		useSubnetDiscovery: true,
	}

	_, err := cache.AllocENI(false, nil, "", 5)
	assert.NoError(t, err)
}

func TestAllocENINoFreeDevice(t *testing.T) {
	ctrl, mockEC2 := setup(t)
	defer ctrl.Finish()

	mockMetadata := testMetadata(nil)

	ipAddressCount := int64(100)
	subnetResult := &ec2.DescribeSubnetsOutput{
		Subnets: []*ec2.Subnet{{
			AvailableIpAddressCount: &ipAddressCount,
			SubnetId:                aws.String(subnetID),
			Tags: []*ec2.Tag{
				{
					Key:   aws.String("kubernetes.io/role/cni"),
					Value: aws.String("1"),
				},
			},
		}},
	}
	mockEC2.EXPECT().DescribeSubnetsWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(subnetResult, nil)

	cureniID := eniID
	eni := ec2.CreateNetworkInterfaceOutput{NetworkInterface: &ec2.NetworkInterface{NetworkInterfaceId: &cureniID}}
	mockEC2.EXPECT().CreateNetworkInterfaceWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(&eni, nil)

	// test no free index
	ec2ENIs := make([]*ec2.InstanceNetworkInterface, 0)

	for i := 0; i < maxENIs; i++ {
		var deviceNums [maxENIs]int64
		deviceNums[i] = int64(i)
		ec2ENI := &ec2.InstanceNetworkInterface{Attachment: &ec2.InstanceNetworkInterfaceAttachment{DeviceIndex: &deviceNums[i], NetworkCardIndex: aws.Int64(0)}}
		ec2ENIs = append(ec2ENIs, ec2ENI)
	}
	result := &ec2.DescribeInstancesOutput{
		Reservations: []*ec2.Reservation{{Instances: []*ec2.Instance{{NetworkInterfaces: ec2ENIs}}}}}

	mockEC2.EXPECT().DescribeInstancesWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(result, nil)
	mockEC2.EXPECT().DeleteNetworkInterfaceWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)

	cache := &EC2InstanceMetadataCache{
		ec2SVC:             mockEC2,
		imds:               TypedIMDS{mockMetadata},
		instanceType:       "c5n.18xlarge",
		useSubnetDiscovery: true,
	}

	_, err := cache.AllocENI(false, nil, "", 5)
	assert.Error(t, err)
}

func TestAllocENIMaxReached(t *testing.T) {
	ctrl, mockEC2 := setup(t)
	defer ctrl.Finish()

	mockMetadata := testMetadata(nil)

	ipAddressCount := int64(100)
	subnetResult := &ec2.DescribeSubnetsOutput{
		Subnets: []*ec2.Subnet{{
			AvailableIpAddressCount: &ipAddressCount,
			SubnetId:                aws.String(subnetID),
			Tags: []*ec2.Tag{
				{
					Key:   aws.String("kubernetes.io/role/cni"),
					Value: aws.String("1"),
				},
			},
		}},
	}
	mockEC2.EXPECT().DescribeSubnetsWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(subnetResult, nil)

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

	cache := &EC2InstanceMetadataCache{
		ec2SVC:             mockEC2,
		imds:               TypedIMDS{mockMetadata},
		instanceType:       "c5n.18xlarge",
		useSubnetDiscovery: true,
	}

	_, err := cache.AllocENI(false, nil, "", 5)
	assert.Error(t, err)
}

func TestAllocENIWithIPAddresses(t *testing.T) {
	ctrl, mockEC2 := setup(t)
	defer ctrl.Finish()

	ipAddressCount := int64(100)
	subnetResult := &ec2.DescribeSubnetsOutput{
		Subnets: []*ec2.Subnet{{
			AvailableIpAddressCount: &ipAddressCount,
			SubnetId:                aws.String(subnetID),
			Tags: []*ec2.Tag{
				{
					Key:   aws.String("kubernetes.io/role/cni"),
					Value: aws.String("1"),
				},
			},
		}},
	}
	mockEC2.EXPECT().DescribeSubnetsWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(subnetResult, nil)

	// when required IP numbers(5) is below ENI's limit(30)
	currentEniID := eniID
	eni := ec2.CreateNetworkInterfaceOutput{NetworkInterface: &ec2.NetworkInterface{NetworkInterfaceId: &currentEniID}}
	mockEC2.EXPECT().CreateNetworkInterfaceWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(&eni, nil)

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
	mockEC2.EXPECT().ModifyNetworkInterfaceAttributeWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)

	cache := &EC2InstanceMetadataCache{ec2SVC: mockEC2, instanceType: "c5n.18xlarge", useSubnetDiscovery: true}
	_, err := cache.AllocENI(false, nil, subnetID, 5)
	assert.NoError(t, err)

	// when required IP numbers(50) is higher than ENI's limit(49)
	mockEC2.EXPECT().DescribeSubnetsWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(subnetResult, nil)
	mockEC2.EXPECT().CreateNetworkInterfaceWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(&eni, nil)
	mockEC2.EXPECT().DescribeInstancesWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(result, nil)
	mockEC2.EXPECT().AttachNetworkInterfaceWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(attachResult, nil)
	mockEC2.EXPECT().ModifyNetworkInterfaceAttributeWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)
	cache = &EC2InstanceMetadataCache{ec2SVC: mockEC2, instanceType: "c5n.18xlarge", useSubnetDiscovery: true}
	_, err = cache.AllocENI(false, nil, subnetID, 49)
	assert.NoError(t, err)
}

func TestAllocENIWithIPAddressesAlreadyFull(t *testing.T) {
	ctrl, mockEC2 := setup(t)
	defer ctrl.Finish()

	mockMetadata := testMetadata(nil)

	ipAddressCount := int64(100)
	subnetResult := &ec2.DescribeSubnetsOutput{
		Subnets: []*ec2.Subnet{{
			AvailableIpAddressCount: &ipAddressCount,
			SubnetId:                aws.String(subnetID),
			Tags: []*ec2.Tag{
				{
					Key:   aws.String("kubernetes.io/role/cni"),
					Value: aws.String("1"),
				},
			},
		}},
	}
	mockEC2.EXPECT().DescribeSubnetsWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(subnetResult, nil)

	retErr := awserr.New("PrivateIpAddressLimitExceeded", "Too many IPs already allocated", nil)
	mockEC2.EXPECT().CreateNetworkInterfaceWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, retErr)

	cache := &EC2InstanceMetadataCache{
		ec2SVC:             mockEC2,
		imds:               TypedIMDS{mockMetadata},
		instanceType:       "t3.xlarge",
		useSubnetDiscovery: true,
	}
	_, err := cache.AllocENI(true, nil, "", 14)
	assert.Error(t, err)
}

func TestAllocENIWithPrefixAddresses(t *testing.T) {
	ctrl, mockEC2 := setup(t)
	defer ctrl.Finish()

	mockMetadata := testMetadata(nil)

	ipAddressCount := int64(100)
	subnetResult := &ec2.DescribeSubnetsOutput{
		Subnets: []*ec2.Subnet{{
			AvailableIpAddressCount: &ipAddressCount,
			SubnetId:                aws.String(subnetID),
			Tags: []*ec2.Tag{
				{
					Key:   aws.String("kubernetes.io/role/cni"),
					Value: aws.String("1"),
				},
			},
		}},
	}
	mockEC2.EXPECT().DescribeSubnetsWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(subnetResult, nil)

	currentEniID := eniID
	eni := ec2.CreateNetworkInterfaceOutput{NetworkInterface: &ec2.NetworkInterface{NetworkInterfaceId: &currentEniID}}
	mockEC2.EXPECT().CreateNetworkInterfaceWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(&eni, nil)

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
	mockEC2.EXPECT().ModifyNetworkInterfaceAttributeWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)

	cache := &EC2InstanceMetadataCache{
		ec2SVC:                 mockEC2,
		imds:                   TypedIMDS{mockMetadata},
		instanceType:           "c5n.18xlarge",
		enablePrefixDelegation: true,
		useSubnetDiscovery:     true,
	}
	_, err := cache.AllocENI(false, nil, subnetID, 1)
	assert.NoError(t, err)
}

func TestAllocENIWithPrefixesAlreadyFull(t *testing.T) {
	ctrl, mockEC2 := setup(t)
	defer ctrl.Finish()

	mockMetadata := testMetadata(nil)

	ipAddressCount := int64(100)
	subnetResult := &ec2.DescribeSubnetsOutput{
		Subnets: []*ec2.Subnet{{
			AvailableIpAddressCount: &ipAddressCount,
			SubnetId:                aws.String(subnetID),
			Tags: []*ec2.Tag{
				{
					Key:   aws.String("kubernetes.io/role/cni"),
					Value: aws.String("1"),
				},
			},
		}},
	}
	mockEC2.EXPECT().DescribeSubnetsWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(subnetResult, nil)

	retErr := awserr.New("PrivateIpAddressLimitExceeded", "Too many IPs already allocated", nil)
	mockEC2.EXPECT().CreateNetworkInterfaceWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, retErr)

	cache := &EC2InstanceMetadataCache{
		ec2SVC:                 mockEC2,
		imds:                   TypedIMDS{mockMetadata},
		instanceType:           "c5n.18xlarge",
		enablePrefixDelegation: true,
		useSubnetDiscovery:     true,
	}
	_, err := cache.AllocENI(true, nil, "", 1)
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

	cache := &EC2InstanceMetadataCache{
		ec2SVC: mockEC2,
	}

	err := cache.freeENI("test-eni", time.Millisecond, time.Millisecond)
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

	cache := &EC2InstanceMetadataCache{
		ec2SVC: mockEC2,
	}

	err := cache.freeENI("test-eni", time.Millisecond, time.Millisecond)
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

	cache := &EC2InstanceMetadataCache{
		ec2SVC: mockEC2,
	}

	err := cache.freeENI("test-eni", time.Millisecond, time.Millisecond)
	assert.Error(t, err)
}

func TestFreeENIDescribeErr(t *testing.T) {
	ctrl, mockEC2 := setup(t)
	defer ctrl.Finish()

	mockEC2.EXPECT().DescribeNetworkInterfacesWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("Error on DescribeNetworkInterfacesWithContext"))

	cache := &EC2InstanceMetadataCache{
		ec2SVC: mockEC2,
	}

	err := cache.FreeENI("test-eni")
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

	cache := &EC2InstanceMetadataCache{ec2SVC: mockEC2}
	cache.instanceType = "not-there"
	err := cache.FetchInstanceTypeLimits()
	assert.NoError(t, err)
	value := cache.GetENILimit()
	assert.Equal(t, 9, value)
	pv4Limit := cache.GetENIIPv4Limit()
	assert.Equal(t, 98, pv4Limit)
}

func TestAllocIPAddress(t *testing.T) {
	ctrl, mockEC2 := setup(t)
	defer ctrl.Finish()

	mockEC2.EXPECT().AssignPrivateIpAddressesWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(&ec2.AssignPrivateIpAddressesOutput{}, nil)

	cache := &EC2InstanceMetadataCache{ec2SVC: mockEC2}
	err := cache.AllocIPAddress("eni-id")
	assert.NoError(t, err)
}

func TestAllocIPAddressOnErr(t *testing.T) {
	ctrl, mockEC2 := setup(t)
	defer ctrl.Finish()

	mockEC2.EXPECT().AssignPrivateIpAddressesWithContext(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("Error on AssignPrivateIpAddressesWithContext"))

	cache := &EC2InstanceMetadataCache{ec2SVC: mockEC2}
	err := cache.AllocIPAddress("eni-id")
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

	cache := &EC2InstanceMetadataCache{ec2SVC: mockEC2, instanceType: "c5n.18xlarge"}
	_, err := cache.AllocIPAddresses(eniID, 5)
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

	cache = &EC2InstanceMetadataCache{ec2SVC: mockEC2, instanceType: "c5n.18xlarge"}
	_, err = cache.AllocIPAddresses(eniID, 50)
	assert.NoError(t, err)

	// Adding 0 should do nothing
	_, err = cache.AllocIPAddresses(eniID, 0)
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
	cache := &EC2InstanceMetadataCache{ec2SVC: mockEC2, instanceType: "t3.xlarge"}

	retErr := awserr.New("PrivateIpAddressLimitExceeded", "Too many IPs already allocated", nil)
	mockEC2.EXPECT().AssignPrivateIpAddressesWithContext(gomock.Any(), input, gomock.Any()).Return(nil, retErr)
	// If EC2 says that all IPs are already attached, then DS is out of sync so alloc will fail
	_, err := cache.AllocIPAddresses(eniID, 14)
	assert.Error(t, err)
}

func TestAllocPrefixAddresses(t *testing.T) {
	ctrl, mockEC2 := setup(t)
	defer ctrl.Finish()

	//Allocate 1 prefix for the ENI
	input := &ec2.AssignPrivateIpAddressesInput{
		NetworkInterfaceId: aws.String(eniID),
		Ipv4PrefixCount:    aws.Int64(1),
	}
	mockEC2.EXPECT().AssignPrivateIpAddressesWithContext(gomock.Any(), input, gomock.Any()).Return(nil, nil)

	cache := &EC2InstanceMetadataCache{ec2SVC: mockEC2, instanceType: "c5n.18xlarge", enablePrefixDelegation: true}
	_, err := cache.AllocIPAddresses(eniID, 1)
	assert.NoError(t, err)

	// Adding 0 should do nothing
	_, err = cache.AllocIPAddresses(eniID, 0)
	assert.NoError(t, err)
}

func TestAllocPrefixesAlreadyFull(t *testing.T) {
	ctrl, mockEC2 := setup(t)
	defer ctrl.Finish()
	// The required Prefixes (1) is the ENI's limit(1)
	input := &ec2.AssignPrivateIpAddressesInput{
		NetworkInterfaceId: aws.String(eniID),
		Ipv4PrefixCount:    aws.Int64(1),
	}
	cache := &EC2InstanceMetadataCache{ec2SVC: mockEC2, instanceType: "t3.xlarge", enablePrefixDelegation: true}

	retErr := awserr.New("PrivateIpAddressLimitExceeded", "Too many IPs already allocated", nil)
	mockEC2.EXPECT().AssignPrivateIpAddressesWithContext(gomock.Any(), input, gomock.Any()).Return(nil, retErr)
	// If EC2 says that all IPs are already attached, then DS is out of sync so alloc will fail
	_, err := cache.AllocIPAddresses(eniID, 1)
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
		IPv4Prefixes: nil,
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
				metadataMACPath:                                primaryMAC + " " + eni2MAC,
				metadataMACPath + eni2MAC:                      imdsMACFields,
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

func TestEC2InstanceMetadataCache_waitForENIAndPrefixesAttached(t *testing.T) {
	type args struct {
		eni                string
		foundPrefixes      int
		wantedSecondaryIPs int
		maxBackoffDelay    time.Duration
		v4Enabled          bool
		v6Enabled          bool
		times              int
	}

	isPrimary := true
	primaryIP := eni2PrivateIP
	prefixIP := eni2Prefix
	eni1Metadata := ENIMetadata{
		ENIID:          eni2ID,
		MAC:            eni2MAC,
		DeviceNumber:   1,
		SubnetIPv4CIDR: subnetCIDR,
		IPv4Addresses: []*ec2.NetworkInterfacePrivateIpAddress{
			{
				Primary:          &isPrimary,
				PrivateIpAddress: &primaryIP,
			},
		},
		IPv4Prefixes: []*ec2.Ipv4PrefixSpecification{
			{
				Ipv4Prefix: &prefixIP,
			},
		},
	}
	v6PrefixIP := eni2v6Prefix
	eni2Metadata := ENIMetadata{
		ENIID:          eni2ID,
		MAC:            eni2MAC,
		DeviceNumber:   1,
		SubnetIPv4CIDR: subnetCIDR,
		IPv4Addresses: []*ec2.NetworkInterfacePrivateIpAddress{
			{
				Primary:          &isPrimary,
				PrivateIpAddress: &primaryIP,
			},
		},
		IPv6Prefixes: []*ec2.Ipv6PrefixSpecification{
			{
				Ipv6Prefix: &v6PrefixIP,
			},
		},
		IPv6Addresses: []*ec2.NetworkInterfaceIpv6Address{},
	}
	tests := []struct {
		name            string
		args            args
		wantEniMetadata ENIMetadata
		wantErr         bool
	}{
		{"Test wait v4 success", args{eni: eni2ID, foundPrefixes: 1, wantedSecondaryIPs: 1, maxBackoffDelay: 5 * time.Millisecond, times: 1, v4Enabled: true, v6Enabled: false}, eni1Metadata, false},
		{"Test partial v4 success", args{eni: eni2ID, foundPrefixes: 1, wantedSecondaryIPs: 1, maxBackoffDelay: 5 * time.Millisecond, times: maxENIEC2APIRetries, v4Enabled: true, v6Enabled: false}, eni1Metadata, false},
		{"Test wait v6 success", args{eni: eni2ID, foundPrefixes: 1, wantedSecondaryIPs: 1, maxBackoffDelay: 5 * time.Millisecond, times: maxENIEC2APIRetries, v4Enabled: false, v6Enabled: true}, eni2Metadata, false},
		{"Test wait fail", args{eni: eni2ID, foundPrefixes: 0, wantedSecondaryIPs: 1, maxBackoffDelay: 5 * time.Millisecond, times: maxENIEC2APIRetries}, ENIMetadata{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl, mockEC2 := setup(t)
			defer ctrl.Finish()
			eniIPs := eni2PrivateIP
			eniPrefixes := eni2Prefix
			metaDataPrefixPath := metadataIPv4Prefixes
			if tt.args.v6Enabled {
				eniPrefixes = eni2v6Prefix
				metaDataPrefixPath = metadataIPv6Prefixes
			}
			if tt.args.foundPrefixes == 0 {
				eniPrefixes = ""
			}
			mockMetadata := testMetadata(map[string]interface{}{
				metadataMACPath:                                primaryMAC + " " + eni2MAC,
				metadataMACPath + eni2MAC:                      imdsMACFields,
				metadataMACPath + eni2MAC + metadataDeviceNum:  eni2Device,
				metadataMACPath + eni2MAC + metadataInterface:  eni2ID,
				metadataMACPath + eni2MAC + metadataSubnetCIDR: subnetCIDR,
				metadataMACPath + eni2MAC + metadataIPv4s:      eniIPs,
				metadataMACPath + eni2MAC + metaDataPrefixPath: eniPrefixes,
			})
			cache := &EC2InstanceMetadataCache{imds: TypedIMDS{mockMetadata}, ec2SVC: mockEC2,
				enablePrefixDelegation: true, v4Enabled: tt.args.v4Enabled, v6Enabled: tt.args.v6Enabled}
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
	cache := &EC2InstanceMetadataCache{imds: TypedIMDS{mockMetadata}}
	cache.SetUnmanagedENIs(nil)
	assert.False(t, cache.IsUnmanagedENI("eni-1"))
	cache.SetUnmanagedENIs([]string{"eni-1", "eni-2"})
	assert.True(t, cache.IsUnmanagedENI("eni-1"))
	assert.False(t, cache.IsUnmanagedENI("eni-99"))
	cache.SetUnmanagedENIs(nil)
	assert.False(t, cache.IsUnmanagedENI("eni-1"))
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

	cache := &EC2InstanceMetadataCache{ec2SVC: mockEC2}
	// Test checks that both mocks gets called.
	cache.cleanUpLeakedENIsInternal(time.Millisecond)
}

func setupDescribeNetworkInterfacesPagesWithContextMock(
	t *testing.T, mockEC2 *mock_ec2wrapper.MockEC2, interfaces []*ec2.NetworkInterface, err error, times int) {
	mockEC2.EXPECT().
		DescribeNetworkInterfacesPagesWithContext(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(times).
		DoAndReturn(func(_ context.Context, _ *ec2.DescribeNetworkInterfacesInput,
			fn func(*ec2.DescribeNetworkInterfacesOutput, bool) bool, _ ...request.Option) error {
			assert.Equal(t, true, fn(&ec2.DescribeNetworkInterfacesOutput{
				NetworkInterfaces: interfaces,
			}, true))
			return err
		})
}

func TestEC2InstanceMetadataCache_buildENITags(t *testing.T) {
	type fields struct {
		instanceID        string
		clusterName       string
		additionalENITags map[string]string
	}
	tests := []struct {
		name   string
		fields fields
		want   map[string]string
	}{
		{
			name: "without clusterName or additionalENITags",
			fields: fields{
				instanceID: "i-xxxxx",
			},
			want: map[string]string{
				"node.k8s.amazonaws.com/instance_id": "i-xxxxx",
			},
		},
		{
			name: "with clusterName",
			fields: fields{
				instanceID:  "i-xxxxx",
				clusterName: "awesome-cluster",
			},
			want: map[string]string{
				"node.k8s.amazonaws.com/instance_id": "i-xxxxx",
				"cluster.k8s.amazonaws.com/name":     "awesome-cluster",
			},
		},
		{
			name: "with additional ENI tags",
			fields: fields{
				instanceID: "i-xxxxx",
				additionalENITags: map[string]string{
					"tagKey-1": "tagVal-1",
					"tagKey-2": "tagVal-2",
				},
			},
			want: map[string]string{
				"node.k8s.amazonaws.com/instance_id": "i-xxxxx",
				"tagKey-1":                           "tagVal-1",
				"tagKey-2":                           "tagVal-2",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := &EC2InstanceMetadataCache{
				instanceID:        tt.fields.instanceID,
				clusterName:       tt.fields.clusterName,
				additionalENITags: tt.fields.additionalENITags,
			}
			got := cache.buildENITags()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestEC2InstanceMetadataCache_getLeakedENIs(t *testing.T) {
	tenMinuteAgo := time.Now().Local().Add(time.Minute * time.Duration(-10))
	now := time.Now().Local()
	type describeNetworkInterfacePagesCall struct {
		input       *ec2.DescribeNetworkInterfacesInput
		outputPages []*ec2.DescribeNetworkInterfacesOutput
		err         error
	}
	type fields struct {
		clusterName                        string
		describeNetworkInterfacePagesCalls []describeNetworkInterfacePagesCall
	}
	tests := []struct {
		name    string
		fields  fields
		want    []*ec2.NetworkInterface
		wantErr error
	}{
		{
			name: "without clusterName - no leaked ENIs",
			fields: fields{
				clusterName: "",
				describeNetworkInterfacePagesCalls: []describeNetworkInterfacePagesCall{
					{
						input: &ec2.DescribeNetworkInterfacesInput{
							Filters: []*ec2.Filter{
								{
									Name:   aws.String("tag-key"),
									Values: []*string{aws.String("node.k8s.amazonaws.com/instance_id")},
								},
								{
									Name:   aws.String("status"),
									Values: []*string{aws.String("available")},
								},
								{
									Name:   aws.String("vpc-id"),
									Values: []*string{aws.String(vpcID)},
								},
							},
							MaxResults: aws.Int64(1000),
						},
						outputPages: []*ec2.DescribeNetworkInterfacesOutput{
							{
								NetworkInterfaces: nil,
							},
						},
					},
				},
			},
			want: nil,
		},
		{
			name: "without clusterName - one ENI leaked",
			fields: fields{
				clusterName: "",
				describeNetworkInterfacePagesCalls: []describeNetworkInterfacePagesCall{
					{
						input: &ec2.DescribeNetworkInterfacesInput{
							Filters: []*ec2.Filter{
								{
									Name:   aws.String("tag-key"),
									Values: []*string{aws.String("node.k8s.amazonaws.com/instance_id")},
								},
								{
									Name:   aws.String("status"),
									Values: []*string{aws.String("available")},
								},
								{
									Name:   aws.String("vpc-id"),
									Values: []*string{aws.String(vpcID)},
								},
							},
							MaxResults: aws.Int64(1000),
						},
						outputPages: []*ec2.DescribeNetworkInterfacesOutput{
							{
								NetworkInterfaces: []*ec2.NetworkInterface{
									{
										NetworkInterfaceId: aws.String("eni-1"),
										Description:        aws.String("aws-K8S-i-xxxxx"),
										Status:             aws.String("available"),
										TagSet: []*ec2.Tag{
											{
												Key:   aws.String("node.k8s.amazonaws.com/instance_id"),
												Value: aws.String("i-xxxxx"),
											},
											{
												Key:   aws.String("node.k8s.amazonaws.com/createdAt"),
												Value: aws.String(tenMinuteAgo.Format(time.RFC3339)),
											},
										},
									},
								},
							},
						},
					},
				},
			},
			want: []*ec2.NetworkInterface{
				{
					NetworkInterfaceId: aws.String("eni-1"),
					Description:        aws.String("aws-K8S-i-xxxxx"),
					Status:             aws.String("available"),
					TagSet: []*ec2.Tag{
						{
							Key:   aws.String("node.k8s.amazonaws.com/instance_id"),
							Value: aws.String("i-xxxxx"),
						},
						{
							Key:   aws.String("node.k8s.amazonaws.com/createdAt"),
							Value: aws.String(tenMinuteAgo.Format(time.RFC3339)),
						},
					},
				},
			},
		},
		{
			name: "without clusterName - one ENI - description didn't match",
			fields: fields{
				clusterName: "",
				describeNetworkInterfacePagesCalls: []describeNetworkInterfacePagesCall{
					{
						input: &ec2.DescribeNetworkInterfacesInput{
							Filters: []*ec2.Filter{
								{
									Name:   aws.String("tag-key"),
									Values: []*string{aws.String("node.k8s.amazonaws.com/instance_id")},
								},
								{
									Name:   aws.String("status"),
									Values: []*string{aws.String("available")},
								},
								{
									Name:   aws.String("vpc-id"),
									Values: []*string{aws.String(vpcID)},
								},
							},
							MaxResults: aws.Int64(1000),
						},
						outputPages: []*ec2.DescribeNetworkInterfacesOutput{
							{
								NetworkInterfaces: []*ec2.NetworkInterface{
									{
										NetworkInterfaceId: aws.String("eni-1"),
										Description:        aws.String("non-k8s-i-xxxxx"),
										Status:             aws.String("available"),
										TagSet: []*ec2.Tag{
											{
												Key:   aws.String("node.k8s.amazonaws.com/instance_id"),
												Value: aws.String("i-xxxxx"),
											},
											{
												Key:   aws.String("node.k8s.amazonaws.com/createdAt"),
												Value: aws.String(tenMinuteAgo.Format(time.RFC3339)),
											},
										},
									},
								},
							},
						},
					},
				},
			},
			want: nil,
		},
		{
			name: "without clusterName - one ENI - creationTime within deletion coolDown",
			fields: fields{
				clusterName: "",
				describeNetworkInterfacePagesCalls: []describeNetworkInterfacePagesCall{
					{
						input: &ec2.DescribeNetworkInterfacesInput{
							Filters: []*ec2.Filter{
								{
									Name:   aws.String("tag-key"),
									Values: []*string{aws.String("node.k8s.amazonaws.com/instance_id")},
								},
								{
									Name:   aws.String("status"),
									Values: []*string{aws.String("available")},
								},
								{
									Name:   aws.String("vpc-id"),
									Values: []*string{aws.String(vpcID)},
								},
							},
							MaxResults: aws.Int64(1000),
						},
						outputPages: []*ec2.DescribeNetworkInterfacesOutput{
							{
								NetworkInterfaces: []*ec2.NetworkInterface{
									{
										NetworkInterfaceId: aws.String("eni-1"),
										Description:        aws.String("aws-K8S-i-xxxxx"),
										Status:             aws.String("available"),
										TagSet: []*ec2.Tag{
											{
												Key:   aws.String("node.k8s.amazonaws.com/instance_id"),
												Value: aws.String("i-xxxxx"),
											},
											{
												Key:   aws.String("node.k8s.amazonaws.com/createdAt"),
												Value: aws.String(now.Format(time.RFC3339)),
											},
										},
									},
								},
							},
						},
					},
				},
			},
			want: nil,
		},
		{
			name: "without clusterName - no leaked ENIs",
			fields: fields{
				clusterName: "",
				describeNetworkInterfacePagesCalls: []describeNetworkInterfacePagesCall{
					{
						input: &ec2.DescribeNetworkInterfacesInput{
							Filters: []*ec2.Filter{
								{
									Name:   aws.String("tag-key"),
									Values: []*string{aws.String("node.k8s.amazonaws.com/instance_id")},
								},
								{
									Name:   aws.String("status"),
									Values: []*string{aws.String("available")},
								},
								{
									Name:   aws.String("vpc-id"),
									Values: []*string{aws.String(vpcID)},
								},
							},
							MaxResults: aws.Int64(1000),
						},
						outputPages: []*ec2.DescribeNetworkInterfacesOutput{
							{
								NetworkInterfaces: nil,
							},
						},
					},
				},
			},
			want: nil,
		},
		{
			name: "with clusterName - one ENI leaked",
			fields: fields{
				clusterName: "awesome-cluster",
				describeNetworkInterfacePagesCalls: []describeNetworkInterfacePagesCall{
					{
						input: &ec2.DescribeNetworkInterfacesInput{
							Filters: []*ec2.Filter{
								{
									Name:   aws.String("tag-key"),
									Values: []*string{aws.String("node.k8s.amazonaws.com/instance_id")},
								},
								{
									Name:   aws.String("status"),
									Values: []*string{aws.String("available")},
								},
								{
									Name:   aws.String("vpc-id"),
									Values: []*string{aws.String(vpcID)},
								},
								{
									Name:   aws.String("tag:cluster.k8s.amazonaws.com/name"),
									Values: []*string{aws.String("awesome-cluster")},
								},
							},
							MaxResults: aws.Int64(1000),
						},
						outputPages: []*ec2.DescribeNetworkInterfacesOutput{
							{
								NetworkInterfaces: []*ec2.NetworkInterface{
									{
										NetworkInterfaceId: aws.String("eni-1"),
										Description:        aws.String("aws-K8S-i-xxxxx"),
										Status:             aws.String("available"),
										TagSet: []*ec2.Tag{
											{
												Key:   aws.String("node.k8s.amazonaws.com/instance_id"),
												Value: aws.String("i-xxxxx"),
											},
											{
												Key:   aws.String("node.k8s.amazonaws.com/createdAt"),
												Value: aws.String(tenMinuteAgo.Format(time.RFC3339)),
											},
											{
												Key:   aws.String("cluster.k8s.amazonaws.com/name"),
												Value: aws.String("awesome-cluster"),
											},
										},
									},
								},
							},
						},
					},
				},
			},
			want: []*ec2.NetworkInterface{
				{
					NetworkInterfaceId: aws.String("eni-1"),
					Description:        aws.String("aws-K8S-i-xxxxx"),
					Status:             aws.String("available"),
					TagSet: []*ec2.Tag{
						{
							Key:   aws.String("node.k8s.amazonaws.com/instance_id"),
							Value: aws.String("i-xxxxx"),
						},
						{
							Key:   aws.String("node.k8s.amazonaws.com/createdAt"),
							Value: aws.String(tenMinuteAgo.Format(time.RFC3339)),
						},
						{
							Key:   aws.String("cluster.k8s.amazonaws.com/name"),
							Value: aws.String("awesome-cluster"),
						},
					},
				},
			},
		},
		{
			name: "with clusterName - one ENI - description didn't match",
			fields: fields{
				clusterName: "awesome-cluster",
				describeNetworkInterfacePagesCalls: []describeNetworkInterfacePagesCall{
					{
						input: &ec2.DescribeNetworkInterfacesInput{
							Filters: []*ec2.Filter{
								{
									Name:   aws.String("tag-key"),
									Values: []*string{aws.String("node.k8s.amazonaws.com/instance_id")},
								},
								{
									Name:   aws.String("status"),
									Values: []*string{aws.String("available")},
								},
								{
									Name:   aws.String("vpc-id"),
									Values: []*string{aws.String(vpcID)},
								},
								{
									Name:   aws.String("tag:cluster.k8s.amazonaws.com/name"),
									Values: []*string{aws.String("awesome-cluster")},
								},
							},
							MaxResults: aws.Int64(1000),
						},
						outputPages: []*ec2.DescribeNetworkInterfacesOutput{
							{
								NetworkInterfaces: []*ec2.NetworkInterface{
									{
										NetworkInterfaceId: aws.String("eni-1"),
										Description:        aws.String("non-k8s-i-xxxxx"),
										Status:             aws.String("available"),
										TagSet: []*ec2.Tag{
											{
												Key:   aws.String("node.k8s.amazonaws.com/instance_id"),
												Value: aws.String("i-xxxxx"),
											},
											{
												Key:   aws.String("node.k8s.amazonaws.com/createdAt"),
												Value: aws.String(tenMinuteAgo.Format(time.RFC3339)),
											},
											{
												Key:   aws.String("cluster.k8s.amazonaws.com/name"),
												Value: aws.String("awesome-cluster"),
											},
										},
									},
								},
							},
						},
					},
				},
			},
			want: nil,
		},
		{
			name: "with clusterName - one ENI - creationTime within deletion coolDown",
			fields: fields{
				clusterName: "awesome-cluster",
				describeNetworkInterfacePagesCalls: []describeNetworkInterfacePagesCall{
					{
						input: &ec2.DescribeNetworkInterfacesInput{
							Filters: []*ec2.Filter{
								{
									Name:   aws.String("tag-key"),
									Values: []*string{aws.String("node.k8s.amazonaws.com/instance_id")},
								},
								{
									Name:   aws.String("status"),
									Values: []*string{aws.String("available")},
								},
								{
									Name:   aws.String("vpc-id"),
									Values: []*string{aws.String(vpcID)},
								},
								{
									Name:   aws.String("tag:cluster.k8s.amazonaws.com/name"),
									Values: []*string{aws.String("awesome-cluster")},
								},
							},
							MaxResults: aws.Int64(1000),
						},
						outputPages: []*ec2.DescribeNetworkInterfacesOutput{
							{
								NetworkInterfaces: []*ec2.NetworkInterface{
									{
										NetworkInterfaceId: aws.String("eni-1"),
										Description:        aws.String("aws-K8S-i-xxxxx"),
										Status:             aws.String("available"),
										TagSet: []*ec2.Tag{
											{
												Key:   aws.String("node.k8s.amazonaws.com/instance_id"),
												Value: aws.String("i-xxxxx"),
											},
											{
												Key:   aws.String("node.k8s.amazonaws.com/createdAt"),
												Value: aws.String(now.Format(time.RFC3339)),
											},
											{
												Key:   aws.String("cluster.k8s.amazonaws.com/name"),
												Value: aws.String("awesome-cluster"),
											},
										},
									},
								},
							},
						},
					},
				},
			},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl, mockEC2 := setup(t)
			defer ctrl.Finish()

			for _, call := range tt.fields.describeNetworkInterfacePagesCalls {
				mockEC2.EXPECT().
					DescribeNetworkInterfacesPagesWithContext(gomock.Any(), call.input, gomock.Any(), gomock.Any()).
					DoAndReturn(func(_ context.Context, _ *ec2.DescribeNetworkInterfacesInput,
						fn func(*ec2.DescribeNetworkInterfacesOutput, bool) bool, _ ...request.Option) error {
						if call.err != nil {
							return call.err
						}
						for _, output := range call.outputPages {
							fn(output, true)
						}
						return nil
					})
			}
			cache := &EC2InstanceMetadataCache{ec2SVC: mockEC2, clusterName: tt.fields.clusterName, vpcID: vpcID}
			got, err := cache.getLeakedENIs()
			if tt.wantErr != nil {
				assert.EqualError(t, err, tt.wantErr.Error())
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestEC2InstanceMetadataCache_TagENI(t *testing.T) {
	type createTagsCall struct {
		input *ec2.CreateTagsInput
		err   error
	}
	type fields struct {
		instanceID        string
		clusterName       string
		additionalENITags map[string]string

		createTagsCalls []createTagsCall
	}
	type args struct {
		eniID       string
		currentTags map[string]string
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr error
	}{
		{
			name: "eni currently have no tags",
			fields: fields{
				instanceID:  "i-xxxx",
				clusterName: "awesome-cluster",
				createTagsCalls: []createTagsCall{
					{
						input: &ec2.CreateTagsInput{
							Resources: []*string{aws.String("eni-xxxx")},
							Tags: []*ec2.Tag{
								{
									Key:   aws.String("cluster.k8s.amazonaws.com/name"),
									Value: aws.String("awesome-cluster"),
								},
								{
									Key:   aws.String("node.k8s.amazonaws.com/instance_id"),
									Value: aws.String("i-xxxx"),
								},
							},
						},
					},
				},
			},
			args: args{
				eniID:       "eni-xxxx",
				currentTags: nil,
			},
			wantErr: nil,
		},
		{
			name: "eni currently have all desired tags",
			fields: fields{
				instanceID:      "i-xxxx",
				clusterName:     "awesome-cluster",
				createTagsCalls: nil,
			},
			args: args{
				eniID: "eni-xxxx",
				currentTags: map[string]string{
					"node.k8s.amazonaws.com/instance_id": "i-xxxx",
					"cluster.k8s.amazonaws.com/name":     "awesome-cluster",
				},
			},
			wantErr: nil,
		},
		{
			name: "eni currently have partial tags",
			fields: fields{
				instanceID:  "i-xxxx",
				clusterName: "awesome-cluster",
				createTagsCalls: []createTagsCall{
					{
						input: &ec2.CreateTagsInput{
							Resources: []*string{aws.String("eni-xxxx")},
							Tags: []*ec2.Tag{
								{
									Key:   aws.String("cluster.k8s.amazonaws.com/name"),
									Value: aws.String("awesome-cluster"),
								},
							},
						},
					},
				},
			},
			args: args{
				eniID: "eni-xxxx",
				currentTags: map[string]string{
					"node.k8s.amazonaws.com/instance_id": "i-xxxx",
					"anotherKey":                         "anotherDay",
				},
			},
			wantErr: nil,
		},
		{
			name: "create tags fails",
			fields: fields{
				instanceID:  "i-xxxx",
				clusterName: "awesome-cluster",
				createTagsCalls: []createTagsCall{
					{
						input: &ec2.CreateTagsInput{
							Resources: []*string{aws.String("eni-xxxx")},
							Tags: []*ec2.Tag{
								{
									Key:   aws.String("cluster.k8s.amazonaws.com/name"),
									Value: aws.String("awesome-cluster"),
								},
								{
									Key:   aws.String("node.k8s.amazonaws.com/instance_id"),
									Value: aws.String("i-xxxx"),
								},
							},
						},
						err: errors.New("permission denied"),
					},
				},
			},
			args: args{
				eniID:       "eni-xxxx",
				currentTags: nil,
			},
			wantErr: errors.New("permission denied"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl, mockEC2 := setup(t)
			defer ctrl.Finish()

			for _, call := range tt.fields.createTagsCalls {
				mockEC2.EXPECT().CreateTagsWithContext(gomock.Any(), call.input).Return(&ec2.CreateTagsOutput{}, call.err).AnyTimes()
			}

			cache := &EC2InstanceMetadataCache{
				ec2SVC:            mockEC2,
				instanceID:        tt.fields.instanceID,
				clusterName:       tt.fields.clusterName,
				additionalENITags: tt.fields.additionalENITags,
			}
			err := cache.TagENI(tt.args.eniID, tt.args.currentTags)
			if tt.wantErr != nil {
				assert.EqualError(t, err, tt.wantErr.Error())
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func Test_convertTagsToSDKTags(t *testing.T) {
	type args struct {
		tags map[string]string
	}
	tests := []struct {
		name string
		args args
		want []*ec2.Tag
	}{
		{
			name: "non-empty tags",
			args: args{
				tags: map[string]string{
					"keyA": "valueA",
					"keyB": "valueB",
				},
			},
			want: []*ec2.Tag{
				{
					Key:   aws.String("keyA"),
					Value: aws.String("valueA"),
				},
				{
					Key:   aws.String("keyB"),
					Value: aws.String("valueB"),
				},
			},
		},
		{
			name: "nil tags",
			args: args{tags: nil},
			want: nil,
		},
		{
			name: "empty tags",
			args: args{tags: map[string]string{}},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertTagsToSDKTags(tt.args.tags)
			assert.Equal(t, tt.want, got)
		})
	}
}

func Test_convertSDKTagsToTags(t *testing.T) {
	type args struct {
		sdkTags []*ec2.Tag
	}
	tests := []struct {
		name string
		args args
		want map[string]string
	}{
		{
			name: "non-empty sdk tags",
			args: args{
				sdkTags: []*ec2.Tag{
					{
						Key:   aws.String("keyA"),
						Value: aws.String("valueA"),
					},
					{
						Key:   aws.String("keyB"),
						Value: aws.String("valueB"),
					},
				},
			},
			want: map[string]string{
				"keyA": "valueA",
				"keyB": "valueB",
			},
		},
		{
			name: "nil sdk tags",
			args: args{
				sdkTags: nil,
			},
			want: nil,
		},
		{
			name: "empty sdk tags",
			args: args{
				sdkTags: []*ec2.Tag{},
			},
			want: nil,
		},
		{
			name: "nil sdk tag value",
			args: args{
				sdkTags: []*ec2.Tag{
					{
						Key:   aws.String("keyA"),
						Value: nil,
					},
				},
			},
			want: map[string]string{
				"keyA": "",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertSDKTagsToTags(tt.args.sdkTags)
			assert.Equal(t, tt.want, got)
		})
	}
}

func Test_loadAdditionalENITags(t *testing.T) {
	tests := []struct {
		name    string
		envVars map[string]string
		want    map[string]string
	}{
		{
			name: "no ADDITIONAL_ENI_TAGS env",
			envVars: map[string]string{
				"ADDITIONAL_ENI_TAGS": "",
			},
			want: nil,
		},
		{
			name: "ADDITIONAL_ENI_TAGS is valid format",
			envVars: map[string]string{
				"ADDITIONAL_ENI_TAGS": "{\"tagKey1\": \"tagVal1\"}",
			},
			want: map[string]string{
				"tagKey1": "tagVal1",
			},
		},
		{
			name: "ADDITIONAL_ENI_TAGS is invalid format",
			envVars: map[string]string{
				"ADDITIONAL_ENI_TAGS": "xxxx",
			},
			want: nil,
		},
		{
			name: "ADDITIONAL_ENI_TAGS is valid format but contains tags with restricted prefix",
			envVars: map[string]string{
				"ADDITIONAL_ENI_TAGS": "{\"bla.k8s.amazonaws.com\": \"bla\"}",
			},
			want: map[string]string{},
		},
		{
			name: "ADDITIONAL_ENI_TAGS is valid format but contains valid tags and tags with restricted prefix",
			envVars: map[string]string{
				"ADDITIONAL_ENI_TAGS": "{\"bla.k8s.amazonaws.com\": \"bla\", \"tagKey1\": \"tagVal1\"}",
			},
			want: map[string]string{
				"tagKey1": "tagVal1",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for key, value := range tt.envVars {
				if value != "" {
					os.Setenv(key, value)
				} else {
					os.Unsetenv(key)
				}
			}
			got := loadAdditionalENITags()
			assert.Equal(t, tt.want, got)
		})
	}
}
