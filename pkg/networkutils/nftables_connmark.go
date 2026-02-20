package networkutils

import (
	"bytes"
	"fmt"
	"net"
	"sync"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/iptableswrapper"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/nft"
	"github.com/coreos/go-iptables/iptables"
	"github.com/google/nftables"
	"github.com/google/nftables/binaryutil"
	"github.com/google/nftables/expr"
	"github.com/pkg/errors"
)

const (
	nftTableName     = "aws-cni"
	nftBaseChainName = "nat-prerouting"
	nftChainName     = "snat-mark"
)

type Connmark interface {
	Setup(exemptCIDRs []string) error
	Cleanup() error
}

type nftConnmark struct {
	nft         nft.Client
	vethPrefix  string
	mark        uint32
	cleanupOnce sync.Once
}

var _ Connmark = (*nftConnmark)(nil)

func NewConnmark(vethPrefix string, mark uint32) (Connmark, error) {

	mode, err := iptableswrapper.GetIptablesMode()
	if err != nil {
		log.Warnf("failed to detect iptables mode, defaulting to nftables: %v", err)
		return nil, err
	}
	if mode.IsNFTables() {
		return newNftablesConnmark(vethPrefix, mark)
	}
	return newIptablesConnmark(vethPrefix, mark)
}

func newNftablesConnmark(vethPrefix string, mark uint32) (Connmark, error) {
	client, err := nft.New()
	if err != nil {
		log.Error(err.Error())
		return nil, err
	}
	return &nftConnmark{
		nft:        client,
		vethPrefix: vethPrefix,
		mark:       mark,
	}, nil
}

func (c *nftConnmark) Setup(exemptCIDRs []string) error {

	// idempotent
	table := c.nft.AddTable(&nftables.Table{
		Family: nftables.TableFamilyIPv4,
		Name:   nftTableName,
	})

	baseChain := c.ensureBaseChain(table)
	connmarkChain, _ := c.ensureConnmarkChain(table)
	if err := c.nft.Flush(); err != nil {
		return errors.Wrap(err, "failed to flush nftable after base chain reconciliation")
	}

	err := c.ensureBaseChainRules(table, baseChain, connmarkChain, c.vethPrefix, c.mark)
	if err != nil {
		log.Error(err.Error())
		return err
	}
	err = c.ensureConnmarkChainRules(table, connmarkChain, exemptCIDRs, c.mark)
	if err != nil {
		log.Error(err.Error())
		return err
	}

	if err := c.nft.Flush(); err != nil {
		return errors.Wrap(err, "failed to flush nftable after chain rules reconciliation")
	}

	c.cleanupOnce.Do(c.cleanupIptablesConnmarkRules)
	return nil
}

func (c *nftConnmark) ensureBaseChain(table *nftables.Table) *nftables.Chain {
	existing := c.getBaseChain(table)
	if existing != nil && !isBaseChainConfigCorrect(existing, c.getDesiredPriority()) {
		// delete and re-add chain
		// need to flush the rules before deleting chain.
		// https://wiki.nftables.org/wiki-nftables/index.php/Configuring_chains#Deleting_chains
		c.nft.FlushChain(existing)
		c.nft.DelChain(existing)
		existing = nil
	}
	if existing == nil {
		priority := c.getDesiredPriority()
		chain := c.nft.AddChain(&nftables.Chain{
			Name:     nftBaseChainName,
			Table:    table,
			Type:     nftables.ChainTypeNAT,
			Hooknum:  nftables.ChainHookPrerouting,
			Priority: &priority,
		})
		return chain
	}
	return existing
}

