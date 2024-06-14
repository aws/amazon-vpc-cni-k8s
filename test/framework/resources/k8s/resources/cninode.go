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

	rcv1alpha1 "github.com/aws/amazon-vpc-resource-controller-k8s/apis/vpcresources/v1alpha1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type CNINodeManager interface {
	GetCNINode(nodeName string) (*rcv1alpha1.CNINode, error)
}

type defaultCNINodeManager struct {
	k8sClient client.Client
}

func (c defaultCNINodeManager) GetCNINode(nodeName string) (*rcv1alpha1.CNINode, error) {
	cniNode := &rcv1alpha1.CNINode{}
	err := c.k8sClient.Get(context.Background(), types.NamespacedName{Name: nodeName}, cniNode)
	return cniNode, err

}

func NewCNINodeManager(k8sClient client.Client) CNINodeManager {
	return &defaultCNINodeManager{k8sClient: k8sClient}
}
