// Copyright 2018 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

package main

import (
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"

	"github.com/golang/mock/gomock"

	"github.com/aws/amazon-vpc-cni-k8s/controller/vpcipresource"
	"github.com/aws/amazon-vpc-cni-k8s/controller/vpcipresource/mocks"
)

var (
	alwaysReady = func() bool { return true }
)

type eniIPController struct {
	controller *ENIIPController
	nodeStore  cache.Store
}

func newTestController(t *testing.T, initialObjects ...runtime.Object) (*eniIPController,
	*fake.Clientset,
	*mock_vpcipresource.MockVPCIPResourceInterface,
	error) {
	clientset := fake.NewSimpleClientset(initialObjects...)
	informerFactory := informers.NewSharedInformerFactory(clientset, 0)

	ctrl := gomock.NewController(t)
	fakeVPCIP := mock_vpcipresource.NewMockVPCIPResourceInterface(ctrl)

	c := NewENIIPController(clientset, fakeVPCIP, "dummyNode")

	return &eniIPController{
		controller: c,
		nodeStore:  informerFactory.Core().V1().Nodes().Informer().GetStore(),
	}, clientset, fakeVPCIP, nil
}

func newNode(name string, label map[string]string) *v1.Node {
	return &v1.Node{
		TypeMeta: metav1.TypeMeta{APIVersion: "extensions/v1beta1"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Labels:    label,
			Namespace: metav1.NamespaceDefault,
		},
		Status: v1.NodeStatus{
			Conditions: []v1.NodeCondition{
				{Type: v1.NodeReady, Status: v1.ConditionTrue},
			},
			Allocatable: v1.ResourceList{
				v1.ResourcePods: resource.MustParse("100"),
			},
		},
	}
}

func addNode(nodeStore cache.Store, instanceType string, key string) *v1.Node {
	label := map[string]string{instanceTypeLabel: instanceType}
	node := newNode(key, label)
	nodeStore.Add(node)
	return node
}

func TestAddNode(t *testing.T) {
	manager, _, mockVPCIP, err := newTestController(t)
	assert.NoError(t, err)

	// Happy Path testing
	insType := "c1.medium"
	nodeName := "node-1"
	label := map[string]string{instanceTypeLabel: insType}
	node := newNode(nodeName, label)
	err = manager.controller.indexer.Add(node)
	assert.NoError(t, err)

	key := metav1.NamespaceDefault + "/" + nodeName

	availableIPs := vpcipresource.InstanceENIsAvailable[insType] * (vpcipresource.InstanceIPsAvailable[insType] - 1)
	mockVPCIP.EXPECT().Update(gomock.Any(), nodeName, availableIPs)

	err = manager.controller.nodeENIIPController(key)
	assert.NoError(t, err)

	// Error
	mockVPCIP.EXPECT().Update(gomock.Any(), nodeName, availableIPs).Return(errors.New("Error"))
	err = manager.controller.nodeENIIPController(key)
	assert.Error(t, err)
}
