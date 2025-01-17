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

package snat

import (
	"fmt"
	"net"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/iptableswrapper"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/cniutils"
)

func iptRules(target, src net.IP, multicastRange, chain, comment string, useRandomFully, useHashRandom bool, fixedPorts string) [][]string {
	var rules [][]string

	// Accept/ignore multicast (just because we can)
	rules = append(rules, []string{chain, "-d", multicastRange, "-j", "ACCEPT", "-m", "comment", "--comment", comment})

	if fixedPorts != "" && (useRandomFully || useHashRandom) {
		// Add protocol-specific SNAT rules for fixed ports
		for _, proto := range []string{"tcp", "udp", "sctp", "dccp"} {
			args := []string{
				chain,
				"-p", proto,
				"-m", "multiport",
				"--sports", fixedPorts,
				"-j", "SNAT",
				"--to-source", target.String(),
				"-m", "comment", "--comment", fmt.Sprintf("%s (fixed ports %s)", comment, proto),
			}
			rules = append(rules, args)
		}
	}

	// SNAT rule for remaining traffic (protocol-agnostic)
	args := []string{
		chain,
		"-j", "SNAT",
		"--to-source", target.String(),
		"-m", "comment", "--comment", comment,
	}

	if useRandomFully {
		args = append(args, "--random-fully")
	} else if useHashRandom {
		args = append(args, "--random")
	}

	rules = append(rules, args)
	rules = append(rules, []string{"POSTROUTING", "-s", src.String(), "-j", chain, "-m", "comment", "--comment", comment})

	return rules
}

// Add NAT entries to iptables for POD egress IPv6/IPv4 traffic
func Add(ipt iptableswrapper.IPTablesIface, nodeIP, src net.IP, multicastRange, chain, comment, rndSNAT, fixedPorts string) error {
	//Defaults to `random-fully` unless a different option is explicitly set via
	//`AWS_VPC_K8S_CNI_RANDOMIZESNAT`. If the underlying iptables version doesn't support
	//'random-fully`, we will fall back to `random`.
	useRandomFully, useHashRandom := true, false
	if rndSNAT == "none" {
		useRandomFully = false
	} else if rndSNAT == "hashrandom" || !ipt.HasRandomFully() {
		useHashRandom, useRandomFully = true, false
	}

	rules := iptRules(nodeIP, src, multicastRange, chain, comment, useRandomFully, useHashRandom, fixedPorts)

	chains, err := ipt.ListChains("nat")
	if err != nil {
		return err
	}
	existingChains := make(map[string]bool, len(chains))
	for _, ch := range chains {
		existingChains[ch] = true
	}

	for _, rule := range rules {
		_chain := rule[0]
		if !existingChains[_chain] {
			if err = ipt.NewChain("nat", _chain); err != nil {
				return err
			}
			existingChains[_chain] = true
		}
	}

	for _, rule := range rules {
		_chain := rule[0]
		if err = ipt.AppendUnique("nat", _chain, rule[1:]...); err != nil {
			return err
		}
	}

	return nil
}

// Del removes rules added by snat
func Del(ipt iptableswrapper.IPTablesIface, src net.IP, chain, comment string) (err error) {
	err = ipt.Delete("nat", "POSTROUTING", "-s", src.String(), "-j", chain, "-m", "comment", "--comment", comment)
	if err != nil && !cniutils.IsIptableTargetNotExist(err) {
		return err
	}

	err = ipt.ClearChain("nat", chain)
	if err != nil && !cniutils.IsIptableTargetNotExist(err) {
		return err
	}

	err = ipt.DeleteChain("nat", chain)
	if err != nil && !cniutils.IsIptableTargetNotExist(err) {
		return err
	}

	return nil
}
