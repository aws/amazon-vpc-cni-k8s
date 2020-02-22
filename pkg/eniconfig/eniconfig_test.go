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
package eniconfig

import (
	"fmt"
	"os"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/apis/crd/v1alpha1"
	sdk "github.com/operator-framework/operator-sdk/pkg/sdk"
	"github.com/stretchr/testify/assert"

	corev1 "k8s.io/api/core/v1"
)

func updateENIConfig(hdlr sdk.Handler, name string, eniConfig v1alpha1.ENIConfigSpec, toDelete bool) {
	event := sdk.Event{
		Object: &v1alpha1.ENIConfig{
			TypeMeta: metav1.TypeMeta{APIVersion: v1alpha1.SchemeGroupVersion.String()},
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Spec: eniConfig},
		Deleted: toDelete,
	}

	hdlr.Handle(nil, event)
}

func updateNodeAnnotation(hdlr sdk.Handler, nodeName string, configName string, toDelete bool) {

	node := corev1.Node{
		TypeMeta: metav1.TypeMeta{APIVersion: corev1.SchemeGroupVersion.String()},
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
		},
	}
	accessor, err := meta.Accessor(&node)

	if err != nil {
		fmt.Printf("Failed to call meta.Access %v", err)
	}

	event := sdk.Event{
		Object:  &node,
		Deleted: toDelete,
	}
	eniAnnotations := make(map[string]string)
	eniConfigAnnotationDef := getEniConfigAnnotationDef()

	if !toDelete {
		eniAnnotations[eniConfigAnnotationDef] = configName
	}
	accessor.SetAnnotations(eniAnnotations)
	hdlr.Handle(nil, event)
}

func updateNodeLabel(hdlr sdk.Handler, nodeName string, configName string, toDelete bool) {

	node := corev1.Node{
		TypeMeta: metav1.TypeMeta{APIVersion: corev1.SchemeGroupVersion.String()},
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
		},
	}
	accessor, err := meta.Accessor(&node)

	if err != nil {
		fmt.Printf("Failed to call meta.Access %v", err)
	}

	event := sdk.Event{
		Object:  &node,
		Deleted: toDelete,
	}
	eniLabels := make(map[string]string)
	eniConfigLabelDef := getEniConfigLabelDef()

	if !toDelete {
		eniLabels[eniConfigLabelDef] = configName
	}
	accessor.SetLabels(eniLabels)
	hdlr.Handle(nil, event)
}

func TestENIConfig(t *testing.T) {

	testENIConfigController := NewENIConfigController()

	testHandler := NewHandler(testENIConfigController)

	// If there is no default ENI config
	_, err := testENIConfigController.MyENIConfig()
	assert.Error(t, err)

	// Start with default config
	defaultSGs := []string{"sg1-id", "sg2-id"}
	defaultSubnet := "subnet1"
	defaultCfg := v1alpha1.ENIConfigSpec{
		SecurityGroups: defaultSGs,
		Subnet:         defaultSubnet}

	updateENIConfig(testHandler, eniConfigDefault, defaultCfg, false)

	outputCfg, err := testENIConfigController.MyENIConfig()
	assert.NoError(t, err)
	assert.Equal(t, defaultCfg, *outputCfg)

	// Add one more ENI config, but it should NOT impact default
	group1Cfg := v1alpha1.ENIConfigSpec{
		SecurityGroups: []string{"sg11-id", "sg12-id"},
		Subnet:         "subnet11"}
	group1Name := "group1ENIconfig"
	updateENIConfig(testHandler, group1Name, group1Cfg, false)

	outputCfg, err = testENIConfigController.MyENIConfig()
	assert.NoError(t, err)
	assert.Equal(t, defaultCfg, *outputCfg)

}

