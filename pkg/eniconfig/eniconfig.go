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
	"strconv"

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

	// Per-node override keys for IPAM tuning settings. Both annotations and labels
	// share the same key string; annotations take precedence over labels.
	NodeAnnotationWarmIPTarget     = "vpc.amazonaws.com/warm-ip-target"
	NodeAnnotationMinimumIPTarget  = "vpc.amazonaws.com/minimum-ip-target"
	NodeAnnotationWarmENITarget    = "vpc.amazonaws.com/warm-eni-target"
	NodeAnnotationWarmPrefixTarget = "vpc.amazonaws.com/warm-prefix-target"
	NodeAnnotationMaxENI           = "vpc.amazonaws.com/max-eni"
	NodeLabelWarmIPTarget          = "vpc.amazonaws.com/warm-ip-target"
	NodeLabelMinimumIPTarget       = "vpc.amazonaws.com/minimum-ip-target"
	NodeLabelWarmENITarget         = "vpc.amazonaws.com/warm-eni-target"
	NodeLabelWarmPrefixTarget      = "vpc.amazonaws.com/warm-prefix-target"
	NodeLabelMaxENI                = "vpc.amazonaws.com/max-eni"
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

	spec := eniConfig.Spec.DeepCopy()
	return spec, nil
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

// Source labels used in NodeOverrides.Sources to identify where a resolved
// value came from. Useful for verifying precedence in logs.
const (
	SourceAnnotation = "annotation"
	SourceLabel      = "label"
	SourceENIConfig  = "eniconfig"
)

// NodeOverrides carries per-node values for IPAM tuning settings that an
// operator wants set differently for individual nodes. A nil pointer means
// "no override; fall back to the cluster-wide env-var default". Sources records
// which source ("annotation"/"label"/"eniconfig") each override came from.
type NodeOverrides struct {
	WarmIPTarget     *int
	MinimumIPTarget  *int
	WarmENITarget    *int
	WarmPrefixTarget *int
	MaxENI           *int
	Sources          map[string]string
}

// Setting names used as keys in NodeOverrides.Sources.
const (
	SettingWarmIPTarget     = "WARM_IP_TARGET"
	SettingMinimumIPTarget  = "MINIMUM_IP_TARGET"
	SettingWarmENITarget    = "WARM_ENI_TARGET"
	SettingWarmPrefixTarget = "WARM_PREFIX_TARGET"
	SettingMaxENI           = "MAX_ENI"
)

