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
	mockNft.EXPECT().AddRule(gomock.Any()).Return(&nftables.Rule{}).Times(3)    // jump, restore rule, set mark

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
		expectedOK  bool
	}{
		{
			name:        "valid restore rule",
			rule:        func() *nftables.Rule { return newTestRestoreRule(mark) },
			mark:        mark,
			expectedBit: mark,
			expectedOK:  true,
		},
		{
			name: "v1.23 overwrite rule",
			rule: func() *nftables.Rule {
				return newTestLegacyRestoreRule(mark)
			},
			mark:       mark,
			expectedOK: false,
		},
		{
			name: "two-rule draft clear rule",
			rule: func() *nftables.Rule {
				return newTestTwoRuleDraftRestoreRule(mark, false)
			},
			mark:       mark,
			expectedOK: false,
		},
		{
			name: "two-rule draft set rule",
			rule: func() *nftables.Rule {
				return newTestTwoRuleDraftRestoreRule(mark, true)
			},
			mark:       mark,
			expectedOK: false,
		},
		{
			name: "missing counter",
			rule: func() *nftables.Rule {
				rule := newTestRestoreRule(mark)
				rule.Exprs = rule.Exprs[1:]
				return rule
			},
			mark: mark,
		},
		{
			name: "packet clear load is a store",
			rule: func() *nftables.Rule {
				rule := newTestRestoreRule(mark)
				rule.Exprs[3].(*expr.Meta).SourceRegister = true
				return rule
			},
			mark: mark,
		},
		{
			name: "wrong packet clear mask",
			rule: func() *nftables.Rule {
				rule := newTestRestoreRule(mark)
				rule.Exprs[4].(*expr.Bitwise).Mask = binaryutil.NativeEndian.PutUint32(0xffffffff)
				return rule
			},
			mark: mark,
		},
		{
			name: "wrong packet clear xor",
			rule: func() *nftables.Rule {
				rule := newTestRestoreRule(mark)
				rule.Exprs[4].(*expr.Bitwise).Xor = binaryutil.NativeEndian.PutUint32(mark)
				return rule
			},
			mark: mark,
		},
		{
			name: "packet clear store is a load",
			rule: func() *nftables.Rule {
				rule := newTestRestoreRule(mark)
				rule.Exprs[5].(*expr.Meta).SourceRegister = false
				return rule
			},
			mark: mark,
		},
		{
			name: "wrong conntrack register",
			rule: func() *nftables.Rule {
				rule := newTestRestoreRule(mark)
				rule.Exprs[1].(*expr.Ct).Register = 1
				return rule
			},
			mark: mark,
		},
		{
			name: "wrong conntrack key",
			rule: func() *nftables.Rule {
				rule := newTestRestoreRule(mark)
				rule.Exprs[1].(*expr.Ct).Key = expr.CtKeySTATE
				return rule
			},
			mark: mark,
		},
		{
			name: "conntrack load is a store",
			rule: func() *nftables.Rule {
				rule := newTestRestoreRule(mark)
				rule.Exprs[1].(*expr.Ct).SourceRegister = true
				return rule
			},
			mark: mark,
		},
		{
			name: "wrong conntrack bitwise source register",
			rule: func() *nftables.Rule {
				rule := newTestRestoreRule(mark)
				rule.Exprs[2].(*expr.Bitwise).SourceRegister = 1
				return rule
			},
			mark: mark,
		},
		{
			name: "wrong conntrack bitwise xor",
			rule: func() *nftables.Rule {
				rule := newTestRestoreRule(mark)
				rule.Exprs[2].(*expr.Bitwise).Xor = binaryutil.NativeEndian.PutUint32(mark)
				return rule
			},
			mark: mark,
		},
		{
			name: "wrong conntrack bitwise length",
			rule: func() *nftables.Rule {
				rule := newTestRestoreRule(mark)
				rule.Exprs[2].(*expr.Bitwise).Len = 2
				return rule
			},
			mark: mark,
		},
		{
			name: "multi-bit selector",
			rule: func() *nftables.Rule {
				return newTestRestoreRule(0xc0)
			},
			mark: mark,
		},
		{
			name: "unowned bit",
			rule: func() *nftables.Rule {
				return newTestRestoreRule(0x40)
			},
			mark: mark,
		},
		{
			name: "wrong comparison register",
			rule: func() *nftables.Rule {
				rule := newTestRestoreRule(mark)
				rule.Exprs[6].(*expr.Cmp).Register = 1
				return rule
			},
			mark: mark,
		},
		{
			name: "wrong comparison operation",
			rule: func() *nftables.Rule {
				rule := newTestRestoreRule(mark)
				rule.Exprs[6].(*expr.Cmp).Op = expr.CmpOpNeq
				return rule
			},
			mark: mark,
		},
		{
			name: "wrong comparison value",
			rule: func() *nftables.Rule {
				rule := newTestRestoreRule(mark)
				rule.Exprs[6].(*expr.Cmp).Data = binaryutil.NativeEndian.PutUint32(0)
				return rule
			},
			mark: mark,
		},
		{
			name: "packet set load is a store",
			rule: func() *nftables.Rule {
				rule := newTestRestoreRule(mark)
				rule.Exprs[7].(*expr.Meta).SourceRegister = true
				return rule
			},
			mark: mark,
		},
		{
			name: "wrong packet set mask",
			rule: func() *nftables.Rule {
				rule := newTestRestoreRule(mark)
				rule.Exprs[8].(*expr.Bitwise).Mask = binaryutil.NativeEndian.PutUint32(0xffffffff)
				return rule
			},
			mark: mark,
		},
		{
			name: "wrong packet set destination register",
			rule: func() *nftables.Rule {
				rule := newTestRestoreRule(mark)
				rule.Exprs[8].(*expr.Bitwise).DestRegister = 2
				return rule
			},
			mark: mark,
		},
		{
			name: "wrong packet set xor",
			rule: func() *nftables.Rule {
				rule := newTestRestoreRule(mark)
				rule.Exprs[8].(*expr.Bitwise).Xor = binaryutil.NativeEndian.PutUint32(0)
				return rule
			},
			mark: mark,
		},
		{
			name: "packet set store is a load",
			rule: func() *nftables.Rule {
				rule := newTestRestoreRule(mark)
				rule.Exprs[9].(*expr.Meta).SourceRegister = false
				return rule
			},
			mark: mark,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bit, ok := classifyRestoreRule(tt.rule(), tt.mark)
			assert.Equal(t, tt.expectedBit, bit)
			assert.Equal(t, tt.expectedOK, ok)
		})
	}
}

