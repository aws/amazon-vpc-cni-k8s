package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type ENIConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []ENIConfig `json:"items"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type ENIConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	Spec              ENIConfigSpec   `json:"spec"`
	Status            ENIConfigStatus `json:"status,omitempty"`
}

type ENIConfigSpec struct {
	// Fill me
	SecurityGroups []string `json:"securitygroups"`
	Subnet         string   `json: "subnet"`
}
type ENIConfigStatus struct {
	// Fill me
}
