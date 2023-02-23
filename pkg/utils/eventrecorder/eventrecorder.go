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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var log = logger.Get()
var MyNodeName = os.Getenv("MY_NODE_NAME")
var MyPodName = os.Getenv("MY_POD_NAME")

const (
	PodNamespace = "kube-system"
	EventReason  = sgpp.VpcCNIEventReason
)

type EventRecorder struct {
	Recorder        events.EventRecorder
	RawK8SClient    client.Client
	CachedK8SClient client.Client
	HostID          string
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

	return eventRecorder, nil
}

func (e *EventRecorder) findMyPod(ctx context.Context) (corev1.Pod, error) {
	log.Infof("findMyPod, namespace: %s, podName: %s", PodNamespace, MyPodName)
	var pod corev1.Pod
	log.Infof("pod addr: %s", &pod)
	// Find my pod
	err := e.CachedK8SClient.Get(ctx, types.NamespacedName{Namespace: PodNamespace, Name: MyPodName}, &pod)
	if err != nil {
		log.Errorf("Cached client failed GET pod (%s)", MyPodName)
	} else {
		log.Infof("pod found: %v", pod)
		log.Debugf("Pod found: %s", pod.Name)
	}
	return pod, err
}

// BroadcastEvent will raise event on aws-node with given type, reason, & message
func (e *EventRecorder) BroadcastPodEvent(eventType, reason, message string) {
	log.Infof("BroadcastPodEvent")
	// Find my pod
	pod, err := e.findMyPod(context.TODO())
	if err != nil {
		log.Errorf("Failed to get pod: %v", err)
		return
	}

	e.Recorder.Eventf(&pod, nil, eventType, reason, "", message)
	log.Debugf("Sent pod event: eventType: %s, reason: %s, message: %s", eventType, reason, message)
}

func (e *EventRecorder) findMyNode(ctx context.Context) (corev1.Node, error) {
	var node corev1.Node
	// Find my node
	err := e.CachedK8SClient.Get(ctx, types.NamespacedName{Name: MyNodeName}, &node)
	if err != nil {
		log.Errorf("Cached client failed GET node (%s)", MyNodeName)
	} else {
		log.Debugf("Node found %s - labels - %d", node.Name, len(node.Labels))
	}
	return node, err
}

// SendNodeEvent sends an event regarding node object
func (e *EventRecorder) SendNodeEvent(eventType, reason, action, message string) error {
	// Find my node
	node, err := e.findMyNode(context.TODO())
	if err != nil {
		log.Errorf("Failed to get node: %v", err)
		return err
	}

	// make a copy before modifying the UID
	// Note: kubectl uses the filter involvedObject.uid=NodeName to fetch the events
	// that are listed in 'kubectl describe node' output. So setting the node UID to
	// nodename before sending the event
	nodeCopy := node.DeepCopy()
	nodeCopy.SetUID(types.UID(e.HostID))

	e.Recorder.Eventf(nodeCopy, nil, eventType, reason, action, message)
	log.Debugf("Sent node event: eventType: %s, reason: %s, action: %s, message: %s", eventType, reason, action, message)

	return nil
}
