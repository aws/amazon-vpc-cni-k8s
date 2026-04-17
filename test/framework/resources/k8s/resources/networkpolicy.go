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

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type NetworkPolicyManager interface {
	CreateNetworkPolicy(networkPolicy client.Object) error
	DeleteNetworkPolicy(networkPolicy client.Object) error
}

type defaultNetworkPolicyManager struct {
	networkPolicyClient client.Client
}

func NewNetworkPolicyManager(client client.Client) NetworkPolicyManager {
	return &defaultNetworkPolicyManager{client}
}

func (d *defaultNetworkPolicyManager) CreateNetworkPolicy(networkPolicy client.Object) error {
	return d.networkPolicyClient.Create(context.Background(), networkPolicy)
}

func (d *defaultNetworkPolicyManager) DeleteNetworkPolicy(networkPolicy client.Object) error {
	return d.networkPolicyClient.Delete(context.Background(), networkPolicy)
}
