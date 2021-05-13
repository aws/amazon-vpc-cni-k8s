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

package manifest

import (
	"github.com/aws/amazon-vpc-cni-k8s/test/framework/utils"
	v1 "k8s.io/api/networking/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type NetworkPolicyBuilder struct {
	name         string
	namespace    string
	podSelector  metaV1.LabelSelector
	policyTypes  []v1.PolicyType
	ingressRules []v1.NetworkPolicyIngressRule
	egressRules  []v1.NetworkPolicyEgressRule
}

func NewDefaultNetworkPolicyBuilder() *NetworkPolicyBuilder {
	return &NetworkPolicyBuilder{
		namespace:   utils.DefaultTestNamespace,
		podSelector: metaV1.LabelSelector{},
	}
}

func (c *NetworkPolicyBuilder) Name(name string) *NetworkPolicyBuilder {
	c.name = name
	return c
}

func (c *NetworkPolicyBuilder) Namespace(namespace string) *NetworkPolicyBuilder {
	c.namespace = namespace
	return c
}

func (c *NetworkPolicyBuilder) PodSelectorMatchLabel(matchLabels map[string]string) *NetworkPolicyBuilder {
	c.podSelector.MatchLabels = matchLabels
	return c
}

func (c *NetworkPolicyBuilder) PolicyTypes(policyTypes []v1.PolicyType) *NetworkPolicyBuilder {
	c.policyTypes = policyTypes
	return c
}

func (c *NetworkPolicyBuilder) IngressRules(rules []v1.NetworkPolicyIngressRule) *NetworkPolicyBuilder {
	c.ingressRules = rules
	return c
}

func (c *NetworkPolicyBuilder) EgressRules(rules []v1.NetworkPolicyEgressRule) *NetworkPolicyBuilder {
	c.egressRules = rules
	return c
}

func (c *NetworkPolicyBuilder) Build() v1.NetworkPolicy {
	return v1.NetworkPolicy{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      c.name,
			Namespace: c.namespace,
		},
		Spec: v1.NetworkPolicySpec{
			PodSelector: c.podSelector,
			Ingress:     c.ingressRules,
			Egress:      c.egressRules,
			PolicyTypes: c.policyTypes,
		},
	}
}
