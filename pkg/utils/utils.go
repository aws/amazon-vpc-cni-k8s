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
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"math"
	"math/big"
	"reflect"
	"strconv"
	"strings"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/ttime"
)

func DefaultIfBlank(str string, defaultValue string) string {
	if len(str) == 0 {
		return defaultValue
	}
	return str
}

func ZeroOrNil(obj interface{}) bool {
	value := reflect.ValueOf(obj)
	if !value.IsValid() {
		return true
	}
	if obj == nil {
		return true
	}
	switch value.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map:
		return value.Len() == 0
	}
	zero := reflect.Zero(reflect.TypeOf(obj))
	if !value.Type().Comparable() {
		return false
	}
	if obj == zero.Interface() {
		return true
	}
	return false
}

// SlicesDeepEqual checks if slice1 and slice2 are equal, disregarding order.
func SlicesDeepEqual(slice1, slice2 interface{}) bool {
	s1 := reflect.ValueOf(slice1)
	s2 := reflect.ValueOf(slice2)

	if s1.Len() != s2.Len() {
		return false
	}
	if s1.Len() == 0 {
		return true
	}

	s2found := make([]int, s2.Len())
OuterLoop:
	for i := 0; i < s1.Len(); i++ {
		s1el := s1.Slice(i, i+1)
		for j := 0; j < s2.Len(); j++ {
			if s2found[j] == 1 {
				// We already counted this s2 element
				continue
			}
			s2el := s2.Slice(j, j+1)
			if reflect.DeepEqual(s1el.Interface(), s2el.Interface()) {
				s2found[j] = 1
				continue OuterLoop
			}
		}
		// Couldn't find something unused equal to s1
		return false
	}
	return true
}

func RandHex() string {
	randInt, _ := rand.Int(rand.Reader, big.NewInt(math.MaxInt64))
	out := make([]byte, 10)
	binary.PutVarint(out, randInt.Int64())
	return hex.EncodeToString(out)
}

func Strptr(s string) *string {
	return &s
}

var _time ttime.Time = &ttime.DefaultTime{}

// RetryWithBackoff takes a Backoff and a function to call that returns an error
// If the error is nil then the function will no longer be called
// If the error is Retriable then that will be used to determine if it should be
// retried
func RetryWithBackoff(backoff Backoff, fn func() error) error {
	return RetryWithBackoffCtx(context.Background(), backoff, fn)
}

// RetryWithBackoffCtx takes a context, a Backoff, and a function to call that returns an error
// If the context is done, nil will be returned
// If the error is nil then the function will no longer be called
// If the error is Retriable then that will be used to determine if it should be
// retried
func RetryWithBackoffCtx(ctx context.Context, backoff Backoff, fn func() error) error {
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

// RetryNWithBackoff takes a Backoff, a maximum number of tries 'n', and a
// function that returns an error. The function is called until either it does
// not return an error or the maximum tries have been reached.
// If the error returned is Retriable, the Retriability of it will be respected.
// If the number of tries is exhausted, the last error will be returned.
func RetryNWithBackoff(backoff Backoff, n int, fn func() error) error {
	return RetryNWithBackoffCtx(context.Background(), backoff, n, fn)
}

// RetryNWithBackoffCtx takes a context, a Backoff, a maximum number of tries 'n', and a function that returns an error.
// The function is called until it does not return an error, the context is done, or the maximum tries have been
// reached.
// If the error returned is Retriable, the Retriability of it will be respected.
// If the number of tries is exhausted, the last error will be returned.
func RetryNWithBackoffCtx(ctx context.Context, backoff Backoff, n int, fn func() error) error {
	var err error
	RetryWithBackoffCtx(ctx, backoff, func() error {
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

// Uint16SliceToStringSlice converts a slice of type uint16 to a slice of type
// *string. It uses strconv.Itoa on each element
func Uint16SliceToStringSlice(slice []uint16) []*string {
	stringSlice := make([]*string, len(slice))
	for i, el := range slice {
		str := strconv.Itoa(int(el))
		stringSlice[i] = &str
	}
	return stringSlice
}

func StrSliceEqual(s1, s2 []string) bool {
	if len(s1) != len(s2) {
		return false
	}

	for i := 0; i < len(s1); i++ {
		if s1[i] != s2[i] {
			return false
		}
	}
	return true
}

func ParseBool(str string, default_ bool) bool {
	res, err := strconv.ParseBool(strings.TrimSpace(str))
	if err != nil {
		return default_
	}
	return res
}