// ResolveNodeOverrides returns per-node overrides for the IPAM tuning settings
// (WARM_IP_TARGET, MINIMUM_IP_TARGET, WARM_ENI_TARGET, WARM_PREFIX_TARGET,
// MAX_ENI). For each setting, a nil pointer means "no override found; the
// caller should fall back to its env-var default".
//
// Precedence per setting: node annotation > node label > ENIConfig spec
// (the ENIConfig is only consulted when useCustomNetworking is true).
func ResolveNodeOverrides(ctx context.Context, k8sClient client.Client, useCustomNetworking bool) NodeOverrides {
	out := NodeOverrides{Sources: map[string]string{}}

	node, err := k8sapi.GetNode(ctx, k8sClient)
	if err != nil {
		log.Warnf("ResolveNodeOverrides: unable to fetch node, skipping per-node overrides: %s", err)
	} else {
		annotations := node.GetAnnotations()
		labels := node.GetLabels()

		out.WarmIPTarget, out.Sources[SettingWarmIPTarget] = pickNonNegative(annotations[NodeAnnotationWarmIPTarget], labels[NodeLabelWarmIPTarget])
		out.MinimumIPTarget, out.Sources[SettingMinimumIPTarget] = pickNonNegative(annotations[NodeAnnotationMinimumIPTarget], labels[NodeLabelMinimumIPTarget])
		out.WarmENITarget, out.Sources[SettingWarmENITarget] = pickNonNegative(annotations[NodeAnnotationWarmENITarget], labels[NodeLabelWarmENITarget])
		out.WarmPrefixTarget, out.Sources[SettingWarmPrefixTarget] = pickNonNegative(annotations[NodeAnnotationWarmPrefixTarget], labels[NodeLabelWarmPrefixTarget])
		out.MaxENI, out.Sources[SettingMaxENI] = pickPositive(annotations[NodeAnnotationMaxENI], labels[NodeLabelMaxENI])
	}

	if useCustomNetworking && !out.complete() {
		spec, err := MyENIConfig(ctx, k8sClient)
		if err != nil {
			log.Debugf("ResolveNodeOverrides: ENIConfig lookup failed, skipping ENIConfig overrides: %s", err)
		} else {
			if out.WarmIPTarget == nil && spec.WarmIPTarget != nil && *spec.WarmIPTarget >= 0 {
				v := *spec.WarmIPTarget
				out.WarmIPTarget = &v
				out.Sources[SettingWarmIPTarget] = SourceENIConfig
			}
			if out.MinimumIPTarget == nil && spec.MinimumIPTarget != nil && *spec.MinimumIPTarget >= 0 {
				v := *spec.MinimumIPTarget
				out.MinimumIPTarget = &v
				out.Sources[SettingMinimumIPTarget] = SourceENIConfig
			}
			if out.WarmENITarget == nil && spec.WarmENITarget != nil && *spec.WarmENITarget >= 0 {
				v := *spec.WarmENITarget
				out.WarmENITarget = &v
				out.Sources[SettingWarmENITarget] = SourceENIConfig
			}
			if out.WarmPrefixTarget == nil && spec.WarmPrefixTarget != nil && *spec.WarmPrefixTarget >= 0 {
				v := *spec.WarmPrefixTarget
				out.WarmPrefixTarget = &v
				out.Sources[SettingWarmPrefixTarget] = SourceENIConfig
			}
			if out.MaxENI == nil && spec.MaxENI != nil && *spec.MaxENI >= 1 {
				v := *spec.MaxENI
				out.MaxENI = &v
				out.Sources[SettingMaxENI] = SourceENIConfig
			}
		}
	}
	return out
}

// complete reports whether every supported override has already been resolved,
// so the caller can skip the ENIConfig lookup.
func (o NodeOverrides) complete() bool {
	return o.WarmIPTarget != nil && o.MinimumIPTarget != nil && o.WarmENITarget != nil &&
		o.WarmPrefixTarget != nil && o.MaxENI != nil
}

// pickNonNegative returns the first of annotation/label that parses as a
// non-negative integer along with the source it came from ("annotation" or
// "label"). Empty strings are treated as "not set"; if neither parses,
// (nil, "") is returned.
func pickNonNegative(annotation, label string) (*int, string) {
	if v, ok := parseNonNegativeInt(annotation); ok {
		return &v, SourceAnnotation
	}
	if v, ok := parseNonNegativeInt(label); ok {
		return &v, SourceLabel
	}
	return nil, ""
}

// pickPositive returns the first of annotation/label that parses as an integer
// >= 1 along with its source. Used for MAX_ENI, where 0 / negative is not
// meaningful (env-var convention treats <1 as "use the instance default").
func pickPositive(annotation, label string) (*int, string) {
	if v, ok := parsePositiveInt(annotation); ok {
		return &v, SourceAnnotation
	}
	if v, ok := parsePositiveInt(label); ok {
		return &v, SourceLabel
	}
	return nil, ""
}

// parseNonNegativeInt parses s as a non-negative integer. Returns ok=false on
// empty string, parse error, or negative value (with a warning log for the
// latter two).
func parseNonNegativeInt(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		log.Warnf("ResolveNodeOverrides: ignoring non-integer value %q: %s", s, err)
		return 0, false
	}
	if v < 0 {
		log.Warnf("ResolveNodeOverrides: ignoring negative value %d", v)
		return 0, false
	}
	return v, true
}

// parsePositiveInt parses s as an integer >= 1. Returns ok=false on empty
// string, parse error, or value < 1 (with a warning log for parse errors and
// out-of-range values).
func parsePositiveInt(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		log.Warnf("ResolveNodeOverrides: ignoring non-integer value %q: %s", s, err)
		return 0, false
	}
	if v < 1 {
		log.Warnf("ResolveNodeOverrides: ignoring out-of-range value %d (must be >= 1)", v)
		return 0, false
	}
	return v, true
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
