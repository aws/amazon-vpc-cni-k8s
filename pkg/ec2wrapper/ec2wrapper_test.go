package ec2wrapper

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2metadata "github.com/aws/aws-sdk-go-v2/feature/ec2/imds"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
)

var testInstanceIdentityDocument = ec2metadata.InstanceIdentityDocument{
	PrivateIP:        "172.1.1.1",
	AvailabilityZone: "us-east-1a",
	Version:          "2010-08-31",
	Region:           "us-east-1",
	AccountID:        "012345678901",
	InstanceID:       "i-01234567",
	BillingProducts:  []string{"bp-01234567"},
	ImageID:          "ami-12345678",
	InstanceType:     "t2.micro",
	PendingTime:      time.Now(),
	Architecture:     "x86_64",
}

func TestGetClusterID(t *testing.T) {
	mockEC2ServiceClient := mockEC2ServiceClient{
		tags: &ec2.DescribeTagsOutput{
			Tags: []ec2types.TagDescription{
				{
					Value: aws.String("TEST_CLUSTER_ID"),
				},
			},
		},
	}

	ec2wrap := EC2Wrapper{
		ec2ServiceClient:         mockEC2ServiceClient,
		instanceIdentityDocument: testInstanceIdentityDocument,
	}

	clusterID, err := ec2wrap.GetClusterTag(clusterIDTag)
	assert.NoError(t, err)
	assert.NotNil(t, clusterID)
}

func TestGetClusterIDWithError(t *testing.T) {
	mockEC2ServiceClient := mockEC2ServiceClient{
		tagsErr: errors.New("test error"),
	}

	ec2wrap := EC2Wrapper{
		ec2ServiceClient:         mockEC2ServiceClient,
		instanceIdentityDocument: testInstanceIdentityDocument,
	}

	clusterID, err := ec2wrap.GetClusterTag(clusterIDTag)
	assert.Error(t, err)
	assert.Empty(t, clusterID)
}

func TestGetClusterIDWithInsufficientTags(t *testing.T) {
	mockEC2ServiceClient := mockEC2ServiceClient{
		tags: &ec2.DescribeTagsOutput{
			Tags: []ec2types.TagDescription{},
		},
	}

	ec2wrap := EC2Wrapper{
		ec2ServiceClient:         mockEC2ServiceClient,
		instanceIdentityDocument: testInstanceIdentityDocument,
	}

	clusterID, err := ec2wrap.GetClusterTag(clusterIDTag)
	assert.Error(t, err)
	assert.Empty(t, clusterID)
}

type mockEC2ServiceClient struct {
	ec2.DescribeInstancesAPIClient
	tags    *ec2.DescribeTagsOutput
	tagsErr error
}

func (m mockEC2ServiceClient) DescribeTags(ctx context.Context, input *ec2.DescribeTagsInput, f ...func(*ec2.Options)) (*ec2.DescribeTagsOutput, error) {
	return m.tags, m.tagsErr
}

func TestNew(t *testing.T) {
	cfg := aws.Config{Region: "us-east-1"}
	client := New(cfg)
	assert.NotNil(t, client)
}

func TestNewMetricsClient(t *testing.T) {
	_, err := NewMetricsClient()
	// This will fail in test environment without IMDS, which is expected
	assert.Error(t, err)
}
