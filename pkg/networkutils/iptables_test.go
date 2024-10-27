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
	"crypto/rand"
	"math/big"
	"testing"

	mock_iptables "github.com/aws/amazon-vpc-cni-k8s/pkg/iptableswrapper/mocks"
	"github.com/cenkalti/backoff/v4"
	"github.com/golang/mock/gomock"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
)

const (
	address1 = "203.0.113.1/32"
	address2 = "203.0.113.2/32"
	subnet1  = "192.0.2.0/24"
)

var (
	timeoutError          = errors.New("timeout")
	permissionDeniedError = errors.New("permission denied")
)

func randChain(t *testing.T) string {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		t.Fatalf("Failed to generate random chain name: %v", err)
	}

	return "TEST-" + n.String()
}

func setupIPTTest(t *testing.T) (*gomock.Controller,
	string,
	string,
	*backoff.ExponentialBackOff) {
	return gomock.NewController(t),
		"filter",
		randChain(t),
		backoff.NewExponentialBackOff(backoff.WithMaxElapsedTime(0))
}

func TestExists(t *testing.T) {
	ctrl, table, chain, expBackoff := setupIPTTest(t)
	mockIptable := mock_iptables.NewMockIPTablesIface(ctrl)

	gomock.InOrder(
		mockIptable.EXPECT().Exists(table, chain, "-s", subnet1, "-d", address2, "-j", "ACCEPT").Return(false, timeoutError),
		mockIptable.EXPECT().Exists(table, chain, "-s", subnet1, "-d", address2, "-j", "ACCEPT").Return(false, nil),
	)

	ipt := &ipTables{
		ipt:     mockIptable,
		backoff: expBackoff,
	}

	exists, _ := ipt.Exists(table, chain, "-s", subnet1, "-d", address2, "-j", "ACCEPT")
	assert.Equal(t, exists, false)
}

func TestInsert(t *testing.T) {
	ctrl, table, chain, expBackoff := setupIPTTest(t)
	mockIptable := mock_iptables.NewMockIPTablesIface(ctrl)

	gomock.InOrder(
		mockIptable.EXPECT().Insert(table, chain, 2, "-s", subnet1, "-d", address2, "-j", "ACCEPT").Return(timeoutError),
		mockIptable.EXPECT().Insert(table, chain, 2, "-s", subnet1, "-d", address2, "-j", "ACCEPT").Return(permissionDeniedError),
		mockIptable.EXPECT().Insert(table, chain, 2, "-s", subnet1, "-d", address2, "-j", "ACCEPT").Return(timeoutError),
		mockIptable.EXPECT().Insert(table, chain, 2, "-s", subnet1, "-d", address2, "-j", "ACCEPT").Return(nil),
	)

	ipt := &ipTables{
		ipt:     mockIptable,
		backoff: expBackoff,
	}

	err := ipt.Insert(table, chain, 2, "-s", subnet1, "-d", address2, "-j", "ACCEPT")
	assert.NoError(t, err)
}

func TestAppend(t *testing.T) {
	ctrl, table, chain, expBackoff := setupIPTTest(t)
	mockIptable := mock_iptables.NewMockIPTablesIface(ctrl)

	gomock.InOrder(
		mockIptable.EXPECT().Append(table, chain, "-s", subnet1, "-d", address2, "-j", "ACCEPT").Return(timeoutError),
		mockIptable.EXPECT().Append(table, chain, "-s", subnet1, "-d", address2, "-j", "ACCEPT").Return(permissionDeniedError),
		mockIptable.EXPECT().Append(table, chain, "-s", subnet1, "-d", address2, "-j", "ACCEPT").Return(timeoutError),
		mockIptable.EXPECT().Append(table, chain, "-s", subnet1, "-d", address2, "-j", "ACCEPT").Return(nil),
	)

	ipt := &ipTables{
		ipt:     mockIptable,
		backoff: expBackoff,
	}

	err := ipt.Append(table, chain, "-s", subnet1, "-d", address2, "-j", "ACCEPT")
	assert.NoError(t, err)
}

func TestAppendUnique(t *testing.T) {
	ctrl, table, chain, expBackoff := setupIPTTest(t)
	mockIptable := mock_iptables.NewMockIPTablesIface(ctrl)

	gomock.InOrder(
		mockIptable.EXPECT().AppendUnique(table, chain, "-s", subnet1, "-d", address2, "-j", "ACCEPT").Return(timeoutError),
		mockIptable.EXPECT().AppendUnique(table, chain, "-s", subnet1, "-d", address2, "-j", "ACCEPT").Return(nil),
	)

	ipt := &ipTables{
		ipt:     mockIptable,
		backoff: expBackoff,
	}

	err := ipt.AppendUnique(table, chain, "-s", subnet1, "-d", address2, "-j", "ACCEPT")
	assert.NoError(t, err)
}

