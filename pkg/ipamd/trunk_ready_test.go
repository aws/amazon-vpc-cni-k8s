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

	// getTrunkLinkIndex would succeed: the link exists. Nothing but the readiness flag can tell
	// us the trunk is not configured yet, so allow these calls without requiring them.
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
	mockContext.trunkENI.markReady(trunkMAC)

	pod := branchENIPod("sgp-pod-trunk-ready")
	require.NoError(t, m.k8sClient.Create(context.TODO(), pod))

	m.awsutils.EXPECT().GetAttachedENIs().Return([]awsutils.ENIMetadata{trunkENIMetadata()}, nil)
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
		assert.Equal(t, trunkMAC, mockContext.trunkENI.readyMAC())
	})

	t.Run("cleared when the trunk's network setup fails", func(t *testing.T) {
		m := setup(t)
		defer m.ctrl.Finish()

		mockContext := trunkENIIPAMContext(m)
		// Pretend an earlier attempt had succeeded, so we can see the flag being cleared.
		mockContext.trunkENI.markReady(trunkMAC)

		m.awsutils.EXPECT().GetPrimaryENI().Return(primaryENIid)
		m.network.EXPECT().GetRouteTableNumberForENI(defaultNetworkCard, gomock.Any(), trunkDevice, mockContext.maxENI, false).Return(trunkDevice+1, false, nil)
		m.network.EXPECT().SetupENINetwork(gomock.Any(), trunkMAC, defaultNetworkCard, secSubnet, maxENIPerNIC, true, gomock.Any(), gomock.Any()).
			Return(errors.New("simulated trunk setup failure"))

		err := mockContext.setupENI(context.Background(), trunkENIid, trunkENIMetadata(), true /* isTrunkENI */, false)
		assert.Error(t, err)
		assert.False(t, mockContext.trunkENI.isReady(), "a failed trunk setup must not leave the gate open")
		assert.Empty(t, mockContext.trunkENI.readyMAC())
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
		assert.Equal(t, trunkMAC, mockContext.trunkENI.readyMAC())
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

// TestReconcileVlanMTU_IPAMD covers the periodic reconciler's wiring: it is a no-op until the
// trunk is ready, it resolves the trunk link from the MAC recorded at setup time without any
// EC2 or IMDS call, and it keeps the readiness flag consistent with the datastore.
func TestReconcileVlanMTU_IPAMD(t *testing.T) {
	t.Run("no-op while the trunk is not ready", func(t *testing.T) {
		m := setup(t)
		defer m.ctrl.Finish()

		mockContext := trunkENIIPAMContext(m)
		// No GetLinkByMac or ReconcileVlanMTUForTrunk expectation: any call fails the test.
		mockContext.reconcileVlanMTU()
	})

	t.Run("reconciles the VLANs under the configured trunk", func(t *testing.T) {
		m := setup(t)
		defer m.ctrl.Finish()

		mockContext := trunkENIIPAMContext(m)
		addTrunkToDatastore(t, mockContext)
		mockContext.trunkENI.markReady(trunkMAC)

		m.network.EXPECT().GetLinkByMac(trunkMAC, gomock.Any()).Return(
			&netlink.Device{LinkAttrs: netlink.LinkAttrs{Name: "eth1", Index: trunkLinkIndex}}, nil)
		m.network.EXPECT().ReconcileVlanMTUForTrunk(trunkLinkIndex).Return(2, nil)

		mockContext.reconcileVlanMTU()
	})

	t.Run("survives a trunk link lookup failure", func(t *testing.T) {
		m := setup(t)
		defer m.ctrl.Finish()

		mockContext := trunkENIIPAMContext(m)
		addTrunkToDatastore(t, mockContext)
		mockContext.trunkENI.markReady(trunkMAC)

		m.network.EXPECT().GetLinkByMac(trunkMAC, gomock.Any()).Return(nil, errors.New("simulated lookup failure"))

		mockContext.reconcileVlanMTU()
	})

	// This is the IPv6 hole: nodeIPPoolReconcile, which owns the sweep that clears the flag, is
	// never reached on an IPv6 node, so the reconciler has to notice a departed trunk itself.
	// Without this, an IPv6 node whose trunk detached keeps a stale-ready flag and warns about a
	// dead MAC on every tick for the life of the process.
	t.Run("clears a stale ready flag when the trunk has left the datastore", func(t *testing.T) {
		m := setup(t)
		defer m.ctrl.Finish()

		mockContext := trunkENIIPAMContext(m)
		mockContext.trunkENI.markReady(trunkMAC) // ready, but no trunk in the datastore
		require.True(t, mockContext.trunkENI.isReady())

		// No GetLinkByMac expectation: looking up the dead MAC would fail the test.
		mockContext.reconcileVlanMTU()

		assert.False(t, mockContext.trunkENI.isReady())
		assert.Empty(t, mockContext.trunkENI.readyMAC())
	})

	// The mirror image, and a node-wide SGP outage if it ever happens: the trunk is in the
	// datastore but this process never finished configuring it, which means setupENI's
	// compensating RemoveENIFromDataStore failed. Nothing re-runs setupENI for an ENI already in
	// the datastore, so the reconciler's job here is to make the outage loud rather than silent.
	t.Run("does not touch netlink, and does not self-arm, when the trunk was never configured", func(t *testing.T) {
		m := setup(t)
		defer m.ctrl.Finish()

		mockContext := trunkENIIPAMContext(m)
		addTrunkToDatastore(t, mockContext) // in the datastore, flag never set

		// No GetLinkByMac and no ReconcileVlanMTUForTrunk: we must not touch a link we never set up.
		mockContext.reconcileVlanMTU()

		assert.False(t, mockContext.trunkENI.isReady(), "the reconciler must never arm the gate itself")
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
	mockContext.trunkENI.markReady(trunkMAC)
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
	assert.Empty(t, mockContext.trunkENI.readyMAC())
}
