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
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/eks"
	"github.com/aws/aws-sdk-go/service/eks/eksiface"
)

type EKS interface {
	DescribeCluster(clusterName string) (*eks.DescribeClusterOutput, error)
}

type defaultEKS struct {
	eksiface.EKSAPI
}

func NewEKS(session *session.Session, endpoint string) EKS {
	return &defaultEKS{
		EKSAPI: eks.New(session, &aws.Config{
			Endpoint: aws.String(endpoint),
			Region:   session.Config.Region,
		}),
	}
}

func (d defaultEKS) DescribeCluster(clusterName string) (*eks.DescribeClusterOutput, error) {
	describeClusterInput := &eks.DescribeClusterInput{
		Name: aws.String(clusterName),
	}

	return d.EKSAPI.DescribeCluster(describeClusterInput)
}
