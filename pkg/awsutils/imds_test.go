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
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetAZ(t *testing.T) {
	f := TypedIMDS{FakeIMDS(map[string]interface{}{
		"placement/availability-zone": "us-west-2b",
	})}

	az, err := f.GetAZ(context.TODO())
	if assert.NoError(t, err) {
		assert.Equal(t, az, "us-west-2b")
	}
}

func TestGetInstanceType(t *testing.T) {
	f := TypedIMDS{FakeIMDS(map[string]interface{}{
		"instance-type": "t3.medium",
	})}

	ty, err := f.GetInstanceType(context.TODO())
	if assert.NoError(t, err) {
		assert.Equal(t, ty, "t3.medium")
	}
}

func TestGetLocalIPv4(t *testing.T) {
	f := TypedIMDS{FakeIMDS(map[string]interface{}{
		"local-ipv4": "10.0.88.3",
	})}

	ip, err := f.GetLocalIPv4(context.TODO())
	if assert.NoError(t, err) {
		assert.Equal(t, ip, net.IPv4(10, 0, 88, 3))
	}
}

func TestGetInstanceID(t *testing.T) {
	f := TypedIMDS{FakeIMDS(map[string]interface{}{
		"instance-id": "i-084abd1f69f27d987",
	})}

	id, err := f.GetInstanceID(context.TODO())
	if assert.NoError(t, err) {
		assert.Equal(t, id, "i-084abd1f69f27d987")
	}
}

func TestGetMAC(t *testing.T) {
	f := TypedIMDS{FakeIMDS(map[string]interface{}{
		"mac": "02:68:f3:f6:c7:ef",
	})}

	mac, err := f.GetMAC(context.TODO())
	if assert.NoError(t, err) {
		assert.Equal(t, mac, "02:68:f3:f6:c7:ef")
	}
}

func TestGetMACs(t *testing.T) {
	f := TypedIMDS{FakeIMDS(map[string]interface{}{
		"network/interfaces/macs": `02:68:f3:f6:c7:ef/
02:c5:f8:3e:6b:27/`,
	})}

	macs, err := f.GetMACs(context.TODO())
	if assert.NoError(t, err) {
		assert.Equal(t, macs, []string{"02:68:f3:f6:c7:ef", "02:c5:f8:3e:6b:27"})
	}
}

func TestGetInterfaceID(t *testing.T) {
	f := TypedIMDS{FakeIMDS(map[string]interface{}{
		"network/interfaces/macs/02:c5:f8:3e:6b:27/interface-id": "eni-0c0fde533492c9df5",
	})}

	id, err := f.GetInterfaceID(context.TODO(), "02:c5:f8:3e:6b:27")
	if assert.NoError(t, err) {
		assert.Equal(t, id, "eni-0c0fde533492c9df5")
	}
}

func TestGetDeviceNumber(t *testing.T) {
	f := TypedIMDS{FakeIMDS(map[string]interface{}{
		"network/interfaces/macs/02:c5:f8:3e:6b:27/device-number": "1",
	})}

	n, err := f.GetDeviceNumber(context.TODO(), "02:c5:f8:3e:6b:27")
	if assert.NoError(t, err) {
		assert.Equal(t, n, 1)
	}

	_, err = f.GetDeviceNumber(context.TODO(), "00:00:de:ad:be:ef")
	if assert.Error(t, err) {
		assert.True(t, IsNotFound(err))
	}
}

func TestGetSubnetID(t *testing.T) {
	f := TypedIMDS{FakeIMDS(map[string]interface{}{
		"network/interfaces/macs/02:c5:f8:3e:6b:27/subnet-id": "subnet-0afaed81bf542db37",
	})}

	id, err := f.GetSubnetID(context.TODO(), "02:c5:f8:3e:6b:27")
	if assert.NoError(t, err) {
		assert.Equal(t, id, "subnet-0afaed81bf542db37")
	}
}

func TestGetSecurityGroupIDs(t *testing.T) {
	f := TypedIMDS{FakeIMDS(map[string]interface{}{
		"network/interfaces/macs/02:c5:f8:3e:6b:27/security-group-ids": "sg-00581e028df71bda8",
	})}

	list, err := f.GetSecurityGroupIDs(context.TODO(), "02:c5:f8:3e:6b:27")
	if assert.NoError(t, err) {
		assert.Equal(t, list, []string{"sg-00581e028df71bda8"})
	}
}

