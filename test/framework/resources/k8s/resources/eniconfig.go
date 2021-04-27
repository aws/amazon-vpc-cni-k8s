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

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type CustomResourceManager interface {
	CreateResource(resource runtime.Object) error
	DeleteResource(resource runtime.Object) error
}

type defaultCustomResourceManager struct {
	k8sClient client.DelegatingClient
}

func NewCustomResourceManager(k8sClient client.DelegatingClient) CustomResourceManager {
	return &defaultCustomResourceManager{k8sClient: k8sClient}
}

func (d *defaultCustomResourceManager) CreateResource(resource runtime.Object) error {
	ctx := context.Background()
	return d.k8sClient.Create(ctx, resource)
}

func (d *defaultCustomResourceManager) DeleteResource(resource runtime.Object) error {
	ctx := context.Background()
	return d.k8sClient.Delete(ctx, resource)
}
