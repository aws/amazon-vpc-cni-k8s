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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vishvananda/netlink"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/awsutils"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/networkutils"
	pb "github.com/aws/amazon-vpc-cni-k8s/rpc"
)

const (
	trunkENIid       = "eni-0000trunk"
	trunkMAC         = "12:ef:2a:98:e5:5c"
	trunkDevice      = 3
	trunkLinkIndex   = 4
	branchENIMACTest = "12:ef:2a:98:e5:5d"
)

// branchENIPod is an SGP pod: it carries the pod-eni extended resource that makes AddNetwork
// take the branch ENI path, plus the annotation the resource controller writes.
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

// TestAddNetwork_PodENI_RejectedUntilTrunkIsConfigured is the gate. The trunk ENI is already in
// the datastore (AddENI runs before SetupENINetwork) and its link exists, but ipamd has not
// finished configuring it. Serving a pod ADD here builds the pod's VLAN on a trunk that is
// still at the boot-time MTU, and the kernel never raises that VLAN again. The ADD must fail so
// kubelet retries the sandbox.
func TestAddNetwork_PodENI_RejectedUntilTrunkIsConfigured(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	mockContext := trunkENIIPAMContext(m)
	require.NoError(t, mockContext.dataStoreAccess.GetDataStore(defaultNetworkCard).AddENI(
		trunkENIid, trunkDevice, false, true /* isTrunk */, false,
		networkutils.CalculateRouteTableId(trunkDevice, defaultNetworkCard), ""))
	require.Equal(t, trunkENIid, mockContext.dataStoreAccess.GetDataStore(defaultNetworkCard).GetTrunkENI(),
		"the trunk must be visible to GetTrunkENI for this test to exercise the gate")

	pod := branchENIPod("sgp-pod-trunk-not-ready")
	require.NoError(t, m.k8sClient.Create(context.TODO(), pod))

	// The trunk's link exists and would resolve fine. Nothing but the readiness state can tell us
	// the trunk is not configured yet, so allow these calls without requiring them.
	m.awsutils.EXPECT().GetAttachedENIs().Return([]awsutils.ENIMetadata{trunkENIMetadata()}, nil).AnyTimes()
	m.network.EXPECT().GetLinkByMac(trunkMAC, gomock.Any()).Return(
		&netlink.Device{LinkAttrs: netlink.LinkAttrs{Name: "eth1", Index: trunkLinkIndex}}, nil).AnyTimes()
	m.awsutils.EXPECT().GetVPCIPv4CIDRs().Return([]string{"10.10.0.0/16"}, nil).AnyTimes()
	m.network.EXPECT().UseExternalSNAT().Return(true).AnyTimes()

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
}

// TestAddNetwork_PodENI_AcceptedOnceTrunkIsReady is the other half of the gate: the same
// request succeeds once ipamd has configured the trunk.
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

	// No GetAttachedENIs expectation: the link index now comes from the MAC recorded at setup
	// time, so a pod ADD costs no IMDS round trip.
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

