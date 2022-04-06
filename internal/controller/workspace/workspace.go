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

	"github.com/crossplane-contrib/provider-terraform/apis/v1alpha1"
	"github.com/crossplane-contrib/provider-terraform/internal/terraform"
	"github.com/crossplane-contrib/provider-terraform/internal/workdir"
	getter "github.com/hashicorp/go-getter"
)

const (
	errNotWorkspace = "managed resource is not a Workspace custom resource"
	errTrackPCUsage = "cannot track ProviderConfig usage"
	errGetPC        = "cannot get ProviderConfig"
	errGetCreds     = "cannot get credentials"

	errMkdir         = "cannot make Terraform configuration directory"
	errRemoteModule  = "cannot get remote Terraform module"
	errWriteCreds    = "cannot write Terraform credentials"
	errWriteGitCreds = "cannot write .git-credentials to /tmp dir"
	errWriteConfig   = "cannot write Terraform configuration " + tfConfig
	errWriteMain     = "cannot write Terraform configuration " + tfMain
	errInit          = "cannot initialize Terraform configuration"
	errWorkspace     = "cannot select Terraform workspace"
	errResources     = "cannot list Terraform resources"
	errDiff          = "cannot diff (i.e. plan) Terraform configuration"
	errOutputs       = "cannot list Terraform outputs"
	errOptions       = "cannot determine Terraform options"
	errApply         = "cannot apply Terraform configuration"
	errDestroy       = "cannot apply Terraform configuration"
	errVarFile       = "cannot get tfvars"

	gitCredentialsFilename = ".git-credentials"
)

const (
	// TODO(negz): Make the Terraform binary path and work dir configurable.
	tfPath   = "terraform"
	tfDir    = "/tf"
	tfMain   = "main.tf"
	tfConfig = "crossplane-provider-config.tf"
)

type tfclient interface {
	Init(ctx context.Context, o ...terraform.InitOption) error
	Workspace(ctx context.Context, name string) error
	Outputs(ctx context.Context) ([]terraform.Output, error)
	Resources(ctx context.Context) ([]string, error)
	Diff(ctx context.Context, o ...terraform.Option) (bool, error)
	Apply(ctx context.Context, o ...terraform.Option) error
	Destroy(ctx context.Context, o ...terraform.Option) error
}

// Setup adds a controller that reconciles Workspace managed resources.
func Setup(mgr ctrl.Manager, l logging.Logger, rl workqueue.RateLimiter, poll, timeout time.Duration) error {
	name := managed.ControllerName(v1alpha1.WorkspaceGroupKind)

	o := controller.Options{
		RateLimiter: ratelimiter.NewDefaultManagedRateLimiter(rl),
	}

	fs := afero.Afero{Fs: afero.NewOsFs()}
	gcWorkspace := workdir.NewGarbageCollector(mgr.GetClient(), tfDir, workdir.WithFs(fs), workdir.WithLogger(l))
	go gcWorkspace.Run(context.TODO())

	gcTmp := workdir.NewGarbageCollector(mgr.GetClient(), filepath.Join("/tmp", tfDir), workdir.WithFs(fs), workdir.WithLogger(l))
	go gcTmp.Run(context.TODO())

	c := &connector{
		kube:      mgr.GetClient(),
		usage:     resource.NewProviderConfigUsageTracker(mgr.GetClient(), &v1alpha1.ProviderConfigUsage{}),
		fs:        fs,
		terraform: func(dir string) tfclient { return terraform.Harness{Path: tfPath, Dir: dir} },
	}

	r := managed.NewReconciler(mgr,
		resource.ManagedKind(v1alpha1.WorkspaceGroupVersionKind),
		managed.WithExternalConnecter(c),
		managed.WithPollInterval(poll),
		managed.WithLogger(l.WithValues("controller", name)),
		managed.WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorderFor(name))),
		managed.WithTimeout(timeout))

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

