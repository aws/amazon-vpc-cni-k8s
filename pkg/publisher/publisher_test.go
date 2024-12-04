// Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
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

	"github.com/pkg/errors"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/stretchr/testify/assert"
)

const (
	testClusterID       = "TEST_CLUSTER_ID"
	testMetricOne       = "TEST_METRIC_ONE"
	testMonitorDuration = time.Millisecond * 10
)

func TestCloudWatchPublisherWithNoIMDS(t *testing.T) {
	log := getCloudWatchLog()
	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()

	region := "us-west-2"
	clusterID := testClusterID

	cw, err := New(ctx, region, clusterID, log)
	assert.NoError(t, err)
	assert.NotNil(t, cw)
}

func TestCloudWatchPublisherWithSingleDatum(t *testing.T) {
	cloudwatchPublisher := getCloudWatchPublisher(t)

	testCloudwatchMetricDatum := types.MetricDatum{
		MetricName: aws.String(testMetricOne),
		Unit:       types.StandardUnitNone,
		Value:      aws.Float64(1.0),
		Dimensions: []types.Dimension{
			{
				Name:  aws.String(clusterIDDimension),
				Value: aws.String(testClusterID),
			},
		},
	}

	cloudwatchPublisher.Publish(testCloudwatchMetricDatum)
	assert.Len(t, cloudwatchPublisher.localMetricData, 1)
	assert.EqualValues(t, cloudwatchPublisher.localMetricData[0], testCloudwatchMetricDatum)

	cloudwatchPublisher.pushLocal()
	assert.Empty(t, cloudwatchPublisher.localMetricData)
}

func TestCloudWatchPublisherWithMultipleDatum(t *testing.T) {
	cloudwatchPublisher := getCloudWatchPublisher(t)

	var metricDataPoints []types.MetricDatum

	for i := 0; i < 10; i++ {
		metricName := "TEST_METRIC_" + strconv.Itoa(i)
		testCloudwatchMetricDatum := types.MetricDatum{
			MetricName: aws.String(metricName),
			Unit:       types.StandardUnitNone,
			Value:      aws.Float64(1.0),
		}
		metricDataPoints = append(metricDataPoints, testCloudwatchMetricDatum)
	}

	cloudwatchPublisher.Publish(metricDataPoints...)
	assert.Len(t, cloudwatchPublisher.localMetricData, 10)
	cloudwatchPublisher.pushLocal()

	assert.Empty(t, cloudwatchPublisher.localMetricData)
}

func TestCloudWatchPublisherWithGreaterThanMaxDatapoints(t *testing.T) {
	cloudwatchPublisher := getCloudWatchPublisher(t)

	var metricDataPoints []types.MetricDatum

	for i := 0; i < 30; i++ {
		metricName := "TEST_METRIC_" + strconv.Itoa(i)
		testCloudwatchMetricDatum := types.MetricDatum{
			MetricName: aws.String(metricName),
			Unit:       types.StandardUnitNone,
			Value:      aws.Float64(1.0),
		}
		metricDataPoints = append(metricDataPoints, testCloudwatchMetricDatum)
	}

	cloudwatchPublisher.Publish(metricDataPoints...)
	assert.Len(t, cloudwatchPublisher.localMetricData, 30)
	cloudwatchPublisher.pushLocal()

	assert.Empty(t, cloudwatchPublisher.localMetricData)
}

func TestCloudWatchPublisherWithGreaterThanMaxDatapointsAndStop(t *testing.T) {
	cloudwatchPublisher := getCloudWatchPublisher(t)

	var metricDataPoints []types.MetricDatum
	for i := 0; i < 30; i++ {
		metricName := "TEST_METRIC_" + strconv.Itoa(i)
		testCloudwatchMetricDatum := types.MetricDatum{
			MetricName: aws.String(metricName),
			Unit:       types.StandardUnitNone,
			Value:      aws.Float64(1.0),
		}
		metricDataPoints = append(metricDataPoints, testCloudwatchMetricDatum)
	}

	cloudwatchPublisher.Publish(metricDataPoints...)
	assert.Len(t, cloudwatchPublisher.localMetricData, 30)

	go cloudwatchPublisher.monitor(testMonitorDuration)

	// Delays added to prevent test flakiness
	<-time.After(5 * testMonitorDuration)
	cloudwatchPublisher.Stop()
	<-time.After(5 * testMonitorDuration)

	assert.Empty(t, cloudwatchPublisher.localMetricData)
}

