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

// Package metrics provide common data structure and routines for converting/aggregating prometheus metrics to cloudwatch metrics
package metrics

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	cloudwatchtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	"k8s.io/client-go/kubernetes"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/publisher"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
	"github.com/aws/amazon-vpc-cni-k8s/utils/prometheusmetrics"
	"github.com/prometheus/client_golang/prometheus"
)

type metricMatcher func(metric *dto.Metric) bool
type actionFuncType func(aggregatedValue *float64, sampleValue float64)

type metricsTarget interface {
	grabMetricsFromTarget(ctx context.Context, target string) ([]byte, error)
	getInterestingMetrics() map[string]metricsConvert
	getCWMetricsPublisher() publisher.Publisher
	getTargetList(ctx context.Context) ([]string, error)
	submitCloudWatch() bool
	submitPrometheus() bool
	getLogger() logger.Logger
	finishPoll() (firstPoll bool)
}

// metricsPollState tracks startup for the helper, across all counter families.
type metricsPollState struct {
	started bool
}

func (s *metricsPollState) finishPoll() bool {
	firstPoll := !s.started
	s.started = true
	return firstPoll
}

type metricsConvert struct {
	actions []metricsAction
}

type metricsAction struct {
	cwMetricName string
	matchFunc    metricMatcher
	actionFunc   actionFuncType
	data         *dataPoints
	bucket       *bucketPoints
	logToFile    bool
}

type dataPoints struct {
	curSingleDataPoint float64

	// lastTargetValues and curTargetValues are used only for COUNTER metrics. A
	// Prometheus counter is cumulative, so the value published to CloudWatch is
	// the increase since the previous poll, summed across every node. Keeping
	// the last-seen value per scrape target (keyed by pod name) -- instead of a
	// single cluster-wide scalar -- makes that sum independent of scrape order
	// and lets us tell a node restarting (its counter goes backwards) apart from
	// a node leaving the cluster (its target disappears). See issue #3844.
	lastTargetValues map[string]float64 // previous poll's cumulative value per target
	curTargetValues  map[string]float64 // this poll's cumulative value per target
}

type bucketPoint struct {
	CumulativeCount *float64
	UpperBound      *float64
}

type bucketPoints struct {
	lastBucket []*bucketPoint
	curBucket  []*bucketPoint
}

func matchAny(metric *dto.Metric) bool {
	return true
}

func metricsAdd(aggregatedValue *float64, sampleValue float64) {
	*aggregatedValue += sampleValue
}

func metricsMax(aggregatedValue *float64, sampleValue float64) {
	if *aggregatedValue < sampleValue {
		*aggregatedValue = sampleValue
	}
}

func getMetricsFromPod(ctx context.Context, k8sClient kubernetes.Interface, podName string, namespace string, port int) ([]byte, error) {
	rawOutput, err := k8sClient.CoreV1().RESTClient().Get().
		Namespace(namespace).
		Resource("pods").
		SubResource("proxy").
		Name(fmt.Sprintf("%v:%v", podName, port)).
		Suffix("metrics").
		Do(ctx).Raw()

	if err != nil {
		return nil, err
	}
	return rawOutput, nil
}

func processGauge(metric *dto.Metric, act *metricsAction) {
	act.actionFunc(&act.data.curSingleDataPoint, metric.GetGauge().GetValue())
}

func processCounter(metric *dto.Metric, act *metricsAction, target string) {
	if act.data.curTargetValues == nil {
		act.data.curTargetValues = make(map[string]float64)
	}
	// Accumulate this target's cumulative counter value, summing the metric's
	// label series within the target. actionFunc is metricsAdd for every counter
	// in InterestingCNIMetrics, matching the per-node "Sum" contract documented
	// in cmd/cni-metrics-helper/README.md. The cross-target aggregation into a
	// per-poll delta happens later, once, in postProcessingCounter.
	v := act.data.curTargetValues[target]
	act.actionFunc(&v, metric.GetCounter().GetValue())
	act.data.curTargetValues[target] = v
}

func processPercentile(metric *dto.Metric, act *metricsAction) {
	var p99 float64

	summary := metric.GetSummary()
	quantiles := summary.GetQuantile()

	for _, q := range quantiles {
		if q.GetQuantile() == 0.99 {
			p99 = q.GetValue()
		}
	}
	act.actionFunc(&act.data.curSingleDataPoint, p99)
}

