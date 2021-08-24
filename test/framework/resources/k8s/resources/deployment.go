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

	v1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type DeploymentManager interface {
	CreateAndWaitTillDeploymentIsReady(deployment *v1.Deployment, timeout time.Duration) (*v1.Deployment, error)
	DeleteAndWaitTillDeploymentIsDeleted(deployment *v1.Deployment) error
}

type defaultDeploymentManager struct {
	k8sClient client.Client
}

// CreateAndWaitTillDeploymentIsReady creates and waits for deployment to become ready or timeout
// with error if deployment doesn't become ready.
func (d defaultDeploymentManager) CreateAndWaitTillDeploymentIsReady(deployment *v1.Deployment, timeout time.Duration) (*v1.Deployment, error) {
	ctx := context.Background()
	err := d.k8sClient.Create(ctx, deployment)
	if err != nil {
		return nil, err
	}

	// Allow for the cache to sync
	time.Sleep(utils.PollIntervalShort)

	observed := &v1.Deployment{}
	return observed, wait.PollImmediate(utils.PollIntervalShort, timeout, func() (bool, error) {
		if err := d.k8sClient.Get(ctx, utils.NamespacedName(deployment), observed); err != nil {
			return false, err
		}
		if observed.Status.UpdatedReplicas == (*observed.Spec.Replicas) &&
			observed.Status.Replicas == (*observed.Spec.Replicas) &&
			observed.Status.AvailableReplicas == (*observed.Spec.Replicas) &&
			observed.Status.ObservedGeneration >= observed.Generation {
			return true, nil
		}
		return false, nil
	})
}

//
func (d defaultDeploymentManager) DeleteAndWaitTillDeploymentIsDeleted(deployment *v1.Deployment) error {
	ctx := context.Background()
	err := d.k8sClient.Delete(ctx, deployment)
	if err != nil {
		return err
	}
	observed := &v1.Deployment{}
	return wait.PollImmediateUntil(utils.PollIntervalShort, func() (bool, error) {
		if err := d.k8sClient.Get(ctx, utils.NamespacedName(deployment), observed); err != nil {
			if errors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}
		return false, nil
	}, ctx.Done())
}

func NewDefaultDeploymentManager(k8sClient client.Client) DeploymentManager {
	return &defaultDeploymentManager{k8sClient: k8sClient}
}
