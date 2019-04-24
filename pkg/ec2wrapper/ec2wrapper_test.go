package ec2wrapper

import (
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
)

var testInstanceIdentityDocument = ec2metadata.EC2InstanceIdentityDocument{
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
			Tags: []*ec2.TagDescription{
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
			Tags: []*ec2.TagDescription{},
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
	ec2iface.EC2API
	tags    *ec2.DescribeTagsOutput
	tagsErr error
}

func (f mockEC2ServiceClient) DescribeTags(input *ec2.DescribeTagsInput) (*ec2.DescribeTagsOutput, error) {
	return f.tags, f.tagsErr

}
