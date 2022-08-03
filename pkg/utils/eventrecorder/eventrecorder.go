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
	"errors"
	"os"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/k8sapi"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var log = logger.Get()
var myNodeName = os.Getenv("MY_NODE_NAME")
var eventRecorder *EventRecorder

const (
	awsNode      = "aws-node"
	specNodeName = "spec.nodeName"
	labelK8sapp  = "k8s-app"
)

type EventRecorder struct {
	recorder  record.EventRecorder
	k8sClient client.Client
}

func InitEventRecorder(k8sClient client.Client) error {

	clientSet, err := k8sapi.GetKubeClientSet()
	if err != nil {
		log.Fatalf("Error Fetching Kubernetes Client: %s", err)
		return err
	}
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{
		Interface: clientSet.CoreV1().Events(""),
	})

	recorder := &EventRecorder{}
	recorder.recorder = eventBroadcaster.NewRecorder(clientgoscheme.Scheme, corev1.EventSource{
		Component: awsNode,
		Host:      myNodeName,
	})
	recorder.k8sClient = k8sClient
	eventRecorder = recorder
	return nil
}

func Get() *EventRecorder {
	if eventRecorder == nil {
		err := errors.New("error fetching event recoder, not initialized")
		panic(err.Error())
	}
	return eventRecorder
}

// BroadcastEvent will raise event on aws-node with given type, reason, & message
func (e *EventRecorder) BroadcastEvent(eventType, reason, message string) {

	// Get aws-node pod objects with label & field selectors
	labelSelector := labels.SelectorFromSet(labels.Set{labelK8sapp: awsNode})
	fieldSelector := fields.SelectorFromSet(fields.Set{specNodeName: myNodeName})
	listOptions := client.ListOptions{
		LabelSelector: labelSelector,
		FieldSelector: fieldSelector,
	}

	var podList corev1.PodList
	err := e.k8sClient.List(context.TODO(), &podList, &listOptions)
	if err != nil {
		log.Errorf("Failed to get pods, cannot broadcast events: %v", err)
		return
	}
	for _, pod := range podList.Items {
		log.Debugf("Broadcasting event on pod %s", pod.Name)
		e.recorder.Event(&pod, eventType, reason, message)
	}
}
