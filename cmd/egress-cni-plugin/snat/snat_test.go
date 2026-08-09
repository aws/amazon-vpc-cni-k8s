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
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofrs/flock"
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

type concurrentAddIPTables struct {
	iptableswrapper.IPTablesIface

	mu                        sync.Mutex
	chains                    map[string]bool
	ruleCounts                map[string]int
	listCalls                 int
	activeListCalls           int
	maxConcurrentListCalls    int
	chainExistsCalls          int
	newChainCalls             int
	newChainCollisions        int
	appendUniqueCalls         int
	actualRuleAppendMutations int
}

func newConcurrentAddIPTables() *concurrentAddIPTables {
	return &concurrentAddIPTables{
		chains:     map[string]bool{"POSTROUTING": true},
		ruleCounts: make(map[string]int),
	}
}

func (ipt *concurrentAddIPTables) ListChains(_ string) ([]string, error) {
	ipt.mu.Lock()
	ipt.listCalls++
	ipt.activeListCalls++
	if ipt.activeListCalls > ipt.maxConcurrentListCalls {
		ipt.maxConcurrentListCalls = ipt.activeListCalls
	}
	ipt.mu.Unlock()

	// Keep the transaction open long enough for an unlocked peer to overlap.
	time.Sleep(5 * time.Millisecond)

	ipt.mu.Lock()
	chains := make([]string, 0, len(ipt.chains))
	for chain := range ipt.chains {
		chains = append(chains, chain)
	}
	ipt.activeListCalls--
	ipt.mu.Unlock()
	return chains, nil
}

func (ipt *concurrentAddIPTables) ChainExists(_, chain string) (bool, error) {
	ipt.mu.Lock()
	exists := ipt.chains[chain]
	ipt.chainExistsCalls++
	ipt.mu.Unlock()
	return exists, nil
}

func (ipt *concurrentAddIPTables) NewChain(_, chain string) error {
	ipt.mu.Lock()
	defer ipt.mu.Unlock()
	ipt.newChainCalls++
	if ipt.chains[chain] {
		ipt.newChainCollisions++
		return errors.New("opaque create failure")
	}
	ipt.chains[chain] = true
	return nil
}

func ruleKey(table, chain string, rulespec ...string) string {
	return table + "\x00" + chain + "\x00" + strings.Join(rulespec, "\x00")
}

func (ipt *concurrentAddIPTables) AppendUnique(table, chain string, rulespec ...string) error {
	key := ruleKey(table, chain, rulespec...)
	ipt.mu.Lock()
	ipt.appendUniqueCalls++
	exists := ipt.ruleCounts[key] > 0
	ipt.mu.Unlock()
	if exists {
		return nil
	}

	// Match go-iptables' non-atomic Exists-then-Append implementation.
	time.Sleep(5 * time.Millisecond)

	ipt.mu.Lock()
	ipt.ruleCounts[key]++
	ipt.actualRuleAppendMutations++
	ipt.mu.Unlock()
	return nil
}

func (ipt *concurrentAddIPTables) Delete(table, chain string, rulespec ...string) error {
	key := ruleKey(table, chain, rulespec...)
	ipt.mu.Lock()
	defer ipt.mu.Unlock()
	if ipt.ruleCounts[key] == 0 {
		return &mock_iptables.IptErrNotExists{}
	}
	ipt.ruleCounts[key]--
	if ipt.ruleCounts[key] == 0 {
		delete(ipt.ruleCounts, key)
	}
	return nil
}

func (ipt *concurrentAddIPTables) ClearChain(table, chain string) error {
	ipt.mu.Lock()
	defer ipt.mu.Unlock()
	if !ipt.chains[chain] {
		return &mock_iptables.IptErrNotExists{}
	}
	prefix := table + "\x00" + chain + "\x00"
	for key := range ipt.ruleCounts {
		if strings.HasPrefix(key, prefix) {
			delete(ipt.ruleCounts, key)
		}
	}
	return nil
}

func (ipt *concurrentAddIPTables) DeleteChain(_, chain string) error {
	ipt.mu.Lock()
	defer ipt.mu.Unlock()
	if !ipt.chains[chain] {
		return &mock_iptables.IptErrNotExists{}
	}
	for key := range ipt.ruleCounts {
		parts := strings.Split(key, "\x00")
		if len(parts) >= 2 && parts[1] == chain {
			return errors.New("chain is not empty")
		}
		for i := 2; i+1 < len(parts); i++ {
			if parts[i] == "-j" && parts[i+1] == chain {
				return errors.New("chain is still referenced")
			}
		}
	}
	delete(ipt.chains, chain)
	return nil
}

