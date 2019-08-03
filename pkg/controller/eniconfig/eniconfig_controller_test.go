// Copyright 2018 Amazon.com, Inc. or its affiliates. All Rights Reserved.
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
package eniconfig_test

import (
	"context"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/apis/crd/v1alpha1"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/controller/eniconfig"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/controller/eniconfig/mocks"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/stretchr/testify/assert"
)

const expectedRequeueTime = 5 * time.Second

func updateENIConfig(t *testing.T, client client.Client, reconciler reconcile.Reconciler, name string, eniConfig v1alpha1.ENIConfigSpec) {
	err := client.Create(context.TODO(), &v1alpha1.ENIConfig{
		TypeMeta: metav1.TypeMeta{APIVersion: v1alpha1.SchemeGroupVersion.String()},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: eniConfig,
	})
	assert.NoError(t, err)

	// Mock request to simulate Reconcile() being called on the watched resource
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name: name,
		},
	}
	res, err := reconciler.Reconcile(req)
	assert.NoError(t, err)
	assert.Equal(t, expectedRequeueTime, res.RequeueAfter)
}
func newReconcilerWithFakeClient(ctrl *gomock.Controller, cl client.Client) (*eniconfig.ReconcileENIConfig, *mocks.MockMyENIProvider) {
	s := scheme.Scheme
	s.AddKnownTypes(v1alpha1.SchemeGroupVersion, &v1alpha1.ENIConfig{})

	mgr := mocks.NewMockManager(ctrl)
	mgr.EXPECT().GetClient().Return(cl)
	mgr.EXPECT().GetScheme().Return(s)

	myEniProvider := mocks.NewMockMyENIProvider(ctrl)
	myEniProvider.EXPECT().GetMyENI().Return("")

	return eniconfig.NewReconciler(mgr, myEniProvider), myEniProvider
}

func TestMyENIConfig(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	cl := fake.NewFakeClient()
	reconciler, myEniProvider := newReconcilerWithFakeClient(ctrl, cl)

	// If there is no default ENI config
	_, err := reconciler.MyENIConfig()
	assert.Error(t, err)

	// Start with default config
	defaultSGs := []string{"sg1-id", "sg2-id"}
	defaultSubnet := "subnet1"
	defaultName := "default"
	defaultCfg := v1alpha1.ENIConfigSpec{
		SecurityGroups: defaultSGs,
		Subnet:         defaultSubnet,
	}

	myEniProvider.EXPECT().GetMyENI().AnyTimes().Return(defaultName)
	updateENIConfig(t, cl, reconciler, defaultName, defaultCfg)

	// Check config matches
	outputCfg, err := reconciler.MyENIConfig()
	assert.NoError(t, err)
	assert.Equal(t, defaultCfg, *outputCfg)

	// Add one more ENI config, but it should NOT impact default
	group1Cfg := v1alpha1.ENIConfigSpec{
		SecurityGroups: []string{"sg11-id", "sg12-id"},
		Subnet:         "subnet11"}
	group1Name := "group1ENIconfig"

	updateENIConfig(t, cl, reconciler, group1Name, group1Cfg)

	outputCfg, err = reconciler.MyENIConfig()
	assert.NoError(t, err)
	assert.Equal(t, defaultCfg, *outputCfg)
}

func TestNodeENIConfig(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	cl := fake.NewFakeClient()
	reconciler, myEniProvider := newReconcilerWithFakeClient(ctrl, cl)

	myENIConfig := "testMyENIConfig"
	myEniProvider.EXPECT().GetMyENI().Return(myENIConfig).Times(2)

	// If there is no ENI config
	_, err := reconciler.MyENIConfig()
	assert.Error(t, err)

	// Add eniconfig for myENIConfig
	group1Cfg := v1alpha1.ENIConfigSpec{
		SecurityGroups: []string{"sg21-id", "sg22-id"},
		Subnet:         "subnet21"}
	updateENIConfig(t, cl, reconciler, myENIConfig, group1Cfg)
	outputCfg, err := reconciler.MyENIConfig()
	assert.NoError(t, err)
	assert.Equal(t, group1Cfg, *outputCfg)

	// Add default config
	defaultSGs := []string{"sg1-id", "sg2-id"}
	defaultSubnet := "subnet1"
	defaultName := "default"
	defaultCfg := v1alpha1.ENIConfigSpec{
		SecurityGroups: defaultSGs,
		Subnet:         defaultSubnet}
	updateENIConfig(t, cl, reconciler, defaultName, defaultCfg)
	outputCfg, err = reconciler.MyENIConfig()
	assert.NoError(t, err)
	assert.Equal(t, group1Cfg, *outputCfg)

	// Delete node's myENIConfig annotation, then the value should fallback to default
	myEniProvider.EXPECT().GetMyENI().Return(defaultName)

	outputCfg, err = reconciler.MyENIConfig()
	assert.NoError(t, err)
	assert.Equal(t, defaultCfg, *outputCfg)
}
