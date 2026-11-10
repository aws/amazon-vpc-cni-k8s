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
	"fmt"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/ipamd/datastore"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/networkutils"
	pb "github.com/aws/amazon-vpc-cni-k8s/rpc"
	"github.com/aws/amazon-vpc-cni-k8s/utils/prometheusmetrics"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const waitTestClientVersion = "1.2.3"

type waitTestEnv struct {
	server *server
	stores []*datastore.DataStore
	mocks  *testMocks
}

func newWaitTestEnv(t *testing.T, networkCards, ipsPerCard int, waitPeriod time.Duration) *waitTestEnv {
	t.Helper()
	resetWaitTestMetrics()

	m := setup(t)
	m.awsutils.EXPECT().GetVPCIPv4CIDRs().Return([]string{"10.0.0.0/8"}, nil).AnyTimes()
	m.awsutils.EXPECT().GetVPCIPv6CIDRs().Return([]string{}, nil).AnyTimes()
	m.network.EXPECT().UseExternalSNAT().Return(true).AnyTimes()
	m.network.EXPECT().GetExcludeSNATCIDRs().Return(nil).AnyTimes()

	var stores []*datastore.DataStore
	for card := 0; card < networkCards; card++ {
		ds := datastore.NewDataStore(log, datastore.NullCheckpoint{}, false, card)
		require.NoError(t, ds.AddENI(waitTestENIID(card), card, card == 0, false, false,
			networkutils.CalculateRouteTableId(card, card), ""))
		for i := 0; i < ipsPerCard; i++ {
			require.NoError(t, ds.AddIPv4CidrToStore(waitTestENIID(card), waitTestCardIP(card, i), false))
		}
		stores = append(stores, ds)
	}

	return &waitTestEnv{
		server: &server{
			version: waitTestClientVersion,
			ipamContext: &IPAMContext{
				awsClient:              m.awsutils,
				maxIPsPerENI:           14,
				maxENI:                 4,
				networkClient:          m.network,
				enableIPv4:             true,
				enableMultiNICSupport:  networkCards > 1,
				dataStoreAccess:        &datastore.DataStoreAccess{DataStores: stores},
				ipAssignmentWaitPeriod: waitPeriod,
			},
		},
		stores: stores,
		mocks:  m,
	}
}

func waitTestENIID(card int) string {
	return fmt.Sprintf("eni-%d", card)
}

func waitTestCardIP(card, index int) net.IPNet {
	return net.IPNet{
		IP:   net.ParseIP(fmt.Sprintf("10.%d.0.%d", card, index+2)),
		Mask: net.IPv4Mask(255, 255, 255, 255),
	}
}

func resetWaitTestMetrics() {
	prometheusmetrics.AddIPWaitDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "awscni_add_ip_wait_duration_seconds",
			Help:    "The duration a CNI ADD spent waiting for a pod IP to become available in the datastore",
			Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 7.5, 10},
		},
		[]string{"result"},
	)
	prometheusmetrics.AddIPWaiters = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "awscni_add_ip_waiters",
		Help: "The number of CNI ADD requests currently waiting for a pod IP",
	})
	prometheusmetrics.NoAvailableIPAddrs = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "awscni_no_available_ip_addresses",
		Help: "The number of pod IP assignments that fail due to no available IP addresses",
	})
	prometheusmetrics.AddIPCnt = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "awscni_add_ip_req_count",
		Help: "Number of add IP address requests",
	})
}

func waitTestAddRequest(containerID string, multiNIC bool) *pb.AddNetworkRequest {
	return &pb.AddNetworkRequest{
		ClientVersion:              waitTestClientVersion,
		Netns:                      "netns",
		NetworkName:                "net0",
		ContainerID:                containerID,
		IfName:                     "eni",
		RequiresMultiNICAttachment: multiNIC,
	}
}

func (env *waitTestEnv) parkAdds(n int) func() {
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			_, _ = env.server.AddNetwork(context.Background(), waitTestAddRequest(fmt.Sprintf("cid-parked-%d", id), false))
		}(i)
	}
	return wg.Wait
}

func TestWaitPeriodZeroIsOnePass(t *testing.T) {
	env := newWaitTestEnv(t, 1, 0, 0)
	defer env.mocks.ctrl.Finish()

	start := time.Now()
	resp, err := env.server.AddNetwork(context.Background(), waitTestAddRequest("cid", false))
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.False(t, resp.Success)
	assert.Less(t, elapsed, ipAssignmentPollInterval,
		"with the wait disabled the pass must run once and return without sleeping")
	assert.Equal(t, 0, testutil.CollectAndCount(prometheusmetrics.AddIPWaitDuration),
		"a disabled wait must not record a wait duration")
	assert.Equal(t, float64(1), testutil.ToFloat64(prometheusmetrics.NoAvailableIPAddrs))
}

