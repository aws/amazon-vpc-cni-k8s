// Copyright 2014-2015 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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

package utils

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/ttime"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/ttime/mocks"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
)

func TestDefaultIfBlank(t *testing.T) {
	const defaultValue = "a boring default"
	const specifiedValue = "new value"
	result := DefaultIfBlank(specifiedValue, defaultValue)
	assert.Equal(t, specifiedValue, result)

	result = DefaultIfBlank("", defaultValue)
	assert.Equal(t, defaultValue, result)
}

func TestZeroOrNil(t *testing.T) {
	type ZeroTest struct {
		testInt int
		TestStr string
	}

	var strMap map[string]string

	testCases := []struct {
		param    interface{}
		expected bool
		name     string
	}{
		{nil, true, "Nil is nil"},
		{0, true, "0 is 0"},
		{"", true, "\"\" is the string zerovalue"},
		{ZeroTest{}, true, "ZeroTest zero-value should be zero"},
		{ZeroTest{TestStr: "asdf"}, false, "ZeroTest with a field populated isn't zero"},
		{1, false, "1 is not 0"},
		{[]uint16{1, 2, 3}, false, "[1,2,3] is not zero"},
		{[]uint16{}, true, "[] is zero"},
		{struct{ uncomparable []uint16 }{uncomparable: []uint16{1, 2, 3}}, false, "Uncomparable structs are never zero"},
		{struct{ uncomparable []uint16 }{uncomparable: nil}, false, "Uncomparable structs are never zero"},
		{strMap, true, "map[string]string is zero or nil"},
		{make(map[string]string), true, "empty map[string]string is zero or nil"},
		{map[string]string{"foo": "bar"}, false, "map[string]string{foo:bar} is not zero or nil"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, ZeroOrNil(tc.param), tc.name)
		})
	}
}

func TestSlicesDeepEqual(t *testing.T) {
	testCases := []struct {
		left     []string
		right    []string
		expected bool
		name     string
	}{
		{[]string{}, []string{}, true, "Two empty slices"},
		{[]string{"cat"}, []string{}, false, "One empty slice"},
		{[]string{}, []string{"cat"}, false, "Another empty slice"},
		{[]string{"cat"}, []string{"cat"}, true, "Two slices with one element each"},
		{[]string{"cat", "dog", "cat"}, []string{"dog", "cat", "cat"}, true, "Two slices with multiple elements"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, SlicesDeepEqual(tc.left, tc.right))
		})
	}
}

func TestRetryWithBackoff(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mocktime := mock_ttime.NewMockTime(ctrl)
	_time = mocktime
	defer func() { _time = &ttime.DefaultTime{} }()

	t.Run("retries", func(t *testing.T) {
		mocktime.EXPECT().Sleep(100 * time.Millisecond).Times(3)
		counter := 3
		RetryWithBackoff(NewSimpleBackoff(100*time.Millisecond, 100*time.Millisecond, 0, 1), func() error {
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
		RetryWithBackoff(NewSimpleBackoff(10*time.Second, 20*time.Second, 0, 2), func() error {
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
		RetryWithBackoffCtx(context.TODO(), NewSimpleBackoff(100*time.Millisecond, 100*time.Millisecond, 0, 1), func() error {
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
		RetryWithBackoffCtx(context.TODO(), NewSimpleBackoff(10*time.Second, 20*time.Second, 0, 2), func() error {
			return NewRetriableError(NewRetriable(false), errors.New("can't retry"))
		})
	})

	t.Run("cancel context", func(t *testing.T) {
		mocktime.EXPECT().Sleep(100 * time.Millisecond).Times(2)
		counter := 2
		ctx, cancel := context.WithCancel(context.TODO())
		RetryWithBackoffCtx(ctx, NewSimpleBackoff(100*time.Millisecond, 100*time.Millisecond, 0, 1), func() error {
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

func TestParseBool(t *testing.T) {
	truthyStrings := []string{"true", "1", "t", "true\r", "true ", "true \r"}
	falsyStrings := []string{"false", "0", "f", "false\r", "false ", "false \r"}
	neitherStrings := []string{"apple", " ", "\r", "orange", "maybe"}

	for _, str := range truthyStrings {
		t.Run("truthy", func(t *testing.T) {
			assert.True(t, ParseBool(str, false), "Truthy string should be truthy")
			assert.True(t, ParseBool(str, true), "Truthy string should be truthy (regardless of default)")
		})
	}

	for _, str := range falsyStrings {
		t.Run("falsy", func(t *testing.T) {
			assert.False(t, ParseBool(str, false), "Falsy string should be falsy")
			assert.False(t, ParseBool(str, true), "Falsy string should be falsy (regardless of default)")
		})
	}

	for _, str := range neitherStrings {
		t.Run("defaults", func(t *testing.T) {
			assert.False(t, ParseBool(str, false), "Should default to false")
			assert.True(t, ParseBool(str, true), "Should default to true")
		})
	}
}
