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

package driver

import (
	"net"
	"syscall"
	"testing"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/cninswrapper/mock_ns"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/netlinkwrapper/mock_netlink"
	mock_netlinkwrapper "github.com/aws/amazon-vpc-cni-k8s/pkg/netlinkwrapper/mocks"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/networkutils"
	mock_nswrapper "github.com/aws/amazon-vpc-cni-k8s/pkg/nswrapper/mocks"
	mock_procsyswrapper "github.com/aws/amazon-vpc-cni-k8s/pkg/procsyswrapper/mocks"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/sgpp"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
	"github.com/golang/mock/gomock"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

var testLogCfg = logger.Configuration{
	LogLevel:    "Debug",
	LogLocation: "stdout",
}

var testLogger = logger.New(&testLogCfg)

func Test_linuxNetwork_SetupPodNetwork(t *testing.T) {
	hostVethWithIndex9 := &netlink.Veth{
		LinkAttrs: netlink.LinkAttrs{
			Name:  "eni8ea2c11fe35",
			Index: 9,
		},
	}
	containerAddr := &net.IPNet{
		IP:   net.ParseIP("192.168.100.42"),
		Mask: net.CIDRMask(32, 32),
	}

	toContainerRule := netlink.NewRule()
	toContainerRule.Dst = containerAddr
	toContainerRule.Priority = networkutils.ToContainerRulePriority
	toContainerRule.Table = unix.RT_TABLE_MAIN

	fromContainerRuleForRTTable4 := netlink.NewRule()
	fromContainerRuleForRTTable4.Src = containerAddr
	fromContainerRuleForRTTable4.Priority = networkutils.FromPodRulePriority
	fromContainerRuleForRTTable4.Table = 4

	type linkByNameCall struct {
		linkName string
		link     netlink.Link
		err      error
	}
	type linkDelCall struct {
		link netlink.Link
		err  error
	}
	type linkSetupCall struct {
		link netlink.Link
		err  error
	}
	type routeReplaceCall struct {
		route *netlink.Route
		err   error
	}
	type ruleAddCall struct {
		rule *netlink.Rule
		err  error
	}
	type withNetNSPathCall struct {
		netNSPath string
		err       error
	}
	type procSysSetCall struct {
		key   string
		value string
		err   error
	}

	type fields struct {
		linkByNameCalls    []linkByNameCall
		linkDelCalls       []linkDelCall
		linkSetupCalls     []linkSetupCall
		withNetNSPathCalls []withNetNSPathCall
		procSysSetCalls    []procSysSetCall
		routeReplaceCalls  []routeReplaceCall
		ruleAddCalls       []ruleAddCall
	}
	type args struct {
		hostVethName string
		contVethName string
		netnsPath    string
		ipAddr       *net.IPNet
		deviceNumber int
		mtu          int
		routeTableId int
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr error
	}{
		{
			name: "successfully setup pod network - pod sponsored by eth0",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "eni8ea2c11fe35",
						err:      errors.New("not exists"),
					},
					{
						linkName: "eni8ea2c11fe35",
						link:     hostVethWithIndex9,
					},
				},
				linkSetupCalls: []linkSetupCall{
					{
						link: hostVethWithIndex9,
					},
				},
				routeReplaceCalls: []routeReplaceCall{
					{
						route: &netlink.Route{
							LinkIndex: hostVethWithIndex9.Index,
							Scope:     netlink.SCOPE_LINK,
							Dst:       containerAddr,
							Table:     unix.RT_TABLE_MAIN,
						},
					},
				},
				ruleAddCalls: []ruleAddCall{
					{
						rule: toContainerRule,
					},
				},
				withNetNSPathCalls: []withNetNSPathCall{
					{
						netNSPath: "/proc/42/ns/net",
					},
				},
				procSysSetCalls: []procSysSetCall{
					{
						key:   "net/ipv6/conf/eni8ea2c11fe35/accept_ra",
						value: "0",
					},
					{
						key:   "net/ipv6/conf/eni8ea2c11fe35/accept_redirects",
						value: "1",
					},
					{
						key:   "net/ipv6/conf/eni8ea2c11fe35/forwarding",
						value: "0",
					},
				},
			},
			args: args{
				hostVethName: "eni8ea2c11fe35",
				contVethName: "eth0",
				netnsPath:    "/proc/42/ns/net",
				ipAddr:       containerAddr, // v4 address
				deviceNumber: 0,
				routeTableId: 1,
				mtu:          9001,
			},
		},
		{
			name: "successfully setup pod network - pod sponsored by eth3",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "eni8ea2c11fe35",
						err:      errors.New("not exists"),
					},
					{
						linkName: "eni8ea2c11fe35",
						link:     hostVethWithIndex9,
					},
				},
				linkSetupCalls: []linkSetupCall{
					{
						link: hostVethWithIndex9,
					},
				},
				routeReplaceCalls: []routeReplaceCall{
					{
						route: &netlink.Route{
							LinkIndex: hostVethWithIndex9.Index,
							Scope:     netlink.SCOPE_LINK,
							Dst:       containerAddr,
							Table:     unix.RT_TABLE_MAIN,
						},
					},
				},
				ruleAddCalls: []ruleAddCall{
					{
						rule: toContainerRule,
					},
					{
						rule: fromContainerRuleForRTTable4,
					},
				},
				withNetNSPathCalls: []withNetNSPathCall{
					{
						netNSPath: "/proc/42/ns/net",
					},
				},
				procSysSetCalls: []procSysSetCall{
					{
						key:   "net/ipv6/conf/eni8ea2c11fe35/accept_ra",
						value: "0",
					},
					{
						key:   "net/ipv6/conf/eni8ea2c11fe35/accept_redirects",
						value: "1",
					},
					{
						key:   "net/ipv6/conf/eni8ea2c11fe35/forwarding",
						value: "0",
					},
				},
			},
			args: args{
				hostVethName: "eni8ea2c11fe35",
				contVethName: "eth0",
				netnsPath:    "/proc/42/ns/net",
				ipAddr:       containerAddr, // v4 address
				deviceNumber: 3,
				routeTableId: 4,
				mtu:          9001,
			},
		},
		{
			name: "failed to setup vethPair",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "eni8ea2c11fe35",
						err:      errors.New("not exists"),
					},
				},
				withNetNSPathCalls: []withNetNSPathCall{
					{
						netNSPath: "/proc/42/ns/net",
						err:       errors.New("some error"),
					},
				},
			},
			args: args{
				hostVethName: "eni8ea2c11fe35",
				contVethName: "eth0",
				netnsPath:    "/proc/42/ns/net",
				ipAddr:       containerAddr, // v4 address
				deviceNumber: 3,
				routeTableId: 4,
				mtu:          9001,
			},
			wantErr: errors.New("SetupPodNetwork: failed to setup veth pair: failed to setup veth network: some error"),
		},
		{
			name: "failed to setup container route",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "eni8ea2c11fe35",
						err:      errors.New("not exists"),
					},
					{
						linkName: "eni8ea2c11fe35",
						link:     hostVethWithIndex9,
					},
				},
				linkSetupCalls: []linkSetupCall{
					{
						link: hostVethWithIndex9,
					},
				},
				routeReplaceCalls: []routeReplaceCall{
					{
						route: &netlink.Route{
							LinkIndex: hostVethWithIndex9.Index,
							Scope:     netlink.SCOPE_LINK,
							Dst:       containerAddr,
							Table:     unix.RT_TABLE_MAIN,
						},
						err: errors.New("some error"),
					},
				},
				withNetNSPathCalls: []withNetNSPathCall{
					{
						netNSPath: "/proc/42/ns/net",
					},
				},
				procSysSetCalls: []procSysSetCall{
					{
						key:   "net/ipv6/conf/eni8ea2c11fe35/accept_ra",
						value: "0",
					},
					{
						key:   "net/ipv6/conf/eni8ea2c11fe35/accept_redirects",
						value: "1",
					},
					{
						key:   "net/ipv6/conf/eni8ea2c11fe35/forwarding",
						value: "0",
					},
				},
			},
			args: args{
				hostVethName: "eni8ea2c11fe35",
				contVethName: "eth0",
				netnsPath:    "/proc/42/ns/net",
				ipAddr:       containerAddr, // v4 Address
				deviceNumber: 3,
				routeTableId: 4,
				mtu:          9001,
			},
			wantErr: errors.New("SetupPodNetwork: unable to setup IP based container routes and rules: failed to setup container route, containerAddr=192.168.100.42/32, hostVeth=eni8ea2c11fe35, rtTable=main: some error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			netLink := mock_netlinkwrapper.NewMockNetLink(ctrl)
			netLink.EXPECT().NewRule().DoAndReturn(func() *netlink.Rule { return netlink.NewRule() }).AnyTimes()
			for _, call := range tt.fields.linkByNameCalls {
				netLink.EXPECT().LinkByName(call.linkName).Return(call.link, call.err)
			}
			for _, call := range tt.fields.linkDelCalls {
				netLink.EXPECT().LinkDel(call.link).Return(call.err)
			}
			for _, call := range tt.fields.linkSetupCalls {
				netLink.EXPECT().LinkSetUp(call.link).Return(call.err)
			}
			for _, call := range tt.fields.routeReplaceCalls {
				netLink.EXPECT().RouteReplace(call.route).Return(call.err)
			}
			for _, call := range tt.fields.ruleAddCalls {
				netLink.EXPECT().RuleAdd(call.rule).Return(call.err)
			}

			ns := mock_nswrapper.NewMockNS(ctrl)
			for _, call := range tt.fields.withNetNSPathCalls {
				// we just assume the createVethContext executes, the logic of createVethContext will be tested by createVethContext itself.
				ns.EXPECT().WithNetNSPath(call.netNSPath, gomock.Any()).Return(call.err)
			}

			procSys := mock_procsyswrapper.NewMockProcSys(ctrl)
			for _, call := range tt.fields.procSysSetCalls {
				procSys.EXPECT().Set(call.key, call.value).Return(call.err)
			}

			n := &linuxNetwork{
				netLink: netLink,
				ns:      ns,
				procSys: procSys,
			}
			vIfMetadata := []VirtualInterfaceMetadata{
				{
					IPAddress:         tt.args.ipAddr,
					DeviceNumber:      tt.args.deviceNumber,
					RouteTable:        tt.args.routeTableId,
					HostVethName:      tt.args.hostVethName,
					ContainerVethName: tt.args.contVethName,
				},
			}

			err := n.SetupPodNetwork(vIfMetadata, tt.args.netnsPath, tt.args.mtu, testLogger)
			if tt.wantErr != nil {
				assert.EqualError(t, err, tt.wantErr.Error())
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func Test_linuxNetwork_TeardownPodNetwork(t *testing.T) {
	containerAddr := &net.IPNet{
		IP:   net.ParseIP("192.168.100.42"),
		Mask: net.CIDRMask(32, 32),
	}

	toContainerRoute := &netlink.Route{
		Scope: netlink.SCOPE_LINK,
		Dst:   containerAddr,
		Table: unix.RT_TABLE_MAIN,
	}
	toContainerRule := netlink.NewRule()
	toContainerRule.Dst = containerAddr
	toContainerRule.Priority = networkutils.ToContainerRulePriority
	toContainerRule.Table = unix.RT_TABLE_MAIN

	fromContainerRuleForRTTable4 := netlink.NewRule()
	fromContainerRuleForRTTable4.Src = containerAddr
	fromContainerRuleForRTTable4.Priority = networkutils.FromPodRulePriority
	fromContainerRuleForRTTable4.Table = 4
	type routeDelCall struct {
		route *netlink.Route
		err   error
	}
	type ruleDelCall struct {
		rule *netlink.Rule
		err  error
	}
	type fields struct {
		routeDelCalls []routeDelCall
		ruleDelCalls  []ruleDelCall
	}

	type args struct {
		containerAddr *net.IPNet
		deviceNumber  int
		routeTableId  int
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr error
	}{
		{
			name: "successfully teardown pod network - pod sponsored by eth0",
			fields: fields{
				routeDelCalls: []routeDelCall{
					{
						route: toContainerRoute,
					},
				},
				ruleDelCalls: []ruleDelCall{
					{
						rule: toContainerRule,
					},
				},
			},
			args: args{
				containerAddr: containerAddr,
				deviceNumber:  0,
				routeTableId:  1,
			},
		},
		{
			name: "successfully teardown pod network - pod sponsored by eth3",
			fields: fields{
				routeDelCalls: []routeDelCall{
					{
						route: toContainerRoute,
					},
				},
				ruleDelCalls: []ruleDelCall{
					{
						rule: toContainerRule,
					},
					{
						rule: fromContainerRuleForRTTable4,
					},
					{
						rule: fromContainerRuleForRTTable4,
						err:  syscall.ENOENT,
					},
				},
			},
			args: args{
				containerAddr: containerAddr,
				deviceNumber:  3,
				routeTableId:  4,
			},
		},
		{
			name: "failed to delete toContainer rule",
			fields: fields{
				ruleDelCalls: []ruleDelCall{
					{
						rule: toContainerRule,
						err:  errors.New("some error"),
					},
				},
			},
			args: args{
				containerAddr: containerAddr,
				deviceNumber:  3,
				routeTableId:  4,
			},
			wantErr: errors.New("TeardownPodNetwork: unable to teardown IP based container routes and rules: failed to delete toContainer rule, containerAddr=192.168.100.42/32, rtTable=main: some error"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			netLink := mock_netlinkwrapper.NewMockNetLink(ctrl)
			netLink.EXPECT().NewRule().DoAndReturn(func() *netlink.Rule { return netlink.NewRule() }).AnyTimes()
			for _, call := range tt.fields.routeDelCalls {
				netLink.EXPECT().RouteDel(call.route).Return(call.err)
			}
			for _, call := range tt.fields.ruleDelCalls {
				netLink.EXPECT().RuleDel(call.rule).Return(call.err)
			}

			n := &linuxNetwork{
				netLink: netLink,
			}
			vIfMetadata := []VirtualInterfaceMetadata{
				{
					IPAddress:    tt.args.containerAddr,
					DeviceNumber: tt.args.deviceNumber,
					RouteTable:   tt.args.routeTableId,
				},
			}
			err := n.TeardownPodNetwork(vIfMetadata, testLogger)
			if tt.wantErr != nil {
				assert.EqualError(t, err, tt.wantErr.Error())
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func Test_linuxNetwork_SetupBranchENIPodNetwork(t *testing.T) {
	vlanID := 7
	eniMac := "00:00:5e:00:53:af"
	subnetGW := "192.168.120.1"
	subnetV6GW := "2600::"
	parentIfIndex := 3

	hostVethWithIndex9 := &netlink.Veth{
		LinkAttrs: netlink.LinkAttrs{
			Name:  "eni8ea2c11fe35",
			Index: 9,
		},
	}
	vlanLinkPostAddWithIndex11 := buildVlanLink("vlan.eth.7", vlanID, parentIfIndex, eniMac)
	vlanLinkPostAddWithIndex11.Index = 11
	containerAddr := &net.IPNet{
		IP:   net.ParseIP("192.168.100.42"),
		Mask: net.CIDRMask(32, 32),
	}
	containerV6Addr := &net.IPNet{
		IP:   net.ParseIP("2600::2"),
		Mask: net.CIDRMask(128, 128),
	}

	oldFromHostVethRule := netlink.NewRule()
	oldFromHostVethRule.IifName = "eni8ea2c11fe35"
	oldFromHostVethRule.Priority = networkutils.VlanRulePriority

	oldFromHostVethV6Rule := netlink.NewRule()
	oldFromHostVethV6Rule.IifName = "eni8ea2c11fe35"
	oldFromHostVethV6Rule.Priority = networkutils.VlanRulePriority
	oldFromHostVethV6Rule.Family = netlink.FAMILY_V6

	fromHostVlanRule := netlink.NewRule()
	fromHostVlanRule.IifName = vlanLinkPostAddWithIndex11.Name
	fromHostVlanRule.Priority = networkutils.VlanRulePriority
	fromHostVlanRule.Table = 107

	fromHostVlanV6Rule := netlink.NewRule()
	fromHostVlanV6Rule.IifName = vlanLinkPostAddWithIndex11.Name
	fromHostVlanV6Rule.Priority = networkutils.VlanRulePriority
	fromHostVlanV6Rule.Table = 107
	fromHostVlanV6Rule.Family = netlink.FAMILY_V6

	fromHostVethRule := netlink.NewRule()
	fromHostVethRule.IifName = hostVethWithIndex9.Name
	fromHostVethRule.Priority = networkutils.VlanRulePriority
	fromHostVethRule.Table = 107

	fromHostVethV6Rule := netlink.NewRule()
	fromHostVethV6Rule.IifName = hostVethWithIndex9.Name
	fromHostVethV6Rule.Priority = networkutils.VlanRulePriority
	fromHostVethV6Rule.Table = 107
	fromHostVethV6Rule.Family = netlink.FAMILY_V6

	toContainerRule := netlink.NewRule()
	toContainerRule.Dst = containerAddr
	toContainerRule.Priority = networkutils.ToContainerRulePriority
	toContainerRule.Table = unix.RT_TABLE_MAIN

	toContainerV6Rule := netlink.NewRule()
	toContainerV6Rule.Dst = containerV6Addr
	toContainerV6Rule.Priority = networkutils.ToContainerRulePriority
	toContainerV6Rule.Table = unix.RT_TABLE_MAIN

	fromContainerRule := netlink.NewRule()
	fromContainerRule.Src = containerAddr
	fromContainerRule.Priority = networkutils.FromPodRulePriority
	fromContainerRule.Table = 107

	fromContainerV6Rule := netlink.NewRule()
	fromContainerV6Rule.Src = containerV6Addr
	fromContainerV6Rule.Priority = networkutils.FromPodRulePriority
	fromContainerV6Rule.Table = 107

	type linkByNameCall struct {
		linkName string
		link     netlink.Link
		err      error
	}
	type linkAddCall struct {
		link      netlink.Link
		linkIndex int
		err       error
	}
	type linkDelCall struct {
		link netlink.Link
		err  error
	}
	type linkSetupCall struct {
		link netlink.Link
		err  error
	}
	type routeReplaceCall struct {
		route *netlink.Route
		err   error
	}
	type ruleAddCall struct {
		rule *netlink.Rule
		err  error
	}
	type ruleDelCall struct {
		rule *netlink.Rule
		err  error
	}

	type withNetNSPathCall struct {
		netNSPath string
		err       error
	}
	type procSysSetCall struct {
		key   string
		value string
		err   error
	}

	type fields struct {
		linkByNameCalls    []linkByNameCall
		linkAddCalls       []linkAddCall
		linkDelCalls       []linkDelCall
		linkSetupCalls     []linkSetupCall
		routeReplaceCalls  []routeReplaceCall
		ruleAddCalls       []ruleAddCall
		ruleDelCalls       []ruleDelCall
		withNetNSPathCalls []withNetNSPathCall
		procSysSetCalls    []procSysSetCall
	}
	type args struct {
		hostVethName       string
		contVethName       string
		netnsPath          string
		ipAddr             *net.IPNet
		vlanID             int
		eniMAC             string
		subnetGW           string
		parentIfIndex      int
		mtu                int
		podSGEnforcingMode sgpp.EnforcingMode
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr error
	}{
		{
			name: "successfully setup pod network - traffic enforced with strict mode",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "eni8ea2c11fe35",
						err:      errors.New("not exists"),
					},
					{
						linkName: "eni8ea2c11fe35",
						link:     hostVethWithIndex9,
					},
					{
						linkName: "vlan.eth.7",
						err:      errors.New("not exists"),
					},
				},
				linkAddCalls: []linkAddCall{
					{
						link:      buildVlanLink("vlan.eth.7", vlanID, parentIfIndex, eniMac),
						linkIndex: 11,
					},
				},
				linkSetupCalls: []linkSetupCall{
					{
						link: hostVethWithIndex9,
					},
					{
						link: vlanLinkPostAddWithIndex11,
					},
				},
				routeReplaceCalls: []routeReplaceCall{
					{
						route: &netlink.Route{
							LinkIndex: vlanLinkPostAddWithIndex11.Index,
							Dst:       &net.IPNet{IP: net.ParseIP(subnetGW), Mask: net.CIDRMask(32, 32)},
							Scope:     netlink.SCOPE_LINK,
							Table:     107,
						},
					},
					{
						route: &netlink.Route{
							LinkIndex: vlanLinkPostAddWithIndex11.Index,
							Dst:       &net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(0, 32)},
							Scope:     netlink.SCOPE_UNIVERSE,
							Gw:        net.ParseIP(subnetGW),
							Table:     107,
						},
					},
					{
						route: &netlink.Route{
							LinkIndex: hostVethWithIndex9.Index,
							Scope:     netlink.SCOPE_LINK,
							Dst:       containerAddr,
							Table:     107,
						},
					},
				},
				ruleAddCalls: []ruleAddCall{
					{
						rule: fromHostVlanRule,
					},
					{
						rule: fromHostVethRule,
					},
				},
				ruleDelCalls: []ruleDelCall{
					{
						rule: oldFromHostVethRule,
						err:  syscall.ENOENT,
					},
				},
				withNetNSPathCalls: []withNetNSPathCall{
					{
						netNSPath: "/proc/42/ns/net",
					},
				},
				procSysSetCalls: []procSysSetCall{
					{
						key:   "net/ipv6/conf/eni8ea2c11fe35/accept_ra",
						value: "0",
					},
					{
						key:   "net/ipv6/conf/eni8ea2c11fe35/accept_redirects",
						value: "1",
					},
					{
						key:   "net/ipv6/conf/eni8ea2c11fe35/forwarding",
						value: "0",
					},
					{
						key:   "net/ipv6/conf/vlan.eth.7/accept_ra",
						value: "0",
					},
					{
						key:   "net/ipv6/conf/vlan.eth.7/accept_redirects",
						value: "1",
					},
					{
						key:   "net/ipv6/conf/vlan.eth.7/forwarding",
						value: "0",
					},
				},
			},
			args: args{
				hostVethName:       "eni8ea2c11fe35",
				contVethName:       "eth0",
				netnsPath:          "/proc/42/ns/net",
				ipAddr:             containerAddr,
				vlanID:             vlanID,
				eniMAC:             eniMac,
				subnetGW:           subnetGW,
				parentIfIndex:      parentIfIndex,
				mtu:                9001,
				podSGEnforcingMode: sgpp.EnforcingModeStrict,
			},
		},
		{
			name: "successfully setup IPv6 pod network - traffic enforced with strict mode",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "eni8ea2c11fe35",
						err:      errors.New("not exists"),
					},
					{
						linkName: "eni8ea2c11fe35",
						link:     hostVethWithIndex9,
					},
					{
						linkName: "vlan.eth.7",
						err:      errors.New("not exists"),
					},
				},
				linkAddCalls: []linkAddCall{
					{
						link:      buildVlanLink("vlan.eth.7", vlanID, parentIfIndex, eniMac),
						linkIndex: 11,
					},
				},
				linkSetupCalls: []linkSetupCall{
					{
						link: hostVethWithIndex9,
					},
					{
						link: vlanLinkPostAddWithIndex11,
					},
				},
				routeReplaceCalls: []routeReplaceCall{
					{
						route: &netlink.Route{
							LinkIndex: vlanLinkPostAddWithIndex11.Index,
							Dst:       &net.IPNet{IP: net.ParseIP(subnetV6GW), Mask: net.CIDRMask(128, 128)},
							Scope:     netlink.SCOPE_LINK,
							Table:     107,
						},
					},
					{
						route: &netlink.Route{
							LinkIndex: vlanLinkPostAddWithIndex11.Index,
							Dst:       &net.IPNet{IP: net.IPv6zero, Mask: net.CIDRMask(0, 128)},
							Scope:     netlink.SCOPE_UNIVERSE,
							Gw:        net.ParseIP(subnetV6GW),
							Table:     107,
						},
					},
					{
						route: &netlink.Route{
							LinkIndex: hostVethWithIndex9.Index,
							Scope:     netlink.SCOPE_LINK,
							Dst:       containerV6Addr,
							Table:     107,
						},
					},
				},
				ruleAddCalls: []ruleAddCall{
					{
						rule: fromHostVlanV6Rule,
					},
					{
						rule: fromHostVethV6Rule,
					},
				},
				ruleDelCalls: []ruleDelCall{
					{
						rule: oldFromHostVethV6Rule,
						err:  syscall.ENOENT,
					},
				},
				withNetNSPathCalls: []withNetNSPathCall{
					{
						netNSPath: "/proc/42/ns/net",
					},
				},
				procSysSetCalls: []procSysSetCall{
					{
						key:   "net/ipv6/conf/eni8ea2c11fe35/accept_ra",
						value: "0",
					},
					{
						key:   "net/ipv6/conf/eni8ea2c11fe35/accept_redirects",
						value: "1",
					},
					{
						key:   "net/ipv6/conf/eni8ea2c11fe35/forwarding",
						value: "0",
					},
					{
						key:   "net/ipv6/conf/vlan.eth.7/accept_ra",
						value: "0",
					},
					{
						key:   "net/ipv6/conf/vlan.eth.7/accept_redirects",
						value: "1",
					},
					{
						key:   "net/ipv6/conf/vlan.eth.7/forwarding",
						value: "0",
					},
				},
			},
			args: args{
				hostVethName:       "eni8ea2c11fe35",
				contVethName:       "eth0",
				netnsPath:          "/proc/42/ns/net",
				ipAddr:             containerV6Addr, // v6Address
				vlanID:             vlanID,
				eniMAC:             eniMac,
				subnetGW:           subnetV6GW,
				parentIfIndex:      parentIfIndex,
				mtu:                9001,
				podSGEnforcingMode: sgpp.EnforcingModeStrict,
			},
		},
		{
			name: "successfully setup pod network - traffic enforced with standard mode",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "eni8ea2c11fe35",
						err:      errors.New("not exists"),
					},
					{
						linkName: "eni8ea2c11fe35",
						link:     hostVethWithIndex9,
					},
					{
						linkName: "vlan.eth.7",
						err:      errors.New("not exists"),
					},
				},
				linkAddCalls: []linkAddCall{
					{
						link:      buildVlanLink("vlan.eth.7", vlanID, parentIfIndex, eniMac),
						linkIndex: 11,
					},
				},
				linkSetupCalls: []linkSetupCall{
					{
						link: hostVethWithIndex9,
					},
					{
						link: vlanLinkPostAddWithIndex11,
					},
				},
				routeReplaceCalls: []routeReplaceCall{
					{
						route: &netlink.Route{
							LinkIndex: vlanLinkPostAddWithIndex11.Index,
							Dst:       &net.IPNet{IP: net.ParseIP(subnetGW), Mask: net.CIDRMask(32, 32)},
							Scope:     netlink.SCOPE_LINK,
							Table:     107,
						},
					},
					{
						route: &netlink.Route{
							LinkIndex: vlanLinkPostAddWithIndex11.Index,
							Dst:       &net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(0, 32)},
							Scope:     netlink.SCOPE_UNIVERSE,
							Gw:        net.ParseIP(subnetGW),
							Table:     107,
						},
					},
					{
						route: &netlink.Route{
							LinkIndex: hostVethWithIndex9.Index,
							Scope:     netlink.SCOPE_LINK,
							Dst:       containerAddr,
							Table:     unix.RT_TABLE_MAIN,
						},
					},
				},
				ruleAddCalls: []ruleAddCall{
					{
						rule: toContainerRule,
					},
					{
						rule: fromContainerRule,
					},
				},
				ruleDelCalls: []ruleDelCall{
					{
						rule: oldFromHostVethRule,
						err:  syscall.ENOENT,
					},
				},
				withNetNSPathCalls: []withNetNSPathCall{
					{
						netNSPath: "/proc/42/ns/net",
					},
				},
				procSysSetCalls: []procSysSetCall{
					{
						key:   "net/ipv6/conf/eni8ea2c11fe35/accept_ra",
						value: "0",
					},
					{
						key:   "net/ipv6/conf/eni8ea2c11fe35/accept_redirects",
						value: "1",
					},
					{
						key:   "net/ipv6/conf/eni8ea2c11fe35/forwarding",
						value: "0",
					},
					{
						key:   "net/ipv6/conf/vlan.eth.7/accept_ra",
						value: "0",
					},
					{
						key:   "net/ipv6/conf/vlan.eth.7/accept_redirects",
						value: "1",
					},
					{
						key:   "net/ipv6/conf/vlan.eth.7/forwarding",
						value: "0",
					},
				},
			},
			args: args{
				hostVethName:       "eni8ea2c11fe35",
				contVethName:       "eth0",
				netnsPath:          "/proc/42/ns/net",
				ipAddr:             containerAddr, //v4 Address
				vlanID:             vlanID,
				eniMAC:             eniMac,
				subnetGW:           subnetGW,
				parentIfIndex:      parentIfIndex,
				mtu:                9001,
				podSGEnforcingMode: sgpp.EnforcingModeStandard,
			},
		},
		{
			name: "successfully setup v6 pod network - traffic enforced with standard mode",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "eni8ea2c11fe35",
						err:      errors.New("not exists"),
					},
					{
						linkName: "eni8ea2c11fe35",
						link:     hostVethWithIndex9,
					},
					{
						linkName: "vlan.eth.7",
						err:      errors.New("not exists"),
					},
				},
				linkAddCalls: []linkAddCall{
					{
						link:      buildVlanLink("vlan.eth.7", vlanID, parentIfIndex, eniMac),
						linkIndex: 11,
					},
				},
				linkSetupCalls: []linkSetupCall{
					{
						link: hostVethWithIndex9,
					},
					{
						link: vlanLinkPostAddWithIndex11,
					},
				},
				routeReplaceCalls: []routeReplaceCall{
					{
						route: &netlink.Route{
							LinkIndex: vlanLinkPostAddWithIndex11.Index,
							Dst:       &net.IPNet{IP: net.ParseIP(subnetV6GW), Mask: net.CIDRMask(128, 128)},
							Scope:     netlink.SCOPE_LINK,
							Table:     107,
						},
					},
					{
						route: &netlink.Route{
							LinkIndex: vlanLinkPostAddWithIndex11.Index,
							Dst:       &net.IPNet{IP: net.IPv6zero, Mask: net.CIDRMask(0, 128)},
							Scope:     netlink.SCOPE_UNIVERSE,
							Gw:        net.ParseIP(subnetV6GW),
							Table:     107,
						},
					},
					{
						route: &netlink.Route{
							LinkIndex: hostVethWithIndex9.Index,
							Scope:     netlink.SCOPE_LINK,
							Dst:       containerV6Addr,
							Table:     unix.RT_TABLE_MAIN,
						},
					},
				},
				ruleAddCalls: []ruleAddCall{
					{
						rule: toContainerV6Rule,
					},
					{
						rule: fromContainerV6Rule,
					},
				},
				ruleDelCalls: []ruleDelCall{
					{
						rule: oldFromHostVethV6Rule,
						err:  syscall.ENOENT,
					},
				},
				withNetNSPathCalls: []withNetNSPathCall{
					{
						netNSPath: "/proc/42/ns/net",
					},
				},
				procSysSetCalls: []procSysSetCall{
					{
						key:   "net/ipv6/conf/eni8ea2c11fe35/accept_ra",
						value: "0",
					},
					{
						key:   "net/ipv6/conf/eni8ea2c11fe35/accept_redirects",
						value: "1",
					},
					{
						key:   "net/ipv6/conf/eni8ea2c11fe35/forwarding",
						value: "0",
					},
					{
						key:   "net/ipv6/conf/vlan.eth.7/accept_ra",
						value: "0",
					},
					{
						key:   "net/ipv6/conf/vlan.eth.7/accept_redirects",
						value: "1",
					},
					{
						key:   "net/ipv6/conf/vlan.eth.7/forwarding",
						value: "0",
					},
				},
			},
			args: args{
				hostVethName:       "eni8ea2c11fe35",
				contVethName:       "eth0",
				netnsPath:          "/proc/42/ns/net",
				ipAddr:             containerV6Addr, // v6
				vlanID:             vlanID,
				eniMAC:             eniMac,
				subnetGW:           subnetV6GW,
				parentIfIndex:      parentIfIndex,
				mtu:                9001,
				podSGEnforcingMode: sgpp.EnforcingModeStandard,
			},
		},
		{
			name: "failed to setup vethPair",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "eni8ea2c11fe35",
						err:      errors.New("not exists"),
					},
				},
				withNetNSPathCalls: []withNetNSPathCall{
					{
						netNSPath: "/proc/42/ns/net",
						err:       errors.New("some error"),
					},
				},
			},
			args: args{
				hostVethName:       "eni8ea2c11fe35",
				contVethName:       "eth0",
				netnsPath:          "/proc/42/ns/net",
				ipAddr:             containerAddr,
				vlanID:             vlanID,
				eniMAC:             eniMac,
				subnetGW:           subnetGW,
				parentIfIndex:      parentIfIndex,
				mtu:                9001,
				podSGEnforcingMode: sgpp.EnforcingModeStandard,
			},
			wantErr: errors.New("SetupBranchENIPodNetwork: failed to setup veth pair: failed to setup veth network: some error"),
		},
		{
			name: "failed to clean up old hostVeth rule",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "eni8ea2c11fe35",
						err:      errors.New("not exists"),
					},
					{
						linkName: "eni8ea2c11fe35",
						link:     hostVethWithIndex9,
					},
				},
				linkSetupCalls: []linkSetupCall{
					{
						link: hostVethWithIndex9,
					},
				},
				ruleDelCalls: []ruleDelCall{
					{
						rule: oldFromHostVethRule,
						err:  errors.New("some error"),
					},
				},
				withNetNSPathCalls: []withNetNSPathCall{
					{
						netNSPath: "/proc/42/ns/net",
					},
				},
				procSysSetCalls: []procSysSetCall{
					{
						key:   "net/ipv6/conf/eni8ea2c11fe35/accept_ra",
						value: "0",
					},
					{
						key:   "net/ipv6/conf/eni8ea2c11fe35/accept_redirects",
						value: "1",
					},
					{
						key:   "net/ipv6/conf/eni8ea2c11fe35/forwarding",
						value: "0",
					},
				},
			},
			args: args{
				hostVethName:       "eni8ea2c11fe35",
				contVethName:       "eth0",
				netnsPath:          "/proc/42/ns/net",
				ipAddr:             containerAddr,
				vlanID:             vlanID,
				eniMAC:             eniMac,
				subnetGW:           subnetGW,
				parentIfIndex:      parentIfIndex,
				mtu:                9001,
				podSGEnforcingMode: sgpp.EnforcingModeStandard,
			},
			wantErr: errors.New("SetupBranchENIPodNetwork: failed to delete hostVeth rule for eni8ea2c11fe35: some error"),
		},
		{
			name: "failed to setup vlan",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "eni8ea2c11fe35",
						err:      errors.New("not exists"),
					},
					{
						linkName: "eni8ea2c11fe35",
						link:     hostVethWithIndex9,
					},
					{
						linkName: "vlan.eth.7",
						err:      errors.New("not exists"),
					},
				},
				linkAddCalls: []linkAddCall{
					{
						link: buildVlanLink("vlan.eth.7", vlanID, parentIfIndex, eniMac),
						err:  errors.New("some error"),
					},
				},
				linkSetupCalls: []linkSetupCall{
					{
						link: hostVethWithIndex9,
					},
				},
				ruleDelCalls: []ruleDelCall{
					{
						rule: oldFromHostVethRule,
						err:  syscall.ENOENT,
					},
				},
				withNetNSPathCalls: []withNetNSPathCall{
					{
						netNSPath: "/proc/42/ns/net",
					},
				},
				procSysSetCalls: []procSysSetCall{
					{
						key:   "net/ipv6/conf/eni8ea2c11fe35/accept_ra",
						value: "0",
					},
					{
						key:   "net/ipv6/conf/eni8ea2c11fe35/accept_redirects",
						value: "1",
					},
					{
						key:   "net/ipv6/conf/eni8ea2c11fe35/forwarding",
						value: "0",
					},
				},
			},
			args: args{
				hostVethName:       "eni8ea2c11fe35",
				contVethName:       "eth0",
				netnsPath:          "/proc/42/ns/net",
				ipAddr:             containerAddr,
				vlanID:             vlanID,
				eniMAC:             eniMac,
				subnetGW:           subnetGW,
				parentIfIndex:      parentIfIndex,
				mtu:                9001,
				podSGEnforcingMode: sgpp.EnforcingModeStandard,
			},
			wantErr: errors.New("SetupBranchENIPodNetwork: failed to setup vlan: failed to add vlan link vlan.eth.7: some error"),
		},
		{
			name: "failed to setup IP based container route",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "eni8ea2c11fe35",
						err:      errors.New("not exists"),
					},
					{
						linkName: "eni8ea2c11fe35",
						link:     hostVethWithIndex9,
					},
					{
						linkName: "vlan.eth.7",
						err:      errors.New("not exists"),
					},
				},
				linkAddCalls: []linkAddCall{
					{
						link:      buildVlanLink("vlan.eth.7", vlanID, parentIfIndex, eniMac),
						linkIndex: 11,
					},
				},
				linkSetupCalls: []linkSetupCall{
					{
						link: hostVethWithIndex9,
					},
					{
						link: vlanLinkPostAddWithIndex11,
					},
				},
				routeReplaceCalls: []routeReplaceCall{
					{
						route: &netlink.Route{
							LinkIndex: vlanLinkPostAddWithIndex11.Index,
							Dst:       &net.IPNet{IP: net.ParseIP(subnetGW), Mask: net.CIDRMask(32, 32)},
							Scope:     netlink.SCOPE_LINK,
							Table:     107,
						},
					},
					{
						route: &netlink.Route{
							LinkIndex: vlanLinkPostAddWithIndex11.Index,
							Dst:       &net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(0, 32)},
							Scope:     netlink.SCOPE_UNIVERSE,
							Gw:        net.ParseIP(subnetGW),
							Table:     107,
						},
					},
					{
						route: &netlink.Route{
							LinkIndex: hostVethWithIndex9.Index,
							Scope:     netlink.SCOPE_LINK,
							Dst:       containerAddr,
							Table:     unix.RT_TABLE_MAIN,
						},
						err: errors.New("some error"),
					},
				},
				ruleDelCalls: []ruleDelCall{
					{
						rule: oldFromHostVethRule,
						err:  syscall.ENOENT,
					},
				},
				withNetNSPathCalls: []withNetNSPathCall{
					{
						netNSPath: "/proc/42/ns/net",
					},
				},
				procSysSetCalls: []procSysSetCall{
					{
						key:   "net/ipv6/conf/eni8ea2c11fe35/accept_ra",
						value: "0",
					},
					{
						key:   "net/ipv6/conf/eni8ea2c11fe35/accept_redirects",
						value: "1",
					},
					{
						key:   "net/ipv6/conf/eni8ea2c11fe35/forwarding",
						value: "0",
					},
					{
						key:   "net/ipv6/conf/vlan.eth.7/accept_ra",
						value: "0",
					},
					{
						key:   "net/ipv6/conf/vlan.eth.7/accept_redirects",
						value: "1",
					},
					{
						key:   "net/ipv6/conf/vlan.eth.7/forwarding",
						value: "0",
					},
				},
			},
			args: args{
				hostVethName:       "eni8ea2c11fe35",
				contVethName:       "eth0",
				netnsPath:          "/proc/42/ns/net",
				ipAddr:             containerAddr,
				vlanID:             vlanID,
				eniMAC:             eniMac,
				subnetGW:           subnetGW,
				parentIfIndex:      parentIfIndex,
				mtu:                9001,
				podSGEnforcingMode: sgpp.EnforcingModeStandard,
			},
			wantErr: errors.New("SetupBranchENIPodNetwork: unable to setup IP based container routes and rules: failed to setup container route, containerAddr=192.168.100.42/32, hostVeth=eni8ea2c11fe35, rtTable=main: some error"),
		},
		{
			name: "failed to setup IIF based container route",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "eni8ea2c11fe35",
						err:      errors.New("not exists"),
					},
					{
						linkName: "eni8ea2c11fe35",
						link:     hostVethWithIndex9,
					},
					{
						linkName: "vlan.eth.7",
						err:      errors.New("not exists"),
					},
				},
				linkAddCalls: []linkAddCall{
					{
						link:      buildVlanLink("vlan.eth.7", vlanID, parentIfIndex, eniMac),
						linkIndex: 11,
					},
				},
				linkSetupCalls: []linkSetupCall{
					{
						link: hostVethWithIndex9,
					},
					{
						link: vlanLinkPostAddWithIndex11,
					},
				},
				routeReplaceCalls: []routeReplaceCall{
					{
						route: &netlink.Route{
							LinkIndex: vlanLinkPostAddWithIndex11.Index,
							Dst:       &net.IPNet{IP: net.ParseIP(subnetGW), Mask: net.CIDRMask(32, 32)},
							Scope:     netlink.SCOPE_LINK,
							Table:     107,
						},
					},
					{
						route: &netlink.Route{
							LinkIndex: vlanLinkPostAddWithIndex11.Index,
							Dst:       &net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(0, 32)},
							Scope:     netlink.SCOPE_UNIVERSE,
							Gw:        net.ParseIP(subnetGW),
							Table:     107,
						},
					},
					{
						route: &netlink.Route{
							LinkIndex: hostVethWithIndex9.Index,
							Scope:     netlink.SCOPE_LINK,
							Dst:       containerAddr,
							Table:     107,
						},
						err: errors.New("some error"),
					},
				},
				ruleDelCalls: []ruleDelCall{
					{
						rule: oldFromHostVethRule,
						err:  syscall.ENOENT,
					},
				},
				withNetNSPathCalls: []withNetNSPathCall{
					{
						netNSPath: "/proc/42/ns/net",
					},
				},
				procSysSetCalls: []procSysSetCall{
					{
						key:   "net/ipv6/conf/eni8ea2c11fe35/accept_ra",
						value: "0",
					},
					{
						key:   "net/ipv6/conf/eni8ea2c11fe35/accept_redirects",
						value: "1",
					},
					{
						key:   "net/ipv6/conf/eni8ea2c11fe35/forwarding",
						value: "0",
					},
					{
						key:   "net/ipv6/conf/vlan.eth.7/accept_ra",
						value: "0",
					},
					{
						key:   "net/ipv6/conf/vlan.eth.7/accept_redirects",
						value: "1",
					},
					{
						key:   "net/ipv6/conf/vlan.eth.7/forwarding",
						value: "0",
					},
				},
			},
			args: args{
				hostVethName:       "eni8ea2c11fe35",
				contVethName:       "eth0",
				netnsPath:          "/proc/42/ns/net",
				ipAddr:             containerAddr,
				vlanID:             vlanID,
				eniMAC:             eniMac,
				subnetGW:           subnetGW,
				parentIfIndex:      parentIfIndex,
				mtu:                9001,
				podSGEnforcingMode: sgpp.EnforcingModeStrict,
			},
			wantErr: errors.New("SetupBranchENIPodNetwork: unable to setup IIF based container routes and rules: failed to setup container route, containerAddr=192.168.100.42/32, hostVeth=eni8ea2c11fe35, rtTable=107: some error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			netLink := mock_netlinkwrapper.NewMockNetLink(ctrl)
			netLink.EXPECT().NewRule().DoAndReturn(func() *netlink.Rule { return netlink.NewRule() }).AnyTimes()
			for _, call := range tt.fields.linkByNameCalls {
				netLink.EXPECT().LinkByName(call.linkName).Return(call.link, call.err)
			}
			for _, call := range tt.fields.linkAddCalls {
				netLink.EXPECT().LinkAdd(call.link).DoAndReturn(func(link netlink.Link) error {
					if call.err != nil {
						return call.err
					}
					vlanBeforeAdd := link.(*netlink.Vlan)
					vlanBeforeAdd.Index = call.linkIndex
					return nil
				})
			}
			for _, call := range tt.fields.linkDelCalls {
				netLink.EXPECT().LinkDel(call.link).Return(call.err)
			}
			for _, call := range tt.fields.linkSetupCalls {
				netLink.EXPECT().LinkSetUp(call.link).Return(call.err)
			}
			for _, call := range tt.fields.routeReplaceCalls {
				netLink.EXPECT().RouteReplace(call.route).Return(call.err)
			}
			for _, call := range tt.fields.ruleAddCalls {
				netLink.EXPECT().RuleAdd(call.rule).Return(call.err)
			}
			for _, call := range tt.fields.ruleDelCalls {
				netLink.EXPECT().RuleDel(call.rule).Return(call.err)
			}

			ns := mock_nswrapper.NewMockNS(ctrl)
			for _, call := range tt.fields.withNetNSPathCalls {
				// we just assume the createVethContext executes, the logic of createVethContext will be tested by createVethContext itself.
				ns.EXPECT().WithNetNSPath(call.netNSPath, gomock.Any()).Return(call.err)
			}

			procSys := mock_procsyswrapper.NewMockProcSys(ctrl)
			for _, call := range tt.fields.procSysSetCalls {
				procSys.EXPECT().Set(call.key, call.value).Return(call.err)
			}

			n := &linuxNetwork{
				netLink: netLink,
				ns:      ns,
				procSys: procSys,
			}

			vIfMetadata := VirtualInterfaceMetadata{
				IPAddress:         tt.args.ipAddr,
				HostVethName:      tt.args.hostVethName,
				ContainerVethName: tt.args.contVethName,
			}

			err := n.SetupBranchENIPodNetwork(vIfMetadata, tt.args.netnsPath, tt.args.vlanID, tt.args.eniMAC, tt.args.subnetGW, tt.args.parentIfIndex, tt.args.mtu, tt.args.podSGEnforcingMode, testLogger)
			if tt.wantErr != nil {
				assert.EqualError(t, err, tt.wantErr.Error())
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func Test_linuxNetwork_TeardownBranchENIPodNetwork(t *testing.T) {
	vlanID := 7
	containerAddr := &net.IPNet{
		IP:   net.ParseIP("192.168.100.42"),
		Mask: net.CIDRMask(32, 32),
	}
	containerV6Addr := &net.IPNet{
		IP:   net.ParseIP("2600::2"),
		Mask: net.CIDRMask(128, 128),
	}

	vlanRuleForRTTable107 := netlink.NewRule()
	vlanRuleForRTTable107.Priority = networkutils.VlanRulePriority
	vlanRuleForRTTable107.Table = 107
	vlanRuleForRTTable107.Family = netlink.FAMILY_V4

	vlanV6RuleForRTTable107 := netlink.NewRule()
	vlanV6RuleForRTTable107.Priority = networkutils.VlanRulePriority
	vlanV6RuleForRTTable107.Table = 107
	vlanV6RuleForRTTable107.Family = netlink.FAMILY_V6

	toContainerRoute := &netlink.Route{
		Scope: netlink.SCOPE_LINK,
		Dst:   containerAddr,
		Table: unix.RT_TABLE_MAIN,
	}
	toContainerV6Route := &netlink.Route{
		Scope: netlink.SCOPE_LINK,
		Dst:   containerV6Addr,
		Table: unix.RT_TABLE_MAIN,
	}

	toContainerRule := netlink.NewRule()
	toContainerRule.Dst = containerAddr
	toContainerRule.Priority = networkutils.ToContainerRulePriority
	toContainerRule.Table = unix.RT_TABLE_MAIN

	toContainerV6Rule := netlink.NewRule()
	toContainerV6Rule.Dst = containerV6Addr
	toContainerV6Rule.Priority = networkutils.ToContainerRulePriority
	toContainerV6Rule.Table = unix.RT_TABLE_MAIN

	fromContainerRule := netlink.NewRule()
	fromContainerRule.Src = containerAddr
	fromContainerRule.Priority = networkutils.FromPodRulePriority
	fromContainerRule.Table = 107

	fromContainerV6Rule := netlink.NewRule()
	fromContainerV6Rule.Src = containerV6Addr
	fromContainerV6Rule.Priority = networkutils.FromPodRulePriority
	fromContainerV6Rule.Table = 107

	type linkByNameCall struct {
		linkName string
		link     netlink.Link
		err      error
	}
	type linkDelCall struct {
		link netlink.Link
		err  error
	}
	type routeDelCall struct {
		route *netlink.Route
		err   error
	}
	type ruleDelCall struct {
		rule *netlink.Rule
		err  error
	}

	type fields struct {
		linkByNameCalls []linkByNameCall
		linkDelCalls    []linkDelCall
		routeDelCalls   []routeDelCall
		ruleDelCalls    []ruleDelCall
	}
	type args struct {
		containerAddr      *net.IPNet
		vlanID             int
		podSGEnforcingMode sgpp.EnforcingMode
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr error
	}{
		{
			name: "successfully teardown pod network - pod was setup under strict mode",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "vlan.eth.7",
						link:     &netlink.Vlan{VlanId: vlanID},
					},
				},
				linkDelCalls: []linkDelCall{
					{
						link: &netlink.Vlan{VlanId: vlanID},
					},
				},
				routeDelCalls: []routeDelCall{
					{
						route: toContainerRoute,
						err:   syscall.ESRCH,
					},
				},
				ruleDelCalls: []ruleDelCall{
					{
						rule: vlanRuleForRTTable107,
					},
					{
						rule: vlanRuleForRTTable107,
					},
					{
						rule: vlanRuleForRTTable107,
						err:  syscall.ENOENT,
					},
					{
						rule: toContainerRule,
						err:  syscall.ENOENT,
					},
					{
						rule: fromContainerRule,
						err:  syscall.ENOENT,
					},
				},
			},
			args: args{
				containerAddr:      containerAddr,
				vlanID:             vlanID,
				podSGEnforcingMode: sgpp.EnforcingModeStrict,
			},
		},
		{
			name: "successfully teardown v6 pod network - pod was setup under strict mode",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "vlan.eth.7",
						link:     &netlink.Vlan{VlanId: vlanID},
					},
				},
				linkDelCalls: []linkDelCall{
					{
						link: &netlink.Vlan{VlanId: vlanID},
					},
				},
				routeDelCalls: []routeDelCall{
					{
						route: toContainerV6Route,
						err:   syscall.ESRCH,
					},
				},
				ruleDelCalls: []ruleDelCall{
					{
						rule: vlanV6RuleForRTTable107,
					},
					{
						rule: vlanV6RuleForRTTable107,
					},
					{
						rule: vlanV6RuleForRTTable107,
						err:  syscall.ENOENT,
					},
					{
						rule: toContainerV6Rule,
						err:  syscall.ENOENT,
					},
					{
						rule: fromContainerV6Rule,
						err:  syscall.ENOENT,
					},
				},
			},
			args: args{
				containerAddr:      containerV6Addr,
				vlanID:             vlanID,
				podSGEnforcingMode: sgpp.EnforcingModeStrict,
			},
		},
		{
			name: "successfully teardown pod network - pod was setup under standard mode",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "vlan.eth.7",
						link:     &netlink.Vlan{VlanId: vlanID},
					},
				},
				linkDelCalls: []linkDelCall{
					{
						link: &netlink.Vlan{VlanId: vlanID},
					},
				},
				routeDelCalls: []routeDelCall{
					{
						route: toContainerRoute,
					},
				},
				ruleDelCalls: []ruleDelCall{
					{
						rule: vlanRuleForRTTable107,
						err:  syscall.ENOENT,
					},
					{
						rule: toContainerRule,
					},
					{
						rule: fromContainerRule,
					},
					{
						rule: fromContainerRule,
						err:  syscall.ENOENT,
					},
				},
			},
			args: args{
				containerAddr:      containerAddr,
				vlanID:             vlanID,
				podSGEnforcingMode: sgpp.EnforcingModeStandard,
			},
		},
		{
			name: "successfully teardown v6 pod network - pod was setup under standard mode",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "vlan.eth.7",
						link:     &netlink.Vlan{VlanId: vlanID},
					},
				},
				linkDelCalls: []linkDelCall{
					{
						link: &netlink.Vlan{VlanId: vlanID},
					},
				},
				routeDelCalls: []routeDelCall{
					{
						route: toContainerV6Route,
					},
				},
				ruleDelCalls: []ruleDelCall{
					{
						rule: vlanV6RuleForRTTable107,
						err:  syscall.ENOENT,
					},
					{
						rule: toContainerV6Rule,
					},
					{
						rule: fromContainerV6Rule,
					},
					{
						rule: fromContainerV6Rule,
						err:  syscall.ENOENT,
					},
				},
			},
			args: args{
				containerAddr:      containerV6Addr,
				vlanID:             vlanID,
				podSGEnforcingMode: sgpp.EnforcingModeStandard,
			},
		},
		{
			name: "failed to teardown vlan",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "vlan.eth.7",
						link:     &netlink.Vlan{VlanId: vlanID},
					},
				},
				linkDelCalls: []linkDelCall{
					{
						link: &netlink.Vlan{VlanId: vlanID},
						err:  errors.New("some error"),
					},
				},
			},
			args: args{
				containerAddr:      containerAddr,
				vlanID:             vlanID,
				podSGEnforcingMode: sgpp.EnforcingModeStandard,
			},
			wantErr: errors.New("TeardownBranchENIPodNetwork: failed to teardown vlan: failed to delete vlan link vlan.eth.7: some error"),
		},
		{
			name: "failed to delete vlan rule",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "vlan.eth.7",
						link:     &netlink.Vlan{VlanId: vlanID},
					},
				},
				linkDelCalls: []linkDelCall{
					{
						link: &netlink.Vlan{VlanId: vlanID},
					},
				},
				ruleDelCalls: []ruleDelCall{
					{
						rule: vlanRuleForRTTable107,
						err:  errors.New("some error"),
					},
				},
			},
			args: args{
				containerAddr:      containerAddr,
				vlanID:             vlanID,
				podSGEnforcingMode: sgpp.EnforcingModeStrict,
			},
			wantErr: errors.New("TeardownBranchENIPodNetwork: unable to teardown IIF based container routes and rules: failed to delete IIF based rules, rtTable=107: some error"),
		},
		{
			name: "failed to delete toContainer rule",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "vlan.eth.7",
						link:     &netlink.Vlan{VlanId: vlanID},
					},
				},
				linkDelCalls: []linkDelCall{
					{
						link: &netlink.Vlan{VlanId: vlanID},
					},
				},
				ruleDelCalls: []ruleDelCall{
					{
						rule: vlanRuleForRTTable107,
						err:  syscall.ENOENT,
					},
					{
						rule: toContainerRule,
						err:  errors.New("some error"),
					},
				},
			},
			args: args{
				containerAddr:      containerAddr,
				vlanID:             vlanID,
				podSGEnforcingMode: sgpp.EnforcingModeStandard,
			},
			wantErr: errors.New("TeardownBranchENIPodNetwork: unable to teardown IP based container routes and rules: failed to delete toContainer rule, containerAddr=192.168.100.42/32, rtTable=main: some error"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			netLink := mock_netlinkwrapper.NewMockNetLink(ctrl)
			netLink.EXPECT().NewRule().DoAndReturn(func() *netlink.Rule { return netlink.NewRule() }).AnyTimes()
			for _, call := range tt.fields.linkByNameCalls {
				netLink.EXPECT().LinkByName(call.linkName).Return(call.link, call.err)
			}
			for _, call := range tt.fields.linkDelCalls {
				netLink.EXPECT().LinkDel(call.link).Return(call.err)
			}
			for _, call := range tt.fields.routeDelCalls {
				netLink.EXPECT().RouteDel(call.route).Return(call.err)
			}
			for _, call := range tt.fields.ruleDelCalls {
				netLink.EXPECT().RuleDel(call.rule).Return(call.err)
			}
			n := &linuxNetwork{
				netLink: netLink,
			}

			vIfMetadata := VirtualInterfaceMetadata{
				IPAddress: tt.args.containerAddr,
			}

			err := n.TeardownBranchENIPodNetwork(vIfMetadata, tt.args.vlanID, tt.args.podSGEnforcingMode, testLogger)
			if tt.wantErr != nil {
				assert.EqualError(t, err, tt.wantErr.Error())
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func Test_createVethPairContext_run(t *testing.T) {
	contVethWithIndex1 := &netlink.Veth{
		LinkAttrs: netlink.LinkAttrs{
			Name:  "eth0",
			Index: 1,
		},
	}

	contVethWithIndex2 := &netlink.Veth{
		LinkAttrs: netlink.LinkAttrs{
			Name:  "mNicIf1",
			Index: 1,
		},
	}

	hostVethWithIndex9 := &netlink.Veth{
		LinkAttrs: netlink.LinkAttrs{
			Name:         "eni8ea2c11fe35",
			Index:        9,
			HardwareAddr: net.HardwareAddr("00:00:5e:00:53:af"),
		},
	}

	type linkByNameCall struct {
		linkName string
		link     netlink.Link
		err      error
	}
	type linkAddCall struct {
		link netlink.Link
		err  error
	}
	type linkSetupCall struct {
		link netlink.Link
		err  error
	}
	type routeReplaceCall struct {
		route *netlink.Route
		err   error
	}
	type routeAddCall struct {
		route *netlink.Route
		err   error
	}
	type addrAddCall struct {
		link netlink.Link
		addr *netlink.Addr
		err  error
	}
	type addrListCall struct {
		link   netlink.Link
		family int
		addrs  []netlink.Addr
		err    error
	}
	type neighAddCall struct {
		neigh *netlink.Neigh
		err   error
	}
	type linkSetNsFdCall struct {
		link netlink.Link
		fd   int
		err  error
	}
	type procSysSetCall struct {
		key   string
		value string
		err   error
	}
	type nsFDCall struct {
		fd uintptr
	}
	type ruleAddCall struct {
		rule *netlink.Rule
		err  error
	}

	type fields struct {
		linkByNameCalls   []linkByNameCall
		linkAddCalls      []linkAddCall
		linkSetupCalls    []linkSetupCall
		routeReplaceCalls []routeReplaceCall
		routeAddCalls     []routeAddCall
		addrAddCalls      []addrAddCall
		addrListCalls     []addrListCall
		neighAddCalls     []neighAddCall
		linkSetNsFdCalls  []linkSetNsFdCall
		procSysSetCalls   []procSysSetCall
		nsFDCalls         []nsFDCall
		linkRuleAddCalls  []ruleAddCall
	}
	type args struct {
		contVethName string
		hostVethName string
		ipAddr       *net.IPNet
		mtu          int
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr error
		index   int
	}{
		{
			name: "successfully created vethPair for ipv4 pods",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "eni8ea2c11fe35",
						link:     hostVethWithIndex9,
					},
					{
						linkName: "eth0",
						link:     contVethWithIndex1,
					},
				},
				linkAddCalls: []linkAddCall{
					{
						link: &netlink.Veth{
							LinkAttrs: netlink.LinkAttrs{
								Name:  "eth0",
								Flags: net.FlagUp,
								MTU:   9001,
							},
							PeerName: "eni8ea2c11fe35",
						},
					},
				},
				linkSetupCalls: []linkSetupCall{
					{
						link: hostVethWithIndex9,
					},
					{
						link: contVethWithIndex1,
					},
				},
				routeReplaceCalls: []routeReplaceCall{
					{
						route: &netlink.Route{
							LinkIndex: contVethWithIndex1.Attrs().Index,
							Scope:     netlink.SCOPE_LINK,
							Table:     254,
							Dst: &net.IPNet{
								IP:   net.IPv4(169, 254, 1, 1),
								Mask: net.CIDRMask(32, 32),
							},
						},
					},
				},
				routeAddCalls: []routeAddCall{
					{
						route: &netlink.Route{
							LinkIndex: contVethWithIndex1.Attrs().Index,
							Scope:     netlink.SCOPE_UNIVERSE,
							Table:     254,
							Dst: &net.IPNet{
								IP:   net.IPv4zero,
								Mask: net.CIDRMask(0, 32),
							},
							Gw: net.IPv4(169, 254, 1, 1),
						},
					},
				},
				addrAddCalls: []addrAddCall{
					{
						link: contVethWithIndex1,
						addr: &netlink.Addr{
							IPNet: &net.IPNet{
								IP:   net.ParseIP("192.168.120.1"),
								Mask: net.CIDRMask(32, 32),
							},
						},
					},
				},
				neighAddCalls: []neighAddCall{
					{
						neigh: &netlink.Neigh{
							LinkIndex:    contVethWithIndex1.Attrs().Index,
							State:        netlink.NUD_PERMANENT,
							IP:           net.IPv4(169, 254, 1, 1),
							HardwareAddr: hostVethWithIndex9.Attrs().HardwareAddr,
						},
					},
				},
				linkSetNsFdCalls: []linkSetNsFdCall{
					{
						link: hostVethWithIndex9,
						fd:   3,
					},
				},
				procSysSetCalls: []procSysSetCall{},
				nsFDCalls: []nsFDCall{
					{
						fd: uintptr(3),
					},
				},
			},
			args: args{
				contVethName: "eth0",
				hostVethName: "eni8ea2c11fe35",
				ipAddr: &net.IPNet{
					IP:   net.ParseIP("192.168.120.1"),
					Mask: net.CIDRMask(32, 32),
				},
				mtu: 9001,
			},
			index: 0,
		},
		{
			name: "successfully created vethPair for ipv4 pods with mNicIf1",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "eni8ea2c11fe35",
						link:     hostVethWithIndex9,
					},
					{
						linkName: "mNicIf1",
						link:     contVethWithIndex2,
					},
				},
				linkAddCalls: []linkAddCall{
					{
						link: &netlink.Veth{
							LinkAttrs: netlink.LinkAttrs{
								Name:  "mNicIf1",
								Flags: net.FlagUp,
								MTU:   9001,
							},
							PeerName: "eni8ea2c11fe35",
						},
					},
				},
				linkSetupCalls: []linkSetupCall{
					{
						link: hostVethWithIndex9,
					},
					{
						link: contVethWithIndex2,
					},
				},
				routeReplaceCalls: []routeReplaceCall{
					{
						route: &netlink.Route{
							LinkIndex: contVethWithIndex2.Attrs().Index,
							Scope:     netlink.SCOPE_LINK,
							Table:     1,
							Dst: &net.IPNet{
								IP:   net.IPv4(169, 254, 1, 2),
								Mask: net.CIDRMask(32, 32),
							},
						},
					},
				},
				routeAddCalls: []routeAddCall{
					{
						route: &netlink.Route{
							LinkIndex: contVethWithIndex2.Attrs().Index,
							Scope:     netlink.SCOPE_UNIVERSE,
							Table:     1,
							Dst: &net.IPNet{
								IP:   net.IPv4zero,
								Mask: net.CIDRMask(0, 32),
							},
							Gw: net.IPv4(169, 254, 1, 2),
						},
					},
				},
				addrAddCalls: []addrAddCall{
					{
						link: contVethWithIndex2,
						addr: &netlink.Addr{
							IPNet: &net.IPNet{
								IP:   net.ParseIP("192.168.120.1"),
								Mask: net.CIDRMask(32, 32),
							},
						},
					},
				},
				neighAddCalls: []neighAddCall{
					{
						neigh: &netlink.Neigh{
							LinkIndex:    contVethWithIndex2.Attrs().Index,
							State:        netlink.NUD_PERMANENT,
							IP:           net.IPv4(169, 254, 1, 2),
							HardwareAddr: hostVethWithIndex9.Attrs().HardwareAddr,
						},
					},
				},
				linkSetNsFdCalls: []linkSetNsFdCall{
					{
						link: hostVethWithIndex9,
						fd:   3,
					},
				},
				procSysSetCalls: []procSysSetCall{},
				nsFDCalls: []nsFDCall{
					{
						fd: uintptr(3),
					},
				},
				linkRuleAddCalls: []ruleAddCall{
					{
						rule: &netlink.Rule{
							Src: &net.IPNet{
								IP:   net.ParseIP("192.168.120.1"),
								Mask: net.CIDRMask(32, 32),
							},
							Priority: networkutils.FromInterfaceRulePriority,
							Table:    1,
						},
						err: nil,
					},
				},
			},
			args: args{
				contVethName: "mNicIf1",
				hostVethName: "eni8ea2c11fe35",
				ipAddr: &net.IPNet{
					IP:   net.ParseIP("192.168.120.1"),
					Mask: net.CIDRMask(32, 32),
				},
				mtu: 9001,
			},
			index: 1,
		},
		{
			name: "successfully created vethPair for ipv6 pods",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "eni8ea2c11fe35",
						link:     hostVethWithIndex9,
					},
					{
						linkName: "eth0",
						link:     contVethWithIndex1,
					},
					{
						linkName: "eth0",
						link:     contVethWithIndex1,
					},
				},
				linkAddCalls: []linkAddCall{
					{
						link: &netlink.Veth{
							LinkAttrs: netlink.LinkAttrs{
								Name:  "eth0",
								Flags: net.FlagUp,
								MTU:   9001,
							},
							PeerName: "eni8ea2c11fe35",
						},
					},
				},
				linkSetupCalls: []linkSetupCall{
					{
						link: hostVethWithIndex9,
					},
					{
						link: contVethWithIndex1,
					},
				},
				routeReplaceCalls: []routeReplaceCall{
					{
						route: &netlink.Route{
							LinkIndex: contVethWithIndex1.Attrs().Index,
							Scope:     netlink.SCOPE_LINK,
							Table:     254,
							Dst: &net.IPNet{
								IP:   net.IP{0xfe, 0x80, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
								Mask: net.CIDRMask(128, 128),
							},
						},
					},
				},
				routeAddCalls: []routeAddCall{
					{
						route: &netlink.Route{
							LinkIndex: contVethWithIndex1.Attrs().Index,
							Scope:     netlink.SCOPE_UNIVERSE,
							Table:     254,
							Dst: &net.IPNet{
								IP:   net.IPv6zero,
								Mask: net.CIDRMask(0, 128),
							},
							Gw: net.IP{0xfe, 0x80, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
						},
					},
				},
				addrAddCalls: []addrAddCall{
					{
						link: contVethWithIndex1,
						addr: &netlink.Addr{
							IPNet: &net.IPNet{
								IP:   net.ParseIP("2001:db8:3333:4444:5555:6666:7777:8888"),
								Mask: net.CIDRMask(128, 128),
							},
						},
					},
				},
				addrListCalls: []addrListCall{
					{
						link:   contVethWithIndex1,
						family: netlink.FAMILY_V6,
						addrs: []netlink.Addr{
							{
								IPNet: &net.IPNet{
									IP:   net.ParseIP("2001:db8:3333:4444:5555:6666:7777:8888"),
									Mask: net.CIDRMask(128, 128),
								},
							},
						},
					},
				},
				neighAddCalls: []neighAddCall{
					{
						neigh: &netlink.Neigh{
							LinkIndex:    contVethWithIndex1.Attrs().Index,
							State:        netlink.NUD_PERMANENT,
							IP:           net.IP{0xfe, 0x80, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
							HardwareAddr: hostVethWithIndex9.Attrs().HardwareAddr,
						},
					},
				},
				linkSetNsFdCalls: []linkSetNsFdCall{
					{
						link: hostVethWithIndex9,
						fd:   3,
					},
				},
				procSysSetCalls: []procSysSetCall{
					{
						key:   "net/ipv6/conf/eth0/disable_ipv6",
						value: "0",
					},
					{
						key:   "net/ipv6/conf/lo/disable_ipv6",
						value: "0",
					},
				},
				nsFDCalls: []nsFDCall{
					{
						fd: uintptr(3),
					},
				},
			},
			args: args{
				contVethName: "eth0",
				hostVethName: "eni8ea2c11fe35",
				ipAddr: &net.IPNet{
					IP:   net.ParseIP("2001:db8:3333:4444:5555:6666:7777:8888"),
					Mask: net.CIDRMask(128, 128),
				},
				mtu: 9001,
			},
			index: 0,
		},
		{
			name: "successfully created vethPair for ipv6 pods with mNicIf1",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "eni8ea2c11fe35",
						link:     hostVethWithIndex9,
					},
					{
						linkName: "mNicIf1",
						link:     contVethWithIndex2,
					},
					{
						linkName: "mNicIf1",
						link:     contVethWithIndex2,
					},
				},
				linkAddCalls: []linkAddCall{
					{
						link: &netlink.Veth{
							LinkAttrs: netlink.LinkAttrs{
								Name:  "mNicIf1",
								Flags: net.FlagUp,
								MTU:   9001,
							},
							PeerName: "eni8ea2c11fe35",
						},
					},
				},
				linkSetupCalls: []linkSetupCall{
					{
						link: hostVethWithIndex9,
					},
					{
						link: contVethWithIndex2,
					},
				},
				routeReplaceCalls: []routeReplaceCall{
					{
						route: &netlink.Route{
							LinkIndex: contVethWithIndex2.Attrs().Index,
							Scope:     netlink.SCOPE_LINK,
							Table:     1,
							Dst: &net.IPNet{
								IP:   net.IP{0xfe, 0x80, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2},
								Mask: net.CIDRMask(128, 128),
							},
						},
					},
				},
				routeAddCalls: []routeAddCall{
					{
						route: &netlink.Route{
							LinkIndex: contVethWithIndex2.Attrs().Index,
							Scope:     netlink.SCOPE_UNIVERSE,
							Table:     1,
							Dst: &net.IPNet{
								IP:   net.IPv6zero,
								Mask: net.CIDRMask(0, 128),
							},
							Gw: net.IP{0xfe, 0x80, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2},
						},
					},
				},
				addrAddCalls: []addrAddCall{
					{
						link: contVethWithIndex2,
						addr: &netlink.Addr{
							IPNet: &net.IPNet{
								IP:   net.ParseIP("2001:db8:3333:4444:5555:6666:7777:8888"),
								Mask: net.CIDRMask(128, 128),
							},
						},
					},
				},
				addrListCalls: []addrListCall{
					{
						link:   contVethWithIndex2,
						family: netlink.FAMILY_V6,
						addrs: []netlink.Addr{
							{
								IPNet: &net.IPNet{
									IP:   net.ParseIP("2001:db8:3333:4444:5555:6666:7777:8888"),
									Mask: net.CIDRMask(128, 128),
								},
							},
						},
					},
				},
				neighAddCalls: []neighAddCall{
					{
						neigh: &netlink.Neigh{
							LinkIndex:    contVethWithIndex2.Attrs().Index,
							State:        netlink.NUD_PERMANENT,
							IP:           net.IP{0xfe, 0x80, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2},
							HardwareAddr: hostVethWithIndex9.Attrs().HardwareAddr,
						},
					},
				},
				linkSetNsFdCalls: []linkSetNsFdCall{
					{
						link: hostVethWithIndex9,
						fd:   3,
					},
				},
				procSysSetCalls: []procSysSetCall{
					{
						key:   "net/ipv6/conf/mNicIf1/disable_ipv6",
						value: "0",
					},
					{
						key:   "net/ipv6/conf/lo/disable_ipv6",
						value: "0",
					},
				},
				nsFDCalls: []nsFDCall{
					{
						fd: uintptr(3),
					},
				},

				linkRuleAddCalls: []ruleAddCall{
					{
						rule: &netlink.Rule{
							Src: &net.IPNet{
								IP:   net.ParseIP("2001:db8:3333:4444:5555:6666:7777:8888"),
								Mask: net.CIDRMask(128, 128),
							},
							Priority: networkutils.FromInterfaceRulePriority,
							Table:    1,
							Family:   netlink.FAMILY_V6,
						},
						err: nil,
					},
				},
			},
			args: args{
				contVethName: "mNicIf1",
				hostVethName: "eni8ea2c11fe35",
				ipAddr: &net.IPNet{
					IP:   net.ParseIP("2001:db8:3333:4444:5555:6666:7777:8888"),
					Mask: net.CIDRMask(128, 128),
				},
				mtu: 9001,
			},
			index: 1,
		},
		{
			name: "failed to add vethPair",
			fields: fields{
				linkAddCalls: []linkAddCall{
					{
						link: &netlink.Veth{
							LinkAttrs: netlink.LinkAttrs{
								Name:  "eth0",
								Flags: net.FlagUp,
								MTU:   9001,
							},
							PeerName: "eni8ea2c11fe35",
						},
						err: errors.New("some error"),
					},
				},
			},
			args: args{
				contVethName: "eth0",
				hostVethName: "eni8ea2c11fe35",
				ipAddr: &net.IPNet{
					IP:   net.ParseIP("192.168.120.1"),
					Mask: net.CIDRMask(32, 32),
				},
				mtu: 9001,
			},
			wantErr: errors.New("some error"),
			index:   0,
		},
		{
			name: "failed to find hostVeth",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "eni8ea2c11fe35",
						err:      errors.New("some error"),
					},
				},
				linkAddCalls: []linkAddCall{
					{
						link: &netlink.Veth{
							LinkAttrs: netlink.LinkAttrs{
								Name:  "eth0",
								Flags: net.FlagUp,
								MTU:   9001,
							},
							PeerName: "eni8ea2c11fe35",
						},
					},
				},
			},
			args: args{
				contVethName: "eth0",
				hostVethName: "eni8ea2c11fe35",
				ipAddr: &net.IPNet{
					IP:   net.ParseIP("192.168.120.1"),
					Mask: net.CIDRMask(32, 32),
				},
				mtu: 9001,
			},
			wantErr: errors.New("setup NS network: failed to find link \"eni8ea2c11fe35\": some error"),
			index:   0,
		},
		{
			name: "failed to setUp hostVeth",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "eni8ea2c11fe35",
						link:     hostVethWithIndex9,
					},
				},
				linkAddCalls: []linkAddCall{
					{
						link: &netlink.Veth{
							LinkAttrs: netlink.LinkAttrs{
								Name:  "eth0",
								Flags: net.FlagUp,
								MTU:   9001,
							},
							PeerName: "eni8ea2c11fe35",
						},
					},
				},
				linkSetupCalls: []linkSetupCall{
					{
						link: hostVethWithIndex9,
						err:  errors.New("some error"),
					},
				},
			},
			args: args{
				contVethName: "eth0",
				hostVethName: "eni8ea2c11fe35",
				ipAddr: &net.IPNet{
					IP:   net.ParseIP("192.168.120.1"),
					Mask: net.CIDRMask(32, 32),
				},
				mtu: 9001,
			},
			wantErr: errors.New("setup NS network: failed to set link \"eni8ea2c11fe35\" up: some error"),
			index:   0,
		},
		{
			name: "failed to find contVeth",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "eni8ea2c11fe35",
						link:     hostVethWithIndex9,
					},
					{
						linkName: "eth0",
						err:      errors.New("some error"),
					},
				},
				linkAddCalls: []linkAddCall{
					{
						link: &netlink.Veth{
							LinkAttrs: netlink.LinkAttrs{
								Name:  "eth0",
								Flags: net.FlagUp,
								MTU:   9001,
							},
							PeerName: "eni8ea2c11fe35",
						},
					},
				},
				linkSetupCalls: []linkSetupCall{
					{
						link: hostVethWithIndex9,
					},
				},
			},
			args: args{
				contVethName: "eth0",
				hostVethName: "eni8ea2c11fe35",
				ipAddr: &net.IPNet{
					IP:   net.ParseIP("192.168.120.1"),
					Mask: net.CIDRMask(32, 32),
				},
				mtu: 9001,
			},
			wantErr: errors.New("setup NS network: failed to find link \"eth0\": some error"),
			index:   0,
		},
		{
			name: "failed to setUp contVeth",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "eni8ea2c11fe35",
						link:     hostVethWithIndex9,
					},
					{
						linkName: "eth0",
						link:     contVethWithIndex1,
					},
				},
				linkAddCalls: []linkAddCall{
					{
						link: &netlink.Veth{
							LinkAttrs: netlink.LinkAttrs{
								Name:  "eth0",
								Flags: net.FlagUp,
								MTU:   9001,
							},
							PeerName: "eni8ea2c11fe35",
						},
					},
				},
				linkSetupCalls: []linkSetupCall{
					{
						link: hostVethWithIndex9,
					},
					{
						link: contVethWithIndex1,
						err:  errors.New("some error"),
					},
				},
			},
			args: args{
				contVethName: "eth0",
				hostVethName: "eni8ea2c11fe35",
				ipAddr: &net.IPNet{
					IP:   net.ParseIP("192.168.120.1"),
					Mask: net.CIDRMask(32, 32),
				},
				mtu: 9001,
			},
			wantErr: errors.New("setup NS network: failed to set link \"eth0\" up: some error"),
			index:   0,
		},
		{
			name: "failed to add default gateway",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "eni8ea2c11fe35",
						link:     hostVethWithIndex9,
					},
					{
						linkName: "eth0",
						link:     contVethWithIndex1,
					},
				},
				linkAddCalls: []linkAddCall{
					{
						link: &netlink.Veth{
							LinkAttrs: netlink.LinkAttrs{
								Name:  "eth0",
								Flags: net.FlagUp,
								MTU:   9001,
							},
							PeerName: "eni8ea2c11fe35",
						},
					},
				},
				linkSetupCalls: []linkSetupCall{
					{
						link: hostVethWithIndex9,
					},
					{
						link: contVethWithIndex1,
					},
				},
				routeReplaceCalls: []routeReplaceCall{
					{
						route: &netlink.Route{
							LinkIndex: contVethWithIndex1.Attrs().Index,
							Scope:     netlink.SCOPE_LINK,
							Table:     254,
							Dst: &net.IPNet{
								IP:   net.IPv4(169, 254, 1, 1),
								Mask: net.CIDRMask(32, 32),
							},
						},
						err: errors.New("some error"),
					},
				},
			},
			args: args{
				contVethName: "eth0",
				hostVethName: "eni8ea2c11fe35",
				ipAddr: &net.IPNet{
					IP:   net.ParseIP("192.168.120.1"),
					Mask: net.CIDRMask(32, 32),
				},
				mtu: 9001,
			},
			wantErr: errors.New("setup NS network: failed to add default gateway: some error"),
			index:   0,
		},
		{
			name: "failed to add default route",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "eni8ea2c11fe35",
						link:     hostVethWithIndex9,
					},
					{
						linkName: "eth0",
						link:     contVethWithIndex1,
					},
				},
				linkAddCalls: []linkAddCall{
					{
						link: &netlink.Veth{
							LinkAttrs: netlink.LinkAttrs{
								Name:  "eth0",
								Flags: net.FlagUp,
								MTU:   9001,
							},
							PeerName: "eni8ea2c11fe35",
						},
					},
				},
				linkSetupCalls: []linkSetupCall{
					{
						link: hostVethWithIndex9,
					},
					{
						link: contVethWithIndex1,
					},
				},
				routeReplaceCalls: []routeReplaceCall{
					{
						route: &netlink.Route{
							LinkIndex: contVethWithIndex1.Attrs().Index,
							Scope:     netlink.SCOPE_LINK,
							Table:     254,
							Dst: &net.IPNet{
								IP:   net.IPv4(169, 254, 1, 1),
								Mask: net.CIDRMask(32, 32),
							},
						},
					},
				},
				routeAddCalls: []routeAddCall{
					{
						route: &netlink.Route{
							LinkIndex: contVethWithIndex1.Attrs().Index,
							Scope:     netlink.SCOPE_UNIVERSE,
							Table:     254,
							Dst: &net.IPNet{
								IP:   net.IPv4zero,
								Mask: net.CIDRMask(0, 32),
							},
							Gw: net.IPv4(169, 254, 1, 1),
						},
						err: errors.New("some error"),
					},
				},
			},
			args: args{
				contVethName: "eth0",
				hostVethName: "eni8ea2c11fe35",
				ipAddr: &net.IPNet{
					IP:   net.ParseIP("192.168.120.1"),
					Mask: net.CIDRMask(32, 32),
				},
				mtu: 9001,
			},
			wantErr: errors.New("setup NS network: failed to add default route: some error"),
			index:   0,
		},
		{
			name: "failed to add container IP",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "eni8ea2c11fe35",
						link:     hostVethWithIndex9,
					},
					{
						linkName: "eth0",
						link:     contVethWithIndex1,
					},
				},
				linkAddCalls: []linkAddCall{
					{
						link: &netlink.Veth{
							LinkAttrs: netlink.LinkAttrs{
								Name:  "eth0",
								Flags: net.FlagUp,
								MTU:   9001,
							},
							PeerName: "eni8ea2c11fe35",
						},
					},
				},
				linkSetupCalls: []linkSetupCall{
					{
						link: hostVethWithIndex9,
					},
					{
						link: contVethWithIndex1,
					},
				},
				routeReplaceCalls: []routeReplaceCall{
					{
						route: &netlink.Route{
							LinkIndex: contVethWithIndex1.Attrs().Index,
							Scope:     netlink.SCOPE_LINK,
							Table:     254,
							Dst: &net.IPNet{
								IP:   net.IPv4(169, 254, 1, 1),
								Mask: net.CIDRMask(32, 32),
							},
						},
					},
				},
				routeAddCalls: []routeAddCall{
					{
						route: &netlink.Route{
							LinkIndex: contVethWithIndex1.Attrs().Index,
							Scope:     netlink.SCOPE_UNIVERSE,
							Table:     254,
							Dst: &net.IPNet{
								IP:   net.IPv4zero,
								Mask: net.CIDRMask(0, 32),
							},
							Gw: net.IPv4(169, 254, 1, 1),
						},
					},
				},
				addrAddCalls: []addrAddCall{
					{
						link: contVethWithIndex1,
						addr: &netlink.Addr{
							IPNet: &net.IPNet{
								IP:   net.ParseIP("192.168.120.1"),
								Mask: net.CIDRMask(32, 32),
							},
						},
						err: errors.New("some error"),
					},
				},
			},
			args: args{
				contVethName: "eth0",
				hostVethName: "eni8ea2c11fe35",
				ipAddr: &net.IPNet{
					IP:   net.ParseIP("192.168.120.1"),
					Mask: net.CIDRMask(32, 32),
				},
				mtu: 9001,
			},
			wantErr: errors.New("setup NS network: failed to add IP addr to \"eth0\": some error"),
			index:   0,
		},
		{
			name: "failed to add static ARP",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "eni8ea2c11fe35",
						link:     hostVethWithIndex9,
					},
					{
						linkName: "eth0",
						link:     contVethWithIndex1,
					},
				},
				linkAddCalls: []linkAddCall{
					{
						link: &netlink.Veth{
							LinkAttrs: netlink.LinkAttrs{
								Name:  "eth0",
								Flags: net.FlagUp,
								MTU:   9001,
							},
							PeerName: "eni8ea2c11fe35",
						},
					},
				},
				linkSetupCalls: []linkSetupCall{
					{
						link: hostVethWithIndex9,
					},
					{
						link: contVethWithIndex1,
					},
				},
				routeReplaceCalls: []routeReplaceCall{
					{
						route: &netlink.Route{
							LinkIndex: contVethWithIndex1.Attrs().Index,
							Scope:     netlink.SCOPE_LINK,
							Table:     254,
							Dst: &net.IPNet{
								IP:   net.IPv4(169, 254, 1, 1),
								Mask: net.CIDRMask(32, 32),
							},
						},
					},
				},
				routeAddCalls: []routeAddCall{
					{
						route: &netlink.Route{
							LinkIndex: contVethWithIndex1.Attrs().Index,
							Scope:     netlink.SCOPE_UNIVERSE,
							Table:     254,
							Dst: &net.IPNet{
								IP:   net.IPv4zero,
								Mask: net.CIDRMask(0, 32),
							},
							Gw: net.IPv4(169, 254, 1, 1),
						},
					},
				},
				addrAddCalls: []addrAddCall{
					{
						link: contVethWithIndex1,
						addr: &netlink.Addr{
							IPNet: &net.IPNet{
								IP:   net.ParseIP("192.168.120.1"),
								Mask: net.CIDRMask(32, 32),
							},
						},
					},
				},
				neighAddCalls: []neighAddCall{
					{
						neigh: &netlink.Neigh{
							LinkIndex:    contVethWithIndex1.Attrs().Index,
							State:        netlink.NUD_PERMANENT,
							IP:           net.IPv4(169, 254, 1, 1),
							HardwareAddr: hostVethWithIndex9.Attrs().HardwareAddr,
						},
						err: errors.New("some error"),
					},
				},
			},
			args: args{
				contVethName: "eth0",
				hostVethName: "eni8ea2c11fe35",
				ipAddr: &net.IPNet{
					IP:   net.ParseIP("192.168.120.1"),
					Mask: net.CIDRMask(32, 32),
				},
				mtu: 9001,
			},
			wantErr: errors.New("setup NS network: failed to add static ARP: some error"),
			index:   0,
		},
		{
			name: "failed to move hostVeth to host netNS",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "eni8ea2c11fe35",
						link:     hostVethWithIndex9,
					},
					{
						linkName: "eth0",
						link:     contVethWithIndex1,
					},
				},
				linkAddCalls: []linkAddCall{
					{
						link: &netlink.Veth{
							LinkAttrs: netlink.LinkAttrs{
								Name:  "eth0",
								Flags: net.FlagUp,
								MTU:   9001,
							},
							PeerName: "eni8ea2c11fe35",
						},
					},
				},
				linkSetupCalls: []linkSetupCall{
					{
						link: hostVethWithIndex9,
					},
					{
						link: contVethWithIndex1,
					},
				},
				routeReplaceCalls: []routeReplaceCall{
					{
						route: &netlink.Route{
							LinkIndex: contVethWithIndex1.Attrs().Index,
							Scope:     netlink.SCOPE_LINK,
							Table:     254,
							Dst: &net.IPNet{
								IP:   net.IPv4(169, 254, 1, 1),
								Mask: net.CIDRMask(32, 32),
							},
						},
					},
				},
				routeAddCalls: []routeAddCall{
					{
						route: &netlink.Route{
							LinkIndex: contVethWithIndex1.Attrs().Index,
							Scope:     netlink.SCOPE_UNIVERSE,
							Table:     254,
							Dst: &net.IPNet{
								IP:   net.IPv4zero,
								Mask: net.CIDRMask(0, 32),
							},
							Gw: net.IPv4(169, 254, 1, 1),
						},
					},
				},
				addrAddCalls: []addrAddCall{
					{
						link: contVethWithIndex1,
						addr: &netlink.Addr{
							IPNet: &net.IPNet{
								IP:   net.ParseIP("192.168.120.1"),
								Mask: net.CIDRMask(32, 32),
							},
						},
					},
				},
				neighAddCalls: []neighAddCall{
					{
						neigh: &netlink.Neigh{
							LinkIndex:    contVethWithIndex1.Attrs().Index,
							State:        netlink.NUD_PERMANENT,
							IP:           net.IPv4(169, 254, 1, 1),
							HardwareAddr: hostVethWithIndex9.Attrs().HardwareAddr,
						},
					},
				},
				linkSetNsFdCalls: []linkSetNsFdCall{
					{
						link: hostVethWithIndex9,
						fd:   3,
						err:  errors.New("some error"),
					},
				},
				nsFDCalls: []nsFDCall{
					{
						fd: uintptr(3),
					},
				},
			},
			args: args{
				contVethName: "eth0",
				hostVethName: "eni8ea2c11fe35",
				ipAddr: &net.IPNet{
					IP:   net.ParseIP("192.168.120.1"),
					Mask: net.CIDRMask(32, 32),
				},
				mtu: 9001,
			},
			wantErr: errors.New("setup NS network: failed to move veth to host netns: some error"),
			index:   0,
		},
		{
			name: "failed to enable IPv6 on eth0",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "eni8ea2c11fe35",
						link:     hostVethWithIndex9,
					},
					{
						linkName: "eth0",
						link:     contVethWithIndex1,
					},
				},
				linkAddCalls: []linkAddCall{
					{
						link: &netlink.Veth{
							LinkAttrs: netlink.LinkAttrs{
								Name:  "eth0",
								Flags: net.FlagUp,
								MTU:   9001,
							},
							PeerName: "eni8ea2c11fe35",
						},
					},
				},
				linkSetupCalls: []linkSetupCall{
					{
						link: hostVethWithIndex9,
					},
					{
						link: contVethWithIndex1,
					},
				},
				procSysSetCalls: []procSysSetCall{
					{
						key:   "net/ipv6/conf/eth0/disable_ipv6",
						value: "0",
						err:   errors.New("some error"),
					},
				},
			},
			args: args{
				contVethName: "eth0",
				hostVethName: "eni8ea2c11fe35",
				ipAddr: &net.IPNet{
					IP:   net.ParseIP("2001:db8:3333:4444:5555:6666:7777:8888"),
					Mask: net.CIDRMask(128, 128),
				},
				mtu: 9001,
			},
			wantErr: errors.New("setupVeth network: failed to enable IPv6 on container veth interface: some error"),
			index:   0,
		},
		{
			name: "failed to enable IPv6 on lo",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "eni8ea2c11fe35",
						link:     hostVethWithIndex9,
					},
					{
						linkName: "eth0",
						link:     contVethWithIndex1,
					},
				},
				linkAddCalls: []linkAddCall{
					{
						link: &netlink.Veth{
							LinkAttrs: netlink.LinkAttrs{
								Name:  "eth0",
								Flags: net.FlagUp,
								MTU:   9001,
							},
							PeerName: "eni8ea2c11fe35",
						},
					},
				},
				linkSetupCalls: []linkSetupCall{
					{
						link: hostVethWithIndex9,
					},
					{
						link: contVethWithIndex1,
					},
				},
				procSysSetCalls: []procSysSetCall{
					{
						key:   "net/ipv6/conf/eth0/disable_ipv6",
						value: "0",
					},
					{
						key:   "net/ipv6/conf/lo/disable_ipv6",
						value: "0",
						err:   errors.New("some error"),
					},
				},
			},
			args: args{
				contVethName: "eth0",
				hostVethName: "eni8ea2c11fe35",
				ipAddr: &net.IPNet{
					IP:   net.ParseIP("2001:db8:3333:4444:5555:6666:7777:8888"),
					Mask: net.CIDRMask(128, 128),
				},
				mtu: 9001,
			},
			wantErr: errors.New("setupVeth network: failed to enable IPv6 on container's lo interface: some error"),
			index:   0,
		},
		{
			name: "failed to wait IPv6 address become stable",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "eni8ea2c11fe35",
						link:     hostVethWithIndex9,
					},
					{
						linkName: "eth0",
						link:     contVethWithIndex1,
					},
					{
						linkName: "eth0",
						link:     contVethWithIndex1,
					},
				},
				linkAddCalls: []linkAddCall{
					{
						link: &netlink.Veth{
							LinkAttrs: netlink.LinkAttrs{
								Name:  "eth0",
								Flags: net.FlagUp,
								MTU:   9001,
							},
							PeerName: "eni8ea2c11fe35",
						},
					},
				},
				linkSetupCalls: []linkSetupCall{
					{
						link: hostVethWithIndex9,
					},
					{
						link: contVethWithIndex1,
					},
				},
				routeReplaceCalls: []routeReplaceCall{
					{
						route: &netlink.Route{
							LinkIndex: contVethWithIndex1.Attrs().Index,
							Scope:     netlink.SCOPE_LINK,
							Table:     254,
							Dst: &net.IPNet{
								IP:   net.IP{0xfe, 0x80, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
								Mask: net.CIDRMask(128, 128),
							},
						},
					},
				},
				routeAddCalls: []routeAddCall{
					{
						route: &netlink.Route{
							LinkIndex: contVethWithIndex1.Attrs().Index,
							Scope:     netlink.SCOPE_UNIVERSE,
							Table:     254,
							Dst: &net.IPNet{
								IP:   net.IPv6zero,
								Mask: net.CIDRMask(0, 128),
							},
							Gw: net.IP{0xfe, 0x80, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
						},
					},
				},
				addrAddCalls: []addrAddCall{
					{
						link: contVethWithIndex1,
						addr: &netlink.Addr{
							IPNet: &net.IPNet{
								IP:   net.ParseIP("2001:db8:3333:4444:5555:6666:7777:8888"),
								Mask: net.CIDRMask(128, 128),
							},
						},
					},
				},
				addrListCalls: []addrListCall{
					{
						link:   contVethWithIndex1,
						family: netlink.FAMILY_V6,
						err:    errors.New("some error"),
					},
				},
				neighAddCalls: []neighAddCall{
					{
						neigh: &netlink.Neigh{
							LinkIndex:    contVethWithIndex1.Attrs().Index,
							State:        netlink.NUD_PERMANENT,
							IP:           net.IP{0xfe, 0x80, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
							HardwareAddr: hostVethWithIndex9.Attrs().HardwareAddr,
						},
					},
				},
				procSysSetCalls: []procSysSetCall{
					{
						key:   "net/ipv6/conf/eth0/disable_ipv6",
						value: "0",
					},
					{
						key:   "net/ipv6/conf/lo/disable_ipv6",
						value: "0",
					},
				},
			},
			args: args{
				contVethName: "eth0",
				hostVethName: "eni8ea2c11fe35",
				ipAddr: &net.IPNet{
					IP:   net.ParseIP("2001:db8:3333:4444:5555:6666:7777:8888"),
					Mask: net.CIDRMask(128, 128),
				},
				mtu: 9001,
			},
			wantErr: errors.New("setup NS network: failed while waiting for v6 addresses to be stable: could not list addresses: some error"),
			index:   0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			netLink := mock_netlinkwrapper.NewMockNetLink(ctrl)
			for _, call := range tt.fields.linkByNameCalls {
				netLink.EXPECT().LinkByName(call.linkName).Return(call.link, call.err)
			}
			for _, call := range tt.fields.linkAddCalls {
				netLink.EXPECT().LinkAdd(call.link).Return(call.err)
			}
			for _, call := range tt.fields.linkSetupCalls {
				netLink.EXPECT().LinkSetUp(call.link).Return(call.err)
			}
			for _, call := range tt.fields.routeReplaceCalls {
				netLink.EXPECT().RouteReplace(call.route).Return(call.err)
			}
			for _, call := range tt.fields.routeAddCalls {
				netLink.EXPECT().RouteAdd(call.route).Return(call.err)
			}
			for _, call := range tt.fields.addrAddCalls {
				netLink.EXPECT().AddrAdd(call.link, call.addr).Return(call.err)
			}
			for _, call := range tt.fields.addrListCalls {
				netLink.EXPECT().AddrList(call.link, call.family).Return(call.addrs, call.err)
			}
			for _, call := range tt.fields.neighAddCalls {
				netLink.EXPECT().NeighAdd(call.neigh).Return(call.err)
			}
			for _, call := range tt.fields.linkSetNsFdCalls {
				netLink.EXPECT().LinkSetNsFd(call.link, call.fd).Return(call.err)
			}
			for _, call := range tt.fields.linkRuleAddCalls {
				netLink.EXPECT().NewRule().Return(call.rule)
				netLink.EXPECT().RuleAdd(call.rule).Return(call.err)
			}

			procSys := mock_procsyswrapper.NewMockProcSys(ctrl)
			for _, call := range tt.fields.procSysSetCalls {
				procSys.EXPECT().Set(call.key, call.value).Return(call.err)
			}
			hostNS := mock_ns.NewMockNetNS(ctrl)
			for _, call := range tt.fields.nsFDCalls {
				// we just assume the createVethContext executes, the logic of createVethContext will be tested by createVethContext itself.
				hostNS.EXPECT().Fd().Return(call.fd)
			}

			createVethContext := &createVethPairContext{
				contVethName: tt.args.contVethName,
				hostVethName: tt.args.hostVethName,
				ipAddr:       tt.args.ipAddr,
				mtu:          tt.args.mtu,
				netLink:      netLink,
				procSys:      procSys,
				index:        tt.index,
			}
			err := createVethContext.run(hostNS)
			if tt.wantErr != nil {
				assert.EqualError(t, err, tt.wantErr.Error())
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func Test_linuxNetwork_setupVeth(t *testing.T) {
	hostVethWithIndex9 := &netlink.Veth{
		LinkAttrs: netlink.LinkAttrs{
			Name:  "eni8ea2c11fe35",
			Index: 9,
		},
	}
	type linkByNameCall struct {
		linkName string
		link     netlink.Link
		err      error
	}
	type linkDelCall struct {
		link netlink.Link
		err  error
	}
	type linkSetupCall struct {
		link netlink.Link
		err  error
	}
	type withNetNSPathCall struct {
		netNSPath string
		err       error
	}
	type procSysSetCall struct {
		key   string
		value string
		err   error
	}

	type fields struct {
		linkByNameCalls    []linkByNameCall
		linkDelCalls       []linkDelCall
		linkSetupCalls     []linkSetupCall
		withNetNSPathCalls []withNetNSPathCall
		procSysSetCalls    []procSysSetCall
	}

	type args struct {
		hostVethName string
		contVethName string
		netnsPath    string
		mtu          int
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    netlink.Link
		wantErr error
	}{
		{
			name: "successfully setup veth - old hostVeth don't exists",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "eni8ea2c11fe35",
						err:      errors.New("not exists"),
					},
					{
						linkName: "eni8ea2c11fe35",
						link:     hostVethWithIndex9,
					},
				},
				linkSetupCalls: []linkSetupCall{
					{
						link: hostVethWithIndex9,
					},
				},
				withNetNSPathCalls: []withNetNSPathCall{
					{
						netNSPath: "/proc/42/ns/net",
					},
				},
				procSysSetCalls: []procSysSetCall{
					{
						key:   "net/ipv6/conf/eni8ea2c11fe35/accept_ra",
						value: "0",
					},
					{
						key:   "net/ipv6/conf/eni8ea2c11fe35/accept_redirects",
						value: "1",
					},
					{
						key:   "net/ipv6/conf/eni8ea2c11fe35/forwarding",
						value: "0",
					},
				},
			},
			args: args{
				hostVethName: "eni8ea2c11fe35",
				contVethName: "eth0",
				netnsPath:    "/proc/42/ns/net",
			},
			want: hostVethWithIndex9,
		},
		{
			name: "successfully setup veth - old hostVeth exists",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "eni8ea2c11fe35",
						link:     hostVethWithIndex9,
					},
					{
						linkName: "eni8ea2c11fe35",
						link:     hostVethWithIndex9,
					},
				},
				linkDelCalls: []linkDelCall{
					{
						link: hostVethWithIndex9,
					},
				},
				linkSetupCalls: []linkSetupCall{
					{
						link: hostVethWithIndex9,
					},
				},
				withNetNSPathCalls: []withNetNSPathCall{
					{
						netNSPath: "/proc/42/ns/net",
					},
				},
				procSysSetCalls: []procSysSetCall{
					{
						key:   "net/ipv6/conf/eni8ea2c11fe35/accept_ra",
						value: "0",
					},
					{
						key:   "net/ipv6/conf/eni8ea2c11fe35/accept_redirects",
						value: "1",
					},
					{
						key:   "net/ipv6/conf/eni8ea2c11fe35/forwarding",
						value: "0",
					},
				},
			},
			args: args{
				hostVethName: "eni8ea2c11fe35",
				contVethName: "eth0",
				netnsPath:    "/proc/42/ns/net",
			},
			want: hostVethWithIndex9,
		},
		{
			name: "failed to delete old hostVeth",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "eni8ea2c11fe35",
						link:     hostVethWithIndex9,
					},
				},
				linkDelCalls: []linkDelCall{
					{
						link: hostVethWithIndex9,
						err:  errors.New("some error"),
					},
				},
			},
			args: args{
				hostVethName: "eni8ea2c11fe35",
				contVethName: "eth0",
				netnsPath:    "/proc/42/ns/net",
			},
			wantErr: errors.New("failed to delete old hostVeth eni8ea2c11fe35: some error"),
		},
		{
			name: "failed to create veth pair",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "eni8ea2c11fe35",
						err:      errors.New("not exists"),
					},
				},
				withNetNSPathCalls: []withNetNSPathCall{
					{
						netNSPath: "/proc/42/ns/net",
						err:       errors.New("some error"),
					},
				},
			},
			args: args{
				hostVethName: "eni8ea2c11fe35",
				contVethName: "eth0",
				netnsPath:    "/proc/42/ns/net",
			},
			wantErr: errors.New("failed to setup veth network: some error"),
		},
		{
			name: "failed to obtain created hostVeth",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "eni8ea2c11fe35",
						err:      errors.New("not exists"),
					},
					{
						linkName: "eni8ea2c11fe35",
						err:      errors.New("not exists"),
					},
				},
				withNetNSPathCalls: []withNetNSPathCall{
					{
						netNSPath: "/proc/42/ns/net",
					},
				},
			},
			args: args{
				hostVethName: "eni8ea2c11fe35",
				contVethName: "eth0",
				netnsPath:    "/proc/42/ns/net",
			},
			wantErr: errors.New("failed to find hostVeth eni8ea2c11fe35: not exists"),
		},
		{
			name: "failed to disable IPv6 accept_ra",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "eni8ea2c11fe35",
						err:      errors.New("not exists"),
					},
					{
						linkName: "eni8ea2c11fe35",
						link:     hostVethWithIndex9,
					},
				},
				withNetNSPathCalls: []withNetNSPathCall{
					{
						netNSPath: "/proc/42/ns/net",
					},
				},
				procSysSetCalls: []procSysSetCall{
					{
						key:   "net/ipv6/conf/eni8ea2c11fe35/accept_ra",
						value: "0",
						err:   errors.New("some error"),
					},
				},
			},
			args: args{
				hostVethName: "eni8ea2c11fe35",
				contVethName: "eth0",
				netnsPath:    "/proc/42/ns/net",
			},
			wantErr: errors.New("failed to disable IPv6 router advertisements: some error"),
		},
		{
			name: "failed to disable IPv6 accept_redirects",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "eni8ea2c11fe35",
						err:      errors.New("not exists"),
					},
					{
						linkName: "eni8ea2c11fe35",
						link:     hostVethWithIndex9,
					},
				},
				withNetNSPathCalls: []withNetNSPathCall{
					{
						netNSPath: "/proc/42/ns/net",
					},
				},
				procSysSetCalls: []procSysSetCall{
					{
						key:   "net/ipv6/conf/eni8ea2c11fe35/accept_ra",
						value: "0",
					},
					{
						key:   "net/ipv6/conf/eni8ea2c11fe35/accept_redirects",
						value: "1",
						err:   errors.New("some error"),
					},
				},
			},
			args: args{
				hostVethName: "eni8ea2c11fe35",
				contVethName: "eth0",
				netnsPath:    "/proc/42/ns/net",
			},
			wantErr: errors.New("failed to disable IPv6 ICMP redirects: some error"),
		},
		{
			name: "failed to disable IPv6 accept_ra and accept_redirects due to lack IPv6 support",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "eni8ea2c11fe35",
						err:      errors.New("not exists"),
					},
					{
						linkName: "eni8ea2c11fe35",
						link:     hostVethWithIndex9,
					},
				},
				linkSetupCalls: []linkSetupCall{
					{
						link: hostVethWithIndex9,
					},
				},
				withNetNSPathCalls: []withNetNSPathCall{
					{
						netNSPath: "/proc/42/ns/net",
					},
				},
				procSysSetCalls: []procSysSetCall{
					{
						key:   "net/ipv6/conf/eni8ea2c11fe35/accept_ra",
						value: "0",
						err:   syscall.ENOENT,
					},
					{
						key:   "net/ipv6/conf/eni8ea2c11fe35/accept_redirects",
						value: "1",
						err:   syscall.ENOENT,
					},
					{
						key:   "net/ipv6/conf/eni8ea2c11fe35/forwarding",
						value: "0",
						err:   syscall.ENOENT,
					},
				},
			},
			args: args{
				hostVethName: "eni8ea2c11fe35",
				contVethName: "eth0",
				netnsPath:    "/proc/42/ns/net",
			},
			want: hostVethWithIndex9,
		},
		{
			name: "failed to setUp hostVeth",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "eni8ea2c11fe35",
						err:      errors.New("not exists"),
					},
					{
						linkName: "eni8ea2c11fe35",
						link:     hostVethWithIndex9,
					},
				},
				linkSetupCalls: []linkSetupCall{
					{
						link: hostVethWithIndex9,
						err:  errors.New("some error"),
					},
				},
				withNetNSPathCalls: []withNetNSPathCall{
					{
						netNSPath: "/proc/42/ns/net",
					},
				},
				procSysSetCalls: []procSysSetCall{
					{
						key:   "net/ipv6/conf/eni8ea2c11fe35/accept_ra",
						value: "0",
					},
					{
						key:   "net/ipv6/conf/eni8ea2c11fe35/accept_redirects",
						value: "1",
					},
					{
						key:   "net/ipv6/conf/eni8ea2c11fe35/forwarding",
						value: "0",
					},
				},
			},
			args: args{
				hostVethName: "eni8ea2c11fe35",
				contVethName: "eth0",
				netnsPath:    "/proc/42/ns/net",
			},
			wantErr: errors.New("failed to setup hostVeth eni8ea2c11fe35: some error"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			netLink := mock_netlinkwrapper.NewMockNetLink(ctrl)
			for _, call := range tt.fields.linkByNameCalls {
				netLink.EXPECT().LinkByName(call.linkName).Return(call.link, call.err)
			}
			for _, call := range tt.fields.linkDelCalls {
				netLink.EXPECT().LinkDel(call.link).Return(call.err)
			}
			for _, call := range tt.fields.linkSetupCalls {
				netLink.EXPECT().LinkSetUp(call.link).Return(call.err)
			}

			ns := mock_nswrapper.NewMockNS(ctrl)
			for _, call := range tt.fields.withNetNSPathCalls {
				// we just assume the createVethContext executes, the logic of createVethContext will be tested by createVethContext itself.
				ns.EXPECT().WithNetNSPath(call.netNSPath, gomock.Any()).Return(call.err)
			}

			procSys := mock_procsyswrapper.NewMockProcSys(ctrl)
			for _, call := range tt.fields.procSysSetCalls {
				procSys.EXPECT().Set(call.key, call.value).Return(call.err)
			}

			n := &linuxNetwork{
				netLink: netLink,
				ns:      ns,
				procSys: procSys,
			}
			got, err := n.setupVeth(tt.args.hostVethName, tt.args.contVethName, tt.args.netnsPath, nil, tt.args.mtu, testLogger, 0)
			if tt.wantErr != nil {
				assert.EqualError(t, err, tt.wantErr.Error())
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func Test_linuxNetwork_setupVlan(t *testing.T) {
	vlanID := 7
	parentIfIndex := 3
	eniMac := "01:23:45:67:89:ab"

	vlanLinkPostAddWithIndex9 := buildVlanLink("vlan.eth.7", vlanID, parentIfIndex, eniMac)
	vlanLinkPostAddWithIndex9.Index = 9
	type linkByNameCall struct {
		linkName string
		link     netlink.Link
		err      error
	}
	type linkAddCall struct {
		link      netlink.Link
		linkIndex int
		err       error
	}
	type linkDelCall struct {
		link netlink.Link
		err  error
	}
	type linkSetupCall struct {
		link netlink.Link
		err  error
	}
	type routeReplaceCall struct {
		route *netlink.Route
		err   error
	}
	type procSysSetCall struct {
		key   string
		value string
		err   error
	}

	type fields struct {
		linkByNameCalls   []linkByNameCall
		linkAddCalls      []linkAddCall
		linkDelCalls      []linkDelCall
		linkSetupCalls    []linkSetupCall
		routeReplaceCalls []routeReplaceCall
		procSysSetCalls   []procSysSetCall
	}

	type args struct {
		vlanID        int
		eniMAC        string
		subnetGW      string
		parentIfIndex int
		rtTable       int
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    netlink.Link
		wantErr error
	}{
		{
			name: "successfully setup vlan - old vlan don't exists",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "vlan.eth.7",
						err:      errors.Errorf("don't exists"),
					},
				},
				linkAddCalls: []linkAddCall{
					{
						link:      buildVlanLink("vlan.eth.7", vlanID, parentIfIndex, eniMac),
						linkIndex: 9,
					},
				},
				linkSetupCalls: []linkSetupCall{
					{
						link: vlanLinkPostAddWithIndex9,
					},
				},
				routeReplaceCalls: []routeReplaceCall{
					{
						route: &netlink.Route{
							LinkIndex: 9,
							Dst:       &net.IPNet{IP: net.ParseIP("192.168.120.1"), Mask: net.CIDRMask(32, 32)},
							Scope:     netlink.SCOPE_LINK,
							Table:     107,
						},
					},
					{
						route: &netlink.Route{
							LinkIndex: 9,
							Dst:       &net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(0, 32)},
							Scope:     netlink.SCOPE_UNIVERSE,
							Gw:        net.ParseIP("192.168.120.1"),
							Table:     107,
						},
					},
				},
				procSysSetCalls: []procSysSetCall{
					{
						key:   "net/ipv6/conf/vlan.eth.7/accept_ra",
						value: "0",
					},
					{
						key:   "net/ipv6/conf/vlan.eth.7/accept_redirects",
						value: "1",
					},
					{
						key:   "net/ipv6/conf/vlan.eth.7/forwarding",
						value: "0",
					},
				},
			},
			args: args{
				vlanID:        vlanID,
				eniMAC:        eniMac,
				subnetGW:      "192.168.120.1",
				parentIfIndex: parentIfIndex,
				rtTable:       107,
			},
			want: vlanLinkPostAddWithIndex9,
		},
		{
			name: "successfully setup vlan - old vlan exists",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "vlan.eth.7",
						link:     buildVlanLink("vlan.eth.7", vlanID, parentIfIndex, eniMac),
					},
				},
				linkDelCalls: []linkDelCall{
					{
						link: buildVlanLink("vlan.eth.7", vlanID, parentIfIndex, eniMac),
					},
				},
				linkAddCalls: []linkAddCall{
					{
						link:      buildVlanLink("vlan.eth.7", vlanID, parentIfIndex, eniMac),
						linkIndex: 9,
					},
				},
				linkSetupCalls: []linkSetupCall{
					{
						link: vlanLinkPostAddWithIndex9,
					},
				},
				routeReplaceCalls: []routeReplaceCall{
					{
						route: &netlink.Route{
							LinkIndex: 9,
							Dst:       &net.IPNet{IP: net.ParseIP("192.168.120.1"), Mask: net.CIDRMask(32, 32)},
							Scope:     netlink.SCOPE_LINK,
							Table:     107,
						},
					},
					{
						route: &netlink.Route{
							LinkIndex: 9,
							Dst:       &net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(0, 32)},
							Scope:     netlink.SCOPE_UNIVERSE,
							Gw:        net.ParseIP("192.168.120.1"),
							Table:     107,
						},
					},
				},
				procSysSetCalls: []procSysSetCall{
					{
						key:   "net/ipv6/conf/vlan.eth.7/accept_ra",
						value: "0",
					},
					{
						key:   "net/ipv6/conf/vlan.eth.7/accept_redirects",
						value: "1",
					},
					{
						key:   "net/ipv6/conf/vlan.eth.7/forwarding",
						value: "0",
					},
				},
			},
			args: args{
				vlanID:        vlanID,
				eniMAC:        eniMac,
				subnetGW:      "192.168.120.1",
				parentIfIndex: parentIfIndex,
				rtTable:       107,
			},
			want: vlanLinkPostAddWithIndex9,
		},
		{
			name: "failed to delete old vlan link",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "vlan.eth.7",
						link:     buildVlanLink("vlan.eth.7", vlanID, parentIfIndex, eniMac),
					},
				},
				linkDelCalls: []linkDelCall{
					{
						link: buildVlanLink("vlan.eth.7", vlanID, parentIfIndex, eniMac),
						err:  errors.New("some error"),
					},
				},
			},
			args: args{
				vlanID:        vlanID,
				eniMAC:        eniMac,
				subnetGW:      "192.168.120.1",
				parentIfIndex: parentIfIndex,
				rtTable:       107,
			},
			wantErr: errors.New("failed to delete old vlan link vlan.eth.7: some error"),
		},
		{
			name: "failed to add vlan link",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "vlan.eth.7",
						err:      errors.Errorf("don't exists"),
					},
				},
				linkAddCalls: []linkAddCall{
					{
						link: buildVlanLink("vlan.eth.7", vlanID, parentIfIndex, eniMac),
						err:  errors.New("some error"),
					},
				},
			},
			args: args{
				vlanID:        vlanID,
				eniMAC:        eniMac,
				subnetGW:      "192.168.120.1",
				parentIfIndex: parentIfIndex,
				rtTable:       107,
			},
			wantErr: errors.New("failed to add vlan link vlan.eth.7: some error"),
		},
		{
			name: "failed to setUp vlan link",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "vlan.eth.7",
						err:      errors.Errorf("don't exists"),
					},
				},
				linkAddCalls: []linkAddCall{
					{
						link:      buildVlanLink("vlan.eth.7", vlanID, parentIfIndex, eniMac),
						linkIndex: 9,
					},
				},
				linkSetupCalls: []linkSetupCall{
					{
						link: vlanLinkPostAddWithIndex9,
						err:  errors.New("some error"),
					},
				},
				procSysSetCalls: []procSysSetCall{
					{
						key:   "net/ipv6/conf/vlan.eth.7/accept_ra",
						value: "0",
					},
					{
						key:   "net/ipv6/conf/vlan.eth.7/accept_redirects",
						value: "1",
					},
					{
						key:   "net/ipv6/conf/vlan.eth.7/forwarding",
						value: "0",
					},
				},
			},
			args: args{
				vlanID:        vlanID,
				eniMAC:        eniMac,
				subnetGW:      "192.168.120.1",
				parentIfIndex: parentIfIndex,
				rtTable:       107,
			},
			wantErr: errors.New("failed to setUp vlan link vlan.eth.7: some error"),
		},
		{
			name: "failed to replace routes",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "vlan.eth.7",
						err:      errors.Errorf("don't exists"),
					},
				},
				linkAddCalls: []linkAddCall{
					{
						link:      buildVlanLink("vlan.eth.7", vlanID, parentIfIndex, eniMac),
						linkIndex: 9,
					},
				},
				linkSetupCalls: []linkSetupCall{
					{
						link: vlanLinkPostAddWithIndex9,
					},
				},
				routeReplaceCalls: []routeReplaceCall{
					{
						route: &netlink.Route{
							LinkIndex: 9,
							Dst:       &net.IPNet{IP: net.ParseIP("192.168.120.1"), Mask: net.CIDRMask(32, 32)},
							Scope:     netlink.SCOPE_LINK,
							Table:     107,
						},
						err: errors.New("some error"),
					},
				},
				procSysSetCalls: []procSysSetCall{
					{
						key:   "net/ipv6/conf/vlan.eth.7/accept_ra",
						value: "0",
					},
					{
						key:   "net/ipv6/conf/vlan.eth.7/accept_redirects",
						value: "1",
					},
					{
						key:   "net/ipv6/conf/vlan.eth.7/forwarding",
						value: "0",
					},
				},
			},
			args: args{
				vlanID:        vlanID,
				eniMAC:        eniMac,
				subnetGW:      "192.168.120.1",
				parentIfIndex: parentIfIndex,
				rtTable:       107,
			},
			wantErr: errors.New("failed to replace route entry 192.168.120.1 via 192.168.120.1: some error"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			netLink := mock_netlinkwrapper.NewMockNetLink(ctrl)
			for _, call := range tt.fields.linkByNameCalls {
				netLink.EXPECT().LinkByName(call.linkName).Return(call.link, call.err)
			}
			for _, call := range tt.fields.linkAddCalls {
				netLink.EXPECT().LinkAdd(call.link).DoAndReturn(func(link netlink.Link) error {
					if call.err != nil {
						return call.err
					}
					vlanBeforeAdd := link.(*netlink.Vlan)
					vlanBeforeAdd.Index = call.linkIndex
					return nil
				})
			}
			for _, call := range tt.fields.linkDelCalls {
				netLink.EXPECT().LinkDel(call.link).Return(call.err)
			}
			for _, call := range tt.fields.linkSetupCalls {
				netLink.EXPECT().LinkSetUp(call.link).Return(call.err)
			}
			for _, call := range tt.fields.routeReplaceCalls {
				netLink.EXPECT().RouteReplace(call.route).Return(call.err)
			}
			procSys := mock_procsyswrapper.NewMockProcSys(ctrl)
			for _, call := range tt.fields.procSysSetCalls {
				procSys.EXPECT().Set(call.key, call.value).Return(call.err)
			}

			n := &linuxNetwork{
				netLink: netLink,
				procSys: procSys,
			}
			got, err := n.setupVlan(tt.args.vlanID, tt.args.eniMAC, tt.args.subnetGW, tt.args.parentIfIndex, tt.args.rtTable, testLogger)
			if tt.wantErr != nil {
				assert.EqualError(t, err, tt.wantErr.Error())
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func Test_linuxNetwork_teardownVlan(t *testing.T) {
	type linkByNameCall struct {
		linkName string
		link     netlink.Link
		err      error
	}
	type linkDelCall struct {
		link netlink.Link
		err  error
	}

	type fields struct {
		linkByNameCalls []linkByNameCall
		linkDelCalls    []linkDelCall
	}
	type args struct {
		vlanID int
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr error
	}{
		{
			name: "successfully deleted vlan link",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "vlan.eth.7",
						link:     &netlink.Vlan{VlanId: 7},
					},
				},
				linkDelCalls: []linkDelCall{
					{
						link: &netlink.Vlan{VlanId: 7},
					},
				},
			},
			args: args{
				vlanID: 7,
			},
		},
		{
			name: "failed to deleted vlan link",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "vlan.eth.7",
						link:     &netlink.Vlan{VlanId: 7},
					},
				},
				linkDelCalls: []linkDelCall{
					{
						link: &netlink.Vlan{VlanId: 7},
						err:  errors.New("some error"),
					},
				},
			},
			args: args{
				vlanID: 7,
			},
			wantErr: errors.New("failed to delete vlan link vlan.eth.7: some error"),
		},
		{
			name: "vlan link don't exists",
			fields: fields{
				linkByNameCalls: []linkByNameCall{
					{
						linkName: "vlan.eth.7",
						err:      errors.New("don't exists"),
					},
				},
			},
			args: args{
				vlanID: 7,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			netLink := mock_netlinkwrapper.NewMockNetLink(ctrl)
			for _, call := range tt.fields.linkByNameCalls {
				netLink.EXPECT().LinkByName(call.linkName).Return(call.link, call.err)
			}
			for _, call := range tt.fields.linkDelCalls {
				netLink.EXPECT().LinkDel(call.link).Return(call.err)
			}

			n := &linuxNetwork{
				netLink: netLink,
			}
			err := n.teardownVlan(tt.args.vlanID, testLogger)
			if tt.wantErr != nil {
				assert.EqualError(t, err, tt.wantErr.Error())
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func Test_linuxNetwork_setupIPBasedContainerRouteRules(t *testing.T) {
	hostVethAttrs := netlink.LinkAttrs{
		Name:  "eni00bcc08c834",
		Index: 7,
	}
	containerAddr := &net.IPNet{
		IP:   net.ParseIP("192.168.100.42"),
		Mask: net.CIDRMask(32, 32),
	}
	containerV6Addr := &net.IPNet{
		IP:   net.ParseIP("2600::2"),
		Mask: net.CIDRMask(128, 128),
	}

	toContainerRule := netlink.NewRule()
	toContainerRule.Dst = containerAddr
	toContainerRule.Priority = networkutils.ToContainerRulePriority
	toContainerRule.Table = unix.RT_TABLE_MAIN

	toContainerV6Rule := netlink.NewRule()
	toContainerV6Rule.Dst = containerV6Addr
	toContainerV6Rule.Priority = networkutils.ToContainerRulePriority
	toContainerV6Rule.Table = unix.RT_TABLE_MAIN

	fromContainerRule := netlink.NewRule()
	fromContainerRule.Src = containerAddr
	fromContainerRule.Priority = networkutils.FromPodRulePriority
	fromContainerRule.Table = 101

	fromContainerV6Rule := netlink.NewRule()
	fromContainerV6Rule.Src = containerV6Addr
	fromContainerV6Rule.Priority = networkutils.FromPodRulePriority
	fromContainerV6Rule.Table = 101

	type routeReplaceCall struct {
		route *netlink.Route
		err   error
	}
	type ruleAddCall struct {
		rule *netlink.Rule
		err  error
	}
	type fields struct {
		routeReplaceCalls []routeReplaceCall
		ruleAddCalls      []ruleAddCall
	}
	type args struct {
		hostVethAttrs netlink.LinkAttrs
		containerAddr *net.IPNet
		rtTable       int
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr error
	}{
		{
			name: "successfully setup routes and rules - without dedicated route table - IPv4",
			fields: fields{
				routeReplaceCalls: []routeReplaceCall{
					{
						route: &netlink.Route{
							LinkIndex: hostVethAttrs.Index,
							Scope:     netlink.SCOPE_LINK,
							Dst:       containerAddr,
							Table:     unix.RT_TABLE_MAIN,
						},
					},
				},
				ruleAddCalls: []ruleAddCall{
					{
						rule: toContainerRule,
					},
				},
			},
			args: args{
				hostVethAttrs: hostVethAttrs,
				containerAddr: containerAddr,
				rtTable:       unix.RT_TABLE_MAIN,
			},
		},
		{
			name: "successfully setup routes and rules - without dedicated route table - IPv6",
			fields: fields{
				routeReplaceCalls: []routeReplaceCall{
					{
						route: &netlink.Route{
							LinkIndex: hostVethAttrs.Index,
							Scope:     netlink.SCOPE_LINK,
							Dst:       containerV6Addr,
							Table:     unix.RT_TABLE_MAIN,
						},
					},
				},
				ruleAddCalls: []ruleAddCall{
					{
						rule: toContainerV6Rule,
					},
				},
			},
			args: args{
				hostVethAttrs: hostVethAttrs,
				containerAddr: containerV6Addr,
				rtTable:       unix.RT_TABLE_MAIN,
			},
		},
		{
			name: "successfully setup routes and rules - with dedicated route table - IPv4",
			fields: fields{
				routeReplaceCalls: []routeReplaceCall{
					{
						route: &netlink.Route{
							LinkIndex: hostVethAttrs.Index,
							Scope:     netlink.SCOPE_LINK,
							Dst:       containerAddr,
							Table:     unix.RT_TABLE_MAIN,
						},
					},
				},
				ruleAddCalls: []ruleAddCall{
					{
						rule: toContainerRule,
					},
					{
						rule: fromContainerRule,
					},
				},
			},
			args: args{
				hostVethAttrs: hostVethAttrs,
				containerAddr: containerAddr,
				rtTable:       101,
			},
		},
		{
			name: "successfully setup routes and rules - with dedicated route table - IPv6",
			fields: fields{
				routeReplaceCalls: []routeReplaceCall{
					{
						route: &netlink.Route{
							LinkIndex: hostVethAttrs.Index,
							Scope:     netlink.SCOPE_LINK,
							Dst:       containerV6Addr,
							Table:     unix.RT_TABLE_MAIN,
						},
					},
				},
				ruleAddCalls: []ruleAddCall{
					{
						rule: toContainerV6Rule,
					},
					{
						rule: fromContainerV6Rule,
					},
				},
			},
			args: args{
				hostVethAttrs: hostVethAttrs,
				containerAddr: containerV6Addr,
				rtTable:       101,
			},
		},
		{
			name: "successfully setup routes and rules - toContainerRule already exists",
			fields: fields{
				routeReplaceCalls: []routeReplaceCall{
					{
						route: &netlink.Route{
							LinkIndex: hostVethAttrs.Index,
							Scope:     netlink.SCOPE_LINK,
							Dst:       containerAddr,
							Table:     unix.RT_TABLE_MAIN,
						},
					},
				},
				ruleAddCalls: []ruleAddCall{
					{
						rule: toContainerRule,
						err:  syscall.EEXIST,
					},
					{
						rule: fromContainerRule,
					},
				},
			},
			args: args{
				hostVethAttrs: hostVethAttrs,
				containerAddr: containerAddr,
				rtTable:       101,
			},
		},
		{
			name: "successfully setup routes and rules - fromContainerRule already exists",
			fields: fields{
				routeReplaceCalls: []routeReplaceCall{
					{
						route: &netlink.Route{
							LinkIndex: hostVethAttrs.Index,
							Scope:     netlink.SCOPE_LINK,
							Dst:       containerAddr,
							Table:     unix.RT_TABLE_MAIN,
						},
					},
				},
				ruleAddCalls: []ruleAddCall{
					{
						rule: toContainerRule,
					},
					{
						rule: fromContainerRule,
						err:  syscall.EEXIST,
					},
				},
			},
			args: args{
				hostVethAttrs: hostVethAttrs,
				containerAddr: containerAddr,
				rtTable:       101,
			},
		},
		{
			name: "failed to setup container route",
			fields: fields{
				routeReplaceCalls: []routeReplaceCall{
					{
						route: &netlink.Route{
							LinkIndex: hostVethAttrs.Index,
							Scope:     netlink.SCOPE_LINK,
							Dst:       containerAddr,
							Table:     unix.RT_TABLE_MAIN,
						},
						err: errors.New("some error"),
					},
				},
			},
			args: args{
				hostVethAttrs: hostVethAttrs,
				containerAddr: containerAddr,
				rtTable:       101,
			},
			wantErr: errors.New("failed to setup container route, containerAddr=192.168.100.42/32, hostVeth=eni00bcc08c834, rtTable=main: some error"),
		},
		{
			name: "failed to setup toContainer rule",
			fields: fields{
				routeReplaceCalls: []routeReplaceCall{
					{
						route: &netlink.Route{
							LinkIndex: hostVethAttrs.Index,
							Scope:     netlink.SCOPE_LINK,
							Dst:       containerAddr,
							Table:     unix.RT_TABLE_MAIN,
						},
					},
				},
				ruleAddCalls: []ruleAddCall{
					{
						rule: toContainerRule,
						err:  errors.New("some error"),
					},
				},
			},
			args: args{
				hostVethAttrs: hostVethAttrs,
				containerAddr: containerAddr,
				rtTable:       101,
			},
			wantErr: errors.New("failed to setup toContainer rule, containerAddr=192.168.100.42/32, rtTable=main: some error"),
		},
		{
			name: "failed to setup fromContainer rule",
			fields: fields{
				routeReplaceCalls: []routeReplaceCall{
					{
						route: &netlink.Route{
							LinkIndex: hostVethAttrs.Index,
							Scope:     netlink.SCOPE_LINK,
							Dst:       containerAddr,
							Table:     unix.RT_TABLE_MAIN,
						},
					},
				},
				ruleAddCalls: []ruleAddCall{
					{
						rule: toContainerRule,
					},
					{
						rule: fromContainerRule,
						err:  errors.New("some error"),
					},
				},
			},
			args: args{
				hostVethAttrs: hostVethAttrs,
				containerAddr: containerAddr,
				rtTable:       101,
			},
			wantErr: errors.New("failed to setup fromContainer rule, containerAddr=192.168.100.42/32, rtTable=101: some error"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			hostVeth := mock_netlink.NewMockLink(ctrl)
			hostVeth.EXPECT().Attrs().Return(&tt.args.hostVethAttrs).AnyTimes()

			netLink := mock_netlinkwrapper.NewMockNetLink(ctrl)
			netLink.EXPECT().NewRule().DoAndReturn(func() *netlink.Rule { return netlink.NewRule() }).AnyTimes()
			for _, call := range tt.fields.routeReplaceCalls {
				netLink.EXPECT().RouteReplace(call.route).Return(call.err)
			}
			for _, call := range tt.fields.ruleAddCalls {
				netLink.EXPECT().RuleAdd(call.rule).Return(call.err)
			}

			n := &linuxNetwork{
				netLink: netLink,
			}
			err := n.setupIPBasedContainerRouteRules(hostVeth, tt.args.containerAddr, tt.args.rtTable, testLogger)
			if tt.wantErr != nil {
				assert.EqualError(t, err, tt.wantErr.Error())
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func Test_linuxNetwork_teardownIPBasedContainerRouteRules(t *testing.T) {
	containerAddr := &net.IPNet{
		IP:   net.ParseIP("192.168.100.42"),
		Mask: net.CIDRMask(32, 32),
	}
	containerV6Addr := &net.IPNet{
		IP:   net.ParseIP("2600::2"),
		Mask: net.CIDRMask(128, 128),
	}

	toContainerRoute := &netlink.Route{
		Scope: netlink.SCOPE_LINK,
		Dst:   containerAddr,
		Table: unix.RT_TABLE_MAIN,
	}
	toContainerV6Route := &netlink.Route{
		Scope: netlink.SCOPE_LINK,
		Dst:   containerV6Addr,
		Table: unix.RT_TABLE_MAIN,
	}

	toContainerRule := netlink.NewRule()
	toContainerRule.Dst = containerAddr
	toContainerRule.Priority = networkutils.ToContainerRulePriority
	toContainerRule.Table = unix.RT_TABLE_MAIN

	toContainerV6Rule := netlink.NewRule()
	toContainerV6Rule.Dst = containerV6Addr
	toContainerV6Rule.Priority = networkutils.ToContainerRulePriority
	toContainerV6Rule.Table = unix.RT_TABLE_MAIN

	fromContainerRule := netlink.NewRule()
	fromContainerRule.Src = containerAddr
	fromContainerRule.Priority = networkutils.FromPodRulePriority
	fromContainerRule.Table = 101

	fromContainerV6Rule := netlink.NewRule()
	fromContainerV6Rule.Src = containerV6Addr
	fromContainerV6Rule.Priority = networkutils.FromPodRulePriority
	fromContainerV6Rule.Table = 101

	type routeDelCall struct {
		route *netlink.Route
		err   error
	}
	type ruleDelCall struct {
		rule *netlink.Rule
		err  error
	}
	type fields struct {
		routeDelCalls []routeDelCall
		ruleDelCalls  []ruleDelCall
	}

	type args struct {
		containerAddr *net.IPNet
		rtTable       int
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr error
	}{
		{
			name: "successfully teardown routes and rules - without dedicated route table - IPv4",
			fields: fields{
				routeDelCalls: []routeDelCall{
					{
						route: toContainerRoute,
					},
				},
				ruleDelCalls: []ruleDelCall{
					{
						rule: toContainerRule,
					},
				},
			},
			args: args{
				containerAddr: containerAddr,
				rtTable:       unix.RT_TABLE_MAIN,
			},
		},
		{
			name: "successfully teardown routes and rules - without dedicated route table - IPv6",
			fields: fields{
				routeDelCalls: []routeDelCall{
					{
						route: toContainerV6Route,
					},
				},
				ruleDelCalls: []ruleDelCall{
					{
						rule: toContainerV6Rule,
					},
				},
			},
			args: args{
				containerAddr: containerV6Addr,
				rtTable:       unix.RT_TABLE_MAIN,
			},
		},
		{
			name: "successfully teardown routes and rules - with dedicated route table - IPv4",
			fields: fields{
				routeDelCalls: []routeDelCall{
					{
						route: toContainerRoute,
					},
				},
				ruleDelCalls: []ruleDelCall{
					{
						rule: toContainerRule,
					},
					{
						rule: fromContainerRule,
					},
					{
						rule: fromContainerRule,
						err:  syscall.ENOENT,
					},
				},
			},
			args: args{
				containerAddr: containerAddr,
				rtTable:       101,
			},
		},
		{
			name: "successfully teardown routes and rules - with dedicated route table - IPv6",
			fields: fields{
				routeDelCalls: []routeDelCall{
					{
						route: toContainerV6Route,
					},
				},
				ruleDelCalls: []ruleDelCall{
					{
						rule: toContainerV6Rule,
					},
					{
						rule: fromContainerV6Rule,
					},
					{
						rule: fromContainerV6Rule,
						err:  syscall.ENOENT,
					},
				},
			},
			args: args{
				containerAddr: containerV6Addr,
				rtTable:       101,
			},
		},
		{
			name: "successfully teardown routes and rules - succeed when route already deleted",
			fields: fields{
				routeDelCalls: []routeDelCall{
					{
						route: toContainerRoute,
						err:   syscall.ESRCH,
					},
				},
				ruleDelCalls: []ruleDelCall{
					{
						rule: toContainerRule,
					},
				},
			},
			args: args{
				containerAddr: containerAddr,
				rtTable:       unix.RT_TABLE_MAIN,
			},
		},
		{
			name: "successfully teardown routes and rules - succeed even when route deletion failed",
			fields: fields{
				routeDelCalls: []routeDelCall{
					{
						route: toContainerRoute,
						err:   errors.New("some error"),
					},
				},
				ruleDelCalls: []ruleDelCall{
					{
						rule: toContainerRule,
					},
				},
			},
			args: args{
				containerAddr: containerAddr,
				rtTable:       unix.RT_TABLE_MAIN,
			},
		},
		{
			name: "successfully teardown routes and rules - toContainerRule already deleted",
			fields: fields{
				routeDelCalls: []routeDelCall{
					{
						route: toContainerRoute,
					},
				},
				ruleDelCalls: []ruleDelCall{
					{
						rule: toContainerRule,
						err:  syscall.ENOENT,
					},
					{
						rule: fromContainerRule,
					},
					{
						rule: fromContainerRule,
						err:  syscall.ENOENT,
					},
				},
			},
			args: args{
				containerAddr: containerAddr,
				rtTable:       101,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			netLink := mock_netlinkwrapper.NewMockNetLink(ctrl)
			netLink.EXPECT().NewRule().DoAndReturn(func() *netlink.Rule { return netlink.NewRule() }).AnyTimes()
			for _, call := range tt.fields.routeDelCalls {
				netLink.EXPECT().RouteDel(call.route).Return(call.err)
			}
			for _, call := range tt.fields.ruleDelCalls {
				netLink.EXPECT().RuleDel(call.rule).Return(call.err)
			}

			n := &linuxNetwork{
				netLink: netLink,
			}
			err := n.teardownIPBasedContainerRouteRules(tt.args.containerAddr, tt.args.rtTable, testLogger)
			if tt.wantErr != nil {
				assert.EqualError(t, err, tt.wantErr.Error())
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func Test_linuxNetwork_setupIIFBasedContainerRouteRules(t *testing.T) {
	hostVethAttrs := netlink.LinkAttrs{
		Name:  "eni00bcc08c834",
		Index: 7,
	}
	hostVlanAttrs := netlink.LinkAttrs{
		Name:  "vlan.eth.1",
		Index: 3,
	}
	containerAddr := &net.IPNet{
		IP:   net.ParseIP("192.168.100.42"),
		Mask: net.CIDRMask(32, 32),
	}
	containerV6Addr := &net.IPNet{
		IP:   net.ParseIP("2600::2"),
		Mask: net.CIDRMask(128, 128),
	}

	rtTable := 101
	fromHostVlanRule := netlink.NewRule()
	fromHostVlanRule.IifName = hostVlanAttrs.Name
	fromHostVlanRule.Priority = networkutils.VlanRulePriority
	fromHostVlanRule.Table = rtTable

	fromHostV6VlanRule := netlink.NewRule()
	fromHostV6VlanRule.IifName = hostVlanAttrs.Name
	fromHostV6VlanRule.Priority = networkutils.VlanRulePriority
	fromHostV6VlanRule.Table = rtTable
	fromHostV6VlanRule.Family = unix.AF_INET6

	fromHostVethRule := netlink.NewRule()
	fromHostVethRule.IifName = hostVethAttrs.Name
	fromHostVethRule.Priority = networkutils.VlanRulePriority
	fromHostVethRule.Table = rtTable

	fromHostV6VethRule := netlink.NewRule()
	fromHostV6VethRule.IifName = hostVethAttrs.Name
	fromHostV6VethRule.Priority = networkutils.VlanRulePriority
	fromHostV6VethRule.Table = rtTable
	fromHostV6VethRule.Family = unix.AF_INET6

	type routeReplaceCall struct {
		route *netlink.Route
		err   error
	}
	type ruleAddCall struct {
		rule *netlink.Rule
		err  error
	}
	type fields struct {
		routeReplaceCalls []routeReplaceCall
		ruleAddCalls      []ruleAddCall
	}
	type args struct {
		hostVethAttrs netlink.LinkAttrs
		containerAddr *net.IPNet
		hostVlanAttrs netlink.LinkAttrs
		rtTable       int
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr error
	}{
		{
			name: "successfully setup routes and rules - IPv4",
			fields: fields{
				routeReplaceCalls: []routeReplaceCall{
					{
						route: &netlink.Route{
							LinkIndex: hostVethAttrs.Index,
							Scope:     netlink.SCOPE_LINK,
							Dst:       containerAddr,
							Table:     rtTable,
						},
					},
				},
				ruleAddCalls: []ruleAddCall{
					{
						rule: fromHostVlanRule,
					},
					{
						rule: fromHostVethRule,
					},
				},
			},
			args: args{
				hostVethAttrs: hostVethAttrs,
				containerAddr: containerAddr,
				hostVlanAttrs: hostVlanAttrs,
				rtTable:       rtTable,
			},
		},
		{
			name: "successfully setup routes and rules - IPv6",
			fields: fields{
				routeReplaceCalls: []routeReplaceCall{
					{
						route: &netlink.Route{
							LinkIndex: hostVethAttrs.Index,
							Scope:     netlink.SCOPE_LINK,
							Dst:       containerV6Addr,
							Table:     rtTable,
						},
					},
				},
				ruleAddCalls: []ruleAddCall{
					{
						rule: fromHostV6VlanRule,
					},
					{
						rule: fromHostV6VethRule,
					},
				},
			},
			args: args{
				hostVethAttrs: hostVethAttrs,
				containerAddr: containerV6Addr,
				hostVlanAttrs: hostVlanAttrs,
				rtTable:       rtTable,
			},
		},
		{
			name: "successfully setup routes and rules - fromHostVlanRule already exists",
			fields: fields{
				routeReplaceCalls: []routeReplaceCall{
					{
						route: &netlink.Route{
							LinkIndex: hostVethAttrs.Index,
							Scope:     netlink.SCOPE_LINK,
							Dst:       containerAddr,
							Table:     rtTable,
						},
					},
				},
				ruleAddCalls: []ruleAddCall{
					{
						rule: fromHostVlanRule,
						err:  syscall.EEXIST,
					},
					{
						rule: fromHostVethRule,
					},
				},
			},
			args: args{
				hostVethAttrs: hostVethAttrs,
				containerAddr: containerAddr,
				hostVlanAttrs: hostVlanAttrs,
				rtTable:       rtTable,
			},
		},
		{
			name: "successfully setup routes and rules - fromHostVethRule already exists",
			fields: fields{
				routeReplaceCalls: []routeReplaceCall{
					{
						route: &netlink.Route{
							LinkIndex: hostVethAttrs.Index,
							Scope:     netlink.SCOPE_LINK,
							Dst:       containerAddr,
							Table:     rtTable,
						},
					},
				},
				ruleAddCalls: []ruleAddCall{
					{
						rule: fromHostVlanRule,
					},
					{
						rule: fromHostVethRule,
						err:  syscall.EEXIST,
					},
				},
			},
			args: args{
				hostVethAttrs: hostVethAttrs,
				containerAddr: containerAddr,
				hostVlanAttrs: hostVlanAttrs,
				rtTable:       rtTable,
			},
		},
		{
			name: "failed to setup container route",
			fields: fields{
				routeReplaceCalls: []routeReplaceCall{
					{
						route: &netlink.Route{
							LinkIndex: hostVethAttrs.Index,
							Scope:     netlink.SCOPE_LINK,
							Dst:       containerAddr,
							Table:     rtTable,
						},
						err: errors.New("some error"),
					},
				},
			},
			args: args{
				hostVethAttrs: hostVethAttrs,
				containerAddr: containerAddr,
				hostVlanAttrs: hostVlanAttrs,
				rtTable:       rtTable,
			},
			wantErr: errors.New("failed to setup container route, containerAddr=192.168.100.42/32, hostVeth=eni00bcc08c834, rtTable=101: some error"),
		},
		{
			name: "failed to setup fromHostVlan rule",
			fields: fields{
				routeReplaceCalls: []routeReplaceCall{
					{
						route: &netlink.Route{
							LinkIndex: hostVethAttrs.Index,
							Scope:     netlink.SCOPE_LINK,
							Dst:       containerAddr,
							Table:     rtTable,
						},
					},
				},
				ruleAddCalls: []ruleAddCall{
					{
						rule: fromHostVlanRule,
						err:  errors.New("some error"),
					},
				},
			},
			args: args{
				hostVethAttrs: hostVethAttrs,
				containerAddr: containerAddr,
				hostVlanAttrs: hostVlanAttrs,
				rtTable:       rtTable,
			},
			wantErr: errors.New("unable to setup fromHostVlan rule, hostVlan=vlan.eth.1, rtTable=101: some error"),
		},
		{
			name: "failed to setup fromHostVeth rule",
			fields: fields{
				routeReplaceCalls: []routeReplaceCall{
					{
						route: &netlink.Route{
							LinkIndex: hostVethAttrs.Index,
							Scope:     netlink.SCOPE_LINK,
							Dst:       containerAddr,
							Table:     rtTable,
						},
					},
				},
				ruleAddCalls: []ruleAddCall{
					{
						rule: fromHostVlanRule,
					},
					{
						rule: fromHostVethRule,
						err:  errors.New("some error"),
					},
				},
			},
			args: args{
				hostVethAttrs: hostVethAttrs,
				containerAddr: containerAddr,
				hostVlanAttrs: hostVlanAttrs,
				rtTable:       rtTable,
			},
			wantErr: errors.New("unable to setup fromHostVeth rule, hostVeth=eni00bcc08c834, rtTable=101: some error"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			hostVeth := mock_netlink.NewMockLink(ctrl)
			hostVeth.EXPECT().Attrs().Return(&tt.args.hostVethAttrs).AnyTimes()
			hostVlan := mock_netlink.NewMockLink(ctrl)
			hostVlan.EXPECT().Attrs().Return(&tt.args.hostVlanAttrs).AnyTimes()

			netLink := mock_netlinkwrapper.NewMockNetLink(ctrl)
			netLink.EXPECT().NewRule().DoAndReturn(func() *netlink.Rule { return netlink.NewRule() }).AnyTimes()
			for _, call := range tt.fields.routeReplaceCalls {
				netLink.EXPECT().RouteReplace(call.route).Return(call.err)
			}
			for _, call := range tt.fields.ruleAddCalls {
				netLink.EXPECT().RuleAdd(call.rule).Return(call.err)
			}

			n := &linuxNetwork{
				netLink: netLink,
			}
			err := n.setupIIFBasedContainerRouteRules(hostVeth, tt.args.containerAddr, hostVlan, tt.args.rtTable, testLogger)
			if tt.wantErr != nil {
				assert.EqualError(t, err, tt.wantErr.Error())
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func Test_linuxNetwork_teardownIIFBasedContainerRouteRules(t *testing.T) {
	vlanRuleForTableID101 := netlink.NewRule()
	vlanRuleForTableID101.Priority = networkutils.VlanRulePriority
	vlanRuleForTableID101.Table = 101
	vlanRuleForTableID101.Family = netlink.FAMILY_V4

	vlanIPv6RuleForTableID101 := netlink.NewRule()
	vlanIPv6RuleForTableID101.Priority = networkutils.VlanRulePriority
	vlanIPv6RuleForTableID101.Table = 101
	vlanIPv6RuleForTableID101.Family = netlink.FAMILY_V6

	type ruleDelCall struct {
		rule *netlink.Rule
		err  error
	}
	type fields struct {
		ruleDelCalls []ruleDelCall
	}

	type args struct {
		rtTable int
		family  int
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr error
	}{
		{
			name: "teardown both rules successfully - IPv4",
			fields: fields{
				ruleDelCalls: []ruleDelCall{
					{
						rule: vlanRuleForTableID101,
					},
					{
						rule: vlanRuleForTableID101,
					},
					{
						rule: vlanRuleForTableID101,
						err:  syscall.ENOENT,
					},
				},
			},
			args: args{
				rtTable: 101,
				family:  netlink.FAMILY_V4,
			},
		},
		{
			name: "teardown both rules successfully - IPv6",
			fields: fields{
				ruleDelCalls: []ruleDelCall{
					{
						rule: vlanIPv6RuleForTableID101,
					},
					{
						rule: vlanIPv6RuleForTableID101,
					},
					{
						rule: vlanIPv6RuleForTableID101,
						err:  syscall.ENOENT,
					},
				},
			},
			args: args{
				rtTable: 101,
				family:  netlink.FAMILY_V6,
			},
		},
		{
			name: "failed to delete rules",
			fields: fields{
				ruleDelCalls: []ruleDelCall{
					{
						rule: vlanRuleForTableID101,
						err:  errors.New("some error"),
					},
				},
			},
			args: args{
				rtTable: 101,
				family:  netlink.FAMILY_V4,
			},
			wantErr: errors.New("failed to delete IIF based rules, rtTable=101: some error"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			netLink := mock_netlinkwrapper.NewMockNetLink(ctrl)
			netLink.EXPECT().NewRule().DoAndReturn(func() *netlink.Rule { return netlink.NewRule() }).AnyTimes()
			for _, call := range tt.fields.ruleDelCalls {
				netLink.EXPECT().RuleDel(call.rule).Return(call.err)
			}
			n := &linuxNetwork{
				netLink: netLink,
			}
			err := n.teardownIIFBasedContainerRouteRules(tt.args.rtTable, tt.args.family, testLogger)
			if tt.wantErr != nil {
				assert.EqualError(t, err, tt.wantErr.Error())
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func Test_buildRoutesForVlan(t *testing.T) {
	v4Gateway := net.ParseIP("192.168.128.1")
	v6Gateway := net.ParseIP("fe80::beef")
	vlanTableID := 101
	vlanIndex := 7

	type args struct {
		vlanTableID int
		vlanIndex   int
		gw          net.IP
	}
	tests := []struct {
		name string
		args args
		want []netlink.Route
	}{
		{
			name: "IPv4",
			args: args{
				vlanTableID: vlanTableID,
				vlanIndex:   vlanIndex,
				gw:          v4Gateway,
			},
			want: []netlink.Route{
				{
					LinkIndex: vlanIndex,
					Dst:       &net.IPNet{IP: v4Gateway, Mask: net.CIDRMask(32, 32)},
					Scope:     netlink.SCOPE_LINK,
					Table:     vlanTableID,
				},
				{
					LinkIndex: vlanIndex,
					Dst:       &net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(0, 32)},
					Scope:     netlink.SCOPE_UNIVERSE,
					Gw:        v4Gateway,
					Table:     vlanTableID,
				},
			},
		},
		{
			name: "IPv6",
			args: args{
				vlanTableID: vlanTableID,
				vlanIndex:   vlanIndex,
				gw:          v6Gateway,
			},
			want: []netlink.Route{
				{
					LinkIndex: vlanIndex,
					Dst:       &net.IPNet{IP: v6Gateway, Mask: net.CIDRMask(128, 128)},
					Scope:     netlink.SCOPE_LINK,
					Table:     vlanTableID,
				},
				{
					LinkIndex: vlanIndex,
					Dst:       &net.IPNet{IP: net.IPv6zero, Mask: net.CIDRMask(0, 128)},
					Scope:     netlink.SCOPE_UNIVERSE,
					Gw:        v6Gateway,
					Table:     vlanTableID,
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildRoutesForVlan(tt.args.vlanTableID, tt.args.vlanIndex, tt.args.gw)
			assert.Equal(t, tt.want, got)
		})
	}
}

func Test_buildVlanLinkName(t *testing.T) {
	type args struct {
		vlanID int
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "vlanID == 1",
			args: args{
				vlanID: 1,
			},
			want: "vlan.eth.1",
		},
		{
			name: "vlanID == 2",
			args: args{
				vlanID: 2,
			},
			want: "vlan.eth.2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildVlanLinkName(tt.args.vlanID)
			assert.Equal(t, tt.want, got)
		})
	}
}

func Test_buildVlanLink(t *testing.T) {
	sampleMacAddress := "00:00:5e:00:53:af"
	sampleMac, _ := net.ParseMAC(sampleMacAddress)
	type args struct {
		vlanName      string
		vlanID        int
		parentIfIndex int
		eniMAC        string
	}
	tests := []struct {
		name                      string
		args                      args
		wantVlanLinkName          string
		wantVlanLinkID            int
		wantVlanLinkParentIfIndex int
		wantVlanLinkENIMac        net.HardwareAddr
	}{
		{
			name: "vlan.eth.1",
			args: args{
				vlanName:      "vlan.eth.1",
				vlanID:        1,
				parentIfIndex: 3,
				eniMAC:        "00:00:5e:00:53:af",
			},
			wantVlanLinkName:          "vlan.eth.1",
			wantVlanLinkID:            1,
			wantVlanLinkParentIfIndex: 3,
			wantVlanLinkENIMac:        sampleMac,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildVlanLink(tt.args.vlanName, tt.args.vlanID, tt.args.parentIfIndex, tt.args.eniMAC)
			assert.Equal(t, tt.wantVlanLinkName, got.Attrs().Name)
			assert.Equal(t, tt.wantVlanLinkID, got.VlanId)
			assert.Equal(t, tt.wantVlanLinkParentIfIndex, got.Attrs().ParentIndex)
			assert.Equal(t, tt.wantVlanLinkENIMac, got.Attrs().HardwareAddr)
		})
	}
}
