// Copyright 2014-2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/aws/amazon-ecs-agent/agent/tcs/model/ecstcs"
	"github.com/cihub/seelog"
)

const (
	// BytesInMiB is the number of bytes in a MebiByte.
	BytesInMiB = 1024 * 1024
)

const minimumQueueDatapoints = 2

// Queue abstracts a queue using UsageStats slice.
type Queue struct {
	buffer        []UsageStats
	maxSize       int
	lastResetTime time.Time
	bufferLock    sync.RWMutex
}

// NewQueue creates a queue.
func NewQueue(maxSize int) *Queue {
	return &Queue{
		buffer:  make([]UsageStats, 0, maxSize),
		maxSize: maxSize,
	}
}

// Reset resets the stats queue.
func (queue *Queue) Reset() {
	queue.bufferLock.Lock()
	defer queue.bufferLock.Unlock()
	queue.lastResetTime = time.Now()
	queue.buffer = queue.buffer[:0]
}

// Add adds a new set of container stats to the queue.
func (queue *Queue) Add(rawStat *ContainerStats) {
	queue.bufferLock.Lock()
	defer queue.bufferLock.Unlock()

	queueLength := len(queue.buffer)
	stat := UsageStats{
		CPUUsagePerc:      float32(nan32()),
		MemoryUsageInMegs: uint32(rawStat.memoryUsage / BytesInMiB),
		Timestamp:         rawStat.timestamp,
		cpuUsage:          rawStat.cpuUsage,
	}
	if queueLength != 0 {
		// % utilization can be calculated only when queue is non-empty.
		lastStat := queue.buffer[queueLength-1]
		timeSinceLastStat := float32(rawStat.timestamp.Sub(lastStat.Timestamp).Nanoseconds())
		if timeSinceLastStat > 0 {
			cpuUsageSinceLastStat := float32(rawStat.cpuUsage - lastStat.cpuUsage)
			stat.CPUUsagePerc = 100 * cpuUsageSinceLastStat / timeSinceLastStat
		} else {
			// Ignore the stat if the current timestamp is same as the last one. This
			// results in the value being set as +infinity
			// float32(1) / float32(0) = +Inf
			seelog.Debugf("time since last stat is zero. Ignoring cpu stat")
		}
		if queue.maxSize == queueLength {
			// Remove first element if queue is full.
			queue.buffer = queue.buffer[1:queueLength]
		}
	}

	queue.buffer = append(queue.buffer, stat)
}

// GetCPUStatsSet gets the stats set for CPU utilization.
func (queue *Queue) GetCPUStatsSet() (*ecstcs.CWStatsSet, error) {
	return queue.getCWStatsSet(getCPUUsagePerc)
}

// GetMemoryStatsSet gets the stats set for memory utilization.
func (queue *Queue) GetMemoryStatsSet() (*ecstcs.CWStatsSet, error) {
	return queue.getCWStatsSet(getMemoryUsagePerc)
}

// GetRawUsageStats gets the array of most recent raw UsageStats, in descending
// order of timestamps.
func (queue *Queue) GetRawUsageStats(numStats int) ([]UsageStats, error) {
	queue.bufferLock.Lock()
	defer queue.bufferLock.Unlock()

	queueLength := len(queue.buffer)
	if queueLength == 0 {
		return nil, fmt.Errorf("No data in the queue")
	}

	if numStats > queueLength {
		numStats = queueLength
	}

	usageStats := make([]UsageStats, numStats)
	for i := 0; i < numStats; i++ {
		// Order such that usageStats[i].timestamp > usageStats[i+1].timestamp
		rawUsageStat := queue.buffer[queueLength-i-1]
		usageStats[i] = UsageStats{
			CPUUsagePerc:      rawUsageStat.CPUUsagePerc,
			MemoryUsageInMegs: rawUsageStat.MemoryUsageInMegs,
			Timestamp:         rawUsageStat.Timestamp,
		}
	}

	return usageStats, nil
}

func getCPUUsagePerc(s *UsageStats) float64 {
	return float64(s.CPUUsagePerc)
}

func getMemoryUsagePerc(s *UsageStats) float64 {
	return float64(s.MemoryUsageInMegs)
}

type getUsageFunc func(*UsageStats) float64

func (queue *Queue) resetThresholdElapsed(timeout time.Duration) bool {
	queue.bufferLock.RLock()
	defer queue.bufferLock.RUnlock()
	duration := time.Since(queue.lastResetTime)
	return duration.Seconds() > timeout.Seconds()
}

func (queue *Queue) enoughDatapointsInBuffer() bool {
	queue.bufferLock.RLock()
	defer queue.bufferLock.RUnlock()
	return len(queue.buffer) >= minimumQueueDatapoints
}

// getCWStatsSet gets the stats set for either CPU or Memory based on the
// function pointer.
func (queue *Queue) getCWStatsSet(f getUsageFunc) (*ecstcs.CWStatsSet, error) {
	queue.bufferLock.Lock()
	defer queue.bufferLock.Unlock()

	queueLength := len(queue.buffer)
	if queueLength < 2 {
		// Need at least 2 data points to calculate this.
		return nil, fmt.Errorf("No data in the queue")
	}

	var min, max, sum float64
	var sampleCount int64
	min = math.MaxFloat64
	max = -math.MaxFloat64
	sum = 0
	sampleCount = 0

	for _, stat := range queue.buffer {
		perc := f(&stat)
		if math.IsNaN(perc) {
			continue
		}

		min = math.Min(min, perc)
		max = math.Max(max, perc)
		sampleCount++
		sum += perc
	}

	return &ecstcs.CWStatsSet{
		Max:         &max,
		Min:         &min,
		SampleCount: &sampleCount,
		Sum:         &sum,
	}, nil
}
