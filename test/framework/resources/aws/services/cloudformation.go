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

	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation/types"
	"k8s.io/apimachinery/pkg/util/wait"
)

type CloudFormation interface {
	WaitTillStackCreated(ctx context.Context, stackName string, stackParams []types.Parameter, templateBody string) (*cloudformation.DescribeStacksOutput, error)
	WaitTillStackDeleted(ctx context.Context, stackName string) error
}

// Directly using the client instead of the Interface.
type defaultCloudFormation struct {
	client *cloudformation.Client
}

func NewCloudFormation(cfg aws.Config) CloudFormation {
	return &defaultCloudFormation{
		client: cloudformation.NewFromConfig(cfg),
	}
}

func (d *defaultCloudFormation) WaitTillStackCreated(ctx context.Context, stackName string, stackParams []types.Parameter, templateBody string) (*cloudformation.DescribeStacksOutput, error) {
	createStackInput := &cloudformation.CreateStackInput{
		Parameters:   stackParams,
		StackName:    aws.String(stackName),
		TemplateBody: aws.String(templateBody),
		Capabilities: []types.Capability{types.CapabilityCapabilityIam},
	}

	_, err := d.client.CreateStack(ctx, createStackInput)
	if err != nil {
		return nil, err
	}

	describeStackInput := &cloudformation.DescribeStacksInput{
		StackName: aws.String(stackName),
	}

	var describeStackOutput *cloudformation.DescribeStacksOutput
	// Using the provided ctx, ctx.Done() allows wait.PollImmediateUtil to cancel
	err = wait.PollImmediateUntil(utils.PollIntervalLong, func() (done bool, err error) {
		describeStackOutput, err = d.client.DescribeStacks(ctx, describeStackInput)
		if err != nil {
			return true, err
		}
		if describeStackOutput.Stacks[0].StackStatus == types.StackStatusCreateComplete {
			return true, nil
		}
		return false, nil
	}, ctx.Done())

	return describeStackOutput, err
}

func (d *defaultCloudFormation) WaitTillStackDeleted(ctx context.Context, stackName string) error {
	deleteStackInput := &cloudformation.DeleteStackInput{
		StackName: aws.String(stackName),
	}
	_, err := d.client.DeleteStack(ctx, deleteStackInput)
	if err != nil {
		return fmt.Errorf("failed to delete stack %s: %v", stackName, err)
	}

	describeStackInput := &cloudformation.DescribeStacksInput{
		StackName: aws.String(stackName),
	}

	var describeStackOutput *cloudformation.DescribeStacksOutput
	// Using the provided ctx, ctx.Done() allows wait.PollImmediateUtil to cancel if required.
	err = wait.PollImmediateUntil(utils.PollIntervalLong, func() (done bool, err error) {
		describeStackOutput, err = d.client.DescribeStacks(ctx, describeStackInput)
		if err != nil {
			return true, err
		}
		if describeStackOutput.Stacks[0].StackStatus == types.StackStatusDeleteComplete {
			return true, nil
		}
		return false, nil
	}, ctx.Done())

	return nil
}