// TestSetupENI_TrunkReadyFlagLifecycle covers the flag itself: false before setup, true only
// after SetupENINetwork succeeds for the trunk, and cleared again when trunk setup fails.
func TestSetupENI_TrunkReadyFlagLifecycle(t *testing.T) {
	t.Run("set only after the trunk's network is set up", func(t *testing.T) {
		m := setup(t)
		defer m.ctrl.Finish()

		mockContext := trunkENIIPAMContext(m)
		assert.False(t, mockContext.trunkENI.isReady(), "must start out not ready")

		m.awsutils.EXPECT().GetPrimaryENI().Return(primaryENIid)
		m.network.EXPECT().GetRouteTableNumberForENI(defaultNetworkCard, gomock.Any(), trunkDevice, mockContext.maxENI, false).Return(trunkDevice+1, false, nil)
		m.network.EXPECT().SetupENINetwork(gomock.Any(), trunkMAC, defaultNetworkCard, secSubnet, maxENIPerNIC, true, gomock.Any(), gomock.Any()).Return(nil)

		err := mockContext.setupENI(context.Background(), trunkENIid, trunkENIMetadata(), true /* isTrunkENI */, false)
		assert.NoError(t, err)
		assert.True(t, mockContext.trunkENI.isReady())
		assert.Equal(t, trunkMAC, mockContext.trunkENI.mac)
	})

	t.Run("cleared when the trunk's network setup fails", func(t *testing.T) {
		m := setup(t)
		defer m.ctrl.Finish()

		mockContext := trunkENIIPAMContext(m)
		// Pretend an earlier attempt had succeeded, so we can see the flag being cleared.
		mockContext.trunkENI.markReady(trunkENIid, trunkMAC)

		m.awsutils.EXPECT().GetPrimaryENI().Return(primaryENIid)
		m.network.EXPECT().GetRouteTableNumberForENI(defaultNetworkCard, gomock.Any(), trunkDevice, mockContext.maxENI, false).Return(trunkDevice+1, false, nil)
		m.network.EXPECT().SetupENINetwork(gomock.Any(), trunkMAC, defaultNetworkCard, secSubnet, maxENIPerNIC, true, gomock.Any(), gomock.Any()).
			Return(errors.New("simulated trunk setup failure"))

		err := mockContext.setupENI(context.Background(), trunkENIid, trunkENIMetadata(), true /* isTrunkENI */, false)
		assert.Error(t, err)
		assert.False(t, mockContext.trunkENI.isReady(), "a failed trunk setup must not leave the gate open")
		assert.Empty(t, mockContext.trunkENI.mac)
		// And the trunk is no longer in the datastore either, so GetTrunkENI agrees.
		assert.Empty(t, mockContext.dataStoreAccess.GetDataStore(defaultNetworkCard).GetTrunkENI())
	})

	// Defensive branch: a trunk ENI is always a secondary attachment, so this should be
	// unreachable. If it ever is reached, staying silent would reject every SGP pod on the node
	// forever, which is strictly worse than the bug being fixed.
	t.Run("set for a trunk that is somehow also the primary ENI", func(t *testing.T) {
		m := setup(t)
		defer m.ctrl.Finish()

		mockContext := trunkENIIPAMContext(m)
		primaryAsTrunk := trunkENIMetadata()
		primaryAsTrunk.ENIID = primaryENIid

		m.awsutils.EXPECT().GetPrimaryENI().Return(primaryENIid)
		m.network.EXPECT().GetRouteTableNumberForENI(defaultNetworkCard, gomock.Any(), trunkDevice, mockContext.maxENI, false).Return(trunkDevice+1, false, nil)
		// No SetupENINetwork expectation: setupENI skips it for the primary ENI, because ipamd
		// configures the primary link in SetupHostNetwork instead.

		err := mockContext.setupENI(context.Background(), primaryENIid, primaryAsTrunk, true /* isTrunkENI */, false)
		assert.NoError(t, err)
		assert.True(t, mockContext.trunkENI.isReady(), "a primary-ENI trunk must not leave the gate shut forever")
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
		assert.False(t, mockContext.trunkENI.isReady())
	})
}

// addTrunkToDatastore puts the trunk ENI in the datastore, which is what setupENI does before
// it configures the link. The periodic reconciler cross-checks the datastore, so a test that
// only sets the flag is not a realistic state.
func addTrunkToDatastore(t *testing.T, c *IPAMContext) {
	t.Helper()
	require.NoError(t, c.dataStoreAccess.GetDataStore(defaultNetworkCard).AddENI(
		trunkENIid, trunkDevice, false, true, false,
		networkutils.CalculateRouteTableId(trunkDevice, defaultNetworkCard), ""))
}

