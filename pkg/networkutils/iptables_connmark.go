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
	jumpRule := []string{"-i", c.vethPrefix + "+", "-m", "comment", "--comment", "AWS, outbound connections", "-j", connmarkChainName}
	err = ipt.Delete("nat", "PREROUTING", jumpRule...)
	if err != nil {
		return err
	}

	// Delete restore rule
	restoreRule := []string{"-m", "comment", "--comment", "AWS, CONNMARK", "-j", "CONNMARK", "--restore-mark", "--mask", fmt.Sprintf("%#x", c.mark)}
	err = ipt.Delete("nat", "PREROUTING", restoreRule...)
	if err != nil {
		return err
	}

	// Clear and delete chain
	err = ipt.ClearChain("nat", connmarkChainName)
	if err != nil {
		return err
	}

	return nil
}

func (c *iptablesConnmark) buildRules(exemptCIDRs []string, ipt iptableswrapper.IPTablesIface) ([]iptablesRule, error) {
	if err := ipt.NewChain("nat", connmarkChainName); err != nil && !containChainExistErr(err) {
		return nil, err
	}

	var rules []iptablesRule
	// Force delete legacy rule with state match
	rules = append(rules, iptablesRule{
		name: "legacy connmark", shouldExist: false, table: "nat", chain: "PREROUTING",
		rule: []string{"-i", c.vethPrefix + "+", "-m", "comment", "--comment", "AWS, outbound connections",
			"-m", "state", "--state", "NEW", "-j", connmarkChainName},
	})

	// Jump rule
	rules = append(rules, iptablesRule{
		name: "connmark jump", shouldExist: true, table: "nat", chain: "PREROUTING",
		rule: []string{"-i", c.vethPrefix + "+", "-m", "comment", "--comment", "AWS, outbound connections", "-j", connmarkChainName},
	})

	// CIDR return rules
	for _, cidr := range exemptCIDRs {
		rule := []string{"-d", cidr, "-m", "comment", "--comment", "AWS CONNMARK CHAIN", "-j", "RETURN"}
		// Kernel will strip -d, if dst is 0.0.0.0/0
		if cidr == "0.0.0.0/0" {
			rule = []string{"-m", "comment", "--comment", "AWS CONNMARK CHAIN", "-j", "RETURN"}
		}
		rules = append(rules, iptablesRule{
			name: connmarkChainName, shouldExist: true, table: "nat", chain: connmarkChainName,
			rule: rule,
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
	staleRules, err := computeStaleIptablesRules(ipt, "nat", connmarkChainName, rules, []string{connmarkChainName})
	if err != nil {
		return nil, err
	}
	rules = append(rules, staleRules...)
	// Set mark rule
	rules = append(rules, iptablesRule{
		name: "set connmark", shouldExist: true, table: "nat", chain: connmarkChainName,
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
			if rule.name == connmarkChainName { // CIDR rules
				err = ipt.Insert(rule.table, rule.chain, 1, rule.rule...)
			} else {
				err = ipt.Append(rule.table, rule.chain, rule.rule...)
			}
			if err != nil {
				return err
			}
		} else if exists && !rule.shouldExist {
			err = ipt.Delete(rule.table, rule.chain, rule.rule...)
			if err != nil {
				log.Errorf("Failed to delete rule %v: %v", rule.rule, err)
				return err
			}
		}
	}
	return nil
}
