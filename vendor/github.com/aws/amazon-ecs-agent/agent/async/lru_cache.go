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
	"container/list"
	"sync"
	"time"
)

type Cache interface {
	// Get fetches a value from cache, returns nil, false on miss
	Get(key string) (Value, bool)
	// Set sets a value in cache. overrites any existing value
	Set(key string, value Value)
	// Delete deletes the value from the cache
	Delete(key string)
}

// Creates an LRUCache with maximum size, ttl for items.
func NewLRUCache(size int, ttl time.Duration) Cache {
	lru := &lruCache{size: size,
		ttl:       ttl,
		cache:     make(map[string]*entry),
		evictList: list.New(),
	}
	return lru
}

type Value interface{}

type entry struct {
	value Value
	added time.Time
}

type lruCache struct {
	sync.Mutex
	cache     map[string]*entry
	evictList *list.List
	size      int
	ttl       time.Duration
}

// Get returns the value associated with the key
func (lru *lruCache) Get(key string) (Value, bool) {
	lru.Lock()
	defer lru.Unlock()

	entry, ok := lru.cache[key]

	if !ok {
		return nil, false
	}

	ok = lru.evictStale(entry, key)
	if !ok {
		return nil, false
	}

	lru.updateAccessed(key)

	return entry.value, true
}

// Set sets the key-value pair in the cache
func (lru *lruCache) Set(key string, value Value) {
	lru.Lock()
	defer lru.Unlock()

	lru.cache[key] = &entry{value: value, added: time.Now()}

	// Remove from the evict list if an entry already existed
	lru.removeFromEvictList(key)

	lru.evictList.PushBack(key)
	lru.purgeSize()
}

// Delete removes the entry associated with the key from cache
func (lru *lruCache) Delete(key string) {
	lru.Lock()
	defer lru.Unlock()

	lru.removeFromEvictList(key)
	delete(lru.cache, key)
}

func (lru *lruCache) updateAccessed(key string) {
	// update evict list
	for elem := lru.evictList.Front(); elem != nil; elem = elem.Next() {
		if elem.Value == key {
			lru.evictList.MoveToBack(elem)
			break
		}
	}
}

func (lru *lruCache) removeFromEvictList(key string) {
	var next *list.Element
	for element := lru.evictList.Front(); element != nil; element = next {
		next = element.Next()
		if element.Value == key {
			lru.evictList.Remove(element)
			break
		}
	}

}

func (lru *lruCache) evictStale(entry *entry, key string) bool {
	if time.Since(entry.added) >= lru.ttl {
		// remove from evict list
		for elem := lru.evictList.Front(); elem != nil; elem = elem.Next() {
			if elem.Value == key {
				lru.evictList.Remove(elem)
				break
			}
		}

		// delete from cache
		delete(lru.cache, key)

		return false
	}

	return true
}

func (lru *lruCache) purgeSize() {
	for elem := lru.evictList.Front(); elem != nil; elem = elem.Next() {
		if len(lru.cache) <= lru.size {
			break
		}
		key := elem.Value.(string)
		// remove from list
		lru.evictList.Remove(elem)

		// delete from cache
		delete(lru.cache, key)
	}
}
