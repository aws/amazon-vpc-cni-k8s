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

// postProcessingCounter collapses the cumulative counter values collected from
// every scrape target this poll (curTargetValues) into a single cluster-wide
// per-poll delta (curSingleDataPoint). It diffs each target against its own
// previous value (lastTargetValues) so the result is independent of scrape
// order and well defined when targets restart, disappear, or appear:
//
//   - target seen last poll and now, value not decreased -> add (cur - last)
//   - target whose value decreased -> ipamd restarted; add cur, the post-reset
//     total, rather than a negative delta
//   - target seen for the first time -> add nothing; its value becomes the
//     baseline for the next poll
//   - target still live but not scraped this poll -> carry its baseline forward
//     unchanged (a transient scrape failure is not a node leaving)
//   - target no longer live -> drop it; a node that left the cluster is removed,
//     not treated as a decrease
//
// liveTargets is the set of targets that currently exist in the cluster. The
// function returns true only when there is no prior baseline at all while there
// is current data (the first poll, or the first poll after the helper
// restarted); the caller then skips publishing that single poll instead of
// emitting cumulative totals as if they were a one-interval delta.
func postProcessingCounter(convert metricsConvert, liveTargets map[string]bool) bool {
	firstPoll := false
	for i := range convert.actions {
		data := convert.actions[i].data
		if data == nil {
			continue
		}

		hasCurrent := len(data.curTargetValues) > 0
		noBaseline := len(data.lastTargetValues) == 0
		if hasCurrent && noBaseline {
			firstPoll = true
		}

		var delta, rawTotal float64
		for target, cur := range data.curTargetValues {
			rawTotal += cur
			last, seen := data.lastTargetValues[target]
			switch {
			case !seen:
				// newly discovered target: this poll only establishes a baseline
			case cur >= last:
				delta += cur - last
			default:
				// counter went backwards: target restarted; count the post-reset total
				delta += cur
			}
		}

		if noBaseline {
			// Nothing to diff against yet. Surface the raw cluster total for
			// visibility; the returned firstPoll tells the caller to skip
			// publishing this poll.
			data.curSingleDataPoint = rawTotal
		} else {
			data.curSingleDataPoint = delta
		}

		// Build the next poll's baseline. Reuse the current map (it already holds
		// every scraped target) and add any still-live target that failed to
		// scrape this poll, carrying its baseline forward. Targets that are no
		// longer live are simply not carried, so a departed node drops out.
		next := data.curTargetValues
		if next == nil {
			next = make(map[string]float64)
		}
		for target, last := range data.lastTargetValues {
			if _, scraped := next[target]; !scraped && liveTargets[target] {
				next[target] = last
			}
		}
		data.lastTargetValues = next
		data.curTargetValues = nil
	}
	return firstPoll
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

func filterMetrics(originalMetrics map[string]*dto.MetricFamily,
	interestingMetrics map[string]metricsConvert,
) (map[string]*dto.MetricFamily, error) {
	result := map[string]*dto.MetricFamily{}

	for metric := range interestingMetrics {
		if family, found := originalMetrics[metric]; found {
			result[metric] = family
		}
	}
	return result, nil
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
				// Clear this poll's per-target scratch for counters. Doing it
				// here (rather than only at the end of postProcessingCounter)
				// keeps aggregation correct even when a poll aborts early on a
				// parse error: the next poll starts from a clean slate while the
				// per-target baseline in lastTargetValues is preserved.
				act.data.curTargetValues = nil
			}

			if act.bucket != nil {
				act.bucket.curBucket = make([]*bucketPoint, 0)
			}
		}
	}
}

func metricsListGrabAggregateConvert(ctx context.Context, t metricsTarget) (map[string]*dto.MetricFamily, map[string]metricsConvert, bool, error) {
	var resetDetected = false
	var families map[string]*dto.MetricFamily

	interestingMetrics := t.getInterestingMetrics()
	resetMetrics(interestingMetrics)

	targetList, _ := t.getTargetList(ctx)
	t.getLogger().Debugf("Total TargetList pod count: %d", len(targetList))

	// Set of targets that currently exist in the cluster. Used by
	// postProcessingCounter so a target that is live but fails to scrape this
	// poll keeps its counter baseline instead of being mistaken for a departed
	// node.
	liveTargets := make(map[string]bool, len(targetList))
	for _, target := range targetList {
		liveTargets[target] = true
	}

	// Metric names observed as Prometheus counters this poll. Their cluster-wide
	// per-poll delta is computed once, after the target loop, so it does not
	// depend on scrape order (issue #3844).
	counterMetrics := make(map[string]struct{})

	for _, target := range targetList {
		rawOutput, err := t.grabMetricsFromTarget(ctx, target)
		if err != nil {
			// it may take times to remove some metric targets
			continue
		}

		parser := expfmt.NewTextParser(model.UTF8Validation)
		origFamilies, err := parser.TextToMetricFamilies(bytes.NewReader(rawOutput))

		if err != nil {
			return nil, nil, true, err
		}

		families, err = filterMetrics(origFamilies, interestingMetrics)
		if err != nil {
			return nil, nil, true, err
		}

		for _, family := range families {
			if family.GetType() == dto.MetricType_COUNTER {
				counterMetrics[family.GetName()] = struct{}{}
			}
			convert := interestingMetrics[family.GetName()]
			curReset, err := processMetric(family, convert, target, t.getLogger())
			if err != nil {
				return nil, nil, true, err
			}
			if curReset {
				resetDetected = true
			}
		}
	}

	// Aggregate every counter across the whole cluster into a single per-poll
	// delta now that all targets have been scraped. Doing this per target (the
	// previous behaviour) made the published value depend on scrape order and
	// could not distinguish a node leaving from an ipamd restart -- issue #3844.
	for name := range counterMetrics {
		if postProcessingCounter(interestingMetrics[name], liveTargets) {
			resetDetected = true
		}
	}

	return families, interestingMetrics, resetDetected, nil
}

// Handler grabs metrics from target, aggregates the metrics and convert them into cloudwatch metrics
func Handler(ctx context.Context, t metricsTarget) {
	families, interestingMetrics, resetDetected, err := metricsListGrabAggregateConvert(ctx, t)

	if err != nil || resetDetected {
		t.getLogger().Infof("Skipping 1st poll after reset, error: %v", err)
	}

	if t.submitCloudWatch() {
		cw := t.getCWMetricsPublisher()
		produceCloudWatchMetrics(t, families, interestingMetrics, cw)
	}

	if t.submitPrometheus() {
		producePrometheusMetrics(t, families, interestingMetrics)
	}
}
