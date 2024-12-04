// Package ec2wrapper is used to wrap around the ec2 service APIs
package ec2wrapper

import (
	"context"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/ec2metadatawrapper"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	ec2metadata "github.com/aws/aws-sdk-go-v2/feature/ec2/imds"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/pkg/errors"
)

const (
	resourceID   = "resource-id"
	resourceKey  = "key"
	clusterIDTag = "CLUSTER_ID"
)

var log = logger.Get()

// EC2Wrapper is used to wrap around EC2 service APIs to obtain ClusterID from
// the ec2 instance tags
type EC2Wrapper struct {
	ec2ServiceClient         ec2.DescribeTagsAPIClient
	instanceIdentityDocument ec2metadata.InstanceIdentityDocument
}

// NewMetricsClient returns an instance of the EC2 wrapper
func NewMetricsClient() (*EC2Wrapper, error) {
	ctx := context.TODO()
	ec2MetadataClient, err := ec2metadatawrapper.New(ctx)
	if err != nil {
		return &EC2Wrapper{}, err
	}

	instanceIdentityDocumentOutput, err := ec2MetadataClient.GetInstanceIdentityDocument(ctx, &ec2metadata.GetInstanceIdentityDocumentInput{})
	if err != nil {
		return &EC2Wrapper{}, err
	}

	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(instanceIdentityDocumentOutput.Region))
	if err != nil {
		return &EC2Wrapper{}, err
	}
	ec2ServiceClient := ec2.NewFromConfig(awsCfg)

	return &EC2Wrapper{
		ec2ServiceClient:         ec2ServiceClient,
		instanceIdentityDocument: instanceIdentityDocumentOutput.InstanceIdentityDocument,
	}, nil
}

// GetClusterTag is used to retrieve a tag from the ec2 instance
func (e *EC2Wrapper) GetClusterTag(tagKey string) (string, error) {
	ctx := context.TODO()
	input := ec2.DescribeTagsInput{
		Filters: []ec2types.Filter{
			{
				Name: aws.String(resourceID),
				Values: []string{
					e.instanceIdentityDocument.InstanceID,
				},
			}, {
				Name: aws.String(resourceKey),
				Values: []string{
					tagKey,
				},
			},
		},
	}

	log.Infof("Calling DescribeTags with key %s", tagKey)
	results, err := e.ec2ServiceClient.DescribeTags(ctx, &input)
	if err != nil {
		return "", errors.Wrap(err, "GetClusterTag: Unable to obtain EC2 instance tags")
	}

	if len(results.Tags) < 1 {
		return "", errors.Errorf("GetClusterTag: No tag matching key: %s", tagKey)
	}

	return aws.ToString(results.Tags[0].Value), nil
}
