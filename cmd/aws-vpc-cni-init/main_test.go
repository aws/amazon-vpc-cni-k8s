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

package main

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vishvananda/netlink"
)

func TestFindLinkNameByMAC(t *testing.T) {
	primaryMAC, _ := net.ParseMAC("0a:1b:2c:3d:4e:5f")
	otherMAC, _ := net.ParseMAC("0a:1b:2c:3d:4e:60")

	// A pod's host-side veth that carries the primary ENI's address is listed
	// before the ENI itself.
	hostVeth := &netlink.Veth{LinkAttrs: netlink.LinkAttrs{Name: "eni0123456789a", HardwareAddr: primaryMAC}}
	secondaryENI := &netlink.Device{LinkAttrs: netlink.LinkAttrs{Name: "eth1", HardwareAddr: otherMAC}}
	primaryENI := &netlink.Device{LinkAttrs: netlink.LinkAttrs{Name: "eth0", HardwareAddr: primaryMAC}}

	tests := []struct {
		name  string
		links []netlink.Link
		want  string
	}{
		{
			name:  "returns the link with the address",
			links: []netlink.Link{secondaryENI, primaryENI},
			want:  "eth0",
		},
		{
			name:  "skips a veth with the same address",
			links: []netlink.Link{hostVeth, secondaryENI, primaryENI},
			want:  "eth0",
		},
		{
			name:  "no link when only a veth has the address",
			links: []netlink.Link{hostVeth, secondaryENI},
			want:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, findLinkNameByMAC(tt.links, primaryMAC.String()))
		})
	}
}
