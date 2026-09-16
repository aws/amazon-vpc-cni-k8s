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

// Package sagemakerwrapper is used to wrap around the sagemaker service APIs
package sagemakerwrapper

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sagemaker"
)

// SageMaker is the SageMaker wrapper interface
type SageMaker interface {
	AttachClusterNodeNetworkInterface(ctx context.Context, input *sagemaker.AttachClusterNodeNetworkInterfaceInput, opts ...func(*sagemaker.Options)) (*sagemaker.AttachClusterNodeNetworkInterfaceOutput, error)
}

// New creates a new SageMaker wrapper
func New(cfg aws.Config) *sagemaker.Client {
	return sagemaker.NewFromConfig(cfg)
}
