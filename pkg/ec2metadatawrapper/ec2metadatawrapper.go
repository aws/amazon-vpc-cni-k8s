// Package ec2metadatawrapper is used to retrieve data from EC2 IMDS
package ec2metadatawrapper

import (
	"context"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/ec2/imds"
)

// HTTPClient is used to help with testing
type HTTPClient interface {
	GetInstanceIdentityDocument(ctx context.Context, params *imds.GetInstanceIdentityDocumentInput) (*imds.GetInstanceIdentityDocumentOutput, error)
	GetRegion(ctx context.Context, params *imds.GetRegionInput) (*imds.GetRegionOutput, error)
}

// EC2MetadataClient to used to obtain a subset of information from EC2 IMDS
type EC2MetadataClient interface {
	GetInstanceIdentityDocument(ctx context.Context) (*imds.GetInstanceIdentityDocumentOutput, error)
	GetRegion(ctx context.Context) (string, error)
}

type ec2MetadataClientImpl struct {
	client *imds.Client
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
func NewMetadataService(client *imds.Client) EC2MetadataClient {
	return &ec2MetadataClientImpl{client: client}
}

// InstanceIdentityDocument returns instance identity documents
// http://docs.aws.amazon.com/AWSEC2/latest/UserGuide/instance-identity-documents.html
func (c *ec2MetadataClientImpl) GetInstanceIdentityDocument(ctx context.Context) (*imds.GetInstanceIdentityDocumentOutput, error) {
	input := &imds.GetInstanceIdentityDocumentInput{}
	return c.client.GetInstanceIdentityDocument(ctx, input)
}

// GetRegion returns the AWS Region the instance is running in
func (c *ec2MetadataClientImpl) GetRegion(ctx context.Context) (string, error) {
	input := &imds.GetRegionInput{}
	output, err := c.client.GetRegion(ctx, input)
	if err != nil {
		return "", err
	}
	return output.Region, nil
}
