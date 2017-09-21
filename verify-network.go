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

package main

import (
	"fmt"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/networkutils"
	"github.com/aws/amazon-vpc-cni-k8s/plugins/routed-eni/driver"
	"net"
)

const (
	// verify SetupENINetwork
	// manually verify  ip route show table eni-2
	// have corret default route out eth2
	// && ip route show does NOT default gw over eth2 on main table
	eniDeviceNum     = 1 // eth1
	eniIP            = "10.0.77.104"
	eniMAC           = "02:7e:b6:0e:75:70"
	updatedENISubnet = "10.0.64.0/19"
	eniSubnet        = "10.0.1.0/24"

	// verify SetupNodeNetwork
	// manually verify iptables-save have correct NAT rule
	nodeIP  = "10.0.93.145"
	vpcCIDR = "10.0.0.0/16"
)

type podSpec struct {
	VethName string
	EthName  string
	IP       string
	NS       string
	Table    int
}

func main() {
	hdlr := networkutils.New()
	drv := driver.New()

	hdlr.SetupENINetwork(eniIP, eniMAC, eniDeviceNum, eniSubnet)

	hdlr.SetupENINetwork(eniIP, eniMAC, eniDeviceNum, updatedENISubnet)

	ip := net.ParseIP(nodeIP)

	_, cidr, _ := net.ParseCIDR(vpcCIDR)
	err := hdlr.SetupHostNetwork(cidr, &ip)

	fmt.Printf("SetupNodeNetwork %v", err)

	// verify SetupPodNetwork
	// TODO: make sure manually  ip rule add not to <vpc'subnet> table main priority 1024
	// manually verify ping between
	// - pod to pod on same ENI
	// - pod to pod on different ENs
	// - pod to vpc's address on different node
	// - pod to www.yahoo.com
	//
	PodList := []podSpec{
		podSpec{
			VethName: "veth-0",
			EthName:  "eth0",
			IP:       "10.0.64.219",
			NS:       "/proc/7058/ns/net",
			Table:    0,
		},
		podSpec{
			VethName: "veth-1",
			EthName:  "eth0",
			IP:       "10.0.92.55",
			NS:       "/proc/7114/ns/net",
			Table:    1,
		},
		podSpec{
			VethName: "veth-2",
			EthName:  "eth0",
			IP:       "10.0.71.56",
			NS:       "/proc/7163/ns/net",
			Table:    1,
		},
	}

	for _, pod := range PodList {
		addr := &net.IPNet{
			IP:   net.ParseIP(pod.IP),
			Mask: net.IPv4Mask(255, 255, 255, 255),
		}
		err = drv.SetupNS(pod.VethName, pod.EthName, pod.NS, addr, pod.Table)
		fmt.Printf("SetupPodNetwork Veth %s  err %v", pod.VethName, err)
	}

}
