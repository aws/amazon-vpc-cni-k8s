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
	"strconv"
	"sync"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/retry"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var log = logger.Get()

var (
	IpamdErr = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "awscni_ipamd_error_count",
			Help: "The number of errors encountered in ipamd",
		},
		[]string{"fn"},
	)
	IpamdActionsInprogress = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "awscni_ipamd_action_inprogress",
			Help: "The number of ipamd actions in progress",
		},
		[]string{"fn"},
	)
	EnisMax = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "awscni_eni_max",
			Help: "The maximum number of ENIs that can be attached to the instance, accounting for unmanaged ENIs",
		},
	)
	IpMax = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "awscni_ip_max",
			Help: "The maximum number of IP addresses that can be allocated to the instance",
		},
	)
	ReconcileCnt = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "awscni_reconcile_count",
			Help: "The number of times ipamd reconciles on ENIs and IP/Prefix addresses",
		},
		[]string{"fn"},
	)
	AddIPCnt = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "awscni_add_ip_req_count",
			Help: "The number of add IP address requests",
		},
	)
	DelIPCnt = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "awscni_del_ip_req_count",
			Help: "The number of delete IP address requests",
		},
		[]string{"reason"},
	)
	PodENIErr = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "awscni_pod_eni_error_count",
			Help: "The number of errors encountered for pod ENIs",
		},
		[]string{"fn"},
	)
	AwsAPILatency = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name: "awscni_aws_api_latency_ms",
			Help: "AWS API call latency in ms",
		},
		[]string{"api", "error", "status"},
	)
	AwsAPIErr = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "awscni_aws_api_error_count",
			Help: "The number of times AWS API returns an error",
		},
		[]string{"api", "error"},
	)
	AwsUtilsErr = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "awscni_aws_utils_error_count",
			Help: "The number of errors not handled in awsutils library",
		},
		[]string{"fn", "error"},
	)
	Ec2ApiReq = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "awscni_ec2api_req_count",
			Help: "The number of requests made to EC2 APIs by CNI",
		},
		[]string{"fn"},
	)
	Ec2ApiErr = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "awscni_ec2api_error_count",
			Help: "The number of failed EC2 APIs requests",
		},
		[]string{"fn"},
	)
	Enis = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "awscni_eni_allocated",
			Help: "The number of ENIs allocated",
		},
	)
	TotalIPs = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "awscni_total_ip_addresses",
			Help: "The total number of IP addresses",
		},
	)
	AssignedIPs = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "awscni_assigned_ip_addresses",
			Help: "The number of IP addresses assigned to pods",
		},
	)
	ForceRemovedENIs = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "awscni_force_removed_enis",
			Help: "The number of ENIs force removed while they had assigned pods",
		},
	)
	ForceRemovedIPs = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "awscni_force_removed_ips",
			Help: "The number of IPs force removed while they had assigned pods",
		},
	)
	TotalPrefixes = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "awscni_total_ipv4_prefixes",
			Help: "The total number of IPv4 prefixes",
		},
	)
	IpsPerCidr = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "awscni_assigned_ip_per_cidr",
			Help: "The total number of IP addresses assigned per cidr",
		},
		[]string{"cidr"},
	)
	NoAvailableIPAddrs = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "awscni_no_available_ip_addresses",
			Help: "The number of pod IP assignments that fail due to no available IP addresses",
		},
	)
	EniIPsInUse = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "awscni_assigned_ip_per_eni",
			Help: "The number of allocated ips partitioned by eni",
		},
		[]string{"eni"},
	)
)

// ServeMetrics sets up ipamd metrics and introspection endpoints
func ServeMetrics(metricsPort int) {
	log.Infof("Serving metrics on port %d", metricsPort)
	server := SetupMetricsServer(metricsPort)
	for {
		once := sync.Once{}
		_ = retry.WithBackoff(retry.NewSimpleBackoff(time.Second, time.Minute, 0.2, 2), func() error {
			err := server.ListenAndServe()
			once.Do(func() {
				log.Warnf("Error running http API: %v", err)
			})
			return err
		})
	}
}

func SetupMetricsServer(metricsPort int) *http.Server {
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

func PrometheusRegister() {
	prometheus.MustRegister(IpamdErr)
	prometheus.MustRegister(IpamdActionsInprogress)
	prometheus.MustRegister(EnisMax)
	prometheus.MustRegister(IpMax)
	prometheus.MustRegister(ReconcileCnt)
	prometheus.MustRegister(AddIPCnt)
	prometheus.MustRegister(DelIPCnt)
	prometheus.MustRegister(PodENIErr)
	prometheus.MustRegister(AwsAPILatency)
	prometheus.MustRegister(AwsAPIErr)
	prometheus.MustRegister(AwsUtilsErr)
	prometheus.MustRegister(Ec2ApiReq)
	prometheus.MustRegister(Ec2ApiErr)
	prometheus.MustRegister(Enis)
	prometheus.MustRegister(TotalIPs)
	prometheus.MustRegister(AssignedIPs)
	prometheus.MustRegister(ForceRemovedENIs)
	prometheus.MustRegister(ForceRemovedIPs)
	prometheus.MustRegister(TotalPrefixes)
	prometheus.MustRegister(IpsPerCidr)
	prometheus.MustRegister(NoAvailableIPAddrs)
	prometheus.MustRegister(EniIPsInUse)
}

// This can be enhanced to get it programatically.
// Initial CNI metrics helper enhancement includes only Gauge. Doesn't support GaugeVec, Counter, CounterVec and Summary
func GetSupportedPrometheusCNIMetricsMapping() map[string]prometheus.Collector {
	prometheusCNIMetrics := map[string]prometheus.Collector{
		"awscni_eni_max":               EnisMax,
		"awscni_ip_max":                IpMax,
		"awscni_eni_allocated":         Enis,
		"awscni_total_ip_addresses":    TotalIPs,
		"awscni_assigned_ip_addresses": AssignedIPs,
		"awscni_total_ipv4_prefixes":   TotalPrefixes,
	}
	return prometheusCNIMetrics
}
