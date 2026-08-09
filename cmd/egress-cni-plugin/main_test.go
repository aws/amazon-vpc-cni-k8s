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
	snatChainV6 = "CNI-E6-e335b08ae90730d0a659a"

	containerIDV4 = "containerId-123"
	containerIDV6 = "containerId-789"
)

func TestCmdAddV4(t *testing.T) {
	ctrl := gomock.NewController(t)

	args := &skel.CmdArgs{
		ContainerID: containerIDV4,
		IfName:      "eth0",
		StdinData: []byte(`{
				"cniVersion":"1.0.0",
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
					"cniVersion":"1.0.0",
					"interfaces":
						[
							{"name":"eni36e5b0ee702"},
							{"name":"eth0","sandbox":"/var/run/netns/cni-266298c1-b141-9c7f-f26b-97ff084f3fcc"},
							{"name":"dummy36e5b0ee702","mac":"0","sandbox":"0"}],
					"ips":
						[{"version":"6","interface":1,"address":"2600:1f16:828:c404:af46:9f44:d2ea:4569/128"}],
					"dns":{}
					},
				"type":"aws-cni",
				"vethPrefix":"eni"
		}`),
	}

	ec := egressContext{
		Procsys:       mock_procsyswrapper.NewMockProcSys(ctrl),
		Ns:            mock_nswrapper.NewMockNS(ctrl),
		NsPath:        "/var/run/netns/cni-xxxx",
		ArgsIfName:    args.IfName,
		IPTablesIface: mock_iptables.NewMockIPTablesIface(ctrl),
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
		fmt.Sprintf("nat %s -d 224.0.0.0/4 -j ACCEPT -m comment --comment name: \"aws-cni\" id: \"%s\"", snatChainV4, containerIDV4),
		fmt.Sprintf("nat %s -j SNAT --to-source 192.168.1.123 -m comment --comment name: \"aws-cni\" id: \"%s\" --random-fully", snatChainV4, containerIDV4),
		fmt.Sprintf("nat POSTROUTING -s 169.254.172.10 -j %s -m comment --comment name: \"aws-cni\" id: \"%s\"", snatChainV4, containerIDV4)}
	assert.EqualValues(t, expectIptablesRules, actualIptablesRules)

	expectRouteDel := []string{"route del: {Ifindex: 2 Dst: 169.254.172.0/22 Src: <nil> Gw: <nil> Flags: [] Table: 0 Realm: 0}"}
	assert.EqualValues(t, expectRouteDel, actualRouteDel)

	expectRouteAdd := []string{
		"route add: {Ifindex: 2 Dst: 169.254.172.1/32 Src: 169.254.172.10 Gw: <nil> Flags: [] Table: 0 Realm: 0}",
		"route add: {Ifindex: 2 Dst: 169.254.172.0/22 Src: 169.254.172.10 Gw: 169.254.172.1 Flags: [] Table: 0 Realm: 0}",
		"route add: {Ifindex: 100 Dst: 169.254.172.10/32 Src: <nil> Gw: <nil> Flags: [] Table: 0 Realm: 0}"}
	assert.EqualValues(t, expectRouteAdd, actualRouteAdd)

	// the unit test write some output string not ends with '\n' and this cause go runner unable to interpret that a test was run.
	// Adding a newline, keeps a clean output
	fmt.Println()
}

