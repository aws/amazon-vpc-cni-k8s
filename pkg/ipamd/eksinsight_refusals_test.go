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

package ipamd

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/smithy-go"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/ipamd/datastore"
)

// The pool manager tops up existing ENIs far more often than it creates new ones, so these
// two writers produce most of the refusals the metric ever sees.

// shortage is EC2 refusing because the subnet has nothing left to give.
func shortage(code string) error {
	return &smithy.GenericAPIError{Code: code, Message: "test"}
}

// topUpContext builds a context whose one card holds a single ENI with room to grow, and
// returns the id of that ENI.
func topUpContext(t *testing.T, m *testMocks, pd bool) (*IPAMContext, string) {
	t.Helper()
	ds, eniID := dsWith(t, 0, subnetA, 0), "eni-0"
	if pd {
		ds, eniID = dsWithPrefixes(t, 0, subnetA, 0), "eni-pd-0"
	}
	ctx := &IPAMContext{
		awsClient:              m.awsutils,
		dataStoreAccess:        &datastore.DataStoreAccess{DataStores: []*datastore.DataStore{ds}},
		maxIPsPerENI:           9,
		maxPrefixesPerENI:      1,
		maxENI:                 3,
		enablePrefixDelegation: pd,
	}
	withInsights(ctx)
	return ctx, eniID
}

func TestTryAssignIPsNotesARefusedSubnet(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	ctx, eniID := topUpContext(t, m, false)
	// The batch is refused, then the retry for a single address is refused too. Only the
	// second failure is terminal, so only then is the subnet noted.
	m.awsutils.EXPECT().AllocIPAddresses(gomock.Any(), eniID, 9).
		Return(nil, shortage("InsufficientFreeAddressesInSubnet"))
	m.awsutils.EXPECT().AllocIPAddresses(gomock.Any(), eniID, 1).
		Return(nil, shortage("InsufficientFreeAddressesInSubnet"))

	increased, err := ctx.tryAssignIPs(context.Background(), 0)
	require.Error(t, err)
	assert.False(t, increased)
	assert.Equal(t, []string{subnetA}, ctx.eksRefusals.Recent(0))
	assert.Empty(t, ctx.eksRefusals.Recent(1),
		"topping up card 0's ENI says nothing about card 1")
}

func TestTryAssignIPsClearsTheNoteWhenEC2Supplies(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	ctx, eniID := topUpContext(t, m, false)
	ctx.eksRefusals.RefusedCardIf(shortage("InsufficientFreeAddressesInSubnet"), 0, subnetA)
	require.Equal(t, []string{subnetA}, ctx.eksRefusals.Recent(0))

	m.awsutils.EXPECT().AllocIPAddresses(gomock.Any(), eniID, 9).Return(
		&ec2.AssignPrivateIpAddressesOutput{
			AssignedPrivateIpAddresses: []ec2types.AssignedPrivateIpAddress{
				{PrivateIpAddress: aws.String("10.0.0.1")},
			},
		}, nil)

	increased, err := ctx.tryAssignIPs(context.Background(), 0)
	require.NoError(t, err)
	assert.True(t, increased)
	assert.Empty(t, ctx.eksRefusals.Recent(0), "the subnet supplied an address, so it has capacity")
}

// Prefix delegation asks EC2 for prefixes rather than addresses, so it is a separate writer
// with its own error code.
func TestTryAssignPrefixesNotesARefusedSubnet(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	ctx, eniID := topUpContext(t, m, true)
	m.awsutils.EXPECT().AllocIPAddresses(gomock.Any(), eniID, 1).
		Return(nil, shortage("InsufficientCidrBlocks")).Times(2)

	increased, err := ctx.tryAssignPrefixes(context.Background(), 0)
	require.Error(t, err)
	assert.False(t, increased)
	assert.Equal(t, []string{subnetA}, ctx.eksRefusals.Recent(0))
	assert.Empty(t, ctx.eksRefusals.Recent(1),
		"topping up card 0's ENI says nothing about card 1")
}

func TestTryAssignPrefixesClearsTheNoteWhenEC2Supplies(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	ctx, eniID := topUpContext(t, m, true)
	ctx.eksRefusals.RefusedCardIf(shortage("InsufficientCidrBlocks"), 0, subnetA)
	require.Equal(t, []string{subnetA}, ctx.eksRefusals.Recent(0))

	m.awsutils.EXPECT().AllocIPAddresses(gomock.Any(), eniID, 1).Return(
		&ec2.AssignPrivateIpAddressesOutput{
			AssignedIpv4Prefixes: []ec2types.Ipv4PrefixSpecification{
				{Ipv4Prefix: aws.String("10.0.1.0/28")},
			},
		}, nil)

	increased, err := ctx.tryAssignPrefixes(context.Background(), 0)
	require.NoError(t, err)
	assert.True(t, increased)
	assert.Empty(t, ctx.eksRefusals.Recent(0))
}

// Throttling and permission errors say nothing about the subnet, so neither writer may note
// one on their account.
func TestTopUpDoesNotNoteNonShortageErrors(t *testing.T) {
	for _, code := range []string{"RequestLimitExceeded", "UnauthorizedOperation"} {
		t.Run(code, func(t *testing.T) {
			m := setup(t)
			defer m.ctrl.Finish()

			ctx, eniID := topUpContext(t, m, false)
			m.awsutils.EXPECT().AllocIPAddresses(gomock.Any(), eniID, 9).Return(nil, shortage(code))
			m.awsutils.EXPECT().AllocIPAddresses(gomock.Any(), eniID, 1).Return(nil, shortage(code))

			_, err := ctx.tryAssignIPs(context.Background(), 0)
			require.Error(t, err)
			assert.Empty(t, ctx.eksRefusals.Recent(0))
		})
	}
}
