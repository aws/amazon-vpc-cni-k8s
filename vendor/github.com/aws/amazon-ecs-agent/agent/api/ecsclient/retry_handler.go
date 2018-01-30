// Copyright 2014-2016 Amazon.com, Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//	http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package ecsclient

import (
	"math"
	"math/rand"
	"time"

	"github.com/aws/amazon-ecs-agent/agent/ecs_client/model/ecs"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/client"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/session"
)

const (
	// submitStateChangeInitialRetries is the initial set of retries where delay
	// between retries grows exponentially at 2^n * 45+-15ms.  This should be roughly
	// 5 minutes of delay at the last growing retry.
	// 5 min = 2^n * (30 + 15) ms
	// n ~= 12.7
	// We round up to 13 (which really gives us ~6 minutes) for no
	// particular reason.
	// Because of jitter, this caps at ~8 minutes
	submitStateChangeInitialRetries = 13

	// submitStateChangeExtraRetries is the set of retries (at the max delay per
	// retry of exactly 5 minutes) which should reach roughly 24 hours of elapsed
	// time.
	// baseTryTime = \sum_{i=0}^13{2^i * 45 ms} ~= 12 minutes
	// 24 hours ~= 12 minutes + (n * 5 minutes)
	// n ~= 285
	submitStateChangeExtraRetries = 285
)

// newSubmitStateChangeClient returns a client intended to be used for
// Submit*StateChange APIs which has the behavior of retrying the call on
// retriable errors for an extended period of time (roughly 24 hours).
func newSubmitStateChangeClient(awsConfig *aws.Config) *ecs.ECS {
	sscConfig := awsConfig.Copy()
	sscConfig.Retryer = &oneDayRetrier{}
	client := ecs.New(session.New(sscConfig))
	return client
}

// oneDayRetrier is a retrier for the AWS SDK that retries up to one day.
// Each retry will have an exponential backoff from 30ms to 5 minutes. Once the
// backoff has reached 5 minutes, it will not increase further.
// Conforms to the request.Retryer interface https://github.com/aws/aws-sdk-go/blob/v1.0.0/aws/request/retryer.go#L13
type oneDayRetrier struct {
	client.DefaultRetryer
}

// MaxRetries returns the number of retries needed to retry for roughly a day
// for this Retrier
func (retrier *oneDayRetrier) MaxRetries() int {
	return submitStateChangeExtraRetries + submitStateChangeInitialRetries
}

// RetryRules returns the correct time to delay between retries for this
// instance of a retry. For the first 14 requests, it follows an exponential
// backoff between 30ms and 1 minute.
// See the const comments for math on how this gets us to around 24 hours
// total.
func (retrier *oneDayRetrier) RetryRules(r *request.Request) time.Duration {
	// This logic is the same as the default retrier, but duplicated here such
	// that upstream changes do not invalidate the math done above.
	if r.RetryCount <= submitStateChangeInitialRetries {
		delay := int(math.Pow(2, float64(r.RetryCount))) * (rand.Intn(30) + 30)
		return time.Duration(delay) * time.Millisecond
	}
	return 5 * time.Minute
}
