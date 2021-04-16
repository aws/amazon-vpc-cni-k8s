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

package resources

import (
	"context"

	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type NodeManager interface {
	GetNodes(nodeLabelKey string, nodeLabelVal string) (v1.NodeList, error)
	GetAllNodes() (v1.NodeList, error)
}

type defaultNodeManager struct {
	k8sClient client.DelegatingClient
}

func NewDefaultNodeManager(k8sClient client.DelegatingClient) NodeManager {
	return &defaultNodeManager{k8sClient: k8sClient}
}

func (d *defaultNodeManager) GetNodes(nodeLabelKey string, nodeLabelVal string) (v1.NodeList, error) {
	ctx := context.Background()
	nodeList := v1.NodeList{}
	err := d.k8sClient.List(ctx, &nodeList, client.MatchingLabels{
		nodeLabelKey: nodeLabelVal,
	})
	return nodeList, err
}

func (d *defaultNodeManager) GetAllNodes() (v1.NodeList, error) {
	ctx := context.Background()
	nodeList := v1.NodeList{}
	err := d.k8sClient.List(ctx, &nodeList)
	return nodeList, err
}