func (c *nftConnmark) cleanupIptablesConnmarkRules() {
	// Delete the PREROUTING jump rule to AWS-CONNMARK-CHAIN-0
	ipt, err := iptableswrapper.NewIPTables(iptables.ProtocolIPv4)
	if err != nil {
		log.Errorf("failed to init iptables %s", err)
	}

	jumpRule := []string{
		"-i", c.vethPrefix + "+", "-m", "comment", "--comment", "AWS, outbound connections",
		"-j", "AWS-CONNMARK-CHAIN-0",
	}
	if err := ipt.Delete("nat", "PREROUTING", jumpRule...); err != nil && !isNotExistError(err) {
		log.Warnf("failed to delete connmark jump rule: %v", err)
	}

	// Delete the PREROUTING restore-mark rule
	restoreRule := []string{
		"-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK",
		"--restore-mark", "--mask", fmt.Sprintf("%#x", c.mark),
	}
	if err := ipt.Delete("nat", "PREROUTING", restoreRule...); err != nil && !isNotExistError(err) {
		log.Warnf("failed to delete connmark restore rule: %v", err)
	}

	// Flush and delete AWS-CONNMARK-CHAIN-0 if it exists
	if err := ipt.ClearChain("nat", "AWS-CONNMARK-CHAIN-0"); err != nil && !isNotExistError(err) {
		log.Warnf("failed to clear AWS-CONNMARK-CHAIN-0: %v", err)
	}
	if err := ipt.DeleteChain("nat", "AWS-CONNMARK-CHAIN-0"); err != nil && !isNotExistError(err) {
		log.Warnf("failed to delete AWS-CONNMARK-CHAIN-0: %v", err)
	}
}

// it has two rules, one to match vethprefix and jump to connmark chain, second is to mark packet with conntrack mark
func (c *nftConnmark) ensureBaseChainRules(table *nftables.Table, baseChain, targetChain *nftables.Chain, vethPrefix string, mark uint32) error {
	rules, err := c.nft.GetRules(table, baseChain)
	if err != nil {
		log.Error(err.Error())
		return err
	}
	hasJumpRule := false
	hasRestoreRule := false
	hasFiblocalReturnRule := false

	for _, rule := range rules {
		if isFibLocalReturnRule(rule) {
			hasFiblocalReturnRule = true
			continue
		}
		if isJumpRule(rule, targetChain.Name, vethPrefix) {
			hasJumpRule = true
			continue
		}
		if isRestoreRule(rule, mark) {
			hasRestoreRule = true
		}
	}

	if !hasFiblocalReturnRule {
		c.addFibLocalReturnRule(table, baseChain)
	}
	if !hasJumpRule {
		c.addJumpRule(table, baseChain, targetChain, vethPrefix)
	}
	if !hasRestoreRule {
		c.addRestoreRule(table, baseChain, mark)
	}
	return nil
}

// isFibLocalReturnRule checks for: fib daddr type local return
func isFibLocalReturnRule(rule *nftables.Rule) bool {
	hasFibAddrType := false
	hasCmpLocal := false
	hasReturn := false

	for _, e := range rule.Exprs {
		if fib, ok := e.(*expr.Fib); ok && fib.FlagDADDR && fib.ResultADDRTYPE {
			hasFibAddrType = true
		}
		// RTN_LOCAL = 2
		if cmp, ok := e.(*expr.Cmp); ok && cmp.Op == expr.CmpOpEq && len(cmp.Data) == 4 {
			val := binaryutil.NativeEndian.Uint32(cmp.Data)
			if val == 2 { // RTN_LOCAL
				hasCmpLocal = true
			}
		}
		if v, ok := e.(*expr.Verdict); ok && v.Kind == expr.VerdictReturn {
			hasReturn = true
		}
	}
	return hasFibAddrType && hasCmpLocal && hasReturn
}

// Add this function
func (c *nftConnmark) addFibLocalReturnRule(table *nftables.Table, chain *nftables.Chain) {
	c.nft.InsertRule(&nftables.Rule{
		Table: table,
		Chain: chain,
		Exprs: []expr.Any{
			&expr.Fib{
				Register:       1,
				FlagDADDR:      true,
				ResultADDRTYPE: true,
			},
			&expr.Cmp{
				Op:       expr.CmpOpEq,
				Register: 1,
				Data:     binaryutil.NativeEndian.PutUint32(2),
			},
			&expr.Verdict{Kind: expr.VerdictReturn},
		},
	})
}

