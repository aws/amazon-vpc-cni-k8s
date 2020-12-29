//Package ec2wrapper is used to wrap around the ec2 service APIs
package ec2wrapper

import (
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/ec2metadatawrapper"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
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

	// Http client timeout env for sessions
	httpTimeoutEnv = "HTTP_TIMEOUT"
)

var (
	log = logger.Get()

	// HTTP timeout default value in seconds (10 seconds)
	httpTimeoutValue = 10 * time.Second
)

// EC2Wrapper is used to wrap around EC2 service APIs to obtain ClusterID from
// the ec2 instance tags
type EC2Wrapper struct {
	ec2ServiceClient         ec2iface.EC2API
	instanceIdentityDocument ec2metadata.EC2InstanceIdentityDocument
}

//NewMetricsClient returns an instance of the EC2 wrapper
func NewMetricsClient() (*EC2Wrapper, error) {

	httpTimeoutEnvInput := os.Getenv(httpTimeoutEnv)
	// if httpTimeout is not empty, we convert value to int and overwrite default httpTimeoutValue
	if httpTimeoutEnvInput != "" {
		if input, err := strconv.Atoi(httpTimeoutEnvInput); err == nil && input >= 1 {
			log.Debugf("Using HTTP_TIMEOUT %v", input)
			httpTimeoutValue = time.Duration(input) * time.Second
		}
	}

	metricsSession := session.Must(session.NewSession(
		&aws.Config{
			MaxRetries: aws.Int(15),
			HTTPClient: &http.Client{
				Timeout: httpTimeoutValue,
			},
		},
	))
	ec2MetadataClient := ec2metadatawrapper.New(nil)

	instanceIdentityDocument, err := ec2MetadataClient.GetInstanceIdentityDocument()
	if err != nil {
		return &EC2Wrapper{}, err
	}

	ec2ServiceClient := ec2.New(metricsSession, aws.NewConfig().WithMaxRetries(maxRetries).WithRegion(instanceIdentityDocument.Region))

	return &EC2Wrapper{
		ec2ServiceClient:         ec2ServiceClient,
		instanceIdentityDocument: instanceIdentityDocument,
	}, nil
}

// GetClusterTag is used to retrieve a tag from the ec2 instance
func (e *EC2Wrapper) GetClusterTag(tagKey string) (string, error) {
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
					aws.String(tagKey),
				},
			},
		},
	}

	log.Infof("Calling DescribeTags with key %s", tagKey)
	results, err := e.ec2ServiceClient.DescribeTags(&input)
	if err != nil {
		return "", errors.Wrap(err, "GetClusterTag: Unable to obtain EC2 instance tags")
	}

	if len(results.Tags) < 1 {
		return "", errors.Errorf("GetClusterTag: No tag matching key: %s", tagKey)
	}

	return aws.StringValue(results.Tags[0].Value), nil
}
