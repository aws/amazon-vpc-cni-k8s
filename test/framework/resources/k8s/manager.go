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

package k8s

import (
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s/resources"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ResourceManagers interface {
	JobManager() resources.JobManager
	DeploymentManager() resources.DeploymentManager
	CustomResourceManager() resources.CustomResourceManager
	NamespaceManager() resources.NamespaceManager
	ServiceManager() resources.ServiceManager
	NodeManager() resources.NodeManager
	PodManager() resources.PodManager
	DaemonSetManager() resources.DaemonSetManager
	ConfigMapManager() resources.ConfigMapManager
}

type defaultManager struct {
	jobManager            resources.JobManager
	deploymentManager     resources.DeploymentManager
	customResourceManager resources.CustomResourceManager
	namespaceManager      resources.NamespaceManager
	serviceManager        resources.ServiceManager
	nodeManager           resources.NodeManager
	podManager            resources.PodManager
	daemonSetManager      resources.DaemonSetManager
	configMapManager      resources.ConfigMapManager
}

func NewResourceManager(k8sClient client.Client,
	scheme *runtime.Scheme, config *rest.Config) ResourceManagers {
	return &defaultManager{
		jobManager:            resources.NewDefaultJobManager(k8sClient),
		deploymentManager:     resources.NewDefaultDeploymentManager(k8sClient),
		customResourceManager: resources.NewCustomResourceManager(k8sClient),
		namespaceManager:      resources.NewDefaultNamespaceManager(k8sClient),
		serviceManager:        resources.NewDefaultServiceManager(k8sClient),
		nodeManager:           resources.NewDefaultNodeManager(k8sClient),
		podManager:            resources.NewDefaultPodManager(k8sClient, scheme, config),
		daemonSetManager:      resources.NewDefaultDaemonSetManager(k8sClient),
		configMapManager:      resources.NewConfigMapManager(k8sClient),
	}
}

func (m *defaultManager) JobManager() resources.JobManager {
	return m.jobManager
}

func (m *defaultManager) DeploymentManager() resources.DeploymentManager {
	return m.deploymentManager
}

func (m *defaultManager) CustomResourceManager() resources.CustomResourceManager {
	return m.customResourceManager
}

func (m *defaultManager) NamespaceManager() resources.NamespaceManager {
	return m.namespaceManager
}

func (m *defaultManager) ServiceManager() resources.ServiceManager {
	return m.serviceManager
}

func (m *defaultManager) NodeManager() resources.NodeManager {
	return m.nodeManager
}

func (m *defaultManager) PodManager() resources.PodManager {
	return m.podManager
}

func (m *defaultManager) DaemonSetManager() resources.DaemonSetManager {
	return m.daemonSetManager
}

func (m *defaultManager) ConfigMapManager() resources.ConfigMapManager {
	return m.configMapManager
}
