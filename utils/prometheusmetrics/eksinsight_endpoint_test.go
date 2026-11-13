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
	// Built and registered exactly as ipamd does, so this proves the object the endpoint
	// serves is the object a caller records into.
	clk := &fakeClock{t: time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)}
	reg := eksinsight.NewRegistry(eksinsight.DefaultMaxSubnets, clk.now)
	RegisterEksInsight(reg)
	t.Cleanup(reg.Reset)

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

	// A subnet must keep failing to be published, so record repeatedly with gaps inside the
	// freshness window, which is half the hold-down. The wound clock makes this instant.
	const holdDown = time.Minute
	for elapsed := time.Duration(0); elapsed <= holdDown; elapsed += 20 * time.Second {
		reg.Record([]string{"subnet-0abcc2fe41543d2ea"}, holdDown)
		clk.advance(20 * time.Second)
	}

	out := body()
	assert.Contains(t, out, `eks_insight{reason="InsufficientSubnetAddressCapacity",resource_subnet_id="subnet-0abcc2fe41543d2ea",type="PodIPAllocationFailed"} 1`)
	assert.Contains(t, out, "# TYPE eks_insight gauge")

	// The drop gauges share the collector's registration, so pin them too: without this a
	// deleted MustRegister line leaves every other assertion green.
	assert.Contains(t, out, "awscni_eks_insight_dropped")

	// awscni_total_ip_addresses is a long-standing gauge on this endpoint. Assert it still
	// appears, so this change cannot silently break it.
	assert.Contains(t, out, "awscni_total_ip_addresses")

	// Clearing removes the series rather than zeroing it.
	reg.Reset()
	assert.NotContains(t, body(), "eks_insight{")
}

// The address the handler is configured with must stay the documented ipamd metrics port.
func TestMetricsServerAddrIsUnchanged(t *testing.T) {
	srv := SetupMetricsServer(61678)
	assert.True(t, strings.HasSuffix(srv.Addr, ":61678"),
		"this change must not alter the metrics listen address; got %q", srv.Addr)
}

// fakeClock lets the test serve a 60s hold-down without waiting for one.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func init() {
	// PrometheusRegister panics if called twice, so a second caller appearing here should
	// fail loudly rather than be skipped.
	PrometheusRegister()
}
