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
	"os"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/pkg/errors"
	"github.com/vishvananda/netlink"

	"github.com/aws/amazon-vpc-cni-k8s/ipamd/datastore"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/apis/crd/v1alpha1"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/awsutils"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/awsutils/mocks"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/docker"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/docker/mocks"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/eniconfig/mocks"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/k8sapi"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/k8sapi/mocks"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/networkutils/mocks"

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
	primaryDevice    = 0
	secDevice        = 2
	primarySubnet    = "10.10.10.0/24"
	secSubnet        = "10.10.20.0/24"
	ipaddr01         = "10.10.10.11"
	ipaddr02         = "10.10.10.12"
	ipaddr03         = "10.10.10.13"
	ipaddr11         = "10.10.20.11"
	ipaddr12         = "10.10.20.12"
	ipaddr13         = "10.10.20.13"
	vpcCIDR          = "10.10.0.0/16"
)

func setup(t *testing.T) (*gomock.Controller,
	*mock_awsutils.MockAPIs,
	*mock_k8sapi.MockK8SAPIs,
	*mock_docker.MockAPIs,
	*mock_networkutils.MockNetworkAPIs,
	*mock_eniconfig.MockENIConfig) {
	ctrl := gomock.NewController(t)
	return ctrl,
		mock_awsutils.NewMockAPIs(ctrl),
		mock_k8sapi.NewMockK8SAPIs(ctrl),
		mock_docker.NewMockAPIs(ctrl),
		mock_networkutils.NewMockNetworkAPIs(ctrl),
		mock_eniconfig.NewMockENIConfig(ctrl)
}