func processHistogram(metric *dto.Metric, act *metricsAction, log logger.Logger) {
	histogram := metric.GetHistogram()

	for _, bucket := range histogram.GetBucket() {
		existingBucket := false
		for _, bucketInAct := range act.bucket.curBucket {
			if bucket.GetUpperBound() == *bucketInAct.UpperBound {
				// found the matching bucket
				act.actionFunc(bucketInAct.CumulativeCount, float64(bucket.GetCumulativeCount()))
				existingBucket = true
				break
			}
		}

		if !existingBucket {
			upperBound := new(float64)
			*upperBound = float64(bucket.GetUpperBound())
			cumulativeCount := new(float64)
			*cumulativeCount = float64(bucket.GetCumulativeCount())
			newBucket := &bucketPoint{UpperBound: upperBound, CumulativeCount: cumulativeCount}
			act.bucket.curBucket = append(act.bucket.curBucket, newBucket)
			log.Infof("Created a new bucket with upperBound: %f", bucket.GetUpperBound())
		}
	}
}

// postProcessingCounter sums the increase since the last scrape of each target.
// A new target or a reset contributes its full value after the helper's first
// poll. Keep baselines for live targets even when their scrape or series is
// missing this poll.
func postProcessingCounter(data *dataPoints, liveTargets map[string]bool, firstPoll bool) {
	if data.lastTargetValues == nil {
		data.lastTargetValues = make(map[string]float64)
	}

	var delta float64
	for target, cur := range data.curTargetValues {
		last := data.lastTargetValues[target]
		if cur >= last {
			delta += cur - last
		} else {
			delta += cur
		}
		data.lastTargetValues[target] = cur
	}
	if firstPoll {
		delta = 0
	}
	if len(data.curTargetValues) > 0 {
		data.curSingleDataPoint = delta
	}

	for target := range data.lastTargetValues {
		if !liveTargets[target] {
			delete(data.lastTargetValues, target)
		}
	}
}

func postProcessingHistogram(convert metricsConvert, log logger.Logger) bool {
	resetDetected := false
	noLastBucket := true

	for _, action := range convert.actions {
		numOfBuckets := len(action.bucket.curBucket)
		if numOfBuckets == 0 {
			log.Info("Post Histogram Processing: no bucket found")
			continue
		}
		for i := 1; i < numOfBuckets; i++ {
			log.Infof("Found numOfBuckets-i:=%d, *action.bucket.curBucket[numOfBuckets-i].CumulativeCount=%f",
				numOfBuckets-i, *action.bucket.curBucket[numOfBuckets-i].CumulativeCount)

			// Delta against the previous bucket value
			// e.g. diff between bucket LE250000 and previous bucket LE125000
			*action.bucket.curBucket[numOfBuckets-i].CumulativeCount -= *action.bucket.curBucket[numOfBuckets-i-1].CumulativeCount
			log.Infof("Found numOfBuckets-i:=%d, *action.bucket.curBucket[numOfBuckets-i].CumulativeCount=%f, *action.bucket.curBucket[numOfBuckets-i-1].CumulativeCount=%f",
				numOfBuckets-i, *action.bucket.curBucket[numOfBuckets-i].CumulativeCount, *action.bucket.curBucket[numOfBuckets-i-1].CumulativeCount)

			// Delta against the previous value
			if action.bucket.lastBucket != nil {
				log.Infof("Found *action.bucket.lastBucket[numOfBuckets-i].CumulativeCount=%f",
					*action.bucket.lastBucket[numOfBuckets-i].CumulativeCount)
				currentTotal := *action.bucket.curBucket[numOfBuckets-i].CumulativeCount
				// Only do delta if there is no restart for metric target
				if *action.bucket.curBucket[numOfBuckets-i].CumulativeCount >= *action.bucket.lastBucket[numOfBuckets-i].CumulativeCount {
					*action.bucket.curBucket[numOfBuckets-i].CumulativeCount -= *action.bucket.lastBucket[numOfBuckets-i].CumulativeCount
					log.Infof("Found *action.bucket.lastBucket[numOfBuckets-i].CumulativeCount=%f, *action.bucket.lastBucket[numOfBuckets-i].CumulativeCount=%f",
						*action.bucket.curBucket[numOfBuckets-i].CumulativeCount, *action.bucket.lastBucket[numOfBuckets-i].CumulativeCount)
				} else {
					resetDetected = true
				}
				*action.bucket.lastBucket[numOfBuckets-i].CumulativeCount = currentTotal
			}
		}

		if action.bucket.lastBucket != nil {
			currentTotal := *action.bucket.curBucket[0].CumulativeCount
			// Only do delta if there is no restart for metric target
			if *action.bucket.curBucket[0].CumulativeCount >= *action.bucket.lastBucket[0].CumulativeCount {
				*action.bucket.curBucket[0].CumulativeCount -= *action.bucket.lastBucket[0].CumulativeCount
			} else {
				resetDetected = true
			}
			*action.bucket.lastBucket[0].CumulativeCount = currentTotal
		}

		if action.bucket.lastBucket == nil {
			action.bucket.lastBucket = action.bucket.curBucket
		} else {
			noLastBucket = false
		}
	}
	return resetDetected || noLastBucket
}

