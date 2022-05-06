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
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ServiceManager interface {
	GetService(ctx context.Context, namespace string, name string) (*v1.Service, error)
	CreateService(ctx context.Context, service *v1.Service) (*v1.Service, error)
	DeleteAndWaitTillServiceDeleted(ctx context.Context, service *v1.Service) error
}

type defaultServiceManager struct {
	k8sClient client.Client
}

func NewDefaultServiceManager(k8sClient client.Client) ServiceManager {
	return &defaultServiceManager{k8sClient: k8sClient}
}

func (s *defaultServiceManager) GetService(ctx context.Context, namespace string,
	name string) (*v1.Service, error) {

	service := &v1.Service{}
	err := s.k8sClient.Get(ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}, service)

	return service, err
}

func (s *defaultServiceManager) CreateService(ctx context.Context, service *v1.Service) (*v1.Service, error) {
	err := s.k8sClient.Create(ctx, service)
	if err != nil {
		return nil, err
	}

	// Wait till the cache is refreshed
	time.Sleep(utils.PollIntervalShort)

	observedService := &v1.Service{}
	return observedService, wait.PollImmediateUntil(utils.PollIntervalShort, func() (bool, error) {
		if err := s.k8sClient.Get(ctx, utils.NamespacedName(service), observedService); err != nil {
			return false, err
		}
		return true, nil
	}, ctx.Done())
}

func (s *defaultServiceManager) DeleteAndWaitTillServiceDeleted(ctx context.Context, service *v1.Service) error {
	err := s.k8sClient.Delete(ctx, service)
	if err != nil {
		return err
	}

	observed := &v1.Service{}
	return wait.PollImmediateUntil(utils.PollIntervalShort, func() (bool, error) {
		if err := s.k8sClient.Get(ctx, utils.NamespacedName(service), observed); err != nil {
			if errors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}
		return false, nil
	}, ctx.Done())
}
