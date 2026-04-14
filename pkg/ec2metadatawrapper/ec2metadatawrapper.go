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

// Package ec2metadatawrapper is used to retrieve data from EC2 IMDS
package ec2metadatawrapper

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/ec2/imds"
)

// HTTPClient is used to help with testing
type HTTPClient interface {
	GetInstanceIdentityDocument(ctx context.Context, params *imds.GetInstanceIdentityDocumentInput, optFns ...func(*imds.Options)) (*imds.GetInstanceIdentityDocumentOutput, error)
	GetRegion(ctx context.Context, params *imds.GetRegionInput, optFns ...func(*imds.Options)) (*imds.GetRegionOutput, error)
}

// EC2MetadataClient to used to obtain a subset of information from EC2 IMDS
type EC2MetadataClient interface {
	GetInstanceIdentityDocument(ctx context.Context, params *imds.GetInstanceIdentityDocumentInput, optFns ...func(*imds.Options)) (*imds.GetInstanceIdentityDocumentOutput, error)
	GetRegion(ctx context.Context, params *imds.GetRegionInput, optFns ...func(*imds.Options)) (*imds.GetRegionOutput, error)
}

type ec2MetadataClientImpl struct {
	client HTTPClient
}

// New creates an ec2metadata client to retrieve metadata
func New(ctx context.Context) (EC2MetadataClient, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, err
	}

	client := imds.NewFromConfig(cfg)
	return NewMetadataService(client), nil
}

// NewMetadataService creates an ec2metadata client to retrieve metadata
func NewMetadataService(client HTTPClient) EC2MetadataClient {
	return &ec2MetadataClientImpl{client: client}
}

// GetInstanceIdentityDocument returns instance identity documents
func (c *ec2MetadataClientImpl) GetInstanceIdentityDocument(ctx context.Context, params *imds.GetInstanceIdentityDocumentInput, optFns ...func(*imds.Options)) (*imds.GetInstanceIdentityDocumentOutput, error) {
	return c.client.GetInstanceIdentityDocument(ctx, params, optFns...)
}

// GetRegion returns the AWS Region the instance is running in
func (c *ec2MetadataClientImpl) GetRegion(ctx context.Context, params *imds.GetRegionInput, optFns ...func(*imds.Options)) (*imds.GetRegionOutput, error) {
	return c.client.GetRegion(ctx, params, optFns...)
}
