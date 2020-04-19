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

package retry

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/ttime"
	mock_ttime "github.com/aws/amazon-vpc-cni-k8s/pkg/utils/ttime/mocks"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
)

func TestRetryWithBackoff(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mocktime := mock_ttime.NewMockTime(ctrl)
	_time = mocktime
	defer func() { _time = &ttime.DefaultTime{} }()

	t.Run("retries", func(t *testing.T) {
		mocktime.EXPECT().Sleep(100 * time.Millisecond).Times(3)
		counter := 3
		_ = RetryWithBackoff(NewSimpleBackoff(100*time.Millisecond, 100*time.Millisecond, 0, 1), func() error {
			if counter == 0 {
				return nil
			}
			counter--
			return errors.New("err")
		})
		assert.Equal(t, 0, counter, "Counter didn't go to 0; didn't get retried enough")
	})

	t.Run("no retries", func(t *testing.T) {
		// no sleeps
		_ = RetryWithBackoff(NewSimpleBackoff(10*time.Millisecond, 20*time.Millisecond, 0, 2), func() error {
			return NewRetriableError(NewRetriable(false), errors.New("can't retry"))
		})
	})
}

func TestRetryWithBackoffCtx(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mocktime := mock_ttime.NewMockTime(ctrl)
	_time = mocktime
	defer func() { _time = &ttime.DefaultTime{} }()

	t.Run("retries", func(t *testing.T) {
		mocktime.EXPECT().Sleep(100 * time.Millisecond).Times(3)
		counter := 3
		_ = RetryWithBackoffCtx(context.TODO(), NewSimpleBackoff(100*time.Millisecond, 100*time.Millisecond, 0, 1), func() error {
			if counter == 0 {
				return nil
			}
			counter--
			return errors.New("err")
		})
		assert.Equal(t, 0, counter, "Counter didn't go to 0; didn't get retried enough")
	})

	t.Run("no retries", func(t *testing.T) {
		// no sleeps
		_ = RetryWithBackoffCtx(context.TODO(), NewSimpleBackoff(10*time.Millisecond, 20*time.Millisecond, 0, 2), func() error {
			return NewRetriableError(NewRetriable(false), errors.New("can't retry"))
		})
	})

	t.Run("cancel context", func(t *testing.T) {
		mocktime.EXPECT().Sleep(100 * time.Millisecond).Times(2)
		counter := 2
		ctx, cancel := context.WithCancel(context.TODO())
		_ = RetryWithBackoffCtx(ctx, NewSimpleBackoff(100*time.Millisecond, 100*time.Millisecond, 0, 1), func() error {
			counter--
			if counter == 0 {
				cancel()
			}
			return errors.New("err")
		})
		assert.Equal(t, 0, counter, "Counter not 0; went the wrong number of times")
	})

}

func TestRetryNWithBackoff(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mocktime := mock_ttime.NewMockTime(ctrl)
	_time = mocktime
	defer func() { _time = &ttime.DefaultTime{} }()

	t.Run("count exceeded", func(t *testing.T) {
		// 2 tries, 1 sleep
		mocktime.EXPECT().Sleep(100 * time.Millisecond).Times(1)
		counter := 3
		err := RetryNWithBackoff(NewSimpleBackoff(100*time.Millisecond, 100*time.Millisecond, 0, 1), 2, func() error {
			counter--
			return errors.New("err")
		})
		assert.Equal(t, 1, counter, "Should have stopped after two tries")
		assert.Error(t, err)
	})

	t.Run("retry succeeded", func(t *testing.T) {
		// 3 tries, 2 sleeps
		mocktime.EXPECT().Sleep(100 * time.Millisecond).Times(2)
		counter := 3
		err := RetryNWithBackoff(NewSimpleBackoff(100*time.Millisecond, 100*time.Millisecond, 0, 1), 5, func() error {
			counter--
			if counter == 0 {
				return nil
			}
			return errors.New("err")
		})
		assert.Equal(t, 0, counter)
		assert.NoError(t, err)
	})
}

func TestRetryNWithBackoffCtx(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mocktime := mock_ttime.NewMockTime(ctrl)
	_time = mocktime
	defer func() { _time = &ttime.DefaultTime{} }()

	t.Run("count exceeded", func(t *testing.T) {
		// 2 tries, 1 sleep
		mocktime.EXPECT().Sleep(100 * time.Millisecond).Times(1)
		counter := 3
		err := RetryNWithBackoffCtx(context.TODO(), NewSimpleBackoff(100*time.Millisecond, 100*time.Millisecond, 0, 1), 2, func() error {
			counter--
			return errors.New("err")
		})
		assert.Equal(t, 1, counter, "Should have stopped after two tries")
		assert.Error(t, err)
	})

	t.Run("retry succeeded", func(t *testing.T) {
		// 3 tries, 2 sleeps
		mocktime.EXPECT().Sleep(100 * time.Millisecond).Times(2)
		counter := 3
		err := RetryNWithBackoffCtx(context.TODO(), NewSimpleBackoff(100*time.Millisecond, 100*time.Millisecond, 0, 1), 5, func() error {
			counter--
			if counter == 0 {
				return nil
			}
			return errors.New("err")
		})
		assert.Equal(t, 0, counter)
		assert.NoError(t, err)
	})

	t.Run("cancel context", func(t *testing.T) {
		// 2 tries, 2 sleeps
		mocktime.EXPECT().Sleep(100 * time.Millisecond).Times(2)
		counter := 3
		ctx, cancel := context.WithCancel(context.TODO())
		err := RetryNWithBackoffCtx(ctx, NewSimpleBackoff(100*time.Millisecond, 100*time.Millisecond, 0, 1), 5, func() error {
			counter--
			if counter == 1 {
				cancel()
			}
			return errors.New("err")
		})
		assert.Equal(t, 1, counter, "Should have stopped after two tries")
		assert.Error(t, err)
	})
}