func TestCmdDelV4(t *testing.T) {
	ctrl := gomock.NewController(t)

	args := &skel.CmdArgs{
		ContainerID: containerIDV4,
		IfName:      "eth0",
		StdinData: []byte(`{
				"cniVersion":"1.0.0",
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
					"cniVersion":"1.0.0",
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

	ec := egressContext{
		Ns:            mock_nswrapper.NewMockNS(ctrl),
		NsPath:        "/var/run/netns/cni-xxxx",
		IPTablesIface: mock_iptables.NewMockIPTablesIface(ctrl),
		Ipam:          mock_ipamwrapper.NewMockHostIpam(ctrl),
		Link:          mock_netlinkwrapper.NewMockNetLink(ctrl),
	}

	var actualIptablesDel []string
	err := SetupDelExpectV4(ec, &actualIptablesDel)
	assert.Nil(t, err)

	err = del(args, &ec)
	assert.Nil(t, err)

	expectIptablesDel := []string{
		fmt.Sprintf("nat POSTROUTING -s 169.254.172.10 -j %s -m comment --comment name: \"aws-cni\" id: \"containerId-123\"", snatChainV4),
		fmt.Sprintf("clear chain nat %s", snatChainV4),
		fmt.Sprintf("del chain nat %s", snatChainV4)}
	assert.EqualValues(t, expectIptablesDel, actualIptablesDel)
}

func TestCmdAddV6(t *testing.T) {
	ctrl := gomock.NewController(t)

	args := &skel.CmdArgs{
		ContainerID: containerIDV6,
		IfName:      "eth0",
		StdinData: []byte(`{
				"cniVersion":"1.0.0",
				"mtu":"9001",
				"name":"aws-cni",
				"enabled":"true",
				"nodeIP": "2600::",
				"ipam": {"type":"host-local","ranges":[[{"subnet": "fd00::ac:00/118"}]],"routes":[{"dst":"::/0"}],"dataDir":"/run/cni/v4pd/egress-v6-ipam"},
				"pluginLogFile":"egress-plugin.log",
				"pluginLogLevel":"DEBUG",
				"podSGEnforcingMode":"strict",
				"prevResult":
					{
					"cniVersion":"1.0.0",
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

	ec := egressContext{
		Procsys:       mock_procsyswrapper.NewMockProcSys(ctrl),
		Ns:            mock_nswrapper.NewMockNS(ctrl),
		NsPath:        "/var/run/netns/cni-xxxx",
		ArgsIfName:    args.IfName,
		IPTablesIface: mock_iptables.NewMockIPTablesIface(ctrl),
		Ipam:          mock_ipamwrapper.NewMockHostIpam(ctrl),
		Link:          mock_netlinkwrapper.NewMockNetLink(ctrl),
		Veth:          mock_veth.NewMockVeth(ctrl),
	}

	var actualIptablesRules, actualRouteAdd, actualRouteReplace []string
	err := SetupAddExpectV6(ec, snatChainV6, &actualIptablesRules, &actualRouteAdd, &actualRouteReplace)
	assert.Nil(t, err)

	err = add(args, &ec)
	assert.Nil(t, err)

	expectIptablesRules := []string{
		fmt.Sprintf("nat %s -d ff00::/8 -j ACCEPT -m comment --comment name: \"aws-cni\" id: \"%s\"", snatChainV6, containerIDV6),
		fmt.Sprintf("nat %s -j SNAT --to-source 2600:: -m comment --comment name: \"aws-cni\" id: \"%s\" --random-fully", snatChainV6, containerIDV6),
		fmt.Sprintf("nat POSTROUTING -s fd00::10 -j %s -m comment --comment name: \"aws-cni\" id: \"%s\"", snatChainV6, containerIDV6)}
	assert.EqualValues(t, expectIptablesRules, actualIptablesRules)

	expectRouteAdd := []string{"{Ifindex: 100 Dst: fd00::10/128 Src: <nil> Gw: <nil> Flags: [] Table: 0 Realm: 0}"}
	assert.EqualValues(t, expectRouteAdd, actualRouteAdd)

	expectRouteReplace := []string{"{Ifindex: 2 Dst: ::/0 Src: <nil> Gw: fe80::10 Flags: [] Table: 0 Realm: 0}"}
	assert.EqualValues(t, expectRouteReplace, actualRouteReplace)

	// the unit test write some output string not ends with '\n' and this cause go runner unable to interpret that a test was run.
	// Adding a newline, keeps a clean output
	fmt.Println()
}

func TestCmdDelV6(t *testing.T) {
	ctrl := gomock.NewController(t)

	args := &skel.CmdArgs{
		ContainerID: containerIDV6,
		IfName:      "eth0",
		StdinData: []byte(`{
				"cniVersion":"1.0.0",
				"mtu":"9001",
				"name":"aws-cni",
				"enabled":"true",
				"nodeIP": "2600::",
				"ipam": {"type":"host-local","ranges":[[{"subnet": "fd00::ac:00/118"}]],"routes":[{"dst":"::/0"}],"dataDir":"/run/cni/v4pd/egress-v6-ipam"},
				"pluginLogFile":"egress-plugin.log",
				"pluginLogLevel":"DEBUG",
				"podSGEnforcingMode":"strict",
				"prevResult":
					{
					"cniVersion":"1.0.0",
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
	ec := egressContext{
		Ns:            mock_nswrapper.NewMockNS(ctrl),
		NsPath:        "/var/run/netns/cni-xxxx",
		Ipam:          mock_ipamwrapper.NewMockHostIpam(ctrl),
		Link:          mock_netlinkwrapper.NewMockNetLink(ctrl),
		IPTablesIface: mock_iptables.NewMockIPTablesIface(ctrl),
	}

	var actualIptablesDel []string
	err := SetupDelExpectV6(ec, snatChainV6, &actualIptablesDel)
	assert.Nil(t, err)

	err = del(args, &ec)
	assert.Nil(t, err)

	expectIptablesDel := []string{
		fmt.Sprintf("nat POSTROUTING -s fd00::10 -j %s -m comment --comment name: \"aws-cni\" id: \"%s\"", snatChainV6, containerIDV6),
		fmt.Sprintf("clear chain nat %s", snatChainV6),
		fmt.Sprintf("del chain nat %s", snatChainV6)}
	assert.EqualValues(t, expectIptablesDel, actualIptablesDel)
}