func TestNodeInit(t *testing.T) {
	ctrl, mockAWS, mockK8S, mockDocker, mockNetwork, _ := setup(t)
	defer ctrl.Finish()

	mockContext := &IPAMContext{
		awsClient:     mockAWS,
		k8sClient:     mockK8S,
		dockerClient:  mockDocker,
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
	var cidrs []*string
	mockAWS.EXPECT().GetENILimit().Return(4, nil)
	mockAWS.EXPECT().GetENIipLimit().Return(int64(56), nil)
	mockAWS.EXPECT().GetAttachedENIs().Return([]awsutils.ENIMetadata{eni1, eni2}, nil)
	mockAWS.EXPECT().GetVPCIPv4CIDR().Return(vpcCIDR)

	_, vpcCIDR, _ := net.ParseCIDR(vpcCIDR)
	primaryIP := net.ParseIP(ipaddr01)
	mockAWS.EXPECT().GetVPCIPv4CIDRs().Return(cidrs)
	mockAWS.EXPECT().GetPrimaryENImac().Return("")
	mockNetwork.EXPECT().SetupHostNetwork(vpcCIDR, cidrs, "", &primaryIP).Return(nil)

	//primaryENIid
	mockAWS.EXPECT().GetPrimaryENI().Return(primaryENIid)
	attachmentID := testAttachmentID
	testAddr1 := ipaddr01
	testAddr2 := ipaddr02
	primary := true
	notPrimary := false
	eniResp := []*ec2.NetworkInterfacePrivateIpAddress{
		{
			PrivateIpAddress: &testAddr1, Primary: &primary},
		{
			PrivateIpAddress: &testAddr2, Primary: &notPrimary}}
	mockAWS.EXPECT().GetENIipLimit().Return(int64(56), nil)
	mockAWS.EXPECT().GetPrimaryENI().Return(primaryENIid)
	mockAWS.EXPECT().DescribeENI(primaryENIid).Return(eniResp, &attachmentID, nil)

	//secENIid
	mockAWS.EXPECT().GetPrimaryENI().Return(primaryENIid)
	attachmentID = testAttachmentID
	testAddr11 := ipaddr11
	testAddr12 := ipaddr12
	eniResp = []*ec2.NetworkInterfacePrivateIpAddress{
		{
			PrivateIpAddress: &testAddr11, Primary: &primary},
		{
			PrivateIpAddress: &testAddr12, Primary: &notPrimary}}
	mockAWS.EXPECT().GetENIipLimit().Return(int64(56), nil)
	mockAWS.EXPECT().GetPrimaryENI().Return(primaryENIid)
	mockAWS.EXPECT().DescribeENI(secENIid).Return(eniResp, &attachmentID, nil)
	mockNetwork.EXPECT().SetupENINetwork(gomock.Any(), secMAC, secDevice, secSubnet)

	mockAWS.EXPECT().GetLocalIPv4().Return(ipaddr01)
	k8sName := "/k8s_POD_" + "pod1" + "_" + "default" + "_" + "pod-uid" + "_0"
	mockK8S.EXPECT().K8SGetLocalPodIPs().Return([]*k8sapi.K8SPodInfo{{Name: "pod1",
		Namespace: "default", UID: "pod-uid", IP: ipaddr02}}, nil)

	var dockerList = make(map[string]*docker.ContainerInfo, 0)
	dockerList["pod-uid"] = &docker.ContainerInfo{ID: "docker-id",
		Name: k8sName, K8SUID: "pod-uid"}
	mockDocker.EXPECT().GetRunningContainers().Return(dockerList, nil)

	var rules []netlink.Rule
	mockNetwork.EXPECT().GetRuleList().Return(rules, nil)

	mockAWS.EXPECT().GetVPCIPv4CIDRs().Return(cidrs)
	mockNetwork.EXPECT().UseExternalSNAT().Return(false)
	mockNetwork.EXPECT().UpdateRuleListBySrc(gomock.Any(), gomock.Any(), gomock.Any(), true)

	err := mockContext.nodeInit()
	assert.NoError(t, err)
}

func TestIncreaseIPPoolDefault(t *testing.T) {
	os.Unsetenv(envCustomNetworkCfg)
	testIncreaseIPPool(t, false)
}

func TestIncreaseIPPoolCustomENI(t *testing.T) {
	os.Setenv(envCustomNetworkCfg, "true")
	testIncreaseIPPool(t, true)
}

func TestIncreaseIPPoolCustomENINoCfg(t *testing.T) {
	os.Setenv(envCustomNetworkCfg, "true")
	ctrl, mockAWS, mockK8S, _, mockNetwork, mockENIConfig := setup(t)
	defer ctrl.Finish()

	mockContext := &IPAMContext{
		awsClient:     mockAWS,
		k8sClient:     mockK8S,
		networkClient: mockNetwork,
		eniConfig:     mockENIConfig,
		primaryIP:     make(map[string]string),
	}

	mockContext.dataStore = datastore.NewDataStore()

	mockAWS.EXPECT().GetENILimit().Return(4, nil)
	mockENIConfig.EXPECT().MyENIConfig().Return(nil, errors.New("no POD eni config"))

	mockContext.increaseIPPool()

}

func testIncreaseIPPool(t *testing.T, useENIConfig bool) {
	ctrl, mockAWS, mockK8S, _, mockNetwork, mockENIConfig := setup(t)
	defer ctrl.Finish()

	mockContext := &IPAMContext{
		awsClient:     mockAWS,
		k8sClient:     mockK8S,
		networkClient: mockNetwork,
		eniConfig:     mockENIConfig,
		primaryIP:     make(map[string]string),
	}

	mockContext.dataStore = datastore.NewDataStore()

	eni2 := secENIid

	mockAWS.EXPECT().GetENILimit().Return(4, nil)

	podENIConfig := &v1alpha1.ENIConfigSpec{
		SecurityGroups: []string{"sg1-id", "sg2-id"},
		Subnet:         "subnet1",
	}
	var sg []*string

	for _, sgID := range podENIConfig.SecurityGroups {
		sg = append(sg, aws.String(sgID))
	}

	if useENIConfig {
		mockENIConfig.EXPECT().MyENIConfig().Return(podENIConfig, nil)
		mockAWS.EXPECT().AllocENI(true, sg, podENIConfig.Subnet).Return(eni2, nil)
	} else {
		mockAWS.EXPECT().AllocENI(false, nil, "").Return(eni2, nil)
	}

	mockAWS.EXPECT().GetENIipLimit().Return(int64(5), nil)

	mockAWS.EXPECT().AllocIPAddresses(eni2, int64(5))

	mockAWS.EXPECT().GetAttachedENIs().Return([]awsutils.ENIMetadata{
		{
			ENIID:          primaryENIid,
			MAC:            primaryMAC,
			DeviceNumber:   primaryDevice,
			SubnetIPv4CIDR: primarySubnet,
			LocalIPv4s:     []string{ipaddr01, ipaddr02},
		},
		{
			ENIID:          secENIid,
			MAC:            secMAC,
			DeviceNumber:   secDevice,
			SubnetIPv4CIDR: secSubnet,
			LocalIPv4s:     []string{ipaddr11, ipaddr12}},
	}, nil)

	mockAWS.EXPECT().GetENIipLimit().Return(int64(5), nil)
	mockAWS.EXPECT().GetPrimaryENI().Return(primaryENIid)

	primary := true
	notPrimary := false
	attachmentID := testAttachmentID
	testAddr11 := ipaddr11
	testAddr12 := ipaddr12

	mockAWS.EXPECT().DescribeENI(eni2).Return(
		[]*ec2.NetworkInterfacePrivateIpAddress{
			{
				PrivateIpAddress: &testAddr11, Primary: &primary},
			{
				PrivateIpAddress: &testAddr12, Primary: &notPrimary}}, &attachmentID, nil)

	mockAWS.EXPECT().GetPrimaryENI().Return(primaryENIid)
	mockNetwork.EXPECT().SetupENINetwork(gomock.Any(), secMAC, secDevice, secSubnet)

	mockContext.increaseIPPool()

}

func TestNodeIPPoolReconcile(t *testing.T) {
	ctrl, mockAWS, mockK8S, _, mockNetwork, _ := setup(t)
	defer ctrl.Finish()

	mockContext := &IPAMContext{
		awsClient:     mockAWS,
		k8sClient:     mockK8S,
		networkClient: mockNetwork,
		primaryIP:     make(map[string]string),
	}

	mockContext.dataStore = datastore.NewDataStore()

	mockAWS.EXPECT().GetAttachedENIs().Return([]awsutils.ENIMetadata{
		{
			ENIID:          primaryENIid,
			MAC:            primaryMAC,
			DeviceNumber:   primaryDevice,
			SubnetIPv4CIDR: primarySubnet,
			LocalIPv4s:     []string{ipaddr01, ipaddr02},
		},
	}, nil)

	mockAWS.EXPECT().GetPrimaryENI().Return(primaryENIid)

	primary := true
	notPrimary := false
	attachmentID := testAttachmentID
	testAddr1 := ipaddr01
	testAddr2 := ipaddr02

	mockAWS.EXPECT().DescribeENI(primaryENIid).Return(
		[]*ec2.NetworkInterfacePrivateIpAddress{
			{
				PrivateIpAddress: &testAddr1, Primary: &primary},
			{
				PrivateIpAddress: &testAddr2, Primary: &notPrimary}}, &attachmentID, nil)
	mockAWS.EXPECT().GetENIipLimit().Return(int64(5), nil)
	mockAWS.EXPECT().GetPrimaryENI().Return(primaryENIid)

	mockContext.nodeIPPoolReconcile(0)

	curENIs := mockContext.dataStore.GetENIInfos()
	assert.Equal(t, len(curENIs.ENIIPPools), 1)
	assert.Equal(t, curENIs.TotalIPs, 1)

	// remove 1 IP
	mockAWS.EXPECT().GetAttachedENIs().Return([]awsutils.ENIMetadata{
		{
			ENIID:          primaryENIid,
			MAC:            primaryMAC,
			DeviceNumber:   primaryDevice,
			SubnetIPv4CIDR: primarySubnet,
			LocalIPv4s:     []string{ipaddr01},
		},
	}, nil)

	mockContext.nodeIPPoolReconcile(0)
	curENIs = mockContext.dataStore.GetENIInfos()
	assert.Equal(t, len(curENIs.ENIIPPools), 1)
	assert.Equal(t, curENIs.TotalIPs, 0)

	// remove eni
	mockAWS.EXPECT().GetAttachedENIs().Return(nil, nil)

	mockContext.nodeIPPoolReconcile(0)
	curENIs = mockContext.dataStore.GetENIInfos()
	assert.Equal(t, len(curENIs.ENIIPPools), 0)
	assert.Equal(t, curENIs.TotalIPs, 0)
}

func TestGetWarmENITarget(t *testing.T) {
	ctrl, _, _, _, _, _ := setup(t)
	defer ctrl.Finish()

	os.Setenv("WARM_IP_TARGET", "5")
	warmIPTarget := getWarmIPTarget()
	assert.Equal(t, warmIPTarget, 5)

	os.Unsetenv("WARM_IP_TARGET")
	warmIPTarget = getWarmIPTarget()
	assert.Equal(t, warmIPTarget, noWarmIPTarget)

	os.Setenv("WARM_IP_TARGET", "non-integer-string")
	warmIPTarget = getWarmIPTarget()
	assert.Equal(t, warmIPTarget, noWarmIPTarget)
}

func TestGetMaxENI(t *testing.T) {
	ctrl, _, _, _, _, _ := setup(t)
	defer ctrl.Finish()

	// MaxENI 5 is less than lower bound of 10, so 5
	os.Setenv("MAX_ENI", "5")
	maxENI := getMaxENI(10)
	assert.Equal(t, maxENI, 5)

	// MaxENI 5 is greater than lower bound of 4, so 4
	os.Setenv("MAX_ENI", "5")
	maxENI = getMaxENI(4)
	assert.Equal(t, maxENI, 4)

	// MaxENI 0 is 0, which means disabled; so use lower bound
	os.Setenv("MAX_ENI", "0")
	maxENI = getMaxENI(4)
	assert.Equal(t, maxENI, 4)

	// MaxENI 1 is less than lower bound of 4, so 1.
	os.Setenv("MAX_ENI", "1")
	maxENI = getMaxENI(4)
	assert.Equal(t, maxENI, 1)

	// Empty MaxENI means disabled, so use lower bound
	os.Unsetenv("MAX_ENI")
	maxENI = getMaxENI(10)
	assert.Equal(t, maxENI, 10)

	// Invalid MaxENI means disabled, so use lower bound
	os.Setenv("MAX_ENI", "non-integer-string")
	maxENI = getMaxENI(10)
	assert.Equal(t, maxENI, 10)
}

func TestGetCurWarmIPTarget(t *testing.T) {
	ctrl, mockAWS, mockK8S, _, mockNetwork, _ := setup(t)
	defer ctrl.Finish()

	mockContext := &IPAMContext{
		awsClient:     mockAWS,
		k8sClient:     mockK8S,
		networkClient: mockNetwork,
		primaryIP:     make(map[string]string),
	}

	mockContext.dataStore = datastore.NewDataStore()

	os.Unsetenv("WARM_IP_TARGET")
	_, warmIPTargetDefined := mockContext.getCurWarmIPTarget()
	assert.False(t, warmIPTargetDefined)

	os.Setenv("WARM_IP_TARGET", "5")
	curWarmIPTarget, warmIPTargetDefined := mockContext.getCurWarmIPTarget()
	assert.True(t, warmIPTargetDefined)
	assert.Equal(t, curWarmIPTarget, int64(5))

	// add 2 addresses to datastore
	mockContext.dataStore.AddENI("eni-1", 1, true)
	mockContext.dataStore.AddENIIPv4Address("eni-1", "1.1.1.1")
	mockContext.dataStore.AddENIIPv4Address("eni-1", "1.1.1.2")

	curWarmIPTarget, warmIPTargetDefined = mockContext.getCurWarmIPTarget()
	assert.True(t, warmIPTargetDefined)
	assert.Equal(t, curWarmIPTarget, int64(3))

	// add 3 more addresses to datastore
	mockContext.dataStore.AddENIIPv4Address("eni-1", "1.1.1.3")
	mockContext.dataStore.AddENIIPv4Address("eni-1", "1.1.1.4")
	mockContext.dataStore.AddENIIPv4Address("eni-1", "1.1.1.5")

	curWarmIPTarget, warmIPTargetDefined = mockContext.getCurWarmIPTarget()
	assert.True(t, warmIPTargetDefined)
	assert.Equal(t, curWarmIPTarget, int64(0))
}
