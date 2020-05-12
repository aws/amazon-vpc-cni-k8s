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
	"fmt"
	"net"
	"os"
	"reflect"
	"testing"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/apis/crd/v1alpha1"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/awsutils"
	mock_awsutils "github.com/aws/amazon-vpc-cni-k8s/pkg/awsutils/mocks"
	mock_eniconfig "github.com/aws/amazon-vpc-cni-k8s/pkg/eniconfig/mocks"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/ipamd/datastore"
	mock_networkutils "github.com/aws/amazon-vpc-cni-k8s/pkg/networkutils/mocks"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/vishvananda/netlink"
	k8s_fake "k8s.io/client-go/kubernetes/fake"
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

type testMocks struct {
	ctrl      *gomock.Controller
	awsutils  *mock_awsutils.MockAPIs
	clientset *k8s_fake.Clientset
	network   *mock_networkutils.MockNetworkAPIs
	eniconfig *mock_eniconfig.MockENIConfig
}

func setup(t *testing.T) *testMocks {
	ctrl := gomock.NewController(t)
	return &testMocks{
		ctrl:      ctrl,
		awsutils:  mock_awsutils.NewMockAPIs(ctrl),
		clientset: k8s_fake.NewSimpleClientset(),
		network:   mock_networkutils.NewMockNetworkAPIs(ctrl),
		eniconfig: mock_eniconfig.NewMockENIConfig(ctrl),
	}
}

func TestNodeInit(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	fakeCheckpoint := datastore.CheckpointData{
		Version: datastore.CheckpointFormatVersion,
		Allocations: []datastore.CheckpointEntry{
			{IPAMKey: datastore.IPAMKey{NetworkName: "net0", ContainerID: "sandbox-id", IfName: "eth0"}, IPv4: ipaddr02},
		},
	}

	mockContext := &IPAMContext{
		awsClient:     m.awsutils,
		maxIPsPerENI:  14,
		maxENI:        4,
		warmENITarget: 1,
		warmIPTarget:  3,
		primaryIP:     make(map[string]string),
		terminating:   int32(0),
		networkClient: m.network,
		dataStore:     datastore.NewDataStore(log, datastore.NewTestCheckpoint(fakeCheckpoint)),
	}

	eni1, eni2 := getDummyENIMetadata()

	var cidrs []*string
	m.awsutils.EXPECT().GetENILimit().Return(4, nil)
	m.awsutils.EXPECT().GetENIipLimit().Return(14, nil)
	m.awsutils.EXPECT().GetIPv4sFromEC2(eni1.ENIID).AnyTimes().Return(eni1.IPv4Addresses, nil)
	m.awsutils.EXPECT().GetIPv4sFromEC2(eni2.ENIID).AnyTimes().Return(eni2.IPv4Addresses, nil)
	m.awsutils.EXPECT().GetVPCIPv4CIDR().Return(vpcCIDR)

	_, parsedVPCCIDR, _ := net.ParseCIDR(vpcCIDR)
	primaryIP := net.ParseIP(ipaddr01)
	m.awsutils.EXPECT().GetVPCIPv4CIDRs().Return(cidrs)
	m.awsutils.EXPECT().GetPrimaryENImac().Return("")
	m.network.EXPECT().SetupHostNetwork(parsedVPCCIDR, cidrs, "", &primaryIP).Return(nil)

	m.awsutils.EXPECT().GetPrimaryENI().AnyTimes().Return(primaryENIid)

	eniMetadataSlice := []awsutils.ENIMetadata{eni1, eni2}
	m.awsutils.EXPECT().DescribeAllENIs().Return(eniMetadataSlice, map[string]awsutils.TagMap{}, nil)
	m.network.EXPECT().SetupENINetwork(gomock.Any(), secMAC, secDevice, secSubnet)

	m.awsutils.EXPECT().GetLocalIPv4().Return(ipaddr01)

	var rules []netlink.Rule
	m.network.EXPECT().GetRuleList().Return(rules, nil)

	m.network.EXPECT().UseExternalSNAT().Return(false)
	m.network.EXPECT().UpdateRuleListBySrc(gomock.Any(), gomock.Any(), gomock.Any(), true)
	// Add IPs
	m.awsutils.EXPECT().AllocIPAddresses(gomock.Any(), gomock.Any())

	err := mockContext.nodeInit()
	assert.NoError(t, err)
}

