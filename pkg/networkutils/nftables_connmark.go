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

package networkutils

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"math/bits"
	"net"
	"strings"
	"sync/atomic"
	"syscall"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/iptableswrapper"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/nft"
	"github.com/aws/amazon-vpc-cni-k8s/utils/prometheusmetrics"
	"github.com/coreos/go-iptables/iptables"
	"github.com/google/nftables"
	"github.com/google/nftables/binaryutil"
	"github.com/google/nftables/expr"
)

const (
	nftTableName     = "aws-cni"
	nftBaseChainName = "nat-prerouting"
	nftChainName     = "snat-mark"
	// Each owned connmark bit needs one rule to clear the packet bit and one
	// rule to set it.
	restoreRulesPerBit = 2
	// https://github.com/torvalds/linux/blob/v7.0/include/uapi/linux/rtnetlink.h#L264
	rtnLocal = uint32(2)
	// nftBasePriority sits at dstnat (-100) + 10, so our chain runs after
	// kube-proxy's DNAT and sees the rewritten PodIP, not the ClusterIP.
	nftBasePriority nftables.ChainPriority = -90
)

// Connmark manages connection marking rules for SNAT.
// Implementations may use nftables or iptables as the backend.
//
// Both Setup and Cleanup are idempotent: they can be called repeatedly and will
// converge to the desired state without creating duplicate rules or errors.
type Connmark interface {
	// Setup configures rules to mark outbound pod traffic for SNAT, excluding exemptCIDRs.
	Setup(exemptCIDRs []string) error

	// Cleanup removes all connmark rules when external SNAT is enabled.
	Cleanup() error
}

type nftConnmark struct {
	nft         nft.Client
	vethPrefix  string
	mark        uint32
	cleanupOnce atomic.Bool
	newIptables func(iptables.Protocol) (iptableswrapper.IPTablesIface, error)
}

// restoreRuleKey identifies one rule in the clear/set restore pair for an owned bit.
type restoreRuleKey struct {
	bit uint32 // One VPC CNI-owned bit restored from the conntrack mark.
	set bool   // True if the rule sets this packet-mark bit; false if it clears it.
}

var _ Connmark = (*nftConnmark)(nil)

func NewConnmark(vethPrefix string, mark uint32) (Connmark, error) {
	mode, err := iptableswrapper.GetIptablesMode()
	if err != nil {
		return nil, err
	}
	var (
		c       Connmark
		backend string
	)
	if mode.IsNFTables() {
		c, err = newNftablesConnmark(vethPrefix, mark)
		backend = "nftables"
	} else {
		c, err = newIptablesConnmark(vethPrefix, mark)
		backend = "iptables"
	}
	if err != nil {
		return nil, err
	}
	prometheusmetrics.ConnmarkBackend.WithLabelValues(backend).Set(1)
	return c, nil
}

func newNftablesConnmark(vethPrefix string, mark uint32) (Connmark, error) {
	client, err := nft.New()
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}
	return &nftConnmark{
		nft:         client,
		vethPrefix:  vethPrefix,
		mark:        mark,
		newIptables: iptableswrapper.NewIPTables,
	}, nil
}

