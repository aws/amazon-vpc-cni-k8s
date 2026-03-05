package imds

import (
	"context"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/ec2/imds"
)

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
	defer requestedData.Content.Close()
	content, err := io.ReadAll(requestedData.Content)
	if err != nil {
		return "", fmt.Errorf("get instance metadata: failed to read %s - %s", key, err)
	}
	return string(content), nil
}
