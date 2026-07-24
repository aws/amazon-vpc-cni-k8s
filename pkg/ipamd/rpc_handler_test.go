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
	"os"
	"testing"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/ipamd/datastore"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/networkutils"
	"github.com/aws/amazon-vpc-cni-k8s/rpc"
	multiErr "github.com/hashicorp/go-multierror"

	pb "github.com/aws/amazon-vpc-cni-k8s/rpc"

	"github.com/aws/amazon-vpc-cni-k8s/utils/prometheusmetrics"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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

	type ENIConfig struct {
		IPs          []string
		DeviceNumber int
		IsPrimary    bool
	}

	type fields struct {
		managedNetworkCards           int
		ipV4AddressByENIID            map[int]map[string]ENIConfig
		ipV6PrefixByENIID             map[int]map[string]ENIConfig
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
				ipV4AddressByENIID: map[int]map[string]ENIConfig{
					0: {"eni-1": {IPs: []string{"192.168.1.100"}, DeviceNumber: 0, IsPrimary: true}}},
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
						RouteTableId: int32(254),
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
				ipV4AddressByENIID: map[int]map[string]ENIConfig{
					0: {"eni-1": {IPs: []string{"192.168.1.100"}, DeviceNumber: 0, IsPrimary: true}},
					1: {"eni-2": {IPs: []string{"192.168.16.100"}, DeviceNumber: 1, IsPrimary: false}},
				}, getVPCIPv4CIDRsCalls: []getVPCIPv4CIDRsCall{
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
						RouteTableId: int32(254),
					},
					{
						IPv4Addr:     "192.168.16.100",
						IPv6Addr:     "",
						DeviceNumber: int32(1),
						RouteTableId: int32(10101), // Network Card * DeviceNumber + Max ENIs per card + 1
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
				ipV4AddressByENIID: map[int]map[string]ENIConfig{
					0: {"eni-1": {IPs: []string{"192.168.1.100"}, DeviceNumber: 0, IsPrimary: true}},
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
						RouteTableId: int32(254),
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
				ipV4AddressByENIID:  map[int]map[string]ENIConfig{},
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
				ipV4AddressByENIID:    map[int]map[string]ENIConfig{},
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
				ipV4AddressByENIID:            map[int]map[string]ENIConfig{0: {}},
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
				ipV4AddressByENIID: map[int]map[string]ENIConfig{
					0: {},
					1: {"eni-1": {IPs: []string{"192.168.1.100"}, DeviceNumber: 0, IsPrimary: false}},
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
						RouteTableId: int32(10100),
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
				ipV4AddressByENIID: map[int]map[string]ENIConfig{
					0: {},
					1: {"eni-1": {IPs: []string{"192.168.1.100"}, DeviceNumber: 0, IsPrimary: false}},
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
				ipV6PrefixByENIID: map[int]map[string]ENIConfig{
					0: {
						"eni-1": {IPs: []string{"2001:db8::/64"}, DeviceNumber: 0, IsPrimary: true},
					},
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
						RouteTableId: int32(254),
					},
				},
				VPCv6CIDRs: []string{"2001:db8::/56"},
			},
		},
		{
			name: "successfully allocated IPv6Address multinic pods",
			fields: fields{
				managedNetworkCards: 2,
				ipV6PrefixByENIID: map[int]map[string]ENIConfig{
					0: {
						"eni-1": {IPs: []string{"2001:db8::/64"}, DeviceNumber: 0, IsPrimary: true},
					},
					1: {
						"eni-2": {IPs: []string{"2001:db8:0:01::/64"}, DeviceNumber: 1, IsPrimary: false},
					},
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
						RouteTableId: int32(254),
					},
					{
						IPv4Addr:     "",
						IPv6Addr:     "2001:db8:0:1::",
						DeviceNumber: int32(1),
						RouteTableId: int32(10101),
					},
				},
				VPCv6CIDRs: []string{"2001:db8::/56"},
			},
		},
		{
			name: "failed allocating IPv6Address - No IP addresses available",
			fields: fields{
				managedNetworkCards:     1,
				ipV6PrefixByENIID:       map[int]map[string]ENIConfig{},
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
				ipV6PrefixByENIID: map[int]map[string]ENIConfig{
					0: {
						"eni-1": {IPs: []string{"2001:db8::/64"}, DeviceNumber: 0, IsPrimary: true},
					},
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
				for eniID, eniConfig := range eniMap {
					dsAccess.GetDataStore(networkCard).AddENI(eniID, eniConfig.DeviceNumber, eniConfig.IsPrimary, false, false, networkutils.CalculateRouteTableId(eniConfig.DeviceNumber, networkCard), "")
					for _, ipv4Address := range eniConfig.IPs {
						ipv4Addr := net.IPNet{IP: net.ParseIP(ipv4Address), Mask: net.IPv4Mask(255, 255, 255, 255)}
						dsAccess.GetDataStore(networkCard).AddIPv4CidrToStore(eniID, ipv4Addr, false)
					}
				}
			}

			for networkCard, eniMap := range tt.fields.ipV6PrefixByENIID {
				for eniID, eniConfig := range eniMap {
					dsAccess.GetDataStore(networkCard).AddENI(eniID, eniConfig.DeviceNumber, eniConfig.IsPrimary, false, false, networkutils.CalculateRouteTableId(eniConfig.DeviceNumber, networkCard), "")
					for _, ipv6Prefix := range eniConfig.IPs {
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

func TestServer_GetNetworkPolicyConfigs(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	mockContext := &IPAMContext{
		networkPolicyMode:     "standard",
		enableMultiNICSupport: true,
	}

	rpcServer := server{
		ipamContext: mockContext,
	}

	resp, err := rpcServer.GetNetworkPolicyConfigs(context.TODO(), nil)
	assert.NoError(t, err)
	assert.Equal(t, "standard", resp.NetworkPolicyMode)
	assert.True(t, resp.MultiNICEnabled)
}

func TestRunRPCHandler_UnixSocket(t *testing.T) {
	socketPath := t.TempDir() + "/ipamd.sock"

	m := setup(t)
	defer m.ctrl.Finish()

	mockContext := &IPAMContext{
		awsClient:       m.awsutils,
		networkClient:   m.network,
		dataStoreAccess: datastore.InitializeDataStores([]bool{false}, "test", false, log),
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- mockContext.runRPCHandlerWithSocketPath("1.0.0", socketPath)
	}()

	// Wait for the socket to appear
	timeout := time.After(5 * time.Second)
	for {
		info, err := os.Stat(socketPath)
		if err == nil {
			assert.Equal(t, os.FileMode(0660), info.Mode().Perm(), "Socket should have 0660 permissions")

			conn, err := net.Dial("unix", socketPath)
			assert.NoError(t, err, "Should be able to connect to Unix socket")
			if conn != nil {
				conn.Close()
			}
			// Clean up: close the listener to stop the gRPC server goroutine
			t.Cleanup(func() {
				os.Remove(socketPath)
			})
			return
		}
		select {
		case e := <-errCh:
			t.Fatalf("Server failed to start: %v", e)
		case <-timeout:
			t.Fatalf("Timed out waiting for socket to appear")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestRunRPCHandler_RemovesStaleSocket(t *testing.T) {
	socketPath := t.TempDir() + "/ipamd.sock"

	// Create a stale regular file to simulate leftover from a previous run
	require.NoError(t, os.WriteFile(socketPath, []byte("stale"), 0600))

	m := setup(t)
	defer m.ctrl.Finish()

	mockContext := &IPAMContext{
		awsClient:       m.awsutils,
		networkClient:   m.network,
		dataStoreAccess: datastore.InitializeDataStores([]bool{false}, "test", false, log),
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- mockContext.runRPCHandlerWithSocketPath("1.0.0", socketPath)
	}()

	// Wait for the socket to appear — proves the stale file was removed and replaced
	timeout := time.After(5 * time.Second)
	for {
		info, err := os.Stat(socketPath)
		if err == nil && info.Mode()&os.ModeSocket != 0 {
			conn, err := net.Dial("unix", socketPath)
			assert.NoError(t, err, "Should be able to connect after stale socket removal")
			if conn != nil {
				conn.Close()
			}
			t.Cleanup(func() {
				os.Remove(socketPath)
			})
			return
		}
		select {
		case e := <-errCh:
			t.Fatalf("Server failed to start: %v", e)
		case <-timeout:
			t.Fatalf("Timed out waiting for socket to appear")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestRunRPCHandler_CreatesSocketDirectory(t *testing.T) {
	socketPath := t.TempDir() + "/sub/dir/ipamd.sock"

	m := setup(t)
	defer m.ctrl.Finish()

	mockContext := &IPAMContext{
		awsClient:       m.awsutils,
		networkClient:   m.network,
		dataStoreAccess: datastore.InitializeDataStores([]bool{false}, "test", false, log),
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- mockContext.runRPCHandlerWithSocketPath("1.0.0", socketPath)
	}()

	// Wait for the socket to appear — proves MkdirAll created intermediate directories
	timeout := time.After(5 * time.Second)
	for {
		if _, err := os.Stat(socketPath); err == nil {
			t.Cleanup(func() {
				os.Remove(socketPath)
			})
			return
		}
		select {
		case e := <-errCh:
			t.Fatalf("Server failed to start: %v", e)
		case <-timeout:
			t.Fatalf("Timed out waiting for socket to appear")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestServer_DelNetwork_PodENI_InvalidAnnotation(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	mockContext := &IPAMContext{
		awsClient:       m.awsutils,
		k8sClient:       m.k8sClient,
		enablePodENI:    true,
		enableIPv4:      true,
		networkClient:   m.network,
		dataStoreAccess: datastore.InitializeDataStores([]bool{false}, "test", false, log),
	}

	tests := []struct {
		name       string
		podName    string
		podUID     string
		requestUID string
		annotation string
		wantErr    bool
		wantMsg    string
	}{
		{
			name:       "malformed JSON annotation returns error",
			podName:    "test-pod-malformed-json",
			podUID:     "test-uid",
			requestUID: "test-uid",
			annotation: "not-valid-json",
			wantErr:    true,
		},
		{
			name:       "empty JSON array annotation returns error",
			podName:    "test-pod-empty-array",
			podUID:     "test-uid",
			requestUID: "test-uid",
			annotation: "[]",
			wantErr:    true,
			wantMsg:    "parsed PodENIData is empty",
		},
		{
			name:       "pod UID mismatch returns error",
			podName:    "test-pod-uid-mismatch",
			podUID:     "actual-uid",
			requestUID: "wrong-uid",
			annotation: `[{"eniId":"eni-abc","ifAddress":"01:23:45:67:89:ab","privateIp":"10.0.0.1","vlanID":1,"subnetCidr":"10.0.0.0/24"}]`,
			wantErr:    true,
			wantMsg:    "pod UID mismatch",
		},
		{
			name:       "valid annotation succeeds",
			podName:    "test-pod-valid",
			podUID:     "test-uid",
			requestUID: "test-uid",
			annotation: `[{"eniId":"eni-abc","ifAddress":"01:23:45:67:89:ab","privateIp":"10.0.0.1","vlanID":1,"subnetCidr":"10.0.0.0/24"}]`,
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a pod with the given annotation
			pod := corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      tt.podName,
					Namespace: "default",
					UID:       types.UID(tt.podUID),
					Annotations: map[string]string{
						"vpc.amazonaws.com/pod-eni": tt.annotation,
					},
				},
			}
			err := m.k8sClient.Create(context.TODO(), &pod)
			require.NoError(t, err)

			rpcServer := server{
				version:     "1.0.0",
				ipamContext: mockContext,
			}

			delReq := &pb.DelNetworkRequest{
				ClientVersion:    "1.0.0",
				K8S_POD_NAME:     pod.Name,
				K8S_POD_NAMESPACE: pod.Namespace,
				K8S_POD_UID:      tt.requestUID,
				NetworkName:      "net0",
				ContainerID:      "container-id",
				IfName:           "eth0",
				Reason:           "PodDeleted",
			}

			resp, err := rpcServer.DelNetwork(context.TODO(), delReq)

			if tt.wantErr {
				assert.False(t, resp.Success)
				assert.Error(t, err)
				if tt.wantMsg != "" {
					assert.Contains(t, err.Error(), tt.wantMsg)
				}
			} else {
				assert.True(t, resp.Success)
				assert.NoError(t, err)
			}
		})
	}
}