// Setup creates the nftables rules for SNAT connmark. With vethPrefix="eni", mark=0x80,
// and exemptCIDRs=["10.0.0.0/8", "172.16.0.0/12"], the resulting table looks like:
//
//	table ip aws-cni {
//	    chain nat-prerouting {
//	        type nat hook prerouting priority -90; policy accept;
//	        fib daddr type local return
//	        iifname "eni*" counter jump snat-mark
//	        ct mark & 0x80 == 0x0 counter meta mark set meta mark & 0xffffff7f
//	        ct mark & 0x80 == 0x80 counter meta mark set meta mark | 0x80
//	    }
//	    chain snat-mark {
//	        counter ip daddr 172.16.0.0/12 return
//	        counter ip daddr 10.0.0.0/8 return
//	        counter ct mark set ct mark | 0x80
//	    }
//	}
//
// On first successful setup, it also cleans up stale legacy iptables connmark
// rules (PREROUTING jump, restore-mark, and AWS-CONNMARK-CHAIN-0) that may
// remain from a previous iptables-based backend.
func (c *nftConnmark) Setup(exemptCIDRs []string) error {
	if len(exemptCIDRs) == 0 {
		return fmt.Errorf("exemptCIDRs cannot be empty")
	}
	desiredCIDRs := make(map[string]*net.IPNet, len(exemptCIDRs))
	for _, cidrString := range exemptCIDRs {
		_, cidr, err := net.ParseCIDR(cidrString)
		if err != nil {
			return fmt.Errorf("parse CIDR %s: %w", cidrString, err)
		}
		if cidr.IP.To4() == nil || len(cidr.Mask) != net.IPv4len {
			return fmt.Errorf("CIDR %s is not an IPv4 network", cidrString)
		}
		desiredCIDRs[cidr.String()] = cidr
	}
	// nft add table ip aws-cni
	table := c.nft.AddTable(&nftables.Table{
		Family: nftables.TableFamilyIPv4,
		Name:   nftTableName,
	})

	baseChain, err := c.ensureBaseChain(table)
	if err != nil {
		return err
	}
	connmarkChain, err := c.ensureConnmarkChain(table)
	if err != nil {
		return err
	}
	if err := c.nft.Flush(); err != nil {
		return fmt.Errorf("failed to flush nftable after base chain reconciliation: %w", err)
	}

	err = c.ensureBaseChainRules(table, baseChain, connmarkChain)
	if err != nil {
		return err
	}
	err = c.ensureConnmarkChainRules(table, connmarkChain, desiredCIDRs)
	if err != nil {
		log.Error(err.Error())
		return err
	}

	if err := c.nft.Flush(); err != nil {
		return fmt.Errorf("failed to flush nftable after chain rules reconciliation: %w", err)
	}

	// Cleanup legacy iptables rules once after successful nftables setup
	if !c.cleanupOnce.Load() {
		if err := c.cleanupIptablesConnmarkRules(); err != nil {
			return fmt.Errorf("failed to cleanup legacy iptables connmark rules: %w", err)
		}
		c.cleanupOnce.Store(true)
	}
	return nil
}

// ensureBaseChain creates or recreates the base chain: nft add chain ip aws-cni nat-prerouting '{ type nat hook prerouting priority -90; policy accept; }'
func (c *nftConnmark) ensureBaseChain(table *nftables.Table) (*nftables.Chain, error) {
	existing, err := c.nft.ListChain(table, nftBaseChainName)
	if err != nil {
		if isNFTNotExistsError(err) {
			log.Infof("chain does not exists %s: %v", nftBaseChainName, err)
			// We will recreate chain.
			existing = nil
		} else {
			return nil, err
		}
	}
	if existing != nil && !isBaseChainConfigUpToDate(existing, nftBasePriority) {
		// delete and re-add chain
		// need to flush the rules before deleting chain.
		// https://wiki.nftables.org/wiki-nftables/index.php/Configuring_chains#Deleting_chains
		c.nft.FlushChain(existing)
		c.nft.DelChain(existing)
		existing = nil
	}
	if existing == nil {
		priority := nftBasePriority
		policy := nftables.ChainPolicyAccept
		chain := c.nft.AddChain(&nftables.Chain{
			Name:     nftBaseChainName,
			Table:    table,
			Type:     nftables.ChainTypeNAT,
			Hooknum:  nftables.ChainHookPrerouting,
			Priority: &priority,
			Policy:   &policy,
		})
		return chain, nil
	}
	return existing, nil
}

