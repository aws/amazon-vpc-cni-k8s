// Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//	http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package networkutils

import (
	"fmt"
	"math/bits"
	"net"
	"syscall"
	"testing"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/iptableswrapper"
	mock_iptables "github.com/aws/amazon-vpc-cni-k8s/pkg/iptableswrapper/mocks"
	mock_nft "github.com/aws/amazon-vpc-cni-k8s/pkg/nft/mocks"
	"github.com/coreos/go-iptables/iptables"
	"github.com/golang/mock/gomock"
	"github.com/google/nftables"
	"github.com/google/nftables/binaryutil"
	"github.com/google/nftables/expr"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNftConnmarkSetup(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockNft := mock_nft.NewMockClient(ctrl)
	mockIpt := mock_iptables.NewMockIptables()
	connmark := &nftConnmark{
		nft:        mockNft,
		vethPrefix: "eni",
		mark:       0x80,
		newIptables: func(protocol iptables.Protocol) (iptableswrapper.IPTablesIface, error) {
			return mockIpt, nil
		},
	}

	table := &nftables.Table{Family: nftables.TableFamilyIPv4, Name: nftTableName}
	priority := nftBasePriority
	policy := nftables.ChainPolicyAccept
	baseChain := &nftables.Chain{
		Name:     nftBaseChainName,
		Table:    table,
		Type:     nftables.ChainTypeNAT,
		Hooknum:  nftables.ChainHookPrerouting,
		Priority: &priority,
		Policy:   &policy,
	}
	connmarkChain := &nftables.Chain{Name: nftChainName, Table: table}

	// Setup expectations
	mockNft.EXPECT().AddTable(gomock.Any()).Return(table)
	mockNft.EXPECT().ListChain(table, nftBaseChainName).Return(baseChain, nil)
	mockNft.EXPECT().ListChain(table, nftChainName).Return(nil, syscall.ENOENT)
	mockNft.EXPECT().AddChain(gomock.Any()).Return(connmarkChain)
	mockNft.EXPECT().Flush().Return(nil).Times(2)
	mockNft.EXPECT().GetRules(table, baseChain).Return([]*nftables.Rule{}, nil)
	mockNft.EXPECT().FlushChain(baseChain)
	mockNft.EXPECT().GetRules(table, connmarkChain).Return([]*nftables.Rule{}, nil)
	mockNft.EXPECT().InsertRule(gomock.Any()).Return(&nftables.Rule{}).Times(3) // fib rule + 2 CIDRs
	mockNft.EXPECT().AddRule(gomock.Any()).Return(&nftables.Rule{}).Times(4)    // jump, 2 restore rules, set mark

	err := connmark.Setup([]string{"10.0.0.0/8", "172.16.0.0/12"})
	assert.NoError(t, err)
}

func TestNftConnmarkSetup_FlushError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockNft := mock_nft.NewMockClient(ctrl)
	connmark := &nftConnmark{
		nft:        mockNft,
		vethPrefix: "eni",
		mark:       0x80,
	}
	connmark.cleanupOnce.Store(true) // skip iptables cleanup for this test

	table := &nftables.Table{Family: nftables.TableFamilyIPv4, Name: nftTableName}
	priority := nftBasePriority
	policy := nftables.ChainPolicyAccept
	baseChain := &nftables.Chain{
		Name:     nftBaseChainName,
		Table:    table,
		Priority: &priority,
		Policy:   &policy,
		Hooknum:  nftables.ChainHookPrerouting,
		Type:     nftables.ChainTypeNAT,
	}

	mockNft.EXPECT().AddTable(gomock.Any()).Return(table)
	mockNft.EXPECT().ListChain(table, nftBaseChainName).Return(baseChain, nil)
	mockNft.EXPECT().ListChain(table, nftChainName).Return(nil, syscall.ENOENT)
	mockNft.EXPECT().AddChain(gomock.Any()).Return(&nftables.Chain{})
	mockNft.EXPECT().Flush().Return(errors.New("flush failed"))

	err := connmark.Setup([]string{"10.0.0.0/8"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "flush failed")
}

func TestNftConnmarkCleanup(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockNft := mock_nft.NewMockClient(ctrl)
	mockIpt := mock_iptables.NewMockIptables()
	connmark := &nftConnmark{
		nft:        mockNft,
		vethPrefix: "eni",
		mark:       0x80,
		newIptables: func(protocol iptables.Protocol) (iptableswrapper.IPTablesIface, error) {
			return mockIpt, nil
		},
	}

	// Seed legacy iptables rules so we can verify the cross-backend cleanup runs.
	_ = mockIpt.NewChain("nat", connmarkChainName)
	_ = mockIpt.Append("nat", "PREROUTING", "-i", "eni+", "-m", "comment", "--comment", "AWS, outbound connections", "-j", connmarkChainName)
	_ = mockIpt.Append("nat", "PREROUTING", "-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK", "--restore-mark", "--mask", fmt.Sprintf("%#x", uint32(0x80)))

	mockNft.EXPECT().DelTable(&nftables.Table{
		Family: nftables.TableFamilyIPv4,
		Name:   nftTableName,
	})
	mockNft.EXPECT().Flush().Return(nil)

	err := connmark.Cleanup()
	assert.NoError(t, err)

	exists, _ := mockIpt.Exists("nat", "PREROUTING", "-i", "eni+", "-m", "comment", "--comment", "AWS, outbound connections", "-j", connmarkChainName)
	assert.False(t, exists, "stale iptables jump rule should be cleaned up")
	exists, _ = mockIpt.ChainExists("nat", connmarkChainName)
	assert.False(t, exists, "stale iptables connmark chain should be deleted")
	assert.True(t, connmark.cleanupOnce.Load(), "cleanupOnce should be set after cross-backend cleanup")
}

func TestNftConnmarkCleanup_FlushError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockNft := mock_nft.NewMockClient(ctrl)
	connmark := &nftConnmark{
		nft:        mockNft,
		vethPrefix: "eni",
		mark:       0x80,
	}

	mockNft.EXPECT().DelTable(gomock.Any())
	mockNft.EXPECT().Flush().Return(errors.New("flush failed"))

	err := connmark.Cleanup()
	assert.Error(t, err)
}

