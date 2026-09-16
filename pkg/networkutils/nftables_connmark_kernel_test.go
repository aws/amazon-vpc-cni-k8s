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
	"fmt"
	"math/bits"
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
	assert.Less(t, jumpIndex, restoreSetIndex, "jump must precede restore-set")

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

func TestNftKernel_RestoreRulesReconciled(t *testing.T) {
	skipUnlessKernelTest(t)

	tests := []struct {
		name string
		rule func() *nftables.Rule
	}{
		{
			name: "unrecognized rule",
			rule: func() *nftables.Rule {
				return &nftables.Rule{Exprs: []expr.Any{&expr.Counter{}}}
			},
		},
		{
			name: "full packet-mark overwrite rule",
			rule: func() *nftables.Rule {
				return newTestFullOverwriteRestoreRule(0x80)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
				tt.rule(),
			} {
				rule.Table = table
				rule.Chain = baseChain
				conn.AddRule(rule)
			}
			require.NoError(t, conn.Flush())

			require.NoError(t, c.Setup(cidrs))
			require.NoError(t, c.Setup(cidrs))

			var fibRules, jumpRules, clearRules, setRules int
			rules := getRules(t, nftBaseChainName)
			for _, rule := range rules {
				switch {
				case isFibLocalReturnRule(rule):
					fibRules++
				case isJumpRule(rule, nftChainName, "eni"):
					jumpRules++
				default:
					bit, set, ok := classifyRestoreRule(rule, 0x80)
					require.True(t, ok, "unexpected rule remained after reconciliation")
					require.Equal(t, uint32(0x80), bit)
					if set {
						setRules++
					} else {
						clearRules++
					}
				}
			}
			assert.Equal(t, 1, fibRules)
			assert.Equal(t, 1, jumpRules)
			assert.Equal(t, 1, clearRules)
			assert.Equal(t, 1, setRules)
			assert.Len(t, rules, 4)
		})
	}
}

type nftKernelRestoreTestCase struct {
	destination string
	packetMark  uint32
	ctMark      uint32
	expected    uint32
	untracked   bool
}

func TestNftKernel_RestoreExpressionsCopyOwnedBit(t *testing.T) {
	skipUnlessKernelTest(t)

	testNftKernelRestoreExpressions(t, 0x80, []nftKernelRestoreTestCase{
		{destination: "127.0.0.2", packetMark: 0x01000000, ctMark: 0, expected: 0x01000000},
		{destination: "127.0.0.3", packetMark: 0x01000000, ctMark: 0x80, expected: 0x01000080},
		{destination: "127.0.0.4", packetMark: 0x01000080, ctMark: 0, expected: 0x01000000},
		{destination: "127.0.0.5", packetMark: 0x01000080, ctMark: 0x80, expected: 0x01000080},
		{destination: "127.0.0.6", packetMark: 0x01000080, expected: 0x01000080, untracked: true},
	})
}

func TestNftKernel_RestoreExpressionsCopyMultipleOwnedBits(t *testing.T) {
	skipUnlessKernelTest(t)

	// Clear the low owned bit, set the high owned bit, and preserve the
	// unrelated Calico bit between them.
	testNftKernelRestoreExpressions(t, 0x80000001, []nftKernelRestoreTestCase{
		{
			destination: "127.0.0.2",
			packetMark:  0x01000001,
			ctMark:      0x80000000,
			expected:    0x81000000,
		},
	})
}

func testNftKernelRestoreExpressions(t *testing.T, mark uint32, testCases []nftKernelRestoreTestCase) {
	t.Helper()

	// Install the production restore expressions in an isolated, real kernel
	// nftables table instead of modifying the host's ip aws-cni table:
	//
	//	table ip aws-cni-restore-<pid> {
	//	    chain raw-output {
	//	        type filter hook output priority raw;
	//	        ip daddr <untracked-ip> meta mark set <packet-mark> notrack
	//	    }
	//	    chain output {
	//	        type filter hook output priority filter;
	//	        ip daddr <tracked-ip> meta mark set <packet-mark> ct mark set <ct-mark>
	//	        ct mark & <bit> == 0 counter meta mark set meta mark & ~<bit>
	//	        ct mark & <bit> == <bit> counter meta mark set meta mark | <bit>
	//	        ip daddr <test-ip> meta mark == <expected-mark> counter
	//	    }
	//	}
	//
	// addRestoreRules repeats the clear/set pair for every bit in mark. The
	// seed and verification rules are repeated for each test case.
	loopback, err := netlink.LinkByName("lo")
	require.NoError(t, err)
	require.NoError(t, netlink.LinkSetUp(loopback))

	client, err := nft.New()
	require.NoError(t, err)
	table := client.AddTable(&nftables.Table{
		Family: nftables.TableFamilyIPv4,
		Name:   fmt.Sprintf("aws-cni-restore-%d", os.Getpid()),
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

	// Add the rules that establish each packet's initial state before the
	// production restore pair runs.
	for _, tc := range testCases {
		destination := net.ParseIP(tc.destination).To4()
		require.NotNil(t, destination)
		if tc.untracked {
			// At raw priority, seed the packet mark and disable conntrack. This
			// verifies that a missing ct mark prevents the later restore pair
			// from changing the packet mark.
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
		// For tracked cases, seed both the packet mark and conntrack mark. These
		// destination-specific rules precede the restore pair in the chain.
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

	// Install the exact production clear/set pair under test for the AWS-owned
	// bits in mark.
	connmark := &nftConnmark{nft: client, mark: mark}
	connmark.addRestoreRules(table, chain)

	// Append one assertion rule per destination after the restore pair. Its
	// counter increments only when the final packet mark equals tc.expected.
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
	// Commit the setup, production, and assertion rules to the kernel together.
	require.NoError(t, client.Flush())

	// Send one locally generated packet through the table for each test case.
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

	// Read the assertion counters back from the kernel. Every expected-mark
	// rule must have matched its packet exactly once.
	rules, err := client.GetRules(table, chain)
	require.NoError(t, err)
	var matchedCounters, restoreRuleCount int
	for _, rule := range rules {
		if _, _, ok := classifyRestoreRule(rule, mark); ok {
			restoreRuleCount++
		}
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
	assert.Equal(t, restoreRulesPerBit*bits.OnesCount32(mark), restoreRuleCount)
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
