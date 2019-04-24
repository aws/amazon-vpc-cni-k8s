// Package metrics handles the processing of all metrics. This file handles metrics for kube-state-metrics
package metrics

import (
	"github.com/golang/glog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/k8sapi"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/publisher"
)

// InterestingCNIMetrics defines metrics parsing definition for kube-state-metrics
var InterestingCNIMetrics = map[string]metricsConvert{
	"awscni_assigned_ip_addresses": {
		actions: []metricsAction{
			{cwMetricName: "assignIPAddresses",
				matchFunc:  matchAny,
				actionFunc: metricsAdd,
				data:       &dataPoints{}}}},
	"awscni_total_ip_addresses": {
		actions: []metricsAction{
			{cwMetricName: "totalIPAddresses",
				matchFunc:  matchAny,
				actionFunc: metricsAdd,
				data:       &dataPoints{}}}},
	"awscni_eni_allocated": {
		actions: []metricsAction{
			{cwMetricName: "eniAllocated",
				matchFunc:  matchAny,
				actionFunc: metricsAdd,
				data:       &dataPoints{}}}},
	"awscni_eni_max": {
		actions: []metricsAction{
			{cwMetricName: "eniMaxAvailable",
				matchFunc:  matchAny,
				actionFunc: metricsAdd,
				data:       &dataPoints{}}}},
	"awscni_ip_max": {
		actions: []metricsAction{
			{cwMetricName: "maxIPAddresses",
				matchFunc:  matchAny,
				actionFunc: metricsAdd,
				data:       &dataPoints{}}}},
	"awscni_aws_api_latency_ms": {
		actions: []metricsAction{
			{cwMetricName: "awsAPILatency",
				matchFunc:  matchAny,
				actionFunc: metricsMax,
				data:       &dataPoints{},
				logToFile:  true}}},
	"awscni_aws_api_error_count": {
		actions: []metricsAction{
			{cwMetricName: "awsAPIErr",
				matchFunc:  matchAny,
				actionFunc: metricsAdd,
				data:       &dataPoints{},
				logToFile:  true}}},
	"awscni_aws_utils_error_count": {
		actions: []metricsAction{
			{cwMetricName: "awsUtilErr",
				matchFunc:  matchAny,
				actionFunc: metricsAdd,
				data:       &dataPoints{},
				logToFile:  true}}},
	"awscni_ipamd_error_count": {
		actions: []metricsAction{
			{cwMetricName: "ipamdErr",
				matchFunc:  matchAny,
				actionFunc: metricsAdd,
				data:       &dataPoints{},
				logToFile:  true}}},
	"awscni_ipamd_action_inprogress": {
		actions: []metricsAction{
			{cwMetricName: "ipamdActionInProgress",
				matchFunc:  matchAny,
				actionFunc: metricsAdd,
				data:       &dataPoints{},
				logToFile:  true}}},
	"awscni_reconcile_count": {
		actions: []metricsAction{
			{cwMetricName: "reconcileCount",
				matchFunc:  matchAny,
				actionFunc: metricsAdd,
				data:       &dataPoints{},
				logToFile:  true}}},
	"awscni_add_ip_req_count": {
		actions: []metricsAction{
			{cwMetricName: "addReqCount",
				matchFunc:  matchAny,
				actionFunc: metricsAdd,
				data:       &dataPoints{},
				logToFile:  true}}},
	"awscni_del_ip_req_count": {
		actions: []metricsAction{
			{cwMetricName: "delReqCount",
				matchFunc:  matchAny,
				actionFunc: metricsAdd,
				data:       &dataPoints{},
				logToFile:  true}}},
}

// CNIMetricsTarget defines data structure for kube-state-metric target
type CNIMetricsTarget struct {
	interestingMetrics  map[string]metricsConvert
	cwClient            publisher.Publisher
	kubeClient          clientset.Interface
	cniPods             []string
	discoveryController *k8sapi.Controller
	submitCW            bool
}

// CNIMetricsNew creates a new metricsTarget
func CNIMetricsNew(c clientset.Interface, cw publisher.Publisher, d *k8sapi.Controller, submitCW bool) *CNIMetricsTarget {
	return &CNIMetricsTarget{
		interestingMetrics:  InterestingCNIMetrics,
		cwClient:            cw,
		kubeClient:          c,
		discoveryController: d,
		submitCW:            submitCW,
	}
}

func (t *CNIMetricsTarget) grabMetricsFromTarget(cniPod string) ([]byte, error) {

	glog.Info("Grabbing metrics from CNI ", cniPod)
	output, err := getMetricsFromPod(t.kubeClient, cniPod, metav1.NamespaceSystem, 61678)
	if err != nil {
		glog.Errorf("grabMetricsFromTarget: Failed to grab CNI endpoint: %v", err)
		return nil, err
	}

	glog.V(5).Infof("cni-metrics text output: %v", string(output))

	return output, nil
}

func (t *CNIMetricsTarget) getInterestingMetrics() map[string]metricsConvert {
	return InterestingCNIMetrics
}

func (t *CNIMetricsTarget) getCWContext() publisher.Publisher {
	return t.cwClient
}

func (t *CNIMetricsTarget) getTargetList() []string {
	pods := t.discoveryController.GetCNIPods()
	return pods
}

func (t *CNIMetricsTarget) submitCloudWatch() bool {
	return t.submitCW
}
