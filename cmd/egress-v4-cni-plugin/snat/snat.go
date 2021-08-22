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

func iptRules4(target, src net.IP, chain, comment string, useRandomFully bool) [][]string {
	var rules [][]string

	// Accept/ignore multicast (just because we can)
	rules = append(rules, []string{chain, "-d", "224.0.0.0/4", "-j", "ACCEPT", "-m", "comment", "--comment", comment})

	// SNAT
	args := []string{
		chain,
		"-j", "SNAT",
		"--to-source", target.String(),
		"-m", "comment", "--comment", comment,
	}
	if useRandomFully {
		args = append(args, "--random-fully")
	}
	rules = append(rules, args)
	rules = append(rules, []string{"POSTROUTING", "-s", src.String(), "-j", chain, "-m", "comment", "--comment", comment})

	return rules
}

//Setup a rule to block egress traffic directed to 169.254.172.0/22 from the Pod
func SetupRuleToBlockNodeLocalV4Access() error{
	ipt, _ := iptables.NewWithProtocol(iptables.ProtocolIPv4)
	if err := ipt.AppendUnique("filter", "OUTPUT", "-d", "169.254.172.0/22", "-m", "comment",
			"--comment", "Block Node Local Pod access via IPv4", "-j", "DROP"); err != nil {
			return fmt.Errorf("failed adding v4 drop route: %v", err)
			}

	return nil
}

// Snat4 SNATs IPv4 connections from `src` to `target`
func Snat4(target, src net.IP, chain, comment string) error {
	ipt, err := iptables.NewWithProtocol(iptables.ProtocolIPv4)
	if err != nil {
		return fmt.Errorf("failed to locate iptables: %v", err)
	}

	rules := iptRules4(target, src, chain, comment, ipt.HasRandomFully())

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

// Snat4Del removes rules added by snat4
func Snat4Del(src net.IP, chain, comment string) error {
	ipt, err := iptables.NewWithProtocol(iptables.ProtocolIPv4)
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
