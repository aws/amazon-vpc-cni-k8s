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
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/awsutils"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/networkutils"
	pb "github.com/aws/amazon-vpc-cni-k8s/rpc"
	"github.com/aws/amazon-vpc-cni-k8s/utils/prometheusmetrics"
)

const (
	trunkENIid       = "eni-0000trunk"
	trunkMAC         = "12:ef:2a:98:e5:5c"
	trunkDevice      = 3
	trunkLinkIndex   = 4
	branchENIMACTest = "12:ef:2a:98:e5:5d"
)

// branchENIPod builds an SGP pod, which is what makes AddNetwork take the branch ENI path.
func branchENIPod(name string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Annotations: map[string]string{
				"vpc.amazonaws.com/pod-eni": `[{"eniId":"eni-0000branch","ifAddress":"` + branchENIMACTest +
					`","privateIp":"10.10.30.10","vlanID":7,"subnetCidr":"10.10.30.0/24"}]`,
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:  "test",
				Image: "test",
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{"vpc.amazonaws.com/pod-eni": resourceQuantityOne()},
				},
			}},
		},
	}
}

func resourceQuantityOne() resource.Quantity {
	return *resource.NewQuantity(1, resource.DecimalSI)
}

func trunkENIMetadata() awsutils.ENIMetadata {
	return awsutils.ENIMetadata{
		ENIID:          trunkENIid,
		MAC:            trunkMAC,
		NetworkCard:    defaultNetworkCard,
		DeviceNumber:   trunkDevice,
		SubnetIPv4CIDR: secSubnet,
	}
}

func trunkENIIPAMContext(m *testMocks) *IPAMContext {
	return &IPAMContext{
		awsClient:       m.awsutils,
		k8sClient:       m.k8sClient,
		networkClient:   m.network,
		enablePodENI:    true,
		enableIPv4:      true,
		primaryIP:       make(map[string]string),
		maxENI:          maxENIPerNIC,
		numNetworkCards: 1,
		dataStoreAccess: testDatastore(),
	}
}

// TestAddNetwork_PodENI_RejectedUntilTrunkIsConfigured exercises the gate while the trunk is in the datastore unconfigured.
func TestAddNetwork_PodENI_RejectedUntilTrunkIsConfigured(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	// Fresh counter, so the assertions below are about this ADD only.
	prometheusmetrics.PodENIErr.Reset()

	mockContext := trunkENIIPAMContext(m)
	require.NoError(t, mockContext.dataStoreAccess.GetDataStore(defaultNetworkCard).AddENI(
		trunkENIid, trunkDevice, false, true /* isTrunk */, false,
		networkutils.CalculateRouteTableId(trunkDevice, defaultNetworkCard), ""))
	require.Equal(t, trunkENIid, mockContext.dataStoreAccess.GetDataStore(defaultNetworkCard).GetTrunkENI(),
		"the trunk must be visible to GetTrunkENI for this test to exercise the gate")

	pod := branchENIPod("sgp-pod-trunk-not-ready")
	require.NoError(t, m.k8sClient.Create(context.TODO(), pod))

	// No netlink or IMDS expectations: any such call fails the test as unexpected.

	s := &server{version: "1.2.3", ipamContext: mockContext}
	resp, err := s.AddNetwork(context.Background(), &pb.AddNetworkRequest{
		ClientVersion:     "1.2.3",
		Netns:             "netns",
		NetworkName:       "net0",
		ContainerID:       "cid",
		IfName:            "eth0",
		K8S_POD_NAME:      pod.Name,
		K8S_POD_NAMESPACE: pod.Namespace,
	})

	assert.NoError(t, err)
	assert.False(t, resp.Success, "pod ENI ADD must be rejected while the trunk ENI is not configured")
	assert.Zero(t, resp.PodVlanId, "no VLAN may be handed to the plugin before the trunk is configured")
	// Assert which rejection fired; checking only Success would stay green with the gate deleted.
	assert.Equal(t, float64(1), testutil.ToFloat64(prometheusmetrics.PodENIErr.With(prometheus.Labels{"fn": "trunkENINotReady"})),
		"the ADD must be rejected by the readiness gate specifically")
	assert.Zero(t, testutil.ToFloat64(prometheusmetrics.PodENIErr.With(prometheus.Labels{"fn": "trunkENILinkIndex"})),
		"and not by the link resolution, which would mean the gate did not fire")
}

