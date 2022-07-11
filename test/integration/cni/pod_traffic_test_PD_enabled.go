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
	"github.com/aws/amazon-vpc-cni-k8s/test/framework"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/manifest"
	k8sUtils "github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	"github.com/aws/amazon-vpc-cni-k8s/test/integration/common"

	. "github.com/onsi/ginkgo/v2"
)

var f *framework.Framework

var _ = Describe("Test pod networking with prefix delegation enabled", func() {
	var (
		serverDeploymentBuilder *manifest.DeploymentBuilder
		// Value for the Environment variable ENABLE_PREFIX_DELEGATION
		enableIPv4PrefixDelegation string
	)

	JustBeforeEach(func() {
		By("creating deployment")
		serverDeploymentBuilder = manifest.NewDefaultDeploymentBuilder().
			Name("traffic-server").
			NodeSelector(f.Options.NgNameLabelKey, f.Options.NgNameLabelVal)

		By("Set PD")
		k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName,
			utils.AwsNodeNamespace, utils.AwsNodeName,
			map[string]string{"ENABLE_PREFIX_DELEGATION": enableIPv4PrefixDelegation})
	})

	JustAfterEach(func() {
		k8sUtils.AddEnvVarToDaemonSetAndWaitTillUpdated(f, utils.AwsNodeName,
			utils.AwsNodeNamespace, utils.AwsNodeName,
			map[string]string{"ENABLE_PREFIX_DELEGATION": "false"})
	})

	Context("when testing TCP traffic between client and server pods", func() {
		BeforeEach(func() {
			enableIPv4PrefixDelegation = "true"
		})

		//TODO : Add pod IP validation if IP belongs to prefix or SIP
		//TODO : remove hardcoding from client/server count
		It("should have 99+% success rate", func() {
			common.ValidateTraffic(f, serverDeploymentBuilder, 99, "tcp")
		})
	})

	Context("when testing UDP traffic between client and server pods", func() {
		BeforeEach(func() {
			enableIPv4PrefixDelegation = "true"
		})

		//TODO : Add pod IP validation if IP belongs to prefix or SIP
		//TODO : remove hardcoding from client/server count
		It("should have 99+% success rate", func() {
			common.ValidateTraffic(f, serverDeploymentBuilder, 99, "udp")
		})
	})
})
