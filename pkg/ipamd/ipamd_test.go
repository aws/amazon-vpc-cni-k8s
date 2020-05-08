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

package ipamd

import (
	"net"
	"os"
	"reflect"
	"testing"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/apis/crd/v1alpha1"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/awsutils"
	mock_awsutils "github.com/aws/amazon-vpc-cni-k8s/pkg/awsutils/mocks"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/cri"
	mock_cri "github.com/aws/amazon-vpc-cni-k8s/pkg/cri/mocks"
	mock_eniconfig "github.com/aws/amazon-vpc-cni-k8s/pkg/eniconfig/mocks"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/ipamd/datastore"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/k8sapi"
	mock_k8sapi "github.com/aws/amazon-vpc-cni-k8s/pkg/k8sapi/mocks"
	mock_networkutils "github.com/aws/amazon-vpc-cni-k8s/pkg/networkutils/mocks"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/vishvananda/netlink"
)

const (
	primaryENIid  = "eni-00000000"
	secENIid      = "eni-00000001"
	primaryMAC    = "12:ef:2a:98:e5:5a"
	secMAC        = "12:ef:2a:98:e5:5b"
	primaryDevice = 0
	secDevice     = 2
	primarySubnet = "10.10.10.0/24"
	secSubnet     = "10.10.20.0/24"
	ipaddr01      = "10.10.10.11"
	ipaddr02      = "10.10.10.12"
	ipaddr03      = "10.10.10.13"
	ipaddr11      = "10.10.20.11"
	ipaddr12      = "10.10.20.12"
	vpcCIDR       = "10.10.0.0/16"
)

func setup(t *testing.T) (*gomock.Controller,
	*mock_awsutils.MockAPIs,
	*mock_k8sapi.MockK8SAPIs,
	*mock_cri.MockAPIs,
	*mock_networkutils.MockNetworkAPIs,
	*mock_eniconfig.MockENIConfig) {
	ctrl := gomock.NewController(t)
	return ctrl,
		mock_awsutils.NewMockAPIs(ctrl),
		mock_k8sapi.NewMockK8SAPIs(ctrl),
		mock_cri.NewMockAPIs(ctrl),
		mock_networkutils.NewMockNetworkAPIs(ctrl),
		mock_eniconfig.NewMockENIConfig(ctrl)
}

func TestNodeInit(t *testing.T) {
	ctrl, mockAWS, mockK8S, mockCRI, mockNetwork, _ := setup(t)
	defer ctrl.Finish()

	mockContext := &IPAMContext{
		awsClient:     mockAWS,
		k8sClient:     mockK8S,
		maxIPsPerENI:  14,
		maxENI:        4,
		warmENITarget: 1,
		warmIPTarget:  3,
		primaryIP:     make(map[string]string),
		terminating:   int32(0),
		criClient:     mockCRI,
		networkClient: mockNetwork}

	eni1, eni2 := getDummyENIMetdata()

	var cidrs []*string
	mockAWS.EXPECT().GetENILimit().Return(4, nil)
	mockAWS.EXPECT().GetENIipLimit().Return(14, nil)
	mockAWS.EXPECT().GetIPv4sFromEC2(eni1.ENIID).Return(eni1.IPv4Addresses, nil)
	mockAWS.EXPECT().GetVPCIPv4CIDR().Return(vpcCIDR)

	_, parsedVPCCIDR, _ := net.ParseCIDR(vpcCIDR)
	primaryIP := net.ParseIP(ipaddr01)
	mockAWS.EXPECT().GetVPCIPv4CIDRs().Return(cidrs)
	mockAWS.EXPECT().GetPrimaryENImac().Return("")
	mockNetwork.EXPECT().SetupHostNetwork(parsedVPCCIDR, cidrs, "", &primaryIP).Return(nil)

	mockAWS.EXPECT().GetPrimaryENI().AnyTimes().Return(primaryENIid)

	eniMetadataSlice := []awsutils.ENIMetadata{eni1, eni2}
	mockAWS.EXPECT().DescribeAllENIs().Return(eniMetadataSlice, map[string]awsutils.TagMap{}, nil)
	mockNetwork.EXPECT().SetupENINetwork(gomock.Any(), secMAC, secDevice, secSubnet)

	mockAWS.EXPECT().GetLocalIPv4().Return(ipaddr01)
	k8sName := "/k8s_POD_" + "pod1" + "_" + "default" + "_" + "pod-uid" + "_0"
	mockK8S.EXPECT().K8SGetLocalPodIPs().Return([]*k8sapi.K8SPodInfo{{Name: "pod1",
		Namespace: "default", UID: "pod-uid", IP: ipaddr02}}, nil)

	var criList = make(map[string]*cri.SandboxInfo, 0)
	criList["pod-uid"] = &cri.SandboxInfo{ID: "sandbox-id",
		Name: k8sName, K8SUID: "pod-uid"}
	mockCRI.EXPECT().GetRunningPodSandboxes(gomock.Any()).Return(criList, nil)

	var rules []netlink.Rule
	mockNetwork.EXPECT().GetRuleList().Return(rules, nil)

	mockNetwork.EXPECT().UseExternalSNAT().Return(false)
	mockNetwork.EXPECT().UpdateRuleListBySrc(gomock.Any(), gomock.Any(), gomock.Any(), true)
	// Add IPs
	mockAWS.EXPECT().AllocIPAddresses(gomock.Any(), gomock.Any())

	err := mockContext.nodeInit()
	assert.NoError(t, err)
}

