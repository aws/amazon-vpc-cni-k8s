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

	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/events"
)

var fakeRecorder *events.FakeRecorder

func setup(t *testing.T) *gomock.Controller {
	fakeRecorder = InitMockEventRecorder()
	return gomock.NewController(t)
}

func TestSendPodEvent(t *testing.T) {
	ctrl := setup(t)
	defer ctrl.Finish()
	ctx := context.Background()
	MyPodName = "aws-node-5test"
	mockEventRecorder := Get()

	labels := map[string]string{"k8s-app": "aws-node"}
	pod := v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      MyPodName,
			Namespace: utils.AwsNodeNamespace,
			Labels:    labels,
		},
	}
	// Create pod
	mockEventRecorder.K8sClient.Create(ctx, &pod)

	// Validate event call for missing permissions case
	action := "ec2:DescribeNetworkInterfaces"
	reason := "MissingIAMPermission"
	msg := "Failed to call ec2:DescribeNetworkInterfaces due to missing permissions. Please refer to https://github.com/aws/amazon-vpc-cni-k8s/blob/master/docs/iam-policy.md"
	mockEventRecorder.SendPodEvent(v1.EventTypeWarning, reason, action, msg)
	assert.Len(t, fakeRecorder.Events, 1)

	expected := fmt.Sprintf("%s %s %s", v1.EventTypeWarning, reason, msg)
	got := <-fakeRecorder.Events
	assert.Equal(t, expected, got)
}
