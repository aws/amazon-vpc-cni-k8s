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

// Package metrics handles the processing of all metrics. This file handles metrics for ipamd
package metrics

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/k8sapi"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/publisher"
)

// Port where prometheus metrics are published.
const metricsPort = 61678

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
	"awscni_force_removed_enis": {
		actions: []metricsAction{
			{cwMetricName: "forceRemoveENI",
				matchFunc:  matchAny,
				actionFunc: metricsAdd,
				data:       &dataPoints{},
				logToFile:  true}}},
	"awscni_force_removed_ips": {
		actions: []metricsAction{
			{cwMetricName: "forceRemoveIPs",
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
	cwMetricsPublisher  publisher.Publisher
	kubeClient          clientset.Interface
	discoveryController *k8sapi.Controller
	submitCW            bool
}

// CNIMetricsNew creates a new metricsTarget
func CNIMetricsNew(c clientset.Interface, cw publisher.Publisher, d *k8sapi.Controller, submitCW bool) *CNIMetricsTarget {
	return &CNIMetricsTarget{
		interestingMetrics:  InterestingCNIMetrics,
		cwMetricsPublisher:  cw,
		kubeClient:          c,
		discoveryController: d,
		submitCW:            submitCW,
	}
}

func (t *CNIMetricsTarget) grabMetricsFromTarget(cniPod string) ([]byte, error) {
	output, err := getMetricsFromPod(t.kubeClient, cniPod, metav1.NamespaceSystem, metricsPort)
	if err != nil {
		log.Errorf("grabMetricsFromTarget: Failed to grab CNI endpoint: %v", err)
		return nil, err
	}

	log.Infof("cni-metrics text output: %s", string(output))
	return output, nil
}

func (t *CNIMetricsTarget) getInterestingMetrics() map[string]metricsConvert {
	return InterestingCNIMetrics
}

func (t *CNIMetricsTarget) getCWMetricsPublisher() publisher.Publisher {
	return t.cwMetricsPublisher
}

func (t *CNIMetricsTarget) getTargetList() []string {
	pods := t.discoveryController.GetCNIPods()
	return pods
}

func (t *CNIMetricsTarget) submitCloudWatch() bool {
	return t.submitCW
}