func getDummyENIMetdata() (awsutils.ENIMetadata, awsutils.ENIMetadata) {
	primary := true
	notPrimary := false
	testAddr1 := ipaddr01
	testAddr2 := ipaddr02
	testAddr11 := ipaddr11
	testAddr12 := ipaddr12
	eni1 := awsutils.ENIMetadata{
		ENIID:          primaryENIid,
		MAC:            primaryMAC,
		DeviceNumber:   primaryDevice,
		SubnetIPv4CIDR: primarySubnet,
		IPv4Addresses: []*ec2.NetworkInterfacePrivateIpAddress{
			{
				PrivateIpAddress: &testAddr1, Primary: &primary,
			},
			{
				PrivateIpAddress: &testAddr2, Primary: &notPrimary,
			},
		},
	}

	eni2 := awsutils.ENIMetadata{
		ENIID:          secENIid,
		MAC:            secMAC,
		DeviceNumber:   secDevice,
		SubnetIPv4CIDR: secSubnet,
		IPv4Addresses: []*ec2.NetworkInterfacePrivateIpAddress{
			{
				PrivateIpAddress: &testAddr11, Primary: &notPrimary,
			},
			{
				PrivateIpAddress: &testAddr12, Primary: &notPrimary,
			},
		},
	}
	return eni1, eni2
}

func TestIncreaseIPPoolDefault(t *testing.T) {
	_ = os.Unsetenv(envCustomNetworkCfg)
	testIncreaseIPPool(t, false)
}

func TestIncreaseIPPoolCustomENI(t *testing.T) {
	_ = os.Setenv(envCustomNetworkCfg, "true")
	testIncreaseIPPool(t, true)
}

func testIncreaseIPPool(t *testing.T, useENIConfig bool) {
	ctrl, mockAWS, mockK8S, _, mockNetwork, mockENIConfig := setup(t)
	defer ctrl.Finish()

	mockContext := &IPAMContext{
		awsClient:           mockAWS,
		k8sClient:           mockK8S,
		maxIPsPerENI:        14,
		maxENI:              4,
		warmENITarget:       1,
		networkClient:       mockNetwork,
		useCustomNetworking: UseCustomNetworkCfg(),
		eniConfig:           mockENIConfig,
		primaryIP:           make(map[string]string),
		terminating:         int32(0),
	}

	mockContext.dataStore = datastore.NewDataStore(log)

	primary := true
	notPrimary := false
	testAddr1 := ipaddr01
	testAddr2 := ipaddr02
	testAddr11 := ipaddr11
	testAddr12 := ipaddr12
	eni2 := secENIid

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

	mockAWS.EXPECT().GetAttachedENIs().Return([]awsutils.ENIMetadata{
		{
			ENIID:          primaryENIid,
			MAC:            primaryMAC,
			DeviceNumber:   primaryDevice,
			SubnetIPv4CIDR: primarySubnet,
			IPv4Addresses: []*ec2.NetworkInterfacePrivateIpAddress{
				{
					PrivateIpAddress: &testAddr1, Primary: &primary,
				},
				{
					PrivateIpAddress: &testAddr2, Primary: &primary,
				},
			},
		},
		{
			ENIID:          secENIid,
			MAC:            secMAC,
			DeviceNumber:   secDevice,
			SubnetIPv4CIDR: secSubnet,
			IPv4Addresses: []*ec2.NetworkInterfacePrivateIpAddress{
				{
					PrivateIpAddress: &testAddr11, Primary: &notPrimary,
				},
				{
					PrivateIpAddress: &testAddr12, Primary: &notPrimary,
				},
			},
		},
	}, nil)

	mockAWS.EXPECT().GetPrimaryENI().Return(primaryENIid)
	mockNetwork.EXPECT().SetupENINetwork(gomock.Any(), secMAC, secDevice, secSubnet)

	mockAWS.EXPECT().AllocIPAddresses(eni2, 14)
	mockAWS.EXPECT().GetPrimaryENI().Return(primaryENIid)

	mockContext.increaseIPPool()
}

