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
	"bytes"
	"fmt"
	"os/exec"
	"regexp"

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
	ipt *iptables.IPTables
}

// NewIPTables return a ipTables struct that implements IPTablesIface
func NewIPTables(protocol iptables.Protocol) (IPTablesIface, error) {
	ipt, err := iptables.NewWithProtocol(protocol)
	if err != nil {
		return nil, err
	}
	return &ipTables{
		ipt: ipt,
	}, nil
}

// Exists implements IPTablesIface interface by calling iptables package
func (i ipTables) Exists(table, chain string, rulespec ...string) (bool, error) {
	return i.ipt.Exists(table, chain, rulespec...)
}

// Insert implements IPTablesIface interface by calling iptables package
func (i ipTables) Insert(table, chain string, pos int, rulespec ...string) error {
	return i.ipt.Insert(table, chain, pos, rulespec...)
}

// Append implements IPTablesIface interface by calling iptables package
func (i ipTables) Append(table, chain string, rulespec ...string) error {
	return i.ipt.Append(table, chain, rulespec...)
}

// AppendUnique implements IPTablesIface interface by calling iptables package
func (i ipTables) AppendUnique(table, chain string, rulespec ...string) error {
	return i.ipt.AppendUnique(table, chain, rulespec...)
}

// Delete implements IPTablesIface interface by calling iptables package
func (i ipTables) Delete(table, chain string, rulespec ...string) error {
	return i.ipt.Delete(table, chain, rulespec...)
}

// List implements IPTablesIface interface by calling iptables package
func (i ipTables) List(table, chain string) ([]string, error) {
	return i.ipt.List(table, chain)
}

// NewChain implements IPTablesIface interface by calling iptables package
func (i ipTables) NewChain(table, chain string) error {
	return i.ipt.NewChain(table, chain)
}

// ClearChain implements IPTablesIface interface by calling iptables package
func (i ipTables) ClearChain(table, chain string) error {
	return i.ipt.ClearChain(table, chain)
}

// DeleteChain implements IPTablesIface interface by calling iptables package
func (i ipTables) DeleteChain(table, chain string) error {
	return i.ipt.DeleteChain(table, chain)
}

// ListChains implements IPTablesIface interface by calling iptables package
func (i ipTables) ListChains(table string) ([]string, error) {
	return i.ipt.ListChains(table)
}

// ChainExists implements IPTablesIface interface by calling iptables package
func (i ipTables) ChainExists(table, chain string) (bool, error) {
	return i.ipt.ChainExists(table, chain)
}

// HasRandomFully implements IPTablesIface interface by calling iptables package
func (i ipTables) HasRandomFully() bool {
	return i.ipt.HasRandomFully()
}

type IptablesMode string

const (
	IptablesModeLegacy IptablesMode = "legacy"
	IptablesModeNFT    IptablesMode = "nf_tables"
)

func (m IptablesMode) IsNFTables() bool {
	return m == IptablesModeNFT
}

var iptablesVersionRegex = regexp.MustCompile(`v([0-9]+)\.([0-9]+)\.([0-9]+)(?:\s+\((\w+))?`)

// GetIptablesMode runs "iptables --version" to detect backend (nf_tables or legacy)
func GetIptablesMode() (IptablesMode, error) {
	cmd := exec.Command("iptables", "--version")
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("iptables --version failed: %w, stderr: %s", err, stderr.String())
	}

	result := iptablesVersionRegex.FindStringSubmatch(out.String())
	if result == nil {
		return "", fmt.Errorf("failed to parse iptables version from: %s", out.String())
	}

	if result[4] == string(IptablesModeNFT) {
		return IptablesModeNFT, nil
	}
	return IptablesModeLegacy, nil
}
