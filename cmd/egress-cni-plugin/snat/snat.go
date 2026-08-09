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
	"errors"
	"fmt"
	"net"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/iptableswrapper"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/cniutils"
)

// Locker serializes the complete SNAT transaction across CNI processes.
type Locker interface {
	Lock() error
	Unlock() error
}

func withLock(lock Locker, action func() error) (err error) {
	if lock == nil {
		return action()
	}
	if err = lock.Lock(); err != nil {
		return fmt.Errorf("acquire SNAT transaction lock: %w", err)
	}
	defer func() {
		if unlockErr := lock.Unlock(); unlockErr != nil {
			err = errors.Join(err, fmt.Errorf("release SNAT transaction lock: %w", unlockErr))
		}
	}()
	return action()
}

func iptRules(target, src net.IP, multicastRange, chain, comment string, useRandomFully, useHashRandom bool) [][]string {
	var rules [][]string

	// Accept/ignore multicast (just because we can)
	rules = append(rules, []string{chain, "-d", multicastRange, "-j", "ACCEPT", "-m", "comment", "--comment", comment})

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

// Add NAT entries to iptables for POD egress IPv6/IPv4 traffic
func Add(ipt iptableswrapper.IPTablesIface, lock Locker, nodeIP, src net.IP, multicastRange, chain, comment, rndSNAT string) error {
	return withLock(lock, func() error {
		return add(ipt, nodeIP, src, multicastRange, chain, comment, rndSNAT)
	})
}

func add(ipt iptableswrapper.IPTablesIface, nodeIP, src net.IP, multicastRange, chain, comment, rndSNAT string) error {
	//Defaults to `random-fully` unless a different option is explicitly set via
	//`AWS_VPC_K8S_CNI_RANDOMIZESNAT`. If the underlying iptables version doesn't support
	//'random-fully`, we will fall back to `random`.
	useRandomFully, useHashRandom := true, false
	if rndSNAT == "none" {
		useRandomFully = false
	} else if rndSNAT == "hashrandom" || !ipt.HasRandomFully() {
		useHashRandom, useRandomFully = true, false
	}

	rules := iptRules(nodeIP, src, multicastRange, chain, comment, useRandomFully, useHashRandom)

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
			// ListChains is a snapshot. Recheck immediately before NewChain in
			// case a concurrent ADD for the same sandbox created this chain.
			exists, existsErr := ipt.ChainExists("nat", _chain)
			if existsErr != nil {
				return existsErr
			}
			if !exists {
				// A peer can still create the chain between the recheck and
				// NewChain. Verify the resulting state instead of relying on
				// backend-specific error text.
				if err = ipt.NewChain("nat", _chain); err != nil {
					createErr := err
					exists, existsErr = ipt.ChainExists("nat", _chain)
					if existsErr != nil {
						return errors.Join(createErr, existsErr)
					}
					if !exists {
						return createErr
					}
				}
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

// Del removes rules added by snat. Each step tolerates a not-exist result
// because a concurrent DEL for the same sandbox may have removed it already.
func Del(ipt iptableswrapper.IPTablesIface, lock Locker, src net.IP, chain, comment string) error {
	return withLock(lock, func() error {
		return del(ipt, src, chain, comment)
	})
}

func del(ipt iptableswrapper.IPTablesIface, src net.IP, chain, comment string) (err error) {
	err = ipt.Delete("nat", "POSTROUTING", "-s", src.String(), "-j", chain, "-m", "comment", "--comment", comment)
	if err != nil && !cniutils.IsChainNotExistErr(err) {
		return err
	}

	err = ipt.ClearChain("nat", chain)
	if err != nil && !cniutils.IsChainNotExistErr(err) {
		return err
	}

	err = ipt.DeleteChain("nat", chain)
	if err != nil && !cniutils.IsChainNotExistErr(err) {
		return err
	}

	return nil
}
