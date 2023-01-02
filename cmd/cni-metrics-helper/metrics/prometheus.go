package metrics

import (
	"net/http"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/log"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	awsAPILatency = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name: "awscni_aws_api_latency_ms",
			Help: "AWS API call latency in ms",
		},
		[]string{"api", "error", "status"},
	)
	awsAPIErr = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "awscni_aws_api_error_count",
			Help: "The number of times AWS API returns an error",
		},
		[]string{"api", "error"},
	)
	awsUtilsErr = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "awscni_aws_utils_error_count",
			Help: "The number of errors not handled in awsutils library",
		},
		[]string{"fn", "error"},
	)
	ec2ApiReq = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "awscni_ec2api_req_count",
			Help: "The number of requests made to EC2 APIs by CNI",
		},
		[]string{"fn"},
	)
	ec2ApiErr = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "awscni_ec2api_error_count",
			Help: "The number of failed EC2 APIs requests",
		},
		[]string{"fn"},
	)
	ipamdErr = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "awscni_ipamd_error_count",
			Help: "The number of errors encountered in ipamd",
		},
		[]string{"fn"},
	)
	ipamdActionsInprogress = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "awscni_ipamd_action_inprogress",
			Help: "The number of ipamd actions in progress",
		},
		[]string{"fn"},
	)
	enisMax = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "awscni_eni_max",
			Help: "The maximum number of ENIs that can be attached to the instance, accounting for unmanaged ENIs",
		},
	)
	ipMax = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "awscni_ip_max",
			Help: "The maximum number of IP addresses that can be allocated to the instance",
		},
	)
	reconcileCnt = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "awscni_reconcile_count",
			Help: "The number of times ipamd reconciles on ENIs and IP/Prefix addresses",
		},
		[]string{"fn"},
	)
	addIPCnt = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "awscni_add_ip_req_count",
			Help: "The number of add IP address requests",
		},
		[]string{"fn"},
	)
	delIPCnt = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "awscni_del_ip_req_count",
			Help: "The number of delete IP address requests",
		},
		[]string{"reason"},
	)
	podENIErr = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "awscni_pod_eni_error_count",
			Help: "The number of errors encountered for pod ENIs",
		},
		[]string{"fn"},
	)

	enis = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "awscni_eni_allocated",
			Help: "The number of ENIs allocated",
		},
	)
	totalIPs = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "awscni_total_ip_addresses",
			Help: "The total number of IP addresses",
		},
	)
	assignedIPs = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "awscni_assigned_ip_addresses",
			Help: "The number of IP addresses assigned to pods",
		},
	)
	forceRemovedENIs = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "awscni_force_removed_enis",
			Help: "The number of ENIs force removed while they had assigned pods",
		},
		[]string{"fn"},
	)
	forceRemovedIPs = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "awscni_force_removed_ips",
			Help: "The number of IPs force removed while they had assigned pods",
		},
		[]string{"fn"},
	)
	totalPrefixes = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "awscni_total_ipv4_prefixes",
			Help: "The total number of IPv4 prefixes",
		},
	)
	ipsPerCidr = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "awscni_assigned_ip_per_cidr",
			Help: "The total number of IP addresses assigned per cidr",
		},
		[]string{"cidr"},
	)
)

func StartPrometheusMetricsServer(){
	log.Info("Starting prometehus metrics server for cni-metrics-helper")
	http.ListenAndServe("localhost:2112",nil)
}

func init(){
	reg := prometheus.NewRegistry()
	promHandler := promhttp.HandlerFor(reg,promhttp.HandlerOpts{})

	reg.MustRegister(awsAPILatency)
	reg.MustRegister(awsAPIErr)
	reg.MustRegister(awsUtilsErr)
	reg.MustRegister(ec2ApiReq)
	reg.MustRegister(ec2ApiErr)
	reg.MustRegister(ipamdErr)
	reg.MustRegister(ipamdActionsInprogress)
	reg.MustRegister(enisMax)
	reg.MustRegister(ipMax)
	reg.MustRegister(reconcileCnt)
	reg.MustRegister(addIPCnt)
	reg.MustRegister(delIPCnt)
	reg.MustRegister(podENIErr)
	reg.MustRegister(enis)
	reg.MustRegister(totalIPs)
	reg.MustRegister(assignedIPs)
	reg.MustRegister(forceRemovedENIs)
	reg.MustRegister(forceRemovedIPs)
	reg.MustRegister(totalPrefixes)
	reg.MustRegister(ipsPerCidr)

	http.Handle("/metrics",promHandler)
}
