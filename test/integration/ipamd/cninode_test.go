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
	"github.com/aws/amazon-vpc-cni-k8s/pkg/config"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("CNINode Validation", func() {
	Describe("Validate CNINode contains cluster name tag", func() {
		Context("when nodes are ready", func() {
			It("should have the cluster name tag populated", func() {
				By("getting CNINode for the primary node and verify cluster name tag exists")
				cniNode, err := f.K8sResourceManagers.CNINodeManager().GetCNINode(primaryNode.Name)
				Expect(err).ToNot(HaveOccurred())
				val, ok := cniNode.Spec.Tags[config.ClusterNameTagKey]
				Expect(ok).To(BeTrue())
				Expect(val).To(Equal(f.Options.ClusterName))
			})
		})
	})
})