func (c *nftConnmark) cleanupIptablesConnmarkRules() error {
	// Delete the PREROUTING jump rule to AWS-CONNMARK-CHAIN-0
	ipt, err := c.newIptables(iptables.ProtocolIPv4)
	if err != nil {
		return err
	}

	jumpRule := []string{
		"-i", c.vethPrefix + "+", "-m", "comment", "--comment", "AWS, outbound connections",
		"-j", connmarkChainName,
	}
	if err := ipt.Delete("nat", "PREROUTING", jumpRule...); err != nil && !isNotExistError(err) {
		return err
	}

	// Delete the PREROUTING restore-mark rule
	restoreRule := []string{
		"-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK",
		"--restore-mark", "--mask", fmt.Sprintf("%#x", c.mark),
	}
	if err := ipt.Delete("nat", "PREROUTING", restoreRule...); err != nil && !isNotExistError(err) {
		return err
	}

	// Flush and delete AWS-CONNMARK-CHAIN-0 if it exists
	if err := ipt.ClearChain("nat", connmarkChainName); err != nil && !isNotExistError(err) {
		return err
	}
	if err := ipt.DeleteChain("nat", connmarkChainName); err != nil && !isNotExistError(err) {
		return err
	}
	return nil
}

// ensureBaseChainRules ensures the nat-prerouting base chain contains the
// required rules in the correct order, adding any that are missing:
//
//  1. fib daddr type local return — short-circuits locally-destined traffic so it
//     is never marked for SNAT.
//
//  2. iifname "<vethPrefix>*" counter jump <targetChain> — matches ingress traffic
//     from pod veth interfaces and jumps to the snat-mark chain for CIDR-based
//     exemption checks and conntrack mark setting.
//
//  3. For each <bit> in <mark>, two mutually exclusive rules copy that
//     conntrack bit to the packet's firewall mark (fwmark):
//
//     ct mark & <bit> == 0     counter meta mark set meta mark & ~<bit>
//     ct mark & <bit> == <bit> counter meta mark set meta mark | <bit>
//
//     The first rule clears the packet bit when the conntrack bit is clear; the
//     second sets it when the conntrack bit is set. Unrelated bits are unchanged,
//     preserving marks owned by other networking components.
//
// This function will remove rules which are not present in above list, and maintain sequence of this rules.
func (c *nftConnmark) ensureBaseChainRules(table *nftables.Table, baseChain, targetChain *nftables.Chain) error {
	rules, err := c.nft.GetRules(table, baseChain)
	if err != nil {
		return err
	}
	fibRuleIndex, jumpRuleIndex := -1, -1
	restoreRuleIndexes := make(map[restoreRuleKey]int)
	var staleRules []*nftables.Rule

	for ruleIndex, rule := range rules {
		bit, set, isRestoreRule := classifyRestoreRule(rule, c.mark)
		restoreKey := restoreRuleKey{bit: bit, set: set}
		_, restoreExists := restoreRuleIndexes[restoreKey]

		switch {
		case isFibLocalReturnRule(rule) && fibRuleIndex == -1:
			fibRuleIndex = ruleIndex
		case isJumpRule(rule, targetChain.Name, c.vethPrefix) && jumpRuleIndex == -1:
			jumpRuleIndex = ruleIndex
		case isRestoreRule && !restoreExists:
			restoreRuleIndexes[restoreKey] = ruleIndex
		default:
			staleRules = append(staleRules, rule)
		}
	}

	desiredRestoreRuleCount := restoreRulesPerBit * bits.OnesCount32(c.mark)
	allRulesPresent := fibRuleIndex != -1 && jumpRuleIndex != -1 && len(restoreRuleIndexes) == desiredRestoreRuleCount
	ordered := allRulesPresent && fibRuleIndex < jumpRuleIndex
	for bit := uint32(1); ordered && bit != 0; bit <<= 1 {
		if c.mark&bit == 0 {
			continue
		}
		clearRuleIndex, clearExists := restoreRuleIndexes[restoreRuleKey{bit: bit, set: false}]
		setRuleIndex, setExists := restoreRuleIndexes[restoreRuleKey{bit: bit, set: true}]
		ordered = clearExists && setExists &&
			jumpRuleIndex < clearRuleIndex && jumpRuleIndex < setRuleIndex
	}

	desiredRuleCount := 2 + desiredRestoreRuleCount
	if ordered {
		if len(staleRules) == 0 {
			log.Debugf("base chain %s: all %d desired rules present in correct order; no reconciliation needed", baseChain.Name, desiredRuleCount)
		} else {
			log.Infof("base chain %s: %d desired rules present and ordered, but %d stale rule(s) found; deleting stale rules", baseChain.Name, desiredRuleCount, len(staleRules))
		}
		var errs []error
		for _, r := range staleRules {
			if err := c.nft.DelRule(r); err != nil {
				errs = append(errs, fmt.Errorf("failed to delete stale rule with handle %d: %w", r.Handle, err))
			}
		}
		return errors.Join(errs...)
	}

	log.Infof("base chain %s: rules missing or out of order (fib=%v jump=%v restore=%d/%d ordered=%v stale=%d); flushing and reinstalling all %d rules",
		baseChain.Name, fibRuleIndex != -1, jumpRuleIndex != -1, len(restoreRuleIndexes), desiredRestoreRuleCount, ordered, len(staleRules), desiredRuleCount)
	c.nft.FlushChain(baseChain)
	c.addFibLocalReturnRule(table, baseChain)
	c.addJumpRule(table, baseChain, targetChain)
	c.addRestoreRules(table, baseChain)
	return nil
}

