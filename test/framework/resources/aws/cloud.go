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
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/aws/services"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
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

func NewCloud(config CloudConfig) Cloud {
	session := session.Must(session.NewSession(&aws.Config{
		Region: aws.String(config.Region)}))

	return &defaultCloud{
		cfg:            config,
		ec2:            services.NewEC2(session),
		iam:            services.NewIAM(session),
		eks:            services.NewEKS(session, config.EKSEndpoint),
		autoScaling:    services.NewAutoScaling(session),
		cloudFormation: services.NewCloudFormation(session),
		cloudWatch:     services.NewCloudWatch(session),
	}
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
