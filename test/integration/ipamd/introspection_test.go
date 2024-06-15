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

// TODO: In future, we should also have verification for individual introspection API
var _ = Describe("test Environment Variables for IPAMD Introspection ", func() {
	// default port on which IPAMD listen for introspect API
	defaultIntrospectionAddr := "127.0.0.1:61679"
	// Container used to curl IPAMD to verify introspection URL is available
	var curlContainer corev1.Container
	// Job whose output determines if the introspect URL is available or not
	var curlJob *v1.Job

	JustBeforeEach(func() {

		// Initially the host networking job pod should succeed
		curlContainer = manifest.NewCurlContainer(f.Options.TestImageRegistry).
			Command([]string{"curl"}).
			Args([]string{"--fail", defaultIntrospectionAddr}).
			Build()

		curlJob = manifest.NewDefaultJobBuilder().
			Container(curlContainer).
			Name("verify-introspection").
			Parallelism(1).
			HostNetwork(true).
			Build()

		// Set the ENV variable
		By("enabling introspection on the aws-node")
		k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace,
			utils.AwsNodeName, map[string]string{"DISABLE_INTROSPECTION": "false"})

		By("deploying a host networking pod to verify introspection is working on default addr")
		_, err = f.K8sResourceManagers.JobManager().
			CreateAndWaitTillJobCompleted(curlJob)
		Expect(err).ToNot(HaveOccurred())

		By("deleting the verification job")
		err = f.K8sResourceManagers.JobManager().
			DeleteAndWaitTillJobIsDeleted(curlJob)
		Expect(err).ToNot(HaveOccurred())
	})

	Context("when disabling introspection by setting DISABLE_INTROSPECTION to true", func() {
		It("introspection should not work anymore", func() {

			By("disabling introspection on the aws-node")
			k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName,
				utils.AwsNodeNamespace, utils.AwsNodeName,
				map[string]string{"DISABLE_INTROSPECTION": "true"})

			curlJob = manifest.NewDefaultJobBuilder().
				Container(curlContainer).
				Name("verify-introspection-fails-on-disabling").
				Parallelism(1).
				HostNetwork(true).
				Build()

			// It should fail this time
			By("creating a new job that should error out")
			curlJob, err = f.K8sResourceManagers.JobManager().
				CreateAndWaitTillJobCompleted(curlJob)
			Expect(err).To(HaveOccurred())

			err = f.K8sResourceManagers.JobManager().
				DeleteAndWaitTillJobIsDeleted(curlJob)
			Expect(err).ToNot(HaveOccurred())

			// Set the ENV variable
			By("removing the DISABLE_INTROSPECTION on the aws-node")
			k8sUtils.RemoveVarFromDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace,
				utils.AwsNodeName, map[string]struct{}{"DISABLE_INTROSPECTION": {}})
		})
	})

	Context("when changing introspection bind address", func() {
		It("the introspection API be available on new address", func() {
			newAddr := "127.0.0.1:61671"

			By("updating introspection bind address by setting INTROSPECTION_BIND_ADDRESS")
			k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName, utils.AwsNodeNamespace,
				utils.AwsNodeName, map[string]string{"INTROSPECTION_BIND_ADDRESS": newAddr})

			curlContainerUpdatedAddr := curlContainer.DeepCopy()
			curlContainerUpdatedAddr.Args = []string{"--fail", newAddr}

			curlJob = manifest.NewDefaultJobBuilder().
				Container(*curlContainerUpdatedAddr).
				Name("verify-introspection-new-add").
				Parallelism(1).
				HostNetwork(true).
				Build()

			// It should fail this time
			By("creating a new job that should not error out")
			curlJob, err = f.K8sResourceManagers.JobManager().
				CreateAndWaitTillJobCompleted(curlJob)
			Expect(err).ToNot(HaveOccurred())

			err = f.K8sResourceManagers.JobManager().
				DeleteAndWaitTillJobIsDeleted(curlJob)
			Expect(err).ToNot(HaveOccurred())

			// Set the ENV variable
			By("removing the INTROSPECTION_BIND_ADDRESS on the aws-node")
			k8sUtils.RemoveVarFromDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName,
				utils.AwsNodeNamespace, utils.AwsNodeName,
				map[string]struct{}{"INTROSPECTION_BIND_ADDRESS": {}})
		})
	})
})
