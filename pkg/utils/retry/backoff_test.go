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
	"testing"
	"time"
)

func TestSimpleBackoff(t *testing.T) {
	sb := NewSimpleBackoff(10*time.Second, time.Minute, 0, 2)

	for i := 0; i < 2; i++ {
		duration := sb.Duration()
		if duration.Nanoseconds() != 10*time.Second.Nanoseconds() {
			t.Error("Initial duration incorrect. Got ", duration.Nanoseconds())
		}

		duration = sb.Duration()
		if duration.Nanoseconds() != 20*time.Second.Nanoseconds() {
			t.Error("Increase incorrect")
		}
		_ = sb.Duration() // 40s
		duration = sb.Duration()
		if duration.Nanoseconds() != 60*time.Second.Nanoseconds() {
			t.Error("Didn't stop at maximum")
		}
		sb.Reset()
		// loop to redo the above tests after resetting, they should be the same
	}
}

func TestJitter(t *testing.T) {
	for i := 0; i < 10; i++ {
		duration := AddJitter(10*time.Second, 3*time.Second)
		if duration < 10*time.Second || duration > 13*time.Second {
			t.Error("Excessive amount of jitter", duration)
		}
	}
}