func TestDelete(t *testing.T) {
	ctrl, table, chain, expBackoff := setupIPTTest(t)
	mockIptable := mock_iptables.NewMockIPTablesIface(ctrl)

	gomock.InOrder(
		mockIptable.EXPECT().Delete(table, chain, "-s", subnet1, "-d", address2, "-j", "ACCEPT").Return(permissionDeniedError),
		mockIptable.EXPECT().Delete(table, chain, "-s", subnet1, "-d", address2, "-j", "ACCEPT").Return(nil),
	)

	ipt := &ipTables{
		ipt:     mockIptable,
		backoff: expBackoff,
	}

	err := ipt.Delete(table, chain, "-s", subnet1, "-d", address2, "-j", "ACCEPT")
	assert.NoError(t, err)
}

func TestList(t *testing.T) {
	ctrl, table, chain, expBackoff := setupIPTTest(t)
	mockIptable := mock_iptables.NewMockIPTablesIface(ctrl)

	expected := []string{
		"-N " + chain,
		"-A " + chain + " -s " + address1 + " -d " + subnet1 + " -j ACCEPT",
		"-A " + chain + " -s " + address2 + " -d " + subnet1 + " -j ACCEPT",
	}

	gomock.InOrder(
		mockIptable.EXPECT().List(table, chain).Return(nil, timeoutError),
		mockIptable.EXPECT().List(table, chain).Return(nil, timeoutError),
		mockIptable.EXPECT().List(table, chain).Return(expected, nil),
	)

	ipt := &ipTables{
		ipt:     mockIptable,
		backoff: expBackoff,
	}

	result, _ := ipt.List(table, chain)
	assert.Equal(t, result, expected)
}

func TestNewChain(t *testing.T) {
	ctrl, table, chain, expBackoff := setupIPTTest(t)
	mockIptable := mock_iptables.NewMockIPTablesIface(ctrl)

	gomock.InOrder(
		mockIptable.EXPECT().NewChain(table, chain).Return(permissionDeniedError),
		mockIptable.EXPECT().NewChain(table, chain).Return(timeoutError),
		mockIptable.EXPECT().NewChain(table, chain).Return(nil),
	)

	ipt := &ipTables{
		ipt:     mockIptable,
		backoff: expBackoff,
	}

	err := ipt.NewChain(table, chain)
	assert.NoError(t, err)
}

func TestClearChain(t *testing.T) {
	ctrl, table, chain, expBackoff := setupIPTTest(t)
	mockIptable := mock_iptables.NewMockIPTablesIface(ctrl)

	gomock.InOrder(
		mockIptable.EXPECT().ClearChain(table, chain).Return(permissionDeniedError),
		mockIptable.EXPECT().ClearChain(table, chain).Return(timeoutError),
		mockIptable.EXPECT().ClearChain(table, chain).Return(nil),
	)

	ipt := &ipTables{
		ipt:     mockIptable,
		backoff: expBackoff,
	}

	err := ipt.ClearChain(table, chain)
	assert.NoError(t, err)
}

func TestDeleteChain(t *testing.T) {
	ctrl, table, chain, expBackoff := setupIPTTest(t)
	mockIptable := mock_iptables.NewMockIPTablesIface(ctrl)

	gomock.InOrder(
		mockIptable.EXPECT().DeleteChain(table, chain).Return(permissionDeniedError),
		mockIptable.EXPECT().DeleteChain(table, chain).Return(timeoutError),
		mockIptable.EXPECT().DeleteChain(table, chain).Return(permissionDeniedError),
		mockIptable.EXPECT().DeleteChain(table, chain).Return(nil),
	)

	ipt := &ipTables{
		ipt:     mockIptable,
		backoff: expBackoff,
	}

	err := ipt.DeleteChain(table, chain)
	assert.NoError(t, err)
}

func TestListChains(t *testing.T) {
	ctrl, table, _, expBackoff := setupIPTTest(t)
	mockIptable := mock_iptables.NewMockIPTablesIface(ctrl)

	expected := []string{
		"filter",
		"input",
	}

	gomock.InOrder(
		mockIptable.EXPECT().ListChains(table).Return(nil, timeoutError),
		mockIptable.EXPECT().ListChains(table).Return(expected, nil),
	)

	ipt := &ipTables{
		ipt:     mockIptable,
		backoff: expBackoff,
	}

	result, _ := ipt.ListChains(table)
	assert.Equal(t, expected, result)
}

func TestChainExists(t *testing.T) {
	ctrl, table, chain, expBackoff := setupIPTTest(t)

	mockIptable := mock_iptables.NewMockIPTablesIface(ctrl)

	gomock.InOrder(
		mockIptable.EXPECT().ChainExists(table, chain).Return(false, timeoutError),
		mockIptable.EXPECT().ChainExists(table, chain).Return(false, timeoutError),
		mockIptable.EXPECT().ChainExists(table, chain).Return(true, nil),
	)

	ipt := &ipTables{
		ipt:     mockIptable,
		backoff: expBackoff,
	}

	exists, _ := ipt.ChainExists(table, chain)
	assert.Equal(t, exists, true)
}
