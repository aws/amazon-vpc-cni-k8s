package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ENIConfigSpec defines the desired state of ENIConfig
// +k8s:openapi-gen=true
type ENIConfigSpec struct {
	SecurityGroups []string `json:"securityGroups"`
	Subnet         string   `json:"subnet"`
}

// ENIConfigStatus defines the observed state of ENIConfig
// +k8s:openapi-gen=true
type ENIConfigStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	// Add custom validation using kubebuilder tags: https://book.kubebuilder.io/beyond_basics/generating_crd.html
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ENIConfig is the Schema for the eniconfigs API
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
type ENIConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ENIConfigSpec   `json:"spec,omitempty"`
	Status ENIConfigStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ENIConfigList contains a list of ENIConfig
type ENIConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ENIConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ENIConfig{}, &ENIConfigList{})
}
