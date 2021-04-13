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
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
)

type AutoScaling interface {
	DescribeAutoScalingGroup(autoScalingGroupName string) ([]*autoscaling.Group, error)
}

type defaultAutoScaling struct {
	autoscalingiface.AutoScalingAPI
}

func NewAutoScaling(session *session.Session) AutoScaling {
	return &defaultAutoScaling{
		AutoScalingAPI: autoscaling.New(session),
	}
}

func (d defaultAutoScaling) DescribeAutoScalingGroup(autoScalingGroupName string) ([]*autoscaling.Group, error) {
	describeAutoScalingGroupIp := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: aws.StringSlice([]string{autoScalingGroupName}),
	}
	asg, err := d.AutoScalingAPI.DescribeAutoScalingGroups(describeAutoScalingGroupIp)
	if err != nil {
		return nil, err
	}
	if len(asg.AutoScalingGroups) == 0 {
		return nil, fmt.Errorf("failed to find asg %s", autoScalingGroupName)
	}

	return asg.AutoScalingGroups, nil
}
