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

package mock_iptableswrapper

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/pkg/errors"
)

type MockIptables struct {
	// DataplaneState is a map from table name to chain name to slice of rulespecs
	DataplaneState map[string]map[string][][]string
}

type IptErrNotExists struct{}

func (e *IptErrNotExists) Error() string {
	// ref https://github.com/coreos/go-iptables/blob/main/iptables/iptables.go#L52
	return "does not exists"
}

// ref https://github.com/coreos/go-iptables/blob/v0.8.0/iptables/iptables.go#L56
func (e *IptErrNotExists) IsNotExist() bool {
	return true
}

func NewMockIptables() *MockIptables {
	return &MockIptables{DataplaneState: map[string]map[string][][]string{}}
}

func (ipt *MockIptables) Exists(table, chainName string, rulespec ...string) (bool, error) {
	chain := ipt.DataplaneState[table][chainName]
	for _, r := range chain {
		if reflect.DeepEqual(rulespec, r) {
			return true, nil
		}
	}
	return false, nil
}

func (ipt *MockIptables) Insert(table, chain string, pos int, rulespec ...string) error {
	if ipt.DataplaneState[table] == nil {
		ipt.DataplaneState[table] = map[string][][]string{}
	}
	if len(ipt.DataplaneState[table][chain]) == pos-1 {
		ipt.DataplaneState[table][chain] = append(ipt.DataplaneState[table][chain], rulespec)
	} else {
		ipt.DataplaneState[table][chain] = append(ipt.DataplaneState[table][chain][:pos], ipt.DataplaneState[table][chain][pos-1:]...)
		ipt.DataplaneState[table][chain][pos] = rulespec
	}
	return nil
}

func (ipt *MockIptables) Append(table, chain string, rulespec ...string) error {
	if ipt.DataplaneState[table] == nil {
		ipt.DataplaneState[table] = map[string][][]string{}
	}
	ipt.DataplaneState[table][chain] = append(ipt.DataplaneState[table][chain], rulespec)
	return nil
}

func (ipt *MockIptables) AppendUnique(table, chain string, rulespec ...string) error {
	exists, err := ipt.Exists(table, chain, rulespec...)
	if err != nil {
		return err
	}

	if !exists {
		return ipt.Append(table, chain, rulespec...)
	}

	return nil
}

func (ipt *MockIptables) Delete(table, chainName string, rulespec ...string) error {
	chain := ipt.DataplaneState[table][chainName]
	updatedChain := chain[:0]
	found := false
	for _, r := range chain {
		if !found && reflect.DeepEqual(rulespec, r) {
			found = true
			continue
		}
		updatedChain = append(updatedChain, r)
	}
	if !found {
		return &IptErrNotExists{}
	}
	ipt.DataplaneState[table][chainName] = updatedChain
	return nil
}

func (ipt *MockIptables) List(table, chain string) ([]string, error) {
	var chains []string
	chainContents := ipt.DataplaneState[table][chain]
	for _, ruleSpec := range chainContents {
		if slices.Contains(ruleSpec, "-N") {
			chains = append(chains, strings.Join(ruleSpec, " "))
			continue
		}
		sanitizedRuleSpec := []string{"-A", chain}
		for _, item := range ruleSpec {
			if strings.Contains(item, " ") {
				item = fmt.Sprintf("%q", item)
			}
			sanitizedRuleSpec = append(sanitizedRuleSpec, item)
		}
		chains = append(chains, strings.Join(sanitizedRuleSpec, " "))
	}
	return chains, nil
}

func (ipt *MockIptables) NewChain(table, chain string) error {
	exists, _ := ipt.ChainExists(table, chain)
	if exists {
		return errors.New("Chain already exists")
	}
	// Creating a new chain adds a -N chain rule to iptables
	ipt.Append(table, chain, "-N", chain)
	return nil
}

func (ipt *MockIptables) ClearChain(table, chain string) error {
	if ipt.DataplaneState[table] == nil {
		return nil
	}
	if _, ok := ipt.DataplaneState[table][chain]; !ok {
		return nil
	}
	ipt.DataplaneState[table][chain] = [][]string{{"-N", chain}}
	return nil
}

func (ipt *MockIptables) DeleteChain(table, chain string) error {
	// More than just the create chain rule
	if len(ipt.DataplaneState[table][chain]) > 1 {
		err := fmt.Sprintf("Chain %s is not empty", chain)
		return errors.New(err)
	}
	delete(ipt.DataplaneState[table], chain)
	return nil
}

func (ipt *MockIptables) ListChains(table string) ([]string, error) {
	var chains []string
	for chain := range ipt.DataplaneState[table] {
		chains = append(chains, chain)
	}
	return chains, nil
}

func (ipt *MockIptables) ChainExists(table, chain string) (bool, error) {
	_, ok := ipt.DataplaneState[table][chain]
	if ok {
		return true, nil
	}
	return false, nil
}

func (ipt *MockIptables) HasRandomFully() bool {
	// TODO: Work out how to write a test case for this
	return true
}
