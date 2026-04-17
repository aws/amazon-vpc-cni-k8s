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

// Package ttime implements a testable alternative to the Go "time" package.
package ttime

import "time"

// Time represents an implementation for this package's methods
type Time interface {
	Now() time.Time
	Sleep(d time.Duration)
	After(d time.Duration) <-chan time.Time
	AfterFunc(d time.Duration, f func()) Timer
}

// Timer is the timer interface
type Timer interface {
	Reset(d time.Duration) bool
	Stop() bool
}

// DefaultTime is a Time that behaves normally
type DefaultTime struct{}

// Now returns the current time
func (*DefaultTime) Now() time.Time {
	return time.Now()
}

// Sleep sleeps for the given duration
func (*DefaultTime) Sleep(d time.Duration) {
	time.Sleep(d)
}

// After sleeps for the given duration and then writes to to the returned channel
func (*DefaultTime) After(d time.Duration) <-chan time.Time {
	return time.After(d)
}

// AfterFunc waits for the duration to elapse and then calls f in its own
// goroutine. It returns a Timer that can be used to cancel the call using its
// Stop method.
func (*DefaultTime) AfterFunc(d time.Duration, f func()) Timer {
	return time.AfterFunc(d, f)
}
