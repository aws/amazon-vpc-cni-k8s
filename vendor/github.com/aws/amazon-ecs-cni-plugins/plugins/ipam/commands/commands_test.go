// +build !integration,!e2e

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

package commands

import (
	"net"
	"testing"

	"github.com/aws/amazon-ecs-cni-plugins/plugins/ipam/config"
	"github.com/aws/amazon-ecs-cni-plugins/plugins/ipam/ipstore/mocks"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/golang/mock/gomock"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setup(subnetStr, ipStr, gwStr string, t *testing.T) (*config.IPAMConfig, *mock_ipstore.MockIPAllocator) {
	var (
		ip     net.IPNet
		gw     net.IP
		subnet *net.IPNet
		err    error
	)

	_, subnet, err = net.ParseCIDR(subnetStr)
	require.NoError(t, err, "parsing the subnet string failed")

	if ipStr != "" {
		tip, tsub, err := net.ParseCIDR(ipStr)
		require.NoError(t, err, "parsing the ip address failed")
		ip = net.IPNet{
			IP:   tip,
			Mask: tsub.Mask,
		}
	}
	if gwStr != "" {
		gw = net.ParseIP(gwStr)
	}

	ipamConf := &config.IPAMConfig{
		Type:        "ipam",
		IPV4Subnet:  types.IPNet{IP: subnet.IP, Mask: subnet.Mask},
		IPV4Address: types.IPNet{IP: ip.IP, Mask: ip.Mask},
		IPV4Gateway: gw,
	}

	mockCtrl := gomock.NewController(t)
	allocator := mock_ipstore.NewMockIPAllocator(mockCtrl)

	return ipamConf, allocator
}

// TestGetSpecificIPV4HappyPath tests the specified ip will be assigned
func TestGetSpecificIPV4HappyPath(t *testing.T) {
	conf, allocator := setup("10.0.0.0/29", "10.0.0.3/29", "", t)

	gomock.InOrder(
		allocator.EXPECT().Assign(gomock.Any(), gomock.Any()).Return(nil),
		allocator.EXPECT().SetLastKnownIP(net.ParseIP("10.0.0.3")),
	)

	assignedAddress, err := getIPV4Address(allocator, conf)
	assert.NoError(t, err, "get specific ip from subnet failed")
	assert.Equal(t, "10.0.0.3/29", assignedAddress.String(), "Assigned IP is not the one specified")
}

// TestGetNextIPV4HappyPath tests if ip isn't specified, next available one will be picked up
func TestGetNextIPV4HappyPath(t *testing.T) {
	conf, allocator := setup("10.0.0.0/29", "", "", t)

	gomock.InOrder(
		allocator.EXPECT().Exists(config.LastKnownIPKey).Return(true, nil),
		allocator.EXPECT().Get(config.LastKnownIPKey).Return("10.0.0.3", nil),
		allocator.EXPECT().SetLastKnownIP(net.ParseIP("10.0.0.3")),
		allocator.EXPECT().GetAvailableIP(gomock.Any()).Return("10.0.0.4", nil),
	)

	assignedAddress, err := getIPV4Address(allocator, conf)
	assert.NoError(t, err, "get available ip from subnet failed")
	assert.Equal(t, "10.0.0.4/29", assignedAddress.String(), "Assigned ip should be the next available ip")
}

// TestGetUsedIPv4 tests if the specified ip has already been used, it will cause error
func TestGetUsedIPv4(t *testing.T) {
	conf, allocator := setup("10.0.0.0/29", "10.0.0.3/29", "", t)

	allocator.EXPECT().Assign(gomock.Any(), gomock.Any()).Return(errors.New("IP has already been used"))

	assignedAddress, err := getIPV4Address(allocator, conf)
	assert.Error(t, err, "assign an used ip should cause error")
	assert.Nil(t, assignedAddress, "error will cause ip not be assigned")
}

// TestIPUsedUPInSubnet tests there is no available ip in the subnet should cause error
func TestIPUsedUPInSubnet(t *testing.T) {
	conf, allocator := setup("10.0.0.0/29", "", "", t)

	gomock.InOrder(
		allocator.EXPECT().Exists(config.LastKnownIPKey).Return(true, nil),
		allocator.EXPECT().Get(config.LastKnownIPKey).Return("10.0.0.3", nil),
		allocator.EXPECT().SetLastKnownIP(net.ParseIP("10.0.0.3")),
		allocator.EXPECT().GetAvailableIP(gomock.Any()).Return("", errors.New("no available ip in the subnet")),
	)

	assignedAddress, err := getIPV4Address(allocator, conf)
	assert.Error(t, err, "no available ip in the subnet should cause error")
	assert.Nil(t, assignedAddress, "error will cause ip not be assigned")
}

// TestGWUsed tests the default gateway can be used by multiple container
func TestGWUsed(t *testing.T) {
	_, allocator := setup("10.0.0.0/29", "", "", t)
	gw := "10.0.0.1"

	gomock.InOrder(
		allocator.EXPECT().Get(gw).Return(config.GatewayValue, nil),
	)

	err := verifyGateway(net.ParseIP("10.0.0.1"), allocator)
	assert.NoError(t, err, "gateway can be used by multiple containers")
}

