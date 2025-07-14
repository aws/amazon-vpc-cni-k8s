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
	"net"
	"testing"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/ipamd/datastore"
	"github.com/aws/amazon-vpc-cni-k8s/rpc"
	multiErr "github.com/hashicorp/go-multierror"

	pb "github.com/aws/amazon-vpc-cni-k8s/rpc"

	"github.com/aws/amazon-vpc-cni-k8s/utils/prometheusmetrics"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

func TestServer_VersionCheck(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	mockContext := &IPAMContext{
		awsClient:       m.awsutils,
		maxIPsPerENI:    14,
		maxENI:          4,
		warmENITarget:   1,
		warmIPTarget:    3,
		networkClient:   m.network,
		dataStoreAccess: datastore.InitializeDataStores([]bool{false}, "test", false, log),
	}

	m.awsutils.EXPECT().GetVPCIPv4CIDRs().Return([]string{}, nil).AnyTimes()
	m.awsutils.EXPECT().GetVPCIPv6CIDRs().Return([]string{}, nil).AnyTimes()
	m.network.EXPECT().UseExternalSNAT().Return(true).AnyTimes()

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
	var expectedErr error
	expectedErr = multiErr.Append(expectedErr, datastore.ErrUnknownPod)
	assert.EqualError(t, err, expectedErr.Error())

	// Sad path

	addReq.ClientVersion = "1.2.4"
	_, err = rpcServer.AddNetwork(context.TODO(), addReq)
	assert.Error(t, err)

	delReq.ClientVersion = "1.2.4"
	_, err = rpcServer.DelNetwork(context.TODO(), delReq)
	assert.Error(t, err)
}

