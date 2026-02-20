// ===== iptables_connmark.go =====

package networkutils

import (
	"fmt"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/iptableswrapper"
	"github.com/coreos/go-iptables/iptables"
)

type iptablesConnmark struct {
	vethPrefix  string
	mark        uint32
	newIptables func(iptables.Protocol) (iptableswrapper.IPTablesIface, error)
}

var connmarkChainName string = "AWS-CONNMARK-CHAIN-0"
var _ Connmark = (*iptablesConnmark)(nil)

func newIptablesConnmark(vethPrefix string, mark uint32) (*iptablesConnmark, error) {
	return &iptablesConnmark{
		vethPrefix:  vethPrefix,
		mark:        mark,
		newIptables: iptableswrapper.NewIPTables,
	}, nil
}

func (c *iptablesConnmark) Setup(exemptCIDRs []string) error {
	if len(exemptCIDRs) == 0 {
		return fmt.Errorf("exemptCIDRs cannot be empty")
	}
	ipt, err := c.newIptables(iptables.ProtocolIPv4)
	if err != nil {
		return err
	}

	rules, err := c.buildRules(exemptCIDRs, ipt)
	if err != nil {
		return err
	}

	return c.applyRules(rules, ipt)
}

func (c *iptablesConnmark) Cleanup() error {
	ipt, err := c.newIptables(iptables.ProtocolIPv4)
	if err != nil {
		return err
	}

	// Delete jump rule
	jumpRule := []string{"-i", c.vethPrefix + "+", "-m", "comment", "--comment", "AWS, outbound connections", "-j", "AWS-CONNMARK-CHAIN-0"}
	_ = ipt.Delete("nat", "PREROUTING", jumpRule...)

	// Delete restore rule
	restoreRule := []string{"-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK", "--restore-mark", "--mask", fmt.Sprintf("%#x", c.mark)}
	_ = ipt.Delete("nat", "PREROUTING", restoreRule...)

	// Clear and delete chain
	_ = ipt.ClearChain("nat", "AWS-CONNMARK-CHAIN-0")
	//_ = ipt.DeleteChain("nat", "AWS-CONNMARK-CHAIN-0")

	return nil
}

func (c *iptablesConnmark) buildRules(exemptCIDRs []string, ipt iptableswrapper.IPTablesIface) ([]iptablesRule, error) {
	chain := "AWS-CONNMARK-CHAIN-0"
	if err := ipt.NewChain("nat", chain); err != nil && !containChainExistErr(err) {
		return nil, err
	}

	var rules []iptablesRule
	// Force delete legacy rule with state match
	rules = append(rules, iptablesRule{
		name: "legacy connmark", shouldExist: false, table: "nat", chain: "PREROUTING",
		rule: []string{"-i", c.vethPrefix + "+", "-m", "comment", "--comment", "AWS, outbound connections",
			"-m", "state", "--state", "NEW", "-j", chain},
	})

	// Jump rule
	rules = append(rules, iptablesRule{
		name: "connmark jump", shouldExist: true, table: "nat", chain: "PREROUTING",
		rule: []string{"-i", c.vethPrefix + "+", "-m", "comment", "--comment", "AWS, outbound connections", "-j", chain},
	})

	// CIDR return rules
	for _, cidr := range exemptCIDRs {
		rules = append(rules, iptablesRule{
			name: chain, shouldExist: true, table: "nat", chain: chain,
			rule: []string{"-d", cidr, "-m", "comment", "--comment", "AWS CONNMARK CHAIN", "-j", "RETURN"},
		})
	}
	// we might not need to mark this packet for delete.
	rules = append(rules, iptablesRule{
		name:        "connmark to fwmark copy",
		shouldExist: false,
		table:       "nat",
		chain:       "PREROUTING",
		rule: []string{
			"-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK",
			"--restore-mark", "--mask", fmt.Sprintf("%#x", c.mark),
		},
	})

	// Restore mark rule
	rules = append(rules, iptablesRule{
		name: "restore mark", shouldExist: true, table: "nat", chain: "PREROUTING",
		rule: []string{"-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK", "--restore-mark", "--mask", fmt.Sprintf("%#x", c.mark)},
	})

	// Compute stale rules (handles removed CIDRs)
	staleRules, err := computeStaleIptablesRules(ipt, "nat", "AWS-CONNMARK-CHAIN", rules, []string{chain})
	if err != nil {
		return nil, err
	}
	rules = append(rules, staleRules...)
	// Set mark rule
	rules = append(rules, iptablesRule{
		name: "set connmark", shouldExist: true, table: "nat", chain: chain,
		rule: []string{"-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK", "--set-xmark", fmt.Sprintf("%#x/%#x", c.mark, c.mark)},
	})

	return rules, nil
}

func (c *iptablesConnmark) applyRules(rules []iptablesRule, ipt iptableswrapper.IPTablesIface) error {
	for _, rule := range rules {
		exists, err := ipt.Exists(rule.table, rule.chain, rule.rule...)
		if err != nil {
			return err
		}

		if !exists && rule.shouldExist {
			if rule.name == "AWS-CONNMARK-CHAIN-0" { // CIDR rules
				err = ipt.Insert(rule.table, rule.chain, 1, rule.rule...)
			} else {
				err = ipt.Append(rule.table, rule.chain, rule.rule...)
			}
			if err != nil {
				return err
			}
		} else if exists && !rule.shouldExist {
			_ = ipt.Delete(rule.table, rule.chain, rule.rule...)
		}
	}
	return nil
}