func TestTryAddIPToENI(t *testing.T) {
	_ = os.Unsetenv(envCustomNetworkCfg)
	ctrl, mockAWS, mockK8S, _, mockNetwork, mockENIConfig := setup(t)
	defer ctrl.Finish()

	primary := true
	notPrimary := false
	testAddr1 := ipaddr01
	testAddr2 := ipaddr02
	testAddr11 := ipaddr11
	testAddr12 := ipaddr12

	warmIpTarget := 3
	mockContext := &IPAMContext{
		awsClient:     mockAWS,
		k8sClient:     mockK8S,
		maxIPsPerENI:  14,
		maxENI:        4,
		warmENITarget: 1,
		warmIPTarget:  warmIpTarget,
		networkClient: mockNetwork,
		eniConfig:     mockENIConfig,
		primaryIP:     make(map[string]string),
		terminating:   int32(0),
	}

	mockContext.dataStore = datastore.NewDataStore(log)

	podENIConfig := &v1alpha1.ENIConfigSpec{
		SecurityGroups: []string{"sg1-id", "sg2-id"},
		Subnet:         "subnet1",
	}
	var sg []*string
	for _, sgID := range podENIConfig.SecurityGroups {
		sg = append(sg, aws.String(sgID))
	}

	mockAWS.EXPECT().AllocENI(false, nil, "").Return(secENIid, nil)
	mockAWS.EXPECT().AllocIPAddresses(secENIid, warmIpTarget)
	mockAWS.EXPECT().GetAttachedENIs().Return([]awsutils.ENIMetadata{
		{
			ENIID:          primaryENIid,
			MAC:            primaryMAC,
			DeviceNumber:   primaryDevice,
			SubnetIPv4CIDR: primarySubnet,
			IPv4Addresses: []*ec2.NetworkInterfacePrivateIpAddress{
				{
					PrivateIpAddress: &testAddr1, Primary: &primary,
				},
				{
					PrivateIpAddress: &testAddr2, Primary: &notPrimary,
				},
			},
		},
		{
			ENIID:          secENIid,
			MAC:            secMAC,
			DeviceNumber:   secDevice,
			SubnetIPv4CIDR: secSubnet,
			IPv4Addresses: []*ec2.NetworkInterfacePrivateIpAddress{
				{
					PrivateIpAddress: &testAddr11, Primary: &notPrimary,
				},
				{
					PrivateIpAddress: &testAddr12, Primary: &notPrimary,
				},
			},
		},
	}, nil)
	mockAWS.EXPECT().GetPrimaryENI().Return(primaryENIid)
	mockNetwork.EXPECT().SetupENINetwork(gomock.Any(), secMAC, secDevice, secSubnet)
	mockAWS.EXPECT().GetPrimaryENI().Return(primaryENIid)

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
		terminating:   int32(0),
	}

	mockContext.dataStore = datastore.NewDataStore(log)

	primary := true
	notPrimary := false
	testAddr1 := ipaddr01
	testAddr2 := ipaddr02

	eniMetadata := []awsutils.ENIMetadata{
		{
			ENIID:          primaryENIid,
			MAC:            primaryMAC,
			DeviceNumber:   primaryDevice,
			SubnetIPv4CIDR: primarySubnet,
			IPv4Addresses: []*ec2.NetworkInterfacePrivateIpAddress{
				{
					PrivateIpAddress: &testAddr1, Primary: &primary,
				},
				{
					PrivateIpAddress: &testAddr2, Primary: &notPrimary,
				},
			},
		},
	}
	mockAWS.EXPECT().GetAttachedENIs().Return(eniMetadata, nil)
	mockAWS.EXPECT().GetPrimaryENI().Times(2).Return(primaryENIid)
	mockAWS.EXPECT().DescribeAllENIs().Return(eniMetadata, map[string]awsutils.TagMap{}, nil)

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
			IPv4Addresses: []*ec2.NetworkInterfacePrivateIpAddress{
				{
					PrivateIpAddress: &testAddr1, Primary: &primary,
				},
			},
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

	_ = os.Setenv("WARM_IP_TARGET", "5")
	warmIPTarget := getWarmIPTarget()
	assert.Equal(t, warmIPTarget, 5)

	_ = os.Unsetenv("WARM_IP_TARGET")
	warmIPTarget = getWarmIPTarget()
	assert.Equal(t, warmIPTarget, noWarmIPTarget)

	_ = os.Setenv("WARM_IP_TARGET", "non-integer-string")
	warmIPTarget = getWarmIPTarget()
	assert.Equal(t, warmIPTarget, noWarmIPTarget)
}

