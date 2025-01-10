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

package config

import (
	"context"
	"time"

	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/crossplane/crossplane-runtime/pkg/controller"
	"github.com/crossplane/crossplane-runtime/pkg/event"
	"github.com/crossplane/crossplane-runtime/pkg/ratelimiter"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/providerconfig"
	"github.com/crossplane/crossplane-runtime/pkg/resource"

	"github.com/upbound/provider-terraform/apis/v1beta1"
	"github.com/upbound/provider-terraform/internal/controller/identity"
	"github.com/upbound/provider-terraform/internal/utils"
)

const (
	errGetPC = "cannot get ProviderConfig"
)

type shardingReconciler struct {
	client     client.Client
	reconciler *providerconfig.Reconciler
	identity   identity.Identity
	logger     logging.Logger
}

// Setup adds a controller that reconciles ProviderConfigs by accounting for
// their current usage.
func Setup(mgr ctrl.Manager, id identity.Identity, o controller.Options, timeout time.Duration) error {
	name := providerconfig.ControllerName(v1beta1.ProviderConfigGroupKind)

	of := resource.ProviderConfigKinds{
		Config:    v1beta1.ProviderConfigGroupVersionKind,
		UsageList: v1beta1.ProviderConfigUsageListGroupVersionKind,
	}

	r := providerconfig.NewReconciler(mgr, of,
		providerconfig.WithLogger(o.Logger.WithValues("controller", name)),
		providerconfig.WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorderFor(name))))

	sr := &shardingReconciler{
		client:     mgr.GetClient(),
		reconciler: r,
		identity:   id,
		logger:     o.Logger,
	}

	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		WithOptions(o.ForControllerRuntime()).
		For(&v1beta1.ProviderConfig{}).
		Watches(&v1beta1.ProviderConfigUsage{}, &resource.EnqueueRequestForProviderConfig{}).
		Complete(ratelimiter.NewReconciler(name, sr, o.GlobalRateLimiter))
}

func (r *shardingReconciler) Reconcile(ctx context.Context, req reconcile.Request) (ctrl.Result, error) {
	if r.identity != nil {
		r.logger.Debug("ProviderConfig sharding enabled")

		pc := &v1beta1.ProviderConfig{}
		if err := r.client.Get(ctx, req.NamespacedName, pc); err != nil {
			return reconcile.Result{}, errors.Wrap(resource.IgnoreNotFound(err), errGetPC)
		}

		if r.identity.GetIndex() < 0 || r.identity.GetReplicas() < 1 {
			r.logger.Debug("Skipping providerconfig reconciliation", "reason", "invalid index or replicas", "index", r.identity.GetIndex(), "replicas", r.identity.GetReplicas())
			return reconcile.Result{RequeueAfter: 30 * time.Second}, nil
		}

		if utils.HashAndModulo(string(pc.GetUID()), r.identity.GetReplicas()) != r.identity.GetIndex() {
			r.logger.Debug("Skipping providerconfig reconciliation", "reason", "not managed by this reconciler", "index", r.identity.GetIndex(), "replicas", r.identity.GetReplicas())
			return reconcile.Result{}, nil
		}

		r.logger.Debug("Processing providerconfig reconciliation", "reason", "managed by this reconciler", "index", r.identity.GetIndex(), "replicas", r.identity.GetReplicas())
	}

	return r.reconciler.Reconcile(ctx, req)
}
