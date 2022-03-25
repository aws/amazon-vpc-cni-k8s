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
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"reflect"
	"sort"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/vishvananda/netlink"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	testclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/apis/crd/v1alpha1"
	eniconfigscheme "github.com/aws/amazon-vpc-cni-k8s/pkg/apis/crd/v1alpha1"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/awsutils"
	mock_awsutils "github.com/aws/amazon-vpc-cni-k8s/pkg/awsutils/mocks"
	mock_eniconfig "github.com/aws/amazon-vpc-cni-k8s/pkg/eniconfig/mocks"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/ipamd/datastore"
	mock_networkutils "github.com/aws/amazon-vpc-cni-k8s/pkg/networkutils/mocks"
)

const (
	primaryENIid  = "eni-00000000"
	secENIid      = "eni-00000001"
	terENIid      = "eni-00000002"
	primaryMAC    = "12:ef:2a:98:e5:5a"
	secMAC        = "12:ef:2a:98:e5:5b"
	terMAC        = "12:ef:2a:98:e5:5c"
	primaryDevice = 0
	secDevice     = 2
	terDevice     = 3
	primarySubnet = "10.10.10.0/24"
	secSubnet     = "10.10.20.0/24"
	terSubnet     = "10.10.30.0/24"
	ipaddr01      = "10.10.10.11"
	ipaddr02      = "10.10.10.12"
	ipaddr03      = "10.10.10.13"
	ipaddr11      = "10.10.20.11"
	ipaddr12      = "10.10.20.12"
	ipaddr21      = "10.10.30.11"
	ipaddr22      = "10.10.30.12"
	vpcCIDR       = "10.10.0.0/16"
	myNodeName    = "testNodeName"
	prefix01      = "10.10.30.0/28"
	prefix02      = "10.10.40.0/28"
	ipaddrPD01    = "10.10.30.0"
	ipaddrPD02    = "10.10.40.0"
	v6ipaddr01    = "2001:db8::1/128"
	v6prefix01    = "2001:db8::/64"
	instanceID    = "i-0e1f3b9eb950e4980"
)

type testMocks struct {
	ctrl            *gomock.Controller
	awsutils        *mock_awsutils.MockAPIs
	rawK8SClient    client.Client
	cachedK8SClient client.Client
	network         *mock_networkutils.MockNetworkAPIs
	eniconfig       *mock_eniconfig.MockENIConfig
}

func setup(t *testing.T) *testMocks {
	ctrl := gomock.NewController(t)
	k8sSchema := runtime.NewScheme()
	clientgoscheme.AddToScheme(k8sSchema)
	eniconfigscheme.AddToScheme(k8sSchema)

	return &testMocks{
		ctrl:            ctrl,
		awsutils:        mock_awsutils.NewMockAPIs(ctrl),
		rawK8SClient:    testclient.NewClientBuilder().WithScheme(k8sSchema).Build(),
		cachedK8SClient: testclient.NewClientBuilder().WithScheme(k8sSchema).Build(),
		network:         mock_networkutils.NewMockNetworkAPIs(ctrl),
		eniconfig:       mock_eniconfig.NewMockENIConfig(ctrl),
	}
}

