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

package ipamd

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/retry"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	// metricsPort is the port for prometheus metrics
	metricsPort = 61678

	// Environment variable to disable the metrics endpoint on 61678
	envDisableMetrics = "DISABLE_METRICS"
)

// ServeMetrics sets up ipamd metrics and introspection endpoints
func (c *IPAMContext) ServeMetrics() {
	if disableMetrics() {
		log.Info("Metrics endpoint disabled")
		return
	}

	log.Infof("Serving metrics on port %d", metricsPort)
	server := c.setupMetricsServer()
	for {
		once := sync.Once{}
		_ = retry.RetryWithBackoff(retry.NewSimpleBackoff(time.Second, time.Minute, 0.2, 2), func() error {
			err := server.ListenAndServe()
			once.Do(func() {
				log.Warnf("Error running http API: %v", err)
			})
			return err
		})
	}
}

func (c *IPAMContext) setupMetricsServer() *http.Server {
	serveMux := http.NewServeMux()
	serveMux.Handle("/metrics", promhttp.Handler())
	server := &http.Server{
		Addr:         ":" + strconv.Itoa(metricsPort),
		Handler:      serveMux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}
	return server
}

// disableMetrics returns true if we should disable metrics
func disableMetrics() bool {
	return getEnvBoolWithDefault(envDisableMetrics, false)
}
