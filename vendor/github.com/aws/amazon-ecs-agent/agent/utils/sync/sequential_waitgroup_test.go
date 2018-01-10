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

package sync

import "testing"

func TestSequentialWaitgroup(t *testing.T) {
	wg := NewSequentialWaitGroup()
	wg.Add(1, 1)
	wg.Add(2, 1)
	wg.Add(1, 1)

	// Wait for '0' should not fail, nothing for sequence numbers below it
	wg.Wait(0)

	done := make(chan bool)
	go func() {
		wg.Done(1)
		wg.Done(1)
		wg.Wait(1)
		wg.Done(2)
		wg.Wait(2)
		done <- true
	}()
	<-done
}

func TestManyDones(t *testing.T) {
	wg := NewSequentialWaitGroup()

	for i := 1; i < 1000; i++ {
		wg.Add(int64(i), i)
	}

	for i := 1; i < 1000; i++ {
		wg.Wait(int64(i - 1))

		isAwake := make(chan bool)
		go func(i int64) {
			wg.Wait(i)
			isAwake <- true
		}(int64(i))

		for j := 0; j < i; j++ {
			if j < i-1 {
				select {
				case <-isAwake:
					t.Fatal("Should not be awake before all dones called")
				default:
				}
			}
			wg.Done(int64(i))
		}
	}
}
