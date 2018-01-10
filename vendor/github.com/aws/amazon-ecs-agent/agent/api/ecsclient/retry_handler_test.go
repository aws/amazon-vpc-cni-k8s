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
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/aws/amazon-ecs-agent/agent/ecs_client/model/ecs"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/defaults"
)

func TestOneDayRetrier(t *testing.T) {
	stateChangeClient := newSubmitStateChangeClient(defaults.Config())

	request, _ := stateChangeClient.SubmitContainerStateChangeRequest(&ecs.SubmitContainerStateChangeInput{})

	retrier := stateChangeClient.Retryer

	var totalDelay time.Duration
	for retries := 0; retries < retrier.MaxRetries(); retries++ {
		request.Error = errors.New("")
		request.Retryable = aws.Bool(true)
		request.HTTPResponse = &http.Response{StatusCode: 500}
		if request.WillRetry() && retrier.ShouldRetry(request) {
			totalDelay += retrier.RetryRules(request)
			request.RetryCount++
		}
	}

	request.Error = errors.New("")
	request.Retryable = aws.Bool(true)
	request.HTTPResponse = &http.Response{StatusCode: 500}
	if request.WillRetry() {
		t.Errorf("Expected request to not be retried after %v retries", retrier.MaxRetries())
	}

	if totalDelay > 25*time.Hour || totalDelay < 23*time.Hour {
		t.Errorf("Expected accumulated retry delay to be roughly 24 hours; was %v", totalDelay)
	}
}
