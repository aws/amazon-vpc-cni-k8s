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
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"os"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/apis/crd/v1alpha1"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
	"github.com/pkg/errors"
)

const (
	defaultEniConfigAnnotationDef = "k8s.amazonaws.com/eniConfig"
	defaultEniConfigLabelDef      = "k8s.amazonaws.com/eniConfig"
	eniConfigDefault              = "default"

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
	eniConfigName, err := GetNodeSpecificENIConfigName(ctx, k8sClient)
	if err != nil {
        log.Debugf("Error while retrieving Node name")
	}

	log.Infof("Found ENI Config Name: %s", eniConfigName)

	eniConfigsList := v1alpha1.ENIConfigList{}
	err = k8sClient.List(ctx, &eniConfigsList)
	if err != nil {
		fmt.Errorf("Error while EniConfig List Get: %s", err)
	}
	for _, eni := range eniConfigsList.Items {
		if eniConfigName == eni.Name {
			log.Debugf("Matching ENIConfig found: %s - %s - %s ", eni.Name, eni.Spec.Subnet, eni.Spec.SecurityGroups)
			return &v1alpha1.ENIConfigSpec{
				SecurityGroups: eni.Spec.SecurityGroups,
				Subnet:         eni.Spec.Subnet,
			}, nil
		}
	}
	return nil, ErrNoENIConfig
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

func GetNodeSpecificENIConfigName(ctx context.Context, k8sClient client.Client) (string, error) {
    var eniConfigName string
	nodeList := corev1.NodeList{}
	err := k8sClient.List(ctx, &nodeList)
	if err != nil {
		fmt.Errorf("Error while Node List Get: %s", err)
	}
	log.Debugf("Node Count: ", len(nodeList.Items))
	for _, node := range nodeList.Items {
		if node.Name == os.Getenv("MY_NODE_NAME") {
			log.Debugf("Node Info: %s", node.Name)
			val, ok := node.GetAnnotations()[getEniConfigAnnotationDef()]
			if !ok {
				val, ok = node.GetLabels()[getEniConfigLabelDef()]
				if !ok {
					val = eniConfigDefault
				}
			}
			eniConfigName = val
			if val != eniConfigDefault {
				labels := node.GetLabels()
				labels["vpc.amazonaws.com/eniConfig"] = eniConfigName
				node.SetLabels(labels)
			}
		}
	}
	return eniConfigName, nil
}
