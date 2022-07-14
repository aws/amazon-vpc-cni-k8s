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
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	testclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var ctrl *gomock.Controller
var fakeRecorder *record.FakeRecorder

type testMocks struct {
	ctrl          *gomock.Controller
	mockK8sClient client.Client
}

func setup(t *testing.T) *testMocks {
	ctrl = gomock.NewController(t)
	k8sSchema := runtime.NewScheme()
	k8sClient := testclient.NewFakeClientWithScheme(k8sSchema)
	clientgoscheme.AddToScheme(k8sSchema)

	return &testMocks{
		ctrl:          ctrl,
		mockK8sClient: k8sClient,
	}
}

func TestBroadcastEvents(t *testing.T) {
	m := setup(t)
	defer m.ctrl.Finish()
	ctx := context.Background()

	fakeRecorder = record.NewFakeRecorder(3)
	mockEventRecorder := &EventRecorder{
		recorder:  fakeRecorder,
		k8sClient: m.mockK8sClient,
	}

	labels := map[string]string{"k8s-app": "aws-node"}

	pods := []v1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "mockPodWithLabelAndSpec",
				Labels: labels,
			},
			Spec: v1.PodSpec{
				NodeName: myNodeName,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "mockPodWithSpec",
			},
			Spec: v1.PodSpec{
				NodeName: myNodeName,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "mockPod",
			},
		},
		// No pod with only label selector in test- this will raise event as fake client does not filter on fields
	}

	//Create above fake pods
	for _, mockPod := range pods {
		_ = mockEventRecorder.k8sClient.Create(ctx, &mockPod)
	}

	// Testing missing permissions event case: failed to call
	reason := "MissingIAMPermission"
	msg := "Failed to call ec2:DescribeNetworkInterfaces due to missing permissions. Please refer to https://github.com/aws/amazon-vpc-cni-k8s/blob/master/docs/iam-policy.md"
	mockEventRecorder.BroadcastEvent(v1.EventTypeWarning, reason, msg)
	assert.Len(t, fakeRecorder.Events, 1) // event should be recorded only on pod with req label selector & pod spec

	expected := fmt.Sprintf("%s %s %s", v1.EventTypeWarning, reason, msg)
	got := <-fakeRecorder.Events
	assert.Equal(t, expected, got)
}
