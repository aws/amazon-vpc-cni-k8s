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

package netlinkwrapper

import (
	"errors"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/vishvananda/netlink"
)

func TestRetryOnErrDumpInterrupted_Success(t *testing.T) {
	callCount := 0
	err := retryOnErrDumpInterrupted(func() error {
		callCount++
		return nil
	})

	assert.NoError(t, err)
	assert.Equal(t, 1, callCount)
}

func TestRetryOnErrDumpInterrupted_SuccessAfterRetries(t *testing.T) {
	callCount := 0
	err := retryOnErrDumpInterrupted(func() error {
		callCount++
		if callCount < 3 {
			return netlink.ErrDumpInterrupted
		}
		return nil
	})

	assert.NoError(t, err)
	assert.Equal(t, 3, callCount)
}

func TestRetryOnErrDumpInterrupted_NonRecoverableError(t *testing.T) {
	testErr := errors.New("test error")
	callCount := 0
	err := retryOnErrDumpInterrupted(func() error {
		callCount++
		return testErr
	})

	assert.ErrorIs(t, err, testErr)
	assert.Equal(t, 1, callCount)
}

func TestRetryOnErrDumpInterrupted_MaxAttemptsExceeded(t *testing.T) {
	callCount := 0
	err := retryOnErrDumpInterrupted(func() error {
		callCount++
		return netlink.ErrDumpInterrupted
	})

	assert.ErrorIs(t, err, netlink.ErrDumpInterrupted)
	assert.Equal(t, 5, callCount)
	assert.Contains(t, err.Error(), "netlink operation interruption persisted after 5 attempts")
}

func TestRetryOnErrDumpInterrupted_SuccessOnLastAttempt(t *testing.T) {
	callCount := 0
	err := retryOnErrDumpInterrupted(func() error {
		callCount++
		if callCount < 5 {
			return netlink.ErrDumpInterrupted
		}
		return nil
	})

	assert.NoError(t, err)
	assert.Equal(t, 5, callCount)
}

func TestRetryOnErrDumpInterrupted_Delay(t *testing.T) {
	callCount := 0
	var timestamps []time.Time

	err := retryOnErrDumpInterrupted(func() error {
		callCount++
		timestamps = append(timestamps, time.Now())
		if callCount < 4 {
			return netlink.ErrDumpInterrupted
		}
		return nil
	})

	assert.NoError(t, err)
	assert.Equal(t, 4, callCount)

	firstToSecond := timestamps[1].Sub(timestamps[0])
	assert.Less(t, firstToSecond, 50*time.Millisecond, "First retry should happen immediately without delay")

	for i := 2; i < len(timestamps); i++ {
		delay := timestamps[i].Sub(timestamps[i-1])
		assert.GreaterOrEqual(t, delay, 100*time.Millisecond, "Delay before attempt %d should be at least 100ms", i+1)
	}
}

func TestIsNotExistsError(t *testing.T) {
	testCases := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "ESRCH returns true",
			err:      syscall.ESRCH,
			expected: true,
		},
		{
			name:     "ENOENT returns false",
			err:      syscall.ENOENT,
			expected: false,
		},
		{
			name:     "EINVAL returns false",
			err:      syscall.EINVAL,
			expected: false,
		},
		{
			name:     "non-syscall error returns false",
			err:      errors.New("generic error"),
			expected: false,
		},
		{
			name:     "nil error returns false",
			err:      nil,
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := IsNotExistsError(tc.err)
			assert.Equal(t, tc.expected, result)
		})
	}
}
