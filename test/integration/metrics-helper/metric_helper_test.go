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

package metrics_helper

import (
	"context"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cloudwatchtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// assignedIPsMetric tracks IP addresses assigned to pods (one per pod), summed across the cluster's
// aws-node pods, so it rises ~1:1 with pods scheduled. We assert on it rather than addReqCount,
// which cni-metrics-helper publishes as a per-poll delta that stayed pinned near 1 while the
// deployment ran -- the original flake. The cluster-wide sum is safe here because the suite owns a
// single-tenant ephemeral cluster, so the increase is attributable to its own deployment.
const assignedIPsMetric = "assignIPAddresses"

var _ = Describe("test cni-metrics-helper publishes metrics", func() {

	Context("when a metric is updated", func() {
		It("the updated metric is published to CW", func() {
			const lookback = 10 * time.Minute

			// Baseline then poll for the increase: cni-metrics-helper publishes on a delay
			// (collect/flush cadence + CloudWatch lag), so a fixed sleep + single read races it.
			By("recording the baseline assigned-IP count published to CloudWatch")
			var baseline float64
			Eventually(func() float64 {
				baseline = publishedMetricMax(assignedIPsMetric, lookback)
				return baseline
			}, 5*time.Minute, 20*time.Second).Should(BeNumerically(">=", 0),
				"cni-metrics-helper should publish %s for CLUSTER_ID=%s before the test drives load", assignedIPsMetric, ngName)

			By("creating parking pods on the targeted node to drive new IP assignments")
			replicas := nodePodCapacity / 2 // leave room for existing pods on the node
			deployment := manifest.NewBusyBoxDeploymentBuilder(f.Options.TestImageRegistry).
				Replicas(replicas).
				NodeName(nodeName).
				Build()
			deployment, err = f.K8sResourceManagers.DeploymentManager().
				CreateAndWaitTillDeploymentIsReady(deployment, utils.DefaultDeploymentReadyTimeout)
			Expect(err).ToNot(HaveOccurred())

			By("waiting for cni-metrics-helper to publish the increased assigned-IP count")
			Eventually(func() float64 {
				return publishedMetricMax(assignedIPsMetric, lookback)
			}, 8*time.Minute, 30*time.Second).Should(BeNumerically(">", baseline),
				"%s should increase after scheduling %d pods on node %s (baseline=%.0f)", assignedIPsMetric, replicas, nodeName, baseline)
		})
	})
})

// publishedMetricMax returns the max value cni-metrics-helper has published to CloudWatch for the
// metric and this cluster over the lookback window, or -1 if none yet (or on error) so callers can
// poll for it to appear and then increase.
func publishedMetricMax(metricName string, lookback time.Duration) float64 {
	output, statErr := f.CloudServices.CloudWatch().GetMetricStatistics(context.TODO(), &cloudwatch.GetMetricStatisticsInput{
		Dimensions: []cloudwatchtypes.Dimension{
			{Name: aws.String("CLUSTER_ID"), Value: aws.String(ngName)},
		},
		MetricName: aws.String(metricName),
		Namespace:  aws.String("Kubernetes"),
		Period:     aws.Int32(30),
		StartTime:  aws.Time(time.Now().Add(-lookback)),
		EndTime:    aws.Time(time.Now()),
		Statistics: []cloudwatchtypes.Statistic{cloudwatchtypes.StatisticMaximum},
	})
	if statErr != nil {
		return -1
	}
	maxVal := -1.0 // counts are never negative, so -1 means "no datapoint yet"
	for _, dp := range output.Datapoints {
		if dp.Maximum != nil && *dp.Maximum > maxVal {
			maxVal = *dp.Maximum
		}
	}
	return maxVal
}
