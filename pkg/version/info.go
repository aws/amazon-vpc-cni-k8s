package version

import (
	"runtime"

	"github.com/prometheus/client_golang/prometheus"
)

// Build information. Populated at build-time.
var (
	Version   string
	Revision  string
	GoVersion = runtime.Version()
)

func RegisterMetric() {
	buildInfo := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "awscni_build_info",
			Help: "A metric with a constant '1' value labeled by version, revision, and goversion from which amazon-vpc-cni-k8s was built.",
		},
		[]string{"version", "revision", "goversion"},
	)
	buildInfo.WithLabelValues(Version, Revision, GoVersion).Set(1)
	prometheus.MustRegister(buildInfo)
}