// isFibLocalReturnRule checks for: fib daddr type local return
func isFibLocalReturnRule(rule *nftables.Rule) bool {
	if rule == nil || len(rule.Exprs) != 3 {
		return false
	}
	fib, ok := rule.Exprs[0].(*expr.Fib)
	if !ok || fib == nil || *fib != (expr.Fib{Register: 1, FlagDADDR: true, ResultADDRTYPE: true}) {
		return false
	}
	cmp, ok := rule.Exprs[1].(*expr.Cmp)
	if !ok || cmp == nil || cmp.Op != expr.CmpOpEq || cmp.Register != 1 ||
		!bytes.Equal(cmp.Data, binaryutil.NativeEndian.PutUint32(rtnLocal)) {
		return false
	}
	v, ok := rule.Exprs[2].(*expr.Verdict)
	return ok && v != nil && v.Kind == expr.VerdictReturn
}

// addFibLocalReturnRule inserts: nft insert rule ip aws-cni nat-prerouting fib daddr type local return
func (c *nftConnmark) addFibLocalReturnRule(table *nftables.Table, chain *nftables.Chain) {
	c.nft.InsertRule(&nftables.Rule{
		Table: table,
		Chain: chain,
		Exprs: []expr.Any{
			&expr.Fib{
				Register: 1,
				// Use destination address
				FlagDADDR: true,
				// Store the address type (RTN_LOCAL, RTN_UNICAST, etc.)
				ResultADDRTYPE: true,
			},
			&expr.Cmp{
				Op:       expr.CmpOpEq,
				Register: 1,
				Data:     binaryutil.NativeEndian.PutUint32(rtnLocal),
			},
			&expr.Verdict{Kind: expr.VerdictReturn},
		},
	})
}

// ensureConnmarkChain creates the regular (non-base) chain: nft add chain ip aws-cni snat-mark
// Skips SNAT marking for traffic destined to the node's own IPs.
// Compares fib lookup result against RTN_LOCAL (2) from the kernel's rtm_type enum:
// https://github.com/torvalds/linux/blob/v7.0/include/uapi/linux/rtnetlink.h#L264
func (c *nftConnmark) ensureConnmarkChain(table *nftables.Table) (*nftables.Chain, error) {
	existing, err := c.getConnmarkChain(table)
	if err != nil {
		if isNFTNotExistsError(err) {
			log.Infof("chain does not exists %s: %v", nftChainName, err)
			// We will recreate chain.
			existing = nil
		} else {
			return nil, err
		}
	}
	if existing != nil {
		return existing, nil
	}

	// Add chain is idempotent
	return c.nft.AddChain(&nftables.Chain{
		Name:  nftChainName,
		Table: table,
	}), nil
}

