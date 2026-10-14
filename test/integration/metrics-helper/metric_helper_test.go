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
	"fmt"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cloudwatchtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/apps/v1"
)

// metricPublishPeriod matches cni-metrics-helper's publish interval
// (METRIC_UPDATE_INTERVAL, default 30s, published every 2x that). CloudWatch
// also keeps sub-minute resolution for only 3 hours, so a 60s period lets a
// finished run be replayed later for diagnosis.
const metricPublishPeriod = 60

// sumAddReqCount returns the total number of CNI ADD requests cni-metrics-helper
// published in [from, to), and how many datapoints the window held.
//
// cni-metrics-helper publishes addReqCount as a per-interval delta rather than a
// running total: it scrapes ipamd every METRIC_UPDATE_INTERVAL seconds and
// publishes the deltas it collected. The samples cover disjoint intervals, so
// summing them reconstructs the ADD request count over the window regardless of
// how the requests were distributed in time.
func sumAddReqCount(from time.Time, to time.Time) (float64, int, error) {
	out, err := f.CloudServices.CloudWatch().GetMetricStatistics(context.TODO(),
		&cloudwatch.GetMetricStatisticsInput{
			Dimensions: []cloudwatchtypes.Dimension{
				{
					Name:  aws.String("CLUSTER_ID"),
					Value: aws.String(ngName),
				},
			},
			MetricName: aws.String("addReqCount"),
			Namespace:  aws.String("Kubernetes"),
			Period:     aws.Int32(int32(metricPublishPeriod)),
			StartTime:  aws.Time(from),
			EndTime:    aws.Time(to),
			Statistics: []cloudwatchtypes.Statistic{cloudwatchtypes.StatisticSum},
		})
	if err != nil {
		return 0, 0, err
	}

	var total float64
	for _, dp := range out.Datapoints {
		if dp.Sum != nil {
			total += *dp.Sum
		}
	}
	return total, len(out.Datapoints), nil
}

var _ = Describe("test cni-metrics-helper publishes metrics", func() {

	Context("when a metric is updated", func() {
		It("the updated metric is published to CW", func() {
			replicas := nodePodCapacity / 2

			// Create a new deployment to verify addReqCount is updated. Half the
			// node's allocatable pod capacity always fits alongside existing pods.
			var deployment *v1.Deployment
			deployment = manifest.NewBusyBoxDeploymentBuilder(f.Options.TestImageRegistry).
				Replicas(replicas).
				NodeName(nodeName).
				Build()

			By("creating parking pods on the targeted node group")
			// Recorded before the create call: CreateAndWaitTillDeploymentIsReady
			// returns only once every pod is Ready, by which point every ADD
			// request has already been served.
			createdAt := time.Now()
			deployment, err = f.K8sResourceManagers.DeploymentManager().
				CreateAndWaitTillDeploymentIsReady(deployment, utils.DefaultDeploymentReadyTimeout)
			Expect(err).ToNot(HaveOccurred())

			// Each pod costs exactly one CNI ADD request, so the published deltas
			// must account for the whole deployment. Unrelated pod churn and pod
			// retries can only add to the sum, never subtract from it, so the
			// bound is one-sided. The window starts at the publish period
			// containing the create call so no ADD request falls outside it.
			//
			// Polled rather than read once after a fixed sleep: the deltas land one
			// publish interval after the pods get their IPs, and a single read
			// either happens to be late enough or reports a short count and fails.
			By("verifying cni-metrics-helper published one addReqCount per pod")
			Eventually(func(g Gomega) {
				total, count, err := sumAddReqCount(
					createdAt.Truncate(metricPublishPeriod*time.Second), time.Now())
				g.Expect(err).ToNot(HaveOccurred())
				_, _ = fmt.Fprintf(GinkgoWriter,
					"addReqCount total=%v over %d datapoint(s), expected >= %d\n",
					total, count, replicas)

				g.Expect(count).ToNot(BeZero(), "cni-metrics-helper published no datapoints")
				g.Expect(total).To(BeNumerically(">=", float64(replicas)),
					"cni-metrics-helper accounted for %v of %d ADD requests", total, replicas)
			}).WithTimeout(8 * time.Minute).WithPolling(30 * time.Second).Should(Succeed())
		})
	})
})
