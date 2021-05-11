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
	"fmt"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudwatch"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("test cni-metrics-helper publishes metrics", func() {

	JustBeforeEach(func() {
		By("creating test namespace")
		f.K8sResourceManagers.NamespaceManager().
			CreateNamespace(utils.DefaultTestNamespace)
	})

	JustAfterEach(func() {
		By("deleting test namespace")
		f.K8sResourceManagers.NamespaceManager().
			DeleteAndWaitTillNamespaceDeleted(utils.DefaultTestNamespace)
	})

	Context("when a metric is updated", func() {
		It("the updated metric is published to CW", func() {

			By("waiting for the metrics helper to publish initial metrics")
			time.Sleep(time.Minute * 3)

			// Create a new deployment to verify addReqCount is updated
			deployment := manifest.NewBusyBoxDeploymentBuilder().
				Replicas(10).
				NodeName(nodeName).
				Build()

			By("creating parking pods on the targeted node group")
			deployment, err = f.K8sResourceManagers.DeploymentManager().
				CreateAndWaitTillDeploymentIsReady(deployment, utils.DefaultDeploymentReadyTimeout)
			Expect(err).ToNot(HaveOccurred())

			By("waiting for the metrics helper to publish new metrics")
			time.Sleep(time.Minute * 3)

			getMetricStatisticsInput := &cloudwatch.GetMetricStatisticsInput{
				Dimensions: []*cloudwatch.Dimension{
					{
						Name:  aws.String("CLUSTER_ID"),
						Value: aws.String(ngName),
					},
				},
				MetricName: aws.String("addReqCount"),
				Namespace:  aws.String("Kubernetes"),
				Period:     aws.Int64(int64(60)),
				// Start time should sync with when when this test started
				StartTime:  aws.Time(time.Now().Add(time.Duration(-10) * time.Minute)),
				EndTime:    aws.Time(time.Now()),
				Statistics: aws.StringSlice([]string{"Maximum"}),
			}
			getMetricOutput, err := f.CloudServices.CloudWatch().GetMetricStatistics(getMetricStatisticsInput)
			Expect(err).ToNot(HaveOccurred())

			dataPoints := getMetricOutput.Datapoints
			fmt.Fprintf(GinkgoWriter, "data points: %+v", dataPoints)

			By("validating at least 2 metrics are published to CloudWatch")
			Expect(len(dataPoints)).Should(BeNumerically(">=", 2))

			// Verify that the addReqCount counts increased to account for the new deployment creation
			addReqCountIncreased := false
			var lastVal = *dataPoints[0].Maximum
			for _, dp := range dataPoints {
				if *dp.Maximum-lastVal > 0 {
					addReqCountIncreased = true
				}
				lastVal = *dp.Maximum
			}

			By("validating the addReqCount increased on the node after a deployment is created")
			Expect(addReqCountIncreased).To(BeTrue())
		})
	})
})
