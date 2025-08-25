/*
Copyright 2025 The Crossplane Authors.

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

package clients

import (
	"context"
	"encoding/json"

	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1beta1 "github.com/upbound/provider-terraform/apis/cluster/v1beta1"
	namespacedv1beta1 "github.com/upbound/provider-terraform/apis/namespaced/v1beta1"
)

const (
	errProviderConfigNotSet = "provider config is not set"
	errGetProviderConfig    = "cannot get provider config"
	errFailedToTrackUsage   = "cannot track provider config usage"
)

func ResolveProviderConfig(ctx context.Context, crClient client.Client, lt LegacyTracker, mt ModernTracker, mg resource.Managed) (*namespacedv1beta1.ClusterProviderConfig, error) {
	switch managed := mg.(type) {
	case resource.LegacyManaged:
		return resolveProviderConfigLegacy(ctx, crClient, managed, lt)
	case resource.ModernManaged:
		return resolveProviderConfigModern(ctx, crClient, managed, mt)
	default:
		return nil, errors.New("resource is not a managed")
	}
}

func resolveProviderConfigLegacy(ctx context.Context, client client.Client, mg resource.LegacyManaged, lt LegacyTracker) (*namespacedv1beta1.ClusterProviderConfig, error) {
	configRef := mg.GetProviderConfigReference()
	if configRef == nil {
		return nil, errors.New(errProviderConfigNotSet)
	}
	pc := &clusterv1beta1.ProviderConfig{}
	if err := client.Get(ctx, types.NamespacedName{Name: configRef.Name}, pc); err != nil {
		return nil, errors.Wrap(err, errGetProviderConfig)
	}

	if err := lt.Track(ctx, mg); err != nil {
		return nil, errors.Wrap(err, errFailedToTrackUsage)
	}

	return legacyToModernProviderConfigSpec(pc)
}

func resolveProviderConfigModern(ctx context.Context, crClient client.Client, mg resource.ModernManaged, mt ModernTracker) (*namespacedv1beta1.ClusterProviderConfig, error) {
	configRef := mg.GetProviderConfigReference()
	if configRef == nil {
		return nil, errors.New(errProviderConfigNotSet)
	}

	pcRuntimeObj, err := crClient.Scheme().New(namespacedv1beta1.SchemeGroupVersion.WithKind(configRef.Kind))
	if err != nil {
		return nil, errors.Wrapf(err, "referenced provider config kind %q is invalid for %s/%s", configRef.Kind, mg.GetNamespace(), mg.GetName())
	}
	pcObj, ok := pcRuntimeObj.(resource.ProviderConfig)
	if !ok {
		return nil, errors.Errorf("referenced provider config kind %q is not a provider config type %s/%s", configRef.Kind, mg.GetNamespace(), mg.GetName())
	}

	// Namespace will be ignored if the PC is a cluster-scoped type
	if err := crClient.Get(ctx, types.NamespacedName{Name: configRef.Name, Namespace: mg.GetNamespace()}, pcObj); err != nil {
		return nil, errors.Wrap(err, errGetProviderConfig)
	}

	var effectivePC *namespacedv1beta1.ClusterProviderConfig
	switch pc := pcObj.(type) {
	case *namespacedv1beta1.ProviderConfig:
		enrichLocalSecretRefs(pc, mg)
		effectivePC = &namespacedv1beta1.ClusterProviderConfig{
			TypeMeta: metav1.TypeMeta{
				APIVersion: namespacedv1beta1.SchemeGroupVersion.String(),
				Kind:       namespacedv1beta1.ClusterProviderConfigKind,
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        pc.GetName(),
				Labels:      pc.GetLabels(),
				Annotations: pc.GetAnnotations(),
			},
			Spec: pc.Spec,
		}
	case *namespacedv1beta1.ClusterProviderConfig:
		// noop
		effectivePC = pc
	default:
		return nil, errors.New("unknown")
	}

	if err := mt.Track(ctx, mg); err != nil {
		return nil, errors.Wrap(err, errFailedToTrackUsage)
	}
	return effectivePC, nil
}

func legacyToModernProviderConfigSpec(pc *clusterv1beta1.ProviderConfig) (*namespacedv1beta1.ClusterProviderConfig, error) {
	// TODO(erhan): this is hacky and potentially lossy, generate or manually implement
	if pc == nil {
		return nil, nil
	}
	data, err := json.Marshal(pc)
	if err != nil {
		return nil, err
	}

	var mSpec namespacedv1beta1.ClusterProviderConfig
	err = json.Unmarshal(data, &mSpec)
	mSpec.TypeMeta.Kind = namespacedv1beta1.ClusterProviderConfigKind
	mSpec.TypeMeta.APIVersion = namespacedv1beta1.SchemeGroupVersion.String()
	mSpec.ObjectMeta = metav1.ObjectMeta{
		Name:        pc.GetName(),
		Labels:      pc.GetLabels(),
		Annotations: pc.GetAnnotations(),
	}
	return &mSpec, err
}

func enrichLocalSecretRefs(pc *namespacedv1beta1.ProviderConfig, mg resource.Managed) {
	if pc != nil {
		if pc.Spec.Credentials != nil {
			for _, v := range pc.Spec.Credentials {
				if v.SecretRef != nil {
					v.SecretRef.Namespace = mg.GetNamespace()
				}
			}
		}
	}
}