// TestReconcileVlanMTU_IPAMD covers the periodic reconciler's wiring: it is a no-op until a
// trunk is ready, it resolves the trunk link from the MAC recorded at setup time without any EC2
// or IMDS call, and it gives up on a trunk whose link has disappeared.
func TestReconcileVlanMTU_IPAMD(t *testing.T) {
	t.Run("no-op while no trunk is ready", func(t *testing.T) {
		m := setup(t)
		defer m.ctrl.Finish()

		mockContext := trunkENIIPAMContext(m)
		// A trunk in the datastore is NOT enough: that is also the state in the middle of a
		// normal trunk setup. No GetLinkByMac or ReconcileVlanMTUForTrunk expectation, so any
		// netlink call here fails the test.
		addTrunkToDatastore(t, mockContext)
		mockContext.reconcileVlanMTU()

		assert.False(t, mockContext.trunkENI.isReady(), "the reconciler must never arm the gate itself")
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
		assert.True(t, mockContext.trunkENI.isReady(), "a healthy pass must not disturb the state")
	})

	t.Run("a single trunk link lookup failure does not reject pods", func(t *testing.T) {
		m := setup(t)
		defer m.ctrl.Finish()

		mockContext := trunkENIIPAMContext(m)
		addTrunkToDatastore(t, mockContext)
		mockContext.trunkENI.markReady(trunkENIid, trunkMAC)

		m.network.EXPECT().GetLinkByMac(trunkMAC, gomock.Any()).Return(nil, errors.New("simulated lookup failure"))

		mockContext.reconcileVlanMTU()
		assert.True(t, mockContext.trunkENI.isReady(), "one transient netlink failure must not close the gate")
		assert.Equal(t, 1, mockContext.trunkLinkMissingTicks)
	})

	// A trunk whose link cannot be found must NOT have its readiness cleared. Clearing it buys
	// nothing - AddNetwork already fails for these pods when the link cannot be resolved - and
	// costs a node-wide SGP outage, because nothing re-arms the state: the IPv4 reconcile mark
	// phase skips any ENI already in the datastore, and IPv6 never re-runs setupENI at all.
	t.Run("never clears readiness just because the link lookup keeps failing", func(t *testing.T) {
		m := setup(t)
		defer m.ctrl.Finish()

		mockContext := trunkENIIPAMContext(m)
		mockContext.trunkENI.markReady(trunkENIid, trunkMAC)

		const ticks = 25 // past the log rate limit, so the quiet path is exercised too
		m.network.EXPECT().GetLinkByMac(trunkMAC, gomock.Any()).
			Return(nil, errors.New("simulated lookup failure")).Times(ticks)

		for i := 0; i < ticks; i++ {
			mockContext.reconcileVlanMTU()
		}

		assert.True(t, mockContext.trunkENI.isReady(),
			"a netlink lookup failure must never cost the node its trunk readiness")
		assert.Equal(t, ticks, mockContext.trunkLinkMissingTicks)
		assert.Equal(t, trunkENIid, mockContext.trunkLinkMissingENI)
	})

	// The counter is about a particular trunk. If it were not, a replacement trunk would inherit
	// its predecessor's failures - and nodeIPPoolReconcile's mark-before-sweep order means the
	// identity changes without the state ever passing through empty.
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

		// A replacement trunk is configured, and its first lookup also fails.
		mockContext.trunkENI.markReady(newTrunkENIid, newTrunkMAC)
		m.network.EXPECT().GetLinkByMac(newTrunkMAC, gomock.Any()).Return(nil, errors.New("blip"))
		mockContext.reconcileVlanMTU()

		assert.Equal(t, 1, mockContext.trunkLinkMissingTicks, "the count must restart for the new trunk")
		assert.Equal(t, newTrunkENIid, mockContext.trunkLinkMissingENI)
		assert.True(t, mockContext.trunkENI.isReady())
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
		assert.True(t, mockContext.trunkENI.isReady())
	})
}

