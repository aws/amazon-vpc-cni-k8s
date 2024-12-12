package metrics

import (
	"fmt"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	testclient "sigs.k8s.io/controller-runtime/pkg/client/fake"

	eniconfigscheme "github.com/aws/amazon-vpc-cni-k8s/pkg/apis/crd/v1alpha1"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/publisher/mock_publisher"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
	"github.com/aws/amazon-vpc-cni-k8s/utils/prometheusmetrics"
	"github.com/aws/aws-sdk-go-v2/aws"
	cloudwatchtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

var logConfig = logger.Configuration{
	LogLevel:    "Debug",
	LogLocation: "stdout",
}

var testLog = logger.New(&logConfig)

type testMocks struct {
	clientset     *k8sfake.Clientset
	podWatcher    *defaultPodWatcher
	mockPublisher *mock_publisher.MockPublisher
}

func setup(t *testing.T) *testMocks {
	ctrl := gomock.NewController(t)
	fakeClientset := k8sfake.NewSimpleClientset()
	k8sSchema := runtime.NewScheme()
	clientgoscheme.AddToScheme(k8sSchema)
	eniconfigscheme.AddToScheme(k8sSchema)
	podWatcher := NewDefaultPodWatcher(testclient.NewClientBuilder().WithScheme(k8sSchema).WithRuntimeObjects().Build(), testLog)
	return &testMocks{
		clientset:     fakeClientset,
		podWatcher:    podWatcher,
		mockPublisher: mock_publisher.NewMockPublisher(ctrl),
	}
}

func TestCNIMetricsNew(t *testing.T) {
	m := setup(t)
	ctx := context.Background()
	_, _ = m.clientset.CoreV1().Pods("kube-system").Create(ctx, &v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "aws-node-1"}}, metav1.CreateOptions{})
	// cniMetric := CNIMetricsNew(m.clientset, m.mockPublisher, m.discoverController, false, log)
	cniMetric := CNIMetricsNew(m.clientset, m.mockPublisher, false, false, testLog, m.podWatcher)
	assert.NotNil(t, cniMetric)
	assert.NotNil(t, cniMetric.getCWMetricsPublisher())
	assert.NotEmpty(t, cniMetric.getInterestingMetrics())
	assert.Equal(t, testLog, cniMetric.getLogger())
	assert.False(t, cniMetric.submitCloudWatch())
}

// Add these helper functions at the top of the test file
func createTestMetricFamilies() map[string]*dto.MetricFamily {
	return map[string]*dto.MetricFamily{
		"awscni_eni_max": {
			Name: aws.String("awscni_eni_max"),
			Type: dto.MetricType_GAUGE.Enum(),
			Metric: []*dto.Metric{{
				Gauge: &dto.Gauge{Value: aws.Float64(10.0)},
			}},
		},
		"awscni_ip_max": {
			Name: aws.String("awscni_ip_max"),
			Type: dto.MetricType_GAUGE.Enum(),
			Metric: []*dto.Metric{{
				Gauge: &dto.Gauge{Value: aws.Float64(20.0)},
			}},
		},
		"awscni_eni_allocated": {
			Name: aws.String("awscni_eni_allocated"),
			Type: dto.MetricType_GAUGE.Enum(),
			Metric: []*dto.Metric{{
				Gauge: &dto.Gauge{Value: aws.Float64(3.0)},
			}},
		},
		"awscni_total_ip_addresses": {
			Name: aws.String("awscni_total_ip_addresses"),
			Type: dto.MetricType_GAUGE.Enum(),
			Metric: []*dto.Metric{{
				Gauge: &dto.Gauge{Value: aws.Float64(30.0)},
			}},
		},
		"awscni_assigned_ip_addresses": {
			Name: aws.String("awscni_assigned_ip_addresses"),
			Type: dto.MetricType_GAUGE.Enum(),
			Metric: []*dto.Metric{{
				Gauge: &dto.Gauge{Value: aws.Float64(15.0)},
			}},
		},
	}
}

