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

package stats

import (
	"math"
	"regexp"
	"runtime"
	"time"

	"github.com/cihub/seelog"
)

// networkStatsErrorPattern defines the pattern that is used to evaluate
// if there's an error reading network stats.
const networkStatsErrorPattern = "open /sys/class/net/veth.*: no such file or directory"

var numCores = uint64(runtime.NumCPU())

// nan32 returns a 32bit NaN.
func nan32() float32 {
	return (float32)(math.NaN())
}

// parseNanoTime returns the time object from a string formatted with RFC3339Nano layout.
func parseNanoTime(value string) time.Time {
	ts, _ := time.Parse(time.RFC3339Nano, value)
	return ts
}

// isNetworkStatsError returns if the error indicates that files in /sys/class/net
// could not be opened.
func isNetworkStatsError(err error) bool {
	matched, mErr := regexp.MatchString(networkStatsErrorPattern, err.Error())
	if mErr != nil {
		seelog.Debugf("Error matching string: %v", mErr)
		return false
	}

	return matched
}
