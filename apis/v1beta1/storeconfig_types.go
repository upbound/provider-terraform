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

package v1beta1

import (
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
)

// A StoreConfigSpec defines the desired state of a ProviderConfig.
type StoreConfigSpec struct {
	xpv1.SecretStoreConfig `json:",inline"`
}

// A StoreConfigStatus represents the status of a StoreConfig.
type StoreConfigStatus struct {
	xpv1.ConditionedStatus `json:",inline"`
}

// GetCondition of this StoreConfigStatus.
func (s *StoreConfigStatus) GetCondition(ct xpv1.ConditionType) xpv1.Condition {
	return s.ConditionedStatus.GetCondition(ct)
}

// SetConditions of this StoreConfigStatus.
func (s *StoreConfigStatus) SetConditions(c ...xpv1.Condition) {
	s.ConditionedStatus.SetConditions(c...)
}

// +kubebuilder:object:root=true

// A StoreConfig configures how GCP controller should store connection details.
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="TYPE",type="string",JSONPath=".spec.type"
// +kubebuilder:printcolumn:name="DEFAULT-SCOPE",type="string",JSONPath=".spec.defaultScope"
// +kubebuilder:resource:scope=Cluster,categories={crossplane,store,gcp}
// +kubebuilder:subresource:status
type StoreConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   StoreConfigSpec   `json:"spec"`
	Status StoreConfigStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// StoreConfigList contains a list of StoreConfig
type StoreConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []StoreConfig `json:"items"`
}

// Note(turkenh): To be generated with AngryJet

// GetStoreConfig returns SecretStoreConfig
func (in *StoreConfig) GetStoreConfig() xpv1.SecretStoreConfig {
	return in.Spec.SecretStoreConfig
}

// GetCondition of this StoreConfig.
func (in *StoreConfig) GetCondition(ct xpv1.ConditionType) xpv1.Condition {
	return in.Status.GetCondition(ct)
}

// SetConditions of this StoreConfig.
func (in *StoreConfig) SetConditions(c ...xpv1.Condition) {
	in.Status.SetConditions(c...)
}

// StoreConfig type metadata.
var (
	StoreConfigKind             = reflect.TypeOf(StoreConfig{}).Name()
	StoreConfigGroupKind        = schema.GroupKind{Group: Group, Kind: StoreConfigKind}.String()
	StoreConfigKindAPIVersion   = StoreConfigKind + "." + SchemeGroupVersion.String()
	StoreConfigGroupVersionKind = SchemeGroupVersion.WithKind(StoreConfigKind)
)

func init() {
	SchemeBuilder.Register(&StoreConfig{}, &StoreConfigList{})
}
