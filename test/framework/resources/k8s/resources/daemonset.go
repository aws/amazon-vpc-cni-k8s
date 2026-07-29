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
	"fmt"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	v1 "k8s.io/api/apps/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type DaemonSetManager interface {
	GetDaemonSet(namespace string, name string) (*v1.DaemonSet, error)

	CreateAndWaitTillDaemonSetIsReady(daemonSet *v1.DaemonSet, timeout time.Duration) (*v1.DaemonSet, error)

	UpdateAndWaitTillDaemonSetReady(old *v1.DaemonSet, new *v1.DaemonSet) (*v1.DaemonSet, error)
	CheckIfDaemonSetIsReady(namespace string, name string) error
	DeleteAndWaitTillDaemonSetIsDeleted(daemonSet *v1.DaemonSet, timeout time.Duration) error
}

type defaultDaemonSetManager struct {
	k8sClient client.Client
}

func NewDefaultDaemonSetManager(k8sClient client.Client) DaemonSetManager {
	return &defaultDaemonSetManager{k8sClient: k8sClient}
}

func (d *defaultDaemonSetManager) CreateAndWaitTillDaemonSetIsReady(daemonSet *v1.DaemonSet, timeout time.Duration) (*v1.DaemonSet, error) {
	ctx := context.Background()
	err := d.k8sClient.Create(ctx, daemonSet)
	if err != nil {
		return nil, err
	}

	// Allow for the cache to sync
	time.Sleep(utils.PollIntervalLong)

	err = d.CheckIfDaemonSetIsReady(daemonSet.Namespace, daemonSet.Name)
	if err != nil {
		return nil, err
	}

	return daemonSet, nil
}

func (d *defaultDaemonSetManager) GetDaemonSet(namespace string, name string) (*v1.DaemonSet, error) {
	ctx := context.Background()
	daemonSet := &v1.DaemonSet{}
	err := d.k8sClient.Get(ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}, daemonSet)
	return daemonSet, err
}

func (d *defaultDaemonSetManager) UpdateAndWaitTillDaemonSetReady(old *v1.DaemonSet, new *v1.DaemonSet) (*v1.DaemonSet, error) {
	ctx := context.Background()
	err := d.k8sClient.Patch(ctx, new, client.MergeFrom(old))
	if err != nil {
		return nil, err
	}

	observed := &v1.DaemonSet{}
	return observed, wait.PollImmediateUntil(utils.PollIntervalShort, func() (bool, error) {
		if err := d.k8sClient.Get(ctx, utils.NamespacedName(new), observed); err != nil {
			return false, err
		}
		if observed.Status.NumberReady == (new.Status.DesiredNumberScheduled) &&
			observed.Status.NumberAvailable == (new.Status.DesiredNumberScheduled) &&
			observed.Status.UpdatedNumberScheduled == (new.Status.DesiredNumberScheduled) &&
			observed.Status.ObservedGeneration >= new.Generation {
			return true, nil
		}
		return false, nil
	}, ctx.Done())
}

func (d *defaultDaemonSetManager) CheckIfDaemonSetIsReady(namespace string, name string) error {
	ds, err := d.GetDaemonSet(namespace, name)
	if err != nil {
		return err
	}
	// 2 minute budget: aws-node startup includes a 30s background wait for API
	// server connectivity before it degrades gracefully ("Proceeding without
	// API server connectivity"), and specs that restart kube-proxy immediately
	// before restarting aws-node extend that window while the service VIP path
	// is reprogrammed -- measured ~50s+ to Ready. A shorter budget (previously
	// 20s) flaked any spec that restarts aws-node; polling returns as soon as
	// the daemonset is ready.
	err = wait.PollUntilContextTimeout(context.Background(), utils.PollIntervalMedium, 2*time.Minute, true,
		func(ctx context.Context) (bool, error) {
			if err := d.k8sClient.Get(ctx, utils.NamespacedName(ds), ds); err != nil {
				return false, err
			}
			// DesiredNumberScheduled can be 0 while the previous run's daemonset is still deleting.
			return ds.Status.DesiredNumberScheduled != 0 && ds.Status.NumberReady == ds.Status.DesiredNumberScheduled, nil
		})
	if err != nil {
		return fmt.Errorf("daemonset %s/%s not ready: %w", namespace, name, err)
	}
	return nil

}

func (d *defaultDaemonSetManager) DeleteAndWaitTillDaemonSetIsDeleted(daemonSet *v1.DaemonSet, timeout time.Duration) error {
	ctx := context.Background()

	err := d.k8sClient.Delete(ctx, daemonSet)

	if k8sErrors.IsNotFound(err) {
		return nil
	}

	if err != nil {
		return err
	}
	observed := &v1.DaemonSet{}

	return wait.PollImmediateUntil(utils.PollIntervalShort, func() (bool, error) {
		if err := d.k8sClient.Get(ctx, utils.NamespacedName(daemonSet), observed); err != nil {
			if k8sErrors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}
		return false, nil
	}, ctx.Done())
}