// TestAddNetwork_PodENI_AcceptedOnceTrunkIsReady checks the same request succeeds once the trunk is configured.
func TestAddNetwork_PodENI_AcceptedOnceTrunkIsReady(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	mockContext := trunkENIIPAMContext(m)
	require.NoError(t, mockContext.dataStoreAccess.GetDataStore(defaultNetworkCard).AddENI(
		trunkENIid, trunkDevice, false, true /* isTrunk */, false,
		networkutils.CalculateRouteTableId(trunkDevice, defaultNetworkCard), ""))
	mockContext.trunkENI.markReady(trunkENIid, trunkMAC)

	pod := branchENIPod("sgp-pod-trunk-ready")
	require.NoError(t, m.k8sClient.Create(context.TODO(), pod))

	// No GetAttachedENIs expectation: the link index comes from the MAC recorded at setup time.
	m.network.EXPECT().GetLinkByMac(trunkMAC, gomock.Any()).Return(
		&netlink.Device{LinkAttrs: netlink.LinkAttrs{Name: "eth1", Index: trunkLinkIndex}}, nil)
	m.awsutils.EXPECT().GetVPCIPv4CIDRs().Return([]string{"10.10.0.0/16"}, nil)
	m.network.EXPECT().UseExternalSNAT().Return(true)

	s := &server{version: "1.2.3", ipamContext: mockContext}
	resp, err := s.AddNetwork(context.Background(), &pb.AddNetworkRequest{
		ClientVersion:     "1.2.3",
		Netns:             "netns",
		NetworkName:       "net0",
		ContainerID:       "cid",
		IfName:            "eth0",
		K8S_POD_NAME:      pod.Name,
		K8S_POD_NAMESPACE: pod.Namespace,
	})

	assert.NoError(t, err)
	assert.True(t, resp.Success)
	assert.EqualValues(t, 7, resp.PodVlanId)
	assert.EqualValues(t, trunkLinkIndex, resp.ParentIfIndex)
}

