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

	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type NodeManager interface {
	GetNodes(nodeLabelKey string, nodeLabelVal string) (v1.NodeList, error)
	GetAllNodes() (v1.NodeList, error)
	UpdateNode(oldNode *v1.Node, newNode *v1.Node) error
	WaitTillNodesReady(nodeLabelKey string, nodeLabelVal string, asgSize int) error
}

type defaultNodeManager struct {
	k8sClient client.Client
}

func NewDefaultNodeManager(k8sClient client.Client) NodeManager {
	return &defaultNodeManager{k8sClient: k8sClient}
}

func (d *defaultNodeManager) GetNodes(nodeLabelKey string, nodeLabelVal string) (v1.NodeList, error) {
	if nodeLabelVal == "" {
		return d.GetAllNodes()
	}
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

func (d *defaultNodeManager) UpdateNode(oldNode *v1.Node, newNode *v1.Node) error {
	return d.k8sClient.Patch(context.Background(), newNode, client.MergeFrom(oldNode))
}

func (d *defaultNodeManager) WaitTillNodesReady(nodeLabelKey string, nodeLabelVal string, asgSize int) error {
	return wait.PollImmediateUntil(utils.PollIntervalLong, func() (done bool, err error) {
		nodeList, err := d.GetNodes(nodeLabelKey, nodeLabelVal)
		if err != nil {
			return false, err
		}
		if len(nodeList.Items) != asgSize {
			return false, nil
		}
		for _, node := range nodeList.Items {
			for _, condition := range node.Status.Conditions {
				if condition.Type == v1.NodeReady && condition.Status != v1.ConditionTrue {
					return false, nil
				}
			}
		}

		return true, nil

	}, context.Background().Done())
}
