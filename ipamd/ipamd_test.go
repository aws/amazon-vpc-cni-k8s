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

package ipamd

import (
	"net"
	"testing"
	//"time"

	"github.com/aws/amazon-vpc-cni-k8s/ipamd/awsutils"
	"github.com/aws/amazon-vpc-cni-k8s/ipamd/datastore"
	"github.com/aws/amazon-vpc-cni-k8s/ipamd/k8sapi"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"

	"github.com/aws/aws-sdk-go/service/ec2"
)

const (
	primaryENIid     = "eni-00000000"
	secENIid         = "eni-00000001"
	testAttachmentID = "eni-00000000-attach"
	eniID            = "eni-5731da78"
	primaryMAC       = "12:ef:2a:98:e5:5a"
	secMAC           = "12:ef:2a:98:e5:5b"
	primaryDevice    = 2
	secDevice        = 0
	primarySubnet    = "10.10.10.0/24"
	secSubnet        = "10.10.20.0/24"
	ipaddr01         = "10.10.10.11"
	ipaddr02         = "10.10.10.12"
	ipaddr11         = "10.10.20.11"
	ipaddr12         = "10.10.20.12"
	vpcCIDR          = "10.10.0.0/16"
)

func setup(t *testing.T) (*gomock.Controller,
	*mock_awsutils.MockAPIs,
	*mock_k8sapi.MockK8SAPIs,
	*mock_network.MockNetworkAPIs) {
	ctrl := gomock.NewController(t)
	return ctrl,
		mock_awsutils.NewMockAPIs(ctrl),
		mock_k8sapi.NewMockK8SAPIs(ctrl),
		mock_network.NewMockNetworkAPIs(ctrl)
}

func TestNodeInit(t *testing.T) {
	ctrl, mockAWS, mockK8S, mockNetwork := setup(t)
	defer ctrl.Finish()

	mockContext := &IPAMContext{
		awsClient:     mockAWS,
		k8sClient:     mockK8S,
		networkClient: mockNetwork}

	eni1 := awsutils.ENIMetadata{
		ENIID:          primaryENIid,
		MAC:            primaryMAC,
		DeviceNumber:   primaryDevice,
		SubnetIPv4CIDR: primarySubnet,
		LocalIPv4s:     []string{ipaddr01, ipaddr02},
	}

	eni2 := awsutils.ENIMetadata{
		ENIID:          secENIid,
		MAC:            secMAC,
		DeviceNumber:   secDevice,
		SubnetIPv4CIDR: secSubnet,
		LocalIPv4s:     []string{ipaddr11, ipaddr12},
	}
	mockAWS.EXPECT().GetAttachedENIs().Return([]awsutils.ENIMetadata{eni1, eni2}, nil)
	mockAWS.EXPECT().GetVPCIPv4CIDR().Return(vpcCIDR)
	mockAWS.EXPECT().GetLocalIPv4().Return(ipaddr01)

	_, vpcCIDR, _ := net.ParseCIDR(vpcCIDR)
	primaryIP := net.ParseIP(ipaddr01)
	mockNetwork.EXPECT().SetupHostNetwork(vpcCIDR, &primaryIP).Return(nil)

	//primaryENIid
	mockAWS.EXPECT().GetPrimaryENI().Return(primaryENIid)
	mockAWS.EXPECT().AllocAllIPAddress(primaryENIid).Return(nil)
	attachmentID := testAttachmentID
	testAddr1 := ipaddr01
	testAddr2 := ipaddr02
	primary := true
	eniResp := []*ec2.NetworkInterfacePrivateIpAddress{
		&ec2.NetworkInterfacePrivateIpAddress{
			PrivateIpAddress: &testAddr1, Primary: &primary},
		&ec2.NetworkInterfacePrivateIpAddress{
			PrivateIpAddress: &testAddr2, Primary: &primary}}
	mockAWS.EXPECT().GetPrimaryENI().Return(primaryENIid)
	mockAWS.EXPECT().DescribeENI(primaryENIid).Return(eniResp, &attachmentID, nil)

	//secENIid
	mockAWS.EXPECT().GetPrimaryENI().Return(primaryENIid)
	mockAWS.EXPECT().AllocAllIPAddress(secENIid).Return(nil)
	attachmentID = testAttachmentID
	testAddr11 := ipaddr11
	testAddr12 := ipaddr12
	primary = false
	eniResp = []*ec2.NetworkInterfacePrivateIpAddress{
		&ec2.NetworkInterfacePrivateIpAddress{
			PrivateIpAddress: &testAddr11, Primary: &primary},
		&ec2.NetworkInterfacePrivateIpAddress{
			PrivateIpAddress: &testAddr12, Primary: &primary}}
	mockAWS.EXPECT().GetPrimaryENI().Return(primaryENIid)
	mockAWS.EXPECT().DescribeENI(secENIid).Return(eniResp, &attachmentID, nil)
	mockNetwork.EXPECT().SetupENINetwork(gomock.Any(), secMAC, secDevice, secSubnet)

	mockAWS.EXPECT().GetLocalIPv4().Return(ipaddr01)
	mockK8S.EXPECT().K8SGetLocalPodIPs(gomock.Any()).Return([]*k8sapi.K8SPodInfo{&k8sapi.K8SPodInfo{Name: "pod1",
		Namespace: "default"}}, nil)

	err := mockContext.nodeInit()
	assert.NoError(t, err)
}

func TestIncreaseIPPool(t *testing.T) {
	ctrl, mockAWS, mockK8S, mockNetwork := setup(t)
	defer ctrl.Finish()

	mockContext := &IPAMContext{
		awsClient:     mockAWS,
		k8sClient:     mockK8S,
		networkClient: mockNetwork,
	}

	mockContext.dataStore = datastore.NewDataStore()

	eni2 := secENIid

	mockAWS.EXPECT().AllocENI().Return(eni2, nil)

	mockAWS.EXPECT().AllocAllIPAddress(eni2)

	mockAWS.EXPECT().GetAttachedENIs().Return([]awsutils.ENIMetadata{
		awsutils.ENIMetadata{
			ENIID:          primaryENIid,
			MAC:            primaryMAC,
			DeviceNumber:   primaryDevice,
			SubnetIPv4CIDR: primarySubnet,
			LocalIPv4s:     []string{ipaddr01, ipaddr02},
		},
		awsutils.ENIMetadata{
			ENIID:          secENIid,
			MAC:            secMAC,
			DeviceNumber:   secDevice,
			SubnetIPv4CIDR: secSubnet,
			LocalIPv4s:     []string{ipaddr11, ipaddr12}},
	}, nil)

	mockAWS.EXPECT().GetPrimaryENI().Return(primaryENIid)

	primary := false
	attachmentID := testAttachmentID
	testAddr11 := ipaddr11
	testAddr12 := ipaddr12

	mockAWS.EXPECT().DescribeENI(eni2).Return(
		[]*ec2.NetworkInterfacePrivateIpAddress{
			&ec2.NetworkInterfacePrivateIpAddress{
				PrivateIpAddress: &testAddr11, Primary: &primary},
			&ec2.NetworkInterfacePrivateIpAddress{
				PrivateIpAddress: &testAddr12, Primary: &primary}}, &attachmentID, nil)

	mockAWS.EXPECT().GetPrimaryENI().Return(primaryENIid)
	mockNetwork.EXPECT().SetupENINetwork(gomock.Any(), secMAC, secDevice, secSubnet)

	mockContext.increaseIPPool()

}
