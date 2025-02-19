package imds

import (
	"context"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/ec2/imds"
)

// EC2Metadata wraps the methods from the amazon-sdk-go's ec2metadata package
// type EC2Metadata interface {
// 	GetMetadata(path string) (string, error)
//	Region() (string, error)
// }

func GetMetaData(key string) (string, error) {
	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRetryMaxAttempts(10))
	if err != nil {
		return "", fmt.Errorf("unable to load SDK config, %v", err)
	}

	client := imds.NewFromConfig(cfg)
	requestedData, err := client.GetMetadata(context.TODO(), &imds.GetMetadataInput{
		Path: key,
	})
	if err != nil {
		return "", fmt.Errorf("get instance metadata: failed to retrieve %s - %s", key, err)
	}
	content, err := io.ReadAll(requestedData.Content)
	if err != nil {
		return "", fmt.Errorf("get instance metadata: failed to read %s - %s", key, err)
	}
	return string(content), nil
}
