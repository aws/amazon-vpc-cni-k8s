package imds

import (
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	ec2metadatasvc "github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
)

// EC2Metadata wraps the methods from the amazon-sdk-go's ec2metadata package
type EC2Metadata interface {
	GetMetadata(path string) (string, error)
	Region() (string, error)
}

func GetMetaData(key string) (string, error) {
	awsSession := session.Must(session.NewSession(aws.NewConfig().
		WithMaxRetries(10),
	))
	var ec2Metadata EC2Metadata
	ec2Metadata = ec2metadatasvc.New(awsSession)
	requestedData, err := ec2Metadata.GetMetadata(key)
	if err != nil {
		return "", fmt.Errorf("get instance metadata: failed to retrieve %s - %s", key, err)
	}
	return requestedData, nil
}