type testLocker struct {
	lockErr     error
	unlockErr   error
	lockCalls   int
	unlockCalls int
}

func (l *testLocker) Lock() error {
	l.lockCalls++
	return l.lockErr
}

func (l *testLocker) Unlock() error {
	l.unlockCalls++
	return l.unlockErr
}

func TestWithLock(t *testing.T) {
	t.Run("lock error stops action", func(t *testing.T) {
		lockErr := errors.New("lock failed")
		lock := &testLocker{lockErr: lockErr}
		actionCalled := false

		err := withLock(lock, func() error {
			actionCalled = true
			return nil
		})

		assert.ErrorIs(t, err, lockErr)
		assert.False(t, actionCalled)
		assert.Equal(t, 1, lock.lockCalls)
		assert.Zero(t, lock.unlockCalls)
	})

	t.Run("action and unlock errors are preserved", func(t *testing.T) {
		actionErr := errors.New("action failed")
		unlockErr := errors.New("unlock failed")
		lock := &testLocker{unlockErr: unlockErr}

		err := withLock(lock, func() error {
			return actionErr
		})

		assert.ErrorIs(t, err, actionErr)
		assert.ErrorIs(t, err, unlockErr)
		assert.Equal(t, 1, lock.lockCalls)
		assert.Equal(t, 1, lock.unlockCalls)
	})
}

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

	err := Add(ipt, nil, nodeIPv4, containerIPv4, ipv4MulticastRange, chainV4, comment, rndSNAT)
	assert.Nil(t, err)

	assert.EqualValuesf(t, expectChain, actualChain, "iptables chain is expected to be created")

	assert.EqualValuesf(t, expectRule, actualRule, "iptables rules are expected to be created")
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

	err := Del(ipt, nil, containerIPv4, chainV4, comment)
	assert.Nil(t, err)

	assert.EqualValuesf(t, expectClearChain, actualClearChain, "iptables chain is expected to be cleared")

	assert.EqualValuesf(t, expectDeleteChain, actualDeleteChain, "iptables chain is expected to be removed")

	assert.EqualValuesf(t, expectRule, actualRule, "iptables rule is expected to be removed")
}

func TestDelV4_MissingJumpRuleTolerated(t *testing.T) {
	ipt := mock_iptables.NewMockIPTablesIface(gomock.NewController(t))

	ipt.EXPECT().Delete("nat", "POSTROUTING", "-s", containerIPv4.String(), "-j", chainV4, "-m", "comment", "--comment", comment).
		Return(&mock_iptables.IptErrNotExists{})
	ipt.EXPECT().ClearChain("nat", chainV4).Return(nil)
	ipt.EXPECT().DeleteChain("nat", chainV4).Return(nil)

	err := Del(ipt, nil, containerIPv4, chainV4, comment)
	assert.NoError(t, err)
}

func TestDelV4_JumpRuleDeleteErrorPropagates(t *testing.T) {
	ipt := mock_iptables.NewMockIPTablesIface(gomock.NewController(t))
	deleteErr := errors.New("iptables lock timeout")

	ipt.EXPECT().Delete("nat", "POSTROUTING", "-s", containerIPv4.String(), "-j", chainV4, "-m", "comment", "--comment", comment).
		Return(deleteErr)

	err := Del(ipt, nil, containerIPv4, chainV4, comment)
	assert.ErrorIs(t, err, deleteErr)
}

func TestAddV4_NewChainRaceTolerated(t *testing.T) {
	ipt := mock_iptables.NewMockIPTablesIface(gomock.NewController(t))
	createErr := errors.New("opaque create failure")

	ipt.EXPECT().ListChains("nat").Return([]string{"POSTROUTING"}, nil)
	gomock.InOrder(
		ipt.EXPECT().ChainExists("nat", chainV4).Return(false, nil),
		ipt.EXPECT().NewChain("nat", chainV4).Return(createErr),
		ipt.EXPECT().ChainExists("nat", chainV4).Return(true, nil),
	)
	ipt.EXPECT().AppendUnique("nat", gomock.Any(), gomock.Any()).Return(nil).Times(3)

	err := Add(ipt, nil, nodeIPv4, containerIPv4, ipv4MulticastRange, chainV4, comment, rndSNAT)
	assert.NoError(t, err)
}

