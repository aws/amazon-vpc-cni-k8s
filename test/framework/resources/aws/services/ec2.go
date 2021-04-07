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
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"
)

type EC2 interface {
	DescribeInstanceType(instanceType string) ([]*ec2.InstanceTypeInfo, error)
}

type defaultEC2 struct {
	ec2iface.EC2API
}

func (d defaultEC2) DescribeInstanceType(instanceType string) ([]*ec2.InstanceTypeInfo, error) {
	describeInstanceTypeIp := &ec2.DescribeInstanceTypesInput{
		InstanceTypes: aws.StringSlice([]string{instanceType}),
	}
	describeInstanceOp, err := d.EC2API.DescribeInstanceTypes(describeInstanceTypeIp)
	if err != nil {
		return nil, err
	}
	if len(describeInstanceOp.InstanceTypes) == 0 {
		return nil, fmt.Errorf("no instance type found in the output %s", instanceType)
	}
	return describeInstanceOp.InstanceTypes, nil
}

func NewEC2(session *session.Session) EC2 {
	return &defaultEC2{
		EC2API: ec2.New(session),
	}
}