// TestSetupENI_TrunkReadyFlagLifecycle covers the flag: unset before setup, set on success, cleared on failure.
func TestSetupENI_TrunkReadyFlagLifecycle(t *testing.T) {
	t.Run("set only after the trunk's network is set up", func(t *testing.T) {
		m := setup(t)
		defer m.ctrl.Finish()

		mockContext := trunkENIIPAMContext(m)
		assert.False(t, trunkIsReady(mockContext), "must start out not ready")

		m.awsutils.EXPECT().GetPrimaryENI().Return(primaryENIid)
		m.network.EXPECT().GetRouteTableNumberForENI(defaultNetworkCard, gomock.Any(), trunkDevice, mockContext.maxENI, false).Return(trunkDevice+1, false, nil)
		m.network.EXPECT().SetupENINetwork(gomock.Any(), trunkMAC, defaultNetworkCard, secSubnet, maxENIPerNIC, true, gomock.Any(), gomock.Any()).Return(nil)

		err := mockContext.setupENI(context.Background(), trunkENIid, trunkENIMetadata(), true /* isTrunkENI */, false)
		assert.NoError(t, err)
		assert.True(t, trunkIsReady(mockContext))
		assert.Equal(t, trunkMAC, mockContext.trunkENI.mac)
	})

	t.Run("cleared when the trunk's network setup fails", func(t *testing.T) {
		m := setup(t)
		defer m.ctrl.Finish()

		mockContext := trunkENIIPAMContext(m)
		// Pretend an earlier attempt succeeded, so the clear is observable.
		mockContext.trunkENI.markReady(trunkENIid, trunkMAC)

		m.awsutils.EXPECT().GetPrimaryENI().Return(primaryENIid)
		m.network.EXPECT().GetRouteTableNumberForENI(defaultNetworkCard, gomock.Any(), trunkDevice, mockContext.maxENI, false).Return(trunkDevice+1, false, nil)
		m.network.EXPECT().SetupENINetwork(gomock.Any(), trunkMAC, defaultNetworkCard, secSubnet, maxENIPerNIC, true, gomock.Any(), gomock.Any()).
			Return(errors.New("simulated trunk setup failure"))

		err := mockContext.setupENI(context.Background(), trunkENIid, trunkENIMetadata(), true /* isTrunkENI */, false)
		assert.Error(t, err)
		assert.False(t, trunkIsReady(mockContext), "a failed trunk setup must not leave the gate open")
		assert.Empty(t, mockContext.trunkENI.mac)
		assert.Empty(t, mockContext.dataStoreAccess.GetDataStore(defaultNetworkCard).GetTrunkENI())
	})

	// Defensive branch: a trunk is always a secondary attachment, so this should be unreachable.
	t.Run("set for a trunk that is somehow also the primary ENI", func(t *testing.T) {
		m := setup(t)
		defer m.ctrl.Finish()

		mockContext := trunkENIIPAMContext(m)
		primaryAsTrunk := trunkENIMetadata()
		primaryAsTrunk.ENIID = primaryENIid

		m.awsutils.EXPECT().GetPrimaryENI().Return(primaryENIid)
		m.network.EXPECT().GetRouteTableNumberForENI(defaultNetworkCard, gomock.Any(), trunkDevice, mockContext.maxENI, false).Return(trunkDevice+1, false, nil)
		// No SetupENINetwork expectation: setupENI skips it for the primary ENI.

		err := mockContext.setupENI(context.Background(), primaryENIid, primaryAsTrunk, true /* isTrunkENI */, false)
		assert.NoError(t, err)
		assert.True(t, trunkIsReady(mockContext), "a primary-ENI trunk must not leave the gate shut forever")
		assert.Equal(t, trunkMAC, mockContext.trunkENI.mac)
	})

	t.Run("not set for a plain secondary ENI", func(t *testing.T) {
		m := setup(t)
		defer m.ctrl.Finish()

		mockContext := trunkENIIPAMContext(m)
		secENI := getSecondaryENIMetadata()

		m.awsutils.EXPECT().GetPrimaryENI().Return(primaryENIid)
		m.network.EXPECT().GetRouteTableNumberForENI(defaultNetworkCard, gomock.Any(), secDevice, mockContext.maxENI, false).Return(secDevice+1, false, nil)
		m.network.EXPECT().SetupENINetwork(gomock.Any(), secMAC, defaultNetworkCard, secSubnet, maxENIPerNIC, false, gomock.Any(), gomock.Any()).Return(nil)

		err := mockContext.setupENI(context.Background(), secENIid, secENI, false, false)
		assert.NoError(t, err)
		assert.False(t, trunkIsReady(mockContext))
	})
}

// trunkIsReady asks the question AddNetwork asks.
func trunkIsReady(c *IPAMContext) bool {
	eniID, _ := c.trunkENI.readyENI()
	return eniID != ""
}

// addTrunkToDatastore reproduces what setupENI does before it configures the link.
func addTrunkToDatastore(t *testing.T, c *IPAMContext) {
	t.Helper()
	require.NoError(t, c.dataStoreAccess.GetDataStore(defaultNetworkCard).AddENI(
		trunkENIid, trunkDevice, false, true, false,
		networkutils.CalculateRouteTableId(trunkDevice, defaultNetworkCard), ""))
}

