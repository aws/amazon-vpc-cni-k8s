package metrics

import (
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
	//cniMetric := CNIMetricsNew(m.clientset, m.mockPublisher, m.discoverController, false, log)
	cniMetric := CNIMetricsNew(m.clientset, m.mockPublisher, false, testLog, m.podWatcher)
	assert.NotNil(t, cniMetric)
	assert.NotNil(t, cniMetric.getCWMetricsPublisher())
	assert.NotEmpty(t, cniMetric.getInterestingMetrics())
	assert.Equal(t, testLog, cniMetric.getLogger())
	assert.False(t, cniMetric.submitCloudWatch())
}
