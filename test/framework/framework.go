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

package framework

import (
	eniConfig "github.com/aws/amazon-vpc-cni-k8s/pkg/apis/crd/v1alpha1"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/controller"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/helm"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/aws"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s"
	sgp "github.com/aws/amazon-vpc-resource-controller-k8s/apis/vpcresources/v1beta1"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Framework struct {
	Options             Options
	K8sClient           client.Client
	CloudServices       aws.Cloud
	K8sResourceManagers k8s.ResourceManagers
	InstallationManager controller.InstallationManager
}

func New(options Options) *Framework {
	err := options.Validate()
	Expect(err).ToNot(HaveOccurred())

	config, err := clientcmd.BuildConfigFromFlags("", options.KubeConfig)
	Expect(err).ToNot(HaveOccurred())

	config.QPS = 20
	config.Burst = 30

	k8sSchema := runtime.NewScheme()
	clientgoscheme.AddToScheme(k8sSchema)
	eniConfig.AddToScheme(k8sSchema)
	sgp.AddToScheme(k8sSchema)

	stopChan := ctrl.SetupSignalHandler()
	cache, err := cache.New(config, cache.Options{Scheme: k8sSchema})
	Expect(err).NotTo(HaveOccurred())

	go func() {
		cache.Start(stopChan)
	}()
	cache.WaitForCacheSync(stopChan)
	realClient, err := client.New(config, client.Options{Scheme: k8sSchema})
	Expect(err).NotTo(HaveOccurred())

	k8sClient, err := client.NewDelegatingClient(client.NewDelegatingClientInput{
		CacheReader: cache,
		Client:      realClient,
	})
	Expect(err).NotTo(HaveOccurred())

	cloudConfig := aws.CloudConfig{Region: options.AWSRegion, VpcID: options.AWSVPCID,
		EKSEndpoint: options.EKSEndpoint}

	return &Framework{
		Options:             options,
		K8sClient:           k8sClient,
		CloudServices:       aws.NewCloud(cloudConfig),
		K8sResourceManagers: k8s.NewResourceManager(k8sClient, k8sSchema, config),
		InstallationManager: controller.NewDefaultInstallationManager(
			helm.NewDefaultReleaseManager(options.KubeConfig)),
	}
}