func TestNodeInit(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()
	ctx := context.Background()

	fakeCheckpoint := datastore.CheckpointData{
		Version: datastore.CheckpointFormatVersion,
		Allocations: []datastore.CheckpointEntry{
			{IPAMKey: datastore.IPAMKey{NetworkName: "net0", ContainerID: "sandbox-id", IfName: "eth0"}, IPv4: ipaddr02},
		},
	}

	mockContext := &IPAMContext{
		awsClient:       m.awsutils,
		rawK8SClient:    m.rawK8SClient,
		cachedK8SClient: m.cachedK8SClient,
		maxIPsPerENI:    14,
		maxENI:          4,
		warmENITarget:   1,
		warmIPTarget:    3,
		primaryIP:       make(map[string]string),
		terminating:     int32(0),
		networkClient:   m.network,
		dataStore:       datastore.NewDataStore(log, datastore.NewTestCheckpoint(fakeCheckpoint), false),
		myNodeName:      myNodeName,
		enableIPv4:      true,
		enableIPv6:      false,
	}
	mockContext.dataStore.CheckpointMigrationPhase = 2

	eni1, eni2, _ := getDummyENIMetadata()

	var cidrs []string
	m.awsutils.EXPECT().GetENILimit().Return(4)
	m.awsutils.EXPECT().GetENIIPv4Limit().Return(14)
	m.awsutils.EXPECT().GetIPv4sFromEC2(eni1.ENIID).AnyTimes().Return(eni1.IPv4Addresses, nil)
	m.awsutils.EXPECT().GetIPv4sFromEC2(eni2.ENIID).AnyTimes().Return(eni2.IPv4Addresses, nil)
	m.awsutils.EXPECT().IsUnmanagedENI(eni1.ENIID).Return(false).AnyTimes()
	m.awsutils.EXPECT().IsUnmanagedENI(eni2.ENIID).Return(false).AnyTimes()
	m.awsutils.EXPECT().TagENI(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	m.awsutils.EXPECT().IsCNIUnmanagedENI(eni1.ENIID).Return(false).AnyTimes()
	m.awsutils.EXPECT().IsCNIUnmanagedENI(eni2.ENIID).Return(false).AnyTimes()

	primaryIP := net.ParseIP(ipaddr01)
	m.awsutils.EXPECT().GetVPCIPv4CIDRs().AnyTimes().Return(cidrs, nil)
	m.awsutils.EXPECT().GetPrimaryENImac().Return("")
	m.network.EXPECT().SetupHostNetwork(cidrs, "", &primaryIP, false, true, false).Return(nil)

	m.awsutils.EXPECT().GetPrimaryENI().AnyTimes().Return(primaryENIid)

	eniMetadataSlice := []awsutils.ENIMetadata{eni1, eni2}
	resp := awsutils.DescribeAllENIsResult{
		ENIMetadata:     eniMetadataSlice,
		TagMap:          map[string]awsutils.TagMap{},
		TrunkENI:        "",
		EFAENIs:         make(map[string]bool),
		MultiCardENIIDs: nil,
	}
	m.awsutils.EXPECT().DescribeAllENIs().Return(resp, nil)
	m.network.EXPECT().SetupENINetwork(gomock.Any(), secMAC, secDevice, secSubnet)

	m.awsutils.EXPECT().SetCNIUnmanagedENIs(resp.MultiCardENIIDs).AnyTimes()
	m.awsutils.EXPECT().GetLocalIPv4().Return(primaryIP)

	var rules []netlink.Rule
	m.network.EXPECT().GetRuleList().Return(rules, nil)

	m.network.EXPECT().UpdateRuleListBySrc(gomock.Any(), gomock.Any())

	fakeNode := v1.Node{
		TypeMeta:   metav1.TypeMeta{Kind: "Node"},
		ObjectMeta: metav1.ObjectMeta{Name: myNodeName},
		Spec:       v1.NodeSpec{},
		Status:     v1.NodeStatus{},
	}
	_ = m.cachedK8SClient.Create(ctx, &fakeNode)

	// Add IPs
	m.awsutils.EXPECT().AllocIPAddresses(gomock.Any(), gomock.Any())

	err := mockContext.nodeInit()
	assert.NoError(t, err)
}

func TestNodeInitwithPDenabledIPv4Mode(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()
	ctx := context.Background()

	fakeCheckpoint := datastore.CheckpointData{
		Version: datastore.CheckpointFormatVersion,
		Allocations: []datastore.CheckpointEntry{
			{IPAMKey: datastore.IPAMKey{NetworkName: "net0", ContainerID: "sandbox-id", IfName: "eth0"}, IPv4: ipaddrPD01},
		},
	}

	mockContext := &IPAMContext{
		awsClient:              m.awsutils,
		rawK8SClient:           m.rawK8SClient,
		cachedK8SClient:        m.cachedK8SClient,
		maxIPsPerENI:           224,
		maxPrefixesPerENI:      14,
		maxENI:                 4,
		warmENITarget:          1,
		warmIPTarget:           3,
		primaryIP:              make(map[string]string),
		terminating:            int32(0),
		networkClient:          m.network,
		dataStore:              datastore.NewDataStore(log, datastore.NewTestCheckpoint(fakeCheckpoint), true),
		myNodeName:             myNodeName,
		enablePrefixDelegation: true,
		enableIPv4:             true,
		enableIPv6:             false,
	}
	mockContext.dataStore.CheckpointMigrationPhase = 2

	eni1, eni2 := getDummyENIMetadataWithPrefix()

	var cidrs []string
	m.awsutils.EXPECT().GetENILimit().Return(4)
	m.awsutils.EXPECT().GetENIIPv4Limit().Return(14)
	m.awsutils.EXPECT().GetIPv4PrefixesFromEC2(eni1.ENIID).AnyTimes().Return(eni1.IPv4Prefixes, nil)
	m.awsutils.EXPECT().GetIPv4PrefixesFromEC2(eni2.ENIID).AnyTimes().Return(eni2.IPv4Prefixes, nil)
	m.awsutils.EXPECT().IsUnmanagedENI(eni1.ENIID).Return(false).AnyTimes()
	m.awsutils.EXPECT().IsUnmanagedENI(eni2.ENIID).Return(false).AnyTimes()
	m.awsutils.EXPECT().TagENI(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	m.awsutils.EXPECT().IsCNIUnmanagedENI(eni1.ENIID).Return(false).AnyTimes()
	m.awsutils.EXPECT().IsCNIUnmanagedENI(eni2.ENIID).Return(false).AnyTimes()

	primaryIP := net.ParseIP(ipaddr01)
	m.awsutils.EXPECT().GetVPCIPv4CIDRs().AnyTimes().Return(cidrs, nil)
	m.awsutils.EXPECT().GetPrimaryENImac().Return("")
	m.network.EXPECT().SetupHostNetwork(cidrs, "", &primaryIP, false, true, false).Return(nil)

	m.awsutils.EXPECT().GetPrimaryENI().AnyTimes().Return(primaryENIid)

	eniMetadataSlice := []awsutils.ENIMetadata{eni1, eni2}
	resp := awsutils.DescribeAllENIsResult{
		ENIMetadata: eniMetadataSlice,
		TagMap:      map[string]awsutils.TagMap{},
		TrunkENI:    "",
		EFAENIs:     make(map[string]bool),
	}
	m.awsutils.EXPECT().DescribeAllENIs().Return(resp, nil)
	m.network.EXPECT().SetupENINetwork(gomock.Any(), secMAC, secDevice, secSubnet)

	m.awsutils.EXPECT().GetLocalIPv4().Return(primaryIP)
	m.awsutils.EXPECT().SetCNIUnmanagedENIs(resp.MultiCardENIIDs).AnyTimes()

	var rules []netlink.Rule
	m.network.EXPECT().GetRuleList().Return(rules, nil)

	//m.network.EXPECT().UseExternalSNAT().Return(false)
	m.network.EXPECT().UpdateRuleListBySrc(gomock.Any(), gomock.Any())

	fakeNode := v1.Node{
		TypeMeta:   metav1.TypeMeta{Kind: "Node"},
		ObjectMeta: metav1.ObjectMeta{Name: myNodeName},
		Spec:       v1.NodeSpec{},
		Status:     v1.NodeStatus{},
	}
	_ = m.cachedK8SClient.Create(ctx, &fakeNode)

	err := mockContext.nodeInit()
	assert.NoError(t, err)
}

func TestNodeInitwithPDenabledIPv6Mode(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()
	ctx := context.Background()

	fakeCheckpoint := datastore.CheckpointData{
		Version: datastore.CheckpointFormatVersion,
		Allocations: []datastore.CheckpointEntry{
			{IPAMKey: datastore.IPAMKey{NetworkName: "net0", ContainerID: "sandbox-id", IfName: "eth0"}, IPv6: ipaddrPD01},
		},
	}

	mockContext := &IPAMContext{
		awsClient:              m.awsutils,
		rawK8SClient:           m.rawK8SClient,
		cachedK8SClient:        m.cachedK8SClient,
		maxIPsPerENI:           224,
		maxPrefixesPerENI:      1,
		maxENI:                 1,
		warmENITarget:          1,
		warmIPTarget:           1,
		primaryIP:              make(map[string]string),
		terminating:            int32(0),
		networkClient:          m.network,
		dataStore:              datastore.NewDataStore(log, datastore.NewTestCheckpoint(fakeCheckpoint), true),
		myNodeName:             myNodeName,
		enablePrefixDelegation: true,
		enableIPv4:             false,
		enableIPv6:             true,
	}
	mockContext.dataStore.CheckpointMigrationPhase = 2

	eni1 := getDummyENIMetadataWithV6Prefix()

	var cidrs []string
	m.awsutils.EXPECT().IsUnmanagedENI(eni1.ENIID).Return(false).AnyTimes()
	m.awsutils.EXPECT().TagENI(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	m.awsutils.EXPECT().IsCNIUnmanagedENI(eni1.ENIID).Return(false).AnyTimes()

	primaryIP := net.ParseIP(ipaddr01)
	m.network.EXPECT().SetupHostNetwork(cidrs, eni1.MAC, &primaryIP, false, false, true).Return(nil)
	m.awsutils.EXPECT().GetIPv6PrefixesFromEC2(eni1.ENIID).AnyTimes().Return(eni1.IPv6Prefixes, nil)
	m.awsutils.EXPECT().GetPrimaryENI().AnyTimes().Return(primaryENIid)
	m.awsutils.EXPECT().GetPrimaryENImac().Return(eni1.MAC)
	m.awsutils.EXPECT().IsPrimaryENI(primaryENIid).Return(true).AnyTimes()

	eniMetadataSlice := []awsutils.ENIMetadata{eni1}
	resp := awsutils.DescribeAllENIsResult{
		ENIMetadata: eniMetadataSlice,
		TagMap:      map[string]awsutils.TagMap{},
		TrunkENI:    "",
		EFAENIs:     make(map[string]bool),
	}
	m.awsutils.EXPECT().DescribeAllENIs().Return(resp, nil)
	m.awsutils.EXPECT().GetLocalIPv4().Return(primaryIP)
	m.awsutils.EXPECT().SetCNIUnmanagedENIs(resp.MultiCardENIIDs).AnyTimes()

	fakeNode := v1.Node{
		TypeMeta:   metav1.TypeMeta{Kind: "Node"},
		ObjectMeta: metav1.ObjectMeta{Name: myNodeName},
		Spec:       v1.NodeSpec{},
		Status:     v1.NodeStatus{},
	}
	_ = m.cachedK8SClient.Create(ctx, &fakeNode)

	err := mockContext.nodeInit()
	assert.NoError(t, err)
}

func getDummyENIMetadata() (awsutils.ENIMetadata, awsutils.ENIMetadata, awsutils.ENIMetadata) {
	primary := true
	notPrimary := false
	testAddr1 := ipaddr01
	testAddr2 := ipaddr02
	testAddr11 := ipaddr11
	testAddr12 := ipaddr12
	testAddr21 := ipaddr21
	testAddr22 := ipaddr22
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

	eni3 := awsutils.ENIMetadata{
		ENIID:          terENIid,
		MAC:            terMAC,
		DeviceNumber:   terDevice,
		SubnetIPv4CIDR: terSubnet,
		IPv4Addresses: []*ec2.NetworkInterfacePrivateIpAddress{
			{
				PrivateIpAddress: &testAddr21, Primary: &notPrimary,
			},
			{
				PrivateIpAddress: &testAddr22, Primary: &notPrimary,
			},
		},
	}
	return eni1, eni2, eni3
}

func getDummyENIMetadataWithPrefix() (awsutils.ENIMetadata, awsutils.ENIMetadata) {
	primary := true
	testAddr1 := ipaddr01
	testPrefix1 := prefix01
	testAddr2 := ipaddr11
	eni1 := awsutils.ENIMetadata{
		ENIID:          primaryENIid,
		MAC:            primaryMAC,
		DeviceNumber:   primaryDevice,
		SubnetIPv4CIDR: primarySubnet,
		IPv4Addresses: []*ec2.NetworkInterfacePrivateIpAddress{
			{
				PrivateIpAddress: &testAddr1, Primary: &primary,
			},
		},
		IPv4Prefixes: []*ec2.Ipv4PrefixSpecification{
			{
				Ipv4Prefix: &testPrefix1,
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
				PrivateIpAddress: &testAddr2, Primary: &primary,
			},
		},
	}
	return eni1, eni2
}

func getDummyENIMetadataWithV6Prefix() awsutils.ENIMetadata {
	primary := true
	testAddr1 := v6ipaddr01
	testv6Prefix := v6prefix01
	eni1 := awsutils.ENIMetadata{
		ENIID:          primaryENIid,
		MAC:            primaryMAC,
		DeviceNumber:   primaryDevice,
		SubnetIPv4CIDR: primarySubnet,
		IPv4Addresses: []*ec2.NetworkInterfacePrivateIpAddress{
			{
				PrivateIpAddress: &testAddr1, Primary: &primary,
			},
		},
		IPv6Prefixes: []*ec2.Ipv6PrefixSpecification{
			{
				Ipv6Prefix: &testv6Prefix,
			},
		},
	}

	return eni1
}

func TestIncreaseIPPoolDefault(t *testing.T) {
	_ = os.Unsetenv(envCustomNetworkCfg)
	testIncreaseIPPool(t, false)
}

func TestIncreaseIPPoolCustomENI(t *testing.T) {
	_ = os.Setenv(envCustomNetworkCfg, "true")
	_ = os.Setenv("MY_NODE_NAME", myNodeName)
	testIncreaseIPPool(t, true)
}

func testIncreaseIPPool(t *testing.T, useENIConfig bool) {
	m := setup(t)
	defer m.ctrl.Finish()
	ctx := context.Background()

	mockContext := &IPAMContext{
		awsClient:           m.awsutils,
		rawK8SClient:        m.rawK8SClient,
		cachedK8SClient:     m.cachedK8SClient,
		maxIPsPerENI:        14,
		maxENI:              4,
		warmENITarget:       1,
		networkClient:       m.network,
		useCustomNetworking: UseCustomNetworkCfg(),
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
		m.awsutils.EXPECT().AllocENI(true, sg, podENIConfig.Subnet).Return(eni2, nil)
	} else {
		m.awsutils.EXPECT().AllocENI(false, nil, "").Return(eni2, nil)
	}

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
	}

	m.awsutils.EXPECT().GetPrimaryENI().Return(primaryENIid)
	m.awsutils.EXPECT().WaitForENIAndIPsAttached(secENIid, 14).Return(eniMetadata[1], nil)
	m.network.EXPECT().SetupENINetwork(gomock.Any(), secMAC, secDevice, secSubnet)
	m.awsutils.EXPECT().AllocIPAddresses(eni2, 14)

	if mockContext.useCustomNetworking {
		mockContext.myNodeName = myNodeName

		labels := map[string]string{
			"k8s.amazonaws.com/eniConfig": "az1",
		}
		//Create a Fake Node
		fakeNode := v1.Node{
			TypeMeta:   metav1.TypeMeta{Kind: "Node"},
			ObjectMeta: metav1.ObjectMeta{Name: myNodeName, Labels: labels},
			Spec:       v1.NodeSpec{},
			Status:     v1.NodeStatus{},
		}
		_ = m.cachedK8SClient.Create(ctx, &fakeNode)

		//Create a dummy ENIConfig
		fakeENIConfig := v1alpha1.ENIConfig{
			TypeMeta:   metav1.TypeMeta{},
			ObjectMeta: metav1.ObjectMeta{Name: "az1"},
			Spec: eniconfigscheme.ENIConfigSpec{
				Subnet:         "subnet1",
				SecurityGroups: []string{"sg1-id", "sg2-id"},
			},
			Status: eniconfigscheme.ENIConfigStatus{},
		}
		_ = m.cachedK8SClient.Create(ctx, &fakeENIConfig)
	}

	mockContext.increaseDatastorePool(ctx)
}

func TestIncreasePrefixPoolDefault(t *testing.T) {
	_ = os.Unsetenv(envCustomNetworkCfg)
	testIncreasePrefixPool(t, false)
}

func TestIncreasePrefixPoolCustomENI(t *testing.T) {
	_ = os.Setenv(envCustomNetworkCfg, "true")
	testIncreasePrefixPool(t, true)
}

func testIncreasePrefixPool(t *testing.T, useENIConfig bool) {
	m := setup(t)
	defer m.ctrl.Finish()
	ctx := context.Background()

	mockContext := &IPAMContext{
		awsClient:              m.awsutils,
		rawK8SClient:           m.rawK8SClient,
		cachedK8SClient:        m.cachedK8SClient,
		maxIPsPerENI:           256,
		maxPrefixesPerENI:      16,
		maxENI:                 4,
		warmENITarget:          1,
		warmPrefixTarget:       1,
		networkClient:          m.network,
		useCustomNetworking:    UseCustomNetworkCfg(),
		primaryIP:              make(map[string]string),
		terminating:            int32(0),
		enablePrefixDelegation: true,
	}

	mockContext.dataStore = testDatastorewithPrefix()

	primary := true
	testAddr1 := ipaddr01
	testAddr11 := ipaddr11
	testPrefix1 := prefix01
	testPrefix2 := prefix02
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
		m.awsutils.EXPECT().AllocENI(true, sg, podENIConfig.Subnet).Return(eni2, nil)
	} else {
		m.awsutils.EXPECT().AllocENI(false, nil, "").Return(eni2, nil)
	}

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
			},
			IPv4Prefixes: []*ec2.Ipv4PrefixSpecification{
				{
					Ipv4Prefix: &testPrefix1,
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
					PrivateIpAddress: &testAddr11, Primary: &primary,
				},
			},
			IPv4Prefixes: []*ec2.Ipv4PrefixSpecification{
				{
					Ipv4Prefix: &testPrefix2,
				},
			},
		},
	}

	m.awsutils.EXPECT().GetPrimaryENI().Return(primaryENIid)
	m.awsutils.EXPECT().WaitForENIAndIPsAttached(secENIid, 1).Return(eniMetadata[1], nil)
	m.network.EXPECT().SetupENINetwork(gomock.Any(), secMAC, secDevice, secSubnet)
	m.awsutils.EXPECT().AllocIPAddresses(eni2, 1)

	if mockContext.useCustomNetworking {
		mockContext.myNodeName = myNodeName

		labels := map[string]string{
			"k8s.amazonaws.com/eniConfig": "az1",
		}
		//Create a Fake Node
		fakeNode := v1.Node{
			TypeMeta:   metav1.TypeMeta{Kind: "Node"},
			ObjectMeta: metav1.ObjectMeta{Name: myNodeName, Labels: labels},
			Spec:       v1.NodeSpec{},
			Status:     v1.NodeStatus{},
		}
		_ = m.cachedK8SClient.Create(ctx, &fakeNode)

		//Create a dummy ENIConfig
		fakeENIConfig := v1alpha1.ENIConfig{
			TypeMeta:   metav1.TypeMeta{},
			ObjectMeta: metav1.ObjectMeta{Name: "az1"},
			Spec: eniconfigscheme.ENIConfigSpec{
				Subnet:         "subnet1",
				SecurityGroups: []string{"sg1-id", "sg2-id"},
			},
			Status: eniconfigscheme.ENIConfigStatus{},
		}
		_ = m.cachedK8SClient.Create(ctx, &fakeENIConfig)
	}

	mockContext.increaseDatastorePool(ctx)
}

