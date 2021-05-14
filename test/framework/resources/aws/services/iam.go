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
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/aws/aws-sdk-go/service/iam/iamiface"
)

type IAM interface {
	AttachRolePolicy(policyArn string, roleName string) error
	DetachRolePolicy(policyARN string, roleName string) error
}

type defaultIAM struct {
	iamiface.IAMAPI
}

func (d *defaultIAM) AttachRolePolicy(policyARN string, roleName string) error {
	attachRolePolicyInput := &iam.AttachRolePolicyInput{
		PolicyArn: aws.String(policyARN),
		RoleName:  aws.String(roleName),
	}
	_, err := d.IAMAPI.AttachRolePolicy(attachRolePolicyInput)
	return err
}

func (d *defaultIAM) DetachRolePolicy(policyARN string, roleName string) error {
	detachRolePolicyInput := &iam.DetachRolePolicyInput{
		PolicyArn: aws.String(policyARN),
		RoleName:  aws.String(roleName),
	}
	_, err := d.IAMAPI.DetachRolePolicy(detachRolePolicyInput)
	return err
}

func NewIAM(session *session.Session) IAM {
	return &defaultIAM{
		IAMAPI: iam.New(session),
	}
}
