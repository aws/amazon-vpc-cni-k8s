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

type Timer interface {
	Reset(d time.Duration) bool
	Stop() bool
}

// DefaultTime is a Time that behaves normally
type DefaultTime struct{}

var _time Time = &DefaultTime{}

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

// SetTime configures what 'Time' implementation to use for each of the
// package-level methods.
func SetTime(t Time) {
	_time = t
}

// Now returns the implementation's current time
func Now() time.Time {
	return _time.Now()
}

// Since returns the time different from Now and the given time t
func Since(t time.Time) time.Duration {
	return _time.Now().Sub(t)
}