// ensureConnmarkChainRules modifies desiredCIDRs. Callers must pass a fresh map.
func (c *nftConnmark) ensureConnmarkChainRules(table *nftables.Table, chain *nftables.Chain, desiredCIDRs map[string]*net.IPNet) error {
	rules, err := c.nft.GetRules(table, chain)
	if err != nil {
		return err
	}

	var staleRules []*nftables.Rule
	setMarkIndex, lastCIDRIndex := -1, -1
	for index, rule := range rules {
		if cidr := extractCIDRFromRule(rule); desiredCIDRs[cidr] != nil {
			delete(desiredCIDRs, cidr)
			lastCIDRIndex = index
		} else if isSetMarkRule(rule, c.mark) {
			if setMarkIndex != -1 {
				staleRules = append(staleRules, rules[setMarkIndex])
			}
			setMarkIndex = index
		} else {
			staleRules = append(staleRules, rule)
		}
	}

	if setMarkIndex != -1 && setMarkIndex < lastCIDRIndex {
		staleRules = append(staleRules, rules[setMarkIndex])
	}

	for _, rule := range staleRules {
		if err := c.nft.DelRule(rule); err != nil {
			return fmt.Errorf("delete stale rule (handle %d): %w", rule.Handle, err)
		}
	}

	// Insert missing CIDRs before the mark rule.
	for cidr := range maps.Values(desiredCIDRs) {
		c.insertCIDRReturnRule(table, chain, cidr)
	}

	if setMarkIndex == -1 {
		c.addSetMarkRule(table, chain)
	} else if setMarkIndex < lastCIDRIndex {
		moved := *rules[setMarkIndex]
		moved.Handle, moved.ID, moved.Position, moved.PositionID = 0, 0, 0, 0
		c.nft.AddRule(&moved) // Preserve its counter when moving it to the end.
	}
	return nil
}

// Cleanup removes the entire nftables table: nft delete table ip aws-cni
func (c *nftConnmark) Cleanup() error {
	c.nft.DelTable(&nftables.Table{
		Family: nftables.TableFamilyIPv4,
		Name:   nftTableName,
	})
	if err := c.nft.Flush(); err != nil && !isNFTNotExistsError(err) {
		return err
	}
	if !c.cleanupOnce.Load() {
		if err := c.cleanupIptablesConnmarkRules(); err != nil {
			return fmt.Errorf("failed to cleanup legacy iptables connmark rules: %w", err)
		}
		c.cleanupOnce.Store(true)
	}
	return nil
}

func isBaseChainConfigUpToDate(chain *nftables.Chain, desiredPriority nftables.ChainPriority) bool {
	return chain.Hooknum != nil && *chain.Hooknum == *nftables.ChainHookPrerouting &&
		chain.Priority != nil && *chain.Priority == desiredPriority &&
		chain.Policy != nil && *chain.Policy == nftables.ChainPolicyAccept &&
		chain.Type == nftables.ChainTypeNAT
}

func (c *nftConnmark) getConnmarkChain(table *nftables.Table) (*nftables.Chain, error) {
	chain, err := c.nft.ListChain(table, nftChainName)
	if err != nil {
		return nil, err
	}
	return chain, nil
}

// addJumpRule adds: nft add rule ip aws-cni nat-prerouting iifname "eni*" counter jump snat-mark
//
// Data is the bare prefix ("eni"), not "eni*": kernel memcmp's only
// len(NFTA_CMP_DATA) bytes against the iifname register, so 3 bytes of "eni"
// matches "eniXXXX". See nft_cmp_eval `memcmp(..., priv->len)`:
// https://github.com/torvalds/linux/blob/v6.6/net/netfilter/nft_cmp.c#L33
func (c *nftConnmark) addJumpRule(table *nftables.Table, baseChain, targetChain *nftables.Chain) {
	c.nft.AddRule(&nftables.Rule{
		Table: table,
		Chain: baseChain,
		Exprs: []expr.Any{
			&expr.Meta{Key: expr.MetaKeyIIFNAME, Register: 1},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte(c.vethPrefix)},
			&expr.Counter{},
			&expr.Verdict{
				Kind:  expr.VerdictJump,
				Chain: targetChain.Name,
			},
		},
	})
}

