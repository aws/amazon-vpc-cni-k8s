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

//go:build linux

// Kernel-level nftables tests verify rules against a real netfilter stack.
// Run: go test -c -o /tmp/nft_test ./pkg/networkutils/
//      sudo RUN_NFT_KERNEL_TESTS=1 /tmp/nft_test -test.run TestNftKernel -test.v

package networkutils

import (
	"net"
	"os"
	"testing"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/nft"
	"github.com/google/nftables"
	"github.com/google/nftables/binaryutil"
	"github.com/google/nftables/expr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
)

func skipUnlessKernelTest(t *testing.T) {
	if os.Getenv("RUN_NFT_KERNEL_TESTS") == "" {
		t.Skip("set RUN_NFT_KERNEL_TESTS=1 and run as root")
	}
}

func newTestConnmark(t *testing.T) *nftConnmark {
	client, err := nft.New()
	require.NoError(t, err)
	c := &nftConnmark{nft: client, vethPrefix: "eni", mark: 0x80}
	c.cleanupOnce.Store(true)
	c.Cleanup()
	t.Cleanup(func() { c.Cleanup() })
	return c
}

func getRules(t *testing.T, chainName string) []*nftables.Rule {
	conn, err := nftables.New()
	require.NoError(t, err)
	table := &nftables.Table{Family: nftables.TableFamilyIPv4, Name: nftTableName}
	rules, err := conn.GetRules(table, &nftables.Chain{Name: chainName, Table: table})
	require.NoError(t, err)
	return rules
}

func TestNftKernel_Setup(t *testing.T) {
	skipUnlessKernelTest(t)
	c := newTestConnmark(t)
	require.NoError(t, c.Setup([]string{"10.0.0.0/8", "172.16.0.0/12"}))

	base := getRules(t, nftBaseChainName)
	fibIndex, jumpIndex, restoreIndex := -1, -1, -1
	for ruleIndex, r := range base {
		switch {
		case isFibLocalReturnRule(r):
			fibIndex = ruleIndex
		case isJumpRule(r, nftChainName, "eni"):
			jumpIndex = ruleIndex
		default:
			bit, ok := classifyRestoreRule(r, 0x80)
			if ok && bit == 0x80 {
				restoreIndex = ruleIndex
			}
		}
	}
	assert.NotEqual(t, -1, fibIndex, "fib rule missing")
	assert.NotEqual(t, -1, jumpIndex, "jump rule missing")
	assert.NotEqual(t, -1, restoreIndex, "restore rule missing")
	assert.Less(t, fibIndex, jumpIndex, "fib must precede jump")
	assert.Less(t, jumpIndex, restoreIndex, "jump must precede restore")

	snat := getRules(t, nftChainName)
	var cidrs []string
	var hasSetMark bool
	for _, r := range snat {
		if cidr := extractCIDRFromRule(r); cidr != "" {
			cidrs = append(cidrs, cidr)
		}
		if isSetMarkRule(r, 0x80) {
			hasSetMark = true
		}
	}
	assert.ElementsMatch(t, []string{"10.0.0.0/8", "172.16.0.0/12"}, cidrs)
	assert.True(t, hasSetMark, "set-mark rule missing")
}

func TestNftKernel_Idempotent(t *testing.T) {
	skipUnlessKernelTest(t)
	c := newTestConnmark(t)
	cidrs := []string{"10.0.0.0/8", "172.16.0.0/12"}

	require.NoError(t, c.Setup(cidrs))
	require.NoError(t, c.Setup(cidrs))

	assert.Len(t, getRules(t, nftBaseChainName), 3, "base chain: fib + jump + restore rule")
	assert.Len(t, getRules(t, nftChainName), 3, "snat-mark: 2 CIDRs + set-mark")
}

func TestNftKernel_CIDRReconciliation(t *testing.T) {
	skipUnlessKernelTest(t)
	c := newTestConnmark(t)

	require.NoError(t, c.Setup([]string{"10.0.0.0/8", "172.16.0.0/12"}))
	require.NoError(t, c.Setup([]string{"10.0.0.0/8", "192.168.0.0/16"}))

	var cidrs []string
	for _, r := range getRules(t, nftChainName) {
		if cidr := extractCIDRFromRule(r); cidr != "" {
			cidrs = append(cidrs, cidr)
		}
	}
	assert.ElementsMatch(t, []string{"10.0.0.0/8", "192.168.0.0/16"}, cidrs)
}

func TestNftKernel_LegacyRestoreRuleReconciled(t *testing.T) {
	skipUnlessKernelTest(t)
	c := newTestConnmark(t)
	cidrs := []string{"10.0.0.0/8"}
	require.NoError(t, c.Setup(cidrs))

	conn, err := nftables.New()
	require.NoError(t, err)
	table := &nftables.Table{Family: nftables.TableFamilyIPv4, Name: nftTableName}
	baseChain := &nftables.Chain{Name: nftBaseChainName, Table: table}
	conn.FlushChain(baseChain)
	for _, rule := range []*nftables.Rule{
		newTestFibRule(0),
		newTestJumpRule(0),
		newTestLegacyRestoreRule(0x80),
	} {
		rule.Table = table
		rule.Chain = baseChain
		conn.AddRule(rule)
	}
	require.NoError(t, conn.Flush())

	require.NoError(t, c.Setup(cidrs))
	require.NoError(t, c.Setup(cidrs))

	var restoreRules int
	for _, rule := range getRules(t, nftBaseChainName) {
		bit, ok := classifyRestoreRule(rule, 0x80)
		if ok && bit == 0x80 {
			restoreRules++
		}
	}
	assert.Equal(t, 1, restoreRules)
	assert.Len(t, getRules(t, nftBaseChainName), 3)
}

