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
	"time"

	"github.com/aws/smithy-go"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/golang/mock/gomock"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/vishvananda/netlink"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
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
	"github.com/aws/amazon-vpc-cni-k8s/utils/prometheusmetrics"
	rcscheme "github.com/aws/amazon-vpc-resource-controller-k8s/apis/vpcresources/v1alpha1"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

const (
	primaryENIid           = "eni-00000000"
	secENIid               = "eni-00000001"
	terENIid               = "eni-00000002"
	primaryMAC             = "12:ef:2a:98:e5:5a"
	secMAC                 = "12:ef:2a:98:e5:5b"
	terMAC                 = "12:ef:2a:98:e5:5c"
	primaryDevice          = 0
	secDevice              = 2
	terDevice              = 3
	primarySubnet          = "10.10.10.0/24"
	secSubnet              = "10.10.20.0/24"
	terSubnet              = "10.10.30.0/24"
	ipaddr01               = "10.10.10.11"
	ipaddr02               = "10.10.10.12"
	ipaddr03               = "10.10.10.13"
	ipaddr11               = "10.10.20.11"
	ipaddr12               = "10.10.20.12"
	ipaddr21               = "10.10.30.11"
	ipaddr22               = "10.10.30.12"
	vpcCIDR                = "10.10.0.0/16"
	myNodeName             = "testNodeName"
	prefix01               = "10.10.30.0/28"
	prefix02               = "10.10.40.0/28"
	ipaddrPD01             = "10.10.30.0"
	ipaddrPD02             = "10.10.40.0"
	v6ipaddr01             = "2001:db8::1/128"
	v6prefix01             = "2001:db8::/64"
	instanceID             = "i-0e1f3b9eb950e4980"
	externalEniConfigLabel = "vpc.amazonaws.com/externalEniConfig"
)

type testMocks struct {
	ctrl      *gomock.Controller
	awsutils  *mock_awsutils.MockAPIs
	k8sClient client.Client
	network   *mock_networkutils.MockNetworkAPIs
	eniconfig *mock_eniconfig.MockENIConfig
}

