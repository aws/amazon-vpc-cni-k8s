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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var log = logger.Get()
var MyNodeName = os.Getenv("MY_NODE_NAME")
var MyPodName = os.Getenv("MY_POD_NAME")

const (
	EventReason = sgpp.VpcCNIEventReason
)

type EventRecorder struct {
	Recorder        events.EventRecorder
	RawK8SClient    client.Client
	CachedK8SClient client.Client
	HostID          string
	hostPod         corev1.Pod
	hostNode        corev1.Node
}

func New(rawK8SClient, cachedK8SClient client.Client) (*EventRecorder, error) {
	clientSet, err := k8sapi.GetKubeClientSet()
	if err != nil {
		log.Fatalf("Error Fetching Kubernetes Client: %s", err)
		return nil, err
	}
	eventBroadcaster := events.NewBroadcaster(&events.EventSinkImpl{
		Interface: clientSet.EventsV1(),
	})
	stopCh := make(chan struct{})
	eventBroadcaster.StartRecordingToSink(stopCh)

	eventRecorder := &EventRecorder{}
	eventRecorder.Recorder = eventBroadcaster.NewRecorder(clientgoscheme.Scheme, "aws-node")
	eventRecorder.RawK8SClient = rawK8SClient
	eventRecorder.CachedK8SClient = cachedK8SClient

	if eventRecorder.hostNode, err = findMyNode(eventRecorder.CachedK8SClient); err != nil {
		log.Errorf("Failed to find host node: %s", err)
	}

	if eventRecorder.hostPod, err = findMyPod(eventRecorder.CachedK8SClient); err != nil {
		log.Errorf("Failed to find host aws-node pod: %s", err)
	}

	return eventRecorder, nil

}

// SendPodEvent will raise event on aws-node with given type, reason, & message
func (e *EventRecorder) SendPodEvent(eventType, reason, message string) {
	log.Infof("SendPodEvent")

	e.Recorder.Eventf(&e.hostPod, nil, eventType, reason, "", message)
	log.Debugf("Sent pod event: eventType: %s, reason: %s, message: %s", eventType, reason, message)
}

func findMyNode(cachedK8SClient client.Client) (corev1.Node, error) {
	var node corev1.Node
	// Find my node
	err := cachedK8SClient.Get(context.TODO(), types.NamespacedName{Name: MyNodeName}, &node)
	if err != nil {
		log.Errorf("Cached client failed GET node (%s)", MyNodeName)
	} else {
		log.Debugf("Node found %s - labels - %d", node.Name, len(node.Labels))
	}
	return node, err
}

func findMyPod(cachedK8SClient client.Client) (corev1.Pod, error) {
	var pod corev1.Pod
	// Find my aws-node pod
	err := cachedK8SClient.Get(context.TODO(), types.NamespacedName{Name: MyPodName, Namespace: utils.AwsNodeNamespace}, &pod)
	if err != nil {
		log.Errorf("Cached client failed GET pod (%s)", MyPodName)
	} else {
		log.Debugf("Node found %s - labels - %d", pod.Name, len(pod.Labels))
	}
	return pod, err
}

// SendNodeEvent sends an event regarding pod object and related to node object
func (e *EventRecorder) SendNodeEvent(eventType, reason, action, message string) error {
	instance := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:      e.hostNode.Name,
			Namespace: e.hostNode.Namespace,
			UID:       types.UID(e.HostID),
		},
	}

	// make the event unique avoid to be aggregated by event broadcaster
	action = action + ": " + string(uuid.NewUUID())
	e.Recorder.Eventf(&e.hostPod, instance, eventType, reason, action, message)
	log.Debugf("Sent sgp event: eventType: %s, reason: %s, message: %s, action %s", eventType, reason, message, action)

	return nil
}