func (c *connector) Connect(ctx context.Context, mg resource.Managed) (managed.ExternalClient, error) { //nolint:gocyclo
	// NOTE(negz): This method is slightly over our complexity goal, but I
	// can't immediately think of a clean way to decompose it without
	// affecting readability.

	cr, ok := mg.(*v1alpha1.Workspace)
	if !ok {
		return nil, errors.New(errNotWorkspace)
	}

	// NOTE(negz): This directory will be garbage collected by the workdir
	// garbage collector that is started in Setup.
	dir := filepath.Join(tfDir, string(cr.GetUID()))
	if err := c.fs.MkdirAll(dir, 0700); resource.Ignore(os.IsExist, err) != nil {
		return nil, errors.Wrap(err, errMkdir)
	}
	if err := c.fs.MkdirAll(filepath.Join("/tmp", tfDir), 0700); resource.Ignore(os.IsExist, err) != nil {
		return nil, errors.Wrap(err, errMkdir)
	}

	if err := c.usage.Track(ctx, mg); err != nil {
		return nil, errors.Wrap(err, errTrackPCUsage)
	}

	pc := &v1alpha1.ProviderConfig{}
	if err := c.kube.Get(ctx, types.NamespacedName{Name: cr.GetProviderConfigReference().Name}, pc); err != nil {
		return nil, errors.Wrap(err, errGetPC)
	}

	switch cr.Spec.ForProvider.Source {
	case v1alpha1.ModuleSourceRemote:
		// NOTE(ytsarev): Retrieve .git-credentials from Spec to /tmp outside of workspace directory
		gitCredDir := filepath.Clean(filepath.Join("/tmp", dir))
		if err := c.fs.MkdirAll(gitCredDir, 0700); err != nil {
			return nil, errors.Wrap(err, errWriteGitCreds)
		}
		for _, cd := range pc.Spec.Credentials {
			if cd.Filename != gitCredentialsFilename {
				continue
			}
			data, err := resource.CommonCredentialExtractor(ctx, cd.Source, c.kube, cd.CommonCredentialSelectors)
			if err != nil {
				return nil, errors.Wrap(err, errGetCreds)
			}
			p := filepath.Clean(filepath.Join(gitCredDir, filepath.Base(cd.Filename)))
			if err := c.fs.WriteFile(p, data, 0600); err != nil {
				return nil, errors.Wrap(err, errWriteGitCreds)
			}
			// NOTE(ytsarev): Make go-getter pick up .git-credentials, see /.gitconfig in the container image
			err = os.Setenv("GIT_CRED_DIR", gitCredDir)
			if err != nil {
				return nil, errors.Wrap(err, errRemoteModule)
			}
		}

		// Workaround of https://github.com/hashicorp/go-getter/issues/114
		if err := c.fs.RemoveAll(dir); err != nil {
			return nil, errors.Wrap(err, errRemoteModule)
		}

		client := getter.Client{
			Src: cr.Spec.ForProvider.Module,
			Dst: dir,
			Pwd: dir,

			Mode: getter.ClientModeDir,
		}
		err := client.Get()
		if err != nil {
			return nil, errors.Wrap(err, errRemoteModule)
		}
	case v1alpha1.ModuleSourceInline:
		if err := c.fs.WriteFile(filepath.Join(dir, tfMain), []byte(cr.Spec.ForProvider.Module), 0600); err != nil {
			return nil, errors.Wrap(err, errWriteMain)
		}
	}

	for _, cd := range pc.Spec.Credentials {
		data, err := resource.CommonCredentialExtractor(ctx, cd.Source, c.kube, cd.CommonCredentialSelectors)
		if err != nil {
			return nil, errors.Wrap(err, errGetCreds)
		}
		p := filepath.Clean(filepath.Join(dir, filepath.Base(cd.Filename)))
		if err := c.fs.WriteFile(p, data, 0600); err != nil {
			return nil, errors.Wrap(err, errWriteCreds)
		}
	}

	if pc.Spec.Configuration != nil {
		if err := c.fs.WriteFile(filepath.Join(dir, tfConfig), []byte(*pc.Spec.Configuration), 0600); err != nil {
			return nil, errors.Wrap(err, errWriteConfig)
		}
	}

	tf := c.terraform(dir)
	if err := tf.Init(ctx); err != nil {
		return nil, errors.Wrap(err, errInit)
	}
	// TODO(ytsarev): cache .terraform in /tmp to speed up `terraform init` on next reconcile

	return &external{tf: tf, kube: c.kube}, errors.Wrap(tf.Workspace(ctx, meta.GetExternalName(cr)), errWorkspace)
}

type external struct {
	tf   tfclient
	kube client.Reader
}

func (c *external) Observe(ctx context.Context, mg resource.Managed) (managed.ExternalObservation, error) {
	cr, ok := mg.(*v1alpha1.Workspace)
	if !ok {
		return managed.ExternalObservation{}, errors.New(errNotWorkspace)
	}

	o, err := c.options(ctx, cr.Spec.ForProvider)
	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, errOptions)
	}

	differs, err := c.tf.Diff(ctx, o...)
	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, errDiff)
	}

	r, err := c.tf.Resources(ctx)
	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, errResources)
	}

	// Include any non-sensitive outputs in our status
	op, err := c.tf.Outputs(ctx)

	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, errOutputs)
	}
	cr.Status.AtProvider = generateWorkspaceObservation(op)

	return managed.ExternalObservation{
		ResourceExists:          len(r) > 0,
		ResourceUpToDate:        !differs,
		ResourceLateInitialized: false,
		ConnectionDetails:       op2cd(op),
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
	cr.Status.AtProvider = generateWorkspaceObservation(op)
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
		if vf.Format == &v1alpha1.VarFileFormatJSON {
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
			o = append(o, terraform.WithVarFile([]byte(cm.Data[r.Key]), fmt))

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

// generateWorkspaceObservation is used to produce v1alpha1.WorkspaceObservation from
// workspace_type.Workspace.
func generateWorkspaceObservation(op []terraform.Output) v1alpha1.WorkspaceObservation {
	wo := v1alpha1.WorkspaceObservation{
		Outputs: make(map[string]string, len(op)),
	}
	for _, o := range op {
		if !o.Sensitive {
			if o.Type == terraform.OutputTypeString {
				wo.Outputs[o.Name] = o.StringValue()
			} else if j, err := o.JSONValue(); err == nil {
				wo.Outputs[o.Name] = string(j)
			}
		}
	}
	return wo
}
