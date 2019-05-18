// Copyright 2019 Amazon.com, Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//     http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package publisher

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudwatch"
	"github.com/aws/aws-sdk-go/service/cloudwatch/cloudwatchiface"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
)

const (
	testClusterID       = "TEST_CLUSTER_ID"
	testMetricOne       = "TEST_METRIC_ONE"
	testMonitorDuration = time.Millisecond * 10
)

func TestCloudWatchPubisherWithSingleDatum(t *testing.T) {
	cloudwatchPublisher := getCloudWatchPublisher(t)

	testCloudwatchMetricDatum := &cloudwatch.MetricDatum{
		MetricName: aws.String(testMetricOne),
		Unit:       aws.String(cloudwatch.StandardUnitNone),
		Value:      aws.Float64(1.0),
	}

	cloudwatchPublisher.Publish(testCloudwatchMetricDatum)
	assert.Len(t, cloudwatchPublisher.localMetricData, 1)
	assert.EqualValues(t, cloudwatchPublisher.localMetricData[0], testCloudwatchMetricDatum)

	cloudwatchPublisher.pushLocal()
	assert.Empty(t, cloudwatchPublisher.localMetricData)
}

func TestCloudWatchPubisherWithMultipleDatum(t *testing.T) {
	cloudwatchPublisher := getCloudWatchPublisher(t)

	var metricDataPoints []*cloudwatch.MetricDatum

	for i := 0; i < 10; i++ {
		metricName := "TEST_METRIC_" + strconv.Itoa(i)
		testCloudwatchMetricDatum := &cloudwatch.MetricDatum{
			MetricName: aws.String(metricName),
			Unit:       aws.String(cloudwatch.StandardUnitNone),
			Value:      aws.Float64(1.0),
		}
		metricDataPoints = append(metricDataPoints, testCloudwatchMetricDatum)
	}

	cloudwatchPublisher.Publish(metricDataPoints...)
	assert.Len(t, cloudwatchPublisher.localMetricData, 10)
	cloudwatchPublisher.pushLocal()

	assert.Empty(t, cloudwatchPublisher.localMetricData)
}

func TestCloudWatchPubisherWithGreaterThanMaxDatapoints(t *testing.T) {
	cloudwatchPublisher := getCloudWatchPublisher(t)

	var metricDataPoints []*cloudwatch.MetricDatum

	for i := 0; i < 30; i++ {
		metricName := "TEST_METRIC_" + strconv.Itoa(i)
		testCloudwatchMetricDatum := &cloudwatch.MetricDatum{
			MetricName: aws.String(metricName),
			Unit:       aws.String(cloudwatch.StandardUnitNone),
			Value:      aws.Float64(1.0),
		}
		metricDataPoints = append(metricDataPoints, testCloudwatchMetricDatum)
	}

	cloudwatchPublisher.Publish(metricDataPoints...)
	assert.Len(t, cloudwatchPublisher.localMetricData, 30)
	cloudwatchPublisher.pushLocal()

	assert.Empty(t, cloudwatchPublisher.localMetricData)
}

func TestCloudWatchPubisherWithGreaterThanMaxDatapointsAndStop(t *testing.T) {
	cloudwatchPublisher := getCloudWatchPublisher(t)

	var metricDataPoints []*cloudwatch.MetricDatum
	for i := 0; i < 30; i++ {
		metricName := "TEST_METRIC_" + strconv.Itoa(i)
		testCloudwatchMetricDatum := &cloudwatch.MetricDatum{
			MetricName: aws.String(metricName),
			Unit:       aws.String(cloudwatch.StandardUnitNone),
			Value:      aws.Float64(1.0),
		}
		metricDataPoints = append(metricDataPoints, testCloudwatchMetricDatum)
	}

	cloudwatchPublisher.Publish(metricDataPoints...)
	assert.Len(t, cloudwatchPublisher.localMetricData, 30)

	go cloudwatchPublisher.monitor(testMonitorDuration)

	<-time.After(2 * testMonitorDuration)
	cloudwatchPublisher.Stop()

	assert.Empty(t, cloudwatchPublisher.localMetricData)
}

func TestCloudWatchPubisherWithSingleDatumWithError(t *testing.T) {
	derivedContext, cancel := context.WithCancel(context.TODO())

	mockCloudWatch := mockCloudWatchClient{mockPutMetricDataError: errors.New("test error")}

	cloudwatchPublisher := &cloudWatchPublisher{
		ctx:              derivedContext,
		cancel:           cancel,
		cloudwatchClient: mockCloudWatch,
		clusterID:        testClusterID,
		localMetricData:  make([]*cloudwatch.MetricDatum, 0, localMetricDataSize),
	}

	testCloudwatchMetricDatum := &cloudwatch.MetricDatum{
		MetricName: aws.String(testMetricOne),
		Unit:       aws.String(cloudwatch.StandardUnitNone),
		Value:      aws.Float64(1.0),
	}

	cloudwatchPublisher.Publish(testCloudwatchMetricDatum)
	assert.Len(t, cloudwatchPublisher.localMetricData, 1)
	assert.EqualValues(t, cloudwatchPublisher.localMetricData[0], testCloudwatchMetricDatum)

	cloudwatchPublisher.pushLocal()
	assert.Empty(t, cloudwatchPublisher.localMetricData)
}

