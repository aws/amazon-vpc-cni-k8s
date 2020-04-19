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
	"runtime"
	"sync"
	"time"

	"github.com/operator-framework/operator-sdk/pkg/sdk"
	"github.com/pkg/errors"

	"github.com/aws/amazon-vpc-cni-k8s/pkg/apis/crd/v1alpha1"
	"github.com/aws/amazon-vpc-cni-k8s/pkg/utils/logger"
	sdkVersion "github.com/operator-framework/operator-sdk/version"
	corev1 "k8s.io/api/core/v1"
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

type ENIConfig interface {
	MyENIConfig() (*v1alpha1.ENIConfigSpec, error)
	Getter() *ENIConfigInfo
}

var ErrNoENIConfig = errors.New("eniconfig: eniconfig is not available")

var log = logger.Get()

// ENIConfigController defines global context for ENIConfig controller
type ENIConfigController struct {
	eni                    map[string]*v1alpha1.ENIConfigSpec
	myENI                  string
	eniLock                sync.RWMutex
	myNodeName             string
	eniConfigAnnotationDef string
	eniConfigLabelDef      string
}

// ENIConfigInfo returns locally cached ENIConfigs
type ENIConfigInfo struct {
	ENI                    map[string]v1alpha1.ENIConfigSpec
	MyENI                  string
	EniConfigAnnotationDef string
	EniConfigLabelDef      string
}

// NewENIConfigController creates a new ENIConfig controller
func NewENIConfigController() *ENIConfigController {
	return &ENIConfigController{
		myNodeName:             os.Getenv("MY_NODE_NAME"),
		eni:                    make(map[string]*v1alpha1.ENIConfigSpec),
		myENI:                  eniConfigDefault,
		eniConfigAnnotationDef: getEniConfigAnnotationDef(),
		eniConfigLabelDef:      getEniConfigLabelDef(),
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

// Handle handles ENIConfig updates from API Server and store them in local cache
func (h *Handler) Handle(ctx context.Context, event sdk.Event) error {
	switch o := event.Object.(type) {
	case *v1alpha1.ENIConfig:
		eniConfigName := o.GetName()
		if event.Deleted {
			log.Debugf("Deleting ENIConfig: %s", eniConfigName)
			h.controller.eniLock.Lock()
			defer h.controller.eniLock.Unlock()
			delete(h.controller.eni, eniConfigName)
			return nil
		}

		curENIConfig := o.DeepCopy()

		log.Debugf("Handle ENIConfig Add/Update: %s, %v, %s", eniConfigName, curENIConfig.Spec.SecurityGroups, curENIConfig.Spec.Subnet)

		h.controller.eniLock.Lock()
		defer h.controller.eniLock.Unlock()
		h.controller.eni[eniConfigName] = &curENIConfig.Spec

	case *corev1.Node:
		log.Debugf("Handle corev1.Node: %s, %v, %v", o.GetName(), o.GetAnnotations(), o.GetLabels())
		// Get annotations if not found get labels if not found fallback use default
		if h.controller.myNodeName == o.GetName() {
			val, ok := o.GetAnnotations()[h.controller.eniConfigAnnotationDef]
			if !ok {
				val, ok = o.GetLabels()[h.controller.eniConfigLabelDef]
				if !ok {
					val = eniConfigDefault
				}
			}

			if h.controller.myENI != val {
				h.controller.eniLock.Lock()
				defer h.controller.eniLock.Unlock()
				h.controller.myENI = val
				log.Debugf("Setting myENI to: %s", val)
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
	resyncPeriod := time.Second * 5
	log.Infof("Watching %s, %s, every %v s", resource, kind, resyncPeriod.Seconds())
	sdk.Watch(resource, kind, "", resyncPeriod)
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
	output.EniConfigAnnotationDef = getEniConfigAnnotationDef()
	output.EniConfigLabelDef = getEniConfigLabelDef()

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
