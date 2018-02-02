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

package async

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestLRUSimple(t *testing.T) {
	lru := NewLRUCache(10, time.Minute)
	lru.Set("foo", "bar")

	bar, ok := lru.Get("foo")
	assert.True(t, ok)
	assert.Equal(t, bar, "bar")

	baz, ok := lru.Get("fooz")
	assert.False(t, ok)
	assert.Nil(t, baz)

	lru.Delete("foo")
	bar, ok = lru.Get("foo")
	assert.False(t, ok)
	assert.Nil(t, bar)
}

func TestLRUSetDelete(t *testing.T) {
	lru := NewLRUCache(10, time.Minute)

	lru.Set("foo", "bar")
	bar, ok := lru.Get("foo")
	assert.True(t, ok)
	assert.Equal(t, bar, "bar")

	lru.Set("foo", "bar2")
	bar, ok = lru.Get("foo")
	assert.True(t, ok)
	assert.Equal(t, bar, "bar2")

	lru.Delete("foo")
	bar, ok = lru.Get("foo")
	assert.False(t, ok)
	assert.Nil(t, bar)
}

func TestLRUTTlPurge(t *testing.T) {
	lru := NewLRUCache(10, 20*time.Millisecond)
	lru.Set("foo", "bar")

	bar, ok := lru.Get("foo")
	assert.True(t, ok)
	assert.Equal(t, bar, "bar")

	time.Sleep(100 * time.Millisecond)

	bar, ok = lru.Get("foo")
	assert.False(t, ok)
	assert.Nil(t, bar)
}

func TestLRUSizePurge(t *testing.T) {
	lru := NewLRUCache(1, time.Minute)
	lru.Set("foo", "bar")

	bar, ok := lru.Get("foo")
	assert.True(t, ok)
	assert.Equal(t, bar, "bar")

	lru.Set("bar", "baz")

	baz, ok := lru.Get("bar")
	assert.True(t, ok)
	assert.Equal(t, baz, "baz")

	bar, ok = lru.Get("foo")
	assert.False(t, ok)
	assert.Nil(t, bar)
}

func BenchmarkLRUCacheConcurrency(b *testing.B) {
	// prime a cache with %80 size
	size := 10000
	lru := NewLRUCache(size/80, 30*time.Minute)
	for i := 0; i < size; i++ {
		lru.Set(fmt.Sprintf("%d", i), true)
	}

	// fetch or set up to 5x concurrently
	for n := 0; n < 5; n++ {
		go func() {
			for i := 0; i < size; i++ {
				// 1 in 5 times, set the value
				if rand.Intn(5) == 0 {
					lru.Set(fmt.Sprintf("%d", i), true)
				} else {
					lru.Get(fmt.Sprintf("%d", i))
				}
			}
		}()
	}
}

func BenchmarkLRUCacheSet(b *testing.B) {
	// prime a cache as large as the data set
	lru := NewLRUCache(b.N, 30*time.Minute)
	for i := 0; i < b.N; i++ {
		lru.Set(fmt.Sprintf("%d", i), true)
	}
}

func BenchmarkLRUCacheFull(b *testing.B) {
	// prime a cache as large as the data set
	lru := NewLRUCache(b.N, 30*time.Minute)
	for i := 0; i < b.N; i++ {
		lru.Set(fmt.Sprintf("%d", i), true)
	}

	// fetch up to 5x concurrently
	for n := 0; n < 5; n++ {
		go func() {
			for i := 0; i < b.N; i++ {
				lru.Get(fmt.Sprintf("%d", i))
			}
		}()
	}
}

func BenchmarkLRUCacheHalf(b *testing.B) {
	// prime a cache as large as the data set
	lru := NewLRUCache(b.N/2, 30*time.Minute)
	for i := 0; i < b.N; i++ {
		lru.Set(fmt.Sprintf("%d", i), true)
	}

	// fetch up to 5x concurrently
	for n := 0; n < 5; n++ {
		go func() {
			for i := 0; i < b.N; i++ {
				lru.Get(fmt.Sprintf("%d", i))
			}
		}()
	}
}

func BenchmarkLRUCacheWorst(b *testing.B) {
	// prime a cache with only one element
	lru := NewLRUCache(1, 30*time.Minute)
	for i := 0; i < b.N; i++ {
		lru.Set(fmt.Sprintf("%d", i), true)
	}

	// fetch up to 5x concurrently
	for n := 0; n < 5; n++ {
		go func() {
			for i := 0; i < b.N; i++ {
				lru.Get(fmt.Sprintf("%d", i))
			}
		}()
	}
}

func BenchmarkLRUCacheTTLExpiry(b *testing.B) {
	// prime a cache with everything expired immediately
	lru := NewLRUCache(b.N, 0)
	for i := 0; i < b.N; i++ {
		lru.Set(fmt.Sprintf("%d", i), true)
	}

	// fetch up to 5x concurrently
	for n := 0; n < 5; n++ {
		go func() {
			for i := 0; i < b.N; i++ {
				lru.Get(fmt.Sprintf("%d", i))
			}
		}()
	}
}