func (c *nftConnmark) ensureConnmarkChain(table *nftables.Table) (*nftables.Chain, error) {
	existing := c.getConnmarkChain(table)
	if existing != nil {
		return existing, nil
	}
	return c.nft.AddChain(&nftables.Chain{
		Name:  nftChainName,
		Table: table,
	}), nil
}
func (c *nftConnmark) ensureConnmarkChainRules(table *nftables.Table, chain *nftables.Chain, exemptCIDRs []string, mark uint32) error {
	rules, err := c.nft.GetRules(table, chain)
	if err != nil {
		return err
	}

	currentCIDRs := make(map[string]*nftables.Rule)
	var setMarkRule *nftables.Rule
	var unknownRules []*nftables.Rule

	for _, r := range rules {
		if cidr := extractCIDRFromRule(r); cidr != "" {
			currentCIDRs[cidr] = r
		} else if isSetMarkRule(r, mark) {
			setMarkRule = r
		} else {
			unknownRules = append(unknownRules, r)
		}
	}

	// Delete unknown rules
	for _, r := range unknownRules {
		c.nft.DelRule(r)
	}

	desiredCIDRs := make(map[string]bool)
	for _, cidr := range exemptCIDRs {
		desiredCIDRs[cidr] = true
	}

	// Delete stale CIDRs
	for cidr, rule := range currentCIDRs {
		if !desiredCIDRs[cidr] {
			c.nft.DelRule(rule)
		}
	}

	// Insert missing CIDRs (prepends - order doesn't matter for CIDR rules)
	for cidr := range desiredCIDRs {
		if _, exists := currentCIDRs[cidr]; !exists {
			c.insertCIDRReturnRule(table, chain, cidr) // InsertRule
		}
	}

	// Ensure set-mark rule exists (AddRule appends to end)
	if setMarkRule == nil {
		c.addSetMarkRule(table, chain, mark)
	}

	return nil
}

func (c *nftConnmark) Cleanup() error {
	c.nft.DelTable(&nftables.Table{
		Family: nftables.TableFamilyIPv4,
		Name:   nftTableName,
	})

	err := c.nft.Flush()
	return err
}

func (c *nftConnmark) getBaseChain(table *nftables.Table) *nftables.Chain {
	chain, err := c.nft.ListChain(table, nftBaseChainName)
	if err != nil {
		log.Error(err.Error())
		return nil
	}
	return chain
}

func (c *nftConnmark) getDesiredPriority() nftables.ChainPriority {
	const priority nftables.ChainPriority = -90
	return nftables.ChainPriority(priority)
}

func isBaseChainConfigCorrect(chain *nftables.Chain, desiredPriority nftables.ChainPriority) bool {
	return chain.Hooknum == nftables.ChainHookPrerouting &&
		chain.Priority != nil && *chain.Priority == desiredPriority &&
		chain.Policy != nil && *chain.Policy == nftables.ChainPolicyAccept
}

func (c *nftConnmark) getConnmarkChain(table *nftables.Table) *nftables.Chain {
	chain, err := c.nft.ListChain(table, nftChainName)
	if err != nil {
		log.Error(err.Error())
		return nil
	}
	return chain
}

func (c *nftConnmark) addJumpRule(table *nftables.Table, baseChain, targetChain *nftables.Chain, vethPrefix string) {
	c.nft.AddRule(&nftables.Rule{
		Table: table,
		Chain: baseChain,
		Exprs: []expr.Any{
			&expr.Meta{Key: expr.MetaKeyIIFNAME, Register: 1},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte(vethPrefix)},
			&expr.Counter{},
			&expr.Verdict{
				Kind:  expr.VerdictJump,
				Chain: targetChain.Name,
			},
		},
	})
}

func (c *nftConnmark) addRestoreRule(table *nftables.Table, chain *nftables.Chain, mark uint32) {
	markBytes := binaryutil.NativeEndian.PutUint32(mark)
	c.nft.AddRule(&nftables.Rule{
		Table: table,
		Chain: chain,
		Exprs: []expr.Any{
			&expr.Counter{},
			&expr.Ct{Key: expr.CtKeyMARK, Register: 1},                                                          // load ct mark
			&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: 4, Mask: markBytes, Xor: []byte{0, 0, 0, 0}}, // AND with mask
			&expr.Meta{Key: expr.MetaKeyMARK, SourceRegister: true, Register: 1},                                // store to fwmark
		},
	})
}

