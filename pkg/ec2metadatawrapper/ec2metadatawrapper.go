// package ecmetadatawrapper is used to retrieve data from EC2 IMDS
package ec2metadatawrapper

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
)

const (
	metadataRetries = 5
)

// TODO: Move away from using mock

// HttpClient is used to help with testing
type HttpClient interface {
	GetInstanceIdentityDocument() (ec2metadata.EC2InstanceIdentityDocument, error)
	Region() (string, error)
}

// EC2MetadataClient to used to obtain a subset of information from EC2 IMDS
type EC2MetadataClient interface {
	GetInstanceIdentityDocument() (ec2metadata.EC2InstanceIdentityDocument, error)
	Region() (string, error)
}

type ec2MetadataClientImpl struct {
	client HttpClient
}

// New creates an ec2metadata client to retrieve metadata
func New(client HttpClient) EC2MetadataClient {
	if client == nil {
		return &ec2MetadataClientImpl{client: ec2metadata.New(session.New(), aws.NewConfig().WithMaxRetries(metadataRetries))}
	} else {
		return &ec2MetadataClientImpl{client: client}
	}
}

// InstanceIdentityDocument returns instance identity documents
// http://docs.aws.amazon.com/AWSEC2/latest/UserGuide/instance-identity-documents.html
func (c *ec2MetadataClientImpl) GetInstanceIdentityDocument() (ec2metadata.EC2InstanceIdentityDocument, error) {
	return c.client.GetInstanceIdentityDocument()
}

func (c *ec2MetadataClientImpl) Region() (string, error) {
	return c.client.Region()
}
