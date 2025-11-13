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

// ENIConfigSpec defines the desired state of ENIConfig
type ENIConfigSpec struct {
	// SecurityGroups is a list of security group IDs to associate with the ENI
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	SecurityGroups []string `json:"securityGroups"`
	
	// Subnet is the subnet ID where the ENI will be created
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^subnet-[0-9a-f]{8,17}$`
	Subnet string `json:"subnet"`
}

// ENIConfigStatus defines the observed state of ENIConfig
type ENIConfigStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Subnet",type=string,JSONPath=".spec.subnet"
// +kubebuilder:printcolumn:name="Security Groups",type=string,JSONPath=".spec.securityGroups"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// ENIConfig is the Schema for the eniconfigs API
type ENIConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ENIConfigSpec   `json:"spec,omitempty"`
	Status ENIConfigStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ENIConfigList contains a list of ENIConfig
type ENIConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ENIConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ENIConfig{}, &ENIConfigList{})
}
