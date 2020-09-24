package metrics

import (
	"testing"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/k8sapi"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/publisher/mock_publisher"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

var logConfig = logger.Configuration{
	LogLevel:    "Debug",
	LogLocation: "stdout",
}

var log = logger.New(&logConfig)

type testMocks struct {
	ctrl               *gomock.Controller
	clientset          *k8sfake.Clientset
	discoverController *k8sapi.Controller
	mockPublisher      *mock_publisher.MockPublisher
}

func setup(t *testing.T) *testMocks {
	ctrl := gomock.NewController(t)
	fakeClientset := k8sfake.NewSimpleClientset()
	return &testMocks{
		ctrl:               ctrl,
		clientset:          fakeClientset,
		discoverController: k8sapi.NewController(fakeClientset),
		mockPublisher:      mock_publisher.NewMockPublisher(ctrl),
	}
}

func TestCNIMetricsNew(t *testing.T) {
	m := setup(t)
	_, _ = m.clientset.CoreV1().Pods("kube-system").Create(&v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "aws-node-1"}})
	cniMetric := CNIMetricsNew(m.clientset, m.mockPublisher, m.discoverController, false, log)
	assert.NotNil(t, cniMetric)
	assert.NotNil(t, cniMetric.getCWMetricsPublisher())
	assert.NotEmpty(t, cniMetric.getInterestingMetrics())
	assert.Equal(t, log, cniMetric.getLogger())
	assert.False(t, cniMetric.submitCloudWatch())
}
