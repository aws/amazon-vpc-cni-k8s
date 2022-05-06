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
	v1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type DaemonSetManager interface {
	GetDaemonSet(namespace string, name string) (*v1.DaemonSet, error)
	UpdateAndWaitTillDaemonSetReady(old *v1.DaemonSet, new *v1.DaemonSet) (*v1.DaemonSet, error)
}

type defaultDaemonSetManager struct {
	k8sClient client.Client
}

func NewDefaultDaemonSetManager(k8sClient client.Client) DaemonSetManager {
	return &defaultDaemonSetManager{k8sClient: k8sClient}
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