func TestCloudWatchPublisherWithSingleDatumWithError(t *testing.T) {
	derivedContext, cancel := context.WithCancel(context.TODO())

	// Create a mock cloudwatch client that will return an error when PutMetricData is called
	mockCloudWatch := mockCloudWatchClient{
		mockPutMetricDataError: errors.New("error"),
	}

	cloudwatchPublisher := &cloudWatchPublisher{
		ctx:              derivedContext,
		cancel:           cancel,
		cloudwatchClient: &mockCloudWatch,
		clusterID:        testClusterID,
		localMetricData:  make([]types.MetricDatum, 0, localMetricDataSize),
		log:              getCloudWatchLog(),
	}

	testCloudwatchMetricDatum := types.MetricDatum{
		MetricName: aws.String(testMetricOne),
		Unit:       types.StandardUnitNone,
		Value:      aws.Float64(1.0),
		Dimensions: []types.Dimension{
			{
				Name:  aws.String(clusterIDDimension),
				Value: aws.String(testClusterID),
			},
		},
	}

	cloudwatchPublisher.Publish(testCloudwatchMetricDatum)
	assert.Len(t, cloudwatchPublisher.localMetricData, 1)
	assert.EqualValues(t, cloudwatchPublisher.localMetricData[0], testCloudwatchMetricDatum)

	cloudwatchPublisher.pushLocal()
	assert.Empty(t, cloudwatchPublisher.localMetricData)
}

func TestGetCloudWatchMetricNamespace(t *testing.T) {
	cloudwatchPublisher := getCloudWatchPublisher(t)

	testNamespace := cloudwatchPublisher.getCloudWatchMetricNamespace()
	assert.Equal(t, aws.ToString(testNamespace), cloudwatchMetricNamespace)
}

func TestGetCloudWatchMetricDatumDimensions(t *testing.T) {
	cloudwatchPublisher := getCloudWatchPublisher(t)

	expectedCloudwatchDimensions := []types.Dimension{
		{
			Name:  aws.String(clusterIDDimension),
			Value: aws.String(testClusterID),
		},
	}
	testCloudwatchDimensions := cloudwatchPublisher.getCloudWatchMetricDatumDimensions()

	assert.Equal(t, testCloudwatchDimensions, expectedCloudwatchDimensions)
}

func TestGetCloudWatchMetricDatumDimensionsWithMissingClusterID(t *testing.T) {
	cloudwatchPublisher := &cloudWatchPublisher{log: getCloudWatchLog()}

	expectedCloudwatchDimensions := []types.Dimension{
		{
			Name:  aws.String(clusterIDDimension),
			Value: aws.String(""),
		},
	}
	testCloudwatchDimensions := cloudwatchPublisher.getCloudWatchMetricDatumDimensions()

	assert.Equal(t, testCloudwatchDimensions, expectedCloudwatchDimensions)
}

func TestPublishWithNoData(t *testing.T) {
	cloudwatchPublisher := &cloudWatchPublisher{log: getCloudWatchLog()}

	testMetricDataPoints := []types.MetricDatum{}

	cloudwatchPublisher.Publish(testMetricDataPoints...)
	assert.Empty(t, cloudwatchPublisher.localMetricData)
}

func TestPushWithMissingData(t *testing.T) {
	cloudwatchPublisher := &cloudWatchPublisher{log: getCloudWatchLog()}
	testMetricDataPoints := []types.MetricDatum{}

	cloudwatchPublisher.push(testMetricDataPoints)
	assert.Empty(t, cloudwatchPublisher.localMetricData)
}

func TestMin(t *testing.T) {
	a, b := 1, 2

	minimum := min(a, b)
	assert.Equal(t, minimum, a)

	minimum = min(b, a)
	assert.Equal(t, minimum, a)
}

// mockCloudWatchClient is used to facilitate testing and implements the cloudwatch.Client interface
type mockCloudWatchClient struct {
	cloudwatch.Client
	mockPutMetricDataError error
}

func (m *mockCloudWatchClient) PutMetricData(ctx context.Context, params *cloudwatch.PutMetricDataInput, optFns ...func(*cloudwatch.Options)) (*cloudwatch.PutMetricDataOutput, error) {
	return &cloudwatch.PutMetricDataOutput{}, m.mockPutMetricDataError
}

// Implement other methods of the cloudwatch.Client interface as needed for testing.

func getCloudWatchLog() logger.Logger {
	logConfig := logger.Configuration{
		LogLevel:    "Debug",
		LogLocation: "stdout",
	}
	return logger.New(&logConfig)
}

func getCloudWatchPublisher(t *testing.T) *cloudWatchPublisher {
	// Setup context
	derivedContext, cancel := context.WithCancel(context.TODO())

	return &cloudWatchPublisher{
		ctx:              derivedContext,
		cancel:           cancel,
		cloudwatchClient: &mockCloudWatchClient{},
		clusterID:        testClusterID,
		localMetricData:  make([]types.MetricDatum, 0, localMetricDataSize),
		log:              getCloudWatchLog(),
	}
}
