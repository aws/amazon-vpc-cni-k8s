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
	"testing"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var f *framework.Framework

func TestMultusSetup(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Multus Setup Suite")
}

var _ = BeforeSuite(func() {
	f = framework.New(framework.GlobalOptions)

	By("Check if Multus Daemonset is Ready")
	_, err := f.K8sResourceManagers.DaemonSetManager().GetDaemonSet(utils.AwsNodeNamespace, utils.MultusNodeName)
	Expect(err).NotTo(HaveOccurred())

	err = f.K8sResourceManagers.DaemonSetManager().CheckIfDaemonSetIsReady(utils.AwsNodeNamespace, utils.MultusNodeName)
	Expect(err).NotTo(HaveOccurred())

	By(fmt.Sprintf("getting the node with the node label key %s and value %s if specified, else get all nodes",
		f.Options.NgNameLabelKey, f.Options.NgNameLabelVal))
	nodes, err := f.K8sResourceManagers.NodeManager().GetNodes(f.Options.NgNameLabelKey, f.Options.NgNameLabelVal)
	Expect(err).ToNot(HaveOccurred())

	By("verifying that atleast 1 node is present for the test")
	Expect(len(nodes.Items)).Should(BeNumerically(">", 0))
})
