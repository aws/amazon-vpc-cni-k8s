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

// Package iptableswrapper is a wrapper interface for the iptables package
package iptableswrapper

import (
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/cenkalti/backoff/v4"
	"github.com/coreos/go-iptables/iptables"
)

// IPTablesIface is an interface created to make code unit testable.
// Both the iptables package version and mocked version implement the same interface
type IPTablesIface interface {
	Exists(table, chain string, rulespec ...string) (bool, error)
	Insert(table, chain string, pos int, rulespec ...string) error
	Append(table, chain string, rulespec ...string) error
	AppendUnique(table, chain string, rulespec ...string) error
	Delete(table, chain string, rulespec ...string) error
	List(table, chain string) ([]string, error)
	NewChain(table, chain string) error
	ClearChain(table, chain string) error
	DeleteChain(table, chain string) error
	ListChains(table string) ([]string, error)
	ChainExists(table, chain string) (bool, error)
	HasRandomFully() bool
}

// ipTables is a struct that implements IPTablesIface using iptables package.
type ipTables struct {
	ipt     *iptables.IPTables
	backoff *backoff.ExponentialBackOff
}

// NewIPTables return a ipTables struct that implements IPTablesIface
func NewIPTables(protocol iptables.Protocol) (IPTablesIface, error) {
	ipt, err := iptables.New(iptables.IPFamily(protocol), iptables.Timeout(1))
	if err != nil {
		return nil, err
	}
	return &ipTables{
		ipt: ipt,
		backoff: &backoff.ExponentialBackOff{
			MaxElapsedTime: 0, // Never stop retrying
		},
	}, nil
}

// Exists implements IPTablesIface interface by calling iptables package
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

// Insert implements IPTablesIface interface by calling iptables package
func (i ipTables) Insert(table, chain string, pos int, rulespec ...string) error {
	operation := func() error {
		return i.ipt.Insert(table, chain, pos, rulespec...)
	}
	return backoff.RetryNotify(operation, i.backoff, logRetryError)
}

// Append implements IPTablesIface interface by calling iptables package
func (i ipTables) Append(table, chain string, rulespec ...string) error {
	operation := func() error {
		return i.ipt.Append(table, chain, rulespec...)
	}
	return backoff.RetryNotify(operation, i.backoff, logRetryError)
}

// AppendUnique implements IPTablesIface interface by calling iptables package
func (i ipTables) AppendUnique(table, chain string, rulespec ...string) error {
	operation := func() error {
		return i.ipt.AppendUnique(table, chain, rulespec...)
	}
	return backoff.RetryNotify(operation, i.backoff, logRetryError)
}

// Delete implements IPTablesIface interface by calling iptables package
func (i ipTables) Delete(table, chain string, rulespec ...string) error {
	operation := func() error {
		return i.ipt.Delete(table, chain, rulespec...)
	}
	return backoff.RetryNotify(operation, i.backoff, logRetryError)
}

// List implements IPTablesIface interface by calling iptables package
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

// NewChain implements IPTablesIface interface by calling iptables package
func (i ipTables) NewChain(table, chain string) error {
	operation := func() error {
		return i.ipt.NewChain(table, chain)
	}
	return backoff.RetryNotify(operation, i.backoff, logRetryError)
}

// ClearChain implements IPTablesIface interface by calling iptables package
func (i ipTables) ClearChain(table, chain string) error {
	operation := func() error {
		return i.ipt.ClearChain(table, chain)
	}
	return backoff.RetryNotify(operation, i.backoff, logRetryError)
}

// DeleteChain implements IPTablesIface interface by calling iptables package
func (i ipTables) DeleteChain(table, chain string) error {
	operation := func() error {
		return i.ipt.DeleteChain(table, chain)
	}
	return backoff.RetryNotify(operation, i.backoff, logRetryError)
}

// ListChains implements IPTablesIface interface by calling iptables package
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

// ChainExists implements IPTablesIface interface by calling iptables package
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

// HasRandomFully implements IPTablesIface interface by calling iptables package
func (i ipTables) HasRandomFully() bool {
	return i.ipt.HasRandomFully()
}

func logRetryError(err error, t time.Duration) {
	log.WithError(err).Errorf("Another app is currently holding the xtables lock. Retrying in %f seconds", t.Seconds())
}