// TestReconcileVlanMTU_IPAMD covers the periodic reconciler's wiring.
func TestReconcileVlanMTU_IPAMD(t *testing.T) {
	t.Run("no-op while no trunk is ready", func(t *testing.T) {
		m := setup(t)
		defer m.ctrl.Finish()

		mockContext := trunkENIIPAMContext(m)
		// A trunk in the datastore is not enough: that is also the state mid-setup.
		addTrunkToDatastore(t, mockContext)
		mockContext.reconcileVlanMTU()

		assert.False(t, trunkIsReady(mockContext), "the reconciler must never arm the gate itself")
	})

	t.Run("reconciles the VLANs under the configured trunk", func(t *testing.T) {
		m := setup(t)
		defer m.ctrl.Finish()

		mockContext := trunkENIIPAMContext(m)
		addTrunkToDatastore(t, mockContext)
		mockContext.trunkENI.markReady(trunkENIid, trunkMAC)

		m.network.EXPECT().GetLinkByMac(trunkMAC, gomock.Any()).Return(
			&netlink.Device{LinkAttrs: netlink.LinkAttrs{Name: "eth1", Index: trunkLinkIndex}}, nil)
		m.network.EXPECT().ReconcileVlanMTUForTrunk(trunkLinkIndex).Return(2, nil)

		mockContext.reconcileVlanMTU()
		assert.True(t, trunkIsReady(mockContext), "a healthy pass must not disturb the state")
	})

	t.Run("a single trunk link lookup failure does not reject pods", func(t *testing.T) {
		m := setup(t)
		defer m.ctrl.Finish()

		mockContext := trunkENIIPAMContext(m)
		addTrunkToDatastore(t, mockContext)
		mockContext.trunkENI.markReady(trunkENIid, trunkMAC)

		m.network.EXPECT().GetLinkByMac(trunkMAC, gomock.Any()).Return(nil, errors.New("simulated lookup failure"))

		mockContext.reconcileVlanMTU()
		assert.True(t, trunkIsReady(mockContext), "one transient netlink failure must not close the gate")
		assert.Equal(t, 1, mockContext.trunkLinkMissingTicks)
	})

	// Nothing re-arms the state once cleared; see the error path in IPAMContext.reconcileVlanMTU.
	t.Run("never clears readiness just because the link lookup keeps failing", func(t *testing.T) {
		m := setup(t)
		defer m.ctrl.Finish()

		mockContext := trunkENIIPAMContext(m)
		mockContext.trunkENI.markReady(trunkENIid, trunkMAC)

		const ticks = 25 // past the log rate limit, so the quiet path is exercised too
		prometheusmetrics.IpamdErr.Reset()
		m.network.EXPECT().GetLinkByMac(trunkMAC, gomock.Any()).
			Return(nil, errors.New("simulated lookup failure")).Times(ticks)

		for i := 0; i < ticks; i++ {
			mockContext.reconcileVlanMTU()
		}

		assert.True(t, trunkIsReady(mockContext),
			"a netlink lookup failure must never cost the node its trunk readiness")
		assert.Equal(t, ticks, mockContext.trunkLinkMissingTicks)
		assert.Equal(t, trunkENIid, mockContext.trunkLinkMissingENI)
		// Only the log is rate limited; the counter has to tick every time.
		assert.Equal(t, float64(ticks),
			testutil.ToFloat64(prometheusmetrics.IpamdErr.With(prometheus.Labels{"fn": "vlanMTUReconcileTrunkLink"})),
			"the error counter must tick once per failed check, not once per log line")
	})

	// A replacement trunk must not inherit its predecessor's consecutive-failure count.
	t.Run("a replacement trunk does not inherit the previous trunk's failure count", func(t *testing.T) {
		m := setup(t)
		defer m.ctrl.Finish()

		const newTrunkENIid = "eni-0000newtrunk"
		const newTrunkMAC = "12:ef:2a:98:e5:5f"

		mockContext := trunkENIIPAMContext(m)
		mockContext.trunkENI.markReady(trunkENIid, trunkMAC)

		m.network.EXPECT().GetLinkByMac(trunkMAC, gomock.Any()).Return(nil, errors.New("gone")).Times(2)
		mockContext.reconcileVlanMTU()
		mockContext.reconcileVlanMTU()
		require.Equal(t, 2, mockContext.trunkLinkMissingTicks)

		mockContext.trunkENI.markReady(newTrunkENIid, newTrunkMAC)
		m.network.EXPECT().GetLinkByMac(newTrunkMAC, gomock.Any()).Return(nil, errors.New("blip"))
		mockContext.reconcileVlanMTU()

		assert.Equal(t, 1, mockContext.trunkLinkMissingTicks, "the count must restart for the new trunk")
		assert.Equal(t, newTrunkENIid, mockContext.trunkLinkMissingENI)
		assert.True(t, trunkIsReady(mockContext))
	})

	t.Run("a recovered lookup resets the failure counter", func(t *testing.T) {
		m := setup(t)
		defer m.ctrl.Finish()

		mockContext := trunkENIIPAMContext(m)
		mockContext.trunkENI.markReady(trunkENIid, trunkMAC)

		m.network.EXPECT().GetLinkByMac(trunkMAC, gomock.Any()).Return(nil, errors.New("blip"))
		mockContext.reconcileVlanMTU()
		require.Equal(t, 1, mockContext.trunkLinkMissingTicks)

		m.network.EXPECT().GetLinkByMac(trunkMAC, gomock.Any()).Return(
			&netlink.Device{LinkAttrs: netlink.LinkAttrs{Name: "eth1", Index: trunkLinkIndex}}, nil)
		m.network.EXPECT().ReconcileVlanMTUForTrunk(trunkLinkIndex).Return(0, nil)
		mockContext.reconcileVlanMTU()

		assert.Zero(t, mockContext.trunkLinkMissingTicks)
		assert.True(t, trunkIsReady(mockContext))
	})
}