// TestGWUsedByContainer tests the gateway address used by container should cause error
func TestGWUsedByContainer(t *testing.T) {
	_, allocator := setup("10.0.0.0/29", "", "10.0.0.2/29", t)

	gomock.InOrder(
		allocator.EXPECT().Get("10.0.0.2").Return("not gateway", nil),
	)

	err := verifyGateway(net.ParseIP("10.0.0.2"), allocator)
	assert.Error(t, err, "gateway used by container should fail the command")
}

func TestConstructResult(t *testing.T) {
	conf, _ := setup("10.0.0.0/29", "", "", t)

	tip, tsub, err := net.ParseCIDR("10.0.0.1/29")
	assert.NoError(t, err, "Parsing the cidr failed")
	ip := net.IPNet{
		IP:   tip,
		Mask: tsub.Mask,
	}

	result, err := constructResults(conf, ip)
	assert.NoError(t, err)
	assert.NotNil(t, result, "Construct result for bridge plugin failed")
	assert.Equal(t, 1, len(result.IPs), "Only one ip should be assigned each time")
}

func TestConstructResultErrorForIPV6(t *testing.T) {
	conf, _ := setup("10.0.0.0/29", "", "", t)

	tip, tsub, err := net.ParseCIDR("2001:db8::2:1/60")
	assert.NoError(t, err, "Parsing the cidr failed")
	ip := net.IPNet{
		IP:   tip,
		Mask: tsub.Mask,
	}

	_, err = constructResults(conf, ip)
	assert.Error(t, err, "currently ipv6 is not support")
}

func TestAddExistedIP(t *testing.T) {
	conf, allocator := setup("10.0.0.0/29", "10.0.0.3/29", "10.0.0.1", t)

	gomock.InOrder(
		allocator.EXPECT().Get(conf.IPV4Gateway.String()).Return(config.GatewayValue, nil),
		allocator.EXPECT().Assign(conf.IPV4Address.IP.String(), gomock.Any()).Return(errors.New("ip already been used")),
	)

	err := add(allocator, conf, "0.3.0")
	assert.Error(t, err, "assign used ip should cause ADD fail")
}

func TestAddConstructResultError(t *testing.T) {
	conf, allocator := setup("10.0.0.0/29", "2001:db8::2:1/60", "10.0.0.1", t)

	gomock.InOrder(
		allocator.EXPECT().Get(conf.IPV4Gateway.String()).Return(config.GatewayValue, nil),
		allocator.EXPECT().Assign(conf.IPV4Address.IP.String(), gomock.Any()).Return(nil),
		allocator.EXPECT().Update(config.LastKnownIPKey, conf.IPV4Address.IP.String()).Return(nil),
	)
	err := add(allocator, conf, "0.3.0")
	assert.Error(t, err, "assign used ip should cause ADD fail")
}

// TestPrintResultError tests the types.Print error cause command fail
func TestPrintResultError(t *testing.T) {
	conf, allocator := setup("10.0.0.0/29", "10.0.0.2/29", "10.0.0.1", t)

	gomock.InOrder(
		allocator.EXPECT().Get(conf.IPV4Gateway.String()).Return(config.GatewayValue, nil),
		allocator.EXPECT().Assign(conf.IPV4Address.IP.String(), gomock.Any()).Return(nil),
		allocator.EXPECT().Update(config.LastKnownIPKey, conf.IPV4Address.IP.String()).Return(nil),
	)
	err := add(allocator, conf, "invalid")
	assert.Error(t, err, "invalid cni version should cause ADD fail")
}

func TestUpdateFail(t *testing.T) {
	conf, allocator := setup("10.0.0.0/29", "10.0.0.2/29", "", t)

	gomock.InOrder(
		allocator.EXPECT().Release("10.0.0.2").Return(nil),
		allocator.EXPECT().Update(config.LastKnownIPKey, conf.IPV4Address.IP.String()).Return(errors.New("update fail")),
	)

	err := del(allocator, conf)
	assert.NoError(t, err, "Update the last known IP should not cause the DEL fail")
}

func TestDelReleaseError(t *testing.T) {
	conf, allocator := setup("10.0.0.0/29", "10.0.0.2/29", "", t)

	gomock.InOrder(
		allocator.EXPECT().Release("10.0.0.2").Return(errors.New("failed to query the db")),
	)

	err := del(allocator, conf)
	assert.Error(t, err, "Release the ip from db failed should cause the DEL fail")
}

// TestDelByID tests the ipam plugin release ip by id
func TestDelByID(t *testing.T) {
	conf, allocator := setup("10.0.0.0/29", "10.0.0.2/29", "", t)
	conf.ID = "TestDelByID"
	conf.IPV4Address = types.IPNet{}

	gomock.InOrder(
		allocator.EXPECT().ReleaseByID(conf.ID).Return("10.0.0.3", nil),
		allocator.EXPECT().Update(config.LastKnownIPKey, "10.0.0.3").Return(nil),
	)

	err := del(allocator, conf)
	assert.NoError(t, err)
}

func TestDelWithoutIDAndIP(t *testing.T) {
	conf, _ := setup("10.0.0.0/29", "10.0.0.2/29", "", t)
	conf.IPV4Address = types.IPNet{}

	err := validateDelConfiguration(conf)
	assert.Error(t, err, "Empty ip and id should cause deletion fail")
}
