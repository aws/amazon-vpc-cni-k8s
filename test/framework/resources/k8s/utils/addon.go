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

package utils

import (
	"context"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/aws/services"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/util/wait"
)

func WaitTillAddonIsDeleted(eks services.EKS, addonName string, clusterName string) error {
	ctx := context.Background()
	return wait.PollImmediateUntil(utils.PollIntervalShort, func() (bool, error) {
		_, err := eks.DescribeAddon(context.TODO(), services.AddonInput{
			AddonName:   addonName,
			ClusterName: clusterName,
		})
		if err != nil {
			return false, err
		}
		return false, nil
	}, ctx.Done())
}

func WaitTillAddonIsActive(eks services.EKS, addonName string, clusterName string) error {
	ctx := context.Background()
	return wait.PollImmediateUntil(utils.PollIntervalShort, func() (bool, error) {
		describeAddonOutput, err := eks.DescribeAddon(context.TODO(), services.AddonInput{
			AddonName:   addonName,
			ClusterName: clusterName,
		})
		if err != nil {
			return false, err
		}

		status := describeAddonOutput.Addon.Status
		if status == "CREATE_FAILED" || status == "DEGRADED" {
			return false, errors.Errorf("Create Addon Failed, addon status: %s", status)
		}
		if status == "ACTIVE" {
			return true, nil
		}
		return false, nil
	}, ctx.Done())
}
