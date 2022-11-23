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
	"k8s.io/apimachinery/pkg/api/errors"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type NamespaceManager interface {
	CreateNamespace(namespace string) error
	CreateNamespaceWithLabels(namespace string, labels map[string]string) error
	DeleteAndWaitTillNamespaceDeleted(namespace string) error

	WaitUntilNamespaceDeleted(ctx context.Context, ns *v1.Namespace) error
}

type defaultNamespaceManager struct {
	k8sClient client.Client
}

func NewDefaultNamespaceManager(k8sClient client.Client) NamespaceManager {
	return &defaultNamespaceManager{k8sClient: k8sClient}
}

func (m *defaultNamespaceManager) CreateNamespace(namespace string) error {
	if namespace == "default" {
		return nil
	}
	ctx := context.Background()
	return m.k8sClient.Create(ctx, &v1.Namespace{ObjectMeta: metaV1.ObjectMeta{Name: namespace}})
}

func (m *defaultNamespaceManager) CreateNamespaceWithLabels(namespace string, labels map[string]string) error {
	if namespace == "" || namespace == "default" {
		return nil
	}
	return m.k8sClient.Create(context.Background(), &v1.Namespace{ObjectMeta: metaV1.ObjectMeta{Name: namespace, Labels: labels}})
}

func (m *defaultNamespaceManager) DeleteAndWaitTillNamespaceDeleted(namespace string) error {
	if namespace == "default" {
		return nil
	}
	ctx := context.Background()

	namespaceObj := &v1.Namespace{ObjectMeta: metaV1.ObjectMeta{Name: namespace, Namespace: ""}}
	err := m.k8sClient.Delete(ctx, namespaceObj)
	if err != nil {
		return err
	}

	observedNamespace := &v1.Namespace{}
	return wait.PollImmediateUntil(utils.PollIntervalShort, func() (done bool, err error) {
		err = m.k8sClient.Get(ctx, utils.NamespacedName(namespaceObj), observedNamespace)
		if errors.IsNotFound(err) {
			return true, nil
		}
		return false, err
	}, ctx.Done())
}

func (m *defaultNamespaceManager) WaitUntilNamespaceDeleted(ctx context.Context, ns *v1.Namespace) error {
	observedNS := &v1.Namespace{}
	return wait.PollImmediateUntil(utils.PollIntervalShort, func() (bool, error) {
		if err := m.k8sClient.Get(ctx, utils.NamespacedName(ns), observedNS); err != nil {
			if errors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}
		return false, nil
	}, ctx.Done())
}
