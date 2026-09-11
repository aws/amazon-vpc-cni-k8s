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
	"strings"
	"sync"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/iptableswrapper"
	mock_iptables "github.com/aws/amazon-vpc-cni-k8s/pkg/iptableswrapper/mocks"
)

const (
	//ipv6MulticastRange = "ff00::/8"
	ipv4MulticastRange = "224.0.0.0/4"

	chainV4 = "CNI-E4"
	//chainV6   = "CNI-E6"
	comment = "unit-test-comment"
	rndSNAT = "hashrandom"
)

var (
	containerIpv6 = net.ParseIP("fd00::10")
	nodeIPv6      = net.ParseIP("2600::")
	containerIPv4 = net.ParseIP("169.254.172.10")
	nodeIPv4      = net.ParseIP("192.168.1.123")
)

func TestAddV4(t *testing.T) {
	ipt := mock_iptables.NewMockIPTablesIface(gomock.NewController(t))

	expectChain := []string{chainV4}
	actualChain := []string{}

	expectRule := []string{
		fmt.Sprintf("nat %s -d %s -j ACCEPT -m comment --comment %s", chainV4, ipv4MulticastRange, comment),
		fmt.Sprintf("nat %s -j SNAT --to-source %s -m comment --comment %s --random", chainV4, nodeIPv4.String(), comment),
		fmt.Sprintf("nat POSTROUTING -s %s -j %s -m comment --comment %s", containerIPv4.String(), chainV4, comment),
	}
	actualRule := []string{}

	setupAddExpect(ipt, &actualChain, &actualRule)

	err := Add(ipt, nodeIPv4, containerIPv4, ipv4MulticastRange, chainV4, comment, rndSNAT)
	assert.Nil(t, err)

	assert.EqualValuesf(t, expectChain, actualChain, "iptables chain is expected to be created")

	assert.EqualValuesf(t, expectRule, actualRule, "iptables rules are expected to be created")
}

func TestAddV4NewChainFailureWithExistingChainContinues(t *testing.T) {
	ipt := mock_iptables.NewMockIPTablesIface(gomock.NewController(t))
	createErr := errors.New("opaque chain creation failure")

	ipt.EXPECT().ListChains("nat").Return([]string{"POSTROUTING"}, nil)
	ipt.EXPECT().NewChain("nat", chainV4).Return(createErr)
	ipt.EXPECT().ChainExists("nat", chainV4).Return(true, nil)
	ipt.EXPECT().AppendUnique("nat", gomock.Any(), gomock.Any()).Return(nil).Times(3)

	err := Add(ipt, nodeIPv4, containerIPv4, ipv4MulticastRange, chainV4, comment, rndSNAT)
	assert.NoError(t, err)
}

func TestAddV4NewChainFailureWithoutExistingChainReturnsOriginalError(t *testing.T) {
	ipt := mock_iptables.NewMockIPTablesIface(gomock.NewController(t))
	createErr := errors.New("opaque chain creation failure")

	ipt.EXPECT().ListChains("nat").Return([]string{"POSTROUTING"}, nil)
	ipt.EXPECT().NewChain("nat", chainV4).Return(createErr)
	ipt.EXPECT().ChainExists("nat", chainV4).Return(false, nil)

	err := Add(ipt, nodeIPv4, containerIPv4, ipv4MulticastRange, chainV4, comment, rndSNAT)
	assert.ErrorIs(t, err, createErr)
}

func TestAddV4NewChainFailureAndStateCheckFailureReturnsBothErrors(t *testing.T) {
	ipt := mock_iptables.NewMockIPTablesIface(gomock.NewController(t))
	createErr := errors.New("opaque chain creation failure")
	checkErr := errors.New("chain state unavailable")

	ipt.EXPECT().ListChains("nat").Return([]string{"POSTROUTING"}, nil)
	ipt.EXPECT().NewChain("nat", chainV4).Return(createErr)
	ipt.EXPECT().ChainExists("nat", chainV4).Return(false, checkErr)

	err := Add(ipt, nodeIPv4, containerIPv4, ipv4MulticastRange, chainV4, comment, rndSNAT)
	assert.ErrorIs(t, err, createErr)
	assert.ErrorIs(t, err, checkErr)
}

type concurrentPodsIPTables struct {
	iptableswrapper.IPTablesIface

	snapshotsReady sync.WaitGroup
	release        chan struct{}

	mu                   sync.Mutex
	chains               map[string]struct{}
	rules                map[string]map[string][]string
	snapshots            [][]string
	newChainAttempts     map[string]int
	newChainFailures     map[string]int
	chainExistsChecks    map[string]int
	appendUniqueAttempts map[string]int
}