// addRestoreRules copies each bit owned by the CNI from the conntrack mark to
// the packet mark. Two mutually exclusive rules per bit support kernels whose
// nftables bitwise expressions combine a register only with constants.
// Both rules read conntrack state before writing the packet mark, so untracked
// packets remain unchanged.
func (c *nftConnmark) addRestoreRules(table *nftables.Table, chain *nftables.Chain) {
	for bit := uint32(1); bit != 0; bit <<= 1 {
		if c.mark&bit == 0 {
			continue
		}
		c.addRestoreRule(table, chain, bit, false)
		c.addRestoreRule(table, chain, bit, true)
	}
}

func (c *nftConnmark) addRestoreRule(table *nftables.Table, chain *nftables.Chain, bit uint32, set bool) {
	// nftables expressions represent uint32 values as native-endian bytes.
	bitBytes := binaryutil.NativeEndian.PutUint32(bit)
	zeroBytes := binaryutil.NativeEndian.PutUint32(0)

	// The clear rule matches when the conntrack bit is zero and clears that
	// bit from the packet mark. The set rule matches when the conntrack bit is
	// set and sets that bit in the packet mark.
	compareBytes := zeroBytes
	if set {
		compareBytes = bitBytes
	}

	c.nft.AddRule(&nftables.Rule{
		Table: table,
		Chain: chain,
		Exprs: []expr.Any{
			// Load the connection mark into register 1.
			&expr.Ct{Key: expr.CtKeyMARK, Register: 1},

			// Isolate the selected connection-mark bit:
			// register 1 = (connection mark & bit) ^ 0.
			&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: 4, Mask: bitBytes, Xor: zeroBytes},

			// The clear rule continues only when the isolated bit is zero; the
			// set rule continues only when it equals bit.
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: compareBytes},

			// Count packets that matched this clear or set branch.
			&expr.Counter{},

			// Replace register 1 with the packet's current firewall mark.
			&expr.Meta{Key: expr.MetaKeyMARK, Register: 1},

			// Clear rule: (packet mark & ~bit) ^ 0 clears the selected bit.
			// Set rule: (packet mark & ~bit) ^ bit sets the selected bit.
			// Both operations preserve every unrelated packet-mark bit.
			&expr.Bitwise{
				SourceRegister: 1,
				DestRegister:   1,
				Len:            4,
				Mask:           binaryutil.NativeEndian.PutUint32(^bit),
				Xor:            compareBytes,
			},

			// Store register 1 back into the packet firewall mark.
			&expr.Meta{Key: expr.MetaKeyMARK, SourceRegister: true, Register: 1},
		},
	})
}

func isJumpRule(rule *nftables.Rule, targetChain, vethPrefix string) bool {
	if rule == nil || len(rule.Exprs) != 4 {
		return false
	}
	meta, ok := rule.Exprs[0].(*expr.Meta)
	if !ok || meta == nil || meta.Key != expr.MetaKeyIIFNAME || meta.SourceRegister || meta.Register != 1 {
		return false
	}
	cmp, ok := rule.Exprs[1].(*expr.Cmp)
	if !ok || cmp == nil || cmp.Op != expr.CmpOpEq || cmp.Register != 1 ||
		!bytes.Equal(cmp.Data, []byte(vethPrefix)) {
		return false
	}
	if counter, ok := rule.Exprs[2].(*expr.Counter); !ok || counter == nil {
		return false
	}
	v, ok := rule.Exprs[3].(*expr.Verdict)
	return ok && v != nil && v.Kind == expr.VerdictJump && v.Chain == targetChain
}

