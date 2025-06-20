/*
Copyright 2018 The Kubernetes Authors.

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

package pkg

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ConfigReloadSpec defines the desired state of ConfigReload.
type ConfigReloadSpec struct {
	// +kubebuilder:validation:Required
	DeploymentRef DeploymentRef `json:"deploymentRef"`

	// +kubebuilder:validation:Required
	ConfigMapRef ConfigMapRef `json:"configmapRef"`
}

type DeploymentRef struct {
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// +optional
	Namespace string `json:"namespace,omitempty"`
}

type ConfigMapRef struct {
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// ConfigReloadStatus defines the observed state of ConfigReload.
type ConfigReloadStatus struct {
	// Indicate the last time the Deployment was rolled out.
	// +optional
	LastRolloutTime metav1.Time `json:"lastRolloutTime,omitempty"`

	// Indicate the last time the ConfigMap was updated.
	// +optional
	LastConfigMapTime metav1.Time `json:"lastConfigMapVersion,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="last rollout",type="string",JSONPath=".spec.lastRolloutTime",format="date"
// +kubebuilder:printcolumn:name="last configMap update",type="string",JSONPath=".status.LastConlastConfigMapVersionfigMapTime",format="date"
type ConfigReload struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ConfigReloadSpec   `json:"spec,omitempty"`
	Status ConfigReloadStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ConfigReloadList contains a list of ConfigReload
type ConfigReloadList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ConfigReload `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ConfigReload{}, &ConfigReloadList{})
}