func TestTryAddIPToENI(t *testing.T) {
	_ = os.Unsetenv(envCustomNetworkCfg)
	m := setup(t)
	defer m.ctrl.Finish()
	ctx := context.Background()

	primary := true
	notPrimary := false
	testAddr1 := ipaddr01
	testAddr2 := ipaddr02
	testAddr11 := ipaddr11
	testAddr12 := ipaddr12

	warmIPTarget := 3
	mockContext := &IPAMContext{
		awsClient:     m.awsutils,
		maxIPsPerENI:  14,
		maxENI:        4,
		warmENITarget: 1,
		warmIPTarget:  warmIPTarget,
		networkClient: m.network,
		primaryIP:     make(map[string]string),
		terminating:   int32(0),
	}

	mockContext.dataStore = testDatastore()

	m.awsutils.EXPECT().AllocENI(false, nil, "").Return(secENIid, nil)
	m.awsutils.EXPECT().AllocIPAddresses(secENIid, warmIPTarget)
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
	}
	m.awsutils.EXPECT().WaitForENIAndIPsAttached(secENIid, 3).Return(eniMetadata[1], nil)
	m.awsutils.EXPECT().GetPrimaryENI().Return(primaryENIid)
	m.network.EXPECT().SetupENINetwork(gomock.Any(), secMAC, secDevice, secSubnet)

	mockContext.increaseDatastorePool(ctx)
}

