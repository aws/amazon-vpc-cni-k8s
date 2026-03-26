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

// ConnectionTrackingSpec configures connection tracking timeouts for ENIs created from this config.
type ConnectionTrackingSpec struct {
	// TcpEstablishedTimeout is the idle timeout (seconds) for established TCP connections.
	// Min: 60, Max: 432000 (5 days). Default: 432000.
	TcpEstablishedTimeout *int32 `json:"tcpEstablishedTimeout,omitempty"`
	// UdpStreamTimeout is the idle timeout (seconds) for UDP flows with more than one
	// request-response transaction. Min: 60, Max: 180. Default: 180.
	UdpStreamTimeout *int32 `json:"udpStreamTimeout,omitempty"`
	// UdpTimeout is the idle timeout (seconds) for UDP flows with a single
	// request-response transaction. Min: 30, Max: 60. Default: 30.
	UdpTimeout *int32 `json:"udpTimeout,omitempty"`
}

// ENIConfigSpec defines the desired state of ENIConfig
type ENIConfigSpec struct {
	SecurityGroups         []string                `json:"securityGroups"`
	Subnet                 string                  `json:"subnet"`
	ConnectionTrackingSpec *ConnectionTrackingSpec `json:"connectionTrackingSpec,omitempty"`
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
