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
	"flag"

	"github.com/pkg/errors"
	"k8s.io/client-go/tools/clientcmd"
)

var GlobalOptions Options

func init() {
	GlobalOptions.BindFlags()
}

type Options struct {
	KubeConfig       string
	ClusterName      string
	AWSRegion        string
	AWSVPCID         string
	NgNameLabelKey   string
	NgNameLabelVal   string
	EKSEndpoint      string
	CalicoVersion    string
	ContainerRuntime string
	InstanceType     string
	InitialAddon     string
	TargetAddon      string
	InitialManifest  string
	TargetManifest   string
	InstallCalico    bool
}

func (options *Options) BindFlags() {
	flag.StringVar(&options.KubeConfig, "cluster-kubeconfig", "", "Path to kubeconfig containing embedded authinfo (required)")
	flag.StringVar(&options.ClusterName, "cluster-name", "", `Kubernetes cluster name (required)`)
	flag.StringVar(&options.AWSRegion, "aws-region", "", `AWS Region for the kubernetes cluster`)
	flag.StringVar(&options.AWSVPCID, "aws-vpc-id", "", `AWS VPC ID for the kubernetes cluster`)
	flag.StringVar(&options.NgNameLabelKey, "ng-name-label-key", "eks.amazonaws.com/nodegroup", "label key used to identify nodegroup name")
	flag.StringVar(&options.NgNameLabelVal, "ng-name-label-val", "", "label value with the nodegroup name")
	flag.StringVar(&options.EKSEndpoint, "eks-endpoint", "", "optional eks api server endpoint")
	flag.StringVar(&options.InitialAddon, "initial-addon-version", "", "Initial CNI addon version before upgrade applied")
	flag.StringVar(&options.TargetAddon, "target-addon-version", "", "Target CNI addon version after upgrade applied")
	flag.StringVar(&options.InitialManifest, "initial-manifest-file", "", "Initial CNI manifest, can be local file path or remote Url")
	flag.StringVar(&options.TargetManifest, "target-manifest-file", "", "Target CNI manifest, can be local file path or remote Url")
	flag.StringVar(&options.CalicoVersion, "calico-version", "v3.23.0", "calico version to be tested")
	flag.StringVar(&options.ContainerRuntime, "container-runtime", "", "Optionally can specify it as 'containerd' for the test nodes")
	flag.StringVar(&options.InstanceType, "instance-type", "amd64", "Optionally specify instance type as arm64 for the test nodes")
	flag.BoolVar(&options.InstallCalico, "install-calico", true, "Install Calico operator before running e2e tests")
}

func (options *Options) Validate() error {
	if len(options.KubeConfig) == 0 {
		return errors.Errorf("%s must be set!", clientcmd.RecommendedConfigPathFlag)
	}
	if len(options.ClusterName) == 0 {
		return errors.Errorf("%s must be set!", "cluster-name")
	}
	if len(options.AWSRegion) == 0 {
		return errors.Errorf("%s must be set!", "aws-region")
	}
	if len(options.AWSVPCID) == 0 {
		return errors.Errorf("%s must be set!", "aws-vpc-id")
	}
	return nil
}