func newConcurrentPodsIPTables(callers int) *concurrentPodsIPTables {
	ipt := &concurrentPodsIPTables{
		release: make(chan struct{}),
		chains: map[string]struct{}{
			"PREROUTING": {},
			"INPUT":      {},
			"OUTPUT":     {},
		},
		rules:                make(map[string]map[string][]string),
		newChainAttempts:     make(map[string]int),
		newChainFailures:     make(map[string]int),
		chainExistsChecks:    make(map[string]int),
		appendUniqueAttempts: make(map[string]int),
	}
	ipt.snapshotsReady.Add(callers)
	return ipt
}

func (ipt *concurrentPodsIPTables) ListChains(table string) ([]string, error) {
	if table != "nat" {
		ipt.snapshotsReady.Done()
		return nil, fmt.Errorf("unexpected table %s", table)
	}

	ipt.mu.Lock()
	snapshot := make([]string, 0, len(ipt.chains))
	for chain := range ipt.chains {
		snapshot = append(snapshot, chain)
	}
	ipt.snapshots = append(ipt.snapshots, snapshot)
	ipt.mu.Unlock()

	ipt.snapshotsReady.Done()
	<-ipt.release
	return snapshot, nil
}

func (ipt *concurrentPodsIPTables) NewChain(table, chain string) error {
	if table != "nat" {
		return fmt.Errorf("unexpected table %s", table)
	}

	ipt.mu.Lock()
	defer ipt.mu.Unlock()

	ipt.newChainAttempts[chain]++
	if _, exists := ipt.chains[chain]; exists {
		ipt.newChainFailures[chain]++
		return errors.New("opaque concurrent chain creation failure")
	}
	ipt.chains[chain] = struct{}{}
	return nil
}

func (ipt *concurrentPodsIPTables) ChainExists(table, chain string) (bool, error) {
	if table != "nat" {
		return false, fmt.Errorf("unexpected table %s", table)
	}

	ipt.mu.Lock()
	defer ipt.mu.Unlock()

	ipt.chainExistsChecks[chain]++
	_, exists := ipt.chains[chain]
	return exists, nil
}

func (ipt *concurrentPodsIPTables) AppendUnique(table, chain string, rulespec ...string) error {
	if table != "nat" {
		return fmt.Errorf("unexpected table %s", table)
	}

	ipt.mu.Lock()
	defer ipt.mu.Unlock()

	if _, exists := ipt.chains[chain]; !exists {
		return fmt.Errorf("chain %s does not exist", chain)
	}

	ruleKey := strings.Join(rulespec, "\x00")
	attemptKey := chain + "\x00" + ruleKey
	ipt.appendUniqueAttempts[attemptKey]++

	if ipt.rules[chain] == nil {
		ipt.rules[chain] = make(map[string][]string)
	}
	if _, exists := ipt.rules[chain][ruleKey]; !exists {
		ipt.rules[chain][ruleKey] = append([]string(nil), rulespec...)
	}
	return nil
}

func (ipt *concurrentPodsIPTables) HasRandomFully() bool {
	return false
}

// TestAddConcurrentPodsRecoverSharedChainCreationCollision validates recovery
// from the reported collision while different Pods create the shared
// POSTROUTING chain. The test supplies the observed missing-chain snapshot; it
// does not reproduce or explain why nftables omitted this normally built-in
// chain from the production snapshot.
func TestAddConcurrentPodsRecoverSharedChainCreationCollision(t *testing.T) {
	pods := []struct {
		src     net.IP
		chain   string
		comment string
	}{
		{
			src:     net.ParseIP("169.254.172.10"),
			chain:   "CNI-E4-111111111111111111111",
			comment: `name: "aws-cni" id: "container-a"`,
		},
		{
			src:     net.ParseIP("169.254.172.11"),
			chain:   "CNI-E4-222222222222222222222",
			comment: `name: "aws-cni" id: "container-b"`,
		},
	}

	ipt := newConcurrentPodsIPTables(len(pods))
	errs := make(chan error, len(pods))

	var wg sync.WaitGroup
	for _, pod := range pods {
		pod := pod
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- Add(ipt, nodeIPv4, pod.src, ipv4MulticastRange, pod.chain, pod.comment, rndSNAT)
		}()
	}

	ipt.snapshotsReady.Wait()
	close(ipt.release)
	wg.Wait()
	close(errs)

	for err := range errs {
		assert.NoError(t, err)
	}

	ipt.mu.Lock()
	defer ipt.mu.Unlock()

	assert.Len(t, ipt.snapshots, len(pods))
	for _, snapshot := range ipt.snapshots {
		assert.NotContains(t, snapshot, "POSTROUTING")
		for _, pod := range pods {
			assert.NotContains(t, snapshot, pod.chain)
		}
	}

	assert.Contains(t, ipt.chains, "POSTROUTING")
	assert.Equal(t, 2, ipt.newChainAttempts["POSTROUTING"])
	assert.Equal(t, 1, ipt.newChainFailures["POSTROUTING"])
	assert.Equal(t, 1, ipt.chainExistsChecks["POSTROUTING"])
	assert.Len(t, ipt.chains, 6)
	assert.Len(t, ipt.newChainAttempts, 3)
	assert.Len(t, ipt.newChainFailures, 1)
	assert.Len(t, ipt.chainExistsChecks, 1)
	for _, pod := range pods {
		assert.Equal(t, 1, ipt.newChainAttempts[pod.chain])
	}

	expectedRules := map[string][][]string{
		pods[0].chain: {
			{"-d", ipv4MulticastRange, "-j", "ACCEPT", "-m", "comment", "--comment", pods[0].comment},
			{"-j", "SNAT", "--to-source", nodeIPv4.String(), "-m", "comment", "--comment", pods[0].comment, "--random"},
		},
		pods[1].chain: {
			{"-d", ipv4MulticastRange, "-j", "ACCEPT", "-m", "comment", "--comment", pods[1].comment},
			{"-j", "SNAT", "--to-source", nodeIPv4.String(), "-m", "comment", "--comment", pods[1].comment, "--random"},
		},
		"POSTROUTING": {
			{"-s", pods[0].src.String(), "-j", pods[0].chain, "-m", "comment", "--comment", pods[0].comment},
			{"-s", pods[1].src.String(), "-j", pods[1].chain, "-m", "comment", "--comment", pods[1].comment},
		},
	}

	totalRules := 0
	for chain, expected := range expectedRules {
		assert.Contains(t, ipt.chains, chain)
		assert.Len(t, ipt.rules[chain], len(expected))
		totalRules += len(ipt.rules[chain])

		for _, rule := range expected {
			ruleKey := strings.Join(rule, "\x00")
			assert.Contains(t, ipt.rules[chain], ruleKey)
			assert.Equal(t, 1, ipt.appendUniqueAttempts[chain+"\x00"+ruleKey])
		}
	}
	assert.Equal(t, 6, totalRules)
	assert.Len(t, ipt.appendUniqueAttempts, totalRules)
}

