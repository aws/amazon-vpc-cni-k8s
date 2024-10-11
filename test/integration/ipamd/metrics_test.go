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

package ipamd

import (
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
)

// Verifies toggling the DISABLE_METRICS works as expected
// TODO: In future, we should also have verification for individual metrics
var _ = Describe("test IPAMD metric environment variable", func() {
	// container to curl metric API
	var curlContainer corev1.Container
	// Job's output determines if the API is reachable or not
	var curlJob *v1.Job

	Context("when metrics is disabled", func() {
		metricAddr := "127.0.0.1:61678/metrics"
		It("should not be accessible anymore", func() {
			curlContainer = manifest.NewCurlContainer(f.Options.TestImageRegistry).
				Command([]string{"curl"}).
				Args([]string{"--fail", metricAddr}).
				Build()

			By("enabling metrics on aws-node")
			k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace,
				utils.AwsNodeName, map[string]string{"DISABLE_METRICS": "false"})

			curlJob = manifest.NewDefaultJobBuilder().
				Container(curlContainer).
				Name("verify-metrics-works").
				Parallelism(1).
				HostNetwork(true).
				Build()

			By("verify metric is working before disabling it")
			curlJob, err = f.K8sResourceManagers.JobManager().
				CreateAndWaitTillJobCompleted(curlJob)
			Expect(err).ToNot(HaveOccurred())

			By("deleting job used for verification")
			err = f.K8sResourceManagers.JobManager().
				DeleteAndWaitTillJobIsDeleted(curlJob)
			Expect(err).ToNot(HaveOccurred())

			By("disabling metrics on aws-node")
			k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace,
				utils.AwsNodeName, map[string]string{"DISABLE_METRICS": "true"})

			curlJob = manifest.NewDefaultJobBuilder().
				Container(curlContainer).
				Name("verify-metrics-doesnt-works").
				Parallelism(1).
				HostNetwork(true).
				Build()

			By("verifying metrics is not working after disabling it")
			curlJob, err = f.K8sResourceManagers.JobManager().
				CreateAndWaitTillJobCompleted(curlJob)
			Expect(err).To(HaveOccurred())

			By("deleting job used for verification")
			err = f.K8sResourceManagers.JobManager().
				DeleteAndWaitTillJobIsDeleted(curlJob)
			Expect(err).ToNot(HaveOccurred())

			By("reverting to default value for disabling metrics")
			k8sUtils.RemoveVarFromDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace,
				utils.AwsNodeName, map[string]struct{}{"DISABLE_METRICS": {}})
		})
	})
})
