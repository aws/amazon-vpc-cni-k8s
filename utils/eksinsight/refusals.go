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
	"time"

	"github.com/aws/smithy-go"
)

// refusalTTL is how long a refusal stays usable. ipamd waits 2 minutes after an insufficient
// addresses error before asking that subnet again, so this must outlast that wait to stay
// fresh during a shortage.
const refusalTTL = 150 * time.Second

// The EC2 error codes that mean a subnet could not supply what was asked of it. Which one
// comes back depends on whether addresses or prefixes were requested.
const (
	insufficientCidrBlocks    = "InsufficientCidrBlocks"
	insufficientFreeAddresses = "InsufficientFreeAddressesInSubnet"
)

// Refused is the process-wide record of subnets EC2 refused. The pool manager writes to it
// and the pod ADD path reads it.
var Refused = NewRefusals(time.Now)

// Refusals records the subnets EC2 refused for lack of addresses or prefixes. Keyed by
// subnet, since a subnet's capacity is a property of the subnet rather than of the network
// card that asked. It is safe for concurrent use.
type Refusals struct {
	mu  sync.Mutex
	now func() time.Time
	at  map[string]time.Time
}

// NewRefusals returns an empty Refusals, reading the clock through now. A nil now means
// time.Now.
func NewRefusals(now func() time.Time) *Refusals {
	if now == nil {
		now = time.Now
	}
	return &Refusals{now: now, at: map[string]time.Time{}}
}

// RefusedIf records a refusal when err is EC2 reporting that the subnet could not supply
// addresses or prefixes, and does nothing for any other error.
func (r *Refusals) RefusedIf(err error, subnetID string) {
	if subnetID == "" || !isAddressShortage(err) {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.at[subnetID] = r.now()
}

// isAddressShortage reports whether err is EC2 refusing for lack of addresses or prefixes.
func isAddressShortage(err error) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	code := apiErr.ErrorCode()
	return code == insufficientCidrBlocks || code == insufficientFreeAddresses
}

// Supplied records that EC2 did supply addresses or prefixes from this subnet, which clears
// any earlier refusal.
func (r *Refusals) Supplied(subnetID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.at, subnetID)
}

// Recent returns the subnets refused within the last refusalTTL, forgetting older ones.
func (r *Refusals) Recent() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now()
	recent := make([]string, 0, len(r.at))
	for subnet, at := range r.at {
		if now.Sub(at) > refusalTTL {
			delete(r.at, subnet)
			continue
		}
		recent = append(recent, subnet)
	}
	return recent
}

// Reset drops all recorded refusals. For tests.
func (r *Refusals) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.at = map[string]time.Time{}
}
