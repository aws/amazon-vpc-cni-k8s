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
	var fibH, jumpH, restoreH uint64
	for _, r := range base {
		switch {
		case isFibLocalReturnRule(r):
			fibH = r.Handle
		case isJumpRule(r, nftChainName, "eni"):
			jumpH = r.Handle
		case isRestoreRule(r, 0x80):
			restoreH = r.Handle
		}
	}
	assert.NotZero(t, fibH, "fib rule missing")
	assert.NotZero(t, jumpH, "jump rule missing")
	assert.NotZero(t, restoreH, "restore rule missing")
	assert.Less(t, fibH, jumpH, "fib must precede jump")
	assert.Less(t, jumpH, restoreH, "jump must precede restore")

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

	assert.Len(t, getRules(t, nftBaseChainName), 3, "base chain: fib + jump + restore")
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