func TestIsJumpRule(t *testing.T) {
	tests := []struct {
		name        string
		rule        *nftables.Rule
		targetChain string
		vethPrefix  string
		expected    bool
	}{
		{
			name: "valid jump rule",
			rule: &nftables.Rule{
				Exprs: []expr.Any{
					&expr.Meta{Key: expr.MetaKeyIIFNAME, Register: 1},
					&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte("eni")},
					&expr.Counter{},
					&expr.Verdict{Kind: expr.VerdictJump, Chain: "snat-mark"},
				},
			},
			targetChain: "snat-mark",
			vethPrefix:  "eni",
			expected:    true,
		},
		{
			name: "wrong target chain",
			rule: &nftables.Rule{
				Exprs: []expr.Any{
					&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte("eni")},
					&expr.Counter{},
					&expr.Verdict{Kind: expr.VerdictJump, Chain: "other-chain"},
				},
			},
			targetChain: "snat-mark",
			vethPrefix:  "eni",
			expected:    false,
		},
		{
			name: "missing counter",
			rule: &nftables.Rule{
				Exprs: []expr.Any{
					&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte("eni")},
					&expr.Verdict{Kind: expr.VerdictJump, Chain: "snat-mark"},
				},
			},
			targetChain: "snat-mark",
			vethPrefix:  "eni",
			expected:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isJumpRule(tt.rule, tt.targetChain, tt.vethPrefix)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestClassifyRestoreRule(t *testing.T) {
	mark := uint32(0x80)

	tests := []struct {
		name        string
		rule        func() *nftables.Rule
		mark        uint32
		expectedBit uint32
		expectedSet bool
		expectedOK  bool
	}{
		{
			name:        "valid clear rule",
			rule:        func() *nftables.Rule { return newTestRestoreRule(mark, false) },
			mark:        mark,
			expectedBit: mark,
			expectedOK:  true,
		},
		{
			name:        "valid set rule",
			rule:        func() *nftables.Rule { return newTestRestoreRule(mark, true) },
			mark:        mark,
			expectedBit: mark,
			expectedSet: true,
			expectedOK:  true,
		},
		{
			name:       "full packet-mark overwrite rule",
			rule:       func() *nftables.Rule { return newTestFullOverwriteRestoreRule(mark) },
			mark:       mark,
			expectedOK: false,
		},
		{
			name: "multi-bit selector",
			rule: func() *nftables.Rule {
				return newTestRestoreRule(0xc0, false)
			},
			mark: mark,
		},
		{
			name: "unowned bit",
			rule: func() *nftables.Rule {
				return newTestRestoreRule(0x40, false)
			},
			mark: mark,
		},
		{
			name: "comparison value is neither zero nor the bit",
			rule: func() *nftables.Rule {
				rule := newTestRestoreRule(mark, false)
				rule.Exprs[2].(*expr.Cmp).Data = binaryutil.NativeEndian.PutUint32(0x40)
				return rule
			},
			mark: mark,
		},
		{
			name: "set rule packet xor does not set the bit",
			rule: func() *nftables.Rule {
				rule := newTestRestoreRule(mark, true)
				rule.Exprs[5].(*expr.Bitwise).Xor = binaryutil.NativeEndian.PutUint32(0)
				return rule
			},
			mark: mark,
		},
		{
			name: "packet mark load and store directions are swapped",
			rule: func() *nftables.Rule {
				rule := newTestRestoreRule(mark, false)
				rule.Exprs[4].(*expr.Meta).SourceRegister = true
				rule.Exprs[6].(*expr.Meta).SourceRegister = false
				return rule
			},
			mark: mark,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bit, set, ok := classifyRestoreRule(tt.rule(), tt.mark)
			assert.Equal(t, tt.expectedBit, bit)
			assert.Equal(t, tt.expectedSet, set)
			assert.Equal(t, tt.expectedOK, ok)
		})
	}

	for position := range 7 {
		t.Run(fmt.Sprintf("wrong expression type at position %d", position), func(t *testing.T) {
			rule := newTestRestoreRule(mark, false)
			rule.Exprs[position] = &expr.Verdict{Kind: expr.VerdictAccept}
			_, _, ok := classifyRestoreRule(rule, mark)
			assert.False(t, ok)
		})
	}
}

func newTestRestoreRule(bit uint32, set bool) *nftables.Rule {
	bitBytes := binaryutil.NativeEndian.PutUint32(bit)
	zeroBytes := binaryutil.NativeEndian.PutUint32(0)
	compareBytes := zeroBytes
	if set {
		compareBytes = bitBytes
	}
	return &nftables.Rule{
		Exprs: []expr.Any{
			&expr.Ct{Key: expr.CtKeyMARK, Register: 1},
			&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: 4, Mask: bitBytes, Xor: zeroBytes},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: compareBytes},
			&expr.Counter{},
			&expr.Meta{Key: expr.MetaKeyMARK, Register: 1},
			&expr.Bitwise{
				SourceRegister: 1,
				DestRegister:   1,
				Len:            4,
				Mask:           binaryutil.NativeEndian.PutUint32(^bit),
				Xor:            compareBytes,
			},
			&expr.Meta{Key: expr.MetaKeyMARK, SourceRegister: true, Register: 1},
		},
	}
}

func newTestFullOverwriteRestoreRule(mark uint32) *nftables.Rule {
	return &nftables.Rule{
		Exprs: []expr.Any{
			&expr.Counter{},
			&expr.Ct{Key: expr.CtKeyMARK, Register: 1},
			&expr.Bitwise{
				SourceRegister: 1,
				DestRegister:   1,
				Len:            4,
				Mask:           binaryutil.NativeEndian.PutUint32(mark),
				Xor:            binaryutil.NativeEndian.PutUint32(0),
			},
			&expr.Meta{Key: expr.MetaKeyMARK, SourceRegister: true, Register: 1},
		},
	}
}