func setup(t *testing.T) *testMocks {
	ctrl := gomock.NewController(t)
	k8sSchema := runtime.NewScheme()
	clientgoscheme.AddToScheme(k8sSchema)
	eniconfigscheme.AddToScheme(k8sSchema)
	rcscheme.AddToScheme(k8sSchema)

	return &testMocks{
		ctrl:      ctrl,
		awsutils:  mock_awsutils.NewMockAPIs(ctrl),
		k8sClient: testclient.NewClientBuilder().WithScheme(k8sSchema).Build(),
		network:   mock_networkutils.NewMockNetworkAPIs(ctrl),
		eniconfig: mock_eniconfig.NewMockENIConfig(ctrl),
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
		awsClient:     m.awsutils,
		k8sClient:     m.k8sClient,
		maxIPsPerENI:  14,
		maxENI:        4,
		warmENITarget: 1,
		warmIPTarget:  3,
		primaryIP:     make(map[string]string),
		terminating:   int32(0),
		networkClient: m.network,
		dataStore:     datastore.NewDataStore(log, datastore.NewTestCheckpoint(fakeCheckpoint), false),
		myNodeName:    myNodeName,
		enableIPv4:    true,
		enableIPv6:    false,
	}

	eni1, eni2, _ := getDummyENIMetadata()

	var cidrs []string
	m.awsutils.EXPECT().GetENILimit().Return(4)
	m.awsutils.EXPECT().GetENIIPv4Limit().Return(14)
	m.awsutils.EXPECT().GetIPv4sFromEC2(eni1.ENIID).AnyTimes().Return(eni1.IPv4Addresses, nil)
	m.awsutils.EXPECT().GetIPv4sFromEC2(eni2.ENIID).AnyTimes().Return(eni2.IPv4Addresses, nil)
	m.awsutils.EXPECT().IsUnmanagedENI(eni1.ENIID).Return(false).AnyTimes()
	m.awsutils.EXPECT().IsUnmanagedENI(eni2.ENIID).Return(false).AnyTimes()
	m.awsutils.EXPECT().TagENI(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	m.awsutils.EXPECT().IsMultiCardENI(eni1.ENIID).Return(false).AnyTimes()
	m.awsutils.EXPECT().IsMultiCardENI(eni2.ENIID).Return(false).AnyTimes()

	primaryIP := net.ParseIP(ipaddr01)
	m.awsutils.EXPECT().GetVPCIPv4CIDRs().AnyTimes().Return(cidrs, nil)
	m.awsutils.EXPECT().GetPrimaryENImac().Return("")
	m.network.EXPECT().SetupHostNetwork(cidrs, "", &primaryIP, false, true, false).Return(nil)
	m.network.EXPECT().CleanUpStaleAWSChains(true, false).Return(nil)
	m.awsutils.EXPECT().GetPrimaryENI().AnyTimes().Return(primaryENIid)
	m.awsutils.EXPECT().RefreshSGIDs(gomock.Any(), gomock.Any()).AnyTimes().Return(nil)

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

	m.awsutils.EXPECT().SetMultiCardENIs(resp.MultiCardENIIDs).AnyTimes()
	m.awsutils.EXPECT().GetLocalIPv4().Return(primaryIP)

	var rules []netlink.Rule
	m.network.EXPECT().GetRuleList().Return(rules, nil)
	m.network.EXPECT().UpdateRuleListBySrc(gomock.Any(), gomock.Any())
	m.network.EXPECT().GetExternalServiceCIDRs().Return(nil)
	m.network.EXPECT().UpdateExternalServiceIpRules(gomock.Any(), gomock.Any())

	maxPods, _ := resource.ParseQuantity("500")
	fakeNode := v1.Node{
		TypeMeta:   metav1.TypeMeta{Kind: "Node"},
		ObjectMeta: metav1.ObjectMeta{Name: myNodeName},
		Spec:       v1.NodeSpec{},
		Status: v1.NodeStatus{
			Capacity: v1.ResourceList{
				v1.ResourcePods: maxPods,
			},
		},
	}
	m.k8sClient.Create(ctx, &fakeNode)

	// Add IPs
	m.awsutils.EXPECT().AllocIPAddresses(gomock.Any(), gomock.Any())
	os.Setenv("MY_NODE_NAME", myNodeName)
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
		k8sClient:              m.k8sClient,
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

	eni1, eni2 := getDummyENIMetadataWithPrefix()
	var cidrs []string
	m.awsutils.EXPECT().GetENILimit().Return(4)
	m.awsutils.EXPECT().GetENIIPv4Limit().Return(14)
	m.awsutils.EXPECT().GetIPv4PrefixesFromEC2(eni1.ENIID).AnyTimes().Return(eni1.IPv4Prefixes, nil)
	m.awsutils.EXPECT().GetIPv4PrefixesFromEC2(eni2.ENIID).AnyTimes().Return(eni2.IPv4Prefixes, nil)
	m.awsutils.EXPECT().IsUnmanagedENI(eni1.ENIID).Return(false).AnyTimes()
	m.awsutils.EXPECT().IsUnmanagedENI(eni2.ENIID).Return(false).AnyTimes()
	m.awsutils.EXPECT().TagENI(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	m.awsutils.EXPECT().IsMultiCardENI(eni1.ENIID).Return(false).AnyTimes()
	m.awsutils.EXPECT().IsMultiCardENI(eni2.ENIID).Return(false).AnyTimes()

	primaryIP := net.ParseIP(ipaddr01)
	m.awsutils.EXPECT().GetVPCIPv4CIDRs().AnyTimes().Return(cidrs, nil)
	m.awsutils.EXPECT().GetPrimaryENImac().Return("")
	m.network.EXPECT().SetupHostNetwork(cidrs, "", &primaryIP, false, true, false).Return(nil)
	m.network.EXPECT().CleanUpStaleAWSChains(true, false).Return(nil)
	m.awsutils.EXPECT().GetPrimaryENI().AnyTimes().Return(primaryENIid)
	m.awsutils.EXPECT().RefreshSGIDs(gomock.Any(), gomock.Any()).AnyTimes().Return(nil)

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
	m.awsutils.EXPECT().SetMultiCardENIs(resp.MultiCardENIIDs).AnyTimes()

	var rules []netlink.Rule
	m.network.EXPECT().GetRuleList().Return(rules, nil)
	m.network.EXPECT().UpdateRuleListBySrc(gomock.Any(), gomock.Any())
	m.network.EXPECT().GetExternalServiceCIDRs().Return(nil)
	m.network.EXPECT().UpdateExternalServiceIpRules(gomock.Any(), gomock.Any())

	maxPods, _ := resource.ParseQuantity("500")
	fakeNode := v1.Node{
		TypeMeta:   metav1.TypeMeta{Kind: "Node"},
		ObjectMeta: metav1.ObjectMeta{Name: myNodeName},
		Spec:       v1.NodeSpec{},
		Status: v1.NodeStatus{
			Capacity: v1.ResourceList{
				v1.ResourcePods: maxPods,
			},
		},
	}
	m.k8sClient.Create(ctx, &fakeNode)

	os.Setenv("MY_NODE_NAME", myNodeName)
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
		k8sClient:              m.k8sClient,
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

	eni1 := getDummyENIMetadataWithV6Prefix()

	var cidrs []string
	m.awsutils.EXPECT().IsUnmanagedENI(eni1.ENIID).Return(false).AnyTimes()
	m.awsutils.EXPECT().TagENI(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	m.awsutils.EXPECT().IsMultiCardENI(eni1.ENIID).Return(false).AnyTimes()

	primaryIP := net.ParseIP(ipaddr01)
	m.network.EXPECT().SetupHostNetwork(cidrs, eni1.MAC, &primaryIP, false, false, true).Return(nil)
	m.network.EXPECT().CleanUpStaleAWSChains(false, true).Return(nil)
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
	m.awsutils.EXPECT().SetMultiCardENIs(resp.MultiCardENIIDs).AnyTimes()

	fakeNode := v1.Node{
		TypeMeta:   metav1.TypeMeta{Kind: "Node"},
		ObjectMeta: metav1.ObjectMeta{Name: myNodeName},
		Spec:       v1.NodeSpec{},
		Status:     v1.NodeStatus{},
	}
	m.k8sClient.Create(ctx, &fakeNode)

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
		IPv4Addresses: []ec2types.NetworkInterfacePrivateIpAddress{
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
		IPv4Addresses: []ec2types.NetworkInterfacePrivateIpAddress{
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
		IPv4Addresses: []ec2types.NetworkInterfacePrivateIpAddress{
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
		IPv4Addresses: []ec2types.NetworkInterfacePrivateIpAddress{
			{
				PrivateIpAddress: &testAddr1, Primary: &primary,
			},
		},
		IPv4Prefixes: []ec2types.Ipv4PrefixSpecification{
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
		IPv4Addresses: []ec2types.NetworkInterfacePrivateIpAddress{
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
		IPv4Addresses: []ec2types.NetworkInterfacePrivateIpAddress{
			{
				PrivateIpAddress: &testAddr1, Primary: &primary,
			},
		},
		IPv6Prefixes: []ec2types.Ipv6PrefixSpecification{
			{
				Ipv6Prefix: &testv6Prefix,
			},
		},
	}

	return eni1
}

func TestIncreaseIPPoolDefault(t *testing.T) {
	_ = os.Unsetenv(envCustomNetworkCfg)
	testIncreaseIPPool(t, false, false, false)
}

func TestIncreaseIPPoolSubnetDiscoveryUnfilledENI(t *testing.T) {
	_ = os.Unsetenv(envCustomNetworkCfg)
	testIncreaseIPPool(t, false, false, true)
}

func TestIncreaseIPPoolCustomENI(t *testing.T) {
	_ = os.Setenv(envCustomNetworkCfg, "true")
	_ = os.Setenv("MY_NODE_NAME", myNodeName)
	testIncreaseIPPool(t, true, false, false)
}

// Testing that the ENI will be allocated on non schedulable node when the AWS_MANAGE_ENIS_NON_SCHEDULABLE is set to `true`
func TestIncreaseIPPoolCustomENIOnNonSchedulableNode(t *testing.T) {
	_ = os.Setenv(envCustomNetworkCfg, "true")
	_ = os.Setenv(envManageENIsNonSchedulable, "true")
	_ = os.Setenv("MY_NODE_NAME", myNodeName)
	testIncreaseIPPool(t, true, true, false)
}

// Testing that the ENI will NOT be allocated on non schedulable node when the AWS_MANAGE_ENIS_NON_SCHEDULABLE is not set
func TestIncreaseIPPoolCustomENIOnNonSchedulableNodeDefault(t *testing.T) {
	_ = os.Unsetenv(envManageENIsNonSchedulable)
	_ = os.Setenv(envCustomNetworkCfg, "true")
	_ = os.Setenv("MY_NODE_NAME", myNodeName)
	testIncreaseIPPool(t, true, true, false)
}

func testIncreaseIPPool(t *testing.T, useENIConfig bool, unschedulabeNode bool, subnetDiscovery bool) {
	m := setup(t)
	defer m.ctrl.Finish()
	ctx := context.Background()

	mockContext := &IPAMContext{
		awsClient:                 m.awsutils,
		k8sClient:                 m.k8sClient,
		maxIPsPerENI:              14,
		maxENI:                    4,
		warmENITarget:             1,
		networkClient:             m.network,
		useCustomNetworking:       UseCustomNetworkCfg(),
		useSubnetDiscovery:        UseSubnetDiscovery(),
		manageENIsNonScheduleable: ManageENIsOnNonSchedulableNode(),
		primaryIP:                 make(map[string]string),
		terminating:               int32(0),
	}
	mockContext.dataStore = testDatastore()
	if subnetDiscovery {
		mockContext.dataStore.AddENI(primaryENIid, primaryDevice, true, false, false)
	}

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

	eniMetadata := []awsutils.ENIMetadata{
		{
			ENIID:          primaryENIid,
			MAC:            primaryMAC,
			DeviceNumber:   primaryDevice,
			SubnetIPv4CIDR: primarySubnet,
			IPv4Addresses: []ec2types.NetworkInterfacePrivateIpAddress{
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
			IPv4Addresses: []ec2types.NetworkInterfacePrivateIpAddress{
				{
					PrivateIpAddress: &testAddr11, Primary: &notPrimary,
				},
				{
					PrivateIpAddress: &testAddr12, Primary: &notPrimary,
				},
			},
		},
	}

	if unschedulabeNode {
		val, exist := os.LookupEnv(envManageENIsNonSchedulable)
		if exist && val == "true" {
			assertAllocationExternalCalls(true, useENIConfig, m, sg, podENIConfig, eni2, eniMetadata, false)
		} else {
			assertAllocationExternalCalls(false, useENIConfig, m, sg, podENIConfig, eni2, eniMetadata, false)
		}
	} else if subnetDiscovery {
		assertAllocationExternalCalls(true, useENIConfig, m, sg, podENIConfig, eni2, eniMetadata, true)
	} else {
		assertAllocationExternalCalls(true, useENIConfig, m, sg, podENIConfig, eni2, eniMetadata, false)
	}

	if mockContext.useCustomNetworking {
		mockContext.myNodeName = myNodeName

		labels := map[string]string{
			"k8s.amazonaws.com/eniConfig": "az1",
		}
		// Create a Fake Node
		fakeNode := v1.Node{
			TypeMeta:   metav1.TypeMeta{Kind: "Node"},
			ObjectMeta: metav1.ObjectMeta{Name: myNodeName, Labels: labels},
			Spec:       v1.NodeSpec{},
			Status:     v1.NodeStatus{},
		}
		if unschedulabeNode {
			fakeNode.Spec.Taints = append(fakeNode.Spec.Taints, corev1.Taint{
				Key:    "node.kubernetes.io/unschedulable",
				Effect: corev1.TaintEffectNoSchedule,
			})
		}
		m.k8sClient.Create(ctx, &fakeNode)

		// Create a dummy ENIConfig
		fakeENIConfig := v1alpha1.ENIConfig{
			TypeMeta:   metav1.TypeMeta{},
			ObjectMeta: metav1.ObjectMeta{Name: "az1"},
			Spec: eniconfigscheme.ENIConfigSpec{
				Subnet:         "subnet1",
				SecurityGroups: []string{"sg1-id", "sg2-id"},
			},
			Status: eniconfigscheme.ENIConfigStatus{},
		}
		m.k8sClient.Create(ctx, &fakeENIConfig)
	}
	mockContext.increaseDatastorePool(ctx)
}

func assertAllocationExternalCalls(shouldCall bool, useENIConfig bool, m *testMocks, sg []*string, podENIConfig *eniconfigscheme.ENIConfigSpec, eni2 string, eniMetadata []awsutils.ENIMetadata, subnetDiscovery bool) {
	callCount := 0
	if shouldCall {
		callCount = 1
	}

	originalErr := errors.New("err")

	if useENIConfig {
		m.awsutils.EXPECT().AllocENI(true, sg, podENIConfig.Subnet, 14).Times(callCount).Return(eni2, nil)
	} else if subnetDiscovery {
		m.awsutils.EXPECT().AllocIPAddresses(primaryENIid, 14).Times(callCount).Return(nil, &smithy.GenericAPIError{
			Code:    "InsufficientFreeAddressesInSubnet",
			Message: originalErr.Error(),
			Fault:   smithy.FaultUnknown,
		})
		m.awsutils.EXPECT().AllocIPAddresses(primaryENIid, 1).Times(callCount).Return(nil, &smithy.GenericAPIError{
			Code:    "InsufficientFreeAddressesInSubnet",
			Message: originalErr.Error(),
			Fault:   smithy.FaultUnknown,
		})
		m.awsutils.EXPECT().AllocENI(false, nil, "", 14).Times(callCount).Return(eni2, nil)
	} else {
		m.awsutils.EXPECT().AllocENI(false, nil, "", 14).Times(callCount).Return(eni2, nil)
	}
	m.awsutils.EXPECT().GetPrimaryENI().Times(callCount).Return(primaryENIid)
	m.awsutils.EXPECT().WaitForENIAndIPsAttached(secENIid, 14).Times(callCount).Return(eniMetadata[1], nil)
	m.network.EXPECT().SetupENINetwork(gomock.Any(), secMAC, secDevice, secSubnet).Times(callCount)
}

func TestIncreasePrefixPoolDefault(t *testing.T) {
	_ = os.Unsetenv(envCustomNetworkCfg)
	testIncreasePrefixPool(t, false, false)
}

func TestIncreasePrefixPoolSubnetDiscoveryUnfilledENI(t *testing.T) {
	_ = os.Unsetenv(envCustomNetworkCfg)
	testIncreasePrefixPool(t, false, true)
}

func TestIncreasePrefixPoolCustomENI(t *testing.T) {
	_ = os.Setenv(envCustomNetworkCfg, "true")
	testIncreasePrefixPool(t, true, false)
}

func testIncreasePrefixPool(t *testing.T, useENIConfig, subnetDiscovery bool) {
	m := setup(t)
	defer m.ctrl.Finish()
	ctx := context.Background()

	mockContext := &IPAMContext{
		awsClient:                 m.awsutils,
		k8sClient:                 m.k8sClient,
		maxIPsPerENI:              256,
		maxPrefixesPerENI:         16,
		maxENI:                    4,
		warmENITarget:             1,
		warmPrefixTarget:          1,
		networkClient:             m.network,
		useCustomNetworking:       UseCustomNetworkCfg(),
		useSubnetDiscovery:        UseSubnetDiscovery(),
		manageENIsNonScheduleable: ManageENIsOnNonSchedulableNode(),
		primaryIP:                 make(map[string]string),
		terminating:               int32(0),
		enablePrefixDelegation:    true,
	}

	mockContext.dataStore = testDatastorewithPrefix()
	if subnetDiscovery {
		mockContext.dataStore.AddENI(primaryENIid, primaryDevice, true, false, false)
	}

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

	originalErr := errors.New("err")

	if useENIConfig {
		m.awsutils.EXPECT().AllocENI(true, sg, podENIConfig.Subnet, 1).Return(eni2, nil)
	} else if subnetDiscovery {
		m.awsutils.EXPECT().AllocIPAddresses(primaryENIid, 1).Return(nil, &smithy.GenericAPIError{
			Code:    "InsufficientFreeAddressesInSubnet",
			Message: originalErr.Error(),
			Fault:   smithy.FaultUnknown,
		})
		m.awsutils.EXPECT().AllocIPAddresses(primaryENIid, 1).Return(nil, &smithy.GenericAPIError{
			Code:    "InsufficientFreeAddressesInSubnet",
			Message: originalErr.Error(),
			Fault:   smithy.FaultUnknown,
		})
		m.awsutils.EXPECT().AllocENI(false, nil, "", 1).Return(eni2, nil)
	} else {
		m.awsutils.EXPECT().AllocENI(false, nil, "", 1).Return(eni2, nil)
	}

	eniMetadata := []awsutils.ENIMetadata{
		{
			ENIID:          primaryENIid,
			MAC:            primaryMAC,
			DeviceNumber:   primaryDevice,
			SubnetIPv4CIDR: primarySubnet,
			IPv4Addresses: []ec2types.NetworkInterfacePrivateIpAddress{
				{
					PrivateIpAddress: &testAddr1, Primary: &primary,
				},
			},
			IPv4Prefixes: []ec2types.Ipv4PrefixSpecification{
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
			IPv4Addresses: []ec2types.NetworkInterfacePrivateIpAddress{
				{
					PrivateIpAddress: &testAddr11, Primary: &primary,
				},
			},
			IPv4Prefixes: []ec2types.Ipv4PrefixSpecification{
				{
					Ipv4Prefix: &testPrefix2,
				},
			},
		},
	}

	m.awsutils.EXPECT().GetPrimaryENI().Return(primaryENIid)
	m.awsutils.EXPECT().WaitForENIAndIPsAttached(secENIid, 1).Return(eniMetadata[1], nil)
	m.network.EXPECT().SetupENINetwork(gomock.Any(), secMAC, secDevice, secSubnet)

	if mockContext.useCustomNetworking {
		mockContext.myNodeName = myNodeName

		labels := map[string]string{
			"k8s.amazonaws.com/eniConfig": "az1",
		}
		// Create a Fake Node
		fakeNode := v1.Node{
			TypeMeta:   metav1.TypeMeta{Kind: "Node"},
			ObjectMeta: metav1.ObjectMeta{Name: myNodeName, Labels: labels},
			Spec:       v1.NodeSpec{},
			Status:     v1.NodeStatus{},
		}
		m.k8sClient.Create(ctx, &fakeNode)

		// Create a dummy ENIConfig
		fakeENIConfig := v1alpha1.ENIConfig{
			TypeMeta:   metav1.TypeMeta{},
			ObjectMeta: metav1.ObjectMeta{Name: "az1"},
			Spec: eniconfigscheme.ENIConfigSpec{
				Subnet:         "subnet1",
				SecurityGroups: []string{"sg1-id", "sg2-id"},
			},
			Status: eniconfigscheme.ENIConfigStatus{},
		}
		m.k8sClient.Create(ctx, &fakeENIConfig)
	}

	mockContext.increaseDatastorePool(ctx)
}

// TestDecreaseIPPool checks that the deallocation honors the warm IP targets when deallocations happens across multiple enis
// Here we setup two enis and allocate two ip addresses each. We set the warm IP target to 1. We expect that the deallocation
// to happen only once in the loop when multiple enis have one freeable ip address each.
func TestDecreaseIPPool(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	mockContext := &IPAMContext{
		awsClient:          m.awsutils,
		warmIPTarget:       1,
		lastDecreaseIPPool: time.Now().Add(-60 * time.Second),
	}
	mockContext.reconcileCooldownCache.cache = make(map[string]time.Time)

	testAddr1 := net.IPNet{IP: net.ParseIP(ipaddr01), Mask: net.IPv4Mask(255, 255, 255, 255)}
	testAddr2 := net.IPNet{IP: net.ParseIP(ipaddr02), Mask: net.IPv4Mask(255, 255, 255, 255)}
	testAddr11 := net.IPNet{IP: net.ParseIP(ipaddr11), Mask: net.IPv4Mask(255, 255, 255, 255)}
	testAddr12 := net.IPNet{IP: net.ParseIP(ipaddr12), Mask: net.IPv4Mask(255, 255, 255, 255)}

	mockContext.dataStore = testDatastore()

	mockContext.dataStore.AddENI(primaryENIid, primaryDevice, true, false, false)
	mockContext.dataStore.AddIPv4CidrToStore(primaryENIid, testAddr1, false)
	mockContext.dataStore.AddIPv4CidrToStore(primaryENIid, testAddr2, false)
	mockContext.dataStore.AssignPodIPv4Address(datastore.IPAMKey{ContainerID: "container1"}, datastore.IPAMMetadata{K8SPodName: "pod1"})

	mockContext.dataStore.AddENI(secENIid, secDevice, true, false, false)
	mockContext.dataStore.AddIPv4CidrToStore(secENIid, testAddr11, false)
	mockContext.dataStore.AddIPv4CidrToStore(secENIid, testAddr12, false)
	mockContext.dataStore.AssignPodIPv4Address(datastore.IPAMKey{ContainerID: "container2"}, datastore.IPAMMetadata{K8SPodName: "pod2"})

	m.awsutils.EXPECT().DeallocPrefixAddresses(gomock.Any(), gomock.Any()).Times(1)
	m.awsutils.EXPECT().DeallocIPAddresses(gomock.Any(), gomock.Any()).Times(1)

	short, over, enabled := mockContext.datastoreTargetState(nil)
	assert.Equal(t, 0, short)      // there would not be any shortage
	assert.Equal(t, 1, over)       // out of 4 IPs we have 2 IPs assigned, warm IP target is 1, so over is 1
	assert.Equal(t, true, enabled) // there is warm ip target enabled with the value of 1

	mockContext.decreaseDatastorePool(10 * time.Second)

	short, over, enabled = mockContext.datastoreTargetState(nil)
	assert.Equal(t, 0, short)      // there would not be any shortage
	assert.Equal(t, 0, over)       // after the above deallocation this should be zero
	assert.Equal(t, true, enabled) // there is warm ip target enabled with the value of 1

	// make another call just to ensure that more deallocations do not happen
	mockContext.decreaseDatastorePool(10 * time.Second)

	short, over, enabled = mockContext.datastoreTargetState(nil)
	assert.Equal(t, 0, short)      // there would not be any shortage
	assert.Equal(t, 0, over)       // after the above deallocation this should be zero
	assert.Equal(t, true, enabled) // there is warm ip target enabled with the value of 1
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
		k8sClient:     m.k8sClient,
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

	m.awsutils.EXPECT().AllocENI(false, nil, "", warmIPTarget).Return(secENIid, nil)
	eniMetadata := []awsutils.ENIMetadata{
		{
			ENIID:          primaryENIid,
			MAC:            primaryMAC,
			DeviceNumber:   primaryDevice,
			SubnetIPv4CIDR: primarySubnet,
			IPv4Addresses: []ec2types.NetworkInterfacePrivateIpAddress{
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
			IPv4Addresses: []ec2types.NetworkInterfacePrivateIpAddress{
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

	mockContext.myNodeName = myNodeName

	// Create a Fake Node
	fakeNode := v1.Node{
		TypeMeta:   metav1.TypeMeta{Kind: "Node"},
		ObjectMeta: metav1.ObjectMeta{Name: myNodeName},
		Spec:       v1.NodeSpec{},
		Status:     v1.NodeStatus{},
	}
	m.k8sClient.Create(ctx, &fakeNode)
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
	m.awsutils.EXPECT().IsMultiCardENI(primaryENIid).AnyTimes().Return(false)
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

	m.awsutils.EXPECT().SetMultiCardENIs(resp.MultiCardENIIDs).AnyTimes()
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
			IPv4Addresses: []ec2types.NetworkInterfacePrivateIpAddress{
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
	m.awsutils.EXPECT().IsMultiCardENI(secENIid).Times(2).Return(false)
	resp2 := awsutils.DescribeAllENIsResult{
		ENIMetadata:     twoENIs,
		TagMap:          map[string]awsutils.TagMap{},
		TrunkENI:        "",
		EFAENIs:         make(map[string]bool),
		MultiCardENIIDs: nil,
	}
	m.awsutils.EXPECT().DescribeAllENIs().Return(resp2, nil)
	m.network.EXPECT().SetupENINetwork(gomock.Any(), secMAC, secDevice, primarySubnet)
	m.awsutils.EXPECT().SetMultiCardENIs(resp2.MultiCardENIIDs).AnyTimes()

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
	m.awsutils.EXPECT().IsMultiCardENI(primaryENIid).AnyTimes().Return(false)
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

	m.awsutils.EXPECT().SetMultiCardENIs(resp.MultiCardENIIDs).AnyTimes()
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
			IPv4Addresses: []ec2types.NetworkInterfacePrivateIpAddress{
				{
					PrivateIpAddress: &testAddr1, Primary: &primary,
				},
			},
			// IPv4Prefixes: make([]*ec2.Ipv4PrefixSpecification, 0),
			IPv4Prefixes: nil,
		},
	}
	m.awsutils.EXPECT().GetAttachedENIs().Return(oneIPUnassigned, nil)
	m.awsutils.EXPECT().GetIPv4PrefixesFromEC2(primaryENIid).Return(oneIPUnassigned[0].IPv4Prefixes, nil)
	// m.awsutils.EXPECT().GetIPv4sFromEC2(primaryENIid).Return(oneIPUnassigned[0].IPv4Addresses, nil)

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
	m.awsutils.EXPECT().IsMultiCardENI(secENIid).Times(2).Return(false)
	resp2 := awsutils.DescribeAllENIsResult{
		ENIMetadata: twoENIs,
		TagMap:      map[string]awsutils.TagMap{},
		TrunkENI:    "",
		EFAENIs:     make(map[string]bool),
	}
	m.awsutils.EXPECT().DescribeAllENIs().Return(resp2, nil)
	m.network.EXPECT().SetupENINetwork(gomock.Any(), secMAC, secDevice, primarySubnet)
	m.awsutils.EXPECT().SetMultiCardENIs(resp2.MultiCardENIIDs).AnyTimes()

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

	_, _, warmIPTargetDefined := mockContext.datastoreTargetState(nil)
	assert.False(t, warmIPTargetDefined)

	mockContext.warmIPTarget = 5
	short, over, warmIPTargetDefined := mockContext.datastoreTargetState(nil)
	assert.True(t, warmIPTargetDefined)
	assert.Equal(t, 5, short)
	assert.Equal(t, 0, over)

	// add 2 addresses to datastore
	_ = mockContext.dataStore.AddENI("eni-1", 1, true, false, false)
	ipv4Addr := net.IPNet{IP: net.ParseIP("1.1.1.1"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	_ = mockContext.dataStore.AddIPv4CidrToStore("eni-1", ipv4Addr, false)
	ipv4Addr = net.IPNet{IP: net.ParseIP("1.1.1.2"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	_ = mockContext.dataStore.AddIPv4CidrToStore("eni-1", ipv4Addr, false)

	short, over, warmIPTargetDefined = mockContext.datastoreTargetState(nil)
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

	short, over, warmIPTargetDefined = mockContext.datastoreTargetState(nil)
	assert.True(t, warmIPTargetDefined)
	assert.Equal(t, 0, short)
	assert.Equal(t, 0, over)
}

func TestGetWarmIPTargetStateWithPDenabled(t *testing.T) {
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

	_, _, warmIPTargetDefined := mockContext.datastoreTargetState(nil)
	assert.False(t, warmIPTargetDefined)

	mockContext.warmIPTarget = 5
	short, over, warmIPTargetDefined := mockContext.datastoreTargetState(nil)
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

	short, over, warmIPTargetDefined = mockContext.datastoreTargetState(nil)
	assert.True(t, warmIPTargetDefined)
	assert.Equal(t, 0, short)
	assert.Equal(t, 1, over)

	// Del 1 address
	_, ipnet, _ = net.ParseCIDR("20.1.1.0/28")
	_ = mockContext.dataStore.DelIPv4CidrFromStore("eni-1", *ipnet, true)

	short, over, warmIPTargetDefined = mockContext.datastoreTargetState(nil)
	assert.True(t, warmIPTargetDefined)
	assert.Equal(t, 0, short)
	assert.Equal(t, 0, over)
}

func TestIPAMContext_nodeIPPoolTooLow(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	type fields struct {
		maxIPsPerENI  int
		maxEni        int
		warmENITarget int
		warmIPTarget  int
		datastore     *datastore.DataStore
		maxPods       int
	}

	tests := []struct {
		name   string
		fields fields
		want   bool
	}{
		{"Test new ds, all defaults", fields{14, 4, 1, 0, testDatastore(), 500}, true},
		{"Test new ds, 0 ENIs", fields{14, 4, 0, 0, testDatastore(), 500}, true},
		{"Test new ds, 3 warm IPs", fields{14, 4, 0, 3, testDatastore(), 500}, true},
		{"Test 3 unused IPs, 1 warm", fields{3, 4, 1, 1, datastoreWith3FreeIPs(), 500}, false},
		{"Test 1 used, 1 warm ENI", fields{3, 4, 1, 0, datastoreWith1Pod1(), 500}, true},
		{"Test 1 used, 0 warm ENI", fields{3, 4, 0, 0, datastoreWith1Pod1(), 500}, false},
		{"Test 3 used, 1 warm ENI", fields{3, 4, 1, 0, datastoreWith3Pods(), 500}, true},
		{"Test 3 used, 0 warm ENI", fields{3, 4, 0, 0, datastoreWith3Pods(), 500}, true},
		{"Test max pods exceeded", fields{3, 4, 0, 5, datastoreWith3Pods(), 3}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &IPAMContext{
				awsClient:              m.awsutils,
				dataStore:              tt.fields.datastore,
				useCustomNetworking:    false,
				networkClient:          m.network,
				maxIPsPerENI:           tt.fields.maxIPsPerENI,
				maxENI:                 tt.fields.maxEni,
				warmENITarget:          tt.fields.warmENITarget,
				warmIPTarget:           tt.fields.warmIPTarget,
				enablePrefixDelegation: false,
				maxPods:                tt.fields.maxPods,
			}
			if got, _ := c.isDatastorePoolTooLow(); got != tt.want {
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
		maxEni            int
		maxPrefixesPerENI int
		warmPrefixTarget  int
		datastore         *datastore.DataStore
		maxPods           int
	}

	tests := []struct {
		name   string
		fields fields
		want   bool
	}{
		{"Test new ds, all defaults", fields{256, 4, 16, 1, testDatastore(), 500}, true},
		{"Test new ds, 0 ENIs", fields{256, 4, 16, 0, testDatastore(), 500}, true},
		{"Test 3 unused IPs, 1 warm", fields{256, 4, 16, 1, datastoreWithFreeIPsFromPrefix(), 500}, false},
		{"Test 1 used, 1 warm Prefix", fields{256, 4, 16, 1, datastoreWith1Pod1FromPrefix(), 500}, true},
		{"Test 1 used, 0 warm Prefix", fields{256, 4, 16, 0, datastoreWith1Pod1FromPrefix(), 500}, false},
		{"Test 3 used, 1 warm Prefix", fields{256, 4, 16, 1, datastoreWith3PodsFromPrefix(), 500}, true},
		{"Test 3 used, 0 warm Prefix", fields{256, 4, 16, 0, datastoreWith3PodsFromPrefix(), 500}, false},
		{"Test max pods exceeded", fields{256, 4, 16, 1, datastoreWith3PodsFromPrefix(), 4}, false},
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
				maxENI:                 tt.fields.maxEni,
				warmPrefixTarget:       tt.fields.warmPrefixTarget,
				enablePrefixDelegation: true,
				maxPods:                tt.fields.maxPods,
			}
			if got, _ := c.isDatastorePoolTooLow(); got != tt.want {
				t.Errorf("nodeIPPoolTooLow() = %v, want %v", got, tt.want)
			}
		})
	}
}

func testDatastore() *datastore.DataStore {
	return datastore.NewDataStore(log, datastore.NewTestCheckpoint(datastore.CheckpointData{Version: datastore.CheckpointFormatVersion}), false)
}

func testDatastorewithPrefix() *datastore.DataStore {
	return datastore.NewDataStore(log, datastore.NewTestCheckpoint(datastore.CheckpointData{Version: datastore.CheckpointFormatVersion}), true)
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
	}, datastore.IPAMMetadata{
		K8SPodNamespace: "default",
		K8SPodName:      "sample-pod",
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
		_, _, _ = datastoreWith3Pods.AssignPodIPv4Address(key, datastore.IPAMMetadata{
			K8SPodNamespace: "default",
			K8SPodName:      fmt.Sprintf("sample-pod-%d", i),
		})
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
	}, datastore.IPAMMetadata{
		K8SPodNamespace: "default",
		K8SPodName:      "sample-pod",
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
		_, _, _ = datastoreWith3Pods.AssignPodIPv4Address(key,
			datastore.IPAMMetadata{
				K8SPodNamespace: "default",
				K8SPodName:      fmt.Sprintf("sample-pod-%d", i),
			})
	}
	return datastoreWith3Pods
}

func TestIPAMContext_filterUnmanagedENIs(t *testing.T) {
	eni1, eni2, eni3 := getDummyENIMetadata()
	allENIs := []awsutils.ENIMetadata{eni1, eni2, eni3}
	primaryENIonly := []awsutils.ENIMetadata{eni1}
	filteredENIonly := []awsutils.ENIMetadata{eni1, eni3}
	Test1TagMap := map[string]awsutils.TagMap{eni1.ENIID: {"hi": "tag", eniNoManageTagKey: "true"}}
	Test2TagMap := map[string]awsutils.TagMap{
		eni2.ENIID: {"hi": "tag", eniNoManageTagKey: "true"},
		eni3.ENIID: {"hi": "tag", eniNoManageTagKey: "true"},
	}
	Test3TagMap := map[string]awsutils.TagMap{
		eni2.ENIID: {"hi": "tag", eniNoManageTagKey: "true"},
		eni3.ENIID: {"hi": "tag", eniNoManageTagKey: "false"},
	}
	Test4TagMap := map[string]awsutils.TagMap{
		eni2.ENIID: {"hi": "tag", eniNoManageTagKey: "true"},
		eni3.ENIID: {"hi": "tag", eniNodeTagKey: instanceID},
	}
	Test5TagMap := map[string]awsutils.TagMap{
		eni2.ENIID: {"hi": "tag", eniNodeTagKey: "i-abcdabcdabcd"},
		eni3.ENIID: {"hi": "tag", eniNodeTagKey: instanceID},
	}

	tests := []struct {
		name                       string
		tagMap                     map[string]awsutils.TagMap
		enis                       []awsutils.ENIMetadata
		want                       []awsutils.ENIMetadata
		unmanagedenis              []string
		expectedGetPrimaryENICalls int
		expectedGetInstanceIDCalls int
	}{
		{"No tags at all", nil, allENIs, allENIs, nil, 0, 0},
		{"Primary ENI unmanaged", Test1TagMap, allENIs, allENIs, nil, 1, 0},
		{"Secondary/Tertiary ENI unmanaged", Test2TagMap, allENIs, primaryENIonly, []string{eni2.ENIID, eni3.ENIID}, 2, 0},
		{"Secondary ENI unmanaged", Test3TagMap, allENIs, filteredENIonly, []string{eni2.ENIID}, 1, 0},
		{"Secondary ENI unmanaged and Tertiary ENI CNI created", Test4TagMap, allENIs, filteredENIonly, []string{eni2.ENIID}, 1, 1},
		{"Secondary ENI not CNI created and Tertiary ENI CNI created", Test5TagMap, allENIs, filteredENIonly, nil, 0, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockAWSUtils := mock_awsutils.NewMockAPIs(ctrl)

			c := &IPAMContext{
				awsClient:                mockAWSUtils,
				enableManageUntaggedMode: true,
			}

			mockAWSUtils.EXPECT().SetUnmanagedENIs(gomock.Any()).
				Do(func(args []string) {
					sort.Strings(tt.unmanagedenis)
					sort.Strings(args)
					assert.Equal(t, tt.unmanagedenis, args)
				}).AnyTimes()

			mockAWSUtils.EXPECT().GetPrimaryENI().Times(tt.expectedGetPrimaryENICalls).Return(eni1.ENIID)
			mockAWSUtils.EXPECT().GetInstanceID().Times(tt.expectedGetInstanceIDCalls).Return(instanceID)

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

			mockAWSUtils.EXPECT().IsMultiCardENI(gomock.Any()).DoAndReturn(
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
	eni1, eni2, eni3 := getDummyENIMetadata()
	allENIs := []awsutils.ENIMetadata{eni1, eni2, eni3}
	primaryENIonly := []awsutils.ENIMetadata{eni1}
	filteredENIonly := []awsutils.ENIMetadata{eni1, eni3}
	Test1TagMap := map[string]awsutils.TagMap{eni1.ENIID: {"hi": "tag", eniNoManageTagKey: "true"}}
	Test2TagMap := map[string]awsutils.TagMap{
		eni2.ENIID: {"hi": "tag", eniNoManageTagKey: "true"},
		eni3.ENIID: {"hi": "tag", eniNoManageTagKey: "true"},
	}
	Test3TagMap := map[string]awsutils.TagMap{
		eni2.ENIID: {"hi": "tag", eniNoManageTagKey: "true"},
		eni3.ENIID: {"hi": "tag", eniNoManageTagKey: "false"},
	}
	Test4TagMap := map[string]awsutils.TagMap{
		eni2.ENIID: {"hi": "tag", eniNoManageTagKey: "true"},
		eni3.ENIID: {"hi": "tag", eniNodeTagKey: instanceID},
	}
	Test5TagMap := map[string]awsutils.TagMap{
		eni2.ENIID: {"hi": "tag", eniNodeTagKey: "i-abcdabcdabcd"},
		eni3.ENIID: {"hi": "tag", eniNodeTagKey: instanceID},
	}

	tests := []struct {
		name                       string
		tagMap                     map[string]awsutils.TagMap
		enis                       []awsutils.ENIMetadata
		want                       []awsutils.ENIMetadata
		unmanagedenis              []string
		expectedGetPrimaryENICalls int
		expectedGetInstanceIDCalls int
	}{
		{"No tags at all", nil, allENIs, allENIs, []string{eni2.ENIID, eni3.ENIID}, 0, 0},
		{"Primary ENI unmanaged", Test1TagMap, allENIs, allENIs, nil, 1, 0},
		{"Secondary/Tertiary ENI unmanaged", Test2TagMap, allENIs, primaryENIonly, []string{eni2.ENIID, eni3.ENIID}, 2, 0},
		{"Secondary ENI unmanaged", Test3TagMap, allENIs, filteredENIonly, []string{eni2.ENIID}, 1, 0},
		{"Secondary ENI unmanaged and Tertiary ENI CNI created", Test4TagMap, allENIs, filteredENIonly, []string{eni2.ENIID}, 1, 1},
		{"Secondary ENI not CNI created and Tertiary ENI CNI created", Test5TagMap, allENIs, filteredENIonly, []string{eni2.ENIID}, 1, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)

			defer ctrl.Finish()

			mockAWSUtils := mock_awsutils.NewMockAPIs(ctrl)

			c := &IPAMContext{
				awsClient:                mockAWSUtils,
				enableManageUntaggedMode: false,
			}

			mockAWSUtils.EXPECT().GetPrimaryENI().Times(tt.expectedGetPrimaryENICalls).Return(eni1.ENIID)
			mockAWSUtils.EXPECT().GetInstanceID().Times(tt.expectedGetInstanceIDCalls).Return(instanceID)

			mockAWSUtils.
				EXPECT().
				SetUnmanagedENIs(gomock.Any()).
				Do(func(args []string) {
					sort.Strings(tt.unmanagedenis)
					sort.Strings(args)
					assert.Equal(t, tt.unmanagedenis, args)
				}).AnyTimes()

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

			mockAWSUtils.EXPECT().IsMultiCardENI(gomock.Any()).DoAndReturn(
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
	disabled := disableENIProvisioning()
	assert.True(t, disabled)

	_ = os.Unsetenv(envDisableENIProvisioning)
	disabled = disableENIProvisioning()
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
	m.awsutils.EXPECT().IsMultiCardENI(eniID).Return(false).AnyTimes()

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
			IPv4Addresses: []ec2types.NetworkInterfacePrivateIpAddress{
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
			IPv4Addresses: []ec2types.NetworkInterfacePrivateIpAddress{
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
	m.awsutils.EXPECT().IsMultiCardENI(eniID).Return(false).AnyTimes()

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
			IPv4Addresses: []ec2types.NetworkInterfacePrivateIpAddress{
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
			IPv4Addresses: []ec2types.NetworkInterfacePrivateIpAddress{
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
		IPv4Addresses: []ec2types.NetworkInterfacePrivateIpAddress{
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
		IPv4Addresses: []ec2types.NetworkInterfacePrivateIpAddress{
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
		IPv4Addresses: []ec2types.NetworkInterfacePrivateIpAddress{
			{
				PrivateIpAddress: &testAddr1, Primary: &primary,
			},
		},
		IPv4Prefixes: []ec2types.Ipv4PrefixSpecification{
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
		IPv4Addresses: []ec2types.NetworkInterfacePrivateIpAddress{
			{
				PrivateIpAddress: &testAddr3, Primary: &primary,
			},
		},
		IPv4Prefixes: []ec2types.Ipv4PrefixSpecification{
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
	// mockContext.primaryIP[]

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
		IPv4Addresses: []ec2types.NetworkInterfacePrivateIpAddress{
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
	// mockContext.primaryIP[]

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
		IPv4Addresses: []ec2types.NetworkInterfacePrivateIpAddress{
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

func TestIPAMContext_enableSecurityGroupsForPods(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()
	ctx := context.Background()

	mockContext := &IPAMContext{
		k8sClient:     m.k8sClient,
		enableIPv4:    true,
		enableIPv6:    false,
		dataStore:     datastore.NewDataStore(log, datastore.NewTestCheckpoint(datastore.CheckpointData{Version: datastore.CheckpointFormatVersion}), false),
		awsClient:     m.awsutils,
		networkClient: m.network,
		primaryIP:     make(map[string]string),
		terminating:   int32(0),
		maxENI:        1,
		myNodeName:    myNodeName,
	}

	fakeNode := v1.Node{
		TypeMeta:   metav1.TypeMeta{Kind: "Node"},
		ObjectMeta: metav1.ObjectMeta{Name: myNodeName},
		Spec:       v1.NodeSpec{},
		Status:     v1.NodeStatus{},
	}
	m.k8sClient.Create(ctx, &fakeNode)

	fakeCNINode := rcscheme.CNINode{
		ObjectMeta: metav1.ObjectMeta{Name: fakeNode.Name},
	}

	err := m.k8sClient.Create(ctx, &fakeCNINode)
	assert.NoError(t, err)

	_ = mockContext.dataStore.AddENI("eni-1", 1, true, false, false)
	// If ENABLE_POD_ENI is not set, nothing happens
	mockContext.tryEnableSecurityGroupsForPods(ctx)

	mockContext.enablePodENI = true
	mockContext.tryEnableSecurityGroupsForPods(ctx)
	var notUpdatedNode corev1.Node
	NodeKey := types.NamespacedName{
		Namespace: "",
		Name:      myNodeName,
	}
	err = m.k8sClient.Get(ctx, NodeKey, &notUpdatedNode)
	assert.NoError(t, err)
	var cniNode rcscheme.CNINode

	err = mockContext.k8sClient.Get(ctx, types.NamespacedName{
		Name: fakeNode.Name,
	}, &cniNode)
	assert.NoError(t, err)

	contained := lo.ContainsBy(cniNode.Spec.Features, func(addedFeature rcscheme.Feature) bool {
		return rcscheme.SecurityGroupsForPods == addedFeature.Name && addedFeature.Value == ""
	})
	assert.False(t, contained, "CNINode should not be updated when there is no room for a trunk ENI")
	assert.Equal(t, 0, len(cniNode.Spec.Features))

	// Make room for trunk ENI
	mockContext.maxENI = 4
	mockContext.tryEnableSecurityGroupsForPods(ctx)

	err = mockContext.k8sClient.Get(ctx, types.NamespacedName{
		Name: fakeNode.Name,
	}, &cniNode)
	assert.NoError(t, err)

	contained = lo.ContainsBy(cniNode.Spec.Features, func(addedFeature rcscheme.Feature) bool {
		return rcscheme.SecurityGroupsForPods == addedFeature.Name && addedFeature.Value == ""
	})
	assert.True(t, contained, "CNINode should be updated when there is room for a trunk ENI")
	assert.Equal(t, 1, len(cniNode.Spec.Features))
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
			want: true,
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

			if tt.fields.prefixDelegationEnabled {
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

func TestAnnotatePod(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()
	ctx := context.Background()

	// Define the Pod objects to test
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "test-namespace",
		},
	}

	mockContext := &IPAMContext{
		awsClient:     m.awsutils,
		k8sClient:     m.k8sClient,
		primaryIP:     make(map[string]string),
		terminating:   int32(0),
		networkClient: m.network,
		dataStore:     testDatastore(),
		enableIPv4:    true,
		enableIPv6:    false,
	}

	mockContext.k8sClient.Create(ctx, &pod)
	ipOne := "10.0.0.1"
	ipTwo := "10.0.0.2"

	// Test basic add operation for new pod
	err := mockContext.AnnotatePod(pod.Name, pod.Namespace, "ip-address", ipOne, "")
	assert.NoError(t, err)

	updatedPod, err := mockContext.GetPod(pod.Name, pod.Namespace)
	assert.NoError(t, err)
	assert.Equal(t, ipOne, updatedPod.Annotations["ip-address"])

	// Test that add operation is idempotent
	err = mockContext.AnnotatePod(pod.Name, pod.Namespace, "ip-address", ipOne, "")
	assert.NoError(t, err)

	updatedPod, err = mockContext.GetPod(pod.Name, pod.Namespace)
	assert.NoError(t, err)
	assert.Equal(t, ipOne, updatedPod.Annotations["ip-address"])

	// Test that add operation always overwrites value for existing pod
	err = mockContext.AnnotatePod(pod.Name, pod.Namespace, "ip-address", ipTwo, "")
	assert.NoError(t, err)

	updatedPod, err = mockContext.GetPod(pod.Name, pod.Namespace)
	assert.NoError(t, err)
	assert.Equal(t, ipTwo, updatedPod.Annotations["ip-address"])

	// Test that delete operation will not overwrite if IP being released does not match existing value
	err = mockContext.AnnotatePod(pod.Name, pod.Namespace, "ip-address", "", ipOne)
	assert.Error(t, err)
	assert.Equal(t, fmt.Errorf("Released IP %s does not match existing annotation. Not patching pod.", ipOne), err)

	updatedPod, err = mockContext.GetPod(pod.Name, pod.Namespace)
	assert.Equal(t, ipTwo, updatedPod.Annotations["ip-address"])

	// Test that delete operation succeeds when IP being released matches existing value
	err = mockContext.AnnotatePod(pod.Name, pod.Namespace, "ip-address", "", ipTwo)
	assert.NoError(t, err)

	updatedPod, err = mockContext.GetPod(pod.Name, pod.Namespace)
	assert.NoError(t, err)
	assert.Equal(t, "", updatedPod.Annotations["ip-address"])

	// Test that delete on a non-existant pod fails without crashing
	err = mockContext.AnnotatePod("no-exist-name", "no-exist-namespace", "ip-address", "", ipTwo)
	assert.NoError(t, err)
}

func TestAddFeatureToCNINode(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()
	ctx := context.Background()

	nodeName := "fake-node-name"
	key := types.NamespacedName{
		Name: nodeName,
	}

	tests := []struct {
		testFeatures      []rcscheme.Feature
		testFeatureLength int
		sgp               bool
		customNet         bool
		msg               string
	}{
		{
			testFeatures: []rcscheme.Feature{
				{
					Name:  rcscheme.SecurityGroupsForPods,
					Value: "",
				},
			},
			testFeatureLength: 1,
			sgp:               true,
			customNet:         false,
			msg:               "test adding one new feature to CNINode",
		},
		{
			testFeatures: []rcscheme.Feature{
				{
					Name:  rcscheme.SecurityGroupsForPods,
					Value: "",
				},
				{
					Name:  rcscheme.CustomNetworking,
					Value: "default",
				},
			},
			testFeatureLength: 2,
			sgp:               true,
			customNet:         true,
			msg:               "test adding two new feature to CNINode",
		},
		{
			testFeatures: []rcscheme.Feature{
				{
					Name:  rcscheme.SecurityGroupsForPods,
					Value: "",
				},
				{
					Name:  rcscheme.CustomNetworking,
					Value: "default",
				},
			},
			testFeatureLength: 2,
			sgp:               true,
			customNet:         true,
			msg:               "test adding duplicated features to CNINode",
		},
		{
			testFeatures: []rcscheme.Feature{
				{
					Name:  rcscheme.SecurityGroupsForPods,
					Value: "",
				},
				{
					Name:  rcscheme.CustomNetworking,
					Value: "update",
				},
			},
			testFeatureLength: 2,
			sgp:               true,
			customNet:         true,
			msg:               "test updating existing feature to CNINode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.msg, func(t *testing.T) {
			mockContext := &IPAMContext{
				awsClient: m.awsutils,
				k8sClient: m.k8sClient,
			}

			nodeName := "fake-node-name"
			mockContext.myNodeName = nodeName
			fakeCNINode := &rcscheme.CNINode{
				ObjectMeta: metav1.ObjectMeta{Name: nodeName, Namespace: ""},
			}
			// don't check error and let it fail open since we need to create CNINode in test Runner
			mockContext.k8sClient.Create(ctx, fakeCNINode)

			var sgpValue, cnValue string
			var err error
			for _, feature := range tt.testFeatures {
				err = mockContext.AddFeatureToCNINode(ctx, feature.Name, feature.Value)
				assert.NoError(t, err)
				if feature.Name == rcscheme.SecurityGroupsForPods {
					sgpValue = feature.Value
				} else if feature.Name == rcscheme.CustomNetworking {
					cnValue = feature.Value
				}
			}
			var wantedCNINode rcscheme.CNINode
			err = mockContext.k8sClient.Get(ctx, key, &wantedCNINode)
			assert.NoError(t, err)
			assert.True(t, len(wantedCNINode.Spec.Features) == tt.testFeatureLength)
			containedSGP := lo.ContainsBy(wantedCNINode.Spec.Features, func(addedFeature rcscheme.Feature) bool {
				return rcscheme.SecurityGroupsForPods == addedFeature.Name && addedFeature.Value == sgpValue
			})
			containedCN := lo.ContainsBy(wantedCNINode.Spec.Features, func(addedFeature rcscheme.Feature) bool {
				return rcscheme.CustomNetworking == addedFeature.Name && addedFeature.Value == cnValue
			})
			assert.True(t, containedSGP == tt.sgp)
			assert.True(t, containedCN == tt.customNet)
		})
	}
}

func TestPodENIErrInc(t *testing.T) {
	// Reset metrics before test
	prometheusmetrics.PodENIErr.Reset()

	m := setup(t)
	defer m.ctrl.Finish()
	ctx := context.Background()

	mockContext := &IPAMContext{
		awsClient:     m.awsutils,
		k8sClient:     m.k8sClient,
		networkClient: m.network,
		primaryIP:     make(map[string]string),
		terminating:   int32(0),
		dataStore:     testDatastore(),
		enableIPv4:    true,
		enableIPv6:    false,
		enablePodENI:  true,
	}

	// Create a test pod
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "test-namespace",
		},
	}
	err := mockContext.k8sClient.Create(ctx, pod)
	assert.NoError(t, err)

	// Mock AWS API error
	m.awsutils.EXPECT().AllocENI(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return("", errors.New("API error")).Times(2) // Expect 2 calls

	// Test case 1: First error
	err = mockContext.tryAssignPodENI(ctx, pod, "test-function")
	assert.Error(t, err)

	// Verify metric was incremented
	count := testutil.ToFloat64(prometheusmetrics.PodENIErr.With(prometheus.Labels{
		"fn": "test-function",
	}))
	assert.Equal(t, float64(1), count, "Expected error count to be 1 for test-function")

	// Test case 2: Second error with different function
	err = mockContext.tryAssignPodENI(ctx, pod, "another-function")
	assert.Error(t, err)

	// Verify counts for both functions
	count = testutil.ToFloat64(prometheusmetrics.PodENIErr.With(prometheus.Labels{
		"fn": "another-function",
	}))
	assert.Equal(t, float64(1), count, "Expected error count to be 1 for another-function")

	count = testutil.ToFloat64(prometheusmetrics.PodENIErr.With(prometheus.Labels{
		"fn": "test-function",
	}))
	assert.Equal(t, float64(1), count, "Expected error count to remain 1 for test-function")
}

func (c *IPAMContext) tryAssignPodENI(ctx context.Context, pod *corev1.Pod, fnName string) error {
	// Mock implementation for the test
	_, err := c.awsClient.AllocENI(false, nil, "", 0)
	if err != nil {
		prometheusmetrics.PodENIErr.With(prometheus.Labels{"fn": fnName}).Inc()
		return err
	}
	return nil
}
