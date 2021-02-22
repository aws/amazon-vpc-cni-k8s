// Package ec2metadatawrapper is used to retrieve data from EC2 IMDS
package ec2metadatawrapper

import (
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
)

// TODO: Move away from using mock

// HTTPClient is used to help with testing
type HTTPClient interface {
	GetInstanceIdentityDocument() (ec2metadata.EC2InstanceIdentityDocument, error)
	Region() (string, error)
}

// EC2MetadataClient to used to obtain a subset of information from EC2 IMDS
type EC2MetadataClient interface {
	GetInstanceIdentityDocument() (ec2metadata.EC2InstanceIdentityDocument, error)
	Region() (string, error)
}

type ec2MetadataClientImpl struct {
	client HTTPClient
}

// New creates an ec2metadata client to retrieve metadata
func New(session *session.Session) EC2MetadataClient {
	metadata := ec2metadata.New(session)
	return NewMetadataService(metadata)
}

// NewMetadataService creates an ec2metadata client to retrieve metadata
func NewMetadataService(metadata HTTPClient) EC2MetadataClient {
	return &ec2MetadataClientImpl{client: metadata}
}

// InstanceIdentityDocument returns instance identity documents
// http://docs.aws.amazon.com/AWSEC2/latest/UserGuide/instance-identity-documents.html
func (c *ec2MetadataClientImpl) GetInstanceIdentityDocument() (ec2metadata.EC2InstanceIdentityDocument, error) {
	return c.client.GetInstanceIdentityDocument()
}

func (c *ec2MetadataClientImpl) Region() (string, error) {
	return c.client.Region()
}
