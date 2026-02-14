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

package datastore

import (
	"errors"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	mock_netlinkwrapper "github.com/aws/amazon-vpc-cni-k8s/pkg/netlinkwrapper/mocks"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/networkutils"
	"github.com/aws/amazon-vpc-cni-k8s/utils/prometheusmetrics"
	"github.com/golang/mock/gomock"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

var logConfig = logger.Configuration{
	LogLevel:    "Debug",
	LogLocation: "stdout",
}

var Testlog = logger.New(&logConfig)

var defaultNetworkCard = 0

func TestAddENI(t *testing.T) {
	ds := NewDataStore(Testlog, NullCheckpoint{}, false, defaultNetworkCard)

	err := ds.AddENI("eni-1", 1, true, false, false, networkutils.CalculateRouteTableId(1, 0), "subnet-1")
	assert.NoError(t, err)

	err = ds.AddENI("eni-0", 0, true, false, false, networkutils.CalculateRouteTableId(0, 0), "subnet-0")
	assert.NoError(t, err)

	err = ds.AddENI("eni-1", 1, true, false, false, networkutils.CalculateRouteTableId(0, 0), "subnet-1")
	assert.Error(t, err)

	err = ds.AddENI("eni-2", 2, false, false, false, networkutils.CalculateRouteTableId(2, 0), "subnet-2")
	assert.NoError(t, err)

	assert.Equal(t, len(ds.eniPool), 3)

	eniInfos := ds.GetENIInfos()
	assert.Equal(t, len(eniInfos.ENIs), 3)

	// Verify SubnetID is correctly set
	assert.Equal(t, "subnet-1", ds.eniPool["eni-1"].SubnetID)
	assert.Equal(t, "subnet-0", ds.eniPool["eni-0"].SubnetID)
	assert.Equal(t, "subnet-2", ds.eniPool["eni-2"].SubnetID)
}

func TestENISubnetID(t *testing.T) {
	ds := NewDataStore(Testlog, NullCheckpoint{}, false, defaultNetworkCard)

	// Test adding ENI with SubnetID
	err := ds.AddENI("eni-1", 1, false, false, false, networkutils.CalculateRouteTableId(1, 0), "subnet-abc123")
	assert.NoError(t, err)

	// Verify SubnetID is stored correctly
	eni, ok := ds.eniPool["eni-1"]
	assert.True(t, ok)
	assert.Equal(t, "subnet-abc123", eni.SubnetID)

	// Test adding ENI with empty SubnetID
	err = ds.AddENI("eni-2", 2, false, false, false, networkutils.CalculateRouteTableId(2, 0), "")
	assert.NoError(t, err)

	eni2, ok := ds.eniPool["eni-2"]
	assert.True(t, ok)
	assert.Equal(t, "", eni2.SubnetID)
}

func TestDeleteENI(t *testing.T) {
	ds := NewDataStore(Testlog, NullCheckpoint{}, false, defaultNetworkCard)

	enis := []struct {
		id          string
		device      int
		isPrimary   bool
		networkCard int
	}{
		{"eni-1", 0, true, 0},
		{"eni-2", 2, false, 0},
		{"eni-3", 3, false, 0},
	}

	for _, eni := range enis {

		err := ds.AddENI(eni.id, eni.device, eni.isPrimary, false, false, networkutils.CalculateRouteTableId(eni.device, eni.networkCard), "")
		assert.NoError(t, err)
	}

	eniInfos := ds.GetENIInfos()
	assert.Equal(t, len(eniInfos.ENIs), 3)

	err := ds.RemoveENIFromDataStore("eni-2", false)
	assert.NoError(t, err)

	eniInfos = ds.GetENIInfos()
	assert.Equal(t, len(eniInfos.ENIs), 2)

	err = ds.RemoveENIFromDataStore("unknown-eni", false)
	assert.Error(t, err)

	eniInfos = ds.GetENIInfos()
	assert.Equal(t, len(eniInfos.ENIs), 2)

	// Add an IP and assign a pod.
	ipv4Addr := net.IPNet{IP: net.ParseIP("1.1.1.1"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	err = ds.AddIPv4CidrToStore("eni-1", ipv4Addr, false)
	assert.NoError(t, err)
	ip, device, tableNumber, err := ds.AssignPodIPv4Address(
		IPAMKey{"net1", "sandbox1", "eth0"},
		IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod"})
	assert.NoError(t, err)
	assert.Equal(t, "1.1.1.1", ip)
	assert.Equal(t, 0, device)
	assert.Equal(t, 254, tableNumber)

	// Test force removal.  The first call fails because eni-1 has an IP with a pod assigned to it,
	// but the second call force-removes it and succeeds.
	err = ds.RemoveENIFromDataStore("eni-1", false)
	assert.Error(t, err)
	err = ds.RemoveENIFromDataStore("eni-1", true)
	assert.NoError(t, err)

}

func TestDeleteENIwithPDEnabled(t *testing.T) {
	ds := NewDataStore(Testlog, NullCheckpoint{}, true, defaultNetworkCard)
	var err error
	enis := []struct {
		id          string
		device      int
		isPrimary   bool
		networkCard int
	}{
		{"eni-1", 0, true, 0},
		{"eni-2", 2, false, 0},
		{"eni-3", 3, false, 0},
		{"eni-4", 4, false, 0},
	}

	for _, eni := range enis {
		err = ds.AddENI(eni.id, eni.device, eni.isPrimary, false, false, networkutils.CalculateRouteTableId(eni.device, eni.networkCard), "")
		assert.NoError(t, err)
	}

	eniInfos := ds.GetENIInfos()
	assert.Equal(t, len(eniInfos.ENIs), 4)

	err = ds.RemoveENIFromDataStore("eni-2", false)
	assert.NoError(t, err)

	eniInfos = ds.GetENIInfos()
	assert.Equal(t, len(eniInfos.ENIs), 3)

	err = ds.RemoveENIFromDataStore("unknown-eni", false)
	assert.Error(t, err)

	eniInfos = ds.GetENIInfos()
	assert.Equal(t, len(eniInfos.ENIs), 3)

	// Add a prefix and assign a pod
	ipv4Addr := net.IPNet{IP: net.ParseIP("10.0.0.0"), Mask: net.IPv4Mask(255, 255, 255, 240)}
	err = ds.AddIPv4CidrToStore("eni-4", ipv4Addr, true)
	assert.NoError(t, err)
	ip, device, tableNumber, err := ds.AssignPodIPv4Address(
		IPAMKey{"net1", "sandbox1", "eth0"},
		IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod"})
	assert.NoError(t, err)
	assert.Equal(t, "10.0.0.0", ip)
	assert.Equal(t, 4, device)
	assert.Equal(t, 5, tableNumber)

	// Test force removal.  The first call fails because eni-1 has an IP with a pod assigned to it,
	// but the second call force-removes it and succeeds.
	err = ds.RemoveENIFromDataStore("eni-4", false)
	assert.Error(t, err)
	err = ds.RemoveENIFromDataStore("eni-4", true)
	assert.NoError(t, err)

}

func TestAddENIIPv4Address(t *testing.T) {
	ds := NewDataStore(Testlog, NullCheckpoint{}, false, defaultNetworkCard)

	err := ds.AddENI("eni-1", 1, true, false, false, networkutils.CalculateRouteTableId(1, 0), "")
	assert.NoError(t, err)

	err = ds.AddENI("eni-2", 2, false, false, false, networkutils.CalculateRouteTableId(2, 0), "")
	assert.NoError(t, err)

	ipv4Addr := net.IPNet{IP: net.ParseIP("1.1.1.1"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	err = ds.AddIPv4CidrToStore("eni-1", ipv4Addr, false)
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 1)
	assert.Equal(t, len(ds.eniPool["eni-1"].AvailableIPv4Cidrs), 1)

	ipv4Addr = net.IPNet{IP: net.ParseIP("1.1.1.1"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	err = ds.AddIPv4CidrToStore("eni-1", ipv4Addr, false)
	assert.Error(t, err)
	assert.Equal(t, ds.total, 1)
	assert.Equal(t, len(ds.eniPool["eni-1"].AvailableIPv4Cidrs), 1)

	ipv4Addr = net.IPNet{IP: net.ParseIP("1.1.1.2"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	err = ds.AddIPv4CidrToStore("eni-1", ipv4Addr, false)
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 2)
	assert.Equal(t, len(ds.eniPool["eni-1"].AvailableIPv4Cidrs), 2)

	ipv4Addr = net.IPNet{IP: net.ParseIP("1.1.2.2"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	err = ds.AddIPv4CidrToStore("eni-2", ipv4Addr, false)
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 3)
	assert.Equal(t, len(ds.eniPool["eni-1"].AvailableIPv4Cidrs), 2)
	assert.Equal(t, len(ds.eniPool["eni-2"].AvailableIPv4Cidrs), 1)

	ipv4Addr = net.IPNet{IP: net.ParseIP("1.1.2.2"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	err = ds.AddIPv4CidrToStore("dummy-eni", ipv4Addr, false)
	assert.Error(t, err)
	assert.Equal(t, ds.total, 3)
	assert.Equal(t, len(ds.eniPool["eni-1"].AvailableIPv4Cidrs), 2)
	assert.Equal(t, len(ds.eniPool["eni-2"].AvailableIPv4Cidrs), 1)

}

func TestAddENIIPv4AddressWithPDEnabled(t *testing.T) {
	ds := NewDataStore(Testlog, NullCheckpoint{}, true, defaultNetworkCard)

	err := ds.AddENI("eni-1", 1, true, false, false, networkutils.CalculateRouteTableId(1, 0), "")
	assert.NoError(t, err)

	err = ds.AddENI("eni-2", 2, false, false, false, networkutils.CalculateRouteTableId(2, 0), "")
	assert.NoError(t, err)

	ipv4Addr := net.IPNet{IP: net.ParseIP("10.0.0.0"), Mask: net.IPv4Mask(255, 255, 255, 240)}
	err = ds.AddIPv4CidrToStore("eni-1", ipv4Addr, true)
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 16)
	assert.Equal(t, len(ds.eniPool["eni-1"].AvailableIPv4Cidrs), 1)

	ipv4Addr = net.IPNet{IP: net.ParseIP("10.0.0.0"), Mask: net.IPv4Mask(255, 255, 255, 240)}
	err = ds.AddIPv4CidrToStore("eni-1", ipv4Addr, true)
	assert.Error(t, err)
	assert.Equal(t, ds.total, 16)
	assert.Equal(t, len(ds.eniPool["eni-1"].AvailableIPv4Cidrs), 1)

	ipv4Addr = net.IPNet{IP: net.ParseIP("20.0.0.0"), Mask: net.IPv4Mask(255, 255, 255, 240)}
	err = ds.AddIPv4CidrToStore("eni-1", ipv4Addr, true)
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 32)
	assert.Equal(t, len(ds.eniPool["eni-1"].AvailableIPv4Cidrs), 2)

	ipv4Addr = net.IPNet{IP: net.ParseIP("30.0.0.0"), Mask: net.IPv4Mask(255, 255, 255, 240)}
	err = ds.AddIPv4CidrToStore("eni-2", ipv4Addr, true)
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 48)
	assert.Equal(t, len(ds.eniPool["eni-1"].AvailableIPv4Cidrs), 2)
	assert.Equal(t, len(ds.eniPool["eni-2"].AvailableIPv4Cidrs), 1)

	ipv4Addr = net.IPNet{IP: net.ParseIP("40.0.0.0"), Mask: net.IPv4Mask(255, 255, 255, 240)}
	err = ds.AddIPv4CidrToStore("dummy-eni", ipv4Addr, true)
	assert.Error(t, err)
	assert.Equal(t, ds.total, 48)
	assert.Equal(t, len(ds.eniPool["eni-1"].AvailableIPv4Cidrs), 2)
	assert.Equal(t, len(ds.eniPool["eni-2"].AvailableIPv4Cidrs), 1)

}

func TestGetENIIPs(t *testing.T) {
	ds := NewDataStore(Testlog, NullCheckpoint{}, false, defaultNetworkCard)

	err := ds.AddENI("eni-1", 1, true, false, false, networkutils.CalculateRouteTableId(1, 0), "")
	assert.NoError(t, err)

	err = ds.AddENI("eni-2", 2, false, false, false, networkutils.CalculateRouteTableId(2, 0), "")
	assert.NoError(t, err)

	ipv4Addr := net.IPNet{IP: net.ParseIP("1.1.1.1"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	err = ds.AddIPv4CidrToStore("eni-1", ipv4Addr, false)
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 1)
	assert.Equal(t, len(ds.eniPool["eni-1"].AvailableIPv4Cidrs), 1)

	ipv4Addr = net.IPNet{IP: net.ParseIP("1.1.1.2"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	err = ds.AddIPv4CidrToStore("eni-1", ipv4Addr, false)
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 2)
	assert.Equal(t, len(ds.eniPool["eni-1"].AvailableIPv4Cidrs), 2)

	ipv4Addr = net.IPNet{IP: net.ParseIP("1.1.2.2"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	err = ds.AddIPv4CidrToStore("eni-2", ipv4Addr, false)
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 3)
	assert.Equal(t, len(ds.eniPool["eni-1"].AvailableIPv4Cidrs), 2)
	assert.Equal(t, len(ds.eniPool["eni-2"].AvailableIPv4Cidrs), 1)

	eniIPPool, _, err := ds.GetENICIDRs("eni-1")
	assert.NoError(t, err)
	assert.Equal(t, len(eniIPPool), 2)

	_, _, err = ds.GetENICIDRs("dummy-eni")
	assert.Error(t, err)
}

func TestGetENIIPsWithPDEnabled(t *testing.T) {
	ds := NewDataStore(Testlog, NullCheckpoint{}, true, defaultNetworkCard)
	var err error
	enis := []struct {
		id          string
		device      int
		isPrimary   bool
		networkCard int
	}{
		{"eni-1", 0, true, 0},
		{"eni-2", 2, false, 0},
	}

	for _, eni := range enis {
		err = ds.AddENI(eni.id, eni.device, eni.isPrimary, false, false, networkutils.CalculateRouteTableId(eni.device, eni.networkCard), "")
		assert.NoError(t, err)
	}

	ipv4Addr := net.IPNet{IP: net.ParseIP("10.0.0.0"), Mask: net.IPv4Mask(255, 255, 255, 240)}
	err = ds.AddIPv4CidrToStore("eni-1", ipv4Addr, true)
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 16)
	assert.Equal(t, len(ds.eniPool["eni-1"].AvailableIPv4Cidrs), 1)

	ipv4Addr = net.IPNet{IP: net.ParseIP("20.0.0.0"), Mask: net.IPv4Mask(255, 255, 255, 240)}
	err = ds.AddIPv4CidrToStore("eni-1", ipv4Addr, true)
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 32)
	assert.Equal(t, len(ds.eniPool["eni-1"].AvailableIPv4Cidrs), 2)

	ipv4Addr = net.IPNet{IP: net.ParseIP("30.0.0.0"), Mask: net.IPv4Mask(255, 255, 255, 240)}
	err = ds.AddIPv4CidrToStore("eni-2", ipv4Addr, true)
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 48)
	assert.Equal(t, len(ds.eniPool["eni-1"].AvailableIPv4Cidrs), 2)
	assert.Equal(t, len(ds.eniPool["eni-2"].AvailableIPv4Cidrs), 1)

	_, eniPrefixPool, err := ds.GetENICIDRs("eni-1")
	assert.NoError(t, err)
	assert.Equal(t, len(eniPrefixPool), 2)

	_, _, err = ds.GetENICIDRs("dummy-eni")
	assert.Error(t, err)
}

func TestDelENIIPv4Address(t *testing.T) {
	ds := NewDataStore(Testlog, NullCheckpoint{}, false, defaultNetworkCard)
	err := ds.AddENI("eni-1", 0, true, false, false, unix.RT_TABLE_MAIN, "")
	assert.NoError(t, err)

	ipv4Addr := net.IPNet{IP: net.ParseIP("1.1.1.1"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	err = ds.AddIPv4CidrToStore("eni-1", ipv4Addr, false)
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 1)
	assert.Equal(t, len(ds.eniPool["eni-1"].AvailableIPv4Cidrs), 1)

	// Assign a pod.
	key := IPAMKey{"net0", "sandbox-1", "eth0"}
	ip, device, tableNumber, err := ds.AssignPodIPv4Address(key, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-1"})
	assert.NoError(t, err)
	assert.Equal(t, "1.1.1.1", ip)
	assert.Equal(t, 0, device)
	assert.Equal(t, 254, tableNumber)

	ipv4Addr = net.IPNet{IP: net.ParseIP("1.1.1.2"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	err = ds.AddIPv4CidrToStore("eni-1", ipv4Addr, false)
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 2)
	assert.Equal(t, len(ds.eniPool["eni-1"].AvailableIPv4Cidrs), 2)

	ipv4Addr = net.IPNet{IP: net.ParseIP("1.1.1.3"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	err = ds.AddIPv4CidrToStore("eni-1", ipv4Addr, false)
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 3)
	assert.Equal(t, len(ds.eniPool["eni-1"].AvailableIPv4Cidrs), 3)

	ipv4Addr = net.IPNet{IP: net.ParseIP("1.1.1.2"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	err = ds.DelIPv4CidrFromStore("eni-1", ipv4Addr, false)
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 2)
	assert.Equal(t, len(ds.eniPool["eni-1"].AvailableIPv4Cidrs), 2)

	// delete a unknown IP
	ipv4Addr = net.IPNet{IP: net.ParseIP("10.10.10.10"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	err = ds.DelIPv4CidrFromStore("eni-1", ipv4Addr, false)
	assert.Error(t, err)
	assert.Equal(t, ds.total, 2)
	assert.Equal(t, len(ds.eniPool["eni-1"].AvailableIPv4Cidrs), 2)

	// Test force removal.  The first call fails because the IP has a pod assigned to it, but the
	// second call force-removes it and succeeds.
	ipv4Addr = net.IPNet{IP: net.ParseIP("1.1.1.1"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	err = ds.DelIPv4CidrFromStore("eni-1", ipv4Addr, false)
	assert.Error(t, err)
	assert.Equal(t, ds.total, 2)
	assert.Equal(t, len(ds.eniPool["eni-1"].AvailableIPv4Cidrs), 2)

	ipv4Addr = net.IPNet{IP: net.ParseIP("1.1.1.1"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	err = ds.DelIPv4CidrFromStore("eni-1", ipv4Addr, true)
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 1)
	assert.Equal(t, len(ds.eniPool["eni-1"].AvailableIPv4Cidrs), 1)
}

func TestDelENIIPv4AddressWithPDEnabled(t *testing.T) {
	ds := NewDataStore(Testlog, NullCheckpoint{}, true, defaultNetworkCard)
	err := ds.AddENI("eni-1", 0, true, false, false, networkutils.CalculateRouteTableId(0, 0), "")
	assert.NoError(t, err)

	ipv4Addr := net.IPNet{IP: net.ParseIP("10.0.0.0"), Mask: net.IPv4Mask(255, 255, 255, 240)}
	err = ds.AddIPv4CidrToStore("eni-1", ipv4Addr, true)
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 16)
	assert.Equal(t, len(ds.eniPool["eni-1"].AvailableIPv4Cidrs), 1)

	// Assign a pod.
	key := IPAMKey{"net0", "sandbox-1", "eth0"}
	ip, device, tableNumber, err := ds.AssignPodIPv4Address(key, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-1"})
	assert.NoError(t, err)
	assert.Equal(t, "10.0.0.0", ip)
	assert.Equal(t, 0, device)
	assert.Equal(t, 254, tableNumber)

	ipv4Addr = net.IPNet{IP: net.ParseIP("20.0.0.0"), Mask: net.IPv4Mask(255, 255, 255, 240)}
	err = ds.AddIPv4CidrToStore("eni-1", ipv4Addr, true)
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 32)
	assert.Equal(t, len(ds.eniPool["eni-1"].AvailableIPv4Cidrs), 2)

	ipv4Addr = net.IPNet{IP: net.ParseIP("30.0.0.0"), Mask: net.IPv4Mask(255, 255, 255, 240)}
	err = ds.AddIPv4CidrToStore("eni-1", ipv4Addr, true)
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 48)
	assert.Equal(t, len(ds.eniPool["eni-1"].AvailableIPv4Cidrs), 3)

	ipv4Addr = net.IPNet{IP: net.ParseIP("30.0.0.0"), Mask: net.IPv4Mask(255, 255, 255, 240)}
	err = ds.DelIPv4CidrFromStore("eni-1", ipv4Addr, false)
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 32)
	assert.Equal(t, len(ds.eniPool["eni-1"].AvailableIPv4Cidrs), 2)

	// delete a unknown IP
	ipv4Addr = net.IPNet{IP: net.ParseIP("10.10.10.10"), Mask: net.IPv4Mask(255, 255, 255, 240)}
	err = ds.DelIPv4CidrFromStore("eni-1", ipv4Addr, true)
	assert.Error(t, err)
	assert.Equal(t, ds.total, 32)
	assert.Equal(t, len(ds.eniPool["eni-1"].AvailableIPv4Cidrs), 2)

	// Test force removal.  The first call fails because the IP has a pod assigned to it, but the
	// second call force-removes it and succeeds.
	ipv4Addr = net.IPNet{IP: net.ParseIP("10.0.0.0"), Mask: net.IPv4Mask(255, 255, 255, 240)}
	err = ds.DelIPv4CidrFromStore("eni-1", ipv4Addr, false)
	assert.Error(t, err)
	assert.Equal(t, ds.total, 32)
	assert.Equal(t, len(ds.eniPool["eni-1"].AvailableIPv4Cidrs), 2)

	ipv4Addr = net.IPNet{IP: net.ParseIP("10.0.0.0"), Mask: net.IPv4Mask(255, 255, 255, 240)}
	err = ds.DelIPv4CidrFromStore("eni-1", ipv4Addr, true)
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 16)
	assert.Equal(t, len(ds.eniPool["eni-1"].AvailableIPv4Cidrs), 1)
}

func TestTogglePD(t *testing.T) {
	//DS is in secondary IP mode
	ds := NewDataStore(Testlog, NullCheckpoint{}, false, defaultNetworkCard)

	enis := []struct {
		id        string
		device    int
		isPrimary bool
	}{
		{"eni-1", 0, true},
	}

	for _, eni := range enis {
		err := ds.AddENI(eni.id, eni.device, eni.isPrimary, false, false, networkutils.CalculateRouteTableId(eni.device, 0), "")
		assert.NoError(t, err)
	}

	// Add /32 secondary IP
	ipv4Addr := net.IPNet{IP: net.ParseIP("1.1.1.1"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	err := ds.AddIPv4CidrToStore("eni-1", ipv4Addr, false)
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 1)
	assert.Equal(t, len(ds.eniPool["eni-1"].AvailableIPv4Cidrs), 1)

	// Assign a pod.
	key := IPAMKey{"net0", "sandbox-1", "eth0"}
	ip, device, tableNumber, err := ds.AssignPodIPv4Address(key, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-1"})
	assert.NoError(t, err)
	assert.Equal(t, "1.1.1.1", ip)
	assert.Equal(t, 0, device)
	assert.Equal(t, 254, tableNumber)

	//enable pd mode
	ds.isPDEnabled = true

	// Add a /28 prefix to the same eni
	ipv4Addr = net.IPNet{IP: net.ParseIP("10.0.0.0"), Mask: net.IPv4Mask(255, 255, 255, 240)}
	err = ds.AddIPv4CidrToStore("eni-1", ipv4Addr, true)
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 17)
	assert.Equal(t, len(ds.eniPool["eni-1"].AvailableIPv4Cidrs), 2)

	//Assign a pod
	key = IPAMKey{"net0", "sandbox-2", "eth0"}
	ip, device, tableNumber, err = ds.AssignPodIPv4Address(key, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-2"})
	assert.NoError(t, err)
	assert.Equal(t, "10.0.0.0", ip)
	assert.Equal(t, 0, device)
	assert.Equal(t, 254, tableNumber)

	//Pod deletion simulated with force delete
	//Test force removal.  The first call fails because the IP has a pod assigned to it, but the
	//second call force-removes it and succeeds.
	ipv4Addr = net.IPNet{IP: net.ParseIP("1.1.1.1"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	err = ds.DelIPv4CidrFromStore("eni-1", ipv4Addr, false)
	assert.Error(t, err)
	assert.Equal(t, ds.total, 17)
	assert.Equal(t, len(ds.eniPool["eni-1"].AvailableIPv4Cidrs), 2)

	ipv4Addr = net.IPNet{IP: net.ParseIP("1.1.1.1"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	err = ds.DelIPv4CidrFromStore("eni-1", ipv4Addr, true)
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 16)
	assert.Equal(t, len(ds.eniPool["eni-1"].AvailableIPv4Cidrs), 1)

	//disable pd mode
	ds.isPDEnabled = false

	//Add /32 secondary IP
	ipv4Addr = net.IPNet{IP: net.ParseIP("1.1.1.1"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	err = ds.AddIPv4CidrToStore("eni-1", ipv4Addr, false)
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 17)
	assert.Equal(t, len(ds.eniPool["eni-1"].AvailableIPv4Cidrs), 2)

	//Pod deletion simulated with force delete
	//Test force removal.  The first call fails because the IP has a pod assigned to it, but the
	//second call force-removes it and succeeds.
	ipv4Addr = net.IPNet{IP: net.ParseIP("10.0.0.0"), Mask: net.IPv4Mask(255, 255, 255, 240)}
	err = ds.DelIPv4CidrFromStore("eni-1", ipv4Addr, false)
	assert.Error(t, err)
	assert.Equal(t, ds.total, 17)
	assert.Equal(t, len(ds.eniPool["eni-1"].AvailableIPv4Cidrs), 2)

	ipv4Addr = net.IPNet{IP: net.ParseIP("10.0.0.0"), Mask: net.IPv4Mask(255, 255, 255, 240)}
	err = ds.DelIPv4CidrFromStore("eni-1", ipv4Addr, true)
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 1)
	assert.Equal(t, len(ds.eniPool["eni-1"].AvailableIPv4Cidrs), 1)

}

func TestPodIPv4Address(t *testing.T) {
	checkpoint := NewTestCheckpoint(struct{}{})
	ds := NewDataStore(Testlog, checkpoint, false, defaultNetworkCard)

	checkpointDataCmpOpts := cmp.Options{
		cmpopts.IgnoreFields(CheckpointEntry{}, "AllocationTimestamp"),
		cmpopts.SortSlices(func(lhs CheckpointEntry, rhs CheckpointEntry) bool {
			return lhs.ContainerID < rhs.ContainerID
		}),
	}
	enis := []struct {
		id        string
		device    int
		isPrimary bool
	}{
		{"eni-1", 0, true},
		{"eni-2", 1, false},
	}

	for _, eni := range enis {
		err := ds.AddENI(eni.id, eni.device, eni.isPrimary, false, false, networkutils.CalculateRouteTableId(eni.device, 0), "")
		assert.NoError(t, err)
	}

	ipv4Addr1 := net.IPNet{IP: net.ParseIP("1.1.1.1"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	err := ds.AddIPv4CidrToStore("eni-1", ipv4Addr1, false)
	assert.NoError(t, err)

	key1 := IPAMKey{"net0", "sandbox-1", "eth0"}
	ip, _, _, err := ds.AssignPodIPv4Address(key1, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-1"})

	assert.NoError(t, err)
	assert.Equal(t, "1.1.1.1", ip)
	assert.Equal(t, 1, ds.total)
	assert.Equal(t, 1, len(ds.eniPool["eni-1"].AvailableIPv4Cidrs))
	assert.Equal(t, 1, ds.eniPool["eni-1"].AssignedIPv4Addresses())

	expectedCheckpointData := &CheckpointData{
		Version: CheckpointFormatVersion,
		Allocations: []CheckpointEntry{
			{
				IPAMKey:  IPAMKey{NetworkName: "net0", ContainerID: "sandbox-1", IfName: "eth0"},
				IPv4:     "1.1.1.1",
				Metadata: IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-1"},
			},
		},
	}
	assert.True(t,
		cmp.Equal(checkpoint.Data, expectedCheckpointData, checkpointDataCmpOpts),
		cmp.Diff(checkpoint.Data, expectedCheckpointData, checkpointDataCmpOpts),
	)

	podsInfos := ds.AllocatedIPs()
	assert.Equal(t, len(podsInfos), 1)

	ipv4Addr2 := net.IPNet{IP: net.ParseIP("1.1.2.2"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	err = ds.AddIPv4CidrToStore("eni-2", ipv4Addr2, false)
	assert.NoError(t, err)

	// duplicate add
	ip, _, _, err = ds.AssignPodIPv4Address(key1, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-1"}) // same id
	assert.NoError(t, err)
	assert.Equal(t, ip, "1.1.1.1")
	assert.Equal(t, ds.total, 2)
	assert.Equal(t, ds.assigned, 1)
	assert.Equal(t, len(ds.eniPool["eni-1"].AvailableIPv4Cidrs), 1)
	assert.Equal(t, ds.eniPool["eni-1"].AssignedIPv4Addresses(), 1)
	assert.Equal(t, len(ds.eniPool["eni-2"].AvailableIPv4Cidrs), 1)
	assert.Equal(t, ds.eniPool["eni-2"].AssignedIPv4Addresses(), 0)

	// Checkpoint error
	checkpoint.Error = errors.New("fake checkpoint error")
	key2 := IPAMKey{"net0", "sandbox-2", "eth0"}
	_, _, _, err = ds.AssignPodIPv4Address(key2, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-2"})
	assert.Error(t, err)

	expectedCheckpointData = &CheckpointData{
		Version: CheckpointFormatVersion,
		Allocations: []CheckpointEntry{
			{
				IPAMKey:  IPAMKey{NetworkName: "net0", ContainerID: "sandbox-1", IfName: "eth0"},
				IPv4:     "1.1.1.1",
				Metadata: IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-1"},
			},
		},
	}
	assert.True(t,
		cmp.Equal(checkpoint.Data, expectedCheckpointData, checkpointDataCmpOpts),
		cmp.Diff(checkpoint.Data, expectedCheckpointData, checkpointDataCmpOpts),
	)
	checkpoint.Error = nil

	ip, pod1Ns2Device, pod1Ns2TableNumber, err := ds.AssignPodIPv4Address(key2, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-2"})
	assert.NoError(t, err)
	assert.Equal(t, ip, "1.1.2.2")
	assert.Equal(t, ds.total, 2)
	assert.Equal(t, ds.assigned, 2)
	assert.Equal(t, len(ds.eniPool["eni-2"].AvailableIPv4Cidrs), 1)
	assert.Equal(t, ds.eniPool["eni-2"].AssignedIPv4Addresses(), 1)

	expectedCheckpointData = &CheckpointData{
		Version: CheckpointFormatVersion,
		Allocations: []CheckpointEntry{
			{
				IPAMKey:  IPAMKey{NetworkName: "net0", ContainerID: "sandbox-1", IfName: "eth0"},
				IPv4:     "1.1.1.1",
				Metadata: IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-1"},
			},
			{
				IPAMKey:  IPAMKey{NetworkName: "net0", ContainerID: "sandbox-2", IfName: "eth0"},
				IPv4:     "1.1.2.2",
				Metadata: IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-2"},
			},
		},
	}
	assert.True(t,
		cmp.Equal(checkpoint.Data, expectedCheckpointData, checkpointDataCmpOpts),
		cmp.Diff(checkpoint.Data, expectedCheckpointData, checkpointDataCmpOpts),
	)

	podsInfos = ds.AllocatedIPs()
	assert.Equal(t, len(podsInfos), 2)

	ipv4Addr3 := net.IPNet{IP: net.ParseIP("1.1.1.2"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	err = ds.AddIPv4CidrToStore("eni-1", ipv4Addr3, false)
	assert.NoError(t, err)

	key3 := IPAMKey{"net0", "sandbox-3", "eth0"}
	ip, _, _, err = ds.AssignPodIPv4Address(key3, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-3"})
	assert.NoError(t, err)
	assert.Equal(t, ip, "1.1.1.2")
	assert.Equal(t, ds.total, 3)
	assert.Equal(t, ds.assigned, 3)
	assert.Equal(t, len(ds.eniPool["eni-1"].AvailableIPv4Cidrs), 2)
	assert.Equal(t, ds.eniPool["eni-1"].AssignedIPv4Addresses(), 2)
	expectedCheckpointData = &CheckpointData{
		Version: CheckpointFormatVersion,
		Allocations: []CheckpointEntry{
			{
				IPAMKey:  IPAMKey{NetworkName: "net0", ContainerID: "sandbox-1", IfName: "eth0"},
				IPv4:     "1.1.1.1",
				Metadata: IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-1"},
			},
			{
				IPAMKey:  IPAMKey{NetworkName: "net0", ContainerID: "sandbox-2", IfName: "eth0"},
				IPv4:     "1.1.2.2",
				Metadata: IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-2"},
			},
			{
				IPAMKey:  IPAMKey{NetworkName: "net0", ContainerID: "sandbox-3", IfName: "eth0"},
				IPv4:     "1.1.1.2",
				Metadata: IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-3"},
			},
		},
	}
	assert.True(t,
		cmp.Equal(checkpoint.Data, expectedCheckpointData, checkpointDataCmpOpts),
		cmp.Diff(checkpoint.Data, expectedCheckpointData, checkpointDataCmpOpts),
	)

	// no more IP addresses
	key4 := IPAMKey{"net0", "sandbox-4", "eth0"}
	_, _, _, err = ds.AssignPodIPv4Address(key4, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-4"})
	assert.Error(t, err)
	// Unassign unknown Pod
	_, _, _, _, _, err = ds.UnassignPodIPAddress(key4)
	assert.Error(t, err)

	_, _, deviceNum, interfaces, tableNumber, err := ds.UnassignPodIPAddress(key2)
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 3)
	assert.Equal(t, ds.assigned, 2)
	assert.Equal(t, deviceNum, pod1Ns2Device)
	assert.Equal(t, tableNumber, pod1Ns2TableNumber)
	assert.Equal(t, interfaces, 1)
	assert.Equal(t, len(ds.eniPool["eni-2"].AvailableIPv4Cidrs), 1)
	assert.Equal(t, ds.eniPool["eni-2"].AssignedIPv4Addresses(), 0)
	expectedCheckpointData = &CheckpointData{
		Version: CheckpointFormatVersion,
		Allocations: []CheckpointEntry{
			{
				IPAMKey:  IPAMKey{NetworkName: "net0", ContainerID: "sandbox-1", IfName: "eth0"},
				IPv4:     "1.1.1.1",
				Metadata: IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-1"},
			},
			{
				IPAMKey:  IPAMKey{NetworkName: "net0", ContainerID: "sandbox-3", IfName: "eth0"},
				IPv4:     "1.1.1.2",
				Metadata: IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-3"},
			},
		},
	}
	assert.True(t,
		cmp.Equal(checkpoint.Data, expectedCheckpointData, checkpointDataCmpOpts),
		cmp.Diff(checkpoint.Data, expectedCheckpointData, checkpointDataCmpOpts),
	)

	noWarmIPTarget := 0
	noMinimumIPTarget := 0
	noWarmPrefixTarget := 0

	// Should not be able to free this ENI
	eni := ds.RemoveUnusedENIFromStore(noWarmIPTarget, noMinimumIPTarget, noWarmPrefixTarget)
	assert.True(t, eni == "")

	ds.eniPool["eni-2"].createTime = time.Time{}
	ds.eniPool["eni-2"].AvailableIPv4Cidrs[ipv4Addr2.String()].IPAddresses["1.1.2.2"].UnassignedTime = time.Time{}
	eni = ds.RemoveUnusedENIFromStore(noWarmIPTarget, noMinimumIPTarget, noWarmPrefixTarget)
	assert.Equal(t, eni, "eni-2")

	assert.Equal(t, ds.total, 2)
	assert.Equal(t, ds.assigned, 2)
}

func TestPodIPv4AddressWithPDEnabled(t *testing.T) {
	checkpoint := NewTestCheckpoint(struct{}{})
	ds := NewDataStore(Testlog, checkpoint, true, defaultNetworkCard)

	checkpointDataCmpOpts := cmp.Options{
		cmpopts.IgnoreFields(CheckpointEntry{}, "AllocationTimestamp"),
		cmpopts.SortSlices(func(lhs CheckpointEntry, rhs CheckpointEntry) bool {
			return lhs.ContainerID < rhs.ContainerID
		}),
	}
	enis := []struct {
		id        string
		device    int
		isPrimary bool
	}{
		{"eni-1", 0, true},
		{"eni-2", 2, false},
		{"eni-3", 3, false},
	}

	for _, eni := range enis {
		err := ds.AddENI(eni.id, eni.device, eni.isPrimary, false, false, networkutils.CalculateRouteTableId(eni.device, 0), "")
		assert.NoError(t, err)

	}

	ipv4Addr1 := net.IPNet{IP: net.ParseIP("10.0.0.0"), Mask: net.IPv4Mask(255, 255, 255, 240)}
	err := ds.AddIPv4CidrToStore("eni-1", ipv4Addr1, true)
	assert.NoError(t, err)

	key1 := IPAMKey{"net0", "sandbox-1", "eth0"}
	ip, _, _, err := ds.AssignPodIPv4Address(key1, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-1"})

	assert.NoError(t, err)
	assert.Equal(t, "10.0.0.0", ip)
	assert.Equal(t, 16, ds.total)
	assert.Equal(t, 1, len(ds.eniPool["eni-1"].AvailableIPv4Cidrs))
	assert.Equal(t, 1, ds.eniPool["eni-1"].AssignedIPv4Addresses())

	expectedCheckpointData := &CheckpointData{
		Version: CheckpointFormatVersion,
		Allocations: []CheckpointEntry{
			{
				IPAMKey:  IPAMKey{NetworkName: "net0", ContainerID: "sandbox-1", IfName: "eth0"},
				IPv4:     "10.0.0.0",
				Metadata: IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-1"},
			},
		},
	}
	assert.True(t,
		cmp.Equal(checkpoint.Data, expectedCheckpointData, checkpointDataCmpOpts),
		cmp.Diff(checkpoint.Data, expectedCheckpointData, checkpointDataCmpOpts),
	)

	podsInfos := ds.AllocatedIPs()
	assert.Equal(t, len(podsInfos), 1)

	// duplicate add
	ip, _, _, err = ds.AssignPodIPv4Address(key1, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-1"}) // same id
	assert.NoError(t, err)
	assert.Equal(t, ip, "10.0.0.0")
	assert.Equal(t, ds.total, 16)
	assert.Equal(t, ds.assigned, 1)
	assert.Equal(t, len(ds.eniPool["eni-1"].AvailableIPv4Cidrs), 1)
	assert.Equal(t, ds.eniPool["eni-1"].AssignedIPv4Addresses(), 1)

	// Checkpoint error
	checkpoint.Error = errors.New("fake checkpoint error")
	key2 := IPAMKey{"net0", "sandbox-2", "eth0"}
	_, _, _, err = ds.AssignPodIPv4Address(key2, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-2"})
	assert.Error(t, err)

	expectedCheckpointData = &CheckpointData{
		Version: CheckpointFormatVersion,
		Allocations: []CheckpointEntry{
			{
				IPAMKey:  IPAMKey{NetworkName: "net0", ContainerID: "sandbox-1", IfName: "eth0"},
				IPv4:     "10.0.0.0",
				Metadata: IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-1"},
			},
		},
	}
	assert.True(t,
		cmp.Equal(checkpoint.Data, expectedCheckpointData, checkpointDataCmpOpts),
		cmp.Diff(checkpoint.Data, expectedCheckpointData, checkpointDataCmpOpts),
	)
	checkpoint.Error = nil

	ip, pod1Ns2Device, pod1NS2RouteTableNumber, err := ds.AssignPodIPv4Address(key2, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-2"})
	assert.NoError(t, err)
	assert.Equal(t, ip, "10.0.0.1")
	assert.Equal(t, ds.total, 16)
	assert.Equal(t, ds.assigned, 2)
	assert.Equal(t, len(ds.eniPool["eni-1"].AvailableIPv4Cidrs), 1)
	assert.Equal(t, ds.eniPool["eni-1"].AssignedIPv4Addresses(), 2)

	expectedCheckpointData = &CheckpointData{
		Version: CheckpointFormatVersion,
		Allocations: []CheckpointEntry{
			{
				IPAMKey:  IPAMKey{NetworkName: "net0", ContainerID: "sandbox-1", IfName: "eth0"},
				IPv4:     "10.0.0.0",
				Metadata: IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-1"},
			},
			{
				IPAMKey:  IPAMKey{NetworkName: "net0", ContainerID: "sandbox-2", IfName: "eth0"},
				IPv4:     "10.0.0.1",
				Metadata: IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-2"},
			},
		},
	}
	assert.True(t,
		cmp.Equal(checkpoint.Data, expectedCheckpointData, checkpointDataCmpOpts),
		cmp.Diff(checkpoint.Data, expectedCheckpointData, checkpointDataCmpOpts),
	)

	podsInfos = ds.AllocatedIPs()
	assert.Equal(t, len(podsInfos), 2)

	key3 := IPAMKey{"net0", "sandbox-3", "eth0"}
	ip, _, _, err = ds.AssignPodIPv4Address(key3, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-3"})
	assert.NoError(t, err)
	assert.Equal(t, ip, "10.0.0.2")
	assert.Equal(t, ds.total, 16)
	assert.Equal(t, ds.assigned, 3)
	assert.Equal(t, len(ds.eniPool["eni-1"].AvailableIPv4Cidrs), 1)
	assert.Equal(t, ds.eniPool["eni-1"].AssignedIPv4Addresses(), 3)
	expectedCheckpointData = &CheckpointData{
		Version: CheckpointFormatVersion,
		Allocations: []CheckpointEntry{
			{
				IPAMKey:  IPAMKey{NetworkName: "net0", ContainerID: "sandbox-1", IfName: "eth0"},
				IPv4:     "10.0.0.0",
				Metadata: IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-1"},
			},
			{
				IPAMKey:  IPAMKey{NetworkName: "net0", ContainerID: "sandbox-2", IfName: "eth0"},
				IPv4:     "10.0.0.1",
				Metadata: IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-2"},
			},
			{
				IPAMKey:  IPAMKey{NetworkName: "net0", ContainerID: "sandbox-3", IfName: "eth0"},
				IPv4:     "10.0.0.2",
				Metadata: IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-3"},
			},
		},
	}
	assert.True(t,
		cmp.Equal(checkpoint.Data, expectedCheckpointData, checkpointDataCmpOpts),
		cmp.Diff(checkpoint.Data, expectedCheckpointData, checkpointDataCmpOpts),
	)

	_, _, deviceNum, interfaces, tableNumber, err := ds.UnassignPodIPAddress(key2)
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 16)
	assert.Equal(t, ds.assigned, 2)
	assert.Equal(t, interfaces, 1)
	assert.Equal(t, deviceNum, pod1Ns2Device)
	assert.Equal(t, tableNumber, pod1NS2RouteTableNumber)
	assert.Equal(t, len(ds.eniPool["eni-1"].AvailableIPv4Cidrs), 1)
	assert.Equal(t, ds.eniPool["eni-1"].AssignedIPv4Addresses(), 2)
	expectedCheckpointData = &CheckpointData{
		Version: CheckpointFormatVersion,
		Allocations: []CheckpointEntry{
			{
				IPAMKey:  IPAMKey{NetworkName: "net0", ContainerID: "sandbox-1", IfName: "eth0"},
				IPv4:     "10.0.0.0",
				Metadata: IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-1"},
			},
			{
				IPAMKey:  IPAMKey{NetworkName: "net0", ContainerID: "sandbox-3", IfName: "eth0"},
				IPv4:     "10.0.0.2",
				Metadata: IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-3"},
			},
		},
	}
	assert.True(t,
		cmp.Equal(checkpoint.Data, expectedCheckpointData, checkpointDataCmpOpts),
		cmp.Diff(checkpoint.Data, expectedCheckpointData, checkpointDataCmpOpts),
	)

	assert.Equal(t, ds.total, 16)
	assert.Equal(t, ds.assigned, 2)
}

func TestGetIPStatsV4(t *testing.T) {
	os.Setenv(envIPCooldownPeriod, "1")
	defer os.Unsetenv(envIPCooldownPeriod)
	ds := NewDataStore(Testlog, NullCheckpoint{}, false, defaultNetworkCard)

	_ = ds.AddENI("eni-1", 1, true, false, false, networkutils.CalculateRouteTableId(1, 0), "")

	ipv4Addr := net.IPNet{IP: net.ParseIP("1.1.1.1"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	_ = ds.AddIPv4CidrToStore("eni-1", ipv4Addr, false)
	key1 := IPAMKey{"net0", "sandbox-1", "eth0"}
	_, _, _, err := ds.AssignPodIPv4Address(key1, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-1"})
	assert.NoError(t, err)

	ipv4Addr = net.IPNet{IP: net.ParseIP("1.1.1.2"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	_ = ds.AddIPv4CidrToStore("eni-1", ipv4Addr, false)
	key2 := IPAMKey{"net0", "sandbox-2", "eth0"}
	_, _, _, err = ds.AssignPodIPv4Address(key2, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-2"})
	assert.NoError(t, err)

	assert.Equal(t,
		DataStoreStats{
			TotalIPs:    2,
			AssignedIPs: 2,
			CooldownIPs: 0,
		},
		*ds.GetIPStats("4"),
	)

	_, _, _, _, _, err = ds.UnassignPodIPAddress(key2)
	assert.NoError(t, err)

	assert.Equal(t,
		DataStoreStats{
			TotalIPs:    2,
			AssignedIPs: 1,
			CooldownIPs: 1,
		},
		*ds.GetIPStats("4"),
	)

	assert.Equal(t, ds.ipCooldownPeriod, 1*time.Second)
	// wait 1s (cooldown period)
	time.Sleep(ds.ipCooldownPeriod)

	assert.Equal(t,
		DataStoreStats{
			TotalIPs:    2,
			AssignedIPs: 1,
			CooldownIPs: 0,
		},
		*ds.GetIPStats("4"),
	)
}

func TestGetIPStatsV4WithPD(t *testing.T) {
	os.Setenv(envIPCooldownPeriod, "1")
	defer os.Unsetenv(envIPCooldownPeriod)
	ds := NewDataStore(Testlog, NullCheckpoint{}, true, defaultNetworkCard)

	_ = ds.AddENI("eni-1", 1, true, false, false, networkutils.CalculateRouteTableId(1, 0), "")

	ipv4Addr := net.IPNet{IP: net.ParseIP("10.0.0.0"), Mask: net.IPv4Mask(255, 255, 255, 240)}
	_ = ds.AddIPv4CidrToStore("eni-1", ipv4Addr, true)
	key1 := IPAMKey{"net0", "sandbox-1", "eth0"}
	_, _, _, err := ds.AssignPodIPv4Address(key1, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-1"})
	assert.NoError(t, err)

	key2 := IPAMKey{"net0", "sandbox-2", "eth0"}
	_, _, _, err = ds.AssignPodIPv4Address(key2, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-2"})
	assert.NoError(t, err)

	assert.Equal(t,
		DataStoreStats{
			TotalIPs:      16,
			TotalPrefixes: 1,
			AssignedIPs:   2,
			CooldownIPs:   0,
		},
		*ds.GetIPStats("4"),
	)

	_, _, _, _, _, err = ds.UnassignPodIPAddress(key2)
	assert.NoError(t, err)

	assert.Equal(t,
		DataStoreStats{
			TotalIPs:      16,
			TotalPrefixes: 1,
			AssignedIPs:   1,
			CooldownIPs:   1,
		},
		*ds.GetIPStats("4"),
	)

	assert.Equal(t, ds.ipCooldownPeriod, 1*time.Second)
	// wait 1s (cooldown period)
	time.Sleep(ds.ipCooldownPeriod)

	assert.Equal(t,
		DataStoreStats{
			TotalIPs:      16,
			TotalPrefixes: 1,
			AssignedIPs:   1,
			CooldownIPs:   0,
		},
		*ds.GetIPStats("4"),
	)
}

func TestGetIPStatsV6(t *testing.T) {
	v6ds := NewDataStore(Testlog, NullCheckpoint{}, true, defaultNetworkCard)
	_ = v6ds.AddENI("eni-1", 1, true, false, false, networkutils.CalculateRouteTableId(1, 0), "")
	ipv6Addr := net.IPNet{IP: net.IP{0x21, 0xdb, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, Mask: net.CIDRMask(80, 128)}
	_ = v6ds.AddIPv6CidrToStore("eni-1", ipv6Addr, true)
	key3 := IPAMKey{"netv6", "sandbox-3", "eth0"}
	_, _, _, err := v6ds.AssignPodIPv6Address(key3, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-3"})
	assert.NoError(t, err)

	assert.Equal(t,
		DataStoreStats{
			TotalIPs:      281474976710656,
			TotalPrefixes: 1,
			AssignedIPs:   1,
			CooldownIPs:   0,
		},
		*v6ds.GetIPStats("6"),
	)
}

func TestAssignedIPv6Addresses(t *testing.T) {
	ds := NewDataStore(Testlog, NullCheckpoint{}, true, defaultNetworkCard)

	// Test ENI with no IPv6 CIDRs
	_ = ds.AddENI("eni-1", 1, true, false, false, 0, "")
	eniInfo, _ := ds.eniPool["eni-1"]
	assert.Equal(t, 0, eniInfo.AssignedIPv6Addresses(), "ENI with no IPv6 CIDRs should have 0 assigned addresses")

	// Add IPv6 CIDR but don't assign any addresses yet
	ipv6Addr1 := net.IPNet{IP: net.ParseIP("2001:db8::"), Mask: net.CIDRMask(80, 128)}
	_ = ds.AddIPv6CidrToStore("eni-1", ipv6Addr1, true)
	assert.Equal(t, 0, eniInfo.AssignedIPv6Addresses(), "ENI with unassigned IPv6 CIDR should have 0 assigned addresses")

	// Assign first IPv6 address to a pod
	key1 := IPAMKey{"net0", "sandbox-1", "eth0"}
	_, _, _, err := ds.AssignPodIPv6Address(key1, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "pod-1"})
	assert.NoError(t, err)
	assert.Equal(t, 1, eniInfo.AssignedIPv6Addresses(), "Should have 1 assigned IPv6 address after first pod assignment")

	// Assign second IPv6 address to another pod
	key2 := IPAMKey{"net0", "sandbox-2", "eth0"}
	_, _, _, err = ds.AssignPodIPv6Address(key2, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "pod-2"})
	assert.NoError(t, err)
	assert.Equal(t, 2, eniInfo.AssignedIPv6Addresses(), "Should have 2 assigned IPv6 addresses after second pod assignment")

	// Add another IPv6 CIDR to the same ENI
	ipv6Addr2 := net.IPNet{IP: net.ParseIP("2001:db8:1::"), Mask: net.CIDRMask(80, 128)}
	_ = ds.AddIPv6CidrToStore("eni-1", ipv6Addr2, true)
	assert.Equal(t, 2, eniInfo.AssignedIPv6Addresses(), "Adding new CIDR should not change assigned count")

	// Assign address from the second CIDR
	key3 := IPAMKey{"net0", "sandbox-3", "eth0"}
	_, _, _, err = ds.AssignPodIPv6Address(key3, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "pod-3"})
	assert.NoError(t, err)
	assert.Equal(t, 3, eniInfo.AssignedIPv6Addresses(), "Should have 3 assigned IPv6 addresses across multiple CIDRs")

	// Unassign one address
	_, _, _, _, _, err = ds.UnassignPodIPAddress(key1)
	assert.NoError(t, err)
	assert.Equal(t, 2, eniInfo.AssignedIPv6Addresses(), "Should have 2 assigned IPv6 addresses after unassignment")

	// Test ENI with multiple CIDRs and various assignment states
	// In IPv6 PD mode, only primary ENI can have IPv6 addresses assigned, so add CIDRs to eni-1 (primary)
	// Add more IPv6 CIDRs to eni-1 for additional testing
	for i := 0; i < 3; i++ {
		cidr := net.IPNet{IP: net.ParseIP(fmt.Sprintf("2001:db8:%d::", i+10)), Mask: net.CIDRMask(80, 128)}
		_ = ds.AddIPv6CidrToStore("eni-1", cidr, true)
	}
	assert.Equal(t, 2, eniInfo.AssignedIPv6Addresses(), "Should maintain existing assignments after adding new CIDRs")

	// Assign additional addresses from different CIDRs - they will all go to eni-1 (primary) in IPv6 PD mode
	for i := 0; i < 3; i++ {
		key := IPAMKey{"net0", fmt.Sprintf("sandbox-eni1-%d", i), "eth0"}
		_, _, _, err = ds.AssignPodIPv6Address(key, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: fmt.Sprintf("pod-eni1-%d", i)})
		assert.NoError(t, err)
	}
	assert.Equal(t, 5, eniInfo.AssignedIPv6Addresses(), "Should correctly count assigned addresses across multiple CIDRs on primary ENI")
}

func TestWarmENIInteractions(t *testing.T) {
	ds := NewDataStore(Testlog, NullCheckpoint{}, false, defaultNetworkCard)

	_ = ds.AddENI("eni-1", 1, true, false, false, networkutils.CalculateRouteTableId(1, 0), "")
	_ = ds.AddENI("eni-2", 2, false, false, false, networkutils.CalculateRouteTableId(2, 0), "")
	_ = ds.AddENI("eni-3", 3, false, false, false, networkutils.CalculateRouteTableId(3, 0), "")

	// Add an IP address to ENI 1 and assign it to a pod
	ipv4Addr := net.IPNet{IP: net.ParseIP("1.1.1.1"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	_ = ds.AddIPv4CidrToStore("eni-1", ipv4Addr, false)
	key1 := IPAMKey{"net0", "sandbox-1", "eth0"}
	_, _, _, err := ds.AssignPodIPv4Address(key1, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-1"})
	assert.NoError(t, err)

	// Add another IP address to ENI 1 and assign a pod
	ipv4Addr = net.IPNet{IP: net.ParseIP("1.1.1.2"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	_ = ds.AddIPv4CidrToStore("eni-1", ipv4Addr, false)
	key2 := IPAMKey{"net0", "sandbox-2", "eth0"}
	_, _, _, err = ds.AssignPodIPv4Address(key2, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-2"})
	assert.NoError(t, err)

	// Add two IP addresses to ENI 2 and one IP address to ENI 3
	ipv4Addr = net.IPNet{IP: net.ParseIP("1.1.2.1"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	_ = ds.AddIPv4CidrToStore("eni-2", ipv4Addr, false)
	ipv4Addr = net.IPNet{IP: net.ParseIP("1.1.2.2"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	_ = ds.AddIPv4CidrToStore("eni-2", ipv4Addr, false)
	ipv4Addr = net.IPNet{IP: net.ParseIP("1.1.3.1"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	_ = ds.AddIPv4CidrToStore("eni-3", ipv4Addr, false)

	ds.eniPool["eni-2"].createTime = time.Time{}
	ds.eniPool["eni-3"].createTime = time.Time{}

	// We have 3 ENIs, 5 IPs and 2 pods on ENI 1.
	// ENI 1: 2 IPs allocated, 2 IPs in use
	// ENI 2: 2 IPs allocated, 0 IPs in use
	// ENI 3: 1 IP allocated, 0 IPs in use
	// => 3 free IPs
	// We should not be able to remove any ENIs if either warmIPTarget >= 3 or minimumWarmIPTarget >= 5

	// WARM IP TARGET=3, MINIMUM_IP_TARGET=1 => no ENI should be removed
	eni := ds.RemoveUnusedENIFromStore(3, 1, 0)
	assert.Equal(t, "", eni)

	// WARM IP TARGET=1, MINIMUM_IP_TARGET=5 => no ENI should be removed
	eni = ds.RemoveUnusedENIFromStore(1, 5, 0)
	assert.Equal(t, "", eni)

	// WARM IP TARGET=2, MINIMUM_IP_TARGET=4 => ENI 3 should be removed as we only need 2 free IPs, which ENI 2 has
	removedEni := ds.RemoveUnusedENIFromStore(2, 4, 0)
	assert.Equal(t, "eni-3", removedEni)

	// We have 2 ENIs, 4 IPs and 2 pods on ENI 1.
	// ENI 1: 2 IPs allocated, 2 IPs in use
	// ENI 2: 2 IPs allocated, 0 IPs in use
	// => 2 free IPs

	// WARM IP TARGET=0, MINIMUM_IP_TARGET=3 => no ENI should be removed
	eni = ds.RemoveUnusedENIFromStore(0, 3, 0)
	assert.Equal(t, "", eni)

	// WARM IP TARGET=0, MINIMUM_IP_TARGET=2 => ENI 2 should be removed as ENI 1 covers the requirements
	removedEni = ds.RemoveUnusedENIFromStore(0, 2, 0)
	assert.Contains(t, "eni-2", removedEni)

	// Add 2 more ENIs to the datastore and add 1 IP address to each of them
	ds.AddENI("eni-4", 4, false, true, false, networkutils.CalculateRouteTableId(4, 0), "") // trunk ENI
	ds.AddENI("eni-5", 5, false, false, true, networkutils.CalculateRouteTableId(5, 0), "") // EFA ENI
	ipv4Addr = net.IPNet{IP: net.ParseIP("1.1.4.1"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	ds.AddIPv4CidrToStore("eni-4", ipv4Addr, false)
	ipv4Addr = net.IPNet{IP: net.ParseIP("1.1.5.1"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	ds.AddIPv4CidrToStore("eni-5", ipv4Addr, false)
	ds.eniPool["eni-4"].createTime = time.Time{}
	ds.eniPool["eni-5"].createTime = time.Time{}

	// We have 3 ENIs, 4 IPs and 2 pods on ENI 1.
	// ENI 1: 2 IPs allocated, 2 IPs in use
	// ENI 4: 1 IPs allocated, 0 IPs in use
	// ENI 5: 1 IPs allocated, 0 IPs in use
	// => 2 free IPs

	// WARM IP TARGET=0, MINIMUM_IP_TARGET=2 => no ENI can be removed because ENI 4 is a trunk ENI and ENI 5 is an EFA ENI
	removedEni = ds.RemoveUnusedENIFromStore(0, 2, 0)
	assert.Equal(t, "", removedEni)
	assert.Equal(t, 3, ds.GetENIs())

	// Add 1 more normal ENI to the datastore
	ds.AddENI("eni-6", 6, false, false, false, networkutils.CalculateRouteTableId(6, 0), "") // trunk ENI
	ds.eniPool["eni-6"].createTime = time.Time{}

	// We have 4 ENIs, 4 IPs and 2 pods on ENI 1.
	// ENI 1: 2 IPs allocated, 2 IPs in use
	// ENI 4: 1 IPs allocated, 0 IPs in use
	// ENI 5: 1 IPs allocated, 0 IPs in use
	// ENI 6: 0 IPs allocated, 0 IPs in use
	// => 2 free IPs

	// WARM IP TARGET=0, MINIMUM_IP_TARGET=2 => ENI 6 can be removed
	removedEni = ds.RemoveUnusedENIFromStore(0, 2, 0)
	assert.Equal(t, "eni-6", removedEni)
	assert.Equal(t, 3, ds.GetENIs())
}

func TestDataStore_normalizeCheckpointDataByPodVethExistence(t *testing.T) {
	containerAddr := &net.IPNet{
		IP:   net.ParseIP("192.168.1.1"),
		Mask: net.CIDRMask(32, 32),
	}

	toContainerRule := netlink.NewRule()
	toContainerRule.Dst = containerAddr
	toContainerRule.Priority = networkutils.ToContainerRulePriority
	toContainerRule.Table = unix.RT_TABLE_MAIN

	fromContainerRule := netlink.NewRule()
	fromContainerRule.Src = containerAddr
	fromContainerRule.Priority = networkutils.FromPodRulePriority
	fromContainerRule.Table = unix.RT_TABLE_UNSPEC
	type netLinkListCall struct {
		links []netlink.Link
		err   error
	}
	type fields struct {
		netLinkListCalls       []netLinkListCall
		staleToContainerRule   *netlink.Rule
		staleFromContainerRule *netlink.Rule
	}
	type args struct {
		checkpoint CheckpointData
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    CheckpointData
		wantErr error
	}{
		{
			name: "all allocations are valid",
			fields: fields{
				netLinkListCalls: []netLinkListCall{
					{
						links: []netlink.Link{
							&netlink.Device{
								LinkAttrs: netlink.LinkAttrs{
									Name: "eth0",
								},
							},
							&netlink.Veth{
								LinkAttrs: netlink.LinkAttrs{
									Name: "enib5faff8a083",
								},
							},
							&netlink.Veth{
								LinkAttrs: netlink.LinkAttrs{
									Name: "eni9571956a6cc",
								},
							},
						},
					},
				},
			},
			args: args{
				checkpoint: CheckpointData{
					Version: CheckpointFormatVersion,
					Allocations: []CheckpointEntry{
						{
							IPAMKey: IPAMKey{
								ContainerID: "5a1f9118a7125f87b4b0f2f601c0b55cfab8bcf28963bcf7c4ece3109a8b6b86",
								NetworkName: "aws-cni",
								IfName:      "eth0",
							},
							IPv4: "192.168.9.106",
							Metadata: IPAMMetadata{
								K8SPodNamespace: "kube-system",
								K8SPodName:      "coredns-57ff979f67-qqbdh",
							},
						},
						{
							IPAMKey: IPAMKey{
								ContainerID: "b4729ce84caa9e585c2ba84fc9f9a058f4cf783292a84608f717035e09553422",
								NetworkName: "aws-cni",
								IfName:      "eth0",
							},
							IPv4: "192.168.30.161",
							Metadata: IPAMMetadata{
								K8SPodNamespace: "kube-system",
								K8SPodName:      "coredns-57ff979f67-8ns9b",
							},
						},
					},
				},
			},
			want: CheckpointData{
				Version: CheckpointFormatVersion,
				Allocations: []CheckpointEntry{
					{
						IPAMKey: IPAMKey{
							ContainerID: "5a1f9118a7125f87b4b0f2f601c0b55cfab8bcf28963bcf7c4ece3109a8b6b86",
							NetworkName: "aws-cni",
							IfName:      "eth0",
						},
						IPv4: "192.168.9.106",
						Metadata: IPAMMetadata{
							K8SPodNamespace: "kube-system",
							K8SPodName:      "coredns-57ff979f67-qqbdh",
						},
					},
					{
						IPAMKey: IPAMKey{
							ContainerID: "b4729ce84caa9e585c2ba84fc9f9a058f4cf783292a84608f717035e09553422",
							NetworkName: "aws-cni",
							IfName:      "eth0",
						},
						IPv4: "192.168.30.161",
						Metadata: IPAMMetadata{
							K8SPodNamespace: "kube-system",
							K8SPodName:      "coredns-57ff979f67-8ns9b",
						},
					},
				},
			},
		},
		{
			name: "some allocations are invalid and some allocation don't have metadata",
			fields: fields{
				netLinkListCalls: []netLinkListCall{
					{
						links: []netlink.Link{
							&netlink.Device{
								LinkAttrs: netlink.LinkAttrs{
									Name: "eth0",
								},
							},
							&netlink.Veth{
								LinkAttrs: netlink.LinkAttrs{
									Name: "enib5faff8a083",
								},
							},
							&netlink.Veth{
								LinkAttrs: netlink.LinkAttrs{
									Name: "enicc21c2d7785",
								},
							},
						},
					},
				},
				staleToContainerRule:   toContainerRule,
				staleFromContainerRule: fromContainerRule,
			},
			args: args{
				checkpoint: CheckpointData{
					Version: CheckpointFormatVersion,
					Allocations: []CheckpointEntry{
						{
							IPAMKey: IPAMKey{
								ContainerID: "5a1f9118a7125f87b4b0f2f601c0b55cfab8bcf28963bcf7c4ece3109a8b6b86",
								NetworkName: "aws-cni",
								IfName:      "eth0",
							},
							IPv4: "192.168.9.106",
							Metadata: IPAMMetadata{
								K8SPodNamespace: "kube-system",
								K8SPodName:      "coredns-57ff979f67-qqbdh",
							},
						},
						{
							IPAMKey: IPAMKey{
								ContainerID: "b4729ce84caa9e585c2ba84fc9f9a058f4cf783292a84608f717035e09553422",
								NetworkName: "aws-cni",
								IfName:      "eth0",
							},
							IPv4: "192.168.1.1",
							Metadata: IPAMMetadata{
								K8SPodNamespace: "kube-system",
								K8SPodName:      "coredns-57ff979f67-8ns9b",
							},
						},
						{
							IPAMKey: IPAMKey{
								ContainerID: "d0437ce84caa9e585c2ba84fc9f9a058f4cf783292a84608f717035e0952341",
								NetworkName: "aws-cni",
								IfName:      "eth0",
							},
							IPv4: "192.168.42.42",
						},
					},
				},
			},
			want: CheckpointData{
				Version: CheckpointFormatVersion,
				Allocations: []CheckpointEntry{
					{
						IPAMKey: IPAMKey{
							ContainerID: "5a1f9118a7125f87b4b0f2f601c0b55cfab8bcf28963bcf7c4ece3109a8b6b86",
							NetworkName: "aws-cni",
							IfName:      "eth0",
						},
						IPv4: "192.168.9.106",
						Metadata: IPAMMetadata{
							K8SPodNamespace: "kube-system",
							K8SPodName:      "coredns-57ff979f67-qqbdh",
						},
					},
					{
						IPAMKey: IPAMKey{
							ContainerID: "d0437ce84caa9e585c2ba84fc9f9a058f4cf783292a84608f717035e0952341",
							NetworkName: "aws-cni",
							IfName:      "eth0",
						},
						IPv4: "192.168.42.42",
					},
				},
			},
		},
		{
			name: "netlink list failed",
			fields: fields{
				netLinkListCalls: []netLinkListCall{
					{
						err: errors.New("netlink failed"),
					},
				},
			},
			args: args{
				checkpoint: CheckpointData{
					Version: CheckpointFormatVersion,
					Allocations: []CheckpointEntry{
						{
							IPAMKey: IPAMKey{
								ContainerID: "5a1f9118a7125f87b4b0f2f601c0b55cfab8bcf28963bcf7c4ece3109a8b6b86",
								NetworkName: "aws-cni",
								IfName:      "eth0",
							},
							IPv4: "192.168.9.106",
							Metadata: IPAMMetadata{
								K8SPodNamespace: "kube-system",
								K8SPodName:      "coredns-57ff979f67-qqbdh",
							},
						},
						{
							IPAMKey: IPAMKey{
								ContainerID: "b4729ce84caa9e585c2ba84fc9f9a058f4cf783292a84608f717035e09553422",
								NetworkName: "aws-cni",
								IfName:      "eth0",
							},
							IPv4: "192.168.30.161",
							Metadata: IPAMMetadata{
								K8SPodNamespace: "kube-system",
								K8SPodName:      "coredns-57ff979f67-8ns9b",
							},
						},
					},
				},
			},
			wantErr: errors.New("netlink failed"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			netLink := mock_netlinkwrapper.NewMockNetLink(ctrl)
			for _, call := range tt.fields.netLinkListCalls {
				netLink.EXPECT().LinkList().Return(call.links, call.err)
			}
			netLink.EXPECT().NewRule().DoAndReturn(func() *netlink.Rule { return netlink.NewRule() }).AnyTimes()
			// If there are any stale entries, expect toContainer del call to come before fromContainer del call
			if tt.fields.staleToContainerRule != nil {
				toContainerDelCall := netLink.EXPECT().RuleDel(tt.fields.staleToContainerRule).Return(nil)
				fromContainerDelCall := netLink.EXPECT().RuleDel(tt.fields.staleFromContainerRule).Return(nil)
				fromContainerDelCall.After(toContainerDelCall)
			}
			ds := &DataStore{netLink: netLink, log: logger.DefaultLogger()}
			got, err := ds.normalizeCheckpointDataByPodVethExistence(tt.args.checkpoint)
			if tt.wantErr != nil {
				assert.EqualError(t, err, tt.wantErr.Error())
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestDataStore_validateAllocationByPodVethExistence(t *testing.T) {
	type args struct {
		allocation  CheckpointEntry
		hostNSLinks []netlink.Link
	}
	tests := []struct {
		name    string
		args    args
		wantErr error
	}{
		{
			name: "one veth pair found with matching suffix and eni prefix",
			args: args{
				allocation: CheckpointEntry{
					IPAMKey: IPAMKey{
						ContainerID: "5a1f9118a7125f87b4b0f2f601c0b55cfab8bcf28963bcf7c4ece3109a8b6b86",
						NetworkName: "aws-cni",
						IfName:      "eth0",
					},
					IPv4: "192.168.9.106",
					Metadata: IPAMMetadata{
						K8SPodNamespace: "kube-system",
						K8SPodName:      "coredns-57ff979f67-qqbdh",
					},
				},
				hostNSLinks: []netlink.Link{
					&netlink.Device{
						LinkAttrs: netlink.LinkAttrs{
							Name: "eth0",
						},
					},
					&netlink.Veth{
						LinkAttrs: netlink.LinkAttrs{
							Name: "enib5faff8a083",
						},
					},
				},
			},
			wantErr: nil,
		},
		{
			name: "one veth pair found with matching suffix and custom prefix",
			args: args{
				allocation: CheckpointEntry{
					IPAMKey: IPAMKey{
						ContainerID: "5a1f9118a7125f87b4b0f2f601c0b55cfab8bcf28963bcf7c4ece3109a8b6b86",
						NetworkName: "aws-cni",
						IfName:      "eth0",
					},
					IPv4: "192.168.9.106",
					Metadata: IPAMMetadata{
						K8SPodNamespace: "kube-system",
						K8SPodName:      "coredns-57ff979f67-qqbdh",
					},
				},
				hostNSLinks: []netlink.Link{
					&netlink.Device{
						LinkAttrs: netlink.LinkAttrs{
							Name: "eth0",
						},
					},
					&netlink.Veth{
						LinkAttrs: netlink.LinkAttrs{
							Name: "customb5faff8a083",
						},
					},
				},
			},
			wantErr: nil,
		},
		{
			name: "no veth pair found with matching suffix",
			args: args{
				allocation: CheckpointEntry{
					IPAMKey: IPAMKey{
						ContainerID: "5a1f9118a7125f87b4b0f2f601c0b55cfab8bcf28963bcf7c4ece3109a8b6b86",
						NetworkName: "aws-cni",
						IfName:      "eth0",
					},
					IPv4: "192.168.9.106",
					Metadata: IPAMMetadata{
						K8SPodNamespace: "kube-system",
						K8SPodName:      "coredns-57ff979f67-qqbdh",
					},
				},
				hostNSLinks: []netlink.Link{
					&netlink.Device{
						LinkAttrs: netlink.LinkAttrs{
							Name: "eth0",
						},
					},
					&netlink.Veth{
						LinkAttrs: netlink.LinkAttrs{
							Name: "eni9571956a6cc",
						},
					},
				},
			},
			wantErr: errors.New("host-side veth not found for pod kube-system/coredns-57ff979f67-qqbdh"),
		},
		{
			name: "allocation without metadata should pass validation",
			args: args{
				allocation: CheckpointEntry{
					IPAMKey: IPAMKey{
						ContainerID: "5a1f9118a7125f87b4b0f2f601c0b55cfab8bcf28963bcf7c4ece3109a8b6b86",
						NetworkName: "aws-cni",
						IfName:      "eth0",
					},
					IPv4:     "192.168.9.106",
					Metadata: IPAMMetadata{},
				},
				hostNSLinks: []netlink.Link{
					&netlink.Device{
						LinkAttrs: netlink.LinkAttrs{
							Name: "eth0",
						},
					},
					&netlink.Veth{
						LinkAttrs: netlink.LinkAttrs{
							Name: "eni9571956a6cc",
						},
					},
				},
			},
			wantErr: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ds := &DataStore{}
			err := ds.validateAllocationByPodVethExistence(tt.args.allocation, tt.args.hostNSLinks)
			if tt.wantErr != nil {
				assert.EqualError(t, err, tt.wantErr.Error())
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestForceRemovalMetrics(t *testing.T) {
	// Reset metrics by creating new counters
	prometheusmetrics.ForceRemovedENIs = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "awscni_force_removed_enis_total",
		Help: "The total number of ENIs force removed",
	})
	prometheusmetrics.ForceRemovedIPs = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "awscni_force_removed_ips_total",
		Help: "The total number of IPs force removed",
	})

	ds := NewDataStore(Testlog, NullCheckpoint{}, false, defaultNetworkCard)

	// Add an ENI and IP
	err := ds.AddENI("eni-1", 1, false, false, false, networkutils.CalculateRouteTableId(1, 0), "")
	assert.NoError(t, err)

	ipv4Addr := net.IPNet{IP: net.ParseIP("1.1.1.1"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	err = ds.AddIPv4CidrToStore("eni-1", ipv4Addr, false)
	assert.NoError(t, err)

	// Assign IP to a pod
	key := IPAMKey{"net0", "sandbox-1", "eth0"}
	ip, device, tableNumber, err := ds.AssignPodIPv4Address(key, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-1"})
	assert.NoError(t, err)
	assert.Equal(t, "1.1.1.1", ip)
	assert.Equal(t, 1, device)
	assert.Equal(t, 2, tableNumber)

	// Test force removal of IP
	err = ds.DelIPv4CidrFromStore("eni-1", ipv4Addr, false)
	assert.Error(t, err) // Should fail without force
	assert.Contains(t, err.Error(), "IP is used and can not be deleted")

	ipCount := testutil.ToFloat64(prometheusmetrics.ForceRemovedIPs)
	assert.Equal(t, float64(0), ipCount)

	// Force remove the IP
	err = ds.DelIPv4CidrFromStore("eni-1", ipv4Addr, true)
	assert.NoError(t, err) // Should succeed with force

	ipCount = testutil.ToFloat64(prometheusmetrics.ForceRemovedIPs)
	assert.Equal(t, float64(1), ipCount)

	// Add another IP and assign to pod for ENI removal test
	ipv4Addr2 := net.IPNet{IP: net.ParseIP("1.1.1.2"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	err = ds.AddIPv4CidrToStore("eni-1", ipv4Addr2, false)
	assert.NoError(t, err)

	key2 := IPAMKey{"net0", "sandbox-2", "eth0"}
	ip, device, tableNumber, err = ds.AssignPodIPv4Address(key2, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-2"})
	assert.NoError(t, err)
	assert.Equal(t, "1.1.1.2", ip)
	assert.Equal(t, 1, device)
	assert.Equal(t, 2, tableNumber)

	// Test force removal of ENI
	err = ds.RemoveENIFromDataStore("eni-1", false)
	assert.Error(t, err)                                                             // Should fail without force
	assert.Contains(t, err.Error(), "datastore: ENI is used and can not be deleted") // Updated error message

	eniCount := testutil.ToFloat64(prometheusmetrics.ForceRemovedENIs)
	assert.Equal(t, float64(0), eniCount)

	// Force remove the ENI
	err = ds.RemoveENIFromDataStore("eni-1", true)
	assert.NoError(t, err) // Should succeed with force

	eniCount = testutil.ToFloat64(prometheusmetrics.ForceRemovedENIs)
	assert.Equal(t, float64(1), eniCount)
}
func TestInitializeDataStores(t *testing.T) {
	log := Testlog
	defaultPath := "/tmp/test-datastore.json"

	t.Run("single network card, not skipped", func(t *testing.T) {
		skip := []bool{false}
		dsAccess := InitializeDataStores(skip, defaultPath, false, log)
		assert.NotNil(t, dsAccess)
		assert.Equal(t, 1, len(dsAccess.DataStores))
		assert.Equal(t, 0, dsAccess.DataStores[0].GetNetworkCard())
	})

	t.Run("multiple network cards, some skipped", func(t *testing.T) {
		skip := []bool{false, true, false}
		dsAccess := InitializeDataStores(skip, defaultPath, true, log)
		assert.NotNil(t, dsAccess)
		assert.Equal(t, 2, len(dsAccess.DataStores))
		assert.Equal(t, 0, dsAccess.DataStores[0].GetNetworkCard())
		assert.Equal(t, 2, dsAccess.DataStores[1].GetNetworkCard())
	})
}

func TestDataStoreAccess_GetDataStore(t *testing.T) {
	log := Testlog
	defaultPath := "/tmp/test-datastore.json"
	skip := []bool{false, false, false}
	dsAccess := InitializeDataStores(skip, defaultPath, false, log)

	// Should return the correct DataStore for each network card
	for i := 0; i < 3; i++ {
		ds := dsAccess.GetDataStore(i)
		assert.NotNil(t, ds, "Expected DataStore for networkCard %d", i)
		assert.Equal(t, i, ds.GetNetworkCard())
	}

	// Should return nil for a network card that does not exist
	ds := dsAccess.GetDataStore(99)
	assert.Nil(t, ds)
}

func TestAssignPodIPv4AddressWithExcludedENI(t *testing.T) {
	ds := NewDataStore(Testlog, NullCheckpoint{}, false, defaultNetworkCard)

	// Add primary ENI and mark it as excluded
	err := ds.AddENI("eni-1", 0, true, false, false, 0, "")
	assert.NoError(t, err)
	err = ds.SetENIExcludedForPodIPs("eni-1", true)
	assert.NoError(t, err)

	// Add IPs to the excluded ENI
	ipv4Addr := net.IPNet{IP: net.ParseIP("10.0.0.1"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	err = ds.AddIPv4CidrToStore("eni-1", ipv4Addr, false)
	assert.NoError(t, err)

	// Try to assign an IP - should fail as ENI is excluded
	_, _, _, err = ds.AssignPodIPv4Address(
		IPAMKey{
			NetworkName: "net0",
			ContainerID: "container-1",
			IfName:      "eth0",
		},
		IPAMMetadata{
			K8SPodNamespace: "default",
			K8SPodName:      "pod-1",
		},
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no available IP/Prefix addresses")

	// Add a secondary ENI that is not excluded
	err = ds.AddENI("eni-2", 1, false, false, false, 0, "")
	assert.NoError(t, err)

	// Add IPs to the secondary ENI
	ipv4Addr2 := net.IPNet{IP: net.ParseIP("10.0.0.2"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	err = ds.AddIPv4CidrToStore("eni-2", ipv4Addr2, false)
	assert.NoError(t, err)

	// Now assignment should succeed from the non-excluded ENI
	ip, deviceNum, _, err := ds.AssignPodIPv4Address(
		IPAMKey{
			NetworkName: "net0",
			ContainerID: "container-1",
			IfName:      "eth0",
		},
		IPAMMetadata{
			K8SPodNamespace: "default",
			K8SPodName:      "pod-1",
		},
	)
	assert.NoError(t, err)
	assert.Equal(t, "10.0.0.2", ip)
	assert.Equal(t, 1, deviceNum)
}

func TestGetAllocatableENIsWithExclusion(t *testing.T) {
	ds := NewDataStore(Testlog, NullCheckpoint{}, false, defaultNetworkCard)

	// Add primary ENI and mark it as excluded
	err := ds.AddENI("eni-1", 0, true, false, false, 0, "")
	assert.NoError(t, err)
	err = ds.SetENIExcludedForPodIPs("eni-1", true)
	assert.NoError(t, err)

	// Add secondary ENI (not excluded)
	err = ds.AddENI("eni-2", 1, false, false, false, 0, "")
	assert.NoError(t, err)

	// Get allocatable ENIs - should only return the non-excluded one
	allocatable := ds.GetAllocatableENIs(10, false)
	assert.Equal(t, 1, len(allocatable))
	assert.Equal(t, "eni-2", allocatable[0].ID)
}

func TestGetIPStatsWithExcludedENI(t *testing.T) {
	ds := NewDataStore(Testlog, NullCheckpoint{}, false, defaultNetworkCard)

	// Add primary ENI with IPs and mark it as excluded
	err := ds.AddENI("eni-1", 0, true, false, false, 0, "")
	assert.NoError(t, err)
	err = ds.SetENIExcludedForPodIPs("eni-1", true)
	assert.NoError(t, err)

	// Add IPs to excluded ENI
	for i := 1; i <= 3; i++ {
		ipv4Addr := net.IPNet{IP: net.ParseIP(fmt.Sprintf("10.0.0.%d", i)), Mask: net.IPv4Mask(255, 255, 255, 255)}
		err = ds.AddIPv4CidrToStore("eni-1", ipv4Addr, false)
		assert.NoError(t, err)
	}

	// Add secondary ENI with IPs (not excluded)
	err = ds.AddENI("eni-2", 1, false, false, false, 0, "")
	assert.NoError(t, err)

	for i := 11; i <= 12; i++ {
		ipv4Addr := net.IPNet{IP: net.ParseIP(fmt.Sprintf("10.0.0.%d", i)), Mask: net.IPv4Mask(255, 255, 255, 255)}
		err = ds.AddIPv4CidrToStore("eni-2", ipv4Addr, false)
		assert.NoError(t, err)
	}

	// Get IP stats - should only count IPs from non-excluded ENIs
	stats := ds.GetIPStats("4")
	assert.Equal(t, 2, stats.TotalIPs) // Only IPs from eni-2
	assert.Equal(t, 0, stats.AssignedIPs)
	assert.Equal(t, 2, stats.AvailableAddresses())
}

func TestDelIPv6CidrFromStore(t *testing.T) {
	ds := NewDataStore(Testlog, NullCheckpoint{}, true, defaultNetworkCard)
	err := ds.AddENI("eni-1", 1, true, false, false, 0, "")
	assert.NoError(t, err)

	// Test 1: Add and delete unassigned IPv6 CIDRs
	ipv6Addr1 := net.IPNet{IP: net.ParseIP("2001:db8::"), Mask: net.CIDRMask(80, 128)}
	err = ds.AddIPv6CidrToStore("eni-1", ipv6Addr1, true)
	assert.NoError(t, err)
	assert.Equal(t, 281474976710656, ds.total) // 2^48 addresses in /80
	assert.Equal(t, 1, len(ds.eniPool["eni-1"].IPv6Cidrs))

	ipv6Addr2 := net.IPNet{IP: net.ParseIP("2001:db8:1::"), Mask: net.CIDRMask(80, 128)}
	err = ds.AddIPv6CidrToStore("eni-1", ipv6Addr2, true)
	assert.NoError(t, err)
	assert.Equal(t, 562949953421312, ds.total) // 2 * 2^48 addresses
	assert.Equal(t, 2, len(ds.eniPool["eni-1"].IPv6Cidrs))

	// Delete one IPv6 CIDR without assigned IPs - should succeed
	err = ds.DelIPv6CidrFromStore("eni-1", ipv6Addr2, false)
	assert.NoError(t, err)
	assert.Equal(t, 281474976710656, ds.total) // Back to 1 CIDR worth
	assert.Equal(t, 1, len(ds.eniPool["eni-1"].IPv6Cidrs))

	// Test 2: Try to delete a non-existent IPv6 CIDR
	unknownIPv6Addr := net.IPNet{IP: net.ParseIP("2001:db8:9999::"), Mask: net.CIDRMask(80, 128)}
	err = ds.DelIPv6CidrFromStore("eni-1", unknownIPv6Addr, false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown IP")
	assert.Equal(t, 281474976710656, ds.total) // Should remain unchanged
	assert.Equal(t, 1, len(ds.eniPool["eni-1"].IPv6Cidrs))

	// Test 3: Assign an IPv6 address to a pod from the remaining CIDR
	key := IPAMKey{"net0", "sandbox-1", "eth0"}
	ipv6Address, device, _, err := ds.AssignPodIPv6Address(key, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-1"})
	assert.NoError(t, err)
	assert.NotEmpty(t, ipv6Address)
	assert.Equal(t, 1, device)

	// Try to delete the CIDR with assigned IP - should fail without force
	err = ds.DelIPv6CidrFromStore("eni-1", ipv6Addr1, false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "IP is used and can not be deleted")
	assert.Equal(t, 281474976710656, ds.total) // Should remain unchanged
	assert.Equal(t, 1, len(ds.eniPool["eni-1"].IPv6Cidrs))

	// Test 4: Force delete the CIDR with assigned IP - should succeed
	err = ds.DelIPv6CidrFromStore("eni-1", ipv6Addr1, true)
	assert.NoError(t, err)
	assert.Equal(t, 0, ds.total) // Back to 0
	assert.Equal(t, 0, len(ds.eniPool["eni-1"].IPv6Cidrs))

	// Test 5: Try to delete from a non-existent ENI
	err = ds.DelIPv6CidrFromStore("eni-nonexistent", ipv6Addr1, false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown ENI")
}

func TestFreeablePrefixesBothIPv4AndIPv6(t *testing.T) {
	ds := NewDataStore(Testlog, NullCheckpoint{}, true, defaultNetworkCard)
	err := ds.AddENI("eni-1", 1, true, false, false, 0, "")
	assert.NoError(t, err)

	// Add IPv4 prefixes
	ipv4Prefix1 := net.IPNet{IP: net.ParseIP("10.0.0.0"), Mask: net.CIDRMask(28, 32)}
	err = ds.AddIPv4CidrToStore("eni-1", ipv4Prefix1, true)
	assert.NoError(t, err)

	ipv4Prefix2 := net.IPNet{IP: net.ParseIP("10.0.1.0"), Mask: net.CIDRMask(28, 32)}
	err = ds.AddIPv4CidrToStore("eni-1", ipv4Prefix2, true)
	assert.NoError(t, err)

	// Add IPv4 secondary IP (not a prefix)
	ipv4SecondaryIP := net.IPNet{IP: net.ParseIP("10.0.2.1"), Mask: net.CIDRMask(32, 32)}
	err = ds.AddIPv4CidrToStore("eni-1", ipv4SecondaryIP, false)
	assert.NoError(t, err)

	// Add IPv6 prefixes
	ipv6Prefix1 := net.IPNet{IP: net.ParseIP("2001:db8::"), Mask: net.CIDRMask(80, 128)}
	err = ds.AddIPv6CidrToStore("eni-1", ipv6Prefix1, true)
	assert.NoError(t, err)

	ipv6Prefix2 := net.IPNet{IP: net.ParseIP("2001:db8:1::"), Mask: net.CIDRMask(80, 128)}
	err = ds.AddIPv6CidrToStore("eni-1", ipv6Prefix2, true)
	assert.NoError(t, err)

	// Get freeable prefixes - should return all unassigned prefixes (both IPv4 and IPv6)
	freeablePrefixes := ds.FreeablePrefixes("eni-1")
	assert.Equal(t, 4, len(freeablePrefixes)) // 2 IPv4 prefixes + 2 IPv6 prefixes

	// Check that IPv4 secondary IP is not included (it's not a prefix)
	ipv4PrefixCount := 0
	ipv6PrefixCount := 0
	for _, prefix := range freeablePrefixes {
		if prefix.IP.To4() != nil {
			ipv4PrefixCount++
		} else {
			ipv6PrefixCount++
		}
	}
	assert.Equal(t, 2, ipv4PrefixCount, "Should have 2 IPv4 prefixes")
	assert.Equal(t, 2, ipv6PrefixCount, "Should have 2 IPv6 prefixes")

	// Assign an IP from IPv4 prefix1 to a pod
	key1 := IPAMKey{"net0", "sandbox-1", "eth0"}
	ipv4Address, device, _, err := ds.AssignPodIPv4Address(key1, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "pod-1"})
	assert.NoError(t, err)
	assert.NotEmpty(t, ipv4Address)
	assert.Equal(t, 1, device)

	// Assign an IP from IPv6 prefix1 to a pod
	key2 := IPAMKey{"net0", "sandbox-2", "eth0"}
	ipv6Address, device, _, err := ds.AssignPodIPv6Address(key2, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "pod-2"})
	assert.NoError(t, err)
	assert.NotEmpty(t, ipv6Address)
	assert.Equal(t, 1, device)

	// Now get freeable prefixes - should exclude prefixes with assigned IPs
	freeablePrefixes = ds.FreeablePrefixes("eni-1")
	assert.Equal(t, 2, len(freeablePrefixes)) // Only unassigned prefixes

	// Verify the returned prefixes are the ones without assigned IPs
	ipv4PrefixCount = 0
	ipv6PrefixCount = 0
	foundIPv4Prefix := ""
	foundIPv6Prefix := ""
	for _, prefix := range freeablePrefixes {
		if prefix.IP.To4() != nil {
			ipv4PrefixCount++
			foundIPv4Prefix = prefix.IP.String()
		} else {
			ipv6PrefixCount++
			foundIPv6Prefix = prefix.IP.String()
		}
	}
	assert.Equal(t, 1, ipv4PrefixCount, "Should have 1 unassigned IPv4 prefix")
	assert.Equal(t, 1, ipv6PrefixCount, "Should have 1 unassigned IPv6 prefix")
	// Should be the second IPv4 prefix (10.0.1.0/28), not the first one with assigned IP
	assert.Equal(t, "10.0.1.0", foundIPv4Prefix)
	// Should be the second IPv6 prefix (2001:db8:1::/80), not the first one with assigned IP
	assert.Equal(t, "2001:db8:1::", foundIPv6Prefix)

	// Test with non-existent ENI
	freeablePrefixes = ds.FreeablePrefixes("eni-nonexistent")
	assert.Nil(t, freeablePrefixes, "Should return nil for non-existent ENI")
}

func TestDeallocateEmptyCIDR(t *testing.T) {
	ds := NewDataStore(Testlog, NullCheckpoint{}, false, defaultNetworkCard)

	// Setup test ENI
	eniID := "eni-test-dealloc"
	err := ds.AddENI(eniID, 1, false, false, false, 0, "")
	assert.NoError(t, err)

	// Mark ENI as excluded for pod IPs
	err = ds.SetENIExcludedForPodIPs(eniID, true)
	assert.NoError(t, err)

	t.Run("IPv4 CIDR cleanup on excluded ENI", func(t *testing.T) {
		// Add IPv4 CIDR to ENI
		ipv4Cidr := net.IPNet{IP: net.ParseIP("192.168.1.0"), Mask: net.CIDRMask(24, 32)}
		err = ds.AddIPv4CidrToStore(eniID, ipv4Cidr, false)
		assert.NoError(t, err)

		// Verify CIDR exists
		eni := ds.eniPool[eniID]
		cidrStr := ipv4Cidr.String()
		cidrInfo, exists := eni.AvailableIPv4Cidrs[cidrStr]
		assert.True(t, exists, "IPv4 CIDR should exist before cleanup")

		// Call deallocateEmptyCIDR
		ds.deallocateEmptyCIDR(eniID, cidrInfo)

		// Verify CIDR is removed
		_, exists = eni.AvailableIPv4Cidrs[cidrStr]
		assert.False(t, exists, "IPv4 CIDR should be removed after cleanup")
	})

	t.Run("IPv6 CIDR cleanup on excluded ENI", func(t *testing.T) {
		// Add IPv6 CIDR to ENI
		ipv6Cidr := net.IPNet{IP: net.ParseIP("2001:db8:1::"), Mask: net.CIDRMask(64, 128)}
		err = ds.AddIPv6CidrToStore(eniID, ipv6Cidr, false)
		assert.NoError(t, err)

		// Verify CIDR exists
		eni := ds.eniPool[eniID]
		cidrStr := ipv6Cidr.String()
		cidrInfo, exists := eni.IPv6Cidrs[cidrStr]
		assert.True(t, exists, "IPv6 CIDR should exist before cleanup")

		// Call deallocateEmptyCIDR
		ds.deallocateEmptyCIDR(eniID, cidrInfo)

		// Verify CIDR is removed
		_, exists = eni.IPv6Cidrs[cidrStr]
		assert.False(t, exists, "IPv6 CIDR should be removed after cleanup")
	})

	t.Run("Skip cleanup when ENI is not excluded", func(t *testing.T) {
		// Create new non-excluded ENI
		nonExcludedENI := "eni-not-excluded"
		err := ds.AddENI(nonExcludedENI, 1, false, false, false, 0, "")
		assert.NoError(t, err)

		// Don't mark as excluded (default is false)

		// Add IPv4 CIDR
		ipv4Cidr := net.IPNet{IP: net.ParseIP("192.168.2.0"), Mask: net.CIDRMask(24, 32)}
		err = ds.AddIPv4CidrToStore(nonExcludedENI, ipv4Cidr, false)
		assert.NoError(t, err)

		// Get CIDR info
		eni := ds.eniPool[nonExcludedENI]
		cidrStr := ipv4Cidr.String()
		cidrInfo, exists := eni.AvailableIPv4Cidrs[cidrStr]
		assert.True(t, exists, "IPv4 CIDR should exist")

		// Call deallocateEmptyCIDR - should not remove CIDR since ENI is not excluded
		ds.deallocateEmptyCIDR(nonExcludedENI, cidrInfo)

		// Verify CIDR still exists
		_, exists = eni.AvailableIPv4Cidrs[cidrStr]
		assert.True(t, exists, "IPv4 CIDR should remain since ENI is not excluded")
	})

	t.Run("Skip cleanup when CIDR has assigned IPs", func(t *testing.T) {
		// Create fresh datastore for isolated test
		dsIsolated := NewDataStore(Testlog, NullCheckpoint{}, false, defaultNetworkCard)

		// Create new excluded ENI
		excludedENI := "eni-excluded-with-ips"
		err := dsIsolated.AddENI(excludedENI, 1, false, false, false, 0, "")
		assert.NoError(t, err)

		err = dsIsolated.SetENIExcludedForPodIPs(excludedENI, true)
		assert.NoError(t, err)

		// Add IPv4 CIDR
		ipv4Cidr := net.IPNet{IP: net.ParseIP("192.168.10.0"), Mask: net.CIDRMask(24, 32)}
		err = dsIsolated.AddIPv4CidrToStore(excludedENI, ipv4Cidr, false)
		assert.NoError(t, err)

		// Manually assign an IP to the CIDR to simulate non-empty state
		eni := dsIsolated.eniPool[excludedENI]
		cidrStr := ipv4Cidr.String()
		cidrInfo, exists := eni.AvailableIPv4Cidrs[cidrStr]
		assert.True(t, exists, "IPv4 CIDR should exist")

		// Manually mark an IP as assigned in the CIDR
		testIP := net.ParseIP("192.168.10.1")
		cidrInfo.IPAddresses[testIP.String()] = &AddressInfo{
			Address:      testIP.String(),
			IPAMKey:      IPAMKey{NetworkName: "test", ContainerID: "test-container", IfName: "eth0"},
			IPAMMetadata: IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "test-pod"},
			AssignedTime: time.Now(),
		}

		// Verify CIDR has assigned IPs
		assert.Greater(t, cidrInfo.AssignedIPAddressesInCidr(), 0, "CIDR should have assigned IPs")

		// Call deallocateEmptyCIDR - should not remove CIDR since it has assigned IPs
		dsIsolated.deallocateEmptyCIDR(excludedENI, cidrInfo)

		// Verify CIDR still exists
		_, exists = eni.AvailableIPv4Cidrs[cidrStr]
		assert.True(t, exists, "IPv4 CIDR should remain since it has assigned IPs")
	})

	t.Run("Handle non-existent ENI gracefully", func(t *testing.T) {
		// Create dummy CIDR info
		dummyCidr := &CidrInfo{
			Cidr:          net.IPNet{IP: net.ParseIP("192.168.4.0"), Mask: net.CIDRMask(24, 32)},
			AddressFamily: "4",
		}

		// Should not panic when ENI doesn't exist
		ds.deallocateEmptyCIDR("eni-nonexistent", dummyCidr)
	})

	t.Run("Handle non-existent CIDR gracefully", func(t *testing.T) {
		// Create ENI
		testENI := "eni-cidr-test"
		err := ds.AddENI(testENI, 1, false, false, false, 0, "")
		assert.NoError(t, err)

		err = ds.SetENIExcludedForPodIPs(testENI, true)
		assert.NoError(t, err)

		// Create CIDR info that doesn't exist in the ENI
		nonExistentCidr := &CidrInfo{
			Cidr:          net.IPNet{IP: net.ParseIP("192.168.5.0"), Mask: net.CIDRMask(24, 32)},
			AddressFamily: "4",
		}

		// Should handle gracefully when CIDR doesn't exist
		ds.deallocateEmptyCIDR(testENI, nonExistentCidr)

		// Verify ENI still exists and is unaffected
		_, exists := ds.eniPool[testENI]
		assert.True(t, exists, "ENI should still exist")
	})
}

func TestDataStore_IsENIExcludedForPodIPs(t *testing.T) {
	ds := NewDataStore(Testlog, NullCheckpoint{}, false, defaultNetworkCard)
	eniID := "eni-test"
	err := ds.AddENI(eniID, 1, false, false, false, 0, "")
	assert.NoError(t, err)
	assert.False(t, ds.IsENIExcludedForPodIPs(eniID))
	err = ds.SetENIExcludedForPodIPs(eniID, true)
	assert.NoError(t, err)
	assert.True(t, ds.IsENIExcludedForPodIPs(eniID))
	assert.False(t, ds.IsENIExcludedForPodIPs("non-existent"))
}

func TestDataStore_GetTrunkENI(t *testing.T) {
	ds := NewDataStore(Testlog, NullCheckpoint{}, false, defaultNetworkCard)
	assert.Equal(t, "", ds.GetTrunkENI())
	err := ds.AddENI("eni-regular", 1, false, false, false, 0, "")
	assert.NoError(t, err)
	assert.Equal(t, "", ds.GetTrunkENI())
	err = ds.AddENI("eni-trunk", 2, false, true, false, 0, "")
	assert.NoError(t, err)
	assert.Equal(t, "eni-trunk", ds.GetTrunkENI())
}

func TestDataStore_GetEFAENIs(t *testing.T) {
	ds := NewDataStore(Testlog, NullCheckpoint{}, false, defaultNetworkCard)
	efas := ds.GetEFAENIs()
	assert.Empty(t, efas)
	err := ds.AddENI("eni-regular", 1, false, false, false, 0, "")
	assert.NoError(t, err)
	efas = ds.GetEFAENIs()
	assert.Empty(t, efas)
	err = ds.AddENI("eni-efa1", 2, false, false, true, 0, "")
	assert.NoError(t, err)
	err = ds.AddENI("eni-efa2", 3, false, false, true, 0, "")
	assert.NoError(t, err)
	efas = ds.GetEFAENIs()
	assert.Len(t, efas, 2)
	assert.True(t, efas["eni-efa1"])
	assert.True(t, efas["eni-efa2"])
}

func TestDivCeil(t *testing.T) {
	assert.Equal(t, 1, DivCeil(1, 1))
	assert.Equal(t, 2, DivCeil(3, 2))
	assert.Equal(t, 3, DivCeil(5, 2))
	assert.Equal(t, 5, DivCeil(10, 2))
	assert.Equal(t, 1, DivCeil(1, 10))
}

func TestGetPrefixDelegationDefaults(t *testing.T) {
	numPrefixes, numIPs, prefixLen := GetPrefixDelegationDefaults()
	assert.Equal(t, 1, numPrefixes)
	assert.Equal(t, 16, numIPs)
	assert.Equal(t, 28, prefixLen)
}

func TestDataStore_CheckFreeableENIexists(t *testing.T) {
	ds := NewDataStore(Testlog, NullCheckpoint{}, false, defaultNetworkCard)
	assert.False(t, ds.CheckFreeableENIexists())
	err := ds.AddENI("eni-primary", 0, true, false, false, 0, "")
	assert.NoError(t, err)
	assert.False(t, ds.CheckFreeableENIexists())
	err = ds.AddENI("eni-secondary", 1, false, false, false, 0, "")
	assert.NoError(t, err)
	assert.True(t, ds.CheckFreeableENIexists())
	err = ds.AddENI("eni-trunk", 2, false, true, false, 0, "")
	assert.NoError(t, err)
	assert.True(t, ds.CheckFreeableENIexists())
	err = ds.AddENI("eni-efa", 3, false, false, true, 0, "")
	assert.NoError(t, err)
	assert.True(t, ds.CheckFreeableENIexists())
}

func TestDataStoreStats_String(t *testing.T) {
	stats := &DataStoreStats{TotalIPs: 10, AssignedIPs: 5}
	str := stats.String()
	assert.Contains(t, str, "Total")
}

func TestDataStore_FreeableIPs(t *testing.T) {
	ds := NewDataStore(Testlog, NullCheckpoint{}, false, defaultNetworkCard)
	eniID := "eni-test"
	err := ds.AddENI(eniID, 1, false, false, false, 0, "")
	assert.NoError(t, err)
	_, ipnet1, _ := net.ParseCIDR("10.0.1.1/32")
	err = ds.AddIPv4CidrToStore(eniID, *ipnet1, false)
	assert.NoError(t, err)
	_, ipnet2, _ := net.ParseCIDR("10.0.1.2/32")
	err = ds.AddIPv4CidrToStore(eniID, *ipnet2, false)
	assert.NoError(t, err)
	freeableIPs := ds.FreeableIPs(eniID)
	assert.Len(t, freeableIPs, 2)
}

func TestDataStore_GetFreePrefixes(t *testing.T) {
	ds := NewDataStore(Testlog, NullCheckpoint{}, true, defaultNetworkCard)
	eniID := "eni-test"
	err := ds.AddENI(eniID, 1, false, false, false, 0, "")
	assert.NoError(t, err)
	_, ipnet, _ := net.ParseCIDR("10.0.1.0/28")
	err = ds.AddIPv4CidrToStore(eniID, *ipnet, true)
	assert.NoError(t, err)
	freePrefixes := ds.GetFreePrefixes()
	assert.Equal(t, 1, freePrefixes)
}

func TestDataStore_FindFreeableCidrs(t *testing.T) {
	ds := NewDataStore(Testlog, NullCheckpoint{}, true, defaultNetworkCard)
	eniID := "eni-test"
	err := ds.AddENI(eniID, 1, false, false, false, 0, "")
	assert.NoError(t, err)
	_, ipnet, _ := net.ParseCIDR("10.0.1.0/28")
	err = ds.AddIPv4CidrToStore(eniID, *ipnet, true)
	assert.NoError(t, err)
	cidrs := ds.FindFreeableCidrs(eniID)
	assert.Len(t, cidrs, 1)
}

