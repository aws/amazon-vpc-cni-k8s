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
	"fmt"

	"github.com/aws/aws-sdk-go/aws"

	"github.com/aws/aws-sdk-go/service/cloudwatch"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	clientset "k8s.io/client-go/kubernetes"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/publisher"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
)

var log = logger.DefaultLogger()

type metricMatcher func(metric *dto.Metric) bool
type actionFuncType func(aggregatedValue *float64, sampleValue float64)

type metricsTarget interface {
	grabMetricsFromTarget(target string) ([]byte, error)
	getInterestingMetrics() map[string]metricsConvert
	getCWMetricsPublisher() publisher.Publisher
	getTargetList() []string
	submitCloudWatch() bool
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
	lastSingleDataPoint float64
	curSingleDataPoint  float64
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

func getMetricsFromPod(client clientset.Interface, podName string, namespace string, port int) ([]byte, error) {
	rawOutput, err := client.CoreV1().RESTClient().Get().
		Namespace(namespace).
		Resource("pods").
		SubResource("proxy").
		Name(fmt.Sprintf("%v:%v", podName, port)).
		Suffix("metrics").
		Do().Raw()
	if err != nil {
		return nil, err
	}
	return rawOutput, nil
}

func processGauge(metric *dto.Metric, act *metricsAction) {
	act.actionFunc(&act.data.curSingleDataPoint, metric.GetGauge().GetValue())
}

func processCounter(metric *dto.Metric, act *metricsAction) {
	act.actionFunc(&act.data.curSingleDataPoint, metric.GetCounter().GetValue())
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

func processHistogram(metric *dto.Metric, act *metricsAction) {
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
			log.Infof("Created a new bucket with upperBound:%f", bucket.GetUpperBound())
		}
	}
}

func postProcessingCounter(convert metricsConvert) bool {
	resetDetected := false
	noPreviousDataPoint := true
	noCurrentDataPoint := true
	for _, action := range convert.actions {
		currentTotal := action.data.curSingleDataPoint
		// Only do delta if metric target did NOT restart
		if action.data.curSingleDataPoint < action.data.lastSingleDataPoint {
			resetDetected = true
		} else {
			action.data.curSingleDataPoint -= action.data.lastSingleDataPoint
		}

		if action.data.lastSingleDataPoint != 0 {
			noPreviousDataPoint = false
		}

		if action.data.curSingleDataPoint != 0 {
			noCurrentDataPoint = false
		}

		action.data.lastSingleDataPoint = currentTotal
	}

	if resetDetected || (noPreviousDataPoint && !noCurrentDataPoint) {
		log.Infof("Reset detected resetDetected: %v, noPreviousDataPoint: %v, noCurrentDataPoint: %v",
			resetDetected, noPreviousDataPoint, noCurrentDataPoint)
	}
	return resetDetected || (noPreviousDataPoint && !noCurrentDataPoint)
}

