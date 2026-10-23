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

// TestReconcileVlanMTU_IPAMD covers the periodic reconciler's wiring: it is a no-op until the
// trunk is ready, and otherwise resolves the trunk link from the MAC recorded at setup time
// without any EC2 or IMDS call.
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
		mockContext.trunkENI.markReady(trunkMAC)

		m.network.EXPECT().GetLinkByMac(trunkMAC, gomock.Any()).Return(nil, errors.New("simulated lookup failure"))

		mockContext.reconcileVlanMTU()
	})
}