func newTestFibRule(handle uint64) *nftables.Rule {
	return &nftables.Rule{
		Handle: handle,
		Exprs: []expr.Any{
			&expr.Fib{Register: 1, FlagDADDR: true, ResultADDRTYPE: true},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: binaryutil.NativeEndian.PutUint32(rtnLocal)},
			&expr.Verdict{Kind: expr.VerdictReturn},
		},
	}
}

func newTestJumpRule(handle uint64) *nftables.Rule {
	return &nftables.Rule{
		Handle: handle,
		Exprs: []expr.Any{
			&expr.Meta{Key: expr.MetaKeyIIFNAME, Register: 1},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte("eni")},
			&expr.Counter{},
			&expr.Verdict{Kind: expr.VerdictJump, Chain: nftChainName},
		},
	}
}

func newTestCIDRRule(handle uint64, cidrString string) *nftables.Rule {
	_, cidr, _ := net.ParseCIDR(cidrString)
	return &nftables.Rule{
		Handle: handle,
		Exprs: []expr.Any{
			&expr.Counter{},
			&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 16, Len: 4},
			&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: 4, Mask: cidr.Mask, Xor: []byte{0, 0, 0, 0}},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: cidr.IP.To4()},
			&expr.Verdict{Kind: expr.VerdictReturn},
		},
	}
}

func newTestSetMarkRule(handle uint64, mark uint32) *nftables.Rule {
	return &nftables.Rule{
		Handle: handle,
		Exprs: []expr.Any{
			&expr.Counter{},
			&expr.Ct{Key: expr.CtKeyMARK, Register: 1},
			&expr.Bitwise{
				SourceRegister: 1, DestRegister: 1, Len: 4,
				Mask: binaryutil.NativeEndian.PutUint32(^mark),
				Xor:  binaryutil.NativeEndian.PutUint32(mark),
			},
			&expr.Ct{Key: expr.CtKeyMARK, Register: 1, SourceRegister: true},
		},
	}
}

func TestEnsureConnmarkChainRulesRepairsOnlyRequiredRules(t *testing.T) {
	tests := []struct {
		name       string
		rules      []*nftables.Rule
		desired    []string
		deleted    []uint64
		insertCIDR bool
		appendMark bool
	}{
		{
			name: "valid rules remain unchanged",
			rules: []*nftables.Rule{
				newTestCIDRRule(1, "10.0.0.0/8"), newTestSetMarkRule(2, 0x80),
			},
			desired: []string{"10.0.0.0/8"},
		},
		{
			name: "all desired and stale duplicates removed",
			rules: []*nftables.Rule{
				newTestCIDRRule(1, "10.0.0.0/8"), newTestCIDRRule(2, "10.0.0.0/8"),
				newTestCIDRRule(3, "172.16.0.0/12"), newTestCIDRRule(4, "172.16.0.0/12"),
				newTestSetMarkRule(5, 0x80), newTestSetMarkRule(6, 0x80),
				{Handle: 7, Exprs: []expr.Any{&expr.Counter{}}},
			},
			desired: []string{"10.0.0.0/8"}, deleted: []uint64{2, 3, 4, 5, 7},
		},
		{
			name: "trailing stale rules do not move valid mark",
			rules: []*nftables.Rule{
				newTestCIDRRule(1, "10.0.0.0/8"), newTestSetMarkRule(2, 0x80),
				newTestCIDRRule(3, "10.0.0.0/8"), newTestCIDRRule(4, "172.16.0.0/12"),
			},
			desired: []string{"10.0.0.0/8"}, deleted: []uint64{3, 4},
		},
		{
			name: "mark precedes CIDR",
			rules: []*nftables.Rule{
				newTestSetMarkRule(1, 0x80), newTestCIDRRule(2, "10.0.0.0/8"),
			},
			desired: []string{"10.0.0.0/8"}, deleted: []uint64{1}, appendMark: true,
		},
		{
			name: "changed CIDR set preserves final mark",
			rules: []*nftables.Rule{
				newTestCIDRRule(1, "10.0.0.0/8"), newTestCIDRRule(2, "172.16.0.0/12"), newTestSetMarkRule(3, 0x80),
			},
			desired: []string{"10.0.0.0/8", "192.168.0.0/16"}, deleted: []uint64{2}, insertCIDR: true,
		},
		{
			name: "missing mark",
			rules: []*nftables.Rule{
				newTestCIDRRule(1, "10.0.0.0/8"),
			},
			desired: []string{"10.0.0.0/8"}, appendMark: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := mock_nft.NewMockClient(gomock.NewController(t))
			c := &nftConnmark{nft: client, mark: 0x80}
			table := &nftables.Table{Family: nftables.TableFamilyIPv4, Name: nftTableName}
			snat := &nftables.Chain{Name: nftChainName, Table: table}
			client.EXPECT().GetRules(table, snat).Return(tt.rules, nil)
			var deleted []uint64
			client.EXPECT().DelRule(gomock.Any()).DoAndReturn(func(rule *nftables.Rule) error {
				deleted = append(deleted, rule.Handle)
				return nil
			}).Times(len(tt.deleted))
			if tt.insertCIDR {
				client.EXPECT().InsertRule(gomock.Any()).DoAndReturn(func(rule *nftables.Rule) *nftables.Rule {
					require.Len(t, rule.Exprs, 5)
					assert.Equal(t, []byte{192, 168, 0, 0}, rule.Exprs[3].(*expr.Cmp).Data)
					assert.Equal(t, []byte{255, 255, 0, 0}, rule.Exprs[2].(*expr.Bitwise).Mask)
					return rule
				})
			}
			if tt.appendMark {
				if tt.name == "mark precedes CIDR" {
					tt.rules[0].Exprs[0].(*expr.Counter).Packets = 17
				}
				client.EXPECT().AddRule(gomock.Any()).DoAndReturn(func(rule *nftables.Rule) *nftables.Rule {
					require.Len(t, rule.Exprs, 4)
					require.Zero(t, rule.Handle)
					require.Zero(t, rule.Position)
					assert.Equal(t, binaryutil.NativeEndian.PutUint32(0xffffff7f), rule.Exprs[2].(*expr.Bitwise).Mask)
					assert.Equal(t, binaryutil.NativeEndian.PutUint32(0x80), rule.Exprs[2].(*expr.Bitwise).Xor)
					if tt.name == "mark precedes CIDR" {
						assert.Equal(t, uint64(17), rule.Exprs[0].(*expr.Counter).Packets)
					}
					return rule
				})
			}
			desired := make(map[string]*net.IPNet)
			for _, cidrString := range tt.desired {
				_, cidr, err := net.ParseCIDR(cidrString)
				require.NoError(t, err)
				desired[cidr.String()] = cidr
			}
			require.NoError(t, c.ensureConnmarkChainRules(table, snat, desired))
			assert.ElementsMatch(t, tt.deleted, deleted)
		})
	}
}

