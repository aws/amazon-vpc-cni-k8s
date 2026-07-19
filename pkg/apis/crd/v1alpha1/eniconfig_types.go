/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ENIConfigSpec defines the desired state of ENIConfig
type ENIConfigSpec struct {
	SecurityGroups []string `json:"securityGroups"`
	Subnet         string   `json:"subnet"`
	// WarmIPTarget overrides the WARM_IP_TARGET env var for nodes selecting this ENIConfig.
	// Only consulted when AWS_VPC_K8S_CNI_CUSTOM_NETWORK_CFG=true. Read once at daemon startup.
	WarmIPTarget *int `json:"warmIPTarget,omitempty"`
	// MinimumIPTarget overrides the MINIMUM_IP_TARGET env var for nodes selecting this ENIConfig.
	// Only consulted when AWS_VPC_K8S_CNI_CUSTOM_NETWORK_CFG=true. Read once at daemon startup.
	MinimumIPTarget *int `json:"minimumIPTarget,omitempty"`
	// WarmENITarget overrides the WARM_ENI_TARGET env var for nodes selecting this ENIConfig.
	// Only consulted when AWS_VPC_K8S_CNI_CUSTOM_NETWORK_CFG=true. Read once at daemon startup.
	WarmENITarget *int `json:"warmENITarget,omitempty"`
	// WarmPrefixTarget overrides the WARM_PREFIX_TARGET env var for nodes selecting this ENIConfig.
	// Only consulted when AWS_VPC_K8S_CNI_CUSTOM_NETWORK_CFG=true. Read once at daemon startup.
	WarmPrefixTarget *int `json:"warmPrefixTarget,omitempty"`
	// MaxENI overrides the MAX_ENI env var for nodes selecting this ENIConfig. Must be >= 1; the
	// resulting value is clamped to the instance type's ENI limit.
	// Only consulted when AWS_VPC_K8S_CNI_CUSTOM_NETWORK_CFG=true. Read once at daemon startup.
	MaxENI *int `json:"maxENI,omitempty"`
}

// ENIConfigStatus defines the observed state of ENIConfig
type ENIConfigStatus struct {
	// Fill me
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// ENIConfig is the Schema for the eniconfigs API
type ENIConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ENIConfigSpec   `json:"spec,omitempty"`
	Status ENIConfigStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ENIConfigList contains a list of ENIConfig
type ENIConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ENIConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ENIConfig{}, &ENIConfigList{})
}
