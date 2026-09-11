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
	"os"
	"testing"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/nft"
	"github.com/google/nftables"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	fibIndex, jumpIndex, restoreClearIndex, restoreSetIndex := -1, -1, -1, -1
	for ruleIndex, r := range base {
		switch {
		case isFibLocalReturnRule(r):
			fibIndex = ruleIndex
		case isJumpRule(r, nftChainName, "eni"):
			jumpIndex = ruleIndex
		default:
			bit, set, ok := classifyRestoreRule(r, 0x80)
			if !ok || bit != 0x80 {
				continue
			}
			if set {
				restoreSetIndex = ruleIndex
			} else {
				restoreClearIndex = ruleIndex
			}
		}
	}
	assert.NotEqual(t, -1, fibIndex, "fib rule missing")
	assert.NotEqual(t, -1, jumpIndex, "jump rule missing")
	assert.NotEqual(t, -1, restoreClearIndex, "restore-clear rule missing")
	assert.NotEqual(t, -1, restoreSetIndex, "restore-set rule missing")
	assert.Less(t, fibIndex, jumpIndex, "fib must precede jump")
	assert.Less(t, jumpIndex, restoreClearIndex, "jump must precede restore-clear")
	assert.Less(t, restoreClearIndex, restoreSetIndex, "restore-clear must precede restore-set")

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

	assert.Len(t, getRules(t, nftBaseChainName), 4, "base chain: fib + jump + 2 restore rules")
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

	var clearRules, setRules int
	for _, rule := range getRules(t, nftBaseChainName) {
		bit, set, ok := classifyRestoreRule(rule, 0x80)
		if !ok || bit != 0x80 {
			continue
		}
		if set {
			setRules++
		} else {
			clearRules++
		}
	}
	assert.Equal(t, 1, clearRules)
	assert.Equal(t, 1, setRules)
	assert.Len(t, getRules(t, nftBaseChainName), 4)
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
