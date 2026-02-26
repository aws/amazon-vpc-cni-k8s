package networkutils

import (
	"fmt"
	"net"
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
	priority := nftables.ChainPriority(-90)
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
	mockNft.EXPECT().ListChain(table, nftChainName).Return(nil, errors.New("not found"))
	mockNft.EXPECT().AddChain(gomock.Any()).Return(connmarkChain)
	mockNft.EXPECT().Flush().Return(nil).Times(2)
	mockNft.EXPECT().GetRules(table, baseChain).Return([]*nftables.Rule{}, nil)
	mockNft.EXPECT().GetRules(table, connmarkChain).Return([]*nftables.Rule{}, nil)
	mockNft.EXPECT().InsertRule(gomock.Any()).Return(&nftables.Rule{}).Times(3) // fib rule + 2 CIDRs
	mockNft.EXPECT().AddRule(gomock.Any()).Return(&nftables.Rule{}).Times(3)    // jump, restore, set mark

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
	priority := nftables.ChainPriority(-90)
	policy := nftables.ChainPolicyAccept
	baseChain := &nftables.Chain{
		Name:     nftBaseChainName,
		Table:    table,
		Priority: &priority,
		Policy:   &policy,
		Hooknum:  nftables.ChainHookPrerouting,
	}

	mockNft.EXPECT().AddTable(gomock.Any()).Return(table)
	mockNft.EXPECT().ListChain(table, nftBaseChainName).Return(baseChain, nil)
	mockNft.EXPECT().ListChain(table, nftChainName).Return(nil, errors.New("not found"))
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
	connmark := &nftConnmark{
		nft:        mockNft,
		vethPrefix: "eni",
		mark:       0x80,
	}

	mockNft.EXPECT().DelTable(&nftables.Table{
		Family: nftables.TableFamilyIPv4,
		Name:   nftTableName,
	})
	mockNft.EXPECT().Flush().Return(nil)

	err := connmark.Cleanup()
	assert.NoError(t, err)
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
					&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte("eni*\x00")},
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
					&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte("eni*\x00")},
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
					&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte("eni*\x00")},
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

func TestIsRestoreRule(t *testing.T) {
	mark := uint32(0x80)
	markBytes := []byte{0x80, 0, 0, 0}

	tests := []struct {
		name     string
		rule     *nftables.Rule
		mark     uint32
		expected bool
	}{
		{
			name: "valid restore rule",
			rule: &nftables.Rule{
				Exprs: []expr.Any{
					&expr.Counter{},
					&expr.Ct{Key: expr.CtKeyMARK, Register: 1},
					&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: 4, Mask: markBytes, Xor: []byte{0, 0, 0, 0}},
					&expr.Meta{Key: expr.MetaKeyMARK, SourceRegister: true, Register: 1},
				},
			},
			mark:     mark,
			expected: true,
		},
		{
			name: "missing counter",
			rule: &nftables.Rule{
				Exprs: []expr.Any{
					&expr.Ct{Key: expr.CtKeyMARK, Register: 1},
					&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: 4, Mask: markBytes, Xor: []byte{0, 0, 0, 0}},
					&expr.Meta{Key: expr.MetaKeyMARK, SourceRegister: true, Register: 1},
				},
			},
			mark:     mark,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isRestoreRule(tt.rule, tt.mark)
			assert.Equal(t, tt.expected, result)
		})
	}
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
					&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: 4, Mask: []byte{0xff, 0xff, 0xff, 0xff}, Xor: markBytes},
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
					&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: 4, Mask: []byte{0xff, 0xff, 0xff, 0xff}, Xor: markBytes},
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

func TestIsBaseChainConfigCorrect(t *testing.T) {
	priority := nftables.ChainPriority(-90)
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
			result := isBaseChainConfigCorrect(tt.chain, tt.desiredPriority)
			assert.Equal(t, tt.expected, result)
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
	priority := nftables.ChainPriority(-90)
	policy := nftables.ChainPolicyAccept
	baseChain := &nftables.Chain{
		Name:     nftBaseChainName,
		Table:    table,
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
