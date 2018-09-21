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

// Package eniconfig handles eniconfig CRD
package eniconfig

import (
	"context"
	"os"
	"runtime"
	"sync"
	"time"

	sdk "github.com/operator-framework/operator-sdk/pkg/sdk"
	"github.com/pkg/errors"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/apis/crd/v1alpha1"

	corev1 "k8s.io/api/core/v1"

	k8sutil "github.com/operator-framework/operator-sdk/pkg/util/k8sutil"
	sdkVersion "github.com/operator-framework/operator-sdk/version"

	log "github.com/cihub/seelog"
)

const (
	eniConfigAnnotationDef = "k8s.amazonaws.com/eniConfig"
	eniConfigDefault       = "default"
)

type ENIConfig interface {
	MyENIConfig() (*v1alpha1.ENIConfigSpec, error)
	Getter() *ENIConfigInfo
}

var ErrNoENIConfig = errors.New("eniconfig: eniconfig is not available")

// ENIConfigController defines global context for ENIConfig controller
type ENIConfigController struct {
	eni        map[string]*v1alpha1.ENIConfigSpec
	myENI      string
	eniLock    sync.RWMutex
	myNodeName string
}

// ENIConfigInfo returns locally cached ENIConfigs
type ENIConfigInfo struct {
	ENI   map[string]v1alpha1.ENIConfigSpec
	MyENI string
}

// NewENIConfigController creates a new ENIConfig controller
func NewENIConfigController() *ENIConfigController {
	return &ENIConfigController{
		myNodeName: os.Getenv("MY_NODE_NAME"),
		eni:        make(map[string]*v1alpha1.ENIConfigSpec),
		myENI:      eniConfigDefault,
	}
}

// NewHandler creates a new handler for sdk
func NewHandler(controller *ENIConfigController) sdk.Handler {
	return &Handler{controller: controller}
}

// Handler stores the ENIConfigController
type Handler struct {
	controller *ENIConfigController
}

// Handle handles ENIconfigs updates from API Server and store them in local cache
func (h *Handler) Handle(ctx context.Context, event sdk.Event) error {
	switch o := event.Object.(type) {
	case *v1alpha1.ENIConfig:

		eniConfigName := o.GetName()

		curENIConfig := o.DeepCopy()

		if event.Deleted {
			log.Infof("Deleting ENIConfig: %s", eniConfigName)
			h.controller.eniLock.Lock()
			defer h.controller.eniLock.Unlock()
			delete(h.controller.eni, eniConfigName)
			return nil
		}

		log.Infof("Handle ENIConfig Add/Update:  %s, %v, %s", eniConfigName, curENIConfig.Spec.SecurityGroups, curENIConfig.Spec.Subnet)

		h.controller.eniLock.Lock()
		defer h.controller.eniLock.Unlock()
		h.controller.eni[eniConfigName] = &curENIConfig.Spec

	case *corev1.Node:

		log.Infof("Handle corev1.Node: %s, %v", o.GetName(), o.GetAnnotations())
		if h.controller.myNodeName == o.GetName() {
			annotation := o.GetAnnotations()

			val, ok := annotation[eniConfigAnnotationDef]
			if ok {
				h.controller.eniLock.Lock()
				defer h.controller.eniLock.Unlock()
				h.controller.myENI = val
				log.Infof(" Setting myENI to: %s", val)
			} else {
				h.controller.eniLock.Lock()
				defer h.controller.eniLock.Unlock()
				h.controller.myENI = eniConfigDefault
				log.Infof(" Setting myENI to: %s", eniConfigDefault)
			}
		}
	}
	return nil
}

func printVersion() {
	log.Infof("Go Version: %s", runtime.Version())
	log.Infof("Go OS/Arch: %s/%s", runtime.GOOS, runtime.GOARCH)
	log.Infof("operator-sdk Version: %v", sdkVersion.Version)
}

// Start kicks off ENIConfig controller
func (eniCfg *ENIConfigController) Start() {
	printVersion()

	sdk.ExposeMetricsPort()

	resource := "crd.k8s.amazonaws.com/v1alpha1"
	kind := "ENIConfig"
	namespace, err := k8sutil.GetWatchNamespace()
	if err != nil {
		log.Errorf("failed to get watch namespace: %v", err)
	}
	resyncPeriod := time.Second * 5
	log.Infof("Watching %s, %s, %s, %d", resource, kind, namespace, resyncPeriod)
	sdk.Watch(resource, kind, namespace, resyncPeriod)
	sdk.Watch("/v1", "Node", corev1.NamespaceAll, resyncPeriod)
	sdk.Handle(NewHandler(eniCfg))
	sdk.Run(context.TODO())
}

func (eniCfg *ENIConfigController) Getter() *ENIConfigInfo {
	output := &ENIConfigInfo{
		ENI: make(map[string]v1alpha1.ENIConfigSpec),
	}
	eniCfg.eniLock.Lock()
	defer eniCfg.eniLock.Unlock()

	output.MyENI = eniCfg.myENI

	for name, val := range eniCfg.eni {
		output.ENI[name] = *val
	}

	return output
}

// MyENIConfig returns the security
func (eniCfg *ENIConfigController) MyENIConfig() (*v1alpha1.ENIConfigSpec, error) {
	eniCfg.eniLock.Lock()
	defer eniCfg.eniLock.Unlock()

	myENIConfig, ok := eniCfg.eni[eniCfg.myENI]

	if ok {
		return &v1alpha1.ENIConfigSpec{
			SecurityGroups: myENIConfig.SecurityGroups,
			Subnet:         myENIConfig.Subnet,
		}, nil
	}
	return nil, ErrNoENIConfig
}
