// Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//      http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package prometheusmetrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/utils/eksinsight"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The unit tests in utils/eksinsight cover rendering. This one covers the wiring, that
// PrometheusRegister registered the collector, so the series appears in the body served on
// the metrics endpoint.
func TestEksInsightIsServedOnTheMetricsEndpoint(t *testing.T) {
	// SetupMetricsServer builds the same mux ServeMetrics runs. Exercised through httptest
	// rather than a real port, so it cannot collide with an ipamd running on this host.
	srv := SetupMetricsServer(61678)
	require.NotNil(t, srv.Handler)

	body := func() string {
		rec := httptest.NewRecorder()
		srv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
		require.Equal(t, http.StatusOK, rec.Code)
		return rec.Body.String()
	}

	// Nothing recorded yet.
	assert.NotContains(t, body(), "eks_insight{")

	// The process-wide registry reads the real clock, so a short hold-down is served by
	// recording on a loop the way kubelet retries. The gap is well inside the freshness
	// window, half the hold-down, so a slow run cannot restart the hold-down and fail the
	// test. The hold-down itself is covered in the eksinsight unit tests.
	t.Cleanup(eksinsight.Subnets.Reset)
	const (
		holdDown = 2 * time.Second
		gap      = 150 * time.Millisecond
	)
	for elapsed := time.Duration(0); elapsed <= holdDown+2*gap; elapsed += gap {
		eksinsight.Subnets.Record([]string{"subnet-0abcc2fe41543d2ea"}, holdDown)
		time.Sleep(gap)
	}

	out := body()
	assert.Contains(t, out, `eks_insight{reason="InsufficientSubnetAddressCapacity",resource_subnet_id="subnet-0abcc2fe41543d2ea",type="PodIPAllocationFailed"} 1`)
	assert.Contains(t, out, "# TYPE eks_insight gauge")

	// awscni_total_ip_addresses is a long-standing gauge on this endpoint. Assert it still
	// appears, so this change cannot silently break it.
	assert.Contains(t, out, "awscni_total_ip_addresses")

	// Clearing removes the series rather than zeroing it.
	eksinsight.Subnets.Reset()
	assert.NotContains(t, body(), "eks_insight{")
}

// The address the handler is configured with must stay the documented ipamd metrics port.
func TestMetricsServerAddrIsUnchanged(t *testing.T) {
	srv := SetupMetricsServer(61678)
	assert.True(t, strings.HasSuffix(srv.Addr, ":61678"),
		"this change must not alter the metrics listen address; got %q", srv.Addr)
}

func init() {
	// PrometheusRegister panics if called twice, so a second caller appearing here should
	// fail loudly rather than be skipped.
	PrometheusRegister()
}
