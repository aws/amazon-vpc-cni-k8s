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

// Implements a simple counting sempahore on top of channels

type empty struct{}
type Semaphore interface {
	Post()
	Wait()
}

// Implements semaphore
type ChanSemaphore struct {
	semaphore chan empty
	Count     int // Public for introspection; should not be written to
}

func NewSemaphore(count int) Semaphore {
	sem := make(chan empty, count)
	// Init to all resources available
	for i := 0; i < count; i++ {
		sem <- empty{}
	}
	return &ChanSemaphore{
		semaphore: sem,
		Count:     count,
	}
}

func (s *ChanSemaphore) Post() {
	s.semaphore <- empty{}
}

func (s *ChanSemaphore) Wait() {
	<-s.semaphore
}
