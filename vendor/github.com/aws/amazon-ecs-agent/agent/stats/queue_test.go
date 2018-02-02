//+build !integration
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
	"math"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/stretchr/testify/assert"
)

const (
	predictableHighMemoryUtilizationInBytes = 7377772544

	// predictableHighMemoryUtilizationInMiB is the expected Memory usage in MiB for
	// the "predictableHighMemoryUtilizationInBytes" value (7377772544 / (1024 * 1024))
	predictableHighMemoryUtilizationInMiB = 7035
)

func getTimestamps() []time.Time {
	return []time.Time{
		parseNanoTime("2015-02-12T21:22:05.131117533Z"),
		parseNanoTime("2015-02-12T21:22:05.232291187Z"),
		parseNanoTime("2015-02-12T21:22:05.333776335Z"),
		parseNanoTime("2015-02-12T21:22:05.434753595Z"),
		parseNanoTime("2015-02-12T21:22:05.535746779Z"),
		parseNanoTime("2015-02-12T21:22:05.638709495Z"),
		parseNanoTime("2015-02-12T21:22:05.739985398Z"),
		parseNanoTime("2015-02-12T21:22:05.840941705Z"),
		parseNanoTime("2015-02-12T21:22:05.94164351Z"),
		parseNanoTime("2015-02-12T21:22:06.042625519Z"),
		parseNanoTime("2015-02-12T21:22:06.143665077Z"),
		parseNanoTime("2015-02-12T21:22:06.244769169Z"),
		parseNanoTime("2015-02-12T21:22:06.345847001Z"),
		parseNanoTime("2015-02-12T21:22:06.447151399Z"),
		parseNanoTime("2015-02-12T21:22:06.548213586Z"),
		parseNanoTime("2015-02-12T21:22:06.650013301Z"),
		parseNanoTime("2015-02-12T21:22:06.751120187Z"),
		parseNanoTime("2015-02-12T21:22:06.852163377Z"),
		parseNanoTime("2015-02-12T21:22:06.952980001Z"),
		parseNanoTime("2015-02-12T21:22:07.054047217Z"),
		parseNanoTime("2015-02-12T21:22:07.154840095Z"),
		parseNanoTime("2015-02-12T21:22:07.256075769Z"),
	}

}

func getCPUTimes() []uint64 {
	return []uint64{
		22400432,
		116499979,
		248503503,
		372167097,
		502862518,
		638485801,
		780707806,
		911624529,
		1047689820,
		1177013119,
		1313474186,
		1449445062,
		1586294238,
		1719604012,
		1837238842,
		1974606362,
		2112444996,
		2248922292,
		2382142527,
		2516445820,
		2653783456,
		2666483380,
	}
}

func getRandomMemoryUtilizationInBytes() []uint64 {
	return []uint64{
		1839104,
		3649536,
		3649536,
		3649536,
		3649536,
		3649536,
		3649536,
		3649536,
		3649536,
		3649536,
		3649536,
		3649536,
		3649536,
		3649536,
		3649536,
		3649536,
		3649536,
		3649536,
		3649536,
		3649536,
		3649536,
		716800,
	}
}

func getPredictableHighMemoryUtilizationInBytes(size int) []uint64 {
	var memBytes []uint64
	for i := 0; i < size; i++ {
		memBytes = append(memBytes, predictableHighMemoryUtilizationInBytes)
	}
	return memBytes
}

func createQueue(size int, predictableHighMemoryUtilization bool) *Queue {
	timestamps := getTimestamps()
	cpuTimes := getCPUTimes()
	var memoryUtilizationInBytes []uint64
	if predictableHighMemoryUtilization {
		memoryUtilizationInBytes = getPredictableHighMemoryUtilizationInBytes(len(cpuTimes))
	} else {
		memoryUtilizationInBytes = getRandomMemoryUtilizationInBytes()
	}
	queue := NewQueue(size)
	for i, time := range timestamps {
		queue.Add(&ContainerStats{cpuUsage: cpuTimes[i], memoryUsage: memoryUtilizationInBytes[i], timestamp: time})
	}
	return queue
}