func createTestConvertDef(includeCloudWatch bool) map[string]metricsConvert {
	testData := []struct {
		metricName   string
		value        float64
		cwMetricName string
	}{
		{"awscni_eni_max", 10.0, "eni_max"},
		{"awscni_ip_max", 20.0, "ip_max"},
		{"awscni_eni_allocated", 3.0, "eni_allocated"},
		{"awscni_total_ip_addresses", 30.0, "total_ip_addresses"},
		{"awscni_assigned_ip_addresses", 15.0, "assigned_ip_addresses"},
	}

	result := make(map[string]metricsConvert)
	for _, td := range testData {
		action := metricsAction{
			data: &dataPoints{curSingleDataPoint: td.value},
		}
		if includeCloudWatch {
			action.cwMetricName = td.cwMetricName
		}
		result[td.metricName] = metricsConvert{
			actions: []metricsAction{action},
		}
	}
	return result
}

func createExpectedCloudWatchMetrics() []cloudwatchtypes.MetricDatum {
	return []cloudwatchtypes.MetricDatum{
		{
			MetricName: aws.String("eni_max"),
			Unit:       cloudwatchtypes.StandardUnitCount,
			Value:      aws.Float64(10.0),
		},
		{
			MetricName: aws.String("ip_max"),
			Unit:       cloudwatchtypes.StandardUnitCount,
			Value:      aws.Float64(20.0),
		},
		{
			MetricName: aws.String("eni_allocated"),
			Unit:       cloudwatchtypes.StandardUnitCount,
			Value:      aws.Float64(3.0),
		},
		{
			MetricName: aws.String("total_ip_addresses"),
			Unit:       cloudwatchtypes.StandardUnitCount,
			Value:      aws.Float64(30.0),
		},
		{
			MetricName: aws.String("assigned_ip_addresses"),
			Unit:       cloudwatchtypes.StandardUnitCount,
			Value:      aws.Float64(15.0),
		},
	}
}

func TestProduceCloudWatchMetrics(t *testing.T) {
	m := setup(t)
	cniMetric := CNIMetricsNew(m.clientset, m.mockPublisher, true, false, testLog, m.podWatcher)

	families := createTestMetricFamilies()
	testConvertDef := createTestConvertDef(true)
	expectedMetrics := createExpectedCloudWatchMetrics()

	// Expect CloudWatch publish to be called for each metric
	for _, expectedMetric := range expectedMetrics {
		m.mockPublisher.EXPECT().Publish(expectedMetric).Times(1)
	}

	err := produceCloudWatchMetrics(cniMetric, families, testConvertDef, m.mockPublisher)
	assert.NoError(t, err)
}

func TestProducePrometheusMetrics(t *testing.T) {
	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	m := setup(t)
	cniMetric := CNIMetricsNew(m.clientset, m.mockPublisher, false, true, testLog, m.podWatcher)

	families := createTestMetricFamilies()
	testConvertDef := createTestConvertDef(false)

	// Register and initialize Prometheus metrics
	prometheusmetrics.PrometheusRegister()
	metrics := prometheusmetrics.GetSupportedPrometheusCNIMetricsMapping()
	for _, metric := range metrics {
		if gauge, ok := metric.(prometheus.Gauge); ok {
			gauge.Set(0)
		}
	}

	err := producePrometheusMetrics(cniMetric, families, testConvertDef)
	assert.NoError(t, err)

	// Verify metrics
	testCases := []struct {
		metricName string
		expected   float64
	}{
		{"awscni_eni_max", 10.0},
		{"awscni_ip_max", 20.0},
		{"awscni_eni_allocated", 3.0},
		{"awscni_total_ip_addresses", 30.0},
		{"awscni_assigned_ip_addresses", 15.0},
	}

	metrics = prometheusmetrics.GetSupportedPrometheusCNIMetricsMapping()
	for _, tc := range testCases {
		gauge, ok := metrics[tc.metricName].(prometheus.Gauge)
		assert.True(t, ok, fmt.Sprintf("Metric %s should be registered as a Gauge", tc.metricName))

		var metric dto.Metric
		err = gauge.Write(&metric)
		assert.NoError(t, err)
		assert.Equal(t, tc.expected, *metric.Gauge.Value,
			fmt.Sprintf("Metric %s value should be set to %f", tc.metricName, tc.expected))
	}
}