func TestGetLocalIPv4s(t *testing.T) {
	f := TypedIMDS{FakeIMDS(map[string]interface{}{
		"network/interfaces/macs/02:c5:f8:3e:6b:27/local-ipv4s": `10.0.114.236
10.0.120.181`,
	})}

	ips, err := f.GetLocalIPv4s(context.TODO(), "02:c5:f8:3e:6b:27")
	if assert.NoError(t, err) {
		assert.Equal(t, ips, []net.IP{net.IPv4(10, 0, 114, 236), net.IPv4(10, 0, 120, 181)})
	}

	_, err = f.GetLocalIPv4s(context.TODO(), "00:00:de:ad:be:ef")
	if assert.Error(t, err) {
		assert.True(t, IsNotFound(err))
	}
}

func TestGetIPv6s(t *testing.T) {
	f := TypedIMDS{FakeIMDS(map[string]interface{}{
		"network/interfaces/macs/02:c5:f8:3e:6b:27/ipv6s": `2001:db8::1
2001:db8::2`,
	})}

	ips, err := f.GetIPv6s(context.TODO(), "02:c5:f8:3e:6b:27")
	if assert.NoError(t, err) {
		assert.Equal(t, ips, []net.IP{net.ParseIP("2001:db8::1"), net.ParseIP("2001:db8::2")})
	}

	nov6 := TypedIMDS{FakeIMDS(map[string]interface{}{
		// NB: IMDS returns 404, not empty string :(
	})}

	ips, err = nov6.GetIPv6s(context.TODO(), "02:c5:f8:3e:6b:27")
	if assert.NoError(t, err) {
		assert.ElementsMatch(t, ips, []net.IP{})
	}

	// Note can't tell the difference between bad mac and no ipv6 :(
	ips, err = f.GetIPv6s(context.TODO(), "00:00:de:ad:be:ef")
	if assert.NoError(t, err) {
		assert.ElementsMatch(t, ips, []net.IP{})
	}
}

func TestGetSubnetIPv4CIDRBlock(t *testing.T) {
	f := TypedIMDS{FakeIMDS(map[string]interface{}{
		"network/interfaces/macs/02:c5:f8:3e:6b:27/subnet-ipv4-cidr-block": "10.0.64.0/18",
	})}

	ip, err := f.GetSubnetIPv4CIDRBlock(context.TODO(), "02:c5:f8:3e:6b:27")
	if assert.NoError(t, err) {
		assert.Equal(t, ip, net.IPNet{IP: net.IPv4(10, 0, 64, 0), Mask: net.CIDRMask(18, 32)})
	}
}

func TestGetVPCIPv4CIDRBlocks(t *testing.T) {
	f := TypedIMDS{FakeIMDS(map[string]interface{}{
		"network/interfaces/macs/02:c5:f8:3e:6b:27/vpc-ipv4-cidr-blocks": "10.0.0.0/16",
	})}

	ips, err := f.GetVPCIPv4CIDRBlocks(context.TODO(), "02:c5:f8:3e:6b:27")
	if assert.NoError(t, err) {
		assert.Equal(t, ips, []net.IPNet{{IP: net.IPv4(10, 0, 0, 0), Mask: net.CIDRMask(16, 32)}})
	}

	_, err = f.GetLocalIPv4s(context.TODO(), "00:00:de:ad:be:ef")
	if assert.Error(t, err) {
		assert.True(t, IsNotFound(err))
	}
}

func TestGetVPCIPv6CIDRBlocks(t *testing.T) {
	f := TypedIMDS{FakeIMDS(map[string]interface{}{
		"network/interfaces/macs/02:c5:f8:3e:6b:27/vpc-ipv6-cidr-blocks": "2001:db8::/64",
	})}

	ips, err := f.GetVPCIPv6CIDRBlocks(context.TODO(), "02:c5:f8:3e:6b:27")
	if assert.NoError(t, err) {
		assert.Equal(t, ips, []net.IPNet{{IP: net.ParseIP("2001:db8::"), Mask: net.CIDRMask(64, 128)}})
	}

	nov6 := TypedIMDS{FakeIMDS(map[string]interface{}{
		// NB: IMDS returns 404, not empty string :(
	})}

	ips, err = nov6.GetVPCIPv6CIDRBlocks(context.TODO(), "02:c5:f8:3e:6b:27")
	if assert.NoError(t, err) {
		assert.ElementsMatch(t, ips, []net.IPNet{})
	}

	_, err = f.GetLocalIPv4s(context.TODO(), "00:00:de:ad:be:ef")
	if assert.Error(t, err) {
		assert.True(t, IsNotFound(err))
	}
}