func processMetric(family *dto.MetricFamily, convert metricsConvert, target string, log logger.Logger) (bool, error) {
	resetDetected := false

	metricType := family.GetType()
	for _, metric := range family.GetMetric() {
		for i := range convert.actions {
			act := &convert.actions[i]
			if act.matchFunc(metric) {
				switch metricType {
				case dto.MetricType_GAUGE:
					processGauge(metric, act)
				case dto.MetricType_HISTOGRAM:
					processHistogram(metric, act, log)
				case dto.MetricType_COUNTER:
					processCounter(metric, act, target)
				case dto.MetricType_SUMMARY:
					processPercentile(metric, act)
				}
			}
		}
	}

	switch metricType {
	case dto.MetricType_COUNTER:
		// Counters are aggregated into a cluster-wide delta once, after every
		// target has been scraped (postProcessingCounter in
		// metricsListGrabAggregateConvert), so there is no per-target work here.
	case dto.MetricType_GAUGE:
	// no addition work needs for GAUGE
	case dto.MetricType_SUMMARY:
		// no addition work needs for PERCENTILE
	case dto.MetricType_HISTOGRAM:
		curResetDetected := postProcessingHistogram(convert, log)
		if curResetDetected {
			resetDetected = true
		}
	}
	return resetDetected, nil
}

func produceHistogram(act metricsAction, cw publisher.Publisher) {
	prevUpperBound := float64(0)
	for _, bucket := range act.bucket.curBucket {
		mid := (*bucket.UpperBound-float64(prevUpperBound))/2 + prevUpperBound
		if mid == *bucket.UpperBound {
			newMid := prevUpperBound + prevUpperBound/2
			mid = newMid
		}

		prevUpperBound = *bucket.UpperBound
		if *bucket.CumulativeCount != 0 {
			dataPoint := cloudwatchtypes.MetricDatum{
				MetricName: aws.String(act.cwMetricName),
				StatisticValues: &cloudwatchtypes.StatisticSet{
					Maximum:     aws.Float64(mid),
					Minimum:     aws.Float64(mid),
					SampleCount: aws.Float64(*bucket.CumulativeCount),
					Sum:         aws.Float64(mid * float64(*bucket.CumulativeCount)),
				},
			}
			cw.Publish(dataPoint)
		}
	}
}

func produceCloudWatchMetrics(t metricsTarget, families map[string]*dto.MetricFamily, convertDef map[string]metricsConvert, cw publisher.Publisher) error {
	for key, family := range families {
		convertMetrics := convertDef[key]
		metricType := family.GetType()
		for _, action := range convertMetrics.actions {
			switch metricType {
			case dto.MetricType_COUNTER:
				dataPoint := cloudwatchtypes.MetricDatum{
					MetricName: aws.String(action.cwMetricName),
					Unit:       cloudwatchtypes.StandardUnitCount,
					Value:      aws.Float64(action.data.curSingleDataPoint),
				}
				cw.Publish(dataPoint)
			case dto.MetricType_GAUGE:
				dataPoint := cloudwatchtypes.MetricDatum{
					MetricName: aws.String(action.cwMetricName),
					Unit:       cloudwatchtypes.StandardUnitCount,
					Value:      aws.Float64(action.data.curSingleDataPoint),
				}
				cw.Publish(dataPoint)
			case dto.MetricType_SUMMARY:
				dataPoint := cloudwatchtypes.MetricDatum{
					MetricName: aws.String(action.cwMetricName),
					Unit:       cloudwatchtypes.StandardUnitCount,
					Value:      aws.Float64(action.data.curSingleDataPoint),
				}
				cw.Publish(dataPoint)
			case dto.MetricType_HISTOGRAM:
				produceHistogram(action, cw)
			}
		}
	}

	return nil
}

