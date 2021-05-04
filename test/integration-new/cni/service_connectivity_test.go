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

package cni

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	appsV1 "k8s.io/api/apps/v1"
	batchV1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
)

const (
	serviceLabelSelectorKey = "role"
	serviceLabelSelectorVal = "service-test"
)

// Verifies connectivity to deployment behind different service types
var _ = Describe("test service connectivity", func() {
	var err error

	// Deployment running the http server
	var deployment *appsV1.Deployment
	var deploymentContainer v1.Container

	// Service front ending the http server deployment
	var service *v1.Service
	var serviceType v1.ServiceType
	var serviceAnnotation map[string]string

	// Test job that verifies connectivity to the http server
	// by querying the service URL
	var testerJob *batchV1.Job
	var testerContainer v1.Container

	// Test job that verifies test fails when connecting to
	// a non reachable port/address
	var negativeTesterJob *batchV1.Job
	var negativeTesterContainer v1.Container

	JustBeforeEach(func() {
		deploymentContainer = manifest.NewBusyBoxContainerBuilder().
			Image("python").
			Command([]string{"python3"}).
			Args([]string{"-m", "http.server", "80"}).Build()

		deployment = manifest.NewDefaultDeploymentBuilder().
			Name("http-server").
			Container(deploymentContainer).
			Replicas(20).
			PodLabel(serviceLabelSelectorKey, serviceLabelSelectorVal).
			Build()

		By("creating and waiting for deployment to be ready")
		deployment, err = f.K8sResourceManagers.DeploymentManager().
			CreateAndWaitTillDeploymentIsReady(deployment, utils.DefaultDeploymentReadyTimeout)
		Expect(err).ToNot(HaveOccurred())

		service = manifest.NewHTTPService().
			ServiceType(serviceType).
			Name("test-service").
			Selector(serviceLabelSelectorKey, serviceLabelSelectorVal).
			Annotations(serviceAnnotation).
			Build()

		By(fmt.Sprintf("creating the service of type %s", serviceType))
		service, err = f.K8sResourceManagers.ServiceManager().
			CreateService(context.Background(), service)
		Expect(err).ToNot(HaveOccurred())

		fmt.Fprintf(GinkgoWriter, "created service\n: %+v\n", service.Status)

		By("sleeping for some time to allow service to become ready")
		time.Sleep(utils.PollIntervalLong)

		testerContainer = manifest.NewBusyBoxContainerBuilder().
			Command([]string{"wget"}).
			Args([]string{"--spider", "-T", "1", fmt.Sprintf("%s:%d", service.Spec.ClusterIP,
				service.Spec.Ports[0].Port)}).
			Build()

		testerJob = manifest.NewDefaultJobBuilder().
			Parallelism(20).
			Container(testerContainer).
			Build()

		By("creating jobs to verify service connectivity")
		_, err = f.K8sResourceManagers.JobManager().
			CreateAndWaitTillJobCompleted(testerJob)
		Expect(err).ToNot(HaveOccurred())

		// Test connection to an unreachable port should fail
		negativeTesterContainer = manifest.NewBusyBoxContainerBuilder().
			Command([]string{"wget"}).
			Args([]string{"--spider", "-T", "1", fmt.Sprintf("%s:%d", service.Spec.ClusterIP, 2273)}).
			Build()

		negativeTesterJob = manifest.NewDefaultJobBuilder().
			Name("negative-test-job").
			Parallelism(2).
			Container(negativeTesterContainer).
			Build()

		By("creating negative jobs to verify service connectivity fails for unreachable port")
		_, err = f.K8sResourceManagers.JobManager().
			CreateAndWaitTillJobCompleted(negativeTesterJob)
		Expect(err).To(HaveOccurred())
	})

	JustAfterEach(func() {
		err := f.K8sResourceManagers.JobManager().DeleteAndWaitTillJobIsDeleted(testerJob)
		Expect(err).ToNot(HaveOccurred())

		err = f.K8sResourceManagers.JobManager().DeleteAndWaitTillJobIsDeleted(negativeTesterJob)
		Expect(err).ToNot(HaveOccurred())

		err = f.K8sResourceManagers.ServiceManager().DeleteService(context.Background(), service)
		Expect(err).ToNot(HaveOccurred())

		err = f.K8sResourceManagers.DeploymentManager().DeleteAndWaitTillDeploymentIsDeleted(deployment)
		Expect(err).ToNot(HaveOccurred())
	})

	Context("when a deployment behind clb service is created", func() {
		BeforeEach(func() {
			serviceType = v1.ServiceTypeLoadBalancer
		})

		It("clb service pod should be reachable", func() {})
	})

	Context("when a deployment behind nlb service is created", func() {
		BeforeEach(func() {
			serviceType = v1.ServiceTypeLoadBalancer
			serviceAnnotation = map[string]string{"service.beta.kubernetes.io/" +
				"aws-load-balancer-type": "nlb"}
		})

		It("nlb service pod should be reachable", func() {})
	})

	Context("when a deployment behind cluster IP is created", func() {
		BeforeEach(func() {
			serviceType = v1.ServiceTypeClusterIP
		})

		It("clusterIP service pod should be reachable", func() {})
	})

	Context("when a deployment behind node port is created", func() {
		BeforeEach(func() {
			serviceType = v1.ServiceTypeNodePort
		})

		It("node port service pod should be reachable", func() {})
	})

	//TODO: Add test case to install lb controller and test with nlb-ip mode
})
