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

package controller

import (
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/helm"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
)

type InstallationManager interface {
	InstallCNIMetricsHelper(image string, tag string, clusterId string) error
	UnInstallCNIMetricsHelper() error
	InstallTigeraOperator(version string) error
	UninstallTigeraOperator() error
}

func NewDefaultInstallationManager(manager helm.ReleaseManager) InstallationManager {
	return &defaultInstallationManager{releaseManager: manager}
}

type defaultInstallationManager struct {
	releaseManager helm.ReleaseManager
}

func (d *defaultInstallationManager) InstallCNIMetricsHelper(image string, tag string, clusterId string) error {
	values := map[string]interface{}{
		"env": map[string]interface{}{
			"AWS_CLUSTER_ID": clusterId,
		},
		"image": map[string]interface{}{
			"repository": image,
			"tag":        tag,
		},
	}

	projectRoot := utils.GetProjectRoot()
	_, err := d.releaseManager.InstallUnPackagedRelease(projectRoot+CNIMetricsHelperChartDir,
		CNIMetricsHelperReleaseName, CNIMetricHelperNamespace, values)
	return err
}

func (d *defaultInstallationManager) UnInstallCNIMetricsHelper() error {
	_, err := d.releaseManager.UninstallRelease(CNIMetricHelperNamespace, CNIMetricsHelperReleaseName)
	return err
}

func (d *defaultInstallationManager) InstallTigeraOperator(version string) error {
	_, err := d.releaseManager.InstallPackagedRelease(TigeraOperatorHelmCharts, TigeraOperatorReleaseName, version, TigeraOperatorNamespace, map[string]interface{}{})
	return err
}

func (d *defaultInstallationManager) UninstallTigeraOperator() error {
	_, err := d.releaseManager.UninstallRelease(TigeraOperatorNamespace, TigeraOperatorReleaseName)
	return err
}