// Prometheus export supports only gauge metrics for now.

func producePrometheusMetrics(t metricsTarget, families map[string]*dto.MetricFamily, convertDef map[string]metricsConvert) error {
	prometheusCNIMetrics := prometheusmetrics.GetSupportedPrometheusCNIMetricsMapping()
	if len(prometheusCNIMetrics) == 0 {
		errorMsg := "Skipping since prometheus mapping is missing"
		t.getLogger().Infof(errorMsg)
		return errors.New(errorMsg)
	}
	for key, family := range families {
		convertMetrics := convertDef[key]
		metricType := family.GetType()
		for _, action := range convertMetrics.actions {
			switch metricType {
			case dto.MetricType_GAUGE:
				metrics, ok := prometheusCNIMetrics[family.GetName()]
				if ok {
					if gauge, isGauge := metrics.(prometheus.Gauge); isGauge {
						gauge.Set(action.data.curSingleDataPoint)
					} else {
						t.getLogger().Warnf("Metric %s is not a Gauge type, skipping", family.GetName())
					}
				}
			}
		}
	}

	return nil
}

func resetMetrics(interestingMetrics map[string]metricsConvert) {
	for _, convert := range interestingMetrics {
		for _, act := range convert.actions {
			if act.data != nil {
				act.data.curSingleDataPoint = 0
				clear(act.data.curTargetValues)
			}

			if act.bucket != nil {
				act.bucket.curBucket = make([]*bucketPoint, 0)
			}
		}
	}
}

func metricsListGrabAggregateConvert(ctx context.Context, t metricsTarget) (map[string]*dto.MetricFamily, map[string]metricsConvert, bool, error) {
	var resetDetected bool
	families := make(map[string]*dto.MetricFamily)
	interestingMetrics := t.getInterestingMetrics()

	targetList, err := t.getTargetList(ctx)
	if err != nil {
		return nil, nil, false, err
	}
	t.getLogger().Debugf("Total TargetList pod count: %d", len(targetList))

	resetMetrics(interestingMetrics)
	liveTargets := make(map[string]bool, len(targetList))
	for _, target := range targetList {
		liveTargets[target] = true
		rawOutput, err := t.grabMetricsFromTarget(ctx, target)
		if err != nil {
			// Keep the baseline for a live target if its scrape fails.
			continue
		}

		parser := expfmt.NewTextParser(model.UTF8Validation)
		origFamilies, err := parser.TextToMetricFamilies(bytes.NewReader(rawOutput))
		if err != nil {
			return nil, nil, false, err
		}
		for name, convert := range interestingMetrics {
			family, found := origFamilies[name]
			if !found {
				continue
			}
			families[name] = family
			curReset, err := processMetric(family, convert, target, t.getLogger())
			if err != nil {
				return nil, nil, false, err
			}
			resetDetected = resetDetected || curReset
		}
	}

	firstPoll := len(families) > 0 && t.finishPoll()
	for _, convert := range interestingMetrics {
		for _, action := range convert.actions {
			data := action.data
			if data != nil && (data.curTargetValues != nil || data.lastTargetValues != nil) {
				postProcessingCounter(data, liveTargets, firstPoll)
			}
		}
	}

	return families, interestingMetrics, resetDetected, nil
}

// Handler grabs metrics from target, aggregates the metrics and convert them into cloudwatch metrics
func Handler(ctx context.Context, t metricsTarget) {
	families, interestingMetrics, resetDetected, err := metricsListGrabAggregateConvert(ctx, t)

	if err != nil {
		t.getLogger().Infof("Skipping metrics poll: %v", err)
		return
	}
	if resetDetected {
		t.getLogger().Info("Histogram baseline initialized or reset")
	}

	if t.submitCloudWatch() {
		cw := t.getCWMetricsPublisher()
		produceCloudWatchMetrics(t, families, interestingMetrics, cw)
	}

	if t.submitPrometheus() {
		producePrometheusMetrics(t, families, interestingMetrics)
	}
}
