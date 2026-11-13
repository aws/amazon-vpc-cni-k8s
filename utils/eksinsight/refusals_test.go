// Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//      http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package eksinsight

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/aws/smithy-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// apiError is an EC2 API error with the given code.
func apiError(code string) error {
	return &smithy.GenericAPIError{Code: code, Message: "test"}
}

func newTestRefusals() (*Refusals, *fakeClock) {
	clk := &fakeClock{t: time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)}
	return NewRefusals(clk.now), clk
}

// Only an address or prefix shortage is a subnet's fault. Throttling and permission errors
// say nothing about the subnet.
func TestOnlyAddressShortagesAreRecorded(t *testing.T) {
	for _, tc := range []struct {
		name     string
		err      error
		recorded bool
	}{
		{"insufficient addresses", apiError("InsufficientFreeAddressesInSubnet"), true},
		{"insufficient prefixes", apiError("InsufficientCidrBlocks"), true},
		{"throttled", apiError("RequestLimitExceeded"), false},
		{"no permission", apiError("UnauthorizedOperation"), false},
		{"not an API error", errors.New("boom"), false},
		{"no error", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := newTestRefusals()
			r.RefusedIf(tc.err, subnetA)
			if tc.recorded {
				assert.Equal(t, []string{subnetA}, r.Recent(0))
			} else {
				assert.Empty(t, r.Recent(0))
			}
		})
	}
}

func TestAnEmptySubnetIDIsNeverRecorded(t *testing.T) {
	r, _ := newTestRefusals()
	r.RefusedIf(apiError("InsufficientCidrBlocks"), "")
	assert.Empty(t, r.Recent(0))
}

// A refusal must outlast ipamd's 2 minute wait between attempts, so it stays current while a
// shortage lasts, and expire soon after so a fixed subnet is not named for long.
func TestARefusalExpires(t *testing.T) {
	r, clk := newTestRefusals()
	r.RefusedIf(apiError("InsufficientCidrBlocks"), subnetA)

	clk.advance(2 * time.Minute)
	assert.Equal(t, []string{subnetA}, r.Recent(0), "still current across ipamd's retry wait")

	clk.advance(refusalTTL)
	assert.Empty(t, r.Recent(0))
}

// Getting addresses from a subnet proves it has capacity again.
func TestSupplyingClearsARefusal(t *testing.T) {
	r, _ := newTestRefusals()
	r.RefusedIf(apiError("InsufficientCidrBlocks"), subnetA)
	r.RefusedIf(apiError("InsufficientCidrBlocks"), subnetB)
	r.Supplied(subnetA)
	assert.Equal(t, []string{subnetB}, r.Recent(0))
}

// A cache built without a recorder passes nil, so every method must tolerate it. That is
// what keeps the awsutils tests that do not care about refusals working untouched.
func TestANilRefusalsIsANoOp(t *testing.T) {
	var r *Refusals
	assert.NotPanics(t, func() { r.RefusedIf(apiError("InsufficientCidrBlocks"), subnetA) })
	assert.NotPanics(t, func() { r.Supplied(subnetA) })
	assert.NotPanics(t, func() { assert.Nil(t, r.Recent(0)) })
}

// A refusal earned by one card belongs to that card alone.
func TestACardRefusalIsPrivateToThatCard(t *testing.T) {
	r, _ := newTestRefusals()
	r.RefusedCardIf(apiError("InsufficientFreeAddressesInSubnet"), 0, subnetA)

	assert.Equal(t, []string{subnetA}, r.Recent(0))
	assert.Empty(t, r.Recent(1))
}

// A refusal from creating an ENI is about the subnet, so every card sees it.
func TestANodeWideRefusalIsVisibleToEveryCard(t *testing.T) {
	r, _ := newTestRefusals()
	r.RefusedIf(apiError("InsufficientCidrBlocks"), subnetA)

	assert.Equal(t, []string{subnetA}, r.Recent(0))
	assert.Equal(t, []string{subnetA}, r.Recent(7))
}

// A subnet refused both ways is reported once.
func TestASubnetRefusedBothWaysIsNotDuplicated(t *testing.T) {
	r, _ := newTestRefusals()
	r.RefusedCardIf(apiError("InsufficientCidrBlocks"), 0, subnetA)
	r.RefusedIf(apiError("InsufficientCidrBlocks"), subnetA)

	assert.Equal(t, []string{subnetA}, r.Recent(0))
}

// Capacity is a property of the subnet, so supplying clears it for every card and both kinds
// of refusal.
func TestSupplyingClearsTheSubnetForEveryCard(t *testing.T) {
	r, _ := newTestRefusals()
	r.RefusedCardIf(apiError("InsufficientCidrBlocks"), 0, subnetA)
	r.RefusedCardIf(apiError("InsufficientCidrBlocks"), 1, subnetA)
	r.RefusedIf(apiError("InsufficientCidrBlocks"), subnetA)
	r.RefusedCardIf(apiError("InsufficientCidrBlocks"), 0, subnetB)

	r.Supplied(subnetA)

	assert.Equal(t, []string{subnetB}, r.Recent(0))
	assert.Empty(t, r.Recent(1))
}

// Both kinds of refusal expire on the same TTL.
func TestBothKindsOfRefusalExpire(t *testing.T) {
	r, clk := newTestRefusals()
	r.RefusedCardIf(apiError("InsufficientCidrBlocks"), 0, subnetA)
	r.RefusedIf(apiError("InsufficientCidrBlocks"), subnetB)
	require.ElementsMatch(t, []string{subnetA, subnetB}, r.Recent(0))

	clk.advance(refusalTTL + time.Second)
	assert.Empty(t, r.Recent(0))
}

func TestConcurrentRefusalsDoNotRace(t *testing.T) {
	r, _ := newTestRefusals()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(3)
		go func() { defer wg.Done(); r.RefusedIf(apiError("InsufficientCidrBlocks"), subnetA) }()
		go func() { defer wg.Done(); r.Supplied(subnetB) }()
		go func() { defer wg.Done(); r.Recent(0) }()
	}
	wg.Wait()
}