func TestGetWarmIPTargetState(t *testing.T) {
	ctrl, mockAWS, mockK8S, _, mockNetwork, _ := setup(t)
	defer ctrl.Finish()

	mockContext := &IPAMContext{
		awsClient:     mockAWS,
		k8sClient:     mockK8S,
		networkClient: mockNetwork,
		primaryIP:     make(map[string]string),
		terminating:   int32(0),
	}

	mockContext.dataStore = datastore.NewDataStore(log)

	_, _, warmIPTargetDefined := mockContext.ipTargetState()
	assert.False(t, warmIPTargetDefined)

	mockContext.warmIPTarget = 5
	short, over, warmIPTargetDefined := mockContext.ipTargetState()
	assert.True(t, warmIPTargetDefined)
	assert.Equal(t, 5, short)
	assert.Equal(t, 0, over)

	// add 2 addresses to datastore
	_ = mockContext.dataStore.AddENI("eni-1", 1, true)
	_ = mockContext.dataStore.AddIPv4AddressToStore("eni-1", "1.1.1.1")
	_ = mockContext.dataStore.AddIPv4AddressToStore("eni-1", "1.1.1.2")

	short, over, warmIPTargetDefined = mockContext.ipTargetState()
	assert.True(t, warmIPTargetDefined)
	assert.Equal(t, 3, short)
	assert.Equal(t, 0, over)

	// add 3 more addresses to datastore
	_ = mockContext.dataStore.AddIPv4AddressToStore("eni-1", "1.1.1.3")
	_ = mockContext.dataStore.AddIPv4AddressToStore("eni-1", "1.1.1.4")
	_ = mockContext.dataStore.AddIPv4AddressToStore("eni-1", "1.1.1.5")

	short, over, warmIPTargetDefined = mockContext.ipTargetState()
	assert.True(t, warmIPTargetDefined)
	assert.Equal(t, 0, short)
	assert.Equal(t, 0, over)
}

func TestIPAMContext_nodeIPPoolTooLow(t *testing.T) {
	ctrl, mockAWS, mockK8S, _, mockNetwork, mockENIConfig := setup(t)
	defer ctrl.Finish()

	type fields struct {
		maxIPsPerENI  int
		warmENITarget int
		warmIPTarget  int
		datastore     *datastore.DataStore
	}

	tests := []struct {
		name   string
		fields fields
		want   bool
	}{
		{"Test new ds, all defaults", fields{14, 1, 0, datastore.NewDataStore(log)}, true},
		{"Test new ds, 0 ENIs", fields{14, 0, 0, datastore.NewDataStore(log)}, true},
		{"Test new ds, 3 warm IPs", fields{14, 0, 3, datastore.NewDataStore(log)}, true},
		{"Test 3 unused IPs, 1 warm", fields{3, 1, 1, datastoreWith3FreeIPs()}, false},
		{"Test 1 used, 1 warm ENI", fields{3, 1, 0, datastoreWith1Pod1()}, true},
		{"Test 1 used, 0 warm ENI", fields{3, 0, 0, datastoreWith1Pod1()}, false},
		{"Test 3 used, 1 warm ENI", fields{3, 1, 0, datastoreWith3Pods()}, true},
		{"Test 3 used, 0 warm ENI", fields{3, 0, 0, datastoreWith3Pods()}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &IPAMContext{
				awsClient:           mockAWS,
				dataStore:           tt.fields.datastore,
				k8sClient:           mockK8S,
				useCustomNetworking: false,
				eniConfig:           mockENIConfig,
				networkClient:       mockNetwork,
				maxIPsPerENI:        tt.fields.maxIPsPerENI,
				maxENI:              -1,
				warmENITarget:       tt.fields.warmENITarget,
				warmIPTarget:        tt.fields.warmIPTarget,
			}
			if got := c.nodeIPPoolTooLow(); got != tt.want {
				t.Errorf("nodeIPPoolTooLow() = %v, want %v", got, tt.want)
			}
		})
	}
}

