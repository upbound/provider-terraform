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

	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"
)

// TODO: Remove these temporary interfaces once crossplane-runtime supports them
// natively. These interfaces enable unit testing by providing mockable Track
// functions for both legacy and modern managed resources. They are temporary
// workarounds pending the merge of https://github.com/crossplane/crossplane-runtime/pull/862,
// after which we should migrate to the upstream implementations.

// A LegacyTracker tracks legacy managed resources.
type LegacyTracker interface {
	// Track the supplied legacy managed resource.
	Track(ctx context.Context, mg resource.LegacyManaged) error
}

// A LegacyTrackerFn is a function that tracks managed resources.
type LegacyTrackerFn func(ctx context.Context, mg resource.LegacyManaged) error

// Track the supplied legacy managed resource.
func (fn LegacyTrackerFn) Track(ctx context.Context, mg resource.LegacyManaged) error {
	return fn(ctx, mg)
}

// A ModernTracker tracks modern managed resources.
type ModernTracker interface {
	// Track the supplied modern managed resource.
	Track(ctx context.Context, mg resource.ModernManaged) error
}

// A ModernTrackerFn is a function that tracks managed resources.
type ModernTrackerFn func(ctx context.Context, mg resource.ModernManaged) error

// Track the supplied managed resource.
func (fn ModernTrackerFn) Track(ctx context.Context, mg resource.ModernManaged) error {
	return fn(ctx, mg)
}
