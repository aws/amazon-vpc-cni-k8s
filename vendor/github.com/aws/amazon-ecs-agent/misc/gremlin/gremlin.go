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

package main

import (
	"math"
	"math/rand"
	"time"
)

const defaultRunDuration = 60 * time.Second

type loiterFunc func()

func noop() {
}

func allocateMemory() {
	// Assign memory to a slice.
	arr := make([]float64, math.MaxUint16)
	for i := 0; i < math.MaxUint8; i++ {
		arr[i] = rand.Float64()
	}
}

func wieldScissor() {
	// Combined workload.
	timeoutAndMoveOn(noop, 1*time.Millisecond)
	timeoutAndMoveOn(allocateMemory, 10*time.Millisecond)
	time.Sleep(5 * time.Millisecond)
}

// timeoutAndMoveOn executes the long running function pointed to by 'fp' and
// times out after 'timeout' duration.
func timeoutAndMoveOn(fp loiterFunc, timeout time.Duration) {
	ch := time.After(timeout)
	for {
		select {
		case <-ch:
			return
		default:
			fp()
		}
	}
}

func main() {
	timeoutAndMoveOn(wieldScissor, defaultRunDuration)
}
