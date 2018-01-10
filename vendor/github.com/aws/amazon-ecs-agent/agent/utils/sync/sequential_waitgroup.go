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

// Package sync is an analogue to the stdlib sync package.
// It contains lowlevel synchonization primitives, but not quite as low level as 'sync' does
package sync

import stdsync "sync"

// A SequentialWaitGroup waits for a collection of goroutines to finish. Each
// goroutine may add itself to the waitgroup with 'Add', providing a sequence
// number. Each goroutine should then call 'Done' with its sequence number when finished.
// Elsewhere, 'Wait' can be used to wait for all groups at or below the
// provided sequence number to complete.
type SequentialWaitGroup struct {
	mutex stdsync.Mutex
	// Implement our own semaphore over using sync.WaitGroup so that we can safely GC our map
	semaphores map[int64]int
	change     *stdsync.Cond
}

func NewSequentialWaitGroup() *SequentialWaitGroup {
	return &SequentialWaitGroup{
		semaphores: make(map[int64]int),
		change:     stdsync.NewCond(&stdsync.Mutex{}),
	}
}

// Add adds the given delta to the waitgroup at the given sequence
func (s *SequentialWaitGroup) Add(sequence int64, delta int) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	_, ok := s.semaphores[sequence]
	if ok {
		s.semaphores[sequence] += delta
	} else {
		s.semaphores[sequence] = delta
	}
	if s.semaphores[sequence] <= 0 {
		delete(s.semaphores, sequence)
		s.change.Broadcast()
	}
}

// Done decrements the waitgroup at the given sequence by one
func (s *SequentialWaitGroup) Done(sequence int64) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	_, ok := s.semaphores[sequence]
	if ok {
		s.semaphores[sequence]--
		if s.semaphores[sequence] == 0 {
			delete(s.semaphores, sequence)
			s.change.Broadcast()
		}
	}
}

// Wait waits for all waitgroups at or below the given sequence to complete.
// Please note that this is *INCLUSIVE* of the sequence
func (s *SequentialWaitGroup) Wait(sequence int64) {
	waitOver := func() bool {
		s.mutex.Lock()
		defer s.mutex.Unlock()
		for storedSequence := range s.semaphores {
			if storedSequence <= sequence {
				// At least one non-empty seqnum greater than ours; wait more
				return false
			}
		}
		return true
	}

	s.change.L.Lock()
	defer s.change.L.Unlock()
	// Wake up to check if our wait is over after each element being deleted from the map
	for !waitOver() {
		s.change.Wait()
	}
}
