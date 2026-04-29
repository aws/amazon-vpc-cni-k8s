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
	"context"
	"fmt"
	"log"

	eniconfigscheme "github.com/aws/amazon-vpc-cni-k8s/pkg/apis/crd/v1alpha1"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/k8sapi"
	rcscheme "github.com/aws/amazon-vpc-resource-controller-k8s/apis/vpcresources/v1alpha1"
	sgpscheme "github.com/aws/amazon-vpc-resource-controller-k8s/apis/vpcresources/v1beta1"
	"github.com/go-logr/logr"
	. "github.com/onsi/gomega"
	eventsv1 "k8s.io/api/events/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/aws/amazon-vpc-cni-k8s/test/framework/controller"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/helm"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/aws"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/resources/k8s"
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
)

type Framework struct {
	Options             Options
	K8sClient           client.Client
	CloudServices       aws.Cloud
	K8sResourceManagers k8s.ResourceManagers
	InstallationManager controller.InstallationManager
	Logger              logr.Logger
	DiscoveryClient     discovery.DiscoveryInterface
}

func New(options Options) *Framework {
	err := options.Validate()
	Expect(err).ToNot(HaveOccurred())

	// Create config for clients that need to access subresources
	config, err := clientcmd.BuildConfigFromFlags("", options.KubeConfig)
	clientset, err := kubernetes.NewForConfig(config)
	Expect(err).ToNot(HaveOccurred())

	// For integration tests, the schema contains all Kubernetes resources for simplicity.
	k8sSchema := runtime.NewScheme()
	clientgoscheme.AddToScheme(k8sSchema)
	eniconfigscheme.AddToScheme(k8sSchema)
	sgpscheme.AddToScheme(k8sSchema)

	cache, err := k8sapi.CreateKubeClientCache(config, k8sSchema, nil)
	if err != nil {
		log.Fatalf("failed to create cache: %v", err)
	}
	Expect(err).NotTo(HaveOccurred())

	// For the IPAMD events test, the cache must be able to index on Event reasons.
	err = cache.IndexField(context.TODO(), &eventsv1.Event{}, "reason", func(o client.Object) []string {
		event, ok := o.(*eventsv1.Event)
		if !ok {
			panic(fmt.Sprintf("Expected Event but got a %T", o))
		}
		if event.Reason != "" {
			return []string{event.Reason}
		}
		return nil
	})
	Expect(err).NotTo(HaveOccurred())
	// Start cache and wait for initial sync
	k8sapi.StartKubeClientCache(cache)

	// The cache will start a WATCH for all GVKs in the scheme. CNINode objects should not
	// be cached, so their GVK is added only for the client.
	rcscheme.AddToScheme(k8sSchema)
	k8sClient, err := client.New(config, client.Options{
		Cache: &client.CacheOptions{
			Reader: cache,
		},
		Scheme: k8sSchema,
	})
	if err != nil {
		log.Fatalf("failed to create client: %v", err)
	}

	cloudConfig := aws.CloudConfig{Region: options.AWSRegion, VpcID: options.AWSVPCID,
		EKSEndpoint: options.EKSEndpoint}

	awsCloud, err := aws.NewCloud(cloudConfig)

	if err != nil {
		log.Fatalf("failed to create AWS cloud client: %v", err)
	}
	return &Framework{
		Options:             options,
		K8sClient:           k8sClient,
		CloudServices:       awsCloud,
		K8sResourceManagers: k8s.NewResourceManager(k8sClient, clientset, k8sSchema, config),
		InstallationManager: controller.NewDefaultInstallationManager(
			helm.NewDefaultReleaseManager(options.KubeConfig)),
		Logger:          utils.NewGinkgoLogger(),
		DiscoveryClient: clientset.Discovery(),
	}
}