func newTestRestoreRule(bit uint32) *nftables.Rule {
	bitBytes := binaryutil.NativeEndian.PutUint32(bit)
	zeroBytes := binaryutil.NativeEndian.PutUint32(0)
	return &nftables.Rule{
		Exprs: []expr.Any{
			&expr.Counter{},
			&expr.Ct{Key: expr.CtKeyMARK, Register: 2},
			&expr.Bitwise{SourceRegister: 2, DestRegister: 2, Len: 4, Mask: bitBytes, Xor: zeroBytes},
			&expr.Meta{Key: expr.MetaKeyMARK, Register: 1},
			&expr.Bitwise{
				SourceRegister: 1,
				DestRegister:   1,
				Len:            4,
				Mask:           binaryutil.NativeEndian.PutUint32(^bit),
				Xor:            zeroBytes,
			},
			&expr.Meta{Key: expr.MetaKeyMARK, SourceRegister: true, Register: 1},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 2, Data: bitBytes},
			&expr.Meta{Key: expr.MetaKeyMARK, Register: 1},
			&expr.Bitwise{
				SourceRegister: 1,
				DestRegister:   1,
				Len:            4,
				Mask:           binaryutil.NativeEndian.PutUint32(^bit),
				Xor:            bitBytes,
			},
			&expr.Meta{Key: expr.MetaKeyMARK, SourceRegister: true, Register: 1},
		},
	}
}

