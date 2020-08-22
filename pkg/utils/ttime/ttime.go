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