func TestNodeIPPoolReconcile(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()
	ctx := context.Background()

	mockContext := &IPAMContext{
		awsClient:     m.awsutils,
		networkClient: m.network,
		primaryIP:     make(map[string]string),
		terminating:   int32(0),
	}

	mockContext.dataStore = testDatastore()

	primary := true
	primaryENIMetadata := getPrimaryENIMetadata()
	testAddr1 := *primaryENIMetadata.IPv4Addresses[0].PrivateIpAddress
	// Always the primary ENI
	m.awsutils.EXPECT().GetPrimaryENI().AnyTimes().Return(primaryENIid)
	m.awsutils.EXPECT().IsUnmanagedENI(primaryENIid).AnyTimes().Return(false)
	m.awsutils.EXPECT().IsCNIUnmanagedENI(primaryENIid).AnyTimes().Return(false)
	m.awsutils.EXPECT().TagENI(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	eniMetadataList := []awsutils.ENIMetadata{primaryENIMetadata}
	m.awsutils.EXPECT().GetAttachedENIs().Return(eniMetadataList, nil)
	resp := awsutils.DescribeAllENIsResult{
		ENIMetadata:     eniMetadataList,
		TagMap:          map[string]awsutils.TagMap{},
		TrunkENI:        "",
		EFAENIs:         make(map[string]bool),
		MultiCardENIIDs: nil,
	}
	m.awsutils.EXPECT().DescribeAllENIs().Return(resp, nil)

	m.awsutils.EXPECT().SetCNIUnmanagedENIs(resp.MultiCardENIIDs).AnyTimes()
	mockContext.nodeIPPoolReconcile(ctx, 0)

	curENIs := mockContext.dataStore.GetENIInfos()
	assert.Equal(t, 1, len(curENIs.ENIs))
	assert.Equal(t, 2, curENIs.TotalIPs)

	// 1 secondary IP lost in IMDS
	oneIPUnassigned := []awsutils.ENIMetadata{
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
	}
	m.awsutils.EXPECT().GetAttachedENIs().Return(oneIPUnassigned, nil)
	m.awsutils.EXPECT().GetIPv4sFromEC2(primaryENIid).Return(oneIPUnassigned[0].IPv4Addresses, nil)

	mockContext.nodeIPPoolReconcile(ctx, 0)
	curENIs = mockContext.dataStore.GetENIInfos()
	assert.Equal(t, 1, len(curENIs.ENIs))
	assert.Equal(t, 0, curENIs.TotalIPs)

	// New ENI attached
	newENIMetadata := getSecondaryENIMetadata()

	twoENIs := append(oneIPUnassigned, newENIMetadata)

	// Two ENIs found
	m.awsutils.EXPECT().GetAttachedENIs().Return(twoENIs, nil)
	m.awsutils.EXPECT().IsUnmanagedENI(secENIid).Times(2).Return(false)
	m.awsutils.EXPECT().IsCNIUnmanagedENI(secENIid).Times(2).Return(false)
	resp2 := awsutils.DescribeAllENIsResult{
		ENIMetadata:     twoENIs,
		TagMap:          map[string]awsutils.TagMap{},
		TrunkENI:        "",
		EFAENIs:         make(map[string]bool),
		MultiCardENIIDs: nil,
	}
	m.awsutils.EXPECT().DescribeAllENIs().Return(resp2, nil)
	m.network.EXPECT().SetupENINetwork(gomock.Any(), secMAC, secDevice, primarySubnet)
	m.awsutils.EXPECT().SetCNIUnmanagedENIs(resp2.MultiCardENIIDs).AnyTimes()

	mockContext.nodeIPPoolReconcile(ctx, 0)

	// Verify that we now have 2 ENIs, primary ENI with 0 secondary IPs, and secondary ENI with 1 secondary IP
	curENIs = mockContext.dataStore.GetENIInfos()
	assert.Equal(t, 2, len(curENIs.ENIs))
	assert.Equal(t, 1, curENIs.TotalIPs)

	// Remove the secondary ENI in the IMDS metadata
	m.awsutils.EXPECT().GetAttachedENIs().Return(oneIPUnassigned, nil)

	mockContext.nodeIPPoolReconcile(ctx, 0)
	curENIs = mockContext.dataStore.GetENIInfos()
	assert.Equal(t, 1, len(curENIs.ENIs))
	assert.Equal(t, 0, curENIs.TotalIPs)
}

func TestNodePrefixPoolReconcile(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()
	ctx := context.Background()

	mockContext := &IPAMContext{
		awsClient:              m.awsutils,
		networkClient:          m.network,
		primaryIP:              make(map[string]string),
		terminating:            int32(0),
		enablePrefixDelegation: true,
	}

	mockContext.dataStore = testDatastorewithPrefix()

	primary := true
	primaryENIMetadata := getPrimaryENIMetadataPDenabled()

	testAddr1 := *primaryENIMetadata.IPv4Addresses[0].PrivateIpAddress
	// Always the primary ENI
	m.awsutils.EXPECT().GetPrimaryENI().AnyTimes().Return(primaryENIid)
	m.awsutils.EXPECT().IsUnmanagedENI(primaryENIid).AnyTimes().Return(false)
	m.awsutils.EXPECT().IsCNIUnmanagedENI(primaryENIid).AnyTimes().Return(false)
	m.awsutils.EXPECT().TagENI(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	eniMetadataList := []awsutils.ENIMetadata{primaryENIMetadata}
	m.awsutils.EXPECT().GetAttachedENIs().Return(eniMetadataList, nil)
	resp := awsutils.DescribeAllENIsResult{
		ENIMetadata: eniMetadataList,
		TagMap:      map[string]awsutils.TagMap{},
		TrunkENI:    "",
		EFAENIs:     make(map[string]bool),
	}
	m.awsutils.EXPECT().DescribeAllENIs().Return(resp, nil)

	m.awsutils.EXPECT().SetCNIUnmanagedENIs(resp.MultiCardENIIDs).AnyTimes()
	mockContext.nodeIPPoolReconcile(ctx, 0)

	curENIs := mockContext.dataStore.GetENIInfos()
	assert.Equal(t, 1, len(curENIs.ENIs))
	assert.Equal(t, 16, curENIs.TotalIPs)

	// 1 prefix lost in IMDS
	oneIPUnassigned := []awsutils.ENIMetadata{
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
			//IPv4Prefixes: make([]*ec2.Ipv4PrefixSpecification, 0),
			IPv4Prefixes: nil,
		},
	}
	m.awsutils.EXPECT().GetAttachedENIs().Return(oneIPUnassigned, nil)
	m.awsutils.EXPECT().GetIPv4PrefixesFromEC2(primaryENIid).Return(oneIPUnassigned[0].IPv4Prefixes, nil)
	//m.awsutils.EXPECT().GetIPv4sFromEC2(primaryENIid).Return(oneIPUnassigned[0].IPv4Addresses, nil)

	mockContext.nodeIPPoolReconcile(ctx, 0)
	curENIs = mockContext.dataStore.GetENIInfos()
	assert.Equal(t, 1, len(curENIs.ENIs))
	assert.Equal(t, 0, curENIs.TotalIPs)

	// New ENI attached
	newENIMetadata := getSecondaryENIMetadataPDenabled()

	twoENIs := append(oneIPUnassigned, newENIMetadata)

	// Two ENIs found
	m.awsutils.EXPECT().GetAttachedENIs().Return(twoENIs, nil)
	m.awsutils.EXPECT().IsUnmanagedENI(secENIid).Times(2).Return(false)
	m.awsutils.EXPECT().IsCNIUnmanagedENI(secENIid).Times(2).Return(false)
	resp2 := awsutils.DescribeAllENIsResult{
		ENIMetadata: twoENIs,
		TagMap:      map[string]awsutils.TagMap{},
		TrunkENI:    "",
		EFAENIs:     make(map[string]bool),
	}
	m.awsutils.EXPECT().DescribeAllENIs().Return(resp2, nil)
	m.network.EXPECT().SetupENINetwork(gomock.Any(), secMAC, secDevice, primarySubnet)
	m.awsutils.EXPECT().SetCNIUnmanagedENIs(resp2.MultiCardENIIDs).AnyTimes()

	mockContext.nodeIPPoolReconcile(ctx, 0)

	// Verify that we now have 2 ENIs, primary ENI with 0 prefixes, and secondary ENI with 1 prefix
	curENIs = mockContext.dataStore.GetENIInfos()
	assert.Equal(t, 2, len(curENIs.ENIs))
	assert.Equal(t, 16, curENIs.TotalIPs)

	// Remove the secondary ENI in the IMDS metadata
	m.awsutils.EXPECT().GetAttachedENIs().Return(oneIPUnassigned, nil)

	mockContext.nodeIPPoolReconcile(ctx, 0)
	curENIs = mockContext.dataStore.GetENIInfos()
	assert.Equal(t, 1, len(curENIs.ENIs))
	assert.Equal(t, 0, curENIs.TotalIPs)
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

func TestGetWarmPrefixTarget(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	_ = os.Setenv("WARM_PREFIX_TARGET", "5")
	warmPrefixTarget := getWarmPrefixTarget()
	assert.Equal(t, warmPrefixTarget, 5)

	_ = os.Unsetenv("WARM_PREFIX_TARGET")
	warmPrefixTarget = getWarmPrefixTarget()
	assert.Equal(t, warmPrefixTarget, defaultWarmPrefixTarget)

	_ = os.Setenv("WARM_PREFIX_TARGET", "non-integer-string")
	warmPrefixTarget = getWarmPrefixTarget()
	assert.Equal(t, warmPrefixTarget, defaultWarmPrefixTarget)
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

	_, _, warmIPTargetDefined := mockContext.datastoreTargetState()
	assert.False(t, warmIPTargetDefined)

	mockContext.warmIPTarget = 5
	short, over, warmIPTargetDefined := mockContext.datastoreTargetState()
	assert.True(t, warmIPTargetDefined)
	assert.Equal(t, 5, short)
	assert.Equal(t, 0, over)

	// add 2 addresses to datastore
	_ = mockContext.dataStore.AddENI("eni-1", 1, true, false, false)
	ipv4Addr := net.IPNet{IP: net.ParseIP("1.1.1.1"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	_ = mockContext.dataStore.AddIPv4CidrToStore("eni-1", ipv4Addr, false)
	ipv4Addr = net.IPNet{IP: net.ParseIP("1.1.1.2"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	_ = mockContext.dataStore.AddIPv4CidrToStore("eni-1", ipv4Addr, false)

	short, over, warmIPTargetDefined = mockContext.datastoreTargetState()
	assert.True(t, warmIPTargetDefined)
	assert.Equal(t, 3, short)
	assert.Equal(t, 0, over)

	// add 3 more addresses to datastore
	ipv4Addr = net.IPNet{IP: net.ParseIP("1.1.1.3"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	_ = mockContext.dataStore.AddIPv4CidrToStore("eni-1", ipv4Addr, false)
	ipv4Addr = net.IPNet{IP: net.ParseIP("1.1.1.4"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	_ = mockContext.dataStore.AddIPv4CidrToStore("eni-1", ipv4Addr, false)
	ipv4Addr = net.IPNet{IP: net.ParseIP("1.1.1.5"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	_ = mockContext.dataStore.AddIPv4CidrToStore("eni-1", ipv4Addr, false)

	short, over, warmIPTargetDefined = mockContext.datastoreTargetState()
	assert.True(t, warmIPTargetDefined)
	assert.Equal(t, 0, short)
	assert.Equal(t, 0, over)
}

func TestGetWarmIPTargetStatewithPDenabled(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	mockContext := &IPAMContext{
		awsClient:              m.awsutils,
		networkClient:          m.network,
		primaryIP:              make(map[string]string),
		terminating:            int32(0),
		enablePrefixDelegation: true,
	}

	mockContext.dataStore = testDatastorewithPrefix()

	_, _, warmIPTargetDefined := mockContext.datastoreTargetState()
	assert.False(t, warmIPTargetDefined)

	mockContext.warmIPTarget = 5
	short, over, warmIPTargetDefined := mockContext.datastoreTargetState()
	assert.True(t, warmIPTargetDefined)
	assert.Equal(t, 1, short)
	assert.Equal(t, 0, over)

	// add 2 addresses to datastore
	_ = mockContext.dataStore.AddENI("eni-1", 1, true, false, false)
	_, ipnet, _ := net.ParseCIDR("10.1.1.0/28")
	_ = mockContext.dataStore.AddIPv4CidrToStore("eni-1", *ipnet, true)
	_ = mockContext.dataStore.AddENI("eni-2", 2, true, false, false)
	_, ipnet, _ = net.ParseCIDR("20.1.1.0/28")
	_ = mockContext.dataStore.AddIPv4CidrToStore("eni-1", *ipnet, true)

	short, over, warmIPTargetDefined = mockContext.datastoreTargetState()
	assert.True(t, warmIPTargetDefined)
	assert.Equal(t, 0, short)
	assert.Equal(t, 1, over)

	// Del 1 address
	_, ipnet, _ = net.ParseCIDR("20.1.1.0/28")
	_ = mockContext.dataStore.DelIPv4CidrFromStore("eni-1", *ipnet, true)

	short, over, warmIPTargetDefined = mockContext.datastoreTargetState()
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
				awsClient:              m.awsutils,
				dataStore:              tt.fields.datastore,
				useCustomNetworking:    false,
				networkClient:          m.network,
				maxIPsPerENI:           tt.fields.maxIPsPerENI,
				maxENI:                 -1,
				warmENITarget:          tt.fields.warmENITarget,
				warmIPTarget:           tt.fields.warmIPTarget,
				enablePrefixDelegation: false,
			}
			if got := c.isDatastorePoolTooLow(); got != tt.want {
				t.Errorf("nodeIPPoolTooLow() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIPAMContext_nodePrefixPoolTooLow(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	type fields struct {
		maxIPsPerENI      int
		maxPrefixesPerENI int
		warmPrefixTarget  int
		datastore         *datastore.DataStore
	}

	tests := []struct {
		name   string
		fields fields
		want   bool
	}{
		{"Test new ds, all defaults", fields{256, 16, 1, testDatastore()}, true},
		{"Test new ds, 0 ENIs", fields{256, 16, 0, testDatastore()}, true},
		{"Test 3 unused IPs, 1 warm", fields{256, 16, 1, datastoreWithFreeIPsFromPrefix()}, false},
		{"Test 1 used, 1 warm Prefix", fields{256, 16, 1, datastoreWith1Pod1FromPrefix()}, true},
		{"Test 1 used, 0 warm Prefix", fields{256, 16, 0, datastoreWith1Pod1FromPrefix()}, false},
		{"Test 3 used, 1 warm Prefix", fields{256, 16, 1, datastoreWith3PodsFromPrefix()}, true},
		{"Test 3 used, 0 warm Prefix", fields{256, 16, 0, datastoreWith3PodsFromPrefix()}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &IPAMContext{
				awsClient:              m.awsutils,
				dataStore:              tt.fields.datastore,
				useCustomNetworking:    false,
				networkClient:          m.network,
				maxPrefixesPerENI:      tt.fields.maxPrefixesPerENI,
				maxIPsPerENI:           tt.fields.maxIPsPerENI,
				maxENI:                 -1,
				warmPrefixTarget:       tt.fields.warmPrefixTarget,
				enablePrefixDelegation: true,
			}
			if got := c.isDatastorePoolTooLow(); got != tt.want {
				t.Errorf("nodeIPPoolTooLow() = %v, want %v", got, tt.want)
			}
		})
	}
}

func testDatastore() *datastore.DataStore {
	ds := datastore.NewDataStore(log, datastore.NewTestCheckpoint(datastore.CheckpointData{Version: datastore.CheckpointFormatVersion}), false)
	ds.CheckpointMigrationPhase = 2
	return ds
}

func testDatastorewithPrefix() *datastore.DataStore {
	ds := datastore.NewDataStore(log, datastore.NewTestCheckpoint(datastore.CheckpointData{Version: datastore.CheckpointFormatVersion}), true)
	ds.CheckpointMigrationPhase = 2
	return ds
}

func datastoreWith3FreeIPs() *datastore.DataStore {
	datastoreWith3FreeIPs := testDatastore()
	_ = datastoreWith3FreeIPs.AddENI(primaryENIid, 1, true, false, false)
	ipv4Addr := net.IPNet{IP: net.ParseIP(ipaddr01), Mask: net.IPv4Mask(255, 255, 255, 255)}
	_ = datastoreWith3FreeIPs.AddIPv4CidrToStore(primaryENIid, ipv4Addr, false)
	ipv4Addr = net.IPNet{IP: net.ParseIP(ipaddr02), Mask: net.IPv4Mask(255, 255, 255, 255)}
	_ = datastoreWith3FreeIPs.AddIPv4CidrToStore(primaryENIid, ipv4Addr, false)
	ipv4Addr = net.IPNet{IP: net.ParseIP(ipaddr03), Mask: net.IPv4Mask(255, 255, 255, 255)}
	_ = datastoreWith3FreeIPs.AddIPv4CidrToStore(primaryENIid, ipv4Addr, false)
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

func datastoreWithFreeIPsFromPrefix() *datastore.DataStore {
	datastoreWithFreeIPs := testDatastorewithPrefix()
	_ = datastoreWithFreeIPs.AddENI(primaryENIid, 1, true, false, false)
	_, ipnet, _ := net.ParseCIDR(prefix01)
	_ = datastoreWithFreeIPs.AddIPv4CidrToStore(primaryENIid, *ipnet, true)
	return datastoreWithFreeIPs
}

func datastoreWith1Pod1FromPrefix() *datastore.DataStore {
	datastoreWith1Pod1 := datastoreWithFreeIPsFromPrefix()

	_, _, _ = datastoreWith1Pod1.AssignPodIPv4Address(datastore.IPAMKey{
		NetworkName: "net0",
		ContainerID: "sandbox-1",
		IfName:      "eth0",
	})
	return datastoreWith1Pod1
}

func datastoreWith3PodsFromPrefix() *datastore.DataStore {
	datastoreWith3Pods := datastoreWithFreeIPsFromPrefix()

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

	eni1, eni2, eni3 := getDummyENIMetadata()
	allENIs := []awsutils.ENIMetadata{eni1, eni2, eni3}
	primaryENIonly := []awsutils.ENIMetadata{eni1}
	filteredENIonly := []awsutils.ENIMetadata{eni1, eni3}
	Test1TagMap := map[string]awsutils.TagMap{eni1.ENIID: {"hi": "tag", eniNoManageTagKey: "true"}}
	Test2TagMap := map[string]awsutils.TagMap{
		eni2.ENIID: {"hi": "tag", eniNoManageTagKey: "true"},
		eni3.ENIID: {"hi": "tag", eniNoManageTagKey: "true"}}
	Test3TagMap := map[string]awsutils.TagMap{
		eni2.ENIID: {"hi": "tag", eniNoManageTagKey: "true"},
		eni3.ENIID: {"hi": "tag", eniNoManageTagKey: "false"}}
	Test4TagMap := map[string]awsutils.TagMap{
		eni2.ENIID: {"hi": "tag", eniNoManageTagKey: "true"},
		eni3.ENIID: {"hi": "tag", eniNodeTagKey: instanceID}}
	Test5TagMap := map[string]awsutils.TagMap{
		eni2.ENIID: {"hi": "tag", eniNodeTagKey: "i-abcdabcdabcd"},
		eni3.ENIID: {"hi": "tag", eniNodeTagKey: instanceID}}

	mockAWSUtils := mock_awsutils.NewMockAPIs(ctrl)
	mockAWSUtils.EXPECT().GetPrimaryENI().Times(6).Return(eni1.ENIID)
	mockAWSUtils.EXPECT().GetInstanceID().Times(3).Return(instanceID)

	tests := []struct {
		name          string
		tagMap        map[string]awsutils.TagMap
		enis          []awsutils.ENIMetadata
		want          []awsutils.ENIMetadata
		unmanagedenis []string
	}{
		{"No tags at all", nil, allENIs, allENIs, nil},
		{"Primary ENI unmanaged", Test1TagMap, allENIs, allENIs, nil},
		{"Secondary/Tertiary ENI unmanaged", Test2TagMap, allENIs, primaryENIonly, []string{eni2.ENIID, eni3.ENIID}},
		{"Secondary ENI unmanaged", Test3TagMap, allENIs, filteredENIonly, []string{eni2.ENIID}},
		{"Secondary ENI unmanaged and Tertiary ENI CNI created", Test4TagMap, allENIs, filteredENIonly, []string{eni2.ENIID}},
		{"Secondary ENI not CNI created and Tertiary ENI CNI created", Test5TagMap, allENIs, filteredENIonly, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &IPAMContext{
				awsClient:                mockAWSUtils,
				enableManageUntaggedMode: true}

			mockAWSUtils.EXPECT().SetUnmanagedENIs(gomock.Any()).
				Do(func(args []string) {
					sort.Strings(tt.unmanagedenis)
					sort.Strings(args)
					assert.Equal(t, tt.unmanagedenis, args)
				})
			c.setUnmanagedENIs(tt.tagMap)

			mockAWSUtils.EXPECT().IsUnmanagedENI(gomock.Any()).DoAndReturn(
				func(eni string) (unmanaged bool) {
					if eni != eni1.ENIID {
						tags := tt.tagMap[eni]
						if _, ok := tags[eniNoManageTagKey]; ok {
							if tags[eniNoManageTagKey] == "true" {
								return true
							}
						} else if _, ok := tags[eniNodeTagKey]; ok && tags[eniNodeTagKey] != instanceID {
							return true
						}
					}
					return false

				}).AnyTimes()

			mockAWSUtils.EXPECT().IsCNIUnmanagedENI(gomock.Any()).DoAndReturn(
				func(eni string) (unmanaged bool) {
					return false

				}).AnyTimes()

			if got := c.filterUnmanagedENIs(tt.enis); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("filterUnmanagedENIs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIPAMContext_filterUnmanagedENIs_disableManageUntaggedMode(t *testing.T) {
	ctrl := gomock.NewController(t)

	eni1, eni2, eni3 := getDummyENIMetadata()
	allENIs := []awsutils.ENIMetadata{eni1, eni2, eni3}
	primaryENIonly := []awsutils.ENIMetadata{eni1}
	filteredENIonly := []awsutils.ENIMetadata{eni1, eni3}
	Test1TagMap := map[string]awsutils.TagMap{eni1.ENIID: {"hi": "tag", eniNoManageTagKey: "true"}}
	Test2TagMap := map[string]awsutils.TagMap{
		eni2.ENIID: {"hi": "tag", eniNoManageTagKey: "true"},
		eni3.ENIID: {"hi": "tag", eniNoManageTagKey: "true"}}
	Test3TagMap := map[string]awsutils.TagMap{
		eni2.ENIID: {"hi": "tag", eniNoManageTagKey: "true"},
		eni3.ENIID: {"hi": "tag", eniNoManageTagKey: "false"}}
	Test4TagMap := map[string]awsutils.TagMap{
		eni2.ENIID: {"hi": "tag", eniNoManageTagKey: "true"},
		eni3.ENIID: {"hi": "tag", eniNodeTagKey: instanceID}}
	Test5TagMap := map[string]awsutils.TagMap{
		eni2.ENIID: {"hi": "tag", eniNodeTagKey: "i-abcdabcdabcd"},
		eni3.ENIID: {"hi": "tag", eniNodeTagKey: instanceID}}

	mockAWSUtils := mock_awsutils.NewMockAPIs(ctrl)
	mockAWSUtils.EXPECT().GetPrimaryENI().Times(6).Return(eni1.ENIID)
	mockAWSUtils.EXPECT().GetInstanceID().Times(3).Return(instanceID)

	tests := []struct {
		name          string
		tagMap        map[string]awsutils.TagMap
		enis          []awsutils.ENIMetadata
		want          []awsutils.ENIMetadata
		unmanagedenis []string
	}{
		{"No tags at all", nil, allENIs, allENIs, []string{eni2.ENIID, eni3.ENIID}},
		{"Primary ENI unmanaged", Test1TagMap, allENIs, allENIs, nil},
		{"Secondary/Tertiary ENI unmanaged", Test2TagMap, allENIs, primaryENIonly, []string{eni2.ENIID, eni3.ENIID}},
		{"Secondary ENI unmanaged", Test3TagMap, allENIs, filteredENIonly, []string{eni2.ENIID}},
		{"Secondary ENI unmanaged and Tertiary ENI CNI created", Test4TagMap, allENIs, filteredENIonly, []string{eni2.ENIID}},
		{"Secondary ENI not CNI created and Tertiary ENI CNI created", Test5TagMap, allENIs, filteredENIonly, []string{eni2.ENIID}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &IPAMContext{
				awsClient:                mockAWSUtils,
				enableManageUntaggedMode: false}

			mockAWSUtils.
				EXPECT().
				SetUnmanagedENIs(gomock.Any()).
				Do(func(args []string) {
					sort.Strings(tt.unmanagedenis)
					sort.Strings(args)
					assert.Equal(t, tt.unmanagedenis, args)
				})

			c.setUnmanagedENIs(tt.tagMap)

			mockAWSUtils.EXPECT().IsUnmanagedENI(gomock.Any()).DoAndReturn(
				func(eni string) (unmanaged bool) {
					if eni != eni1.ENIID {
						tags := tt.tagMap[eni]
						if _, ok := tags[eniNoManageTagKey]; ok {
							if tags[eniNoManageTagKey] == "true" {
								return true
							}
						} else if _, ok := tags[eniNodeTagKey]; ok && tags[eniNodeTagKey] != instanceID {
							return true
						}
					}
					return false

				}).AnyTimes()

			mockAWSUtils.EXPECT().IsCNIUnmanagedENI(gomock.Any()).DoAndReturn(
				func(eni string) (unmanaged bool) {
					return false

				}).AnyTimes()

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

func TestPodENIConfigFlag(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	_ = os.Setenv(envEnablePodENI, "true")
	disabled := enablePodENI()
	assert.True(t, disabled)

	_ = os.Unsetenv(envEnablePodENI)
	disabled = enablePodENI()
	assert.False(t, disabled)
}

func TestNodeIPPoolReconcileBadIMDSData(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()
	ctx := context.Background()

	mockContext := &IPAMContext{
		awsClient:     m.awsutils,
		networkClient: m.network,
		primaryIP:     make(map[string]string),
		terminating:   int32(0),
	}

	mockContext.dataStore = testDatastore()

	primaryENIMetadata := getPrimaryENIMetadata()
	testAddr1 := *primaryENIMetadata.IPv4Addresses[0].PrivateIpAddress
	// Add ENI and IPs to datastore
	eniID := primaryENIMetadata.ENIID
	_ = mockContext.dataStore.AddENI(eniID, primaryENIMetadata.DeviceNumber, true, false, false)
	mockContext.primaryIP[eniID] = testAddr1
	mockContext.addENIsecondaryIPsToDataStore(primaryENIMetadata.IPv4Addresses, eniID)
	curENIs := mockContext.dataStore.GetENIInfos()
	assert.Equal(t, 1, len(curENIs.ENIs))
	assert.Equal(t, 2, curENIs.TotalIPs)
	eniMetadataList := []awsutils.ENIMetadata{primaryENIMetadata}
	m.awsutils.EXPECT().GetAttachedENIs().Return(eniMetadataList, nil)
	m.awsutils.EXPECT().IsUnmanagedENI(eniID).Return(false).AnyTimes()
	m.awsutils.EXPECT().IsCNIUnmanagedENI(eniID).Return(false).AnyTimes()

	// First reconcile, IMDS returns correct IPs so no change needed
	mockContext.nodeIPPoolReconcile(ctx, 0)

	// IMDS returns no secondary IPs, the EC2 call fails
	primary := true
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

	// eniIPPoolReconcile() calls EC2 to get the actual count, but that call fails
	m.awsutils.EXPECT().GetIPv4sFromEC2(primaryENIid).Return(nil, errors.New("ec2 API call failed"))
	mockContext.nodeIPPoolReconcile(ctx, 0)
	curENIs = mockContext.dataStore.GetENIInfos()
	assert.Equal(t, 1, len(curENIs.ENIs))
	assert.Equal(t, 2, curENIs.TotalIPs)

	// IMDS returns no secondary IPs
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

	// eniIPPoolReconcile() calls EC2 to get the actual count that should still be 2
	m.awsutils.EXPECT().GetIPv4sFromEC2(primaryENIid).Return(primaryENIMetadata.IPv4Addresses, nil)
	mockContext.nodeIPPoolReconcile(ctx, 0)
	curENIs = mockContext.dataStore.GetENIInfos()
	assert.Equal(t, 1, len(curENIs.ENIs))
	assert.Equal(t, 2, curENIs.TotalIPs)

	// If no ENI is found, we abort the reconcile
	m.awsutils.EXPECT().GetAttachedENIs().Return(nil, nil)
	mockContext.nodeIPPoolReconcile(ctx, 0)
	curENIs = mockContext.dataStore.GetENIInfos()
	assert.Equal(t, 1, len(curENIs.ENIs))
	assert.Equal(t, 2, curENIs.TotalIPs)
}

func TestNodePrefixPoolReconcileBadIMDSData(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()
	ctx := context.Background()

	mockContext := &IPAMContext{
		awsClient:              m.awsutils,
		networkClient:          m.network,
		primaryIP:              make(map[string]string),
		terminating:            int32(0),
		enablePrefixDelegation: true,
	}

	mockContext.dataStore = testDatastorewithPrefix()

	primaryENIMetadata := getPrimaryENIMetadataPDenabled()
	testAddr1 := *primaryENIMetadata.IPv4Addresses[0].PrivateIpAddress
	// Add ENI and IPs to datastore
	eniID := primaryENIMetadata.ENIID
	_ = mockContext.dataStore.AddENI(eniID, primaryENIMetadata.DeviceNumber, true, false, false)
	mockContext.primaryIP[eniID] = testAddr1
	mockContext.addENIv4prefixesToDataStore(primaryENIMetadata.IPv4Prefixes, eniID)
	curENIs := mockContext.dataStore.GetENIInfos()
	assert.Equal(t, 1, len(curENIs.ENIs))
	assert.Equal(t, 16, curENIs.TotalIPs)
	eniMetadataList := []awsutils.ENIMetadata{primaryENIMetadata}
	m.awsutils.EXPECT().GetAttachedENIs().Return(eniMetadataList, nil)
	m.awsutils.EXPECT().IsUnmanagedENI(eniID).Return(false).AnyTimes()
	m.awsutils.EXPECT().IsCNIUnmanagedENI(eniID).Return(false).AnyTimes()

	// First reconcile, IMDS returns correct IPs so no change needed
	mockContext.nodeIPPoolReconcile(ctx, 0)

	// IMDS returns no prefixes, the EC2 call fails
	primary := true
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

	// eniIPPoolReconcile() calls EC2 to get the actual count, but that call fails
	m.awsutils.EXPECT().GetIPv4PrefixesFromEC2(primaryENIid).Return(nil, errors.New("ec2 API call failed"))
	mockContext.nodeIPPoolReconcile(ctx, 0)
	curENIs = mockContext.dataStore.GetENIInfos()
	assert.Equal(t, 1, len(curENIs.ENIs))
	assert.Equal(t, 16, curENIs.TotalIPs)

	// IMDS returns no prefixes
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

	// eniIPPoolReconcile() calls EC2 to get the actual count that should still be 16
	m.awsutils.EXPECT().GetIPv4PrefixesFromEC2(primaryENIid).Return(primaryENIMetadata.IPv4Prefixes, nil)
	mockContext.nodeIPPoolReconcile(ctx, 0)
	curENIs = mockContext.dataStore.GetENIInfos()
	assert.Equal(t, 1, len(curENIs.ENIs))
	assert.Equal(t, 16, curENIs.TotalIPs)

	// If no ENI is found, we abort the reconcile
	m.awsutils.EXPECT().GetAttachedENIs().Return(nil, nil)
	mockContext.nodeIPPoolReconcile(ctx, 0)
	curENIs = mockContext.dataStore.GetENIInfos()
	assert.Equal(t, 1, len(curENIs.ENIs))
	assert.Equal(t, 16, curENIs.TotalIPs)
}
func getPrimaryENIMetadata() awsutils.ENIMetadata {
	primary := true
	notPrimary := false
	testAddr1 := ipaddr01
	testAddr2 := ipaddr02
	testAddr3 := ipaddr03

	eniMetadata := awsutils.ENIMetadata{
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
			{
				PrivateIpAddress: &testAddr3, Primary: &notPrimary,
			},
		},
	}
	return eniMetadata
}

func getSecondaryENIMetadata() awsutils.ENIMetadata {
	primary := true
	notPrimary := false
	testAddr3 := ipaddr11
	testAddr4 := ipaddr12
	newENIMetadata := awsutils.ENIMetadata{
		ENIID:          secENIid,
		MAC:            secMAC,
		DeviceNumber:   secDevice,
		SubnetIPv4CIDR: primarySubnet,
		IPv4Addresses: []*ec2.NetworkInterfacePrivateIpAddress{
			{
				PrivateIpAddress: &testAddr3, Primary: &primary,
			},
			{
				PrivateIpAddress: &testAddr4, Primary: &notPrimary,
			},
		},
	}
	return newENIMetadata
}

func getPrimaryENIMetadataPDenabled() awsutils.ENIMetadata {
	primary := true
	testAddr1 := ipaddr01
	testPrefix1 := prefix01

	eniMetadata := awsutils.ENIMetadata{
		ENIID:          primaryENIid,
		MAC:            primaryMAC,
		DeviceNumber:   primaryDevice,
		SubnetIPv4CIDR: primarySubnet,
		IPv4Addresses: []*ec2.NetworkInterfacePrivateIpAddress{
			{
				PrivateIpAddress: &testAddr1, Primary: &primary,
			},
		},
		IPv4Prefixes: []*ec2.Ipv4PrefixSpecification{
			{
				Ipv4Prefix: &testPrefix1,
			},
		},
	}
	return eniMetadata
}

func getSecondaryENIMetadataPDenabled() awsutils.ENIMetadata {
	primary := true
	testAddr3 := ipaddr11
	testPrefix2 := prefix02

	newENIMetadata := awsutils.ENIMetadata{
		ENIID:          secENIid,
		MAC:            secMAC,
		DeviceNumber:   secDevice,
		SubnetIPv4CIDR: primarySubnet,
		IPv4Addresses: []*ec2.NetworkInterfacePrivateIpAddress{
			{
				PrivateIpAddress: &testAddr3, Primary: &primary,
			},
		},
		IPv4Prefixes: []*ec2.Ipv4PrefixSpecification{
			{
				Ipv4Prefix: &testPrefix2,
			},
		},
	}
	return newENIMetadata
}

func TestIPAMContext_setupENI(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	mockContext := &IPAMContext{
		awsClient:     m.awsutils,
		networkClient: m.network,
		primaryIP:     make(map[string]string),
		terminating:   int32(0),
	}
	//mockContext.primaryIP[]

	mockContext.dataStore = testDatastore()
	primary := true
	notPrimary := false
	testAddr1 := ipaddr01
	testAddr2 := ipaddr02
	primaryENIMetadata := awsutils.ENIMetadata{
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
	m.awsutils.EXPECT().GetPrimaryENI().Return(primaryENIid)
	err := mockContext.setupENI(primaryENIMetadata.ENIID, primaryENIMetadata, false, false)
	assert.NoError(t, err)
	// Primary ENI added
	assert.Equal(t, 1, len(mockContext.primaryIP))

	newENIMetadata := getSecondaryENIMetadata()
	m.awsutils.EXPECT().GetPrimaryENI().Return(primaryENIid)
	m.network.EXPECT().SetupENINetwork(gomock.Any(), secMAC, secDevice, primarySubnet).Return(errors.New("not able to set route 0.0.0.0/0 via 10.10.10.1 table 2"))

	err = mockContext.setupENI(newENIMetadata.ENIID, newENIMetadata, false, false)
	assert.Error(t, err)
	assert.Equal(t, 1, len(mockContext.primaryIP))
}

func TestIPAMContext_setupENIwithPDenabled(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	mockContext := &IPAMContext{
		awsClient:     m.awsutils,
		networkClient: m.network,
		primaryIP:     make(map[string]string),
		terminating:   int32(0),
	}
	//mockContext.primaryIP[]

	mockContext.dataStore = testDatastorewithPrefix()
	primary := true
	notPrimary := false
	testAddr1 := ipaddr01
	testAddr2 := ipaddr02
	primaryENIMetadata := awsutils.ENIMetadata{
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
	m.awsutils.EXPECT().GetPrimaryENI().Return(primaryENIid)
	err := mockContext.setupENI(primaryENIMetadata.ENIID, primaryENIMetadata, false, false)
	assert.NoError(t, err)
	// Primary ENI added
	assert.Equal(t, 1, len(mockContext.primaryIP))

	newENIMetadata := getSecondaryENIMetadata()
	m.awsutils.EXPECT().GetPrimaryENI().Return(primaryENIid)
	m.network.EXPECT().SetupENINetwork(gomock.Any(), secMAC, secDevice, primarySubnet).Return(errors.New("not able to set route 0.0.0.0/0 via 10.10.10.1 table 2"))

	err = mockContext.setupENI(newENIMetadata.ENIID, newENIMetadata, false, false)
	assert.Error(t, err)
	assert.Equal(t, 1, len(mockContext.primaryIP))
}

func TestIPAMContext_askForTrunkENIIfNeeded(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()
	ctx := context.Background()

	mockContext := &IPAMContext{
		rawK8SClient:    m.rawK8SClient,
		cachedK8SClient: m.cachedK8SClient,
		dataStore:       datastore.NewDataStore(log, datastore.NewTestCheckpoint(datastore.CheckpointData{Version: datastore.CheckpointFormatVersion}), false),
		awsClient:       m.awsutils,
		networkClient:   m.network,
		primaryIP:       make(map[string]string),
		terminating:     int32(0),
		maxENI:          1,
		myNodeName:      myNodeName,
	}

	labels := map[string]string{
		"testKey": "testValue",
	}
	fakeNode := v1.Node{
		TypeMeta:   metav1.TypeMeta{Kind: "Node"},
		ObjectMeta: metav1.ObjectMeta{Name: myNodeName, Labels: labels},
		Spec:       v1.NodeSpec{},
		Status:     v1.NodeStatus{},
	}
	_ = m.cachedK8SClient.Create(ctx, &fakeNode)

	_ = mockContext.dataStore.AddENI("eni-1", 1, true, false, false)
	// If ENABLE_POD_ENI is not set, nothing happens
	mockContext.askForTrunkENIIfNeeded(ctx)

	mockContext.enablePodENI = true
	// Enabled, we should try to set the label if there is room
	mockContext.askForTrunkENIIfNeeded(ctx)
	var notUpdatedNode corev1.Node
	var updatedNode corev1.Node
	NodeKey := types.NamespacedName{
		Namespace: "",
		Name:      myNodeName,
	}
	err := m.cachedK8SClient.Get(ctx, NodeKey, &notUpdatedNode)
	// Since there was no room, no label should be added
	assert.NoError(t, err)
	assert.Equal(t, 1, len(notUpdatedNode.Labels))

	mockContext.maxENI = 4
	// Now there is room!
	mockContext.askForTrunkENIIfNeeded(ctx)

	// Fetch the updated node and verify that the label is set
	//updatedNode, err := m.clientset.CoreV1().Nodes().Get(myNodeName, metav1.GetOptions{})
	err = m.cachedK8SClient.Get(ctx, NodeKey, &updatedNode)
	assert.NoError(t, err)
	assert.Equal(t, "false", updatedNode.Labels["vpc.amazonaws.com/has-trunk-attached"])
}

func TestIsConfigValid(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	type fields struct {
		ipV4Enabled             bool
		ipV6Enabled             bool
		prefixDelegationEnabled bool
		customNetworkingEnabled bool
		podENIEnabled           bool
		isNitroInstance         bool
	}

	tests := []struct {
		name   string
		fields fields
		want   bool
	}{
		{
			name: "v4 enabled in non-PD mode and v6 disabled",
			fields: fields{
				ipV4Enabled:             true,
				ipV6Enabled:             false,
				prefixDelegationEnabled: false,
				isNitroInstance:         true,
			},
			want: true,
		},
		{
			name: "v4 enabled in PD mode and v6 disabled",
			fields: fields{
				ipV4Enabled:             true,
				ipV6Enabled:             false,
				prefixDelegationEnabled: true,
				isNitroInstance:         true,
			},
			want: true,
		},
		{
			name: "v4 disabled and v6 enabled in PD mode",
			fields: fields{
				ipV4Enabled:             false,
				ipV6Enabled:             true,
				prefixDelegationEnabled: true,
				isNitroInstance:         true,
			},
			want: true,
		},
		{
			name: "v4 disabled and v6 enabled in non-PD mode",
			fields: fields{
				ipV4Enabled:             false,
				ipV6Enabled:             true,
				prefixDelegationEnabled: false,
				isNitroInstance:         true,
			},
			want: false,
		},
		{
			name: "both v4 and v6 enabled",
			fields: fields{
				ipV4Enabled:     true,
				ipV6Enabled:     true,
				isNitroInstance: true,
			},
			want: false,
		},
		{
			name: "v4 disabled and v6 enabled in PD mode on Non-Nitro instance",
			fields: fields{
				ipV4Enabled:             false,
				ipV6Enabled:             true,
				prefixDelegationEnabled: true,
				isNitroInstance:         false,
			},
			want: false,
		},
		{
			name: "ppsg enabled in v6 mode",
			fields: fields{
				ipV4Enabled:             false,
				ipV6Enabled:             true,
				prefixDelegationEnabled: true,
				podENIEnabled:           true,
				isNitroInstance:         true,
			},
			want: false,
		},
		{
			name: "ppsg enabled in v4 mode",
			fields: fields{
				ipV4Enabled:             true,
				ipV6Enabled:             false,
				prefixDelegationEnabled: true,
				podENIEnabled:           true,
				isNitroInstance:         true,
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := setup(t)
			defer m.ctrl.Finish()

			if tt.fields.prefixDelegationEnabled && !(tt.fields.podENIEnabled && tt.fields.ipV6Enabled) {
				if tt.fields.isNitroInstance {
					m.awsutils.EXPECT().IsPrefixDelegationSupported().Return(true)
				} else {
					m.awsutils.EXPECT().GetInstanceType().Return("dummy-instance")
					m.awsutils.EXPECT().IsPrefixDelegationSupported().Return(false)
				}
			}
			ds := datastore.NewDataStore(log, datastore.NullCheckpoint{}, tt.fields.prefixDelegationEnabled)

			mockContext := &IPAMContext{
				awsClient:              m.awsutils,
				networkClient:          m.network,
				enableIPv4:             tt.fields.ipV4Enabled,
				enableIPv6:             tt.fields.ipV6Enabled,
				enablePrefixDelegation: tt.fields.prefixDelegationEnabled,
				enablePodENI:           tt.fields.podENIEnabled,
				useCustomNetworking:    tt.fields.customNetworkingEnabled,
				dataStore:              ds,
			}

			resp := mockContext.isConfigValid()
			assert.Equal(t, tt.want, resp)
		})
	}

}