func postProcessingHistogram(convert metricsConvert) bool {
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

func processMetric(family *dto.MetricFamily, convert metricsConvert) (bool, error) {
	resetDetected := false

	mType := family.GetType()
	for _, metric := range family.GetMetric() {
		for _, act := range convert.actions {
			if act.matchFunc(metric) {
				switch mType {
				case dto.MetricType_GAUGE:
					processGauge(metric, &act)
				case dto.MetricType_HISTOGRAM:
					processHistogram(metric, &act)
				case dto.MetricType_COUNTER:
					processCounter(metric, &act)
				case dto.MetricType_SUMMARY:
					processPercentile(metric, &act)
				}
			}
		}
	}

	switch mType {
	case dto.MetricType_COUNTER:
		curResetDetected := postProcessingCounter(convert)
		if curResetDetected {
			resetDetected = true
		}
	case dto.MetricType_GAUGE:
	// no addition work needs for GAUGE
	case dto.MetricType_SUMMARY:
		// no addition work needs for PERCENTILE
	case dto.MetricType_HISTOGRAM:
		curResetDetected := postProcessingHistogram(convert)
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
			dataPoint := &cloudwatch.MetricDatum{
				MetricName: aws.String(act.cwMetricName),
				StatisticValues: &cloudwatch.StatisticSet{
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
	interestingMetrics map[string]metricsConvert) (map[string]*dto.MetricFamily, error) {
	result := map[string]*dto.MetricFamily{}

	for metric := range interestingMetrics {
		if family, found := originalMetrics[metric]; found {
			result[metric] = family

		}
	}
	return result, nil
}

func produceCloudWatchMetrics(t metricsTarget, families map[string]*dto.MetricFamily, convertDef map[string]metricsConvert, cw publisher.Publisher) {
	for key, family := range families {
		convertMetrics := convertDef[key]
		mType := family.GetType()
		for _, action := range convertMetrics.actions {
			switch mType {
			case dto.MetricType_COUNTER:
				if t.submitCloudWatch() {
					dataPoint := &cloudwatch.MetricDatum{
						MetricName: aws.String(action.cwMetricName),
						Unit:       aws.String(cloudwatch.StandardUnitCount),
						Value:      aws.Float64(action.data.curSingleDataPoint),
					}
					cw.Publish(dataPoint)
				}
			case dto.MetricType_GAUGE:
				if t.submitCloudWatch() {
					dataPoint := &cloudwatch.MetricDatum{
						MetricName: aws.String(action.cwMetricName),
						Unit:       aws.String(cloudwatch.StandardUnitCount),
						Value:      aws.Float64(action.data.curSingleDataPoint),
					}
					cw.Publish(dataPoint)
				}
			case dto.MetricType_SUMMARY:
				if t.submitCloudWatch() {
					dataPoint := &cloudwatch.MetricDatum{
						MetricName: aws.String(action.cwMetricName),
						Unit:       aws.String(cloudwatch.StandardUnitCount),
						Value:      aws.Float64(action.data.curSingleDataPoint),
					}
					cw.Publish(dataPoint)
				}
			case dto.MetricType_HISTOGRAM:
				if t.submitCloudWatch() {
					produceHistogram(action, cw)
				}
			}
		}
	}
}

func resetMetrics(interestingMetrics map[string]metricsConvert) {
	for _, convert := range interestingMetrics {
		for _, act := range convert.actions {
			if act.data != nil {
				act.data.curSingleDataPoint = 0
			}

			if act.bucket != nil {
				act.bucket.curBucket = make([]*bucketPoint, 0)
			}
		}
	}
}

func metricsListGrabAggregateConvert(t metricsTarget) (map[string]*dto.MetricFamily, map[string]metricsConvert, bool, error) {
	var resetDetected = false
	var families map[string]*dto.MetricFamily

	interestingMetrics := t.getInterestingMetrics()
	resetMetrics(interestingMetrics)

	targetList := t.getTargetList()
	for _, target := range targetList {
		rawOutput, err := t.grabMetricsFromTarget(target)
		if err != nil {
			// it may take times to remove some metric targets
			continue
		}

		parser := &expfmt.TextParser{}
		origFamilies, err := parser.TextToMetricFamilies(bytes.NewReader(rawOutput))

		if err != nil {
			return nil, nil, true, err
		}

		families, err = filterMetrics(origFamilies, interestingMetrics)
		if err != nil {
			return nil, nil, true, err
		}

		for _, family := range families {
			convert := interestingMetrics[family.GetName()]
			curReset, err := processMetric(family, convert)
			if err != nil {
				return nil, nil, true, err
			}
			if curReset {
				resetDetected = true
			}
		}
	}

	// TODO resetDetected is NOT right for cniMetrics, so force it for now
	if len(targetList) > 1 {
		resetDetected = false
	}

	return families, interestingMetrics, resetDetected, nil
}

// Handler grabs metrics from target, aggregates the metrics and convert them into cloudwatch metrics
func Handler(t metricsTarget) {
	families, interestingMetrics, resetDetected, err := metricsListGrabAggregateConvert(t)

	if err != nil || resetDetected {
		log.Infof("Skipping 1st poll after reset, error: %v", err)
	}

	cw := t.getCWMetricsPublisher()
	produceCloudWatchMetrics(t, families, interestingMetrics, cw)
}