func isJumpRule(rule *nftables.Rule, targetChain, vethPrefix string) bool {
	// check if rule is a jump rule to connmark chain
	// return true if it is
	// return false if it is not
	hasIFaceMatch := false
	hasJump := false
	hasCounter := false

	for _, e := range rule.Exprs {
		if cmp, ok := e.(*expr.Cmp); ok && bytes.Equal(cmp.Data, []byte(vethPrefix+"*\x00")) {
			hasIFaceMatch = true
		}
		if _, ok := e.(*expr.Counter); ok {
			hasCounter = true
		}
		if v, ok := e.(*expr.Verdict); ok && v.Kind == expr.VerdictJump && v.Chain == targetChain {
			hasJump = true
		}
	}
	return hasIFaceMatch && hasJump && hasCounter
}

func isRestoreRule(rule *nftables.Rule, mark uint32) bool {
	hasCounter := false
	hasCtLoad := false
	hasBitwise := false
	hasMetaStore := false
	markBytes := []byte{byte(mark), byte(mark >> 8), byte(mark >> 16), byte(mark >> 24)}
	for _, e := range rule.Exprs {
		if _, ok := e.(*expr.Counter); ok {
			hasCounter = true
		}
		if ct, ok := e.(*expr.Ct); ok && ct.Key == expr.CtKeyMARK && !ct.SourceRegister {
			hasCtLoad = true
		}
		if bw, ok := e.(*expr.Bitwise); ok && bytes.Equal(bw.Mask, markBytes) {
			hasBitwise = true
		}
		if m, ok := e.(*expr.Meta); ok && m.Key == expr.MetaKeyMARK && m.SourceRegister {
			hasMetaStore = true
		}
	}
	return hasCounter && hasCtLoad && hasBitwise && hasMetaStore
}

func extractCIDRFromRule(rule *nftables.Rule) string {
	var ip net.IP
	var mask net.IPMask
	hasPayload := false
	hasReturn := false

	for _, e := range rule.Exprs {
		if _, ok := e.(*expr.Payload); ok {
			hasPayload = true
		}
		if v, ok := e.(*expr.Verdict); ok && v.Kind == expr.VerdictReturn {
			hasReturn = true
		}
		if bw, ok := e.(*expr.Bitwise); ok && len(bw.Mask) == 4 {
			mask = net.IPMask(bw.Mask)
		}
		if cmp, ok := e.(*expr.Cmp); ok && cmp.Op == expr.CmpOpEq && len(cmp.Data) == 4 {
			ip = net.IP(cmp.Data)
		}
	}

	if ip == nil || mask == nil || !hasPayload || !hasReturn {
		return ""
	}
	ones, bits := mask.Size()
	if bits != 32 {
		return ""
	}
	return fmt.Sprintf("%s/%d", ip.String(), ones)
}

func isSetMarkRule(rule *nftables.Rule, mark uint32) bool {
	hasCtLoad := false
	hasBitwise := false
	hasCtStore := false
	markBytes := binaryutil.NativeEndian.PutUint32(mark)

	for _, e := range rule.Exprs {
		if ct, ok := e.(*expr.Ct); ok && ct.Key == expr.CtKeyMARK {
			if ct.SourceRegister {
				hasCtStore = true
			} else {
				hasCtLoad = true
			}
		}
		// ct mark | 0x80 uses Mask=0xFFFFFFFF, Xor=mark
		if bw, ok := e.(*expr.Bitwise); ok {
			if bytes.Equal(bw.Xor, markBytes) && bytes.Equal(bw.Mask, []byte{0xff, 0xff, 0xff, 0xff}) {
				hasBitwise = true
			}
		}
	}
	return hasCtLoad && hasBitwise && hasCtStore
}

func (c *nftConnmark) addSetMarkRule(table *nftables.Table, chain *nftables.Chain, mark uint32) {
	markBytes := binaryutil.NativeEndian.PutUint32(mark)
	c.nft.AddRule(&nftables.Rule{
		Table: table,
		Chain: chain,
		Exprs: []expr.Any{
			&expr.Counter{},
			&expr.Ct{Key: expr.CtKeyMARK, Register: 1},
			&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: 4, Mask: []byte{0xff, 0xff, 0xff, 0xff}, Xor: markBytes},
			&expr.Ct{Key: expr.CtKeyMARK, Register: 1, SourceRegister: true},
		},
	})
}

func (c *nftConnmark) insertCIDRReturnRule(table *nftables.Table, chain *nftables.Chain, cidrStr string) {
	_, cidr, err := net.ParseCIDR(cidrStr)
	if err != nil {
		return
	}
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
