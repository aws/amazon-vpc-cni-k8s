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
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/cniutils"
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

	err := Add(ipt, nodeIPv4, containerIPv4, ipv4MulticastRange, chainV4, comment, rndSNAT)
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

// TestAddV4_ChainAlreadyExists verifies that Add() succeeds when NewChain
// returns "Chain already exists". This simulates the production race condition
// where multiple concurrent egress-cni invocations all snapshot ListChains
// before any has created the chain, then race to call NewChain — the loser
// gets this error but the chain is in the desired state, so Add() must not fail.
// This was the root cause of FailedCreatePodSandBox failures when 10+ pods
// start simultaneously on the same node.
func TestAddV4_ChainAlreadyExists(t *testing.T) {
	ctrl := gomock.NewController(t)
	ipt := mock_iptables.NewMockIPTablesIface(ctrl)

	// Start with an empty chain list — neither POSTROUTING nor CNI-E4 are
	// visible from this goroutine's snapshot (the concurrent peer hasn't
	// created them yet, from our perspective).
	ipt.EXPECT().ListChains("nat").Return([]string{}, nil)
	ipt.EXPECT().HasRandomFully().Return(false).AnyTimes()

	// Both chains need to be created; the concurrent peer wins the race and
	// creates them first — our NewChain calls get "Chain already exists".
	ipt.EXPECT().NewChain("nat", gomock.Any()).Return(
		fmt.Errorf("iptables: Chain already exists."),
	).AnyTimes()

	// AppendUnique must still be called for all rules — the chains exist and
	// we should proceed normally.
	ipt.EXPECT().AppendUnique("nat", gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	err := Add(ipt, nodeIPv4, containerIPv4, ipv4MulticastRange, chainV4, comment, rndSNAT)
	assert.Nil(t, err, "Add() must succeed when NewChain returns 'Chain already exists'")
}

// TestIsChainExistErr verifies that cniutils.IsChainExistErr correctly
// identifies "Chain already exists" errors from iptables and does not match
// unrelated errors.
func TestIsChainExistErr(t *testing.T) {
	assert.True(t, cniutils.IsChainExistErr(fmt.Errorf("iptables: Chain already exists.")),
		"should match 'Chain already exists'")
	assert.True(t, cniutils.IsChainExistErr(fmt.Errorf("exit status 1: iptables: Chain already exists.")),
		"should match error containing 'Chain already exists'")
	assert.False(t, cniutils.IsChainExistErr(fmt.Errorf("some other error")),
		"should not match unrelated error")
	assert.False(t, cniutils.IsChainExistErr(fmt.Errorf("exit status 2: iptables: some other failure")),
		"should not match plain non-chain-exist error")
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

// TestDelV4_ClearChainNotExist verifies the teardown DEL+DEL race fix: when
// ClearChain returns "No chain/target/match by that name" (because a concurrent
// DEL invocation already removed the chain), Del() must succeed rather than
// propagating the error.
func TestDelV4_ClearChainNotExist(t *testing.T) {
	ctrl := gomock.NewController(t)
	ipt := mock_iptables.NewMockIPTablesIface(ctrl)

	// The POSTROUTING jump rule is already gone (concurrent DEL won the race).
	ipt.EXPECT().Delete("nat", "POSTROUTING", gomock.Any()).Return(
		fmt.Errorf("No chain/target/match by that name"),
	).AnyTimes()

	// ClearChain also finds the chain already gone.
	ipt.EXPECT().ClearChain("nat", chainV4).Return(
		fmt.Errorf("No chain/target/match by that name"),
	)

	// DeleteChain succeeds (or is also missing — tested separately).
	ipt.EXPECT().DeleteChain("nat", chainV4).Return(nil)

	err := Del(ipt, containerIPv4, chainV4, comment)
	assert.Nil(t, err, "Del() must not fail when ClearChain returns 'No chain/target/match by that name'")
}

// TestDelV4_DeleteChainNotExist verifies the teardown DEL+DEL race fix: when
// DeleteChain returns "No chain/target/match by that name" (because a concurrent
// DEL invocation already deleted the chain after we cleared it), Del() must
// succeed rather than propagating the error.
func TestDelV4_DeleteChainNotExist(t *testing.T) {
	ctrl := gomock.NewController(t)
	ipt := mock_iptables.NewMockIPTablesIface(ctrl)

	// The POSTROUTING jump rule deletion succeeds normally.
	ipt.EXPECT().Delete("nat", "POSTROUTING", gomock.Any()).Return(nil).AnyTimes()

	// ClearChain succeeds (empties the chain).
	ipt.EXPECT().ClearChain("nat", chainV4).Return(nil)

	// DeleteChain finds the chain already gone — concurrent DEL deleted it
	// between our ClearChain and DeleteChain calls.
	ipt.EXPECT().DeleteChain("nat", chainV4).Return(
		fmt.Errorf("No chain/target/match by that name"),
	)

	err := Del(ipt, containerIPv4, chainV4, comment)
	assert.Nil(t, err, "Del() must not fail when DeleteChain returns 'No chain/target/match by that name'")
}

// TestIsChainNotExistErr verifies cniutils.IsChainNotExistErr correctly
// identifies "No chain/target/match" errors and ignores unrelated errors.
func TestIsChainNotExistErr(t *testing.T) {
	assert.True(t, cniutils.IsChainNotExistErr(fmt.Errorf("No chain/target/match by that name")))
	assert.True(t, cniutils.IsChainNotExistErr(fmt.Errorf("iptables: No chain/target/match by that name.\n")))
	assert.False(t, cniutils.IsChainNotExistErr(fmt.Errorf("some other error")))
	assert.False(t, cniutils.IsChainNotExistErr(fmt.Errorf("Chain already exists")))
}
