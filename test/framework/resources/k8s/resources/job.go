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

	v1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type JobManager interface {
	CreateAndWaitTillJobCompleted(job *v1.Job) (*v1.Job, error)
	DeleteAndWaitTillJobIsDeleted(job *v1.Job) error
}

type defaultJobManager struct {
	k8sClient client.Client
}

func NewDefaultJobManager(k8sClient client.Client) JobManager {
	return &defaultJobManager{k8sClient: k8sClient}
}

func (d *defaultJobManager) CreateAndWaitTillJobCompleted(job *v1.Job) (*v1.Job, error) {
	ctx := context.Background()
	err := d.k8sClient.Create(ctx, job)
	if err != nil {
		return nil, err
	}

	time.Sleep(utils.PollIntervalShort)

	observedJob := &v1.Job{}
	return observedJob, wait.PollImmediateUntil(utils.PollIntervalShort, func() (bool, error) {
		if err := d.k8sClient.Get(ctx, utils.NamespacedName(job), observedJob); err != nil {
			return false, err
		}
		if observedJob.Status.Failed > 0 {
			return false, fmt.Errorf("failed to execute job :%v", observedJob.Status)
		} else if observedJob.Status.Succeeded == (*job.Spec.Parallelism) {
			return true, nil
		}
		return false, nil
	}, ctx.Done())
}

func (d *defaultJobManager) DeleteAndWaitTillJobIsDeleted(job *v1.Job) error {
	ctx := context.Background()
	err := d.k8sClient.Delete(ctx, job)
	if err != nil {
		return err
	}

	observedJob := &v1.Job{}
	return wait.PollImmediateUntil(utils.PollIntervalShort, func() (bool, error) {
		if err := d.k8sClient.Get(ctx, utils.NamespacedName(job), observedJob); err != nil {
			if errors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}
		return false, nil
	}, ctx.Done())
}
