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

package ipamd

import (
	"context"
	"testing"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/ipamd/datastore"

	pb "github.com/aws/amazon-vpc-cni-k8s/rpc"

	"github.com/stretchr/testify/assert"
)

func TestServer_VersionCheck(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	mockContext := &IPAMContext{
		awsClient:     m.awsutils,
		maxIPsPerENI:  14,
		maxENI:        4,
		warmENITarget: 1,
		warmIPTarget:  3,
		networkClient: m.network,
		dataStore:     datastore.NewDataStore(log, datastore.NullCheckpoint{}, false),
	}
	m.awsutils.EXPECT().GetVPCIPv4CIDRs().Return([]string{}, nil)
	m.network.EXPECT().UseExternalSNAT().Return(true)

	rpcServer := server{
		version:     "1.2.3",
		ipamContext: mockContext,
	}

	// Happy path

	addReq := &pb.AddNetworkRequest{
		ClientVersion: "1.2.3",
		Netns:         "netns",
		NetworkName:   "net0",
		ContainerID:   "cid",
		IfName:        "eni",
	}

	_, err := rpcServer.AddNetwork(context.TODO(), addReq)
	assert.NoError(t, err)

	delReq := &pb.DelNetworkRequest{
		ClientVersion: "1.2.3",
		NetworkName:   "net0",
		ContainerID:   "cid",
		IfName:        "eni",
	}
	_, err = rpcServer.DelNetwork(context.TODO(), delReq)
	assert.EqualError(t, err, datastore.ErrUnknownPod.Error())

	// Sad path

	addReq.ClientVersion = "1.2.4"
	_, err = rpcServer.AddNetwork(context.TODO(), addReq)
	assert.Error(t, err)

	delReq.ClientVersion = "1.2.4"
	_, err = rpcServer.DelNetwork(context.TODO(), delReq)
	assert.Error(t, err)
}

func TestServer_AddNetwork(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	mockContext := &IPAMContext{
		awsClient:     m.awsutils,
		maxIPsPerENI:  14,
		maxENI:        4,
		warmENITarget: 1,
		warmIPTarget:  3,
		networkClient: m.network,
		dataStore:     datastore.NewDataStore(log, datastore.NullCheckpoint{}, false),
	}

	rpcServer := server{
		version:     "1.2.3",
		ipamContext: mockContext,
	}

	addNetworkRequest := &pb.AddNetworkRequest{
		ClientVersion: "1.2.3",
		Netns:         "netns",
		NetworkName:   "net0",
		ContainerID:   "cid",
		IfName:        "eni",
	}

	vpcCIDRs := []string{vpcCIDR}
	testCases := []struct {
		name               string
		useExternalSNAT    bool
		vpcCIDRs           []string
		snatExclusionCIDRs []string
	}{
		{
			"VPC CIDRs",
			true,
			vpcCIDRs,
			nil,
		},
		{
			"SNAT Exclusion CIDRs",
			false,
			vpcCIDRs,
			[]string{"10.12.0.0/16", "10.13.0.0/16"},
		},
	}
	for _, tc := range testCases {
		m.awsutils.EXPECT().GetVPCIPv4CIDRs().Return(tc.vpcCIDRs, nil)
		m.network.EXPECT().UseExternalSNAT().Return(tc.useExternalSNAT)
		if !tc.useExternalSNAT {
			m.network.EXPECT().GetExcludeSNATCIDRs().Return(tc.snatExclusionCIDRs)
		}

		addNetworkReply, err := rpcServer.AddNetwork(context.TODO(), addNetworkRequest)
		if assert.NoError(t, err, tc.name) {

			assert.Equal(t, tc.useExternalSNAT, addNetworkReply.UseExternalSNAT, tc.name)

			expectedCIDRs := append([]string{vpcCIDR}, tc.snatExclusionCIDRs...)
			assert.Equal(t, expectedCIDRs, addNetworkReply.VPCcidrs, tc.name)
		}
	}
}
