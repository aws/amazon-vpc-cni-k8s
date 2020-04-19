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
	"github.com/aws/aws-sdk-go/aws"

	pb "github.com/aws/amazon-vpc-cni-k8s/rpc"

	"github.com/stretchr/testify/assert"
)

func TestServer_AddNetwork(t *testing.T) {
	ctrl, mockAWS, mockK8S, mockCRI, mockNetwork, _ := setup(t)
	defer ctrl.Finish()

	mockContext := &IPAMContext{
		awsClient:     mockAWS,
		k8sClient:     mockK8S,
		maxIPsPerENI:  14,
		maxENI:        4,
		warmENITarget: 1,
		warmIPTarget:  3,
		criClient:     mockCRI,
		networkClient: mockNetwork,
		dataStore:     datastore.NewDataStore(log),
	}

	rpcServer := server{ipamContext: mockContext}

	addNetworkRequest := &pb.AddNetworkRequest{
		Netns:                      "netns",
		K8S_POD_NAME:               "pod",
		K8S_POD_NAMESPACE:          "ns",
		K8S_POD_INFRA_CONTAINER_ID: "cid",
		IfName:                     "eni",
	}

	vpcCIDRs := []*string{aws.String(vpcCIDR)}
	testCases := []struct {
		name               string
		useExternalSNAT    bool
		vpcCIDRs           []*string
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
		mockAWS.EXPECT().GetVPCIPv4CIDRs().Return(tc.vpcCIDRs)
		mockNetwork.EXPECT().UseExternalSNAT().Return(tc.useExternalSNAT)
		if !tc.useExternalSNAT {
			mockNetwork.EXPECT().GetExcludeSNATCIDRs().Return(tc.snatExclusionCIDRs)
		}

		addNetworkReply, err := rpcServer.AddNetwork(context.TODO(), addNetworkRequest)
		assert.NoError(t, err, tc.name)

		assert.Equal(t, tc.useExternalSNAT, addNetworkReply.UseExternalSNAT, tc.name)

		var expectedCIDRs []string
		for _, cidr := range tc.vpcCIDRs {
			expectedCIDRs = append(expectedCIDRs, *cidr)
		}
		expectedCIDRs = append([]string{vpcCIDR}, tc.snatExclusionCIDRs...)
		assert.Equal(t, expectedCIDRs, addNetworkReply.VPCcidrs, tc.name)
	}
}
