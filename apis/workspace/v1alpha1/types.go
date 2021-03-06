/*
Copyright 2020 The Crossplane Authors.

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

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
)

// A Var represents a Terraform configuration variable.
type Var struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// A VarFileSource specifies the source of a Terraform var file.
type VarFileSource string

// Var file sources.
const (
	ConfigMapKey VarFileSource = "ConfigMapKey"
	SecretKey    VarFileSource = "SecretKey"
)

// A VarFile is a file containing many Terraform variables.
type VarFile struct {
	// Source of this var file.
	Source VarFileSource `json:"source"`

	// A ConfigMap key containing the var file.
	// +optional
	ConfigMapKeyReference *KeyReference `json:"configMapKeyRef,omitempty"`

	// A Secret key containing the var file.
	// +optional
	SecretKeyReference *KeyReference `json:"secretKeyRef,omitempty"`

	// TODO(negz): Does Terraform autodetect JSON var files, or do we need to
	// indicate the type?
}

// A KeyReference references a key within a Secret or a ConfigMap.
type KeyReference struct {
	// Namespace of the referenced resource.
	Namespace string `json:"namespace"`

	// Name of the referenced resource.
	Name string `json:"name"`

	// Key within the referenced resource.
	Key string `json:"key"`
}

// WorkspaceParameters are the configurable fields of a Workspace.
type WorkspaceParameters struct {
	// Configuration of this workspace; i.e. the path to the directory that
	// contains the main.tf file of the Terraform configuration's root module.
	// Can be a git repository, GCS bucket, or S3 bucket.
	Configuration string `json:"configuration"`

	// Configuration variables.
	// +optional
	Vars []Var `json:"vars,omitempty"`

	// Files of configuration variables. Explicitly declared vars take
	// precedence.
	// +optional
	VarFiles []VarFile `json:"varFiles,omitempty"`
}

// WorkspaceObservation are the observable fields of a Workspace.
type WorkspaceObservation struct {
	// TODO(negz): Should we include outputs here? Or only in connection
	// details.
}

// A WorkspaceSpec defines the desired state of a Workspace.
type WorkspaceSpec struct {
	xpv1.ResourceSpec `json:",inline"`
	ForProvider       WorkspaceParameters `json:"forProvider"`
}

// A WorkspaceStatus represents the observed state of a Workspace.
type WorkspaceStatus struct {
	xpv1.ResourceStatus `json:",inline"`
	AtProvider          WorkspaceObservation `json:"atProvider,omitempty"`
}

// +kubebuilder:object:root=true

// A Workspace of Terraform Configuration.
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="STATUS",type="string",JSONPath=".status.bindingPhase"
// +kubebuilder:printcolumn:name="STATE",type="string",JSONPath=".status.atProvider.state"
// +kubebuilder:printcolumn:name="CLASS",type="string",JSONPath=".spec.classRef.name"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:scope=Cluster
type Workspace struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   WorkspaceSpec   `json:"spec"`
	Status WorkspaceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// WorkspaceList contains a list of Workspace
type WorkspaceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Workspace `json:"items"`
}
