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

package controller

import (
	"time"

	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/crossplane/crossplane-runtime/pkg/controller"

	"github.com/upbound/provider-terraform/internal/controller/config"
	"github.com/upbound/provider-terraform/internal/controller/features"
	"github.com/upbound/provider-terraform/internal/controller/identity"
	"github.com/upbound/provider-terraform/internal/controller/workspace"
)

// Setup creates all terraform controllers with the supplied options and adds
// them to the supplied manager.
func Setup(mgr ctrl.Manager, o controller.Options, timeout, pollJitter time.Duration) error {
	var id identity.Identity
	if o.Features.Enabled(features.EnableAlphaWorkspaceSharding) {
		if i, err := identity.Setup(mgr, o); err != nil {
			return err
		} else {
			id = i
		}
	}

	if err := config.Setup(mgr, id, o, timeout); err != nil {
		return err
	}
	if err := workspace.Setup(mgr, id, o, timeout, pollJitter); err != nil {
		return err
	}
	return nil
}