func TestNftConnmarkSetupRejectsInvalidCIDRsBeforeChanges(t *testing.T) {
	for _, cidrs := range [][]string{
		nil, {""}, {"invalid"}, {"10.0.0.0/8", "invalid"},
		{"::/0"}, {"::ffff:10.0.0.1/120"},
	} {
		t.Run(fmt.Sprint(cidrs), func(t *testing.T) {
			client := mock_nft.NewMockClient(gomock.NewController(t))
			c := &nftConnmark{nft: client, mark: 0x80}
			require.Error(t, c.Setup(cidrs))
			require.False(t, c.cleanupOnce.Load())
		})
	}
}

func TestEnsureBaseChainRulesPreservesValidPairOrder(t *testing.T) {
	for _, tt := range []struct {
		name  string
		mark  uint32
		pairs []restoreRuleKey
	}{
		{name: "set before clear", mark: 0x80, pairs: []restoreRuleKey{{0x80, true}, {0x80, false}}},
		{name: "mixed multi-bit pairs", mark: 0x80000001, pairs: []restoreRuleKey{{1, true}, {0x80000000, false}, {1, false}, {0x80000000, true}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client := mock_nft.NewMockClient(gomock.NewController(t))
			c := &nftConnmark{nft: client, vethPrefix: "eni", mark: tt.mark}
			table := &nftables.Table{Family: nftables.TableFamilyIPv4, Name: nftTableName}
			base, snat := &nftables.Chain{Name: nftBaseChainName, Table: table}, &nftables.Chain{Name: nftChainName, Table: table}
			rules := []*nftables.Rule{newTestFibRule(400), newTestJumpRule(100)}
			for index, pair := range tt.pairs {
				rule := newTestRestoreRule(pair.bit, pair.set)
				rule.Handle = uint64(300 - index)
				rules = append(rules, rule)
			}
			client.EXPECT().GetRules(table, base).Return(rules, nil).Times(2)
			for range 2 {
				require.NoError(t, c.ensureBaseChainRules(table, base, snat))
				for _, rule := range rules {
					for _, e := range rule.Exprs {
						if counter, ok := e.(*expr.Counter); ok {
							counter.Packets += 10
							counter.Bytes += 200
						}
					}
				}
			}
		})
	}
}

