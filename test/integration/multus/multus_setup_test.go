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

package multus

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("test Multus Deployment", func() {
	Context("validate Multus Deployment", func() {
		It("multus pod logs shouldnt have any errors", func() {
			By("Check Multus pod logs running on all worker nodes")
			multusPods, err := f.K8sResourceManagers.PodManager().GetPodsWithLabelSelector("name", "multus")
			Expect(err).NotTo(HaveOccurred())

			for _, pod := range multusPods.Items {
				podStr := fmt.Sprintf("Validating logs for pod: %v in Namespace: %v", pod.Name, pod.Namespace)
				By(podStr)
				logs, err := f.K8sResourceManagers.PodManager().PodLogs(pod.Namespace, pod.Name)
				Expect(err).NotTo(HaveOccurred())
				fmt.Fprintln(GinkgoWriter, logs)
			}
		})
	})
})
