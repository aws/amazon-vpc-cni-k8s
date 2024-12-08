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

// Package networkutils is a collection of iptables and netlink functions
package networkutils

import (
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/iptableswrapper"
	"github.com/cenkalti/backoff/v4"
	"github.com/coreos/go-iptables/iptables"
)

type ipTables struct {
	ipt     iptableswrapper.IPTablesIface
	backoff *backoff.ExponentialBackOff
}

// NewIPTables return a ipTables struct that implements IPTablesIface
func NewIPTables(protocol iptables.Protocol) (iptableswrapper.IPTablesIface, error) {
	ipt, err := iptables.New(iptables.IPFamily(protocol), iptables.Timeout(1))
	if err != nil {
		return nil, err
	}
	return &ipTables{
		ipt:     ipt,
		backoff: backoff.NewExponentialBackOff(backoff.WithMaxElapsedTime(0)), // Never stop retrying as backward compatibility
	}, nil
}

func (i ipTables) Exists(table, chain string, rulespec ...string) (bool, error) {
	operation := func() (bool, error) {
		return i.ipt.Exists(table, chain, rulespec...)
	}
	result, err := backoff.RetryNotifyWithData(operation, i.backoff, logRetryError)
	if err != nil {
		return true, err
	}
	return result, nil
}

func (i ipTables) Insert(table, chain string, pos int, rulespec ...string) error {
	operation := func() error {
		return i.ipt.Insert(table, chain, pos, rulespec...)
	}
	return backoff.RetryNotify(operation, i.backoff, logRetryError)
}

func (i ipTables) Append(table, chain string, rulespec ...string) error {
	operation := func() error {
		return i.ipt.Append(table, chain, rulespec...)
	}
	return backoff.RetryNotify(operation, i.backoff, logRetryError)
}

func (i ipTables) AppendUnique(table, chain string, rulespec ...string) error {
	operation := func() error {
		return i.ipt.AppendUnique(table, chain, rulespec...)
	}
	return backoff.RetryNotify(operation, i.backoff, logRetryError)
}

func (i ipTables) Delete(table, chain string, rulespec ...string) error {
	operation := func() error {
		return i.ipt.Delete(table, chain, rulespec...)
	}
	return backoff.RetryNotify(operation, i.backoff, logRetryError)
}

func (i ipTables) List(table, chain string) ([]string, error) {
	operation := func() ([]string, error) {
		return i.ipt.List(table, chain)
	}
	result, err := backoff.RetryNotifyWithData(operation, i.backoff, logRetryError)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (i ipTables) NewChain(table, chain string) error {
	operation := func() error {
		return i.ipt.NewChain(table, chain)
	}
	return backoff.RetryNotify(operation, i.backoff, logRetryError)
}

func (i ipTables) ClearChain(table, chain string) error {
	operation := func() error {
		return i.ipt.ClearChain(table, chain)
	}
	return backoff.RetryNotify(operation, i.backoff, logRetryError)
}

func (i ipTables) DeleteChain(table, chain string) error {
	operation := func() error {
		return i.ipt.DeleteChain(table, chain)
	}
	return backoff.RetryNotify(operation, i.backoff, logRetryError)
}

func (i ipTables) ListChains(table string) ([]string, error) {
	operation := func() ([]string, error) {
		return i.ipt.ListChains(table)
	}
	result, err := backoff.RetryNotifyWithData(operation, i.backoff, logRetryError)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (i ipTables) ChainExists(table, chain string) (bool, error) {
	operation := func() (bool, error) {
		return i.ipt.ChainExists(table, chain)
	}
	result, err := backoff.RetryNotifyWithData(operation, i.backoff, logRetryError)
	if err != nil {
		return true, err
	}
	return result, nil
}

func (i ipTables) HasRandomFully() bool {
	return i.ipt.HasRandomFully()
}

func logRetryError(err error, t time.Duration) {
	log.Errorf("Another app is currently holding the xtables lock. Retrying in %f seconds", t.Seconds())
}
