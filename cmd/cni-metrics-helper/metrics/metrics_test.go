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

package metrics

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/publisher"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
)

type testMetricsTarget struct {
	metricFile         string
	interestingMetrics map[string]metricsConvert
}

func (target *testMetricsTarget) getLogger() logger.Logger {
	return logger.DefaultLogger()
}

func newTestMetricsTarget(metricFile string, interestingMetrics map[string]metricsConvert) *testMetricsTarget {
	return &testMetricsTarget{
		metricFile:         metricFile,
		interestingMetrics: interestingMetrics}
}

func (target *testMetricsTarget) grabMetricsFromTarget(ctx context.Context, targetName string) ([]byte, error) {
	testMetrics, _ := os.ReadFile(target.metricFile)

	return testMetrics, nil
}

func (target *testMetricsTarget) getInterestingMetrics() map[string]metricsConvert {
	return target.interestingMetrics
}

func (target *testMetricsTarget) getCWMetricsPublisher() publisher.Publisher {
	return nil
}

func (target *testMetricsTarget) getTargetList(ctx context.Context) ([]string, error) {
	return []string{target.metricFile}, nil
}

func (target *testMetricsTarget) submitCloudWatch() bool {
	return false
}

func (target *testMetricsTarget) submitPrometheus() bool {
	return false
}

func TestAPIServerMetric(t *testing.T) {
	testTarget := newTestMetricsTarget("cni_test1.data", InterestingCNIMetrics)
	ctx := context.Background()
	_, _, resetDetected, err := metricsListGrabAggregateConvert(ctx, testTarget)
	assert.NoError(t, err)
	assert.True(t, resetDetected)

	actions := InterestingCNIMetrics["awscni_assigned_ip_addresses"].actions
	// verify awscni_assigned_ip_addresses value
	assert.Equal(t, 1.0, actions[0].data.curSingleDataPoint)

	// verify awscni_ec2api_req_count value
	actions = InterestingCNIMetrics["awscni_ec2api_req_count"].actions
	assert.Equal(t, 21.0, actions[0].data.curSingleDataPoint)

	// verify awscni_ec2api_error_count value
	actions = InterestingCNIMetrics["awscni_ec2api_error_count"].actions
	assert.Equal(t, 0.0, actions[0].data.curSingleDataPoint)

	actions = InterestingCNIMetrics["awscni_total_ip_addresses"].actions
	// verify awscni_total_ip_addresses value
	assert.Equal(t, 10.0, actions[0].data.curSingleDataPoint)

	actions = InterestingCNIMetrics["awscni_aws_api_error_count"].actions
	// verify awscni_aws_api_error_count value
	assert.Equal(t, 14.0, actions[0].data.curSingleDataPoint)

	actions = InterestingCNIMetrics["awscni_eni_allocated"].actions
	// verify awscni_eni_allocated value
	assert.Equal(t, 2.0, actions[0].data.curSingleDataPoint)

	actions = InterestingCNIMetrics["awscni_add_ip_req_count"].actions
	// verify awscni_add_ip_req_count value
	assert.Equal(t, 100.0, actions[0].data.curSingleDataPoint)

	actions = InterestingCNIMetrics["awscni_aws_api_latency_ms"].actions
	// verify apiserver_request_latencies_bucket
	assert.Equal(t, "awsAPILatency", actions[0].cwMetricName)
	assert.Equal(t, 1.0, actions[0].data.curSingleDataPoint)
	assert.Equal(t, 0.0, actions[0].data.lastSingleDataPoint)
}

func TestAPIServerMetricwithPDenabled(t *testing.T) {
	testTarget := newTestMetricsTarget("cni_test2.data", InterestingCNIMetrics)
	ctx := context.Background()
	_, _, _, err := metricsListGrabAggregateConvert(ctx, testTarget)
	assert.NoError(t, err)

	actions := InterestingCNIMetrics["awscni_assigned_ip_addresses"].actions
	// verify awscni_assigned_ip_addresses value
	assert.Equal(t, 1.0, actions[0].data.curSingleDataPoint)

	actions = InterestingCNIMetrics["awscni_total_ip_addresses"].actions
	// verify awscni_total_ip_addresses value
	assert.Equal(t, 16.0, actions[0].data.curSingleDataPoint)

	actions = InterestingCNIMetrics["awscni_total_ipv4_prefixes"].actions
	// verify awscni_total_ipv4_prefixes value
	assert.Equal(t, 1.0, actions[0].data.curSingleDataPoint)

	actions = InterestingCNIMetrics["awscni_assigned_ip_per_cidr"].actions
	// verify awscni_assigned_ip_per_cidr value
	assert.Equal(t, 1.0, actions[0].data.curSingleDataPoint)
}
