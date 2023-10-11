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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type DeploymentManager interface {
	CreateAndWaitTillDeploymentIsReady(deployment *v1.Deployment, timeout time.Duration) (*v1.Deployment, error)
	DeleteAndWaitTillDeploymentIsDeleted(deployment *v1.Deployment) error
	UpdateAndWaitTillDeploymentIsReady(deployment *v1.Deployment, timeout time.Duration) error
	GetDeployment(name, namespace string) (*v1.Deployment, error)
	WaitTillDeploymentReady(deployment *v1.Deployment, timeout time.Duration) (*v1.Deployment, error)

	WaitUntilDeploymentReady(ctx context.Context, dp *v1.Deployment) (*v1.Deployment, error)
	WaitUntilDeploymentDeleted(ctx context.Context, dp *v1.Deployment) error
}

type defaultDeploymentManager struct {
	k8sClient client.Client
}

// CreateAndWaitTillDeploymentIsReady creates and waits for deployment to become ready or timeout
// with error if deployment doesn't become ready.
func (d *defaultDeploymentManager) CreateAndWaitTillDeploymentIsReady(deployment *v1.Deployment, timeout time.Duration) (*v1.Deployment, error) {
	ctx := context.Background()
	err := d.k8sClient.Create(ctx, deployment)
	if err != nil {
		return nil, err
	}

	// Allow for the cache to sync
	time.Sleep(utils.PollIntervalShort)

	return d.WaitTillDeploymentReady(deployment, timeout)
}

func (d *defaultDeploymentManager) DeleteAndWaitTillDeploymentIsDeleted(deployment *v1.Deployment) error {
	ctx := context.Background()
	err := d.k8sClient.Delete(ctx, deployment)

	if errors.IsNotFound(err) {
		return nil
	}

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

func (d *defaultDeploymentManager) UpdateAndWaitTillDeploymentIsReady(deployment *v1.Deployment, timeout time.Duration) error {
	ctx := context.Background()
	observed := &v1.Deployment{}

	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Retrieve the latest version of Deployment before attempting update
		err := d.k8sClient.Get(ctx, utils.NamespacedName(deployment), observed)
		if err != nil {
			return err
		}
		return d.k8sClient.Update(ctx, deployment)
	})

	if retryErr != nil {
		return retryErr
	}
	_, err := d.WaitTillDeploymentReady(deployment, timeout)
	return err

}

func (d *defaultDeploymentManager) GetDeployment(name, namespace string) (*v1.Deployment, error) {
	ctx := context.Background()
	deployment := &v1.Deployment{}

	err := d.k8sClient.Get(ctx, types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}, deployment)

	return deployment, err
}

func (d *defaultDeploymentManager) WaitTillDeploymentReady(deployment *v1.Deployment, timeout time.Duration) (*v1.Deployment, error) {
	ctx := context.Background()
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

func (d *defaultDeploymentManager) WaitUntilDeploymentReady(ctx context.Context, dp *v1.Deployment) (*v1.Deployment, error) {
	observedDP := &v1.Deployment{}
	return observedDP, wait.PollImmediateUntil(utils.PollIntervalShort, func() (bool, error) {
		if err := d.k8sClient.Get(ctx, utils.NamespacedName(dp), observedDP); err != nil {
			return false, err
		}
		if observedDP.Status.UpdatedReplicas == (*dp.Spec.Replicas) &&
			observedDP.Status.Replicas == (*dp.Spec.Replicas) &&
			observedDP.Status.AvailableReplicas == (*dp.Spec.Replicas) &&
			observedDP.Status.ObservedGeneration >= dp.Generation {
			return true, nil
		}
		return false, nil
	}, ctx.Done())
}

func (d *defaultDeploymentManager) WaitUntilDeploymentDeleted(ctx context.Context, dp *v1.Deployment) error {
	observedDP := &v1.Deployment{}
	return wait.PollImmediateUntil(utils.PollIntervalShort, func() (bool, error) {
		if err := d.k8sClient.Get(ctx, utils.NamespacedName(dp), observedDP); err != nil {
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