func TestServer_AddNetwork(t *testing.T) {
	maxENIsPerCard := 4
	type getVPCIPv4CIDRsCall struct {
		cidrs []string
		err   error
	}
	type getVPCIPv6CIDRsCall struct {
		cidrs []string
		err   error
	}
	type useExternalSNATCall struct {
		useExternalSNAT bool
	}
	type getExcludeSNATCIDRsCall struct {
		snatExclusionCIDRs []string
	}

	type fields struct {
		managedNetworkCards           int
		ipV4AddressByENIID            map[int]map[string][]string
		ipV6PrefixByENIID             map[int]map[string][]string
		getVPCIPv4CIDRsCalls          []getVPCIPv4CIDRsCall
		getVPCIPv6CIDRsCalls          []getVPCIPv6CIDRsCall
		useExternalSNATCalls          []useExternalSNATCall
		getExcludeSNATCIDRsCalls      []getExcludeSNATCIDRsCall
		ipV4Enabled                   bool
		ipV6Enabled                   bool
		prefixDelegationEnabled       bool
		enableMultiNICSupport         bool
		podsRequireMultiNICAttachment bool
	}
	tests := []struct {
		name    string
		fields  fields
		want    *pb.AddNetworkReply
		wantErr error
	}{
		{
			name: "successfully allocated IPv4Address & use externalSNAT",
			fields: fields{
				managedNetworkCards: 1,
				ipV4AddressByENIID:  map[int]map[string][]string{0: {"eni-1": {"192.168.1.100"}}},
				getVPCIPv4CIDRsCalls: []getVPCIPv4CIDRsCall{
					{
						cidrs: []string{"10.10.0.0/16"},
					},
				},
				useExternalSNATCalls: []useExternalSNATCall{
					{
						useExternalSNAT: true,
					},
				},
				ipV4Enabled: true,
				ipV6Enabled: false,
			},
			want: &pb.AddNetworkReply{
				Success: true,
				IPAllocationMetadata: []*rpc.IPAllocationMetadata{
					{
						IPv4Addr:     "192.168.1.100",
						IPv6Addr:     "",
						DeviceNumber: int32(0),
						RouteTableId: int32(1),
					},
				},
				UseExternalSNAT: true,
				VPCv4CIDRs:      []string{"10.10.0.0/16"},
			},
		},
		{
			name: "successfully allocated IPv4Address for multiple NIC pods",
			fields: fields{
				managedNetworkCards: 2,
				ipV4AddressByENIID:  map[int]map[string][]string{0: {"eni-1": {"192.168.1.100"}}, 1: {"eni-2": {"192.168.16.100"}}},
				getVPCIPv4CIDRsCalls: []getVPCIPv4CIDRsCall{
					{
						cidrs: []string{"10.10.0.0/16"},
					},
				},
				useExternalSNATCalls: []useExternalSNATCall{
					{
						useExternalSNAT: false,
					},
				},
				getExcludeSNATCIDRsCalls: []getExcludeSNATCIDRsCall{
					{
						snatExclusionCIDRs: []string{"10.12.0.0/16", "10.13.0.0/16"},
					},
				},
				ipV4Enabled:                   true,
				ipV6Enabled:                   false,
				enableMultiNICSupport:         true,
				podsRequireMultiNICAttachment: true,
			},
			want: &pb.AddNetworkReply{
				Success: true,
				IPAllocationMetadata: []*rpc.IPAllocationMetadata{
					{
						IPv4Addr:     "192.168.1.100",
						IPv6Addr:     "",
						DeviceNumber: int32(0),
						RouteTableId: int32(1),
					},
					{
						IPv4Addr:     "192.168.16.100",
						IPv6Addr:     "",
						DeviceNumber: int32(0),
						RouteTableId: int32(1*0 + maxENIsPerCard + 1), // Network Card * DeviceNumber + Max ENIs per card + 1
					},
				},
				UseExternalSNAT: false,
				VPCv4CIDRs:      []string{"10.10.0.0/16", "10.12.0.0/16", "10.13.0.0/16"},
			},
		},
		{
			name: "successfully allocated IPv4Address & not use externalSNAT",
			fields: fields{
				managedNetworkCards: 1,
				ipV4AddressByENIID: map[int]map[string][]string{
					0: {"eni-1": {"192.168.1.100"}},
				},
				getVPCIPv4CIDRsCalls: []getVPCIPv4CIDRsCall{
					{
						cidrs: []string{"10.10.0.0/16"},
					},
				},
				useExternalSNATCalls: []useExternalSNATCall{
					{
						useExternalSNAT: false,
					},
				},
				getExcludeSNATCIDRsCalls: []getExcludeSNATCIDRsCall{
					{
						snatExclusionCIDRs: []string{"10.12.0.0/16", "10.13.0.0/16"},
					},
				},
				ipV4Enabled:                   true,
				ipV6Enabled:                   false,
				enableMultiNICSupport:         true,
				podsRequireMultiNICAttachment: false,
			},
			want: &pb.AddNetworkReply{
				Success: true,
				IPAllocationMetadata: []*rpc.IPAllocationMetadata{
					{
						IPv4Addr:     "192.168.1.100",
						IPv6Addr:     "",
						DeviceNumber: int32(0),
						RouteTableId: int32(1),
					},
				},
				UseExternalSNAT: false,
				VPCv4CIDRs:      []string{"10.10.0.0/16", "10.12.0.0/16", "10.13.0.0/16"},
			},
		},
		{
			name: "failed allocating IPv4Address",
			fields: fields{
				managedNetworkCards: 1,
				ipV4AddressByENIID:  map[int]map[string][]string{},
				ipV4Enabled:         true,
				ipV6Enabled:         false,
			},
			want: &pb.AddNetworkReply{
				Success:              false,
				IPAllocationMetadata: []*rpc.IPAllocationMetadata{},
			},
		},
		{
			name: "failed allocating IPv4Address from any datastore - no IPs available",
			fields: fields{
				managedNetworkCards:   2,
				ipV4AddressByENIID:    map[int]map[string][]string{0: {}, 1: {}},
				ipV4Enabled:           true,
				ipV6Enabled:           false,
				enableMultiNICSupport: true,
			},
			want: &pb.AddNetworkReply{
				Success:              false,
				IPAllocationMetadata: []*rpc.IPAllocationMetadata{},
			},
		},
		{
			name: "Pods require multinic attachment but multinic is not enabled",
			fields: fields{
				managedNetworkCards:           1,
				ipV4AddressByENIID:            map[int]map[string][]string{0: {}},
				ipV4Enabled:                   true,
				ipV6Enabled:                   false,
				enableMultiNICSupport:         false,
				podsRequireMultiNICAttachment: true,
			},
			want: &pb.AddNetworkReply{
				Success:              false,
				IPAllocationMetadata: []*rpc.IPAllocationMetadata{},
			},
		},
		{
			name: "failed allocating IPv4Address from first datastore but second datastore has IPs",
			fields: fields{
				managedNetworkCards: 2,
				ipV4AddressByENIID: map[int]map[string][]string{
					0: {},
					1: {"eni-1": {"192.168.1.100"}},
				},
				getVPCIPv4CIDRsCalls: []getVPCIPv4CIDRsCall{
					{
						cidrs: []string{"10.10.0.0/16"},
					},
				},
				useExternalSNATCalls: []useExternalSNATCall{
					{
						useExternalSNAT: true,
					},
				},
				ipV4Enabled:           true,
				ipV6Enabled:           false,
				enableMultiNICSupport: true,
			},

			want: &pb.AddNetworkReply{
				Success: true,
				IPAllocationMetadata: []*rpc.IPAllocationMetadata{
					{
						IPv4Addr:     "192.168.1.100",
						IPv6Addr:     "",
						DeviceNumber: int32(0),
						RouteTableId: int32(1*0 + maxENIsPerCard + 1), // Network Card * DeviceNumber + Max ENIs per card + 1
					},
				},
				UseExternalSNAT: true,
				VPCv4CIDRs:      []string{"10.10.0.0/16"},
			},
		},
		{
			name: "failed allocating IPv4Address from first datastore and pod requires multi-nic attachment",
			fields: fields{
				managedNetworkCards: 2,
				ipV4AddressByENIID: map[int]map[string][]string{
					0: {},
					1: {"eni-1": {"192.168.1.100"}},
				},
				ipV4Enabled:                   true,
				ipV6Enabled:                   false,
				enableMultiNICSupport:         true,
				podsRequireMultiNICAttachment: true,
			},

			want: &pb.AddNetworkReply{
				Success:              false,
				IPAllocationMetadata: []*rpc.IPAllocationMetadata{},
			},
		},

		{
			name: "successfully allocated IPv6Address in PD mode",
			fields: fields{
				managedNetworkCards: 1,
				ipV6PrefixByENIID: map[int]map[string][]string{0: {
					"eni-1": {"2001:db8::/64"}},
				},
				getVPCIPv6CIDRsCalls: []getVPCIPv6CIDRsCall{
					{
						cidrs: []string{"2001:db8::/56"},
					},
				},
				ipV4Enabled:             false,
				ipV6Enabled:             true,
				prefixDelegationEnabled: true,
			},
			want: &pb.AddNetworkReply{
				Success: true,
				IPAllocationMetadata: []*rpc.IPAllocationMetadata{
					{
						IPv4Addr:     "",
						IPv6Addr:     "2001:db8::",
						DeviceNumber: int32(0),
						RouteTableId: int32(1),
					},
				},
				VPCv6CIDRs: []string{"2001:db8::/56"},
			},
		},
		{
			name: "successfully allocated IPv6Address multinic pods",
			fields: fields{
				managedNetworkCards: 2,
				ipV6PrefixByENIID: map[int]map[string][]string{0: {
					"eni-1": {"2001:db8::/64"}},
					1: {"eni-2": {"2001:db8:0:01::/64"}},
				},
				getVPCIPv6CIDRsCalls: []getVPCIPv6CIDRsCall{
					{
						cidrs: []string{"2001:db8::/56"},
					},
				},
				ipV4Enabled:                   false,
				ipV6Enabled:                   true,
				prefixDelegationEnabled:       true,
				enableMultiNICSupport:         true,
				podsRequireMultiNICAttachment: true,
			},
			want: &pb.AddNetworkReply{
				Success: true,
				IPAllocationMetadata: []*rpc.IPAllocationMetadata{
					{
						IPv4Addr:     "",
						IPv6Addr:     "2001:db8::",
						DeviceNumber: int32(0),
						RouteTableId: int32(1),
					},
					{
						IPv4Addr:     "",
						IPv6Addr:     "2001:db8:0:1::",
						DeviceNumber: int32(0),
						RouteTableId: int32(1*0 + maxENIsPerCard + 1),
					},
				},
				VPCv6CIDRs: []string{"2001:db8::/56"},
			},
		},
		{
			name: "failed allocating IPv6Address - No IP addresses available",
			fields: fields{
				managedNetworkCards:     1,
				ipV6PrefixByENIID:       map[int]map[string][]string{},
				ipV4Enabled:             true,
				ipV6Enabled:             false,
				prefixDelegationEnabled: true,
			},
			want: &pb.AddNetworkReply{
				Success:              false,
				IPAllocationMetadata: []*rpc.IPAllocationMetadata{},
			},
		},
		{
			name: "failed allocating IPv6Address - PD disabled",
			fields: fields{
				managedNetworkCards: 1,
				ipV6PrefixByENIID: map[int]map[string][]string{0: {
					"eni-1": {"2001:db8::/64"}},
				},
				ipV4Enabled:             false,
				ipV6Enabled:             true,
				prefixDelegationEnabled: false,
			},
			want: &pb.AddNetworkReply{
				Success:              false,
				IPAllocationMetadata: []*rpc.IPAllocationMetadata{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset the counter for each test case
			prometheusmetrics.AddIPCnt = prometheus.NewCounter(prometheus.CounterOpts{
				Name: "awscni_add_ip_req_count",
				Help: "Number of add IP address requests",
			})

			m := setup(t)
			defer m.ctrl.Finish()

			for _, call := range tt.fields.getVPCIPv4CIDRsCalls {
				m.awsutils.EXPECT().GetVPCIPv4CIDRs().Return(call.cidrs, call.err)
			}
			for _, call := range tt.fields.getVPCIPv6CIDRsCalls {
				m.awsutils.EXPECT().GetVPCIPv6CIDRs().Return(call.cidrs, call.err)
			}
			for _, call := range tt.fields.useExternalSNATCalls {
				m.network.EXPECT().UseExternalSNAT().Return(call.useExternalSNAT)
			}
			for _, call := range tt.fields.getExcludeSNATCIDRsCalls {
				m.network.EXPECT().GetExcludeSNATCIDRs().Return(call.snatExclusionCIDRs)
			}
			var dsList []*datastore.DataStore
			for i := 0; i < tt.fields.managedNetworkCards; i++ {
				dsList = append(dsList, datastore.NewDataStore(log, datastore.NullCheckpoint{}, tt.fields.prefixDelegationEnabled, i))
			}

			dsAccess := &datastore.DataStoreAccess{DataStores: dsList}

			for networkCard, eniMap := range tt.fields.ipV4AddressByENIID {
				for eniID, ipv4Addresses := range eniMap {
					dsAccess.GetDataStore(networkCard).AddENI(eniID, 0, false, false, false)
					for _, ipv4Address := range ipv4Addresses {
						ipv4Addr := net.IPNet{IP: net.ParseIP(ipv4Address), Mask: net.IPv4Mask(255, 255, 255, 255)}
						dsAccess.GetDataStore(networkCard).AddIPv4CidrToStore(eniID, ipv4Addr, false)
					}
				}
			}
			for networkCard, eniMap := range tt.fields.ipV6PrefixByENIID {
				for eniID, ipv6Prefixes := range eniMap {
					dsAccess.GetDataStore(networkCard).AddENI(eniID, 0, false, false, false)
					for _, ipv6Prefix := range ipv6Prefixes {
						_, ipnet, _ := net.ParseCIDR(ipv6Prefix)
						dsAccess.GetDataStore(networkCard).AddIPv6CidrToStore(eniID, *ipnet, true)
					}
				}
			}

			mockContext := &IPAMContext{
				awsClient:              m.awsutils,
				maxIPsPerENI:           14,
				maxENI:                 maxENIsPerCard,
				warmENITarget:          1,
				warmIPTarget:           3,
				networkClient:          m.network,
				enableIPv4:             tt.fields.ipV4Enabled,
				enableIPv6:             tt.fields.ipV6Enabled,
				enablePrefixDelegation: tt.fields.prefixDelegationEnabled,
				dataStoreAccess:        dsAccess,
				enableMultiNICSupport:  tt.fields.enableMultiNICSupport,
			}

			s := &server{
				version:     "1.2.3",
				ipamContext: mockContext,
			}

			req := &pb.AddNetworkRequest{
				ClientVersion:              "1.2.3",
				Netns:                      "netns",
				NetworkName:                "net0",
				ContainerID:                "cid",
				IfName:                     "eni",
				RequiresMultiNICAttachment: tt.fields.podsRequireMultiNICAttachment,
			}

			resp, err := s.AddNetwork(context.Background(), req)
			if tt.wantErr != nil {
				assert.EqualError(t, err, tt.wantErr.Error())
			} else {
				assert.NoError(t, err)
				for index, _ := range tt.want.IPAllocationMetadata {
					assert.Equal(t, tt.want.IPAllocationMetadata[index].IPv4Addr, resp.IPAllocationMetadata[index].IPv4Addr)
					assert.Equal(t, tt.want.IPAllocationMetadata[index].IPv6Addr, resp.IPAllocationMetadata[index].IPv6Addr)
					assert.Equal(t, tt.want.IPAllocationMetadata[index].DeviceNumber, resp.IPAllocationMetadata[index].DeviceNumber)
					assert.Equal(t, tt.want.IPAllocationMetadata[index].RouteTableId, resp.IPAllocationMetadata[index].RouteTableId)
				}

				assert.Equal(t, tt.want.UseExternalSNAT, resp.UseExternalSNAT)
				assert.Equal(t, tt.want.VPCv4CIDRs, resp.VPCv4CIDRs)
				assert.Equal(t, tt.want.VPCv6CIDRs, resp.VPCv6CIDRs)
				assert.Equal(t, tt.want.Success, resp.Success)
			}

			// Add more detailed assertion messages
			assert.Equal(t, float64(1), testutil.ToFloat64(prometheusmetrics.AddIPCnt),
				"AddIPCnt should be incremented exactly once for test case: %s", tt.name)
		})
	}
}