func TestQueueAddRemove(t *testing.T) {
	timestamps := getTimestamps()
	queueLength := 5
	// Set predictableHighMemoryUtilization to false, expect random values when aggregated.
	queue := createQueue(queueLength, false)
	buf := queue.buffer
	if len(buf) != queueLength {
		t.Error("Buffer size is incorrect. Expected: 4, Got: ", len(buf))
	}

	timestampsIndex := len(timestamps) - len(buf)
	for i, stat := range buf {
		if stat.Timestamp != timestamps[timestampsIndex+i] {
			t.Error("Unexpected value for Stats element in buffer")
		}
	}

	cpuStatsSet, err := queue.GetCPUStatsSet()
	if err != nil {
		t.Error("Error gettting cpu stats set:", err)
	}
	if *cpuStatsSet.Min == math.MaxFloat64 || math.IsNaN(*cpuStatsSet.Min) {
		t.Error("Min value incorrectly set: ", *cpuStatsSet.Min)
	}
	if *cpuStatsSet.Max == -math.MaxFloat64 || math.IsNaN(*cpuStatsSet.Max) {
		t.Error("Max value incorrectly set: ", *cpuStatsSet.Max)
	}
	if *cpuStatsSet.SampleCount != int64(queueLength) {
		t.Error("Expected samplecount: ", queueLength, " got: ", *cpuStatsSet.SampleCount)
	}
	if *cpuStatsSet.Sum == 0 {
		t.Error("Sum value incorrectly set: ", *cpuStatsSet.Sum)
	}

	memStatsSet, err := queue.GetMemoryStatsSet()
	if err != nil {
		t.Error("Error gettting memory stats set:", err)
	}
	if *memStatsSet.Min == float64(-math.MaxFloat32) {
		t.Error("Min value incorrectly set: ", *memStatsSet.Min)
	}
	if *memStatsSet.Max == 0 {
		t.Error("Max value incorrectly set: ", *memStatsSet.Max)
	}
	if *memStatsSet.SampleCount != int64(queueLength) {
		t.Error("Expected samplecount: ", queueLength, " got: ", *memStatsSet.SampleCount)
	}
	if *memStatsSet.Sum == 0 {
		t.Error("Sum value incorrectly set: ", *memStatsSet.Sum)
	}

	rawUsageStats, err := queue.GetRawUsageStats(2 * queueLength)
	if err != nil {
		t.Error("Error gettting raw usage stats: ", err)
	}

	if len(rawUsageStats) != queueLength {
		t.Error("Expected to get ", queueLength, " raw usage stats. Got: ", len(rawUsageStats))
	}

	prevRawUsageStats := rawUsageStats[0]
	for i := 1; i < queueLength; i++ {
		curRawUsageStats := rawUsageStats[i]
		if prevRawUsageStats.Timestamp.Before(curRawUsageStats.Timestamp) {
			t.Error("Raw usage stats not ordered as expected, index: ", i, " prev stat: ", prevRawUsageStats.Timestamp, " current stat: ", curRawUsageStats.Timestamp)
		}
		prevRawUsageStats = curRawUsageStats
	}

	emptyQueue := NewQueue(queueLength)
	rawUsageStats, err = emptyQueue.GetRawUsageStats(1)
	if err == nil {
		t.Error("Empty queue query did not throw an error")
	}

}

func TestQueueAddPredictableHighMemoryUtilization(t *testing.T) {
	timestamps := getTimestamps()
	queueLength := 5
	// Set predictableHighMemoryUtilization to true
	// This lets us compare the computed values against pre-computed expected values
	queue := createQueue(queueLength, true)
	buf := queue.buffer
	if len(buf) != queueLength {
		t.Error("Buffer size is incorrect. Expected: 4, Got: ", len(buf))
	}

	timestampsIndex := len(timestamps) - len(buf)
	for i, stat := range buf {
		if stat.Timestamp != timestamps[timestampsIndex+i] {
			t.Error("Unexpected value for Stats element in buffer")
		}
	}

	memStatsSet, err := queue.GetMemoryStatsSet()
	if err != nil {
		t.Error("Error gettting memory stats set:", err)
	}

	// Test if both min and max for memory utilization are set to 7035MiB
	// Also test if sum  == queue length * 7035
	expectedMemoryUsageInMiB := float64(predictableHighMemoryUtilizationInMiB)
	if *memStatsSet.Min != expectedMemoryUsageInMiB {
		t.Errorf("Min value incorrectly set: %.0f, expected: %.0f", *memStatsSet.Min, expectedMemoryUsageInMiB)
	}
	if *memStatsSet.Max != expectedMemoryUsageInMiB {
		t.Errorf("Max value incorrectly set: %.0f, expected: %.0f", *memStatsSet.Max, expectedMemoryUsageInMiB)
	}
	if *memStatsSet.SampleCount != int64(queueLength) {
		t.Errorf("Incorrect samplecount, expected: %d got: %d", queueLength, *memStatsSet.SampleCount)
	}

	expectedMemoryUsageInMiBSum := expectedMemoryUsageInMiB * float64(queueLength)
	if *memStatsSet.Sum != expectedMemoryUsageInMiBSum {
		t.Errorf("Sum value incorrectly set: %.0f, expected %.0f", *memStatsSet.Sum, expectedMemoryUsageInMiBSum)
	}
}

