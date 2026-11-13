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

// cardSubnet identifies a refusal that belongs to one network card.
type cardSubnet struct {
	card   int
	subnet string
}

// Refusals records the subnets EC2 refused for lack of addresses or prefixes. Topping up an
// existing ENI asks one card's interface, so those refusals are kept per card. Creating an
// ENI asks subnets shared by the whole node, so those are kept once and are visible to every
// card. It is safe for concurrent use.
type Refusals struct {
	mu       sync.Mutex
	now      func() time.Time
	perCard  map[cardSubnet]time.Time
	nodeWide map[string]time.Time
}

// NewRefusals returns an empty Refusals, reading the clock through now. A nil now means
// time.Now.
func NewRefusals(now func() time.Time) *Refusals {
	if now == nil {
		now = time.Now
	}
	return &Refusals{
		now:      now,
		perCard:  map[cardSubnet]time.Time{},
		nodeWide: map[string]time.Time{},
	}
}

// RefusedCardIf records a refusal that belongs to one network card, which is what topping up
// an existing ENI produces. Does nothing unless err is EC2 reporting a shortage.
func (r *Refusals) RefusedCardIf(err error, card int, subnetID string) {
	if r == nil || subnetID == "" || !isAddressShortage(err) {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.perCard[cardSubnet{card: card, subnet: subnetID}] = r.now()
}

// RefusedIf records a refusal that belongs to the whole node, which is what creating an ENI
// produces: the subnets it tries are shared by every card. Does nothing unless err is EC2
// reporting a shortage.
func (r *Refusals) RefusedIf(err error, subnetID string) {
	if r == nil || subnetID == "" || !isAddressShortage(err) {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nodeWide[subnetID] = r.now()
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

// Supplied records that EC2 did supply addresses or prefixes from this subnet. Capacity is a
// property of the subnet, so this clears the refusal for every card.
func (r *Refusals) Supplied(subnetID string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.nodeWide, subnetID)
	for key := range r.perCard {
		if key.subnet == subnetID {
			delete(r.perCard, key)
		}
	}
}

// Recent returns the subnets refused within the last refusalTTL that this card could be
// suffering from: its own, plus the ones that belong to the whole node. Older entries are
// forgotten.
func (r *Refusals) Recent(card int) []string {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now()
	seen := make(map[string]struct{}, len(r.perCard)+len(r.nodeWide))
	recent := make([]string, 0, len(r.perCard)+len(r.nodeWide))

	for key, at := range r.perCard {
		if now.Sub(at) > refusalTTL {
			delete(r.perCard, key)
			continue
		}
		if key.card != card {
			continue
		}
		if _, dup := seen[key.subnet]; !dup {
			seen[key.subnet] = struct{}{}
			recent = append(recent, key.subnet)
		}
	}
	for subnet, at := range r.nodeWide {
		if now.Sub(at) > refusalTTL {
			delete(r.nodeWide, subnet)
			continue
		}
		if _, dup := seen[subnet]; !dup {
			seen[subnet] = struct{}{}
			recent = append(recent, subnet)
		}
	}
	return recent
}

// Reset drops all recorded refusals. For tests.
func (r *Refusals) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.perCard = map[cardSubnet]time.Time{}
	r.nodeWide = map[string]time.Time{}
}
