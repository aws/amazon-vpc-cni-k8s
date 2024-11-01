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
		awsClient:     m.awsutils,
		maxIPsPerENI:  14,
		maxENI:        4,
		warmENITarget: 1,
		warmIPTarget:  3,
		networkClient: m.network,
		dataStore:     datastore.NewDataStore(log, datastore.NullCheckpoint{}, false),
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
		ipV4AddressByENIID       map[string][]string
		ipV6PrefixByENIID        map[string][]string
		getVPCIPv4CIDRsCalls     []getVPCIPv4CIDRsCall
		getVPCIPv6CIDRsCalls     []getVPCIPv6CIDRsCall
		useExternalSNATCalls     []useExternalSNATCall
		getExcludeSNATCIDRsCalls []getExcludeSNATCIDRsCall
		ipV4Enabled              bool
		ipV6Enabled              bool
		prefixDelegationEnabled  bool
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
				ipV4AddressByENIID: map[string][]string{
					"eni-1": {"192.168.1.100"},
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
				ipV4Enabled: true,
				ipV6Enabled: false,
			},
			want: &pb.AddNetworkReply{
				Success:         true,
				IPv4Addr:        "192.168.1.100",
				DeviceNumber:    int32(0),
				UseExternalSNAT: true,
				VPCv4CIDRs:      []string{"10.10.0.0/16"},
			},
		},
		{
			name: "successfully allocated IPv4Address & not use externalSNAT",
			fields: fields{
				ipV4AddressByENIID: map[string][]string{
					"eni-1": {"192.168.1.100"},
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
				ipV4Enabled: true,
				ipV6Enabled: false,
			},
			want: &pb.AddNetworkReply{
				Success:         true,
				IPv4Addr:        "192.168.1.100",
				DeviceNumber:    int32(0),
				UseExternalSNAT: false,
				VPCv4CIDRs:      []string{"10.10.0.0/16", "10.12.0.0/16", "10.13.0.0/16"},
			},
		},
		{
			name: "failed allocating IPv4Address ",
			fields: fields{
				ipV4AddressByENIID: map[string][]string{},
				ipV4Enabled:        true,
				ipV6Enabled:        false,
			},
			want: &pb.AddNetworkReply{
				Success:      false,
				DeviceNumber: int32(-1),
			},
		},
		{
			name: "successfully allocated IPv6Address in PD mode",
			fields: fields{
				ipV6PrefixByENIID: map[string][]string{
					"eni-1": {"2001:db8::/64"},
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
				Success:      true,
				IPv6Addr:     "2001:db8::",
				DeviceNumber: int32(0),
				VPCv6CIDRs:   []string{"2001:db8::/56"},
			},
		},
		{
			name: "failed allocating IPv6Address - No IP addresses available",
			fields: fields{
				ipV6PrefixByENIID:       map[string][]string{},
				ipV4Enabled:             true,
				ipV6Enabled:             false,
				prefixDelegationEnabled: true,
			},
			want: &pb.AddNetworkReply{
				Success:      false,
				DeviceNumber: int32(-1),
			},
		},
		{
			name: "failed allocating IPv6Address - PD disabled",
			fields: fields{
				ipV6PrefixByENIID: map[string][]string{
					"eni-1": {"2001:db8::/64"},
				},
				ipV4Enabled:             false,
				ipV6Enabled:             true,
				prefixDelegationEnabled: false,
			},
			want: &pb.AddNetworkReply{
				Success:      false,
				IPv6Addr:     "",
				DeviceNumber: int32(-1),
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
			ds := datastore.NewDataStore(log, datastore.NullCheckpoint{}, tt.fields.prefixDelegationEnabled)
			for eniID, ipv4Addresses := range tt.fields.ipV4AddressByENIID {
				ds.AddENI(eniID, 0, false, false, false)
				for _, ipv4Address := range ipv4Addresses {
					ipv4Addr := net.IPNet{IP: net.ParseIP(ipv4Address), Mask: net.IPv4Mask(255, 255, 255, 255)}
					ds.AddIPv4CidrToStore(eniID, ipv4Addr, false)
				}
			}
			for eniID, ipv6Prefixes := range tt.fields.ipV6PrefixByENIID {
				ds.AddENI(eniID, 0, false, false, false)
				for _, ipv6Prefix := range ipv6Prefixes {
					_, ipnet, _ := net.ParseCIDR(ipv6Prefix)
					ds.AddIPv6CidrToStore(eniID, *ipnet, true)
				}
			}

			mockContext := &IPAMContext{
				awsClient:              m.awsutils,
				maxIPsPerENI:           14,
				maxENI:                 4,
				warmENITarget:          1,
				warmIPTarget:           3,
				networkClient:          m.network,
				enableIPv4:             tt.fields.ipV4Enabled,
				enableIPv6:             tt.fields.ipV6Enabled,
				enablePrefixDelegation: tt.fields.prefixDelegationEnabled,
				dataStore:              ds,
			}

			s := &server{
				version:     "1.2.3",
				ipamContext: mockContext,
			}

			req := &pb.AddNetworkRequest{
				ClientVersion: "1.2.3",
				Netns:         "netns",
				NetworkName:   "net0",
				ContainerID:   "cid",
				IfName:        "eni",
			}

			resp, err := s.AddNetwork(context.Background(), req)
			if tt.wantErr != nil {
				assert.EqualError(t, err, tt.wantErr.Error())
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, resp)
			}

			// Add more detailed assertion messages
			assert.Equal(t, float64(1), testutil.ToFloat64(prometheusmetrics.AddIPCnt),
				"AddIPCnt should be incremented exactly once for test case: %s", tt.name)
		})
	}
}