func TestNodeENIConfig(t *testing.T) {
	myNodeName := "testMyNodeWithAnnotation"
	myENIConfig := "testMyENIConfig"
	os.Setenv("MY_NODE_NAME", myNodeName)
	testENIConfigController := NewENIConfigController()

	testHandler := NewHandler(testENIConfigController)
	updateNodeAnnotation(testHandler, myNodeName, myENIConfig, false)

	// If there is no ENI config
	_, err := testENIConfigController.MyENIConfig()
	assert.Error(t, err)

	// Add eniconfig for myENIConfig
	group1Cfg := v1alpha1.ENIConfigSpec{
		SecurityGroups: []string{"sg21-id", "sg22-id"},
		Subnet:         "subnet21"}
	updateENIConfig(testHandler, myENIConfig, group1Cfg, false)
	outputCfg, err := testENIConfigController.MyENIConfig()
	assert.NoError(t, err)
	assert.Equal(t, group1Cfg, *outputCfg)

	// Add default config
	defaultSGs := []string{"sg1-id", "sg2-id"}
	defaultSubnet := "subnet1"
	defaultCfg := v1alpha1.ENIConfigSpec{
		SecurityGroups: defaultSGs,
		Subnet:         defaultSubnet}
	updateENIConfig(testHandler, eniConfigDefault, defaultCfg, false)
	outputCfg, err = testENIConfigController.MyENIConfig()
	assert.NoError(t, err)
	assert.Equal(t, group1Cfg, *outputCfg)

	// Delete node's myENIConfig annotation, then the value should fallback to default
	updateNodeAnnotation(testHandler, myNodeName, myENIConfig, true)
	outputCfg, err = testENIConfigController.MyENIConfig()
	assert.NoError(t, err)
	assert.Equal(t, defaultCfg, *outputCfg)

}

func TestNodeENIConfigLabel(t *testing.T) {
	myNodeName := "testMyNodeWithLabel"
	myENIConfig := "testMyENIConfig"
	os.Setenv("MY_NODE_NAME", myNodeName)
	testENIConfigController := NewENIConfigController()

	testHandler := NewHandler(testENIConfigController)
	updateNodeLabel(testHandler, myNodeName, myENIConfig, false)

	// If there is no ENI config
	_, err := testENIConfigController.MyENIConfig()
	assert.Error(t, err)

	// Add eniconfig for myENIConfig
	group1Cfg := v1alpha1.ENIConfigSpec{
		SecurityGroups: []string{"sg21-id", "sg22-id"},
		Subnet:         "subnet21"}
	updateENIConfig(testHandler, myENIConfig, group1Cfg, false)
	outputCfg, err := testENIConfigController.MyENIConfig()
	assert.NoError(t, err)
	assert.Equal(t, group1Cfg, *outputCfg)

	// Add default config
	defaultSGs := []string{"sg1-id", "sg2-id"}
	defaultSubnet := "subnet1"
	defaultCfg := v1alpha1.ENIConfigSpec{
		SecurityGroups: defaultSGs,
		Subnet:         defaultSubnet}
	updateENIConfig(testHandler, eniConfigDefault, defaultCfg, false)
	outputCfg, err = testENIConfigController.MyENIConfig()
	assert.NoError(t, err)
	assert.Equal(t, group1Cfg, *outputCfg)

	// Delete node's myENIConfig annotation, then the value should fallback to default
	updateNodeLabel(testHandler, myNodeName, myENIConfig, true)
	outputCfg, err = testENIConfigController.MyENIConfig()
	assert.NoError(t, err)
	assert.Equal(t, defaultCfg, *outputCfg)

}

func TestGetEniConfigAnnotationDefDefault(t *testing.T) {
	os.Unsetenv(envEniConfigAnnotationDef)
	eniConfigAnnotationDef := getEniConfigAnnotationDef()
	assert.Equal(t, eniConfigAnnotationDef, defaultEniConfigAnnotationDef)
}

func TestGetEniConfigAnnotationlDefCustom(t *testing.T) {
	os.Setenv(envEniConfigAnnotationDef, "k8s.amazonaws.com/eniConfigCustom")
	eniConfigAnnotationDef := getEniConfigAnnotationDef()
	assert.Equal(t, eniConfigAnnotationDef, "k8s.amazonaws.com/eniConfigCustom")
}

func TestGetEniConfigLabelDefDefault(t *testing.T) {
	os.Unsetenv(envEniConfigLabelDef)
	eniConfigLabelDef := getEniConfigLabelDef()
	assert.Equal(t, eniConfigLabelDef, defaultEniConfigLabelDef)
}

func TestGetEniConfigLabelDefCustom(t *testing.T) {
	os.Setenv(envEniConfigLabelDef, "k8s.amazonaws.com/eniConfigCustom")
	eniConfigLabelDef := getEniConfigLabelDef()
	assert.Equal(t, eniConfigLabelDef, "k8s.amazonaws.com/eniConfigCustom")
}