// classifyRestoreRule recognises one of the two rules that restores a single
// owned conntrack-mark bit into the packet mark:
//
//	ct mark & <bit> == <0|bit> counter meta mark set meta mark <&|> <mask>
func classifyRestoreRule(rule *nftables.Rule, mark uint32) (uint32, bool, bool) {
	if len(rule.Exprs) != 7 {
		return 0, false, false
	}

	ctLoad, ok := rule.Exprs[0].(*expr.Ct)
	if !ok || ctLoad.Key != expr.CtKeyMARK || ctLoad.SourceRegister || ctLoad.Register != 1 {
		return 0, false, false
	}
	ctBitwise, ok := rule.Exprs[1].(*expr.Bitwise)
	if !ok ||
		ctBitwise.SourceRegister != 1 || ctBitwise.DestRegister != 1 || ctBitwise.Len != 4 ||
		len(ctBitwise.Mask) != 4 || !bytes.Equal(ctBitwise.Xor, []byte{0, 0, 0, 0}) {
		return 0, false, false
	}
	bit := binaryutil.NativeEndian.Uint32(ctBitwise.Mask)
	if bit == 0 || bit&(bit-1) != 0 || mark&bit == 0 {
		return 0, false, false
	}
	cmp, ok := rule.Exprs[2].(*expr.Cmp)
	if !ok || cmp.Op != expr.CmpOpEq || cmp.Register != 1 || len(cmp.Data) != 4 {
		return 0, false, false
	}
	compareValue := binaryutil.NativeEndian.Uint32(cmp.Data)
	if compareValue != 0 && compareValue != bit {
		return 0, false, false
	}
	if _, ok := rule.Exprs[3].(*expr.Counter); !ok {
		return 0, false, false
	}
	metaLoad, ok := rule.Exprs[4].(*expr.Meta)
	if !ok || metaLoad.Key != expr.MetaKeyMARK || metaLoad.SourceRegister || metaLoad.Register != 1 {
		return 0, false, false
	}
	packetBitwise, ok := rule.Exprs[5].(*expr.Bitwise)
	if !ok ||
		packetBitwise.SourceRegister != 1 || packetBitwise.DestRegister != 1 || packetBitwise.Len != 4 ||
		!bytes.Equal(packetBitwise.Mask, binaryutil.NativeEndian.PutUint32(^bit)) ||
		!bytes.Equal(packetBitwise.Xor, cmp.Data) {
		return 0, false, false
	}
	metaStore, ok := rule.Exprs[6].(*expr.Meta)
	if !ok || metaStore.Key != expr.MetaKeyMARK || !metaStore.SourceRegister || metaStore.Register != 1 {
		return 0, false, false
	}
	return bit, compareValue == bit, true
}

// extractCIDRFromRule recovers the daddr CIDR from a rule shaped like:
//
//	counter ip daddr <cidr> return
func extractCIDRFromRule(rule *nftables.Rule) string {
	if rule == nil || len(rule.Exprs) != 5 {
		return ""
	}
	if counter, ok := rule.Exprs[0].(*expr.Counter); !ok || counter == nil {
		return ""
	}
	payload, ok := rule.Exprs[1].(*expr.Payload)
	if !ok || payload == nil || payload.OperationType != expr.PayloadLoad ||
		payload.Base != expr.PayloadBaseNetworkHeader || payload.Offset != 16 || payload.Len != 4 ||
		payload.DestRegister != 1 {
		return ""
	}
	bw, ok := rule.Exprs[2].(*expr.Bitwise)
	if !ok || bw == nil || bw.SourceRegister != 1 || bw.DestRegister != 1 || bw.Len != 4 ||
		len(bw.Mask) != 4 || !bytes.Equal(bw.Xor, []byte{0, 0, 0, 0}) {
		return ""
	}
	cmp, ok := rule.Exprs[3].(*expr.Cmp)
	if !ok || cmp == nil || cmp.Op != expr.CmpOpEq || cmp.Register != 1 || len(cmp.Data) != 4 {
		return ""
	}
	v, ok := rule.Exprs[4].(*expr.Verdict)
	if !ok || v == nil || v.Kind != expr.VerdictReturn {
		return ""
	}

	ip, mask := net.IP(cmp.Data), net.IPMask(bw.Mask)
	ones, bits := mask.Size()
	if bits != 32 || !bytes.Equal(ip, ip.Mask(mask)) {
		return ""
	}
	return fmt.Sprintf("%s/%d", ip.String(), ones)
}

