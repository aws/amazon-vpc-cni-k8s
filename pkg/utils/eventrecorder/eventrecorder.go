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

// Package event recorder is used to raise events on aws-node pods
package eventrecorder

import (
	"context"
	"os"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/k8sapi"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/sgpp"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	testclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var log = logger.Get()
var MyNodeName = os.Getenv("MY_NODE_NAME")
var MyPodName = os.Getenv("MY_POD_NAME")

// Global variable for EventRecorder allows dependent packages to simply call Get
var eventRecorder *EventRecorder

const (
	EventReason = sgpp.VpcCNIEventReason
	appName     = "aws-node"
)

type EventRecorder struct {
	Recorder  events.EventRecorder
	K8sClient client.Client
	hostPod   corev1.Pod
}

func Init(k8sClient client.Client) error {
	clientSet, err := k8sapi.GetKubeClientSet()
	if err != nil {
		log.Fatalf("Error Fetching Kubernetes Client: %s", err)
		return err
	}
	eventBroadcaster := events.NewBroadcaster(&events.EventSinkImpl{
		Interface: clientSet.EventsV1(),
	})
	stopCh := make(chan struct{})
	eventBroadcaster.StartRecordingToSink(stopCh)

	eventRecorder = &EventRecorder{}
	eventRecorder.Recorder = eventBroadcaster.NewRecorder(clientgoscheme.Scheme, "aws-node")
	eventRecorder.K8sClient = k8sClient

	if eventRecorder.hostPod, err = findMyPod(eventRecorder.K8sClient); err != nil {
		log.Errorf("Failed to find host aws-node pod: %s", err)
		// EventRecorder is not considered critical, so no error is returned if host pod cannot be queried
	}
	return nil
}

func Get() *EventRecorder {
	return eventRecorder
}

// SendPodEvent will raise event on aws-node with given type, reason, & message
func (e *EventRecorder) SendPodEvent(eventType, reason, action, message string) {
	log.Infof("SendPodEvent")

	e.Recorder.Eventf(&e.hostPod, nil, eventType, reason, action, message)
	log.Debugf("Sent pod event: eventType: %s, reason: %s, message: %s", eventType, reason, message)
}

func findMyPod(k8sClient client.Client) (corev1.Pod, error) {
	var pod corev1.Pod
	// Find my aws-node pod
	err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: MyPodName, Namespace: utils.AwsNodeNamespace}, &pod)
	if err != nil {
		log.Errorf("Client failed to GET pod (%s)", MyPodName)
	} else {
		log.Debugf("Node found %s - labels - %d", pod.Name, len(pod.Labels))
	}
	return pod, err
}

// Functions used for mocking package
func InitMockEventRecorder() *events.FakeRecorder {
	fakeRecorder := events.NewFakeRecorder(3)
	k8sSchema := runtime.NewScheme()
	clientgoscheme.AddToScheme(k8sSchema)

	eventRecorder = &EventRecorder{
		Recorder:  fakeRecorder,
		K8sClient: testclient.NewClientBuilder().WithScheme(k8sSchema).Build(),
	}
	return fakeRecorder
}
