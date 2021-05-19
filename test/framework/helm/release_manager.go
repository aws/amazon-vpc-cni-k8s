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

package helm

import (
	"fmt"

	"github.com/prometheus/common/log"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/release"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

type ReleaseManager interface {
	InstallUnPackagedRelease(chart string, releaseName string, namespace string,
		values map[string]interface{}) (*release.Release, error)
	UninstallRelease(namespace string, releaseName string) (*release.UninstallReleaseResponse, error)
}

type defaultReleaseManager struct {
	kubeConfig string
}

func NewDefaultReleaseManager(kubeConfig string) ReleaseManager {
	return &defaultReleaseManager{kubeConfig: kubeConfig}
}

func (d *defaultReleaseManager) InstallUnPackagedRelease(chart string, releaseName string, namespace string,
	values map[string]interface{}) (*release.Release, error) {
	actionConfig := d.obtainActionConfig(namespace)

	installAction := action.NewInstall(actionConfig)
	installAction.Namespace = namespace
	installAction.Wait = true
	installAction.ReleaseName = releaseName

	cp, err := installAction.ChartPathOptions.LocateChart(chart, cli.New())
	if err != nil {
		return nil, err
	}

	chartRequested, err := loader.Load(cp)
	if err != nil {
		return nil, err
	}

	return installAction.Run(chartRequested, values)
}

func (d *defaultReleaseManager) UninstallRelease(namespace string, releaseName string) (*release.UninstallReleaseResponse, error) {
	actionConfig := d.obtainActionConfig(namespace)

	uninstallAction := action.NewUninstall(actionConfig)
	return uninstallAction.Run(releaseName)
}

func (d *defaultReleaseManager) obtainActionConfig(namespace string) *action.Configuration {
	cfgFlag := genericclioptions.NewConfigFlags(false)
	cfgFlag.KubeConfig = &d.kubeConfig
	cfgFlag.Namespace = &namespace
	actionConfig := new(action.Configuration)
	actionConfig.Init(cfgFlag, namespace, "secrets", func(format string, v ...interface{}) {
		message := fmt.Sprintf(format, v...)
		log.Info(message)
	})
	return actionConfig
}
