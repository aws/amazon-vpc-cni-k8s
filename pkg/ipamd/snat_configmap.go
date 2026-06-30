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

package ipamd

import (
	"context"
	"net"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	snatConfigMapName      = "aws-node-vpc-cidrs"
	snatConfigMapNamespace = "kube-system"
	snatConfigMapKey       = "exclude-snat-cidrs"

	envEnableDynamicSNATCfg = "AWS_VPC_K8S_CNI_ENABLE_DYNAMIC_SNAT_CFG"
)

// GetExcludeSNATCIDRsFromConfigMap reads the aws-node-vpc-cidrs ConfigMap and returns
// the parsed list of CIDRs from the exclude-snat-cidrs key. Returns nil (not error) if
// the ConfigMap doesn't exist or the key is empty.
func GetExcludeSNATCIDRsFromConfigMap(ctx context.Context, k8sClient client.Client) ([]string, error) {
	if k8sClient == nil {
		return nil, nil
	}

	var cm corev1.ConfigMap
	err := k8sClient.Get(ctx, types.NamespacedName{
		Name:      snatConfigMapName,
		Namespace: snatConfigMapNamespace,
	}, &cm)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			return nil, nil
		}
		return nil, err
	}

	raw := cm.Data[snatConfigMapKey]
	if raw == "" {
		return nil, nil
	}

	return parseSNATCIDRs(raw), nil
}

// parseSNATCIDRs parses a newline or comma separated list of CIDRs.
// Invalid entries are logged and skipped.
func parseSNATCIDRs(raw string) []string {
	var cidrs []string

	// Support both newline and comma separation
	raw = strings.ReplaceAll(raw, ",", "\n")
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		_, parsed, err := net.ParseCIDR(line)
		if err != nil {
			log.Warnf("invalid CIDR %q in ConfigMap %s/%s: %v (skipping)", line, snatConfigMapNamespace, snatConfigMapName, err)
			continue
		}
		if parsed.IP.To4() == nil {
			log.Warnf("IPv6 CIDR %q in ConfigMap %s/%s not supported (skipping)", line, snatConfigMapNamespace, snatConfigMapName)
			continue
		}
		cidrs = append(cidrs, parsed.String())
	}
	return cidrs
}