func getDummyENIMetadata() (awsutils.ENIMetadata, awsutils.ENIMetadata) {
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
	m := setup(t)
	defer m.ctrl.Finish()

	mockContext := &IPAMContext{
		awsClient:           m.awsutils,
		maxIPsPerENI:        14,
		maxENI:              4,
		warmENITarget:       1,
		networkClient:       m.network,
		useCustomNetworking: UseCustomNetworkCfg(),
		eniConfig:           m.eniconfig,
		primaryIP:           make(map[string]string),
		terminating:         int32(0),
	}

	mockContext.dataStore = testDatastore()

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
		m.eniconfig.EXPECT().MyENIConfig().Return(podENIConfig, nil)
		m.awsutils.EXPECT().AllocENI(true, sg, podENIConfig.Subnet).Return(eni2, nil)
	} else {
		m.awsutils.EXPECT().AllocENI(false, nil, "").Return(eni2, nil)
	}

	m.awsutils.EXPECT().GetAttachedENIs().Return([]awsutils.ENIMetadata{
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

	m.awsutils.EXPECT().GetPrimaryENI().Return(primaryENIid)
	m.network.EXPECT().SetupENINetwork(gomock.Any(), secMAC, secDevice, secSubnet)

	m.awsutils.EXPECT().AllocIPAddresses(eni2, 14)
	m.awsutils.EXPECT().GetPrimaryENI().Return(primaryENIid)

	mockContext.increaseIPPool()
}

func TestTryAddIPToENI(t *testing.T) {
	_ = os.Unsetenv(envCustomNetworkCfg)
	m := setup(t)
	defer m.ctrl.Finish()

	primary := true
	notPrimary := false
	testAddr1 := ipaddr01
	testAddr2 := ipaddr02
	testAddr11 := ipaddr11
	testAddr12 := ipaddr12

	warmIpTarget := 3
	mockContext := &IPAMContext{
		awsClient:     m.awsutils,
		maxIPsPerENI:  14,
		maxENI:        4,
		warmENITarget: 1,
		warmIPTarget:  warmIpTarget,
		networkClient: m.network,
		eniConfig:     m.eniconfig,
		primaryIP:     make(map[string]string),
		terminating:   int32(0),
	}

	mockContext.dataStore = testDatastore()

	podENIConfig := &v1alpha1.ENIConfigSpec{
		SecurityGroups: []string{"sg1-id", "sg2-id"},
		Subnet:         "subnet1",
	}
	var sg []*string
	for _, sgID := range podENIConfig.SecurityGroups {
		sg = append(sg, aws.String(sgID))
	}

	m.awsutils.EXPECT().AllocENI(false, nil, "").Return(secENIid, nil)
	m.awsutils.EXPECT().AllocIPAddresses(secENIid, warmIpTarget)
	m.awsutils.EXPECT().GetAttachedENIs().Return([]awsutils.ENIMetadata{
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
	m.awsutils.EXPECT().GetPrimaryENI().Return(primaryENIid)
	m.network.EXPECT().SetupENINetwork(gomock.Any(), secMAC, secDevice, secSubnet)
	m.awsutils.EXPECT().GetPrimaryENI().Return(primaryENIid)

	mockContext.increaseIPPool()
}

func TestNodeIPPoolReconcile(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	mockContext := &IPAMContext{
		awsClient:     m.awsutils,
		networkClient: m.network,
		primaryIP:     make(map[string]string),
		terminating:   int32(0),
	}

	mockContext.dataStore = testDatastore()

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
	m.awsutils.EXPECT().GetAttachedENIs().Return(eniMetadata, nil)
	m.awsutils.EXPECT().GetPrimaryENI().Times(2).Return(primaryENIid)
	m.awsutils.EXPECT().DescribeAllENIs().Return(eniMetadata, map[string]awsutils.TagMap{}, nil)

	mockContext.nodeIPPoolReconcile(0)

	curENIs := mockContext.dataStore.GetENIInfos()
	assert.Equal(t, len(curENIs.ENIs), 1)
	assert.Equal(t, curENIs.TotalIPs, 1)

	// remove 1 IP
	m.awsutils.EXPECT().GetAttachedENIs().Return([]awsutils.ENIMetadata{
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
	assert.Equal(t, len(curENIs.ENIs), 1)
	assert.Equal(t, curENIs.TotalIPs, 0)

	// remove eni
	m.awsutils.EXPECT().GetAttachedENIs().Return(nil, nil)

	mockContext.nodeIPPoolReconcile(0)
	curENIs = mockContext.dataStore.GetENIInfos()
	assert.Equal(t, len(curENIs.ENIs), 0)
	assert.Equal(t, curENIs.TotalIPs, 0)
}

func TestGetWarmENITarget(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

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
	m := setup(t)
	defer m.ctrl.Finish()

	mockContext := &IPAMContext{
		awsClient:     m.awsutils,
		networkClient: m.network,
		primaryIP:     make(map[string]string),
		terminating:   int32(0),
	}

	mockContext.dataStore = testDatastore()

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
	m := setup(t)
	defer m.ctrl.Finish()

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
		{"Test new ds, all defaults", fields{14, 1, 0, testDatastore()}, true},
		{"Test new ds, 0 ENIs", fields{14, 0, 0, testDatastore()}, true},
		{"Test new ds, 3 warm IPs", fields{14, 0, 3, testDatastore()}, true},
		{"Test 3 unused IPs, 1 warm", fields{3, 1, 1, datastoreWith3FreeIPs()}, false},
		{"Test 1 used, 1 warm ENI", fields{3, 1, 0, datastoreWith1Pod1()}, true},
		{"Test 1 used, 0 warm ENI", fields{3, 0, 0, datastoreWith1Pod1()}, false},
		{"Test 3 used, 1 warm ENI", fields{3, 1, 0, datastoreWith3Pods()}, true},
		{"Test 3 used, 0 warm ENI", fields{3, 0, 0, datastoreWith3Pods()}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &IPAMContext{
				awsClient:           m.awsutils,
				dataStore:           tt.fields.datastore,
				useCustomNetworking: false,
				eniConfig:           m.eniconfig,
				networkClient:       m.network,
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

func testDatastore() *datastore.DataStore {
	return datastore.NewDataStore(log, datastore.NewTestCheckpoint(datastore.CheckpointData{Version: datastore.CheckpointFormatVersion}))
}

func datastoreWith3FreeIPs() *datastore.DataStore {
	datastoreWith3FreeIPs := testDatastore()
	_ = datastoreWith3FreeIPs.AddENI(primaryENIid, 1, true)
	_ = datastoreWith3FreeIPs.AddIPv4AddressToStore(primaryENIid, ipaddr01)
	_ = datastoreWith3FreeIPs.AddIPv4AddressToStore(primaryENIid, ipaddr02)
	_ = datastoreWith3FreeIPs.AddIPv4AddressToStore(primaryENIid, ipaddr03)
	return datastoreWith3FreeIPs
}

func datastoreWith1Pod1() *datastore.DataStore {
	datastoreWith1Pod1 := datastoreWith3FreeIPs()

	_, _, _ = datastoreWith1Pod1.AssignPodIPv4Address(datastore.IPAMKey{
		NetworkName: "net0",
		ContainerID: "sandbox-1",
		IfName:      "eth0",
	})
	return datastoreWith1Pod1
}

func datastoreWith3Pods() *datastore.DataStore {
	datastoreWith3Pods := datastoreWith3FreeIPs()

	for i := 0; i < 3; i++ {
		key := datastore.IPAMKey{
			NetworkName: "net0",
			ContainerID: fmt.Sprintf("sandbox-%d", i),
			IfName:      "eth0",
		}
		_, _, _ = datastoreWith3Pods.AssignPodIPv4Address(key)
	}
	return datastoreWith3Pods
}

func TestIPAMContext_filterUnmanagedENIs(t *testing.T) {
	ctrl := gomock.NewController(t)

	eni1, eni2 := getDummyENIMetadata()
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
	m := setup(t)
	defer m.ctrl.Finish()

	_ = os.Setenv(envDisableENIProvisioning, "true")
	disabled := disablingENIProvisioning()
	assert.True(t, disabled)

	_ = os.Unsetenv(envDisableENIProvisioning)
	disabled = disablingENIProvisioning()
	assert.False(t, disabled)
}
