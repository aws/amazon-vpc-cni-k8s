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

	"github.com/aws/amazon-vpc-cni-k8s/utils/eksproblem"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The unit tests in utils/eksproblem prove the Collector renders correctly in isolation.
// This one proves the wiring: that PrometheusRegister actually registered it, so the
// series really does appear in the body the node monitoring agent scrapes. Without this,
// a missing MustRegister line would leave every unit test green and ship nothing.
func TestEksProblemIsServedOnTheMetricsEndpoint(t *testing.T) {
	// SetupMetricsServer builds the same mux ServeMetrics runs; exercise it through
	// httptest rather than binding a port, so the test cannot collide with a running
	// ipamd on this host.
	srv := SetupMetricsServer(61678)
	require.NotNil(t, srv.Handler)

	body := func() string {
		rec := httptest.NewRecorder()
		srv.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
		require.Equal(t, http.StatusOK, rec.Code)
		return rec.Body.String()
	}

	// Nothing recorded yet, so no problem series.
	assert.NotContains(t, body(), "eks_problem{")

	// A pod ADD failed for want of an address. holdDown is 0 here so the series is
	// publishable at once; the hold-down itself is covered in the eksproblem unit tests.
	eksproblem.Subnets.RecordPodIPAllocationFailure([]string{"subnet-0abcc2fe41543d2ea"}, 0)

	out := body()
	assert.Contains(t, out, `eks_problem{reason="InsufficientFreeAddresses",resource_subnet_id="subnet-0abcc2fe41543d2ea",type="PodIPAllocationFailed"} 1`)
	assert.Contains(t, out, "# TYPE eks_problem gauge")

	// awscni_total_ip_addresses is the gauge the telemetry pipeline uses as ipamd's
	// liveness signal. Assert it still appears, so this change cannot silently break it.
	assert.Contains(t, out, "awscni_total_ip_addresses")

	// Clearing removes the series from the endpoint rather than zeroing it.
	eksproblem.Subnets.ClearPodIPAllocationFailure()
	assert.NotContains(t, body(), "eks_problem{")
}

// The handler must not bind or listen as a side effect of being built, and the address it
// is configured with must be the documented ipamd metrics port.
func TestMetricsServerAddrIsUnchanged(t *testing.T) {
	srv := SetupMetricsServer(61678)
	assert.True(t, strings.HasSuffix(srv.Addr, ":61678"),
		"this change must not alter the metrics listen address; got %q", srv.Addr)
}

func init() {
	// PrometheusRegister panics if called twice in one process, and other tests in this
	// package may not need it. Register once, here, for this file's tests.
	registerOnceForTest()
}

var registeredForTest bool

func registerOnceForTest() {
	if registeredForTest {
		return
	}
	registeredForTest = true
	PrometheusRegister()
}