func TestAddV4_NewChainErrorPropagatesWhenChainAbsent(t *testing.T) {
	ipt := mock_iptables.NewMockIPTablesIface(gomock.NewController(t))
	createErr := errors.New("iptables: Chain already exists.")

	ipt.EXPECT().ListChains("nat").Return([]string{"POSTROUTING"}, nil)
	gomock.InOrder(
		ipt.EXPECT().ChainExists("nat", chainV4).Return(false, nil),
		ipt.EXPECT().NewChain("nat", chainV4).Return(createErr),
		ipt.EXPECT().ChainExists("nat", chainV4).Return(false, nil),
	)
	ipt.EXPECT().AppendUnique(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)

	err := Add(ipt, nil, nodeIPv4, containerIPv4, ipv4MulticastRange, chainV4, comment, rndSNAT)
	assert.ErrorIs(t, err, createErr)
}

func TestAddV4_NewChainVerificationErrorPropagates(t *testing.T) {
	ipt := mock_iptables.NewMockIPTablesIface(gomock.NewController(t))
	createErr := errors.New("opaque create failure")
	verifyErr := errors.New("iptables lock timeout")

	ipt.EXPECT().ListChains("nat").Return([]string{"POSTROUTING"}, nil)
	gomock.InOrder(
		ipt.EXPECT().ChainExists("nat", chainV4).Return(false, nil),
		ipt.EXPECT().NewChain("nat", chainV4).Return(createErr),
		ipt.EXPECT().ChainExists("nat", chainV4).Return(false, verifyErr),
	)
	ipt.EXPECT().AppendUnique(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)

	err := Add(ipt, nil, nodeIPv4, containerIPv4, ipv4MulticastRange, chainV4, comment, rndSNAT)
	assert.ErrorIs(t, err, createErr)
	assert.ErrorIs(t, err, verifyErr)
}

func TestAddV4_ChainExistsErrorPropagates(t *testing.T) {
	ipt := mock_iptables.NewMockIPTablesIface(gomock.NewController(t))
	existsErr := errors.New("iptables lock timeout")

	ipt.EXPECT().ListChains("nat").Return([]string{"POSTROUTING"}, nil)
	ipt.EXPECT().ChainExists("nat", chainV4).Return(false, existsErr).Times(1)
	ipt.EXPECT().NewChain(gomock.Any(), gomock.Any()).Times(0)
	ipt.EXPECT().AppendUnique(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)

	err := Add(ipt, nil, nodeIPv4, containerIPv4, ipv4MulticastRange, chainV4, comment, rndSNAT)
	assert.ErrorIs(t, err, existsErr)
}

func TestAddV4_ChainExistsSkipsCreate(t *testing.T) {
	ipt := mock_iptables.NewMockIPTablesIface(gomock.NewController(t))

	ipt.EXPECT().ListChains("nat").Return([]string{"POSTROUTING"}, nil)
	ipt.EXPECT().ChainExists("nat", chainV4).Return(true, nil).Times(1)
	ipt.EXPECT().NewChain(gomock.Any(), gomock.Any()).Times(0)
	ipt.EXPECT().AppendUnique("nat", gomock.Any(), gomock.Any()).Return(nil).Times(3)

	err := Add(ipt, nil, nodeIPv4, containerIPv4, ipv4MulticastRange, chainV4, comment, rndSNAT)
	assert.NoError(t, err)
}

func TestDelV4_ClearChainNotExistTolerated(t *testing.T) {
	ipt := mock_iptables.NewMockIPTablesIface(gomock.NewController(t))

	ipt.EXPECT().Delete("nat", "POSTROUTING", gomock.Any()).Return(nil)
	ipt.EXPECT().ClearChain("nat", chainV4).Return(
		fmt.Errorf("iptables: No chain/target/match by that name."))
	ipt.EXPECT().DeleteChain("nat", chainV4).Return(nil)

	err := Del(ipt, nil, containerIPv4, chainV4, comment)
	assert.NoError(t, err)
}

func TestDelV4_DeleteChainNotExistTolerated(t *testing.T) {
	ipt := mock_iptables.NewMockIPTablesIface(gomock.NewController(t))

	ipt.EXPECT().Delete("nat", "POSTROUTING", gomock.Any()).Return(nil)
	ipt.EXPECT().ClearChain("nat", chainV4).Return(nil)
	ipt.EXPECT().DeleteChain("nat", chainV4).Return(
		fmt.Errorf("iptables: No chain/target/match by that name."))

	err := Del(ipt, nil, containerIPv4, chainV4, comment)
	assert.NoError(t, err)
}

