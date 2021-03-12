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

package workspace

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/pkg/errors"
	"github.com/spf13/afero"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/event"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/crossplane/crossplane-runtime/pkg/ratelimiter"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/pkg/resource"

	"github.com/negz/provider-terraform/apis/v1alpha1"
	"github.com/negz/provider-terraform/internal/terraform"
)

const (
	errNotWorkspace = "managed resource is not a Workspace custom resource"
	errTrackPCUsage = "cannot track ProviderConfig usage"
	errGetPC        = "cannot get ProviderConfig"
	errGetCreds     = "cannot get credentials"

	errMkdir      = "cannot make Terraform configuration directory"
	errWriteCreds = "cannot write Terraform credentials"
	errWriteMain  = "cannot write Terraform configuration " + tfMain
	errInit       = "cannot initialize Terraform configuration"
	errWorkspace  = "cannot select Terraform workspace"
	errResources  = "cannot list Terraform resources"
	errOutputs    = "cannot list Terraform outputs"
	errOptions    = "cannot determine Terraform options"
	errApply      = "cannot apply Terraform configuration"
	errDestroy    = "cannot apply Terraform configuration"
	errVarFile    = "cannot get tfvars"
)

const (
	// TODO(negz): Make the Terraform binary path and work dir configurable.
	tfPath  = "terraform"
	tfDir   = "/tf"
	tfCreds = "credentials"
	tfMain  = "main.tf"
)

type tfclient interface {
	Init(ctx context.Context, o ...terraform.InitOption) error
	Workspace(ctx context.Context, name string) error
	Outputs(ctx context.Context) ([]terraform.Output, error)
	Resources(ctx context.Context) ([]string, error)
	Apply(ctx context.Context, o ...terraform.Option) error
	Destroy(ctx context.Context, o ...terraform.Option) error
}

// Setup adds a controller that reconciles Workspace managed resources.
func Setup(mgr ctrl.Manager, l logging.Logger, rl workqueue.RateLimiter) error {
	name := managed.ControllerName(v1alpha1.WorkspaceGroupKind)

	o := controller.Options{
		RateLimiter: ratelimiter.NewDefaultManagedRateLimiter(rl),
	}

	c := &connector{
		kube:      mgr.GetClient(),
		usage:     resource.NewProviderConfigUsageTracker(mgr.GetClient(), &v1alpha1.ProviderConfigUsage{}),
		fs:        afero.Afero{Fs: afero.NewOsFs()},
		terraform: func(dir string) tfclient { return terraform.Harness{Path: tfPath, Dir: dir} },
	}

	r := managed.NewReconciler(mgr,
		resource.ManagedKind(v1alpha1.WorkspaceGroupVersionKind),
		managed.WithExternalConnecter(c),
		managed.WithLogger(l.WithValues("controller", name)),
		managed.WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorderFor(name))),
		managed.WithTimeout(20*time.Minute)) // Terraform likes to block.

	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		WithOptions(o).
		For(&v1alpha1.Workspace{}).
		Complete(r)
}

type connector struct {
	kube  client.Client
	usage resource.Tracker

	fs        afero.Afero
	terraform func(dir string) tfclient
}

func (c *connector) Connect(ctx context.Context, mg resource.Managed) (managed.ExternalClient, error) {
	cr, ok := mg.(*v1alpha1.Workspace)
	if !ok {
		return nil, errors.New(errNotWorkspace)
	}

	// TODO(negz): Garbage collect this directory.
	dir := filepath.Join(tfDir, string(cr.GetUID()))
	if err := c.fs.MkdirAll(dir, 0600); resource.Ignore(os.IsExist, err) != nil {
		return nil, errors.Wrap(err, errMkdir)
	}

	if err := c.usage.Track(ctx, mg); err != nil {
		return nil, errors.Wrap(err, errTrackPCUsage)
	}

	pc := &v1alpha1.ProviderConfig{}
	if err := c.kube.Get(ctx, types.NamespacedName{Name: cr.GetProviderConfigReference().Name}, pc); err != nil {
		return nil, errors.Wrap(err, errGetPC)
	}

	cd := pc.Spec.Credentials
	data, err := resource.CommonCredentialExtractor(ctx, cd.Source, c.kube, cd.CommonCredentialSelectors)
	if err != nil {
		return nil, errors.Wrap(err, errGetCreds)
	}

	if err := c.fs.WriteFile(filepath.Join(dir, tfCreds), data, 0600); err != nil {
		return nil, errors.Wrap(err, errWriteCreds)
	}

	io := []terraform.InitOption{terraform.FromModule(cr.Spec.ForProvider.Module)}
	if cr.Spec.ForProvider.Source == v1alpha1.ModuleSourceInline {
		if err := c.fs.WriteFile(filepath.Join(dir, tfMain), []byte(cr.Spec.ForProvider.Module), 0600); err != nil {
			return nil, errors.Wrap(err, errWriteMain)
		}
		io = nil
	}

	tf := c.terraform(dir)
	if err := tf.Init(ctx, io...); err != nil {
		return nil, errors.Wrap(err, errInit)
	}

	return &external{tf: tf, kube: c.kube}, errors.Wrap(tf.Workspace(ctx, meta.GetExternalName(cr)), errWorkspace)
}