// TestNodeIPPoolReconcile_TrunkSweepClearsReadyFlag covers the sweep path clearing readiness for a detached trunk.
func TestNodeIPPoolReconcile_TrunkSweepClearsReadyFlag(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()
	ctx := context.Background()

	mockContext := trunkENIIPAMContext(m)
	ds := mockContext.dataStoreAccess.GetDataStore(defaultNetworkCard)

	primaryENIMetadata := getPrimaryENIMetadata()
	require.NoError(t, ds.AddENI(primaryENIid, primaryDevice, true, false, false,
		networkutils.CalculateRouteTableId(primaryDevice, defaultNetworkCard), ""))
	require.NoError(t, ds.AddENI(trunkENIid, trunkDevice, false, true, false,
		networkutils.CalculateRouteTableId(trunkDevice, defaultNetworkCard), ""))
	mockContext.trunkENI.markReady(trunkENIid, trunkMAC)
	require.True(t, trunkIsReady(mockContext))
	require.Equal(t, trunkENIid, ds.GetTrunkENI())

	// IMDS now reports the primary ENI and a new secondary one; the trunk is gone.
	newENI := getSecondaryENIMetadata()
	attached := []awsutils.ENIMetadata{primaryENIMetadata, newENI}

	m.awsutils.EXPECT().GetPrimaryENI().AnyTimes().Return(primaryENIid)
	m.awsutils.EXPECT().IsUnmanagedENI(gomock.Any()).AnyTimes().Return(false)
	m.awsutils.EXPECT().IsUnmanagedNIC(gomock.Any()).AnyTimes().Return(false)
	m.awsutils.EXPECT().IsEfaOnlyENI(gomock.Any(), gomock.Any()).AnyTimes().Return(false)
	m.awsutils.EXPECT().SetUnmanagedNetworkCards(gomock.Any()).AnyTimes()
	m.awsutils.EXPECT().TagENI(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(nil)
	m.awsutils.EXPECT().GetAttachedENIs().Return(attached, nil)
	m.awsutils.EXPECT().GetIPv4sFromEC2(gomock.Any(), gomock.Any()).AnyTimes().Return(primaryENIMetadata.IPv4Addresses, nil)
	m.awsutils.EXPECT().DescribeAllENIs(gomock.Any()).Return(awsutils.DescribeAllENIsResult{
		ENIMetadata:             attached,
		TagMap:                  map[string]awsutils.TagMap{},
		TrunkENI:                "", // the trunk has detached, so EC2 reports none
		EFAENIs:                 make(map[string]bool),
		EFAOnlyENIByNetworkCard: []string{""},
		ENIsByNetworkCard:       [][]string{defaultNetworkCard: {primaryENIid, secENIid}},
	}, nil)
	m.network.EXPECT().GetRouteTableNumberForENI(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(secDevice+1, false, nil)
	m.network.EXPECT().SetupENINetwork(gomock.Any(), secMAC, gomock.Any(), gomock.Any(), gomock.Any(), false, gomock.Any(), gomock.Any()).AnyTimes().Return(nil)

	mockContext.nodeIPPoolReconcile(ctx, 0)

	assert.Empty(t, ds.GetTrunkENI(), "the detached trunk must be swept out of the datastore")
	assert.False(t, trunkIsReady(mockContext), "a swept trunk must clear the readiness flag")
	assert.Empty(t, mockContext.trunkENI.mac)
}

// TestNodeIPPoolReconcile_TrunkReplacementKeepsNewTrunkReady guards the pass that configures a new trunk and sweeps the old one.
func TestNodeIPPoolReconcile_TrunkReplacementKeepsNewTrunkReady(t *testing.T) {
	const oldTrunkENIid = "eni-0000oldtrunk"
	const newTrunkMAC = "12:ef:2a:98:e5:5e"

	// Repeated because the failure mode depends on Go map iteration order.
	for attempt := 0; attempt < 20; attempt++ {
		m := setup(t)
		ctx := context.Background()

		mockContext := trunkENIIPAMContext(m)
		ds := mockContext.dataStoreAccess.GetDataStore(defaultNetworkCard)
		primaryENIMetadata := getPrimaryENIMetadata()

		require.NoError(t, ds.AddENI(primaryENIid, primaryDevice, true, false, false,
			networkutils.CalculateRouteTableId(primaryDevice, defaultNetworkCard), ""))
		require.NoError(t, ds.AddENI(oldTrunkENIid, trunkDevice, false, true, false,
			networkutils.CalculateRouteTableId(trunkDevice, defaultNetworkCard), ""))
		mockContext.trunkENI.markReady(oldTrunkENIid, trunkMAC)

		// IMDS now reports the primary ENI and a new trunk; the old trunk is gone.
		newTrunk := trunkENIMetadata()
		newTrunk.ENIID = trunkENIid
		newTrunk.MAC = newTrunkMAC
		attached := []awsutils.ENIMetadata{primaryENIMetadata, newTrunk}

		m.awsutils.EXPECT().GetPrimaryENI().AnyTimes().Return(primaryENIid)
		m.awsutils.EXPECT().IsUnmanagedENI(gomock.Any()).AnyTimes().Return(false)
		m.awsutils.EXPECT().IsUnmanagedNIC(gomock.Any()).AnyTimes().Return(false)
		m.awsutils.EXPECT().IsEfaOnlyENI(gomock.Any(), gomock.Any()).AnyTimes().Return(false)
		m.awsutils.EXPECT().SetUnmanagedNetworkCards(gomock.Any()).AnyTimes()
		m.awsutils.EXPECT().TagENI(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(nil)
		m.awsutils.EXPECT().GetIPv4sFromEC2(gomock.Any(), gomock.Any()).AnyTimes().Return(primaryENIMetadata.IPv4Addresses, nil)
		m.awsutils.EXPECT().GetAttachedENIs().Return(attached, nil)
		m.awsutils.EXPECT().DescribeAllENIs(gomock.Any()).Return(awsutils.DescribeAllENIsResult{
			ENIMetadata:             attached,
			TagMap:                  map[string]awsutils.TagMap{},
			TrunkENI:                trunkENIid, // EC2 now reports the NEW trunk
			EFAENIs:                 make(map[string]bool),
			EFAOnlyENIByNetworkCard: []string{""},
			ENIsByNetworkCard:       [][]string{defaultNetworkCard: {primaryENIid, trunkENIid}},
		}, nil)
		m.network.EXPECT().GetRouteTableNumberForENI(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(trunkDevice+1, false, nil)
		// The new trunk's link is configured successfully, so it must end up ready.
		m.network.EXPECT().SetupENINetwork(gomock.Any(), newTrunkMAC, gomock.Any(), gomock.Any(), gomock.Any(), true, gomock.Any(), gomock.Any()).AnyTimes().Return(nil)

		mockContext.nodeIPPoolReconcile(ctx, 0)

		require.True(t, trunkIsReady(mockContext),
			"attempt %d: the newly configured trunk must still be ready after the old one is swept", attempt)
		eniID, mac := mockContext.trunkENI.readyENI()
		require.Equal(t, trunkENIid, eniID, "attempt %d: readiness must be held for the NEW trunk", attempt)
		require.Equal(t, newTrunkMAC, mac, "attempt %d: and with the NEW trunk's MAC", attempt)
		m.ctrl.Finish()
	}
}

// TestSetupENI_SecondTrunkFailureKeepsFirstTrunkReady: a failed second trunk must not disarm the first.
func TestSetupENI_SecondTrunkFailureKeepsFirstTrunkReady(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	mockContext := trunkENIIPAMContext(m)
	const healthyTrunkENIid = "eni-0000healthy"
	mockContext.trunkENI.markReady(healthyTrunkENIid, trunkMAC)

	failing := trunkENIMetadata() // a different ENI id
	m.awsutils.EXPECT().GetPrimaryENI().Return(primaryENIid)
	m.network.EXPECT().GetRouteTableNumberForENI(defaultNetworkCard, gomock.Any(), trunkDevice, mockContext.maxENI, false).Return(trunkDevice+1, false, nil)
	m.network.EXPECT().SetupENINetwork(gomock.Any(), trunkMAC, defaultNetworkCard, secSubnet, maxENIPerNIC, true, gomock.Any(), gomock.Any()).
		Return(errors.New("simulated trunk setup failure"))

	err := mockContext.setupENI(context.Background(), failing.ENIID, failing, true /* isTrunkENI */, false)
	assert.Error(t, err)

	assert.True(t, trunkIsReady(mockContext), "the healthy trunk's readiness must survive another trunk's failure")
	eniID, _ := mockContext.trunkENI.readyENI()
	assert.Equal(t, healthyTrunkENIid, eniID)
}

// TestSetupENI_TrunkNotReadyUntilSetupENINetworkReturns pins readiness to after SetupENINetwork returns.
func TestSetupENI_TrunkNotReadyUntilSetupENINetworkReturns(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	mockContext := trunkENIIPAMContext(m)
	md := trunkENIMetadata()

	m.awsutils.EXPECT().GetPrimaryENI().Return(primaryENIid)
	m.network.EXPECT().GetRouteTableNumberForENI(defaultNetworkCard, gomock.Any(), trunkDevice, mockContext.maxENI, false).Return(trunkDevice+1, false, nil)
	m.network.EXPECT().SetupENINetwork(gomock.Any(), trunkMAC, defaultNetworkCard, secSubnet, maxENIPerNIC, true, gomock.Any(), gomock.Any()).
		DoAndReturn(func(_, _ string, _ int, _ string, _ int, _ bool, _ int, _ bool) error {
			// The trunk is in the datastore already, and the gate must still be shut.
			assert.Equal(t, md.ENIID, mockContext.dataStoreAccess.GetDataStore(defaultNetworkCard).GetTrunkENI(),
				"the trunk should already be in the datastore here, otherwise this test proves nothing")
			assert.False(t, trunkIsReady(mockContext),
				"readiness must not be announced before the trunk's link is configured")
			return nil
		})

	err := mockContext.setupENI(context.Background(), md.ENIID, md, true /* isTrunkENI */, false)
	assert.NoError(t, err)
	assert.True(t, trunkIsReady(mockContext))
}

// TestAddNetwork_PodENI_UsesTheConfiguredTrunksLink pins the admission decision and the link index to the same ENI.
func TestAddNetwork_PodENI_UsesTheConfiguredTrunksLink(t *testing.T) {
	const configuredTrunk = "eni-0000configured"
	const configuredMAC = "12:ef:2a:98:e5:60"
	const configuredLinkIndex = 11
	const unconfiguredTrunk = "eni-0000unconfigured"

	// Repeated because the failure depends on map iteration order.
	for attempt := 0; attempt < 20; attempt++ {
		m := setup(t)

		mockContext := trunkENIIPAMContext(m)
		ds := mockContext.dataStoreAccess.GetDataStore(defaultNetworkCard)
		// Both trunks are in the datastore; only the first was configured.
		require.NoError(t, ds.AddENI(configuredTrunk, trunkDevice, false, true, false,
			networkutils.CalculateRouteTableId(trunkDevice, defaultNetworkCard), ""))
		require.NoError(t, ds.AddENI(unconfiguredTrunk, trunkDevice+1, false, true, false,
			networkutils.CalculateRouteTableId(trunkDevice+1, defaultNetworkCard), ""))
		mockContext.trunkENI.markReady(configuredTrunk, configuredMAC)

		pod := branchENIPod(fmt.Sprintf("sgp-pod-two-trunks-%d", attempt))
		require.NoError(t, m.k8sClient.Create(context.TODO(), pod))

		// Only the configured trunk's MAC may be looked up; anything else fails the test.
		m.network.EXPECT().GetLinkByMac(configuredMAC, gomock.Any()).Return(
			&netlink.Device{LinkAttrs: netlink.LinkAttrs{Name: "eth1", Index: configuredLinkIndex}}, nil)
		m.awsutils.EXPECT().GetVPCIPv4CIDRs().Return([]string{"10.10.0.0/16"}, nil)
		m.network.EXPECT().UseExternalSNAT().Return(true)

		s := &server{version: "1.2.3", ipamContext: mockContext}
		resp, err := s.AddNetwork(context.Background(), &pb.AddNetworkRequest{
			ClientVersion:     "1.2.3",
			Netns:             "netns",
			NetworkName:       "net0",
			ContainerID:       fmt.Sprintf("cid-%d", attempt),
			IfName:            "eth0",
			K8S_POD_NAME:      pod.Name,
			K8S_POD_NAMESPACE: pod.Namespace,
		})

		require.NoError(t, err)
		require.True(t, resp.Success, "attempt %d", attempt)
		require.EqualValues(t, configuredLinkIndex, resp.ParentIfIndex,
			"attempt %d: the plugin must be given the CONFIGURED trunk's link index", attempt)
		m.ctrl.Finish()
	}
}

// TestReadyTrunkLinkIndex covers each of the resolver's guards on its own.
func TestReadyTrunkLinkIndex(t *testing.T) {
	t.Run("resolves the configured trunk's link from its MAC", func(t *testing.T) {
		m := setup(t)
		defer m.ctrl.Finish()

		mockContext := trunkENIIPAMContext(m)
		m.network.EXPECT().GetLinkByMac(trunkMAC, gomock.Any()).Return(
			&netlink.Device{LinkAttrs: netlink.LinkAttrs{Name: "eth1", Index: trunkLinkIndex}}, nil)

		idx, err := mockContext.readyTrunkLinkIndex(trunkENIid, trunkMAC)
		assert.NoError(t, err)
		assert.Equal(t, trunkLinkIndex, idx)
	})

	t.Run("refuses an empty ENI id", func(t *testing.T) {
		m := setup(t)
		defer m.ctrl.Finish()

		mockContext := trunkENIIPAMContext(m)
		// No GetLinkByMac expectation: it must not try.
		idx, err := mockContext.readyTrunkLinkIndex("", trunkMAC)
		assert.Error(t, err)
		assert.Equal(t, -1, idx, "a failed resolve must not return a usable link index")
	})

	t.Run("refuses an empty MAC", func(t *testing.T) {
		m := setup(t)
		defer m.ctrl.Finish()

		mockContext := trunkENIIPAMContext(m)
		// linkByMac("") matches any link with a nil hardware address, so it must not be attempted.
		idx, err := mockContext.readyTrunkLinkIndex(trunkENIid, "")
		// require, not assert: the Contains below would nil-deref on a nil err.
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no MAC recorded")
		assert.Equal(t, -1, idx)
	})

	t.Run("propagates a link lookup failure without inventing an index", func(t *testing.T) {
		m := setup(t)
		defer m.ctrl.Finish()

		mockContext := trunkENIIPAMContext(m)
		m.network.EXPECT().GetLinkByMac(trunkMAC, gomock.Any()).Return(nil, errors.New("simulated lookup failure"))

		idx, err := mockContext.readyTrunkLinkIndex(trunkENIid, trunkMAC)
		assert.Error(t, err)
		assert.Equal(t, -1, idx)
	})
}