// TestNodeIPPoolReconcile_TrunkSweepClearsReadyFlag covers the sweep path. The subtle part is
// that nodeIPPoolReconcile reassigns its local trunkENI to EC2's view of the trunk as soon as any
// new ENI shows up, and EC2's view is "" exactly when the trunk we are sweeping has detached. If
// the sweep compared against that local, the flag would stay set on a trunk that is gone.
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
	require.True(t, mockContext.trunkENI.isReady())
	require.Equal(t, trunkENIid, ds.GetTrunkENI())

	// IMDS now reports the primary ENI and a brand new secondary one. The trunk is gone.
	// The new ENI is what makes nodeIPPoolReconcile call DescribeAllENIs and overwrite its
	// trunkENI local with the (now empty) EC2 trunk.
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
	assert.False(t, mockContext.trunkENI.isReady(), "a swept trunk must clear the readiness flag")
	assert.Empty(t, mockContext.trunkENI.mac)
}

// TestNodeIPPoolReconcile_TrunkReplacementKeepsNewTrunkReady is the regression test for the
// trunk-replacement case. When a trunk is replaced, one nodeIPPoolReconcile pass both adds and
// configures the NEW trunk (add phase) and sweeps the OLD one out (sweep phase), so the datastore
// briefly holds two trunks. Anything that works out "the" trunk by scanning the datastore - which
// is what DataStore.GetTrunkENI does, returning whichever trunk a Go map iteration reaches first -
// can name the new trunk while sweeping the old one, and disarm a trunk that is correctly
// configured and already carrying pods. Nothing re-arms it, so every SGP pod ADD on the node is
// rejected for the life of the process.
func TestNodeIPPoolReconcile_TrunkReplacementKeepsNewTrunkReady(t *testing.T) {
	const oldTrunkENIid = "eni-0000oldtrunk"
	const newTrunkMAC = "12:ef:2a:98:e5:5e"

	// Run it repeatedly: the failure mode depends on Go map iteration order, so a single pass
	// can pass by luck even with the bug present.
	for attempt := 0; attempt < 20; attempt++ {
		m := setup(t)
		ctx := context.Background()

		mockContext := trunkENIIPAMContext(m)
		ds := mockContext.dataStoreAccess.GetDataStore(defaultNetworkCard)
		primaryENIMetadata := getPrimaryENIMetadata()

		require.NoError(t, ds.AddENI(primaryENIid, primaryDevice, true, false, false,
			networkutils.CalculateRouteTableId(primaryDevice, defaultNetworkCard), ""))
		// The old trunk is in the datastore and is the one this process configured.
		require.NoError(t, ds.AddENI(oldTrunkENIid, trunkDevice, false, true, false,
			networkutils.CalculateRouteTableId(trunkDevice, defaultNetworkCard), ""))
		mockContext.trunkENI.markReady(oldTrunkENIid, trunkMAC)

		// IMDS now reports the primary ENI and a NEW trunk; the old trunk is gone.
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

		require.True(t, mockContext.trunkENI.isReady(),
			"attempt %d: the newly configured trunk must still be ready after the old one is swept", attempt)
		eniID, mac := mockContext.trunkENI.readyENI()
		require.Equal(t, trunkENIid, eniID, "attempt %d: readiness must be held for the NEW trunk", attempt)
		require.Equal(t, newTrunkMAC, mac, "attempt %d: and with the NEW trunk's MAC", attempt)
		m.ctrl.Finish()
	}
}

// TestSetupENI_SecondTrunkFailureKeepsFirstTrunkReady: a failed setup of a second trunk must not
// disarm the first one, which is still configured and still carrying pods.
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

	assert.True(t, mockContext.trunkENI.isReady(), "the healthy trunk's readiness must survive another trunk's failure")
	eniID, _ := mockContext.trunkENI.readyENI()
	assert.Equal(t, healthyTrunkENIid, eniID)
}

