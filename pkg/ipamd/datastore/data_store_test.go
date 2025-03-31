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

func TestAddENI(t *testing.T) {
	ds := NewDataStore(Testlog, NullCheckpoint{}, false)

	err := ds.AddENI("eni-1", 1, true, false, false)
	assert.NoError(t, err)

	err = ds.AddENI("eni-1", 1, true, false, false)
	assert.Error(t, err)

	err = ds.AddENI("eni-2", 2, false, false, false)
	assert.NoError(t, err)

	assert.Equal(t, len(ds.eniPool), 2)

	eniInfos := ds.GetENIInfos()
	assert.Equal(t, len(eniInfos.ENIs), 2)
}

func TestDeleteENI(t *testing.T) {
	ds := NewDataStore(Testlog, NullCheckpoint{}, false)

	err := ds.AddENI("eni-1", 1, true, false, false)
	assert.NoError(t, err)

	err = ds.AddENI("eni-2", 2, false, false, false)
	assert.NoError(t, err)

	err = ds.AddENI("eni-3", 3, false, false, false)
	assert.NoError(t, err)

	eniInfos := ds.GetENIInfos()
	assert.Equal(t, len(eniInfos.ENIs), 3)

	err = ds.RemoveENIFromDataStore("eni-2", false)
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
	ip, device, err := ds.AssignPodIPv4Address(
		IPAMKey{"net1", "sandbox1", "eth0"},
		IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod"})
	assert.NoError(t, err)
	assert.Equal(t, "1.1.1.1", ip)
	assert.Equal(t, 1, device)

	// Test force removal.  The first call fails because eni-1 has an IP with a pod assigned to it,
	// but the second call force-removes it and succeeds.
	err = ds.RemoveENIFromDataStore("eni-1", false)
	assert.Error(t, err)
	err = ds.RemoveENIFromDataStore("eni-1", true)
	assert.NoError(t, err)

}

func TestDeleteENIwithPDEnabled(t *testing.T) {
	ds := NewDataStore(Testlog, NullCheckpoint{}, true)

	err := ds.AddENI("eni-1", 1, true, false, false)
	assert.NoError(t, err)

	err = ds.AddENI("eni-2", 2, false, false, false)
	assert.NoError(t, err)

	err = ds.AddENI("eni-3", 3, false, false, false)
	assert.NoError(t, err)

	err = ds.AddENI("eni-4", 4, false, false, false)
	assert.NoError(t, err)

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
	ip, device, err := ds.AssignPodIPv4Address(
		IPAMKey{"net1", "sandbox1", "eth0"},
		IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod"})
	assert.NoError(t, err)
	assert.Equal(t, "10.0.0.0", ip)
	assert.Equal(t, 4, device)

	// Test force removal.  The first call fails because eni-1 has an IP with a pod assigned to it,
	// but the second call force-removes it and succeeds.
	err = ds.RemoveENIFromDataStore("eni-4", false)
	assert.Error(t, err)
	err = ds.RemoveENIFromDataStore("eni-4", true)
	assert.NoError(t, err)

}

func TestAddENIIPv4Address(t *testing.T) {
	ds := NewDataStore(Testlog, NullCheckpoint{}, false)

	err := ds.AddENI("eni-1", 1, true, false, false)
	assert.NoError(t, err)

	err = ds.AddENI("eni-2", 2, false, false, false)
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
	ds := NewDataStore(Testlog, NullCheckpoint{}, true)

	err := ds.AddENI("eni-1", 1, true, false, false)
	assert.NoError(t, err)

	err = ds.AddENI("eni-2", 2, false, false, false)
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
	ds := NewDataStore(Testlog, NullCheckpoint{}, false)

	err := ds.AddENI("eni-1", 1, true, false, false)
	assert.NoError(t, err)

	err = ds.AddENI("eni-2", 2, false, false, false)
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
	ds := NewDataStore(Testlog, NullCheckpoint{}, true)

	err := ds.AddENI("eni-1", 1, true, false, false)
	assert.NoError(t, err)

	err = ds.AddENI("eni-2", 2, false, false, false)
	assert.NoError(t, err)

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
	ds := NewDataStore(Testlog, NullCheckpoint{}, false)
	err := ds.AddENI("eni-1", 1, true, false, false)
	assert.NoError(t, err)

	ipv4Addr := net.IPNet{IP: net.ParseIP("1.1.1.1"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	err = ds.AddIPv4CidrToStore("eni-1", ipv4Addr, false)
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 1)
	assert.Equal(t, len(ds.eniPool["eni-1"].AvailableIPv4Cidrs), 1)

	// Assign a pod.
	key := IPAMKey{"net0", "sandbox-1", "eth0"}
	ip, device, err := ds.AssignPodIPv4Address(key, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-1"})
	assert.NoError(t, err)
	assert.Equal(t, "1.1.1.1", ip)
	assert.Equal(t, 1, device)

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
	ds := NewDataStore(Testlog, NullCheckpoint{}, true)
	err := ds.AddENI("eni-1", 1, true, false, false)
	assert.NoError(t, err)

	ipv4Addr := net.IPNet{IP: net.ParseIP("10.0.0.0"), Mask: net.IPv4Mask(255, 255, 255, 240)}
	err = ds.AddIPv4CidrToStore("eni-1", ipv4Addr, true)
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 16)
	assert.Equal(t, len(ds.eniPool["eni-1"].AvailableIPv4Cidrs), 1)

	// Assign a pod.
	key := IPAMKey{"net0", "sandbox-1", "eth0"}
	ip, device, err := ds.AssignPodIPv4Address(key, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-1"})
	assert.NoError(t, err)
	assert.Equal(t, "10.0.0.0", ip)
	assert.Equal(t, 1, device)

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
	ds := NewDataStore(Testlog, NullCheckpoint{}, false)
	err := ds.AddENI("eni-1", 1, true, false, false)
	assert.NoError(t, err)

	// Add /32 secondary IP
	ipv4Addr := net.IPNet{IP: net.ParseIP("1.1.1.1"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	err = ds.AddIPv4CidrToStore("eni-1", ipv4Addr, false)
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 1)
	assert.Equal(t, len(ds.eniPool["eni-1"].AvailableIPv4Cidrs), 1)

	// Assign a pod.
	key := IPAMKey{"net0", "sandbox-1", "eth0"}
	ip, device, err := ds.AssignPodIPv4Address(key, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-1"})
	assert.NoError(t, err)
	assert.Equal(t, "1.1.1.1", ip)
	assert.Equal(t, 1, device)

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
	ip, device, err = ds.AssignPodIPv4Address(key, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-2"})
	assert.NoError(t, err)
	assert.Equal(t, "10.0.0.0", ip)
	assert.Equal(t, 1, device)

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
	ds := NewDataStore(Testlog, checkpoint, false)

	checkpointDataCmpOpts := cmp.Options{
		cmpopts.IgnoreFields(CheckpointEntry{}, "AllocationTimestamp"),
		cmpopts.SortSlices(func(lhs CheckpointEntry, rhs CheckpointEntry) bool {
			return lhs.ContainerID < rhs.ContainerID
		}),
	}

	err := ds.AddENI("eni-1", 1, true, false, false)
	assert.NoError(t, err)

	err = ds.AddENI("eni-2", 2, false, false, false)
	assert.NoError(t, err)

	ipv4Addr1 := net.IPNet{IP: net.ParseIP("1.1.1.1"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	err = ds.AddIPv4CidrToStore("eni-1", ipv4Addr1, false)
	assert.NoError(t, err)

	key1 := IPAMKey{"net0", "sandbox-1", "eth0"}
	ip, _, err := ds.AssignPodIPv4Address(key1, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-1"})

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
	ip, _, err = ds.AssignPodIPv4Address(key1, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-1"}) // same id
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
	_, _, err = ds.AssignPodIPv4Address(key2, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-2"})
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

	ip, pod1Ns2Device, err := ds.AssignPodIPv4Address(key2, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-2"})
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
	ip, _, err = ds.AssignPodIPv4Address(key3, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-3"})
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
	_, _, err = ds.AssignPodIPv4Address(key4, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-4"})
	assert.Error(t, err)
	// Unassign unknown Pod
	_, _, _, _, err = ds.UnassignPodIPAddress(key4)
	assert.Error(t, err)

	_, _, deviceNum, interfaces, err := ds.UnassignPodIPAddress(key2)
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 3)
	assert.Equal(t, ds.assigned, 2)
	assert.Equal(t, deviceNum, pod1Ns2Device)
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
	ds := NewDataStore(Testlog, checkpoint, true)

	checkpointDataCmpOpts := cmp.Options{
		cmpopts.IgnoreFields(CheckpointEntry{}, "AllocationTimestamp"),
		cmpopts.SortSlices(func(lhs CheckpointEntry, rhs CheckpointEntry) bool {
			return lhs.ContainerID < rhs.ContainerID
		}),
	}

	err := ds.AddENI("eni-1", 1, true, false, false)
	assert.NoError(t, err)

	err = ds.AddENI("eni-2", 2, false, false, false)
	assert.NoError(t, err)

	ipv4Addr1 := net.IPNet{IP: net.ParseIP("10.0.0.0"), Mask: net.IPv4Mask(255, 255, 255, 240)}
	err = ds.AddIPv4CidrToStore("eni-1", ipv4Addr1, true)
	assert.NoError(t, err)

	key1 := IPAMKey{"net0", "sandbox-1", "eth0"}
	ip, _, err := ds.AssignPodIPv4Address(key1, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-1"})

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
	ip, _, err = ds.AssignPodIPv4Address(key1, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-1"}) // same id
	assert.NoError(t, err)
	assert.Equal(t, ip, "10.0.0.0")
	assert.Equal(t, ds.total, 16)
	assert.Equal(t, ds.assigned, 1)
	assert.Equal(t, len(ds.eniPool["eni-1"].AvailableIPv4Cidrs), 1)
	assert.Equal(t, ds.eniPool["eni-1"].AssignedIPv4Addresses(), 1)

	// Checkpoint error
	checkpoint.Error = errors.New("fake checkpoint error")
	key2 := IPAMKey{"net0", "sandbox-2", "eth0"}
	_, _, err = ds.AssignPodIPv4Address(key2, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-2"})
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

	ip, pod1Ns2Device, err := ds.AssignPodIPv4Address(key2, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-2"})
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
	ip, _, err = ds.AssignPodIPv4Address(key3, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-3"})
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

	_, _, deviceNum, interfaces, err := ds.UnassignPodIPAddress(key2)
	assert.NoError(t, err)
	assert.Equal(t, ds.total, 16)
	assert.Equal(t, ds.assigned, 2)
	assert.Equal(t, interfaces, 1)
	assert.Equal(t, deviceNum, pod1Ns2Device)
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
	ds := NewDataStore(Testlog, NullCheckpoint{}, false)

	_ = ds.AddENI("eni-1", 1, true, false, false)

	ipv4Addr := net.IPNet{IP: net.ParseIP("1.1.1.1"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	_ = ds.AddIPv4CidrToStore("eni-1", ipv4Addr, false)
	key1 := IPAMKey{"net0", "sandbox-1", "eth0"}
	_, _, err := ds.AssignPodIPv4Address(key1, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-1"})
	assert.NoError(t, err)

	ipv4Addr = net.IPNet{IP: net.ParseIP("1.1.1.2"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	_ = ds.AddIPv4CidrToStore("eni-1", ipv4Addr, false)
	key2 := IPAMKey{"net0", "sandbox-2", "eth0"}
	_, _, err = ds.AssignPodIPv4Address(key2, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-2"})
	assert.NoError(t, err)

	assert.Equal(t,
		DataStoreStats{
			TotalIPs:    2,
			AssignedIPs: 2,
			CooldownIPs: 0,
		},
		*ds.GetIPStats("4"),
	)

	_, _, _, _, err = ds.UnassignPodIPAddress(key2)
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
	ds := NewDataStore(Testlog, NullCheckpoint{}, true)

	_ = ds.AddENI("eni-1", 1, true, false, false)

	ipv4Addr := net.IPNet{IP: net.ParseIP("10.0.0.0"), Mask: net.IPv4Mask(255, 255, 255, 240)}
	_ = ds.AddIPv4CidrToStore("eni-1", ipv4Addr, true)
	key1 := IPAMKey{"net0", "sandbox-1", "eth0"}
	_, _, err := ds.AssignPodIPv4Address(key1, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-1"})
	assert.NoError(t, err)

	key2 := IPAMKey{"net0", "sandbox-2", "eth0"}
	_, _, err = ds.AssignPodIPv4Address(key2, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-2"})
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

	_, _, _, _, err = ds.UnassignPodIPAddress(key2)
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
	v6ds := NewDataStore(Testlog, NullCheckpoint{}, true)
	_ = v6ds.AddENI("eni-1", 1, true, false, false)
	ipv6Addr := net.IPNet{IP: net.IP{0x21, 0xdb, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, Mask: net.CIDRMask(80, 128)}
	_ = v6ds.AddIPv6CidrToStore("eni-1", ipv6Addr, true)
	key3 := IPAMKey{"netv6", "sandbox-3", "eth0"}
	_, _, err := v6ds.AssignPodIPv6Address(key3, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-3"})
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

func TestWarmENIInteractions(t *testing.T) {
	ds := NewDataStore(Testlog, NullCheckpoint{}, false)

	_ = ds.AddENI("eni-1", 1, true, false, false)
	_ = ds.AddENI("eni-2", 2, false, false, false)
	_ = ds.AddENI("eni-3", 3, false, false, false)

	// Add an IP address to ENI 1 and assign it to a pod
	ipv4Addr := net.IPNet{IP: net.ParseIP("1.1.1.1"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	_ = ds.AddIPv4CidrToStore("eni-1", ipv4Addr, false)
	key1 := IPAMKey{"net0", "sandbox-1", "eth0"}
	_, _, err := ds.AssignPodIPv4Address(key1, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-1"})
	assert.NoError(t, err)

	// Add another IP address to ENI 1 and assign a pod
	ipv4Addr = net.IPNet{IP: net.ParseIP("1.1.1.2"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	_ = ds.AddIPv4CidrToStore("eni-1", ipv4Addr, false)
	key2 := IPAMKey{"net0", "sandbox-2", "eth0"}
	_, _, err = ds.AssignPodIPv4Address(key2, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-2"})
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
	ds.AddENI("eni-4", 4, false, true, false) // trunk ENI
	ds.AddENI("eni-5", 5, false, false, true) // EFA ENI
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
	ds.AddENI("eni-6", 6, false, false, false) // trunk ENI
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

	ds := NewDataStore(Testlog, NullCheckpoint{}, false)

	// Add an ENI and IP
	err := ds.AddENI("eni-1", 1, true, false, false)
	assert.NoError(t, err)

	ipv4Addr := net.IPNet{IP: net.ParseIP("1.1.1.1"), Mask: net.IPv4Mask(255, 255, 255, 255)}
	err = ds.AddIPv4CidrToStore("eni-1", ipv4Addr, false)
	assert.NoError(t, err)

	// Assign IP to a pod
	key := IPAMKey{"net0", "sandbox-1", "eth0"}
	ip, device, err := ds.AssignPodIPv4Address(key, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-1"})
	assert.NoError(t, err)
	assert.Equal(t, "1.1.1.1", ip)
	assert.Equal(t, 1, device)

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
	ip, device, err = ds.AssignPodIPv4Address(key2, IPAMMetadata{K8SPodNamespace: "default", K8SPodName: "sample-pod-2"})
	assert.NoError(t, err)
	assert.Equal(t, "1.1.1.2", ip)
	assert.Equal(t, 1, device)

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
