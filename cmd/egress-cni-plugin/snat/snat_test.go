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

package snat

import (
	"fmt"
	"net"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/iptableswrapper"
	mock_iptables "github.com/aws/amazon-vpc-cni-k8s/pkg/iptableswrapper/mocks"
)

const (
	//ipv6MulticastRange = "ff00::/8"
	ipv4MulticastRange = "224.0.0.0/4"

	chainV4 = "CNI-E4"
	//chainV6   = "CNI-E6"
	comment = "unit-test-comment"
	rndSNAT = "hashrandom"
)

var (
	containerIpv6 = net.ParseIP("fd00::10")
	nodeIPv6      = net.ParseIP("2600::")
	containerIPv4 = net.ParseIP("169.254.172.10")
	nodeIPv4      = net.ParseIP("192.168.1.123")
)

func TestAddV4(t *testing.T) {
	ipt := mock_iptables.NewMockIPTablesIface(gomock.NewController(t))

	expectChain := []string{chainV4}
	actualChain := []string{}

	expectRule := []string{
		fmt.Sprintf("nat %s -d %s -j ACCEPT -m comment --comment %s", chainV4, ipv4MulticastRange, comment),
		fmt.Sprintf("nat %s -j SNAT --to-source %s -m comment --comment %s --random", chainV4, nodeIPv4.String(), comment),
		fmt.Sprintf("nat POSTROUTING -s %s -j %s -m comment --comment %s", containerIPv4.String(), chainV4, comment),
	}
	actualRule := []string{}

	setupAddExpect(ipt, &actualChain, &actualRule)

	err := Add(ipt, nodeIPv4, containerIPv4, ipv4MulticastRange, chainV4, comment, rndSNAT, "")
	assert.Nil(t, err)

	assert.EqualValuesf(t, expectChain, actualChain, "iptables chain is expected to be created")

	assert.EqualValuesf(t, expectRule, actualRule, "iptables rules are expected to be created")
}

func TestAddV4WithExcludedPorts(t *testing.T) {
	ipt := mock_iptables.NewMockIPTablesIface(gomock.NewController(t))

	expectChain := []string{chainV4}
	actualChain := []string{}

	excludedPorts := "1024:2048,8080:8090"

	expectRule := []string{
		fmt.Sprintf("nat %s -d %s -j ACCEPT -m comment --comment %s",
			chainV4, ipv4MulticastRange, comment),
	}

	for _, proto := range []string{"tcp", "udp", "sctp", "dccp"} {
		expectRule = append(expectRule,
			fmt.Sprintf("nat %s -p %s -m multiport --sports %s -j SNAT --to-source %s -m comment --comment %s (fixed ports %s)",
				chainV4, proto, excludedPorts, nodeIPv4.String(), comment, proto))
	}

	expectRule = append(expectRule,
		fmt.Sprintf("nat %s -j SNAT --to-source %s -m comment --comment %s --random",
			chainV4, nodeIPv4.String(), comment))

	expectRule = append(expectRule,
		fmt.Sprintf("nat POSTROUTING -s %s -j %s -m comment --comment %s",
			containerIPv4.String(), chainV4, comment))

	actualRule := []string{}

	setupAddExpect(ipt, &actualChain, &actualRule)

	err := Add(ipt, nodeIPv4, containerIPv4, ipv4MulticastRange, chainV4, comment, rndSNAT, excludedPorts)
	assert.Nil(t, err)

	assert.EqualValuesf(t, expectChain, actualChain, "iptables chain is expected to be created")
	assert.EqualValuesf(t, expectRule, actualRule, "iptables rules are expected to be created")
}

func TestDelV4(t *testing.T) {
	ipt := mock_iptables.NewMockIPTablesIface(gomock.NewController(t))

	expectClearChain := []string{chainV4}
	actualClearChain := []string{}

	expectDeleteChain := []string{chainV4}
	actualDeleteChain := []string{}

	expectRule := []string{fmt.Sprintf("nat POSTROUTING -s %s -j %s -m comment --comment %s", containerIPv4.String(), chainV4, comment)}
	actualRule := []string{}

	setupDelExpect(ipt, &actualClearChain, &actualDeleteChain, &actualRule)

	err := Del(ipt, containerIPv4, chainV4, comment)
	assert.Nil(t, err)

	assert.EqualValuesf(t, expectClearChain, actualClearChain, "iptables chain is expected to be cleared")

	assert.EqualValuesf(t, expectDeleteChain, actualDeleteChain, "iptables chain is expected to be removed")

	assert.EqualValuesf(t, expectRule, actualRule, "iptables rule is expected to be removed")
}

func setupAddExpect(ipt iptableswrapper.IPTablesIface, actualNewChain, actualNewRule *[]string) {
	ipt.(*mock_iptables.MockIPTablesIface).EXPECT().ListChains("nat").Return(
		[]string{"POSTROUTING"}, nil)

	ipt.(*mock_iptables.MockIPTablesIface).EXPECT().NewChain("nat", gomock.Any()).Do(func(_, arg1 interface{}) {
		chain := arg1.(string)
		*actualNewChain = append(*actualNewChain, chain)
	}).Return(nil)

	ipt.(*mock_iptables.MockIPTablesIface).EXPECT().AppendUnique("nat", gomock.Any(), gomock.Any()).Do(func(arg1, arg2 interface{}, arg3 ...interface{}) {
		rule := arg1.(string) + " " + arg2.(string)
		for _, arg := range arg3 {
			rule += " " + arg.(string)
		}
		*actualNewRule = append(*actualNewRule, rule)
	}).Return(nil).AnyTimes()
}

func setupDelExpect(ipt iptableswrapper.IPTablesIface, actualClearChain, actualDeleteChain, actualRule *[]string) {
	ipt.(*mock_iptables.MockIPTablesIface).EXPECT().ClearChain("nat", gomock.Any()).Do(func(_, arg2 interface{}) {
		*actualClearChain = append(*actualClearChain, arg2.(string))
	}).Return(nil)

	ipt.(*mock_iptables.MockIPTablesIface).EXPECT().DeleteChain("nat", gomock.Any()).Do(func(_, arg2 interface{}) {
		*actualDeleteChain = append(*actualDeleteChain, arg2.(string))
	}).Return(nil)

	ipt.(*mock_iptables.MockIPTablesIface).EXPECT().Delete("nat", gomock.Any(), gomock.Any()).Do(func(arg1, arg2 interface{}, arg3 ...interface{}) {
		rule := arg1.(string) + " " + arg2.(string)
		for _, arg := range arg3 {
			rule += " " + arg.(string)
		}
		*actualRule = append(*actualRule, rule)
	}).Return(nil).AnyTimes()
}
