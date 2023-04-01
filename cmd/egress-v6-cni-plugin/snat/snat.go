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

	"github.com/coreos/go-iptables/iptables"
)

func iptRules6(target, src net.IP, chain, comment string, useRandomFully, useHashRandom bool) [][]string {
	var rules [][]string

	// Accept/ignore multicast (just because we can)
	rules = append(rules, []string{chain, "-d", "ff00::/8", "-j", "ACCEPT", "-m", "comment", "--comment", comment})

	// SNAT
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

// Snat6 SNATs IPv6 connections from `src` to `target`
func Snat6(target, src net.IP, chain, comment, randomizeSNAT string) error {
	ipt, err := iptables.NewWithProtocol(iptables.ProtocolIPv6)
	if err != nil {
		return fmt.Errorf("failed to locate iptables: %v", err)
	}

	//Defaults to `random-fully` unless a different option is explicitly set via
	//`AWS_VPC_K8S_CNI_RANDOMIZESNAT`. If the underlying iptables version doesn't support
	//'random-fully`, we will fall back to `random`.
	useRandomFully, useHashRandom := true, false
	if randomizeSNAT == "none" {
		useRandomFully = false
	} else if randomizeSNAT == "hashrandom" || !ipt.HasRandomFully() {
		useHashRandom, useRandomFully = true, false
	}

	rules := iptRules6(target, src, chain, comment, useRandomFully, useHashRandom)

	chains, err := ipt.ListChains("nat")
	if err != nil {
		return err
	}
	existingChains := make(map[string]bool, len(chains))
	for _, ch := range chains {
		existingChains[ch] = true
	}

	for _, rule := range rules {
		chain := rule[0]
		if !existingChains[chain] {
			if err = ipt.NewChain("nat", chain); err != nil {
				return err
			}
			existingChains[chain] = true
		}
	}

	for _, rule := range rules {
		chain := rule[0]
		if err := ipt.AppendUnique("nat", chain, rule[1:]...); err != nil {
			return err
		}
	}

	return nil
}

// Snat6Del removes rules added by snat6
func Snat6Del(src net.IP, chain, comment string) error {
	ipt, err := iptables.NewWithProtocol(iptables.ProtocolIPv6)
	if err != nil {
		return fmt.Errorf("failed to locate iptables: %v", err)
	}

	err = ipt.Delete("nat", "POSTROUTING", "-s", src.String(), "-j", chain, "-m", "comment", "--comment", comment)
	if err != nil && !isNotExist(err) {
		return err
	}

	err = ipt.ClearChain("nat", chain)
	if err != nil && !isNotExist(err) {
		return err
	}

	err = ipt.DeleteChain("nat", chain)
	if err != nil && !isNotExist(err) {
		return err
	}

	return nil
}

// isNotExist returns true if the error is from iptables indicating
// that the target does not exist.
func isNotExist(err error) bool {
	e, ok := err.(*iptables.Error)
	if !ok {
		return false
	}
	return e.IsNotExist()
}
