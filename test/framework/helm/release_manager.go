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
	"log"
	"time"

	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart/loader"
	"helm.sh/helm/v4/pkg/cli"
	"helm.sh/helm/v4/pkg/getter"
	"helm.sh/helm/v4/pkg/helmpath"
	"helm.sh/helm/v4/pkg/kube"
	"helm.sh/helm/v4/pkg/release"
	repo "helm.sh/helm/v4/pkg/repo/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

type ReleaseManager interface {
	InstallUnPackagedRelease(chart string, releaseName string, namespace string,
		values map[string]interface{}) (release.Releaser, error)
	UninstallRelease(namespace string, releaseName string) (*release.UninstallReleaseResponse, error)
	InstallPackagedRelease(chart string, releaseName string, version string, namespace string,
		values map[string]interface{}) (release.Releaser, error)
}

type defaultReleaseManager struct {
	kubeConfig string
}

func NewDefaultReleaseManager(kubeConfig string) ReleaseManager {
	return &defaultReleaseManager{kubeConfig: kubeConfig}
}

func (d *defaultReleaseManager) InstallUnPackagedRelease(chart string, releaseName string, namespace string,
	values map[string]interface{}) (release.Releaser, error) {
	actionConfig := d.obtainActionConfig(namespace)

	installAction := action.NewInstall(actionConfig)
	installAction.Namespace = namespace
	installAction.WaitStrategy = kube.StatusWatcherStrategy
	installAction.ReleaseName = releaseName
	installAction.Timeout = time.Minute

	return installCharts(installAction, chart, values)
}

func (d *defaultReleaseManager) InstallPackagedRelease(chart string, releaseName string, version string, namespace string,
	values map[string]interface{}) (release.Releaser, error) {
	entry := &repo.Entry{
		Name:                  "projectcalico",
		URL:                   "https://docs.tigera.io/calico/charts",
		InsecureSkipTLSVerify: true,
	}
	setting := cli.New()
	r, err := repo.NewChartRepository(entry, getter.All(setting))
	_, err = r.DownloadIndexFile()

	file := repo.NewFile()
	file.Update(entry)

	err = file.WriteFile(helmpath.ConfigPath("repositories.yaml"), 0644)

	actionConfig := d.obtainActionConfig(namespace)
	client := action.NewInstall(actionConfig)
	client.Version = version
	client.ReleaseName = releaseName
	client.Namespace = namespace
	cp, err := client.ChartPathOptions.LocateChart(chart, setting)
	chartReq, err := loader.Load(cp)
	rel, err := client.Run(chartReq, values)
	return rel, err
}

func installCharts(installAction *action.Install, chart string, values map[string]interface{}) (release.Releaser, error) {
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
	if err := actionConfig.Init(cfgFlag, namespace, "secrets"); err != nil {
		log.Printf("failed to initialize helm action config: %v", err)
	}
	return actionConfig
}