// isSetMarkRule recognises a rule shaped like:
//
//	counter ct mark set ct mark | <mark>
func isSetMarkRule(rule *nftables.Rule, mark uint32) bool {
	if rule == nil || len(rule.Exprs) != 4 {
		return false
	}
	if counter, ok := rule.Exprs[0].(*expr.Counter); !ok || counter == nil {
		return false
	}
	load, ok := rule.Exprs[1].(*expr.Ct)
	if !ok || load == nil || load.Key != expr.CtKeyMARK || load.SourceRegister || load.Register != 1 {
		return false
	}
	bw, ok := rule.Exprs[2].(*expr.Bitwise)
	if !ok || bw == nil || bw.SourceRegister != 1 || bw.DestRegister != 1 || bw.Len != 4 ||
		!bytes.Equal(bw.Mask, binaryutil.NativeEndian.PutUint32(^mark)) ||
		!bytes.Equal(bw.Xor, binaryutil.NativeEndian.PutUint32(mark)) {
		return false
	}
	store, ok := rule.Exprs[3].(*expr.Ct)
	return ok && store != nil && store.Key == expr.CtKeyMARK && store.SourceRegister && store.Register == 1
}

// addSetMarkRule adds: nft add rule ip aws-cni snat-mark counter ct mark set ct mark | 0x80
//
// Equivalent to iptables: -j CONNMARK --set-xmark 0x80/0x80 (OR operation).
// nftables bitwise computes: result = (reg & Mask) ^ Xor
// With Mask=^mark and Xor=mark: (ct_mark & ~0x80) ^ 0x80 = ct_mark | 0x80
func (c *nftConnmark) addSetMarkRule(table *nftables.Table, chain *nftables.Chain) {
	markBytes := binaryutil.NativeEndian.PutUint32(c.mark)
	maskBytes := binaryutil.NativeEndian.PutUint32(^c.mark)
	c.nft.AddRule(&nftables.Rule{
		Table: table,
		Chain: chain,
		Exprs: []expr.Any{
			&expr.Counter{},
			&expr.Ct{Key: expr.CtKeyMARK, Register: 1},
			&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: 4, Mask: maskBytes, Xor: markBytes},
			&expr.Ct{Key: expr.CtKeyMARK, Register: 1, SourceRegister: true},
		},
	})
}

// insertCIDRReturnRule inserts: nft insert rule ip aws-cni snat-mark counter ip daddr <cidr> return
func (c *nftConnmark) insertCIDRReturnRule(table *nftables.Table, chain *nftables.Chain, cidr *net.IPNet) {
	c.nft.InsertRule(&nftables.Rule{
		Table: table,
		Chain: chain,
		Exprs: []expr.Any{
			&expr.Counter{},
			&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 16, Len: 4},
			&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: 4, Mask: cidr.Mask, Xor: []byte{0, 0, 0, 0}},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: cidr.IP.To4()},
			&expr.Verdict{Kind: expr.VerdictReturn},
		},
	})
}

func isNotExistError(err error) bool {
	if err == nil {
		return false
	}
	type notExister interface {
		IsNotExist() bool
	}
	if ne, ok := err.(notExister); ok {
		return ne.IsNotExist()
	}
	return false
}

func isNFTNotExistsError(err error) bool {
	if errors.Is(err, syscall.ENOENT) {
		return true
	}
	return strings.Contains(err.Error(), "no such file or directory")
}