func TestConnmarkMatchersRequireExactRules(t *testing.T) {
	tests := []struct {
		name    string
		rule    func() *nftables.Rule
		matches func(*nftables.Rule) bool
		changes map[string]func(*nftables.Rule)
	}{
		{
			name: "fib", rule: func() *nftables.Rule { return newTestFibRule(1) }, matches: isFibLocalReturnRule,
			changes: map[string]func(*nftables.Rule){
				"result register": func(r *nftables.Rule) { r.Exprs[0].(*expr.Fib).Register = 2 },
				"extra input":     func(r *nftables.Rule) { r.Exprs[0].(*expr.Fib).FlagMARK = true },
				"extra result":    func(r *nftables.Rule) { r.Exprs[0].(*expr.Fib).ResultOIF = true },
				"comparison":      func(r *nftables.Rule) { r.Exprs[1].(*expr.Cmp).Op = expr.CmpOpNeq },
			},
		},
		{
			name: "jump", rule: func() *nftables.Rule { return newTestJumpRule(1) },
			matches: func(r *nftables.Rule) bool { return isJumpRule(r, nftChainName, "eni") },
			changes: map[string]func(*nftables.Rule){
				"meta store":      func(r *nftables.Rule) { r.Exprs[0].(*expr.Meta).SourceRegister = true },
				"meta register":   func(r *nftables.Rule) { r.Exprs[0].(*expr.Meta).Register = 2 },
				"interface key":   func(r *nftables.Rule) { r.Exprs[0].(*expr.Meta).Key = expr.MetaKeyOIFNAME },
				"prefix wildcard": func(r *nftables.Rule) { r.Exprs[1].(*expr.Cmp).Data = []byte("eni*") },
				"prefix zero":     func(r *nftables.Rule) { r.Exprs[1].(*expr.Cmp).Data = []byte("eni\x00") },
				"target chain":    func(r *nftables.Rule) { r.Exprs[3].(*expr.Verdict).Chain = "other" },
			},
		},
		{
			name: "CIDR", rule: func() *nftables.Rule { return newTestCIDRRule(1, "10.0.0.0/8") },
			matches: func(r *nftables.Rule) bool { return extractCIDRFromRule(r) == "10.0.0.0/8" },
			changes: map[string]func(*nftables.Rule){
				"payload store":    func(r *nftables.Rule) { r.Exprs[1].(*expr.Payload).OperationType = expr.PayloadWrite },
				"payload offset":   func(r *nftables.Rule) { r.Exprs[1].(*expr.Payload).Offset = 12 },
				"payload width":    func(r *nftables.Rule) { r.Exprs[1].(*expr.Payload).Len = 16 },
				"mask register":    func(r *nftables.Rule) { r.Exprs[2].(*expr.Bitwise).SourceRegister = 2 },
				"mask width":       func(r *nftables.Rule) { r.Exprs[2].(*expr.Bitwise).Len = 16 },
				"mask XOR":         func(r *nftables.Rule) { r.Exprs[2].(*expr.Bitwise).Xor = []byte{0, 0, 0, 1} },
				"noncontiguous":    func(r *nftables.Rule) { r.Exprs[2].(*expr.Bitwise).Mask = []byte{255, 0, 255, 0} },
				"host bits":        func(r *nftables.Rule) { r.Exprs[3].(*expr.Cmp).Data = []byte{10, 0, 0, 1} },
				"compare register": func(r *nftables.Rule) { r.Exprs[3].(*expr.Cmp).Register = 2 },
			},
		},
		{
			name: "set mark", rule: func() *nftables.Rule { return newTestSetMarkRule(1, 0x80) },
			matches: func(r *nftables.Rule) bool { return isSetMarkRule(r, 0x80) },
			changes: map[string]func(*nftables.Rule){
				"CT load direction":  func(r *nftables.Rule) { r.Exprs[1].(*expr.Ct).SourceRegister = true },
				"CT store direction": func(r *nftables.Rule) { r.Exprs[3].(*expr.Ct).SourceRegister = false },
				"CT key":             func(r *nftables.Rule) { r.Exprs[1].(*expr.Ct).Key = expr.CtKeySTATUS },
				"CT register":        func(r *nftables.Rule) { r.Exprs[3].(*expr.Ct).Register = 2 },
				"bitwise register":   func(r *nftables.Rule) { r.Exprs[2].(*expr.Bitwise).DestRegister = 2 },
				"bitwise width":      func(r *nftables.Rule) { r.Exprs[2].(*expr.Bitwise).Len = 16 },
				"bitwise mask":       func(r *nftables.Rule) { r.Exprs[2].(*expr.Bitwise).Mask = binaryutil.NativeEndian.PutUint32(0x80) },
				"bitwise XOR":        func(r *nftables.Rule) { r.Exprs[2].(*expr.Bitwise).Xor = binaryutil.NativeEndian.PutUint32(0x40) },
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid := tt.rule()
			for _, e := range valid.Exprs {
				if counter, ok := e.(*expr.Counter); ok {
					counter.Packets, counter.Bytes = 10, 500
				}
			}
			require.True(t, tt.matches(valid), "counter values must not affect recognition")
			t.Run("nil rule", func(t *testing.T) { assert.False(t, tt.matches(nil)) })
			t.Run("extra guard", func(t *testing.T) {
				rule := tt.rule()
				rule.Exprs = append([]expr.Any{
					&expr.Meta{Key: expr.MetaKeyL4PROTO, Register: 1},
					&expr.Cmp{Register: 1, Op: expr.CmpOpEq, Data: []byte{6}},
				}, rule.Exprs...)
				assert.False(t, tt.matches(rule))
			})
			for position := range len(valid.Exprs) {
				t.Run(fmt.Sprintf("missing expression %d", position), func(t *testing.T) {
					rule := tt.rule()
					rule.Exprs = append(rule.Exprs[:position], rule.Exprs[position+1:]...)
					assert.False(t, tt.matches(rule))
				})
				t.Run(fmt.Sprintf("wrong expression %d", position), func(t *testing.T) {
					rule := tt.rule()
					rule.Exprs[position] = &expr.Immediate{Register: 1, Data: []byte{0, 0, 0, 0}}
					assert.False(t, tt.matches(rule))
				})
				if position+1 < len(valid.Exprs) {
					t.Run(fmt.Sprintf("reordered expression %d", position), func(t *testing.T) {
						rule := tt.rule()
						rule.Exprs[position], rule.Exprs[position+1] = rule.Exprs[position+1], rule.Exprs[position]
						assert.False(t, tt.matches(rule))
					})
				}
			}
			for name, change := range tt.changes {
				t.Run(name, func(t *testing.T) {
					rule := tt.rule()
					change(rule)
					assert.False(t, tt.matches(rule))
				})
			}
		})
	}
	for _, cidr := range []string{"0.0.0.0/0", "10.0.0.1/32"} {
		assert.Equal(t, cidr, extractCIDRFromRule(newTestCIDRRule(1, cidr)))
	}
}

func TestAddRestoreRulesCopiesOwnedBits(t *testing.T) {
	tests := []struct {
		name       string
		mark       uint32
		packetMark uint32
		ctMark     uint32
		expected   uint32
	}{
		{
			name:       "sets owned bit without clearing Calico mark",
			mark:       0x80,
			packetMark: 0x01000000,
			ctMark:     0x80,
			expected:   0x01000080,
		},
		{
			name:       "clears only owned bit",
			mark:       0x80,
			packetMark: 0x01000080,
			ctMark:     0,
			expected:   0x01000000,
		},
		{
			name:       "preserves packet mark when owned bit remains clear",
			mark:       0x80,
			packetMark: 0x01000000,
			ctMark:     0,
			expected:   0x01000000,
		},
		{
			name:       "preserves packet mark when owned bit remains set",
			mark:       0x80,
			packetMark: 0x01000080,
			ctMark:     0x80,
			expected:   0x01000080,
		},
		{
			name:       "copies multiple owned bits without clearing Calico mark",
			mark:       0x80000001,
			packetMark: 0x01000001,
			ctMark:     0x80000000,
			expected:   0x81000000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockNft := mock_nft.NewMockClient(ctrl)
			connmark := &nftConnmark{nft: mockNft, mark: tt.mark}
			var rules []*nftables.Rule
			mockNft.EXPECT().AddRule(gomock.Any()).DoAndReturn(func(rule *nftables.Rule) *nftables.Rule {
				rules = append(rules, rule)
				return rule
			}).Times(restoreRulesPerBit * bits.OnesCount32(tt.mark))

			connmark.addRestoreRules(
				&nftables.Table{Family: nftables.TableFamilyIPv4, Name: nftTableName},
				&nftables.Chain{Name: nftBaseChainName},
			)
			assert.Equal(t, tt.expected, evaluateRestoreRules(rules, tt.packetMark, tt.ctMark))
		})
	}
}