func TestCpuStatsSetNotSetToInfinity(t *testing.T) {
	// timestamps will be used to simulate +Inf CPU Usage
	// timestamps[0] = timestamps[1]
	timestamps := []time.Time{
		parseNanoTime("2015-02-12T21:22:05.131117533Z"),
		parseNanoTime("2015-02-12T21:22:05.131117533Z"),
		parseNanoTime("2015-02-12T21:22:05.333776335Z"),
	}
	cpuTimes := []uint64{
		22400432,
		116499979,
		248503503,
	}
	memoryUtilizationInBytes := []uint64{
		3649536,
		3649536,
		3649536,
	}

	// Create and add container stats
	queueLength := 3
	queue := NewQueue(queueLength)
	for i, time := range timestamps {
		queue.Add(&ContainerStats{cpuUsage: cpuTimes[i], memoryUsage: memoryUtilizationInBytes[i], timestamp: time})
	}
	cpuStatsSet, err := queue.GetCPUStatsSet()
	if err != nil {
		t.Errorf("Error getting cpu stats set: %v", err)
	}

	// Compute expected usage by using the 1st and 2nd data point in the input queue
	// queue.Add should have ignored the 0th item as it has the same timestamp as the
	// 1st item
	expectedCpuUsage := 100 * float32(cpuTimes[2]-cpuTimes[1]) / float32(timestamps[2].Nanosecond()-timestamps[1].Nanosecond())
	max := float32(aws.Float64Value(cpuStatsSet.Max))
	if max != expectedCpuUsage {
		t.Errorf("Computed cpuStatsSet.Max (%f) != expected value (%f)", max, expectedCpuUsage)
	}
	sum := float32(aws.Float64Value(cpuStatsSet.Sum))
	if sum != expectedCpuUsage {
		t.Errorf("Computed cpuStatsSet.Sum (%f) != expected value (%f)", sum, expectedCpuUsage)
	}
	min := float32(aws.Float64Value(cpuStatsSet.Min))
	if min != expectedCpuUsage {
		t.Errorf("Computed cpuStatsSet.Min (%f) != expected value (%f)", min, expectedCpuUsage)
	}

	// Expected sample count is 1 and not 2 as one data point would be discarded on
	// account of invalid timestamp
	sampleCount := aws.Int64Value(cpuStatsSet.SampleCount)
	if sampleCount != 1 {
		t.Errorf("Computed cpuStatsSet.SampleCount (%d) != expected value (%d)", sampleCount, 1)
	}
}

func TestResetThresholdElapsed(t *testing.T) {
	// create a queue
	queueLength := 3
	queue := NewQueue(queueLength)

	queue.Reset()

	thresholdElapsed := queue.resetThresholdElapsed(2 * time.Millisecond)
	assert.False(t, thresholdElapsed, "Queue reset threshold is not expected to elapse right after reset")

	time.Sleep(3 * time.Millisecond)
	thresholdElapsed = queue.resetThresholdElapsed(2 * time.Millisecond)

	assert.True(t, thresholdElapsed, "Queue reset threshold is expected to elapse after waiting")
}

func TestEnoughDatapointsInBuffer(t *testing.T) {
	// timestamps will be used to simulate +Inf CPU Usage
	// timestamps[0] = timestamps[1]
	timestamps := []time.Time{
		parseNanoTime("2015-02-12T21:22:05.131117533Z"),
		parseNanoTime("2015-02-12T21:22:05.131117533Z"),
		parseNanoTime("2015-02-12T21:22:05.333776335Z"),
	}
	cpuTimes := []uint64{
		22400432,
		116499979,
		248503503,
	}
	memoryUtilizationInBytes := []uint64{
		3649536,
		3649536,
		3649536,
	}
	// create a queue
	queueLength := 3
	queue := NewQueue(queueLength)

	enoughDataPoints := queue.enoughDatapointsInBuffer()
	assert.False(t, enoughDataPoints, "Queue is expected to not have enough data points right after creation")
	for i, time := range timestamps {
		queue.Add(&ContainerStats{cpuUsage: cpuTimes[i], memoryUsage: memoryUtilizationInBytes[i], timestamp: time})
	}

	enoughDataPoints = queue.enoughDatapointsInBuffer()
	assert.True(t, enoughDataPoints, "Queue is expected to have enough data points when it has more than 2 msgs queued")

	queue.Reset()
	enoughDataPoints = queue.enoughDatapointsInBuffer()
	assert.False(t, enoughDataPoints, "Queue is expected to not have enough data points right after RESET")
}
