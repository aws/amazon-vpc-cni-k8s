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

package services

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling/types"
)

type AutoScaling interface {
	DescribeAutoScalingGroup(ctx context.Context, autoScalingGroupName string) ([]types.AutoScalingGroup, error)
	GetASGForInstance(ctx context.Context, instanceID string) (string, error)
	SetDesiredCapacity(ctx context.Context, asgName string, desired int32) error
}

// Directly using the client to interact with the service instead of an interface.
type defaultAutoScaling struct {
	client *autoscaling.Client
}

func NewAutoScaling(cfg aws.Config) AutoScaling {
	return &defaultAutoScaling{
		client: autoscaling.NewFromConfig(cfg),
	}
}

func (d defaultAutoScaling) DescribeAutoScalingGroup(ctx context.Context, autoScalingGroupName string) ([]types.AutoScalingGroup, error) {
	describeAutoScalingGroupIp := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []string{autoScalingGroupName},
	}
	asg, err := d.client.DescribeAutoScalingGroups(ctx, describeAutoScalingGroupIp)
	if err != nil {
		return nil, err
	}
	if len(asg.AutoScalingGroups) == 0 {
		return nil, fmt.Errorf("failed to find asg %s", autoScalingGroupName)
	}

	return asg.AutoScalingGroups, nil
}

func (d defaultAutoScaling) GetASGForInstance(ctx context.Context, instanceID string) (string, error) {
	out, err := d.client.DescribeAutoScalingInstances(ctx, &autoscaling.DescribeAutoScalingInstancesInput{
		InstanceIds: []string{instanceID},
	})
	if err != nil {
		return "", err
	}
	if len(out.AutoScalingInstances) == 0 || out.AutoScalingInstances[0].AutoScalingGroupName == nil {
		return "", fmt.Errorf("instance %s is not part of any ASG", instanceID)
	}
	return *out.AutoScalingInstances[0].AutoScalingGroupName, nil
}

func (d defaultAutoScaling) SetDesiredCapacity(ctx context.Context, asgName string, desired int32) error {
	descOut, err := d.client.DescribeAutoScalingGroups(ctx, &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []string{asgName},
	})
	if err != nil {
		return err
	}
	if len(descOut.AutoScalingGroups) == 0 {
		return fmt.Errorf("ASG %s not found", asgName)
	}
	if aws.ToInt32(descOut.AutoScalingGroups[0].MaxSize) < desired {
		if _, err := d.client.UpdateAutoScalingGroup(ctx, &autoscaling.UpdateAutoScalingGroupInput{
			AutoScalingGroupName: aws.String(asgName),
			MaxSize:              aws.Int32(desired),
		}); err != nil {
			return fmt.Errorf("raise MaxSize: %v", err)
		}
	}
	_, err = d.client.SetDesiredCapacity(ctx, &autoscaling.SetDesiredCapacityInput{
		AutoScalingGroupName: aws.String(asgName),
		DesiredCapacity:      aws.Int32(desired),
		HonorCooldown:        aws.Bool(false),
	})
	return err
}