func evaluateRestoreRules(rules []*nftables.Rule, packetMark, ctMark uint32) uint32 {
	for _, rule := range rules {
		registers := make(map[uint32]uint32)
		for _, e := range rule.Exprs {
			switch e := e.(type) {
			case *expr.Ct:
				if e.SourceRegister {
					ctMark = registers[e.Register]
				} else {
					registers[e.Register] = ctMark
				}
			case *expr.Meta:
				if e.Key != expr.MetaKeyMARK {
					continue
				}
				if e.SourceRegister {
					packetMark = registers[e.Register]
				} else {
					registers[e.Register] = packetMark
				}
			case *expr.Bitwise:
				mask := binaryutil.NativeEndian.Uint32(e.Mask)
				xor := binaryutil.NativeEndian.Uint32(e.Xor)
				registers[e.DestRegister] = registers[e.SourceRegister]&mask ^ xor
			case *expr.Cmp:
				if e.Op == expr.CmpOpEq && registers[e.Register] != binaryutil.NativeEndian.Uint32(e.Data) {
					goto nextRule
				}
			}
		}
	nextRule:
	}
	return packetMark
}

func TestExtractCIDRFromRule(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")

	tests := []struct {
		name     string
		rule     *nftables.Rule
		expected string
	}{
		{
			name: "valid CIDR rule",
			rule: &nftables.Rule{
				Exprs: []expr.Any{
					&expr.Counter{},
					&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 16, Len: 4},
					&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: 4, Mask: cidr.Mask, Xor: []byte{0, 0, 0, 0}},
					&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: cidr.IP.To4()},
					&expr.Verdict{Kind: expr.VerdictReturn},
				},
			},
			expected: "10.0.0.0/8",
		},
		{
			name: "missing payload",
			rule: &nftables.Rule{
				Exprs: []expr.Any{
					&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: 4, Mask: cidr.Mask, Xor: []byte{0, 0, 0, 0}},
					&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: cidr.IP.To4()},
					&expr.Verdict{Kind: expr.VerdictReturn},
				},
			},
			expected: "",
		},
		{
			name: "missing return verdict",
			rule: &nftables.Rule{
				Exprs: []expr.Any{
					&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 16, Len: 4},
					&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: 4, Mask: cidr.Mask, Xor: []byte{0, 0, 0, 0}},
					&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: cidr.IP.To4()},
				},
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractCIDRFromRule(tt.rule)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsSetMarkRule(t *testing.T) {
	mark := uint32(0x80)
	markBytes := binaryutil.NativeEndian.PutUint32(mark)

	tests := []struct {
		name     string
		rule     *nftables.Rule
		mark     uint32
		expected bool
	}{
		{
			name: "valid set mark rule",
			rule: &nftables.Rule{
				Exprs: []expr.Any{
					&expr.Counter{},
					&expr.Ct{Key: expr.CtKeyMARK, Register: 1},
					&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: 4, Mask: binaryutil.NativeEndian.PutUint32(^mark), Xor: markBytes},
					&expr.Ct{Key: expr.CtKeyMARK, Register: 1, SourceRegister: true},
				},
			},
			mark:     mark,
			expected: true,
		},
		{
			name: "missing ct store",
			rule: &nftables.Rule{
				Exprs: []expr.Any{
					&expr.Ct{Key: expr.CtKeyMARK, Register: 1},
					&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: 4, Mask: binaryutil.NativeEndian.PutUint32(^mark), Xor: markBytes},
				},
			},
			mark:     mark,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isSetMarkRule(tt.rule, tt.mark)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsFibLocalReturnRule(t *testing.T) {
	tests := []struct {
		name     string
		rule     *nftables.Rule
		expected bool
	}{
		{
			name: "valid fib local return rule",
			rule: &nftables.Rule{
				Exprs: []expr.Any{
					&expr.Fib{Register: 1, FlagDADDR: true, ResultADDRTYPE: true},
					&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: binaryutil.NativeEndian.PutUint32(2)}, // RTN_LOCAL = 2
					&expr.Verdict{Kind: expr.VerdictReturn},
				},
			},
			expected: true,
		},
		{
			name: "missing fib",
			rule: &nftables.Rule{
				Exprs: []expr.Any{
					&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: binaryutil.NativeEndian.PutUint32(2)},
					&expr.Verdict{Kind: expr.VerdictReturn},
				},
			},
			expected: false,
		},
		{
			name: "wrong address type",
			rule: &nftables.Rule{
				Exprs: []expr.Any{
					&expr.Fib{Register: 1, FlagDADDR: true, ResultADDRTYPE: true},
					&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: binaryutil.NativeEndian.PutUint32(1)}, // not RTN_LOCAL
					&expr.Verdict{Kind: expr.VerdictReturn},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isFibLocalReturnRule(tt.rule)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsBaseChainConfigUpToDate(t *testing.T) {
	priority := nftBasePriority
	wrongPriority := nftables.ChainPriority(-100)
	policy := nftables.ChainPolicyAccept

	tests := []struct {
		name            string
		chain           *nftables.Chain
		desiredPriority nftables.ChainPriority
		expected        bool
	}{
		{
			name: "correct config",
			chain: &nftables.Chain{
				Hooknum:  nftables.ChainHookPrerouting,
				Priority: &priority,
				Policy:   &policy,
				Type:     nftables.ChainTypeNAT,
			},
			desiredPriority: -90,
			expected:        true,
		},
		{
			name: "wrong priority",
			chain: &nftables.Chain{
				Hooknum:  nftables.ChainHookPrerouting,
				Priority: &wrongPriority,
				Policy:   &policy,
			},
			desiredPriority: -90,
			expected:        false,
		},
		{
			name: "nil priority",
			chain: &nftables.Chain{
				Hooknum: nftables.ChainHookPrerouting,
				Policy:  &policy,
			},
			desiredPriority: -90,
			expected:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isBaseChainConfigUpToDate(tt.chain, tt.desiredPriority)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEnsureBaseChain_CreatesChain(t *testing.T) {
	wrongPriority := nftables.ChainPriority(-100)
	policy := nftables.ChainPolicyAccept

	tests := []struct {
		name      string
		setupMock func(mockNft *mock_nft.MockClient, table *nftables.Table, newChain *nftables.Chain)
	}{
		{
			name: "chain does not exist",
			setupMock: func(mockNft *mock_nft.MockClient, table *nftables.Table, newChain *nftables.Chain) {
				mockNft.EXPECT().ListChain(table, nftBaseChainName).Return(nil, syscall.ENOENT)
				mockNft.EXPECT().AddChain(gomock.Any()).Return(newChain)
			},
		},
		{
			name: "chain exists with incorrect config",
			setupMock: func(mockNft *mock_nft.MockClient, table *nftables.Table, newChain *nftables.Chain) {
				existingChain := &nftables.Chain{
					Name:     nftBaseChainName,
					Table:    table,
					Type:     nftables.ChainTypeNAT,
					Hooknum:  nftables.ChainHookPrerouting,
					Priority: &wrongPriority,
					Policy:   &policy,
				}
				mockNft.EXPECT().ListChain(table, nftBaseChainName).Return(existingChain, nil)
				mockNft.EXPECT().FlushChain(existingChain)
				mockNft.EXPECT().DelChain(existingChain)
				mockNft.EXPECT().AddChain(gomock.Any()).Return(newChain)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockNft := mock_nft.NewMockClient(ctrl)
			connmark := &nftConnmark{
				nft:        mockNft,
				vethPrefix: "eni",
				mark:       0x80,
			}

			table := &nftables.Table{Family: nftables.TableFamilyIPv4, Name: nftTableName}
			newChain := &nftables.Chain{Name: nftBaseChainName, Table: table}

			tt.setupMock(mockNft, table, newChain)

			chain, err := connmark.ensureBaseChain(table)
			assert.NoError(t, err)
			assert.Equal(t, newChain, chain)
		})
	}
}

func TestEnsureBaseChainRulesReconcilesInvalidRestoreRuleAndUsesReturnedOrder(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockNft := mock_nft.NewMockClient(ctrl)
	connmark := &nftConnmark{
		nft:        mockNft,
		vethPrefix: "eni",
		mark:       0x80,
	}

	table := &nftables.Table{Family: nftables.TableFamilyIPv4, Name: nftTableName}
	baseChain := &nftables.Chain{Name: nftBaseChainName, Table: table}
	targetChain := &nftables.Chain{Name: nftChainName, Table: table}
	fibRule := newTestFibRule(1)
	jumpRule := newTestJumpRule(2)
	invalidRestoreRule := newTestFullOverwriteRestoreRule(connmark.mark)
	invalidRestoreRule.Handle = 3

	mockNft.EXPECT().GetRules(table, baseChain).Return([]*nftables.Rule{
		fibRule,
		jumpRule,
		invalidRestoreRule,
	}, nil)
	mockNft.EXPECT().FlushChain(baseChain)
	var insertedRules, addedRules []*nftables.Rule
	mockNft.EXPECT().InsertRule(gomock.Any()).DoAndReturn(func(rule *nftables.Rule) *nftables.Rule {
		insertedRules = append(insertedRules, rule)
		return rule
	}).Times(1)
	mockNft.EXPECT().AddRule(gomock.Any()).DoAndReturn(func(rule *nftables.Rule) *nftables.Rule {
		addedRules = append(addedRules, rule)
		return rule
	}).Times(3)

	assert.NoError(t, connmark.ensureBaseChainRules(table, baseChain, targetChain))

	require.Len(t, insertedRules, 1)
	require.Len(t, addedRules, 3)
	assert.True(t, isFibLocalReturnRule(insertedRules[0]))
	assert.True(t, isJumpRule(addedRules[0], targetChain.Name, connmark.vethPrefix))
	bit, set, ok := classifyRestoreRule(addedRules[1], connmark.mark)
	assert.Equal(t, connmark.mark, bit)
	assert.False(t, set)
	assert.True(t, ok)
	bit, set, ok = classifyRestoreRule(addedRules[2], connmark.mark)
	assert.Equal(t, connmark.mark, bit)
	assert.True(t, set)
	assert.True(t, ok)

	installedRules := []*nftables.Rule{insertedRules[0], addedRules[0], addedRules[1], addedRules[2]}
	// Handles are deliberately out of order. Reconciliation must use the order
	// returned by GetRules, which is the rule evaluation order.
	for i, handle := range []uint64{400, 100, 300, 200} {
		installedRules[i].Handle = handle
	}
	mockNft.EXPECT().GetRules(table, baseChain).Return(installedRules, nil)

	assert.NoError(t, connmark.ensureBaseChainRules(table, baseChain, targetChain))
}

func TestEnsureBaseChainRulesReconcilesMultiBitRestoreRules(t *testing.T) {
	const mark = uint32(0x80000001)
	fibRule, jumpRule := newTestFibRule(1), newTestJumpRule(2)
	lowClear, lowSet := newTestRestoreRule(0x1, false), newTestRestoreRule(0x1, true)
	highClear, highSet := newTestRestoreRule(0x80000000, false), newTestRestoreRule(0x80000000, true)
	tests := []struct {
		name    string
		rules   []*nftables.Rule
		rebuild bool
	}{
		{
			name:  "complete pairs in either order",
			rules: []*nftables.Rule{fibRule, jumpRule, lowSet, highClear, lowClear, highSet},
		},
		{
			name:    "jump rule before the fib return",
			rules:   []*nftables.Rule{jumpRule, fibRule, lowSet, highClear, lowClear, highSet},
			rebuild: true,
		},
		{
			name:    "duplicate set rule in place of a missing clear rule",
			rules:   []*nftables.Rule{fibRule, jumpRule, lowSet, lowSet, lowClear, highSet},
			rebuild: true,
		},
		{
			name:    "restore rule before the jump",
			rules:   []*nftables.Rule{lowSet, fibRule, jumpRule, highClear, lowClear, highSet},
			rebuild: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockNft := mock_nft.NewMockClient(ctrl)
			connmark := &nftConnmark{nft: mockNft, vethPrefix: "eni", mark: mark}
			table := &nftables.Table{Family: nftables.TableFamilyIPv4, Name: nftTableName}
			baseChain := &nftables.Chain{Name: nftBaseChainName, Table: table}
			targetChain := &nftables.Chain{Name: nftChainName, Table: table}
			getRules := mockNft.EXPECT().GetRules(table, baseChain).Return(tt.rules, nil)
			if tt.rebuild {
				mockNft.EXPECT().FlushChain(baseChain)
				mockNft.EXPECT().InsertRule(gomock.Any()).Return(&nftables.Rule{})
				mockNft.EXPECT().AddRule(gomock.Any()).Return(&nftables.Rule{}).Times(5)
			} else {
				getRules.Times(2)
			}

			require.NoError(t, connmark.ensureBaseChainRules(table, baseChain, targetChain))
			if !tt.rebuild {
				require.NoError(t, connmark.ensureBaseChainRules(table, baseChain, targetChain))
			}
		})
	}
}

func TestEnsureBaseChainRulesDeletesOnlyStaleRules(t *testing.T) {
	deleteErr := errors.New("delete failed")
	tests := []struct {
		name      string
		deleteErr error
	}{
		{name: "success"},
		{name: "delete error", deleteErr: deleteErr},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			const mark = uint32(0x80)
			mockNft := mock_nft.NewMockClient(ctrl)
			connmark := &nftConnmark{
				nft:        mockNft,
				vethPrefix: "eni",
				mark:       mark,
			}

			table := &nftables.Table{Family: nftables.TableFamilyIPv4, Name: nftTableName}
			baseChain := &nftables.Chain{Name: nftBaseChainName, Table: table}
			targetChain := &nftables.Chain{Name: nftChainName, Table: table}
			staleRule := newTestFullOverwriteRestoreRule(mark)
			staleRule.Handle = 5
			duplicateSetRule := newTestRestoreRule(mark, true)
			duplicateSetRule.Handle = 6

			mockNft.EXPECT().GetRules(table, baseChain).Return([]*nftables.Rule{
				newTestFibRule(1),
				newTestJumpRule(2),
				newTestRestoreRule(mark, false),
				newTestRestoreRule(mark, true),
				staleRule,
				duplicateSetRule,
			}, nil)
			mockNft.EXPECT().DelRule(staleRule).Return(tt.deleteErr)
			mockNft.EXPECT().DelRule(duplicateSetRule).Return(nil)

			err := connmark.ensureBaseChainRules(table, baseChain, targetChain)
			if tt.deleteErr != nil {
				require.ErrorIs(t, err, tt.deleteErr)
				assert.ErrorContains(t, err, "failed to delete stale rule with handle 5")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestNftConnmarkSetup_StaleRulesRemoved(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockNft := mock_nft.NewMockClient(ctrl)
	mockIpt := mock_iptables.NewMockIptables()

	// Pre-populate legacy iptables rules
	_ = mockIpt.NewChain("nat", "AWS-CONNMARK-CHAIN-0")
	_ = mockIpt.Append("nat", "PREROUTING", "-i", "eni+", "-m", "comment", "--comment", "AWS, outbound connections", "-j", "AWS-CONNMARK-CHAIN-0")
	_ = mockIpt.Append("nat", "PREROUTING", "-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK", "--restore-mark", "--mask", fmt.Sprintf("%#x", uint32(0x80)))

	connmark := &nftConnmark{
		nft:        mockNft,
		vethPrefix: "eni",
		mark:       0x80,
		newIptables: func(protocol iptables.Protocol) (iptableswrapper.IPTablesIface, error) {
			return mockIpt, nil
		},
	}

	table := &nftables.Table{Family: nftables.TableFamilyIPv4, Name: nftTableName}
	priority := nftBasePriority
	policy := nftables.ChainPolicyAccept
	baseChain := &nftables.Chain{
		Name:     nftBaseChainName,
		Table:    table,
		Type:     nftables.ChainTypeNAT,
		Priority: &priority,
		Policy:   &policy,
		Hooknum:  nftables.ChainHookPrerouting,
	}
	connmarkChain := &nftables.Chain{Name: nftChainName, Table: table}

	_, staleCIDR, _ := net.ParseCIDR("192.168.0.0/16")
	staleRule := &nftables.Rule{
		Table: table,
		Chain: connmarkChain,
		Exprs: []expr.Any{
			&expr.Counter{},
			&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 16, Len: 4},
			&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: 4, Mask: staleCIDR.Mask, Xor: []byte{0, 0, 0, 0}},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: staleCIDR.IP.To4()},
			&expr.Verdict{Kind: expr.VerdictReturn},
		},
		Handle: 1,
	}

	mockNft.EXPECT().AddTable(gomock.Any()).Return(table)
	mockNft.EXPECT().ListChain(table, nftBaseChainName).Return(baseChain, nil)
	mockNft.EXPECT().ListChain(table, nftChainName).Return(connmarkChain, nil)
	mockNft.EXPECT().Flush().Return(nil).Times(2)
	mockNft.EXPECT().GetRules(table, baseChain).Return([]*nftables.Rule{}, nil)
	mockNft.EXPECT().FlushChain(baseChain)
	mockNft.EXPECT().GetRules(table, connmarkChain).Return([]*nftables.Rule{staleRule}, nil)
	mockNft.EXPECT().DelRule(staleRule).Return(nil) // stale rule should be deleted
	mockNft.EXPECT().InsertRule(gomock.Any()).Return(&nftables.Rule{}).Times(2)
	mockNft.EXPECT().AddRule(gomock.Any()).Return(&nftables.Rule{}).Times(4)

	err := connmark.Setup([]string{"10.0.0.0/8"})
	assert.NoError(t, err)

	// Verify legacy iptables rules were cleaned up
	exists, _ := mockIpt.Exists("nat", "PREROUTING", "-i", "eni+", "-m", "comment", "--comment", "AWS, outbound connections", "-j", "AWS-CONNMARK-CHAIN-0")
	assert.False(t, exists, "jump rule should be deleted")
	exists, _ = mockIpt.Exists("nat", "PREROUTING", "-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK", "--restore-mark", "--mask", fmt.Sprintf("%#x", uint32(0x80)))
	assert.False(t, exists, "restore rule should be deleted")
	exists, _ = mockIpt.ChainExists("nat", "AWS-CONNMARK-CHAIN-0")
	assert.False(t, exists, "chain should be deleted")
}
