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
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var log = logger.Get()
var myNodeName = os.Getenv("MY_NODE_NAME")
var MyPodName = os.Getenv("MY_POD_NAME")
var eventRecorder *EventRecorder

const (
	awsNode = "aws-node"
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

func (e *EventRecorder) findMyPod(ctx context.Context) (corev1.Pod, error) {
	log.Infof("findMyPod, namespace: %s, podName: %s", utils.AwsNodeNamespace, MyPodName)
	var pod corev1.Pod
	// Find my pod
	err := e.k8sClient.Get(ctx, types.NamespacedName{Namespace: utils.AwsNodeNamespace, Name: MyPodName}, &pod)
	if err != nil {
		log.Errorf("Cached client failed GET pod (%s)", MyPodName)
	}
	return pod, err
}

// BroadcastEvent will raise event on aws-node with given type, reason, & message
func (e *EventRecorder) BroadcastEvent(eventType, reason, message string) {
	log.Infof("BroadcastEvent")
	// Find my pod
	pod, err := e.findMyPod(context.TODO())
	if err != nil {
		log.Errorf("Failed to get pod: %v", err)
		return
	}

	e.recorder.Eventf(&pod, eventType, reason, message)
	log.Debugf("Sent pod event: eventType: %s, reason: %s, message: %s", eventType, reason, message)
}