func newTestLegacyRestoreRule(mark uint32) *nftables.Rule {
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

func newTestTwoRuleDraftRestoreRule(bit uint32, set bool) *nftables.Rule {
	bitBytes := binaryutil.NativeEndian.PutUint32(bit)
	zeroBytes := binaryutil.NativeEndian.PutUint32(0)
	compareBytes := zeroBytes
	packetXorBytes := zeroBytes
	if set {
		compareBytes = bitBytes
		packetXorBytes = bitBytes
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
				Xor:            packetXorBytes,
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

func TestAddRestoreRulePreservesUnownedPacketMark(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockNft := mock_nft.NewMockClient(ctrl)
	connmark := &nftConnmark{
		nft:  mockNft,
		mark: 0x80,
	}

	var rules []*nftables.Rule
	mockNft.EXPECT().AddRule(gomock.Any()).DoAndReturn(func(rule *nftables.Rule) *nftables.Rule {
		rules = append(rules, rule)
		return rule
	}).AnyTimes()

	connmark.addRestoreRules(
		&nftables.Table{Family: nftables.TableFamilyIPv4, Name: nftTableName},
		&nftables.Chain{Name: nftBaseChainName},
	)
	require.Len(t, rules, 1)

	tests := []struct {
		name       string
		packetMark uint32
		ctMark     uint32
		expected   uint32
	}{
		{
			name:       "sets owned bit without clearing Calico mark",
			packetMark: 0x01000000,
			ctMark:     0x80,
			expected:   0x01000080,
		},
		{
			name:       "clears only owned bit",
			packetMark: 0x01000080,
			ctMark:     0,
			expected:   0x01000000,
		},
		{
			name:       "preserves packet mark when owned bit remains clear",
			packetMark: 0x01000000,
			ctMark:     0,
			expected:   0x01000000,
		},
		{
			name:       "preserves packet mark when owned bit remains set",
			packetMark: 0x01000080,
			ctMark:     0x80,
			expected:   0x01000080,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, evaluateRestoreRules(rules, tt.packetMark, tt.ctMark))
		})
	}
}

func TestAddRestoreRulesCopiesMultipleOwnedBits(t *testing.T) {
	for _, mask := range []uint32{0xa0, 0x80000001} {
		t.Run(fmt.Sprintf("mask_%#x", mask), func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockNft := mock_nft.NewMockClient(ctrl)
			connmark := &nftConnmark{
				nft:  mockNft,
				mark: mask,
			}

			var rules []*nftables.Rule
			mockNft.EXPECT().AddRule(gomock.Any()).DoAndReturn(func(rule *nftables.Rule) *nftables.Rule {
				rules = append(rules, rule)
				return rule
			}).Times(bits.OnesCount32(mask))

			connmark.addRestoreRules(
				&nftables.Table{Family: nftables.TableFamilyIPv4, Name: nftTableName},
				&nftables.Chain{Name: nftBaseChainName},
			)

			for packetOwned := mask; ; packetOwned = (packetOwned - 1) & mask {
				packetMark := packetOwned | (0x01000000 &^ mask)
				for conntrackOwned := mask; ; conntrackOwned = (conntrackOwned - 1) & mask {
					expected := packetMark&^mask | conntrackOwned
					assert.Equal(t, expected, evaluateRestoreRules(rules, packetMark, conntrackOwned))
					if conntrackOwned == 0 {
						break
					}
				}
				if packetOwned == 0 {
					break
				}
			}
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

func TestEnsureBaseChainRulesReplacesLegacyOverwriteRestoreRule(t *testing.T) {
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
	legacyRestoreRule := newTestLegacyRestoreRule(connmark.mark)
	legacyRestoreRule.Handle = 3

	mockNft.EXPECT().GetRules(table, baseChain).Return([]*nftables.Rule{
		fibRule,
		jumpRule,
		legacyRestoreRule,
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
	}).Times(2)

	assert.NoError(t, connmark.ensureBaseChainRules(table, baseChain, targetChain))

	require.Len(t, insertedRules, 1)
	require.Len(t, addedRules, 2)
	assert.True(t, isFibLocalReturnRule(insertedRules[0]))
	assert.True(t, isJumpRule(addedRules[0], targetChain.Name, connmark.vethPrefix))
	bit, ok := classifyRestoreRule(addedRules[1], connmark.mark)
	assert.Equal(t, connmark.mark, bit)
	assert.True(t, ok)

	installedRules := []*nftables.Rule{insertedRules[0], addedRules[0], addedRules[1]}
	for i, handle := range []uint64{400, 100, 300} {
		installedRules[i].Handle = handle
	}
	mockNft.EXPECT().GetRules(table, baseChain).Return(installedRules, nil)

	assert.NoError(t, connmark.ensureBaseChainRules(table, baseChain, targetChain))
}

func TestEnsureBaseChainRulesReplacesTwoRuleDraftRestoreRules(t *testing.T) {
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
	mockNft.EXPECT().GetRules(table, baseChain).Return([]*nftables.Rule{
		newTestFibRule(1),
		newTestJumpRule(2),
		newTestTwoRuleDraftRestoreRule(connmark.mark, false),
		newTestTwoRuleDraftRestoreRule(connmark.mark, true),
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
	}).Times(2)

	require.NoError(t, connmark.ensureBaseChainRules(table, baseChain, targetChain))
	require.Len(t, insertedRules, 1)
	require.Len(t, addedRules, 2)
	assert.True(t, isFibLocalReturnRule(insertedRules[0]))
	assert.True(t, isJumpRule(addedRules[0], targetChain.Name, connmark.vethPrefix))
	bit, ok := classifyRestoreRule(addedRules[1], connmark.mark)
	assert.Equal(t, connmark.mark, bit)
	assert.True(t, ok)
}

func TestEnsureBaseChainRulesUsesReturnedRuleOrder(t *testing.T) {
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
	restoreRule := newTestRestoreRule(connmark.mark)
	restoreRule.Handle = 3
	jumpRule := newTestJumpRule(2)

	mockNft.EXPECT().GetRules(table, baseChain).Return([]*nftables.Rule{
		fibRule,
		restoreRule,
		jumpRule,
	}, nil)
	mockNft.EXPECT().FlushChain(baseChain)
	mockNft.EXPECT().InsertRule(gomock.Any()).Return(&nftables.Rule{}).Times(1)
	mockNft.EXPECT().AddRule(gomock.Any()).Return(&nftables.Rule{}).Times(2)

	assert.NoError(t, connmark.ensureBaseChainRules(table, baseChain, targetChain))
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
	mockNft.EXPECT().AddRule(gomock.Any()).Return(&nftables.Rule{}).Times(3)

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