type external struct {
	tf   tfclient
	kube client.Reader
}

func (c *external) Observe(ctx context.Context, _ resource.Managed) (managed.ExternalObservation, error) {
	r, err := c.tf.Resources(ctx)
	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, errResources)
	}

	// TODO(negz): Include any non-sensitive outputs in our status?
	o, err := c.tf.Outputs(ctx)
	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, errOutputs)
	}

	// TODO(negz): Is there any value in running terraform plan to determine
	// whether the workspace is up-to-date? Presumably running a no-op apply is
	// about the same as running a plan.
	return managed.ExternalObservation{
		ResourceExists:          len(r) > 0,
		ResourceUpToDate:        false,
		ResourceLateInitialized: false,
		ConnectionDetails:       op2cd(o),
	}, nil
}

func (c *external) Create(ctx context.Context, mg resource.Managed) (managed.ExternalCreation, error) {
	// Terraform does not have distinct 'create' and 'update' operations.
	u, err := c.Update(ctx, mg)
	return managed.ExternalCreation{ConnectionDetails: u.ConnectionDetails}, err
}

func (c *external) Update(ctx context.Context, mg resource.Managed) (managed.ExternalUpdate, error) {
	cr, ok := mg.(*v1alpha1.Workspace)
	if !ok {
		return managed.ExternalUpdate{}, errors.New(errNotWorkspace)
	}

	o, err := c.options(ctx, cr.Spec.ForProvider)
	if err != nil {
		return managed.ExternalUpdate{}, errors.Wrap(err, errOptions)
	}

	if err := c.tf.Apply(ctx, o...); err != nil {
		return managed.ExternalUpdate{}, errors.Wrap(err, errApply)
	}

	op, err := c.tf.Outputs(ctx)
	if err != nil {
		return managed.ExternalUpdate{}, errors.Wrap(err, errOutputs)
	}

	// TODO(negz): Allow Workspaces to optionally derive their readiness from an
	// output - similar to the logic XRs use to derive readiness from a field of
	// a composed resource.
	mg.SetConditions(xpv1.Available())
	return managed.ExternalUpdate{ConnectionDetails: op2cd(op)}, nil
}

func (c *external) Delete(ctx context.Context, mg resource.Managed) error {
	cr, ok := mg.(*v1alpha1.Workspace)
	if !ok {
		return errors.New(errNotWorkspace)
	}

	o, err := c.options(ctx, cr.Spec.ForProvider)
	if err != nil {
		return errors.Wrap(err, errOptions)
	}

	return errors.Wrap(c.tf.Destroy(ctx, o...), errDestroy)
}

func (c *external) options(ctx context.Context, p v1alpha1.WorkspaceParameters) ([]terraform.Option, error) {
	o := make([]terraform.Option, 0, len(p.Vars)+len(p.VarFiles))

	for _, v := range p.Vars {
		o = append(o, terraform.WithVar(v.Key, v.Value))
	}

	for _, vf := range p.VarFiles {
		fmt := terraform.HCL
		if vf.Format == v1alpha1.VarFileFormatJSON {
			fmt = terraform.JSON
		}

		switch vf.Source {
		case v1alpha1.VarFileSourceConfigMapKey:
			cm := &corev1.ConfigMap{}
			r := vf.ConfigMapKeyReference
			nn := types.NamespacedName{Namespace: r.Namespace, Name: r.Name}
			if err := c.kube.Get(ctx, nn, cm); err != nil {
				return nil, errors.Wrap(err, errVarFile)
			}
			o = append(o, terraform.WithVarFile(cm.BinaryData[r.Key], fmt))

		case v1alpha1.VarFileSourceSecretKey:
			s := &corev1.Secret{}
			r := vf.SecretKeyReference
			nn := types.NamespacedName{Namespace: r.Namespace, Name: r.Name}
			if err := c.kube.Get(ctx, nn, s); err != nil {
				return nil, errors.Wrap(err, errVarFile)
			}
			o = append(o, terraform.WithVarFile(s.Data[r.Key], fmt))
		}
	}

	return o, nil
}

func op2cd(o []terraform.Output) managed.ConnectionDetails {
	cd := managed.ConnectionDetails{}
	for _, op := range o {
		if op.Type == terraform.OutputTypeString {
			cd[op.Name] = []byte(op.StringValue())
			continue
		}
		if j, err := op.JSONValue(); err == nil {
			cd[op.Name] = j
		}
	}
	return cd
}
