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

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"

	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"

	"strconv"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
)

// Http client timeout env for sessions
const (
	httpTimeoutEnv = "HTTP_TIMEOUT"
	maxRetries     = 10
)

var (
	log = logger.Get()
	// HTTP timeout default value in seconds (10 seconds)
	httpTimeoutValue = 10 * time.Second
)

func getHTTPTimeout() time.Duration {
	httpTimeoutEnvInput := os.Getenv(httpTimeoutEnv)
	// if httpTimeout is not empty, we convert value to int and overwrite default httpTimeoutValue
	if httpTimeoutEnvInput != "" {
		input, err := strconv.Atoi(httpTimeoutEnvInput)
		if err == nil && input >= 10 {
			log.Debugf("Using HTTP_TIMEOUT %v", input)
			httpTimeoutValue = time.Duration(input) * time.Second
			return httpTimeoutValue
		}
	}
	log.Warn("HTTP_TIMEOUT env is not set or set to less than 10 seconds, defaulting to httpTimeout to 10sec")
	return httpTimeoutValue
}

// New will return aws.Config to be used by Service Clients.
func New(ctx context.Context) (aws.Config, error) {
	httpClient := awshttp.NewBuildableClient().WithTimeout(getHTTPTimeout())
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