func datastoreWith3FreeIPs() *datastore.DataStore {
	datastoreWith3FreeIPs := datastore.NewDataStore(log)
	_ = datastoreWith3FreeIPs.AddENI(primaryENIid, 1, true)
	_ = datastoreWith3FreeIPs.AddIPv4AddressToStore(primaryENIid, ipaddr01)
	_ = datastoreWith3FreeIPs.AddIPv4AddressToStore(primaryENIid, ipaddr02)
	_ = datastoreWith3FreeIPs.AddIPv4AddressToStore(primaryENIid, ipaddr03)
	return datastoreWith3FreeIPs
}

func datastoreWith1Pod1() *datastore.DataStore {
	datastoreWith1Pod1 := datastoreWith3FreeIPs()

	podInfo1 := k8sapi.K8SPodInfo{
		Name:      "pod-1",
		Namespace: "ns-1",
		IP:        ipaddr01,
	}
	_, _, _ = datastoreWith1Pod1.AssignPodIPv4Address(&podInfo1)
	return datastoreWith1Pod1
}

func datastoreWith3Pods() *datastore.DataStore {
	datastoreWith3Pods := datastoreWith3FreeIPs()

	podInfo1 := k8sapi.K8SPodInfo{
		Name:      "pod-1",
		Namespace: "ns-1",
		IP:        ipaddr01,
	}
	_, _, _ = datastoreWith3Pods.AssignPodIPv4Address(&podInfo1)

	podInfo2 := k8sapi.K8SPodInfo{
		Name:      "pod-2",
		Namespace: "ns-1",
		IP:        ipaddr02,
	}
	_, _, _ = datastoreWith3Pods.AssignPodIPv4Address(&podInfo2)

	podInfo3 := k8sapi.K8SPodInfo{
		Name:      "pod-3",
		Namespace: "ns-1",
		IP:        ipaddr03,
	}
	_, _, _ = datastoreWith3Pods.AssignPodIPv4Address(&podInfo3)
	return datastoreWith3Pods
}

func TestIPAMContext_filterUnmanagedENIs(t *testing.T) {
	ctrl := gomock.NewController(t)

	eni1, eni2 := getDummyENIMetdata()
	allENIs := []awsutils.ENIMetadata{eni1, eni2}
	primaryENIonly := []awsutils.ENIMetadata{eni1}
	eni1TagMap := map[string]awsutils.TagMap{eni1.ENIID: {"hi": "tag", eniNoManageTagKey: "true"}}
	eni2TagMap := map[string]awsutils.TagMap{eni2.ENIID: {"hi": "tag", eniNoManageTagKey: "true"}}

	mockAWSUtils := mock_awsutils.NewMockAPIs(ctrl)
	mockAWSUtils.EXPECT().GetPrimaryENI().Times(2).Return(eni1.ENIID)

	tests := []struct {
		name   string
		tagMap map[string]awsutils.TagMap
		enis   []awsutils.ENIMetadata
		want   []awsutils.ENIMetadata
	}{
		{"No tags at all", nil, allENIs, allENIs},
		{"Primary ENI unmanaged", eni1TagMap, allENIs, allENIs},
		{"Secondary ENI unmanaged", eni2TagMap, allENIs, primaryENIonly},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &IPAMContext{awsClient: mockAWSUtils}
			c.setUnmanagedENIs(tt.tagMap)
			if got := c.filterUnmanagedENIs(tt.enis); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("filterUnmanagedENIs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDisablingENIProvisioning(t *testing.T) {
	ctrl, _, _, _, _, _ := setup(t)
	defer ctrl.Finish()

	_ = os.Setenv(envDisableENIProvisioning, "true")
	disabled := disablingENIProvisioning()
	assert.True(t, disabled)

	_ = os.Unsetenv(envDisableENIProvisioning)
	disabled = disablingENIProvisioning()
	assert.False(t, disabled)
}
