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

package version

import (
	"runtime"

	"github.com/prometheus/client_golang/prometheus"
)

// Build information. Populated at build-time.
var (
	Version   string
	GoVersion = runtime.Version()
)

func RegisterMetric() {
	buildInfo := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "awscni_build_info",
			Help: "A metric with a constant '1' value labeled by version, revision, and goversion from which amazon-vpc-cni-k8s was built.",
		},
		[]string{"version", "goversion"},
	)
	buildInfo.WithLabelValues(Version, GoVersion).Set(1)
	prometheus.MustRegister(buildInfo)
}