// TestSetupENI_TrunkNotReadyUntilSetupENINetworkReturns pins the invariant the whole gate rests
// on: the trunk is published to the datastore before its link is configured, so readiness must
// not be announced until SetupENINetwork has actually returned. Asserted from inside the mock,
// because an assertion after setupENI returns cannot tell the two orderings apart.
func TestSetupENI_TrunkNotReadyUntilSetupENINetworkReturns(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()

	mockContext := trunkENIIPAMContext(m)
	md := trunkENIMetadata()

	m.awsutils.EXPECT().GetPrimaryENI().Return(primaryENIid)
	m.network.EXPECT().GetRouteTableNumberForENI(defaultNetworkCard, gomock.Any(), trunkDevice, mockContext.maxENI, false).Return(trunkDevice+1, false, nil)
	m.network.EXPECT().SetupENINetwork(gomock.Any(), trunkMAC, defaultNetworkCard, secSubnet, maxENIPerNIC, true, gomock.Any(), gomock.Any()).
		DoAndReturn(func(_, _ string, _ int, _ string, _ int, _ bool, _ int, _ bool) error {
			// At this instant the trunk IS already in the datastore - that is the defect's
			// window - and the gate must still be shut.
			assert.Equal(t, md.ENIID, mockContext.dataStoreAccess.GetDataStore(defaultNetworkCard).GetTrunkENI(),
				"the trunk should already be in the datastore here, otherwise this test proves nothing")
			assert.False(t, mockContext.trunkENI.isReady(),
				"readiness must not be announced before the trunk's link is configured")
			return nil
		})

	err := mockContext.setupENI(context.Background(), md.ENIID, md, true /* isTrunkENI */, false)
	assert.NoError(t, err)
	assert.True(t, mockContext.trunkENI.isReady())
}

// TestAddNetwork_PodENI_UsesTheConfiguredTrunksLink is the regression test for the unkeyed gate
// read. Readiness used to be a yes/no question - "is some trunk ready?" - while the link index
// handed to the plugin was resolved independently via DataStore.GetTrunkENI, a Go map scan. With
// two trunks in the datastore, one configured and one not, those two answers could name different
// ENIs: the ADD was admitted on the strength of the configured trunk and the plugin was handed
// the unconfigured one's link index, so the pod's VLAN was built on a trunk still at its
// boot-time MTU. That is the exact defect this change exists to prevent.
func TestAddNetwork_PodENI_UsesTheConfiguredTrunksLink(t *testing.T) {
	const configuredTrunk = "eni-0000configured"
	const configuredMAC = "12:ef:2a:98:e5:60"
	const configuredLinkIndex = 11
	const unconfiguredTrunk = "eni-0000unconfigured"

	// Repeat: with the unkeyed read the failure depended on map iteration order.
	for attempt := 0; attempt < 20; attempt++ {
		m := setup(t)

		mockContext := trunkENIIPAMContext(m)
		ds := mockContext.dataStoreAccess.GetDataStore(defaultNetworkCard)
		// Both trunks are in the datastore. Only the first one was configured.
		require.NoError(t, ds.AddENI(configuredTrunk, trunkDevice, false, true, false,
			networkutils.CalculateRouteTableId(trunkDevice, defaultNetworkCard), ""))
		require.NoError(t, ds.AddENI(unconfiguredTrunk, trunkDevice+1, false, true, false,
			networkutils.CalculateRouteTableId(trunkDevice+1, defaultNetworkCard), ""))
		mockContext.trunkENI.markReady(configuredTrunk, configuredMAC)

		pod := branchENIPod(fmt.Sprintf("sgp-pod-two-trunks-%d", attempt))
		require.NoError(t, m.k8sClient.Create(context.TODO(), pod))

		// Only the CONFIGURED trunk's MAC may be looked up. A lookup of anything else - which is
		// what resolving via the datastore would do - fails the test as an unexpected call.
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