func TestDelV4(t *testing.T) {
	ipt := mock_iptables.NewMockIPTablesIface(gomock.NewController(t))

	expectClearChain := []string{chainV4}
	actualClearChain := []string{}

	expectDeleteChain := []string{chainV4}
	actualDeleteChain := []string{}

	expectRule := []string{fmt.Sprintf("nat POSTROUTING -s %s -j %s -m comment --comment %s", containerIPv4.String(), chainV4, comment)}
	actualRule := []string{}

	setupDelExpect(ipt, &actualClearChain, &actualDeleteChain, &actualRule)

	err := Del(ipt, containerIPv4, chainV4, comment)
	assert.Nil(t, err)

	assert.EqualValuesf(t, expectClearChain, actualClearChain, "iptables chain is expected to be cleared")

	assert.EqualValuesf(t, expectDeleteChain, actualDeleteChain, "iptables chain is expected to be removed")

	assert.EqualValuesf(t, expectRule, actualRule, "iptables rule is expected to be removed")
}

func setupAddExpect(ipt iptableswrapper.IPTablesIface, actualNewChain, actualNewRule *[]string) {
	ipt.(*mock_iptables.MockIPTablesIface).EXPECT().ListChains("nat").Return(
		[]string{"POSTROUTING"}, nil)

	ipt.(*mock_iptables.MockIPTablesIface).EXPECT().NewChain("nat", gomock.Any()).Do(func(_, arg1 interface{}) {
		chain := arg1.(string)
		*actualNewChain = append(*actualNewChain, chain)
	}).Return(nil)

	ipt.(*mock_iptables.MockIPTablesIface).EXPECT().AppendUnique("nat", gomock.Any(), gomock.Any()).Do(func(arg1, arg2 interface{}, arg3 ...interface{}) {
		rule := arg1.(string) + " " + arg2.(string)
		for _, arg := range arg3 {
			rule += " " + arg.(string)
		}
		*actualNewRule = append(*actualNewRule, rule)
	}).Return(nil).AnyTimes()
}

func setupDelExpect(ipt iptableswrapper.IPTablesIface, actualClearChain, actualDeleteChain, actualRule *[]string) {
	ipt.(*mock_iptables.MockIPTablesIface).EXPECT().ClearChain("nat", gomock.Any()).Do(func(_, arg2 interface{}) {
		*actualClearChain = append(*actualClearChain, arg2.(string))
	}).Return(nil)

	ipt.(*mock_iptables.MockIPTablesIface).EXPECT().DeleteChain("nat", gomock.Any()).Do(func(_, arg2 interface{}) {
		*actualDeleteChain = append(*actualDeleteChain, arg2.(string))
	}).Return(nil)

	ipt.(*mock_iptables.MockIPTablesIface).EXPECT().Delete("nat", gomock.Any(), gomock.Any()).Do(func(arg1, arg2 interface{}, arg3 ...interface{}) {
		rule := arg1.(string) + " " + arg2.(string)
		for _, arg := range arg3 {
			rule += " " + arg.(string)
		}
		*actualRule = append(*actualRule, rule)
	}).Return(nil).AnyTimes()
}
