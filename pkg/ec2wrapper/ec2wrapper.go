// package ec2wrapper is used to wrap around the ec2 service APIs
package ec2wrapper

import (
	"github.com/aws/amazon-vpc-cni-k8s/pkg/ec2metadatawrapper"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"

	"github.com/pkg/errors"
)

const (
	maxRetries   = 5
	resourceID   = "resource-id"
	resourceKey  = "key"
	clusterIDTag = "CLUSTER_ID"
)

// EC2Wrapper is used to wrap around EC2 service APIs to obtain ClusterID from
// the ec2 instance tags
type EC2Wrapper struct {
	ec2ServiceClient         ec2iface.EC2API
	instanceIdentityDocument ec2metadata.EC2InstanceIdentityDocument
}

// New returns an instance of the EC2 wrapper
func NewMetricsClient() (*EC2Wrapper, error) {
	session := session.Must(session.NewSession())

	ec2MetadataClient := ec2metadatawrapper.New(nil)

	instanceIdentityDocument, err := ec2MetadataClient.GetInstanceIdentityDocument()
	if err != nil {
		return &EC2Wrapper{}, err
	}

	ec2ServiceClient := ec2.New(session, aws.NewConfig().WithMaxRetries(maxRetries).WithRegion(instanceIdentityDocument.Region))

	return &EC2Wrapper{
		ec2ServiceClient:         ec2ServiceClient,
		instanceIdentityDocument: instanceIdentityDocument,
	}, nil
}

// GetClusterID is used to retrieve the CLUSTER_ID from the ec2 instance tags
func (e *EC2Wrapper) GetClusterID() (string, error) {
	input := ec2.DescribeTagsInput{
		Filters: []*ec2.Filter{
			{
				Name: aws.String(resourceID),
				Values: []*string{
					aws.String(e.instanceIdentityDocument.InstanceID),
				},
			}, {
				Name: aws.String(resourceKey),
				Values: []*string{
					aws.String(clusterIDTag),
				},
			},
		},
	}

	results, err := e.ec2ServiceClient.DescribeTags(&input)
	if err != nil {
		return "", errors.Wrap(err, "get cluster-id: unable to obtain ec2 instance tags")
	}

	if len(results.Tags) < 1 {
		return "", errors.Errorf("get cluster-id: insufficient number of tags: %d", len(results.Tags))
	}

	return aws.StringValue(results.Tags[0].Value), nil
}