func TestWaitTimeoutReturnsTodaysError(t *testing.T) {
	const period = time.Second
	env := newWaitTestEnv(t, 1, 0, period)
	defer env.mocks.ctrl.Finish()

	start := time.Now()
	resp, err := env.server.AddNetwork(context.Background(), waitTestAddRequest("cid", false))
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.False(t, resp.Success)
	assert.Empty(t, resp.IPAllocationMetadata)
	assert.GreaterOrEqual(t, elapsed, period)
	assert.Less(t, elapsed, 2*period)
	assert.Equal(t, 1, testutil.CollectAndCount(prometheusmetrics.AddIPWaitDuration))
	assert.Equal(t, float64(0), testutil.ToFloat64(prometheusmetrics.AddIPWaiters),
		"the waiter gauge must return to zero")
	assert.Equal(t, float64(1), testutil.ToFloat64(prometheusmetrics.NoAvailableIPAddrs))
}

func TestWaiterServedMidWait(t *testing.T) {
	const period = 3 * time.Second
	const arrival = 300 * time.Millisecond
	env := newWaitTestEnv(t, 1, 0, period)
	defer env.mocks.ctrl.Finish()

	go func() {
		time.Sleep(arrival)
		_ = env.stores[0].AddIPv4CidrToStore(waitTestENIID(0), waitTestCardIP(0, 0), false)
	}()

	start := time.Now()
	resp, err := env.server.AddNetwork(context.Background(), waitTestAddRequest("cid", false))
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.True(t, resp.Success)
	require.Len(t, resp.IPAllocationMetadata, 1)
	assert.Equal(t, "10.0.0.2", resp.IPAllocationMetadata[0].IPv4Addr)
	assert.GreaterOrEqual(t, elapsed, arrival)
	assert.Less(t, elapsed, period)
	assert.Equal(t, 1, testutil.CollectAndCount(prometheusmetrics.AddIPWaitDuration))
	assert.Equal(t, float64(0), testutil.ToFloat64(prometheusmetrics.AddIPWaiters))
	assert.Equal(t, float64(0), testutil.ToFloat64(prometheusmetrics.NoAvailableIPAddrs),
		"an ADD served mid-wait is not a failed assignment")
}

func TestTimeoutDoesNotMultiplyAcrossNetworkCards(t *testing.T) {
	const period = time.Second
	env := newWaitTestEnv(t, 4, 0, period)
	defer env.mocks.ctrl.Finish()

	start := time.Now()
	resp, err := env.server.AddNetwork(context.Background(), waitTestAddRequest("cid", false))
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.False(t, resp.Success)
	assert.GreaterOrEqual(t, elapsed, period)
	assert.Less(t, elapsed, 2*period,
		"the bound is per ADD, not per network card: four empty cards must cost one period")
	assert.Equal(t, float64(1), testutil.ToFloat64(prometheusmetrics.NoAvailableIPAddrs))
}

func TestNonExhaustionErrorFailsFast(t *testing.T) {
	env := newWaitTestEnv(t, 1, 0, 5*time.Second)
	defer env.mocks.ctrl.Finish()
	env.server.ipamContext.enableIPv4 = false
	env.server.ipamContext.enableIPv6 = true

	start := time.Now()
	resp, err := env.server.AddNetwork(context.Background(), waitTestAddRequest("cid", false))
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.False(t, resp.Success)
	assert.Less(t, elapsed, ipAssignmentPollInterval,
		"an error other than datastore exhaustion must not be retried")
	assert.Equal(t, 0, testutil.CollectAndCount(prometheusmetrics.AddIPWaitDuration))
	assert.Equal(t, float64(0), testutil.ToFloat64(prometheusmetrics.NoAvailableIPAddrs))
}