func TestCloudWatchPubisherWithGreaterThanMaxDatapointsAndExplicitStartStop(t *testing.T) {
	cloudwatchPublisher := getCloudWatchPublisher(t)

	var metricDataPoints []*cloudwatch.MetricDatum

	for i := 0; i < 30; i++ {
		metricName := "TEST_METRIC_" + strconv.Itoa(i)
		testCloudwatchMetricDatum := &cloudwatch.MetricDatum{
			MetricName: aws.String(metricName),
			Unit:       aws.String(cloudwatch.StandardUnitNone),
			Value:      aws.Float64(1.0),
		}
		metricDataPoints = append(metricDataPoints, testCloudwatchMetricDatum)
	}

	cloudwatchPublisher.Publish(metricDataPoints...)
	assert.Len(t, cloudwatchPublisher.localMetricData, 30)
	go cloudwatchPublisher.Start()

	<-time.After(2 * defaultInterval)
	cloudwatchPublisher.Stop()

	assert.Empty(t, cloudwatchPublisher.localMetricData)
}

func TestGetCloudWatchMetricNamespace(t *testing.T) {
	cloudwatchPublisher := getCloudWatchPublisher(t)

	testNamespace := cloudwatchPublisher.getCloudWatchMetricNamespace()
	assert.Equal(t, aws.StringValue(testNamespace), cloudwatchMetricNamespace)
}

func TestGetCloudWatchMetricDatumDimensions(t *testing.T) {
	cloudwatchPublisher := getCloudWatchPublisher(t)

	expectedCloudwatchDimensions := []*cloudwatch.Dimension{
		&cloudwatch.Dimension{
			Name:  aws.String(clusterIDDimension),
			Value: aws.String(testClusterID),
		},
	}
	testCloudwatchDimensions := cloudwatchPublisher.getCloudWatchMetricDatumDimensions()

	assert.Equal(t, testCloudwatchDimensions, expectedCloudwatchDimensions)
}

func TestGetCloudWatchMetricDatumDimensionsWithMissingClusterID(t *testing.T) {
	cloudwatchPublisher := &cloudWatchPublisher{}

	expectedCloudwatchDimensions := []*cloudwatch.Dimension{
		&cloudwatch.Dimension{
			Name:  aws.String(clusterIDDimension),
			Value: aws.String(""),
		},
	}
	testCloudwatchDimensions := cloudwatchPublisher.getCloudWatchMetricDatumDimensions()

	assert.Equal(t, testCloudwatchDimensions, expectedCloudwatchDimensions)
}

func TestPublishWithNoData(t *testing.T) {
	cloudwatchPublisher := &cloudWatchPublisher{}

	testMetricDataPoints := []*cloudwatch.MetricDatum{}

	cloudwatchPublisher.Publish(testMetricDataPoints...)
	assert.Empty(t, cloudwatchPublisher.localMetricData)
}

func TestPushWithMissingData(t *testing.T) {
	cloudwatchPublisher := &cloudWatchPublisher{}
	testMetricDataPoints := []*cloudwatch.MetricDatum{}

	cloudwatchPublisher.push(testMetricDataPoints)
	assert.Empty(t, cloudwatchPublisher.localMetricData)
}

func TestPublisherNewWithoutClusterID(t *testing.T) {
	cloudwatchPublisher, err := New(context.TODO())
	assert.Error(t, err)
	assert.Nil(t, cloudwatchPublisher)
}

func TestMin(t *testing.T) {
	a, b := 1, 2

	minimum := min(a, b)
	assert.Equal(t, minimum, a)

	minimum = min(b, a)
	assert.Equal(t, minimum, a)
}

// mockCloudWatchClient is used to facilitate testing
type mockCloudWatchClient struct {
	cloudwatchiface.CloudWatchAPI
	mockPutMetricDataError error
}

func (m mockCloudWatchClient) PutMetricData(input *cloudwatch.PutMetricDataInput) (*cloudwatch.PutMetricDataOutput, error) {
	return &cloudwatch.PutMetricDataOutput{}, m.mockPutMetricDataError
}

func getCloudWatchPublisher(t *testing.T) *cloudWatchPublisher {
	// Setup context
	derivedContext, cancel := context.WithCancel(context.TODO())

	return &cloudWatchPublisher{
		ctx:              derivedContext,
		cancel:           cancel,
		cloudwatchClient: mockCloudWatchClient{},
		clusterID:        testClusterID,
		localMetricData:  make([]*cloudwatch.MetricDatum, 0, localMetricDataSize),
	}
}