func TestNftKernel_RestoreRuleCopiesOwnedBit(t *testing.T) {
	skipUnlessKernelTest(t)

	loopback, err := netlink.LinkByName("lo")
	require.NoError(t, err)
	require.NoError(t, netlink.LinkSetUp(loopback))

	client, err := nft.New()
	require.NoError(t, err)
	table := client.AddTable(&nftables.Table{
		Family: nftables.TableFamilyIPv4,
		Name:   "aws-cni-restore-test",
	})
	policy := nftables.ChainPolicyAccept
	rawChain := client.AddChain(&nftables.Chain{
		Name:     "raw-output",
		Table:    table,
		Type:     nftables.ChainTypeFilter,
		Hooknum:  nftables.ChainHookOutput,
		Priority: nftables.ChainPriorityRaw,
		Policy:   &policy,
	})
	chain := client.AddChain(&nftables.Chain{
		Name:     "output",
		Table:    table,
		Type:     nftables.ChainTypeFilter,
		Hooknum:  nftables.ChainHookOutput,
		Priority: nftables.ChainPriorityFilter,
		Policy:   &policy,
	})
	t.Cleanup(func() {
		client.DelTable(table)
		require.NoError(t, client.Flush())
	})

	testCases := []struct {
		destination string
		packetMark  uint32
		ctMark      uint32
		expected    uint32
		untracked   bool
	}{
		{destination: "127.0.0.2", packetMark: 0x01000000, ctMark: 0, expected: 0x01000000},
		{destination: "127.0.0.3", packetMark: 0x01000000, ctMark: 0x80, expected: 0x01000080},
		{destination: "127.0.0.4", packetMark: 0x01000080, ctMark: 0, expected: 0x01000000},
		{destination: "127.0.0.5", packetMark: 0x01000080, ctMark: 0x80, expected: 0x01000080},
		{destination: "127.0.0.6", packetMark: 0x01000080, expected: 0x01000080, untracked: true},
	}

	for _, tc := range testCases {
		destination := net.ParseIP(tc.destination).To4()
		require.NotNil(t, destination)
		if tc.untracked {
			client.AddRule(&nftables.Rule{
				Table: table,
				Chain: rawChain,
				Exprs: []expr.Any{
					&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 16, Len: 4},
					&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: destination},
					&expr.Immediate{Register: 1, Data: binaryutil.NativeEndian.PutUint32(tc.packetMark)},
					&expr.Meta{Key: expr.MetaKeyMARK, SourceRegister: true, Register: 1},
					&expr.Notrack{},
				},
			})
			continue
		}
		client.AddRule(&nftables.Rule{
			Table: table,
			Chain: chain,
			Exprs: []expr.Any{
				&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 16, Len: 4},
				&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: destination},
				&expr.Immediate{Register: 1, Data: binaryutil.NativeEndian.PutUint32(tc.packetMark)},
				&expr.Meta{Key: expr.MetaKeyMARK, SourceRegister: true, Register: 1},
				&expr.Immediate{Register: 1, Data: binaryutil.NativeEndian.PutUint32(tc.ctMark)},
				&expr.Ct{Key: expr.CtKeyMARK, SourceRegister: true, Register: 1},
			},
		})
	}

	connmark := &nftConnmark{nft: client, mark: 0x80}
	connmark.addRestoreRules(table, chain)

	for _, tc := range testCases {
		destination := net.ParseIP(tc.destination).To4()
		client.AddRule(&nftables.Rule{
			Table: table,
			Chain: chain,
			Exprs: []expr.Any{
				&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 16, Len: 4},
				&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: destination},
				&expr.Meta{Key: expr.MetaKeyMARK, Register: 1},
				&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: binaryutil.NativeEndian.PutUint32(tc.expected)},
				&expr.Counter{},
			},
		})
	}
	require.NoError(t, client.Flush())

	for _, tc := range testCases {
		connection, err := net.DialUDP("udp4", nil, &net.UDPAddr{
			IP:   net.ParseIP(tc.destination),
			Port: 9,
		})
		require.NoError(t, err)
		_, err = connection.Write([]byte{1})
		require.NoError(t, err)
		require.NoError(t, connection.Close())
	}

	rules, err := client.GetRules(table, chain)
	require.NoError(t, err)
	var matchedCounters int
	for _, rule := range rules {
		if len(rule.Exprs) != 5 {
			continue
		}
		counter, ok := rule.Exprs[4].(*expr.Counter)
		if !ok {
			continue
		}
		assert.Equal(t, uint64(1), counter.Packets)
		matchedCounters++
	}
	assert.Equal(t, len(testCases), matchedCounters)
}

func TestNftKernel_Cleanup(t *testing.T) {
	skipUnlessKernelTest(t)
	c := newTestConnmark(t)

	require.NoError(t, c.Setup([]string{"10.0.0.0/8"}))
	require.NoError(t, c.Cleanup())

	conn, err := nftables.New()
	require.NoError(t, err)
	table := &nftables.Table{Family: nftables.TableFamilyIPv4, Name: nftTableName}
	rules, err := conn.GetRules(table, &nftables.Chain{Name: nftBaseChainName, Table: table})
	assert.True(t, err != nil || len(rules) == 0, "table should not exist or have no rules after Cleanup")
}