func TestPartialMultiNICReentryIsIdempotent(t *testing.T) {
	const period = 3 * time.Second
	const arrival = 300 * time.Millisecond
	env := newWaitTestEnv(t, 3, 0, period)
	defer env.mocks.ctrl.Finish()

	require.NoError(t, env.stores[0].AddIPv4CidrToStore(waitTestENIID(0), waitTestCardIP(0, 0), false))
	require.NoError(t, env.stores[1].AddIPv4CidrToStore(waitTestENIID(1), waitTestCardIP(1, 0), false))

	go func() {
		time.Sleep(arrival)
		_ = env.stores[2].AddIPv4CidrToStore(waitTestENIID(2), waitTestCardIP(2, 0), false)
	}()

	resp, err := env.server.AddNetwork(context.Background(), waitTestAddRequest("cid", true))

	require.NoError(t, err)
	assert.True(t, resp.Success)
	require.Len(t, resp.IPAllocationMetadata, 3,
		"a pod partly served on earlier cards must not double-append on re-entry")

	seen := map[string]bool{}
	for _, md := range resp.IPAllocationMetadata {
		assert.False(t, seen[md.IPv4Addr], "address %s appears twice", md.IPv4Addr)
		seen[md.IPv4Addr] = true
	}
}

func TestDatastoreLockIsFreeThroughoutWait(t *testing.T) {
	const period = 1500 * time.Millisecond
	env := newWaitTestEnv(t, 1, 0, period)
	defer env.mocks.ctrl.Finish()

	join := env.parkAdds(8)

	deadline := time.Now().Add(period)
	probes := 0
	var worst time.Duration
	for time.Now().Before(deadline) {
		start := time.Now()
		env.stores[0].GetIPStats("4")
		if d := time.Since(start); d > worst {
			worst = d
		}
		probes++
		time.Sleep(10 * time.Millisecond)
	}
	join()

	assert.Greater(t, probes, 20, "the probe loop must have run during the wait")
	assert.Less(t, worst, 100*time.Millisecond,
		"the datastore lock must be acquirable throughout the wait, worst probe %s", worst)
}

func TestUnassignCompletesWhileAddsAreWaiting(t *testing.T) {
	const period = 1500 * time.Millisecond
	env := newWaitTestEnv(t, 1, 1, period)
	defer env.mocks.ctrl.Finish()

	held, err := env.server.AddNetwork(context.Background(), waitTestAddRequest("cid-held", false))
	require.NoError(t, err)
	require.True(t, held.Success)

	join := env.parkAdds(8)
	time.Sleep(200 * time.Millisecond)

	start := time.Now()
	_, ip, _, _, _, err := env.stores[0].UnassignPodIPAddress(datastore.IPAMKey{
		ContainerID: "cid-held",
		IfName:      "eni",
		NetworkName: "net0",
	})
	elapsed := time.Since(start)
	join()

	require.NoError(t, err)
	assert.Equal(t, "10.0.0.2", ip)
	assert.Less(t, elapsed, 100*time.Millisecond,
		"an unassign must not block behind parked ADDs, took %s", elapsed)
}

func TestDelNetworkCompletesWhileAddsAreWaiting(t *testing.T) {
	const period = 1500 * time.Millisecond
	env := newWaitTestEnv(t, 1, 1, period)
	defer env.mocks.ctrl.Finish()

	held, err := env.server.AddNetwork(context.Background(), waitTestAddRequest("cid-held", false))
	require.NoError(t, err)
	require.True(t, held.Success)

	join := env.parkAdds(8)
	time.Sleep(200 * time.Millisecond)

	start := time.Now()
	del, err := env.server.DelNetwork(context.Background(), &pb.DelNetworkRequest{
		ClientVersion: waitTestClientVersion,
		NetworkName:   "net0",
		ContainerID:   "cid-held",
		IfName:        "eni",
		Reason:        "PodDeleted",
	})
	elapsed := time.Since(start)
	join()

	require.NoError(t, err)
	assert.True(t, del.Success)
	assert.Less(t, elapsed, 200*time.Millisecond,
		"a DEL must not block behind parked ADDs, took %s", elapsed)
}

func TestGetIPAssignmentWaitPeriod(t *testing.T) {
	defaultPeriod := time.Duration(defaultIPAssignmentWaitPeriod) * time.Second
	tests := []struct {
		name  string
		set   bool
		value string
		want  time.Duration
	}{
		{"unset falls back to the default", false, "", defaultPeriod},
		{"ten seconds", true, "10", 10 * time.Second},
		{"zero disables the wait", true, "0", 0},
		{"empty string is invalid", true, "", defaultPeriod},
		{"non numeric is invalid", true, "abc", defaultPeriod},
		{"negative is invalid", true, "-1", defaultPeriod},
		{"a duration string is invalid", true, "10s", defaultPeriod},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NoError(t, os.Unsetenv(envIPAssignmentWaitPeriod))
			if tt.set {
				require.NoError(t, os.Setenv(envIPAssignmentWaitPeriod, tt.value))
				defer os.Unsetenv(envIPAssignmentWaitPeriod)
			}
			assert.Equal(t, tt.want, getIPAssignmentWaitPeriod())
		})
	}
}
