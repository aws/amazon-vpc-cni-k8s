// Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//     http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package awssession

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
)

// Http client timeout env for sessions
const (
	httpTimeoutEnv = "HTTP_TIMEOUT"
	maxRetries     = 10

	// DefaultAWSSDKClientTimeout is the default timeout for individual HTTP requests made by AWS SDK clients.
	DefaultAWSSDKClientTimeout = 10 * time.Second
)

// NewAWSSDKHTTPClient returns a new HTTP client with the configured AWS SDK timeout.
// It returns *awshttp.BuildableClient (instead of *http.Client) so the SDK can
// inject custom CA bundles via WithTransportOptions in air-gapped regions.
func NewAWSSDKHTTPClient() *awshttp.BuildableClient {
	return awshttp.NewBuildableClient().WithTimeout(getHTTPTimeout())
}

var (
	log = logger.Get()
)

func getHTTPTimeout() time.Duration {
	httpTimeoutEnvInput := os.Getenv(httpTimeoutEnv)
	if httpTimeoutEnvInput != "" {
		input, err := strconv.Atoi(httpTimeoutEnvInput)
		if err == nil && input >= 10 {
			log.Debugf("Using HTTP_TIMEOUT %v", input)
			return time.Duration(input) * time.Second
		}
	}
	log.Debugf("HTTP_TIMEOUT env is not set or set to less than 10 seconds, defaulting to httpTimeout to %v", DefaultAWSSDKClientTimeout)
	return DefaultAWSSDKClientTimeout
}

// New will return aws.Config to be used by Service Clients.
func New(ctx context.Context) (aws.Config, error) {
	httpClient := NewAWSSDKHTTPClient()
	optFns := []func(*config.LoadOptions) error{
		config.WithHTTPClient(httpClient),
		config.WithRetryMaxAttempts(maxRetries),
		config.WithRetryer(func() aws.Retryer {
			return retry.NewStandard()
		}),
	}

	endpoint := os.Getenv("AWS_EC2_ENDPOINT")
	if endpoint != "" {
		optFns = append(optFns, config.WithEndpointResolver(aws.EndpointResolverFunc(
			func(service, region string) (aws.Endpoint, error) {
				if service == ec2.ServiceID {
					return aws.Endpoint{
						URL: endpoint,
					}, nil
				}
				// Fall back to default resolution
				return aws.Endpoint{}, &aws.EndpointNotFoundError{}
			})))

	}

	cfg, err := config.LoadDefaultConfig(ctx, optFns...)

	if err != nil {
		return aws.Config{}, fmt.Errorf("failed to load AWS config: %w", err)
	}

	return cfg, nil
}
