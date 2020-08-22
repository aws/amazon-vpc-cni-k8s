package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ENIConfigList is the ENI config list
type ENIConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []ENIConfig `json:"items"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ENIConfig is the per ENI config
type ENIConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	Spec              ENIConfigSpec   `json:"spec"`
	Status            ENIConfigStatus `json:"status,omitempty"`
}

// ENIConfigSpec is the spec for this ENI
type ENIConfigSpec struct {
	SecurityGroups []string `json:"securityGroups"`
	Subnet         string   `json:"subnet"`
}

// ENIConfigStatus is empty
type ENIConfigStatus struct {
	// Fill me
}
