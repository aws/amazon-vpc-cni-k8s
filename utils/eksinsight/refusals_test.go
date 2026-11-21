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
				assert.Equal(t, []string{subnetA}, r.Recent())
			} else {
				assert.Empty(t, r.Recent())
			}
		})
	}
}

func TestAnEmptySubnetIDIsNeverRecorded(t *testing.T) {
	r, _ := newTestRefusals()
	r.RefusedIf(apiError("InsufficientCidrBlocks"), "")
	assert.Empty(t, r.Recent())
}

// A refusal must outlast ipamd's 2 minute wait between attempts, so it stays current while a
// shortage lasts, and expire soon after so a fixed subnet is not named for long.
func TestARefusalExpires(t *testing.T) {
	r, clk := newTestRefusals()
	r.RefusedIf(apiError("InsufficientCidrBlocks"), subnetA)

	clk.advance(2 * time.Minute)
	assert.Equal(t, []string{subnetA}, r.Recent(), "still current across ipamd's retry wait")

	clk.advance(refusalTTL)
	assert.Empty(t, r.Recent())
}

// Getting addresses from a subnet proves it has capacity again.
func TestSupplyingClearsARefusal(t *testing.T) {
	r, _ := newTestRefusals()
	r.RefusedIf(apiError("InsufficientCidrBlocks"), subnetA)
	r.RefusedIf(apiError("InsufficientCidrBlocks"), subnetB)
	r.Supplied(subnetA)
	assert.Equal(t, []string{subnetB}, r.Recent())
}

func TestConcurrentRefusalsDoNotRace(t *testing.T) {
	r, _ := newTestRefusals()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(3)
		go func() { defer wg.Done(); r.RefusedIf(apiError("InsufficientCidrBlocks"), subnetA) }()
		go func() { defer wg.Done(); r.Supplied(subnetB) }()
		go func() { defer wg.Done(); r.Recent() }()
	}
	wg.Wait()
}
