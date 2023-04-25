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
	"fmt"
	"testing"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"

	mock_ipamwrapper "github.com/aws/amazon-vpc-cni-k8s/pkg/hostipamwrapper/mocks"
	mock_iptables "github.com/aws/amazon-vpc-cni-k8s/pkg/iptableswrapper/mocks"
	mock_netlinkwrapper "github.com/aws/amazon-vpc-cni-k8s/pkg/netlinkwrapper/mocks"
	mock_nswrapper "github.com/aws/amazon-vpc-cni-k8s/pkg/nswrapper/mocks"
	mock_procsyswrapper "github.com/aws/amazon-vpc-cni-k8s/pkg/procsyswrapper/mocks"
	mock_veth "github.com/aws/amazon-vpc-cni-k8s/pkg/vethwrapper/mocks"
)

const (
	snatChainV4 = "CNI-E4-5740307710ebfd5fb24f5"
)

func TestCmdAddV4(t *testing.T) {
	ctrl := gomock.NewController(t)

	args := &skel.CmdArgs{
		ContainerID: "containerId-123",
		IfName:      "eth0",
		StdinData: []byte(`{
				"cniVersion":"0.4.0",
				"mtu":"9001",
				"name":"aws-cni",
				"enabled":"true",
				"nodeIP": "192.168.1.123",
				"ipam": {"type":"host-local","ranges":[[{"subnet": "169.254.172.0/22"}]],"routes":[{"dst":"0.0.0.0"}],"dataDir":"/run/cni/v6pd/egress-v4-ipam"},
				"pluginLogFile":"egress-plugin.log",
				"pluginLogLevel":"DEBUG",
				"podSGEnforcingMode":"strict",
				"prevResult":
					{
					"cniVersion":"0.4.0",
					"interfaces":
						[
							{"name":"eni36e5b0ee702"},
							{"name":"eth0","sandbox":"/var/run/netns/cni-266298c1-b141-9c7f-f26b-97ff084f3fcc"},
							{"name":"dummy36e5b0ee702","mac":"0","sandbox":"0"}],
					"ips":
						[{"version":"4","interface":1,"address":"192.168.13.226/32"}],
					"dns":{}
					},
				"type":"aws-cni",
				"vethPrefix":"eni"
		}`),
	}

	ec := EgressContext{
		Procsys:       mock_procsyswrapper.NewMockProcSys(ctrl),
		Ns:            mock_nswrapper.NewMockNS(ctrl),
		NsPath:        "/var/run/netns/cni-xxxx",
		ArgsIfName:    args.IfName,
		IpTablesIface: mock_iptables.NewMockIPTablesIface(ctrl),
		Ipam:          mock_ipamwrapper.NewMockHostIpam(ctrl),
		Link:          mock_netlinkwrapper.NewMockNetLink(ctrl),
		Veth:          mock_veth.NewMockVeth(ctrl),
	}

	var actualIptablesRules, actualRouteAdd, actualRouteDel []string
	err := SetupAddExpectV4(ec, snatChainV4, &actualIptablesRules, &actualRouteAdd, &actualRouteDel)
	assert.Nil(t, err)

	err = add(args, &ec)
	assert.Nil(t, err)

	expectIptablesRules := []string{
		fmt.Sprintf("nat %s -d 224.0.0.0/4 -j ACCEPT -m comment --comment name: \"aws-cni\" id: \"containerId-123\"", snatChainV4),
		fmt.Sprintf("nat %s -j SNAT --to-source 192.168.1.123 -m comment --comment name: \"aws-cni\" id: \"containerId-123\" --random-fully", snatChainV4),
		fmt.Sprintf("nat POSTROUTING -s 169.254.172.10 -j %s -m comment --comment name: \"aws-cni\" id: \"containerId-123\"", snatChainV4)}
	assert.EqualValues(t, expectIptablesRules, actualIptablesRules)

	expectRouteDel := []string{"route del: {Ifindex: 2 Dst: 169.254.172.0/22 Src: <nil> Gw: <nil> Flags: [] Table: 0}"}
	assert.EqualValues(t, expectRouteDel, actualRouteDel)

	expectRouteAdd := []string{
		"route add: {Ifindex: 2 Dst: 169.254.172.1/32 Src: 169.254.172.10 Gw: <nil> Flags: [] Table: 0}",
		"route add: {Ifindex: 2 Dst: 169.254.172.0/22 Src: 169.254.172.10 Gw: 169.254.172.1 Flags: [] Table: 0}",
		"route add: {Ifindex: 100 Dst: 169.254.172.10/32 Src: <nil> Gw: <nil> Flags: [] Table: 0}"}
	assert.EqualValues(t, expectRouteAdd, actualRouteAdd)

	// the unit test write some output string not ends with '\n' and this cause go runner unable to interpret that a test was run.
	// Adding a newline, keeps a clean output
	fmt.Println()
}

func TestCmdDelV4(t *testing.T) {
	ctrl := gomock.NewController(t)

	args := &skel.CmdArgs{
		ContainerID: "containerId-123",
		IfName:      "eth0",
		StdinData: []byte(`{
				"cniVersion":"0.4.0",
				"mtu":"9001",
				"name":"aws-cni",
				"enabled":"true",
				"nodeIP": "192.168.1.123",
				"ipam": {"type":"host-local","ranges":[[{"subnet": "169.254.172.0/22"}]],"routes":[{"dst":"0.0.0.0"}],"dataDir":"/run/cni/v6pd/egress-v4-ipam"},
				"pluginLogFile":"egress-plugin.log",
				"pluginLogLevel":"DEBUG",
				"podSGEnforcingMode":"strict",
				"prevResult":
					{
					"cniVersion":"0.4.0",
					"interfaces":
						[
							{"name":"eni36e5b0ee702"},
							{"name":"eth0","sandbox":"/var/run/netns/cni-266298c1-b141-9c7f-f26b-97ff084f3fcc"},
							{"name":"dummy36e5b0ee702","mac":"0","sandbox":"0"}],
					"ips":
						[{"version":"4","interface":1,"address":"192.168.13.226/32"}],
					"dns":{}
					},
				"type":"aws-cni",
				"vethPrefix":"eni"
		}`),
	}

	ec := EgressContext{
		Ns:            mock_nswrapper.NewMockNS(ctrl),
		NsPath:        "/var/run/netns/cni-xxxx",
		IpTablesIface: mock_iptables.NewMockIPTablesIface(ctrl),
		Ipam:          mock_ipamwrapper.NewMockHostIpam(ctrl),
		Link:          mock_netlinkwrapper.NewMockNetLink(ctrl),
	}

	var actualLinkDel, actualIptablesDel []string
	err := SetupDelExpectV4(ec, &actualLinkDel, &actualIptablesDel)
	assert.Nil(t, err)

	err = del(args, &ec)
	assert.Nil(t, err)

	expectLinkDel := []string{"link del - name: v4if0"}
	assert.EqualValues(t, expectLinkDel, actualLinkDel)

	expectIptablesDel := []string{
		fmt.Sprintf("nat POSTROUTING -s 169.254.172.10 -j %s -m comment --comment name: \"aws-cni\" id: \"containerId-123\"", snatChainV4),
		fmt.Sprintf("clear chain nat %s", snatChainV4),
		fmt.Sprintf("del chain nat %s", snatChainV4)}
	assert.EqualValues(t, expectIptablesDel, actualIptablesDel)
}