func TestDelV4_RealErrorPropagates(t *testing.T) {
	ipt := mock_iptables.NewMockIPTablesIface(gomock.NewController(t))
	clearErr := errors.New("iptables lock timeout")

	ipt.EXPECT().Delete("nat", "POSTROUTING", gomock.Any()).Return(nil)
	ipt.EXPECT().ClearChain("nat", chainV4).Return(clearErr)
	ipt.EXPECT().DeleteChain(gomock.Any(), gomock.Any()).Times(0)

	err := Del(ipt, nil, containerIPv4, chainV4, comment)
	assert.ErrorIs(t, err, clearErr)
}

func TestAddV4Concurrent(t *testing.T) {
	const callers = 2
	ipt := newConcurrentAddIPTables()
	lockPath := filepath.Join(t.TempDir(), "egress-cni-snat.lock")
	locks := []Locker{flock.New(lockPath), flock.New(lockPath)}
	errs := make(chan error, callers)
	start := make(chan struct{})

	var wg sync.WaitGroup
	var ready sync.WaitGroup
	ready.Add(callers)
	for i := range callers {
		wg.Add(1)
		go func(lock Locker) {
			defer wg.Done()
			ready.Done()
			<-start
			errs <- Add(ipt, lock, nodeIPv4, containerIPv4, ipv4MulticastRange, chainV4, comment, rndSNAT)
		}(locks[i])
	}
	ready.Wait()
	close(start)
	wg.Wait()
	close(errs)

	for err := range errs {
		assert.NoError(t, err)
	}

	ipt.mu.Lock()
	defer ipt.mu.Unlock()
	assert.Equal(t, callers, ipt.listCalls)
	assert.Equal(t, 1, ipt.maxConcurrentListCalls)
	assert.Equal(t, 1, ipt.chainExistsCalls)
	assert.Equal(t, 1, ipt.newChainCalls)
	assert.Zero(t, ipt.newChainCollisions)
	assert.Equal(t, callers*3, ipt.appendUniqueCalls)
	assert.Equal(t, 3, ipt.actualRuleAppendMutations)
	assert.Len(t, ipt.ruleCounts, 3)
	for _, count := range ipt.ruleCounts {
		assert.Equal(t, 1, count)
	}
	assert.True(t, ipt.chains[chainV4])
}

func TestAddDelV4Concurrent(t *testing.T) {
	const callers = 2
	ipt := newConcurrentAddIPTables()
	lockPath := filepath.Join(t.TempDir(), "egress-cni-snat.lock")

	err := Add(ipt, flock.New(lockPath), nodeIPv4, containerIPv4, ipv4MulticastRange, chainV4, comment, rndSNAT)
	assert.NoError(t, err)

	errs := make(chan error, callers)
	start := make(chan struct{})
	locks := []Locker{flock.New(lockPath), flock.New(lockPath)}

	var wg sync.WaitGroup
	var ready sync.WaitGroup
	ready.Add(callers)

	wg.Add(1)
	go func() {
		defer wg.Done()
		ready.Done()
		<-start
		errs <- Add(ipt, locks[0], nodeIPv4, containerIPv4, ipv4MulticastRange, chainV4, comment, rndSNAT)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		ready.Done()
		<-start
		errs <- Del(ipt, locks[1], containerIPv4, chainV4, comment)
	}()

	ready.Wait()
	close(start)
	wg.Wait()
	close(errs)

	for err := range errs {
		assert.NoError(t, err)
	}

	ipt.mu.Lock()
	defer ipt.mu.Unlock()
	if ipt.chains[chainV4] {
		assert.Len(t, ipt.ruleCounts, 3)
		for _, count := range ipt.ruleCounts {
			assert.Equal(t, 1, count)
		}
	} else {
		assert.Empty(t, ipt.ruleCounts)
	}
}

func setupAddExpect(ipt iptableswrapper.IPTablesIface, actualNewChain, actualNewRule *[]string) {
	ipt.(*mock_iptables.MockIPTablesIface).EXPECT().ListChains("nat").Return(
		[]string{"POSTROUTING"}, nil)

	ipt.(*mock_iptables.MockIPTablesIface).EXPECT().ChainExists("nat", gomock.Any()).Return(false, nil).Times(1)

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
	}).Return(nil).Times(3)
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
	}).Return(nil).Times(1)
}
