// Copyright 2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

package eni

import (
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/ec2"
)

func WaitUntilNetworkInterfaceAvailableWithContext(ctx aws.Context, c *ec2.EC2, input *ec2.DescribeNetworkInterfacesInput, opts ...request.WaiterOption) error {
	w := request.Waiter{
		Name:        "WaitUntilNetworkInterfaceAvailable",
		MaxAttempts: 10,
		Delay:       request.ConstantWaiterDelay(20 * time.Second),
		Acceptors: []request.WaiterAcceptor{
			{
				State:   request.SuccessWaiterState,
				Matcher: request.PathAllWaiterMatch, Argument: "NetworkInterfaces[].Status",
				Expected: "available",
			},
			{
				State:   request.SuccessWaiterState,
				Matcher: request.PathAllWaiterMatch, Argument: "NetworkInterfaces[].Status",
				Expected: "in-use",
			},
			{
				State:    request.FailureWaiterState,
				Matcher:  request.ErrorWaiterMatch,
				Expected: "InvalidNetworkInterfaceID.NotFound",
			},
		},
		Logger: c.Config.Logger,
		NewRequest: func(opts []request.Option) (*request.Request, error) {
			var inCpy *ec2.DescribeNetworkInterfacesInput
			if input != nil {
				tmp := *input
				inCpy = &tmp
			}
			req, _ := c.DescribeNetworkInterfacesRequest(inCpy)
			req.SetContext(ctx)
			req.ApplyOptions(opts...)
			return req, nil
		},
	}
	w.ApplyOptions(opts...)

	return w.WaitWithContext(ctx)
}

// TODO(tvi): Finish.
func WaitUntilNetworkInterfaceDeleteWithContext(ctx aws.Context, c *ec2.EC2, input *ec2.DeleteNetworkInterfaceInput, opts ...request.WaiterOption) error {
	w := request.Waiter{
		Name:        "WaitUntilNetworkInterfaceDelete",
		MaxAttempts: 10,
		Delay:       request.ConstantWaiterDelay(20 * time.Second),
		Acceptors: []request.WaiterAcceptor{
			{
				State:   request.SuccessWaiterState,
				Matcher: request.PathAllWaiterMatch, Argument: "Volumes[].State",
				Expected: "deleted",
			},
			{
				State:    request.SuccessWaiterState,
				Matcher:  request.ErrorWaiterMatch,
				Expected: "InvalidVolume.NotFound",
			},
		},
		Logger: c.Config.Logger,
		NewRequest: func(opts []request.Option) (*request.Request, error) {
			var inCpy *ec2.DeleteNetworkInterfaceInput
			if input != nil {
				tmp := *input
				inCpy = &tmp
			}
			req, _ := c.DeleteNetworkInterfaceRequest(inCpy)
			req.SetContext(ctx)
			req.ApplyOptions(opts...)
			return req, nil
		},
	}
	w.ApplyOptions(opts...)

	return w.WaitWithContext(ctx)
}
