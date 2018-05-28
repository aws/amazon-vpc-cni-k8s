package metrics

import (
	"io/ioutil"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/publisher"
)

type testMetricsTarget struct {
	metricFile         string
	interestingMetrics map[string]metricsConvert
	targetList         []string
}

func newTestMetricsTarget(metricFile string, interestingMetrics map[string]metricsConvert) *testMetricsTarget {
	return &testMetricsTarget{
		metricFile:         metricFile,
		interestingMetrics: interestingMetrics}
}

func (target *testMetricsTarget) grabMetricsFromTarget(targetName string) ([]byte, error) {
	testMetrics, _ := ioutil.ReadFile(target.metricFile)

	return testMetrics, nil
}

func (target *testMetricsTarget) getInterestingMetrics() map[string]metricsConvert {
	return target.interestingMetrics
}

func (target *testMetricsTarget) getCWContext() publisher.Publisher {
	return nil
}

func (target *testMetricsTarget) getTargetList() []string {
	return []string{target.metricFile}
}

func (target *testMetricsTarget) submitCloudWatch() bool {
	return false
}

func TestAPIServerMetric(t *testing.T) {
	testTarget := newTestMetricsTarget("apiserver_test1.data", InterestingAPIServerMetrics)

	_, _, resetDetected, err := metricsListGrabAggregateConvert(testTarget)
	assert.NoError(t, err)
	assert.True(t, resetDetected)

	actions := InterestingAPIServerMetrics["apiserver_request_count"].actions
	// verify apiserverRequestCount aggregated value
	assert.Equal(t, actions[0].data.curSingleDataPoint, 2.567083e+06)
	// verify apiserverRequestErrCount aggegated value
	assert.Equal(t, actions[1].data.curSingleDataPoint, float64(7394))

	actions = InterestingAPIServerMetrics["apiserver_request_latencies_summary"].actions
	// verify apiserverLatencyP99x value
	assert.Equal(t, actions[0].data.curSingleDataPoint, float64(19907))

	actions = InterestingAPIServerMetrics["apiserver_request_latencies"].actions
	// verify apiserver_request_latencies_bucket
	assert.Equal(t, *actions[0].bucket.curBucket[0].CumulativeCount, 2.389851e+06)

	// 2nd time  which generate diff for counter/histogram/percentile
	testTarget = newTestMetricsTarget("apiserver_test2.data", InterestingAPIServerMetrics)
	_, _, resetDetected, err = metricsListGrabAggregateConvert(testTarget)
	assert.NoError(t, err)
	assert.False(t, resetDetected)

	actions = InterestingAPIServerMetrics["apiserver_request_count"].actions
	// verify apiserverRequestCount aggregated value
	assert.Equal(t, actions[0].data.curSingleDataPoint, float64(162))
	// verify apiserverRequestErrCount aggegated value
	assert.Equal(t, actions[1].data.curSingleDataPoint, float64(0))

	actions = InterestingAPIServerMetrics["apiserver_request_latencies_summary"].actions
	// verify apiserverLatencyP99x value
	assert.Equal(t, actions[0].data.curSingleDataPoint, float64(19907))

	actions = InterestingAPIServerMetrics["apiserver_request_latencies"].actions
	// verify apiserver_request_latencies_bucket
	assert.Equal(t, *actions[0].bucket.curBucket[0].CumulativeCount, float64(160))

	// test reset
	testTarget = newTestMetricsTarget("apiserver_test1.data", InterestingAPIServerMetrics)

	_, _, resetDetected, err = metricsListGrabAggregateConvert(testTarget)
	assert.NoError(t, err)
	assert.True(t, resetDetected)

}

func TestKubeMetric(t *testing.T) {
	testTarget := newTestMetricsTarget("kube_test.data", InterestingKubeStateMetrics)

	_, _, _, err := metricsListGrabAggregateConvert(testTarget)
	assert.NoError(t, err)

	actions := InterestingKubeStateMetrics["kube_node_info"].actions
	assert.Equal(t, actions[0].data.curSingleDataPoint, float64(11))

	actions = InterestingKubeStateMetrics["kube_endpoint_info"].actions
	assert.Equal(t, actions[0].data.curSingleDataPoint, float64(3))

	actions = InterestingKubeStateMetrics["kube_pod_info"].actions
	assert.Equal(t, actions[0].data.curSingleDataPoint, float64(26))

	actions = InterestingKubeStateMetrics["kube_service_info"].actions
	assert.Equal(t, actions[0].data.curSingleDataPoint, float64(2))
}
