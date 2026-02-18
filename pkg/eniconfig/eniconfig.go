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

// Package eniconfig handles eniconfig CRD
package eniconfig

import (
	"context"
	"os"

	"k8s.io/apimachinery/pkg/types"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/apis/crd/v1alpha1"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/k8sapi"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
)

const (
	defaultEniConfigAnnotationDef = "k8s.amazonaws.com/eniConfig"
	defaultEniConfigLabelDef      = "k8s.amazonaws.com/eniConfig"
	EniConfigDefault              = "default"

	// when this is defined, it is to be treated as the source of truth for the eniconfig.
	// it is meant to be used for out-of-band mananagement of the eniConfig - i.e. on the kubelet or elsewhere
	externalEniConfigLabel = "vpc.amazonaws.com/externalEniConfig"

	// when "ENI_CONFIG_LABEL_DEF is defined, ENIConfigController will use that label key to
	// search if is setting value for eniConfigLabelDef
	// Example:
	//   Node has set label k8s.amazonaws.com/eniConfigCustom=customConfig
	//   We can get that value in controller by setting environmental variable ENI_CONFIG_LABEL_DEF
	//   ENI_CONFIG_LABEL_DEF=k8s.amazonaws.com/eniConfigOverride
	//   This will set eniConfigLabelDef to eniConfigOverride
	envEniConfigAnnotationDef = "ENI_CONFIG_ANNOTATION_DEF"
	envEniConfigLabelDef      = "ENI_CONFIG_LABEL_DEF"
)

// ENIConfig interface
type ENIConfig interface {
	MyENIConfig(client.Client) (*v1alpha1.ENIConfigSpec, error)
	GetENIConfigName(context.Context, client.Client) (string, error)
}

// ErrNoENIConfig is the missing ENIConfig error
var ErrNoENIConfig = errors.New("eniconfig: eniconfig is not available")

var log = logger.Get()

// ENIConfigInfo returns locally cached ENIConfigs
type ENIConfigInfo struct {
	ENI                    map[string]v1alpha1.ENIConfigSpec
	MyENI                  string
	EniConfigAnnotationDef string
	EniConfigLabelDef      string
}

// MyENIConfig returns the ENIConfig applicable to the particular node
func MyENIConfig(ctx context.Context, k8sClient client.Client) (*v1alpha1.ENIConfigSpec, error) {
	node, err := k8sapi.GetNode(ctx, k8sClient)
	if err != nil {
		log.Debugf("Error while retrieving Node")
	}

	eniConfigName, err := GetNodeSpecificENIConfigName(node)
	if err != nil {
		log.Debugf("Error while retrieving Node ENIConfig name")
	}

	log.Infof("Found ENI Config Name: %s", eniConfigName)
	var eniConfig v1alpha1.ENIConfig
	err = k8sClient.Get(ctx, types.NamespacedName{Name: eniConfigName}, &eniConfig)
	if err != nil {
		log.Errorf("error while retrieving eniconfig: %s", err)
		return nil, ErrNoENIConfig
	}

	return &v1alpha1.ENIConfigSpec{
		SecurityGroups: eniConfig.Spec.SecurityGroups,
		Subnet:         eniConfig.Spec.Subnet,
		VpcId:          eniConfig.Spec.VpcId,
	}, nil
}

// getEniConfigAnnotationDef returns eniConfigAnnotation
func getEniConfigAnnotationDef() string {
	inputStr, found := os.LookupEnv(envEniConfigAnnotationDef)

	if !found {
		return defaultEniConfigAnnotationDef
	}
	if len(inputStr) > 0 {
		log.Debugf("Using ENI_CONFIG_ANNOTATION_DEF %v", inputStr)
		return inputStr
	}
	return defaultEniConfigAnnotationDef
}

// getEniConfigLabelDef returns eniConfigLabel name
func getEniConfigLabelDef() string {
	inputStr, found := os.LookupEnv(envEniConfigLabelDef)

	if !found {
		return defaultEniConfigLabelDef
	}
	if len(inputStr) > 0 {
		log.Debugf("Using ENI_CONFIG_LABEL_DEF %v", inputStr)
		return inputStr
	}
	return defaultEniConfigLabelDef
}

func GetNodeSpecificENIConfigName(node corev1.Node) (string, error) {
	var eniConfigName string

	//Derive ENIConfig Name from either externally managed label, Node Annotations or Labels
	labels := node.GetLabels()
	eniConfigName, ok := labels[externalEniConfigLabel]
	if !ok {
		eniConfigName, ok = node.GetAnnotations()[getEniConfigAnnotationDef()]
		if !ok {
			eniConfigName, ok = node.GetLabels()[getEniConfigLabelDef()]
			if !ok {
				eniConfigName = EniConfigDefault
			}
		}
	}

	return eniConfigName, nil
}
