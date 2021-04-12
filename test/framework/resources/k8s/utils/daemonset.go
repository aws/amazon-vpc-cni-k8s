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

package utils

import (
	"fmt"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/apps/v1"
)

func AddEnvVarToDaemonSetAndWaitTillUpdated(f *framework.Framework, dsName string, dsNamespace string,
	containerName string, envVars map[string]string) {

	ds := getDaemonSet(f, dsName, dsNamespace)
	updatedDs := ds.DeepCopy()

	By(fmt.Sprintf("setting the environment variables on the ds to %+v", envVars))
	err := AddOrUpdateEnvironmentVariable(updatedDs.Spec.Template.Spec.Containers,
		containerName, envVars)
	Expect(err).ToNot(HaveOccurred())

	waitTillDaemonSetUpdated(f, ds, updatedDs)
}

func RemoveVarFromDaemonSetAndWaitTillUpdated(f *framework.Framework, dsName string, dsNamespace string,
	containerName string, envVars map[string]struct{}) {

	ds := getDaemonSet(f, dsName, dsNamespace)
	updatedDs := ds.DeepCopy()

	By(fmt.Sprintf("setting the environment variables on the ds to %+v", envVars))
	err := RemoveEnvironmentVariables(updatedDs.Spec.Template.Spec.Containers,
		containerName, envVars)
	Expect(err).ToNot(HaveOccurred())

	waitTillDaemonSetUpdated(f, ds, updatedDs)
}

func getDaemonSet(f *framework.Framework, dsName string, dsNamespace string) *v1.DaemonSet {
	By(fmt.Sprintf("getting the %s daemon set in namesapce %s", dsName, dsNamespace))
	ds, err := f.K8sResourceManagers.
		DaemonSetManager().
		GetDaemonSet(dsNamespace, dsName)
	Expect(err).ToNot(HaveOccurred())
	return ds
}

func waitTillDaemonSetUpdated(f *framework.Framework, oldDs *v1.DaemonSet, updatedDs *v1.DaemonSet) *v1.DaemonSet {
	By("updating the daemon set with new environment variable")
	updatedDs, err := f.K8sResourceManagers.
		DaemonSetManager().
		UpdateAndWaitTillDaemonSetReady(oldDs, updatedDs)
	Expect(err).ToNot(HaveOccurred())
	return updatedDs
}
