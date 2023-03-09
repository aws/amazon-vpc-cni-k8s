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

package eventrecorder

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/sgpp"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var ctrl *gomock.Controller
var fakeRecorder *events.FakeRecorder

type testMocks struct {
	ctrl                *gomock.Controller
	mockK8SClient       client.Client
	mockCachedK8SClient client.Client
}

func setup(t *testing.T) *testMocks {
	ctrl = gomock.NewController(t)
	k8sSchema := runtime.NewScheme()
	k8sClient := fake.NewFakeClientWithScheme(k8sSchema)
	cachedK8SClient := fake.NewFakeClientWithScheme(k8sSchema)
	clientgoscheme.AddToScheme(k8sSchema)

	return &testMocks{
		ctrl:                ctrl,
		mockK8SClient:       k8sClient,
		mockCachedK8SClient: cachedK8SClient,
	}
}

func TestSendPodEvent(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()
	ctx := context.Background()
	MyPodName = "aws-node-5test"

	fakeRecorder = events.NewFakeRecorder(3)
	mockEventRecorder := &EventRecorder{
		Recorder:        fakeRecorder,
		RawK8SClient:    m.mockK8SClient,
		CachedK8SClient: m.mockCachedK8SClient,
	}

	labels := map[string]string{"k8s-app": "aws-node"}
	pod := v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      MyPodName,
			Namespace: utils.AwsNodeNamespace,
			Labels:    labels,
		},
	}

	// Create pod
	mockEventRecorder.CachedK8SClient.Create(ctx, &pod)
	mockEventRecorder.RawK8SClient.Create(ctx, &pod)

	// Validate event call for missing permissions case
	reason := "MissingIAMPermission"
	msg := "Failed to call ec2:DescribeNetworkInterfaces due to missing permissions. Please refer to https://github.com/aws/amazon-vpc-cni-k8s/blob/master/docs/iam-policy.md"
	mockEventRecorder.SendPodEvent(v1.EventTypeWarning, reason, msg)
	assert.Len(t, fakeRecorder.Events, 1)

	expected := fmt.Sprintf("%s %s %s", v1.EventTypeWarning, reason, msg)
	got := <-fakeRecorder.Events
	assert.Equal(t, expected, got)
}

func TestSendNodeEvent(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()
	ctx := context.Background()
	MyNodeName = "test-node"

	fakeRecorder = events.NewFakeRecorder(3)
	mockEventRecorder := &EventRecorder{
		Recorder:        fakeRecorder,
		RawK8SClient:    m.mockK8SClient,
		CachedK8SClient: m.mockCachedK8SClient,
	}

	node := v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: MyNodeName,
		},
	}

	labels := map[string]string{"k8s-app": "aws-node"}

	pod := v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "mockPodWithLabelAndSpec",
			Labels: labels,
		},
		Spec: v1.PodSpec{
			NodeName: MyNodeName,
		},
	}

	mockEventRecorder.CachedK8SClient.Create(ctx, &node)
	mockEventRecorder.RawK8SClient.Create(ctx, &pod)
	reason := sgpp.VpcCNIEventReason
	msg := sgpp.TrunkEventNote
	action := sgpp.VpcCNINodeEventActionForTrunk
	mockEventRecorder.SendNodeEvent(v1.EventTypeNormal, reason, action, msg)
	assert.Len(t, fakeRecorder.Events, 1)

	sgpEvent := <-fakeRecorder.Events
	assert.True(t, strings.Contains(sgpEvent, reason) && strings.Contains(sgpEvent, msg))
}
