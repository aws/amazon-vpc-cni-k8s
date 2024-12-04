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

package aws

import (
	"context"
	"fmt"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/aws/services"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
)

type CloudConfig struct {
	VpcID       string
	Region      string
	EKSEndpoint string
}

type Cloud interface {
	EKS() services.EKS
	EC2() services.EC2
	IAM() services.IAM
	AutoScaling() services.AutoScaling
	CloudFormation() services.CloudFormation
	CloudWatch() services.CloudWatch
}

type defaultCloud struct {
	cfg            CloudConfig
	ec2            services.EC2
	eks            services.EKS
	iam            services.IAM
	autoScaling    services.AutoScaling
	cloudFormation services.CloudFormation
	cloudWatch     services.CloudWatch
}

func NewCloud(config CloudConfig) (Cloud, error) {

	cfg, err := awsconfig.LoadDefaultConfig(context.TODO(), awsconfig.WithRegion(config.Region))

	if err != nil {
		return nil, fmt.Errorf("unable to load SDK config, %v", err)
	}

	eksService, err := services.NewEKS(cfg, config.EKSEndpoint)

	if err != nil {
		return nil, fmt.Errorf("unable to create EKS service client, %v", err)
	}

	return &defaultCloud{
		cfg:            config,
		ec2:            services.NewEC2(cfg),
		iam:            services.NewIAM(cfg),
		eks:            eksService,
		autoScaling:    services.NewAutoScaling(cfg),
		cloudFormation: services.NewCloudFormation(cfg),
		cloudWatch:     services.NewCloudWatch(cfg),
	}, nil
}

func (c *defaultCloud) EC2() services.EC2 {
	return c.ec2
}

func (c *defaultCloud) AutoScaling() services.AutoScaling {
	return c.autoScaling
}

func (c *defaultCloud) CloudFormation() services.CloudFormation {
	return c.cloudFormation
}

func (c *defaultCloud) EKS() services.EKS {
	return c.eks
}

func (c *defaultCloud) IAM() services.IAM {
	return c.iam
}

func (c *defaultCloud) CloudWatch() services.CloudWatch {
	return c.cloudWatch
}
