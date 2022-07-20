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
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ConfigMapManager interface {
	GetConfigMap(namespace string, name string) (*v1.ConfigMap, error)
	UpdateConfigMap(oldConfigMap *v1.ConfigMap, newConfigMap *v1.ConfigMap) error
}

type defaultConfigMapManager struct {
	k8sClient client.Client
}

func (d defaultConfigMapManager) GetConfigMap(namespace string, name string) (*v1.ConfigMap, error) {
	configMap := v1.ConfigMap{}
	return &configMap, d.k8sClient.Get(context.Background(), types.
		NamespacedName{Name: name, Namespace: namespace}, &configMap)
}

func (d defaultConfigMapManager) UpdateConfigMap(oldConfigMap *v1.ConfigMap, newConfigMap *v1.ConfigMap) error {
	ctx := context.Background()
	return d.k8sClient.Patch(ctx, newConfigMap, client.MergeFrom(oldConfigMap))
}

func NewConfigMapManager(k8sClient client.Client) ConfigMapManager {
	return &defaultConfigMapManager{k8sClient: k8sClient}
}
