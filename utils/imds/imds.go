package imds

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/ec2/imds"
)

const (
	// defaultAWSSDKClientTimeout is the timeout for individual HTTP requests made by AWS SDK clients.
	defaultAWSSDKClientTimeout = 10 * time.Second
)

func GetMetaData(key string) (string, error) {
	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithHTTPClient(&http.Client{
			Timeout: defaultAWSSDKClientTimeout,
		}),
		config.WithRetryMaxAttempts(10),
	)
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
