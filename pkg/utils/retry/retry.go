// Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
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

// Package retry is a retry with backoff implementation
package retry

import (
	"context"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/ttime"
)

var _time ttime.Time = &ttime.DefaultTime{}

// WithBackoff takes a Backoff and a function to call that returns an error
// If the error is nil then the function will no longer be called
// If the error is Retriable then that will be used to determine if it should be retried
func WithBackoff(backoff Backoff, fn func() error) error {
	return WithBackoffCtx(context.Background(), backoff, fn)
}

// WithBackoffCtx takes a context, a Backoff, and a function to call that returns an error
// If the context is done, nil will be returned
// If the error is nil then the function will no longer be called
// If the error is Retriable then that will be used to determine if it should be retried
func WithBackoffCtx(ctx context.Context, backoff Backoff, fn func() error) error {
	var err error
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		err = fn()
		retriableErr, isRetriableErr := err.(Retriable)
		if err == nil || (isRetriableErr && !retriableErr.Retry()) {
			return err
		}
		_time.Sleep(backoff.Duration())
	}
}

// NWithBackoff takes a Backoff, a maximum number of tries 'n', and a
// function that returns an error. The function is called until either it does
// not return an error or the maximum tries have been reached.
// If the error returned is Retriable, the Retriability of it will be respected.
// If the number of tries is exhausted, the last error will be returned.
func NWithBackoff(backoff Backoff, n int, fn func() error) error {
	return NWithBackoffCtx(context.Background(), backoff, n, fn)
}

// NWithBackoffCtx takes a context, a Backoff, a maximum number of tries 'n', and a function that returns an error.
// The function is called until it does not return an error, the context is done, or the maximum tries have been
// reached.
// If the error returned is Retriable, the Retriability of it will be respected.
// If the number of tries is exhausted, the last error will be returned.
func NWithBackoffCtx(ctx context.Context, backoff Backoff, n int, fn func() error) error {
	var err error
	_ = WithBackoffCtx(ctx, backoff, func() error {
		err = fn()
		n--
		if n == 0 {
			// Break out after n tries
			return nil
		}
		return err
	})
	return err
}
