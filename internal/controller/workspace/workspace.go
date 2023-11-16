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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/pkg/errors"
	"github.com/spf13/afero"
	corev1 "k8s.io/api/core/v1"
	extensionsV1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/connection"
	"github.com/crossplane/crossplane-runtime/pkg/controller"
	"github.com/crossplane/crossplane-runtime/pkg/event"
	"github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/crossplane/crossplane-runtime/pkg/ratelimiter"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/pkg/resource"

	"github.com/hashicorp/go-getter"

	"github.com/upbound/provider-terraform/apis/v1beta1"
	"github.com/upbound/provider-terraform/internal/controller/features"
	"github.com/upbound/provider-terraform/internal/terraform"
	"github.com/upbound/provider-terraform/internal/workdir"
)

const (
	errNotWorkspace = "managed resource is not a Workspace custom resource"
	errTrackPCUsage = "cannot track ProviderConfig usage"
	errGetPC        = "cannot get ProviderConfig"
	errGetCreds     = "cannot get credentials"

	errMkdir           = "cannot make Terraform configuration directory"
	errRemoteModule    = "cannot get remote Terraform module"
	errSetGitCredDir   = "cannot set GIT_CRED_DIR environment variable"
	errWriteCreds      = "cannot write Terraform credentials"
	errWriteGitCreds   = "cannot write .git-credentials to /tmp dir"
	errWriteConfig     = "cannot write Terraform configuration " + tfConfig
	errWriteMain       = "cannot write Terraform configuration " + tfMain
	errInit            = "cannot initialize Terraform configuration"
	errWorkspace       = "cannot select Terraform workspace"
	errResources       = "cannot list Terraform resources"
	errDiff            = "cannot diff (i.e. plan) Terraform configuration"
	errOutputs         = "cannot list Terraform outputs"
	errOptions         = "cannot determine Terraform options"
	errApply           = "cannot apply Terraform configuration"
	errDestroy         = "cannot destroy Terraform configuration"
	errVarFile         = "cannot get tfvars"
	errVarMap          = "cannot get tfvars from var map"
	errVarResolution   = "cannot resolve variables"
	errDeleteWorkspace = "cannot delete Terraform workspace"
	errChecksum        = "cannot calculate workspace checksum"

	gitCredentialsFilename = ".git-credentials"
)

const (
	// TODO(negz): Make the Terraform binary path and work dir configurable.
	tfPath   = "terraform"
	tfMain   = "main.tf"
	tfConfig = "crossplane-provider-config.tf"
)

func envVarFallback(envvar string, fallback string) string {
	if value, ok := os.LookupEnv(envvar); ok {
		return value
	}
	return fallback
}

var tfDir = envVarFallback("XP_TF_DIR", "/tf")

type tfclient interface {
	Init(ctx context.Context, cache bool, o ...terraform.InitOption) error
	Workspace(ctx context.Context, name string) error
	Outputs(ctx context.Context) ([]terraform.Output, error)
	Resources(ctx context.Context) ([]string, error)
	Diff(ctx context.Context, o ...terraform.Option) (bool, error)
	Apply(ctx context.Context, o ...terraform.Option) error
	Destroy(ctx context.Context, o ...terraform.Option) error
	DeleteCurrentWorkspace(ctx context.Context) error
	GenerateChecksum(ctx context.Context) (string, error)
}

// Setup adds a controller that reconciles Workspace managed resources.
func Setup(mgr ctrl.Manager, o controller.Options, timeout, pollJitter time.Duration) error {
	name := managed.ControllerName(v1beta1.WorkspaceGroupKind)

	fs := afero.Afero{Fs: afero.NewOsFs()}
	gcWorkspace := workdir.NewGarbageCollector(mgr.GetClient(), tfDir, workdir.WithFs(fs), workdir.WithLogger(o.Logger))
	go gcWorkspace.Run(context.TODO())

	gcTmp := workdir.NewGarbageCollector(mgr.GetClient(), filepath.Join("/tmp", tfDir), workdir.WithFs(fs), workdir.WithLogger(o.Logger))
	go gcTmp.Run(context.TODO())

	cps := []managed.ConnectionPublisher{managed.NewAPISecretPublisher(mgr.GetClient(), mgr.GetScheme())}
	if o.Features.Enabled(features.EnableAlphaExternalSecretStores) {
		cps = append(cps, connection.NewDetailsManager(mgr.GetClient(), v1beta1.StoreConfigGroupVersionKind))
	}
	c := &connector{
		kube:   mgr.GetClient(),
		usage:  resource.NewProviderConfigUsageTracker(mgr.GetClient(), &v1beta1.ProviderConfigUsage{}),
		logger: o.Logger,
		fs:     fs,
		terraform: func(dir string, envs ...string) tfclient {
			return terraform.Harness{Path: tfPath, Dir: dir, Envs: envs}
		},
	}

	r := managed.NewReconciler(mgr,
		resource.ManagedKind(v1beta1.WorkspaceGroupVersionKind),
		managed.WithPollInterval(o.PollInterval),
		managed.WithPollJitterHook(pollJitter),
		managed.WithExternalConnecter(c),
		managed.WithLogger(o.Logger.WithValues("controller", name)),
		managed.WithRecorder(event.NewAPIRecorder(mgr.GetEventRecorderFor(name))),
		managed.WithTimeout(timeout),
		managed.WithConnectionPublishers(cps...))

	return ctrl.NewControllerManagedBy(mgr).
		Named(name).
		WithOptions(o.ForControllerRuntime()).
		WithEventFilter(resource.DesiredStateChanged()).
		For(&v1beta1.Workspace{}).
		Complete(ratelimiter.NewReconciler(name, r, o.GlobalRateLimiter))
}

type connector struct {
	kube      client.Client
	usage     resource.Tracker
	logger    logging.Logger
	fs        afero.Afero
	terraform func(dir string, envs ...string) tfclient
}

func (c *connector) Connect(ctx context.Context, mg resource.Managed) (managed.ExternalClient, error) { //nolint:gocyclo
	// NOTE(negz): This method is slightly over our complexity goal, but I
	// can't immediately think of a clean way to decompose it without
	// affecting readability.

	cr, ok := mg.(*v1beta1.Workspace)
	if !ok {
		return nil, errors.New(errNotWorkspace)
	}
	l := c.logger.WithValues("request", cr.Name)
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

	pc := &v1beta1.ProviderConfig{}
	if err := c.kube.Get(ctx, types.NamespacedName{Name: cr.GetProviderConfigReference().Name}, pc); err != nil {
		return nil, errors.Wrap(err, errGetPC)
	}

	// Make git credentials available to inline and remote sources
	for _, cd := range pc.Spec.Credentials {
		if cd.Filename != gitCredentialsFilename {
			continue
		}
		data, err := resource.CommonCredentialExtractor(ctx, cd.Source, c.kube, cd.CommonCredentialSelectors)
		if err != nil {
			return nil, errors.Wrap(err, errGetCreds)
		}
		// NOTE(bobh66): Put the git credentials file in /tmp/tf/<UUID> so it doesn't get removed or overwritten
		// by the remote module source case
		gitCredDir := filepath.Clean(filepath.Join("/tmp", dir))
		if err = c.fs.MkdirAll(gitCredDir, 0700); err != nil {
			return nil, errors.Wrap(err, errWriteGitCreds)
		}

		// NOTE(ytsarev): Make go-getter pick up .git-credentials, see /.gitconfig in the container image
		err = os.Setenv("GIT_CRED_DIR", gitCredDir)
		if err != nil {
			return nil, errors.Wrap(err, errSetGitCredDir)
		}
		p := filepath.Clean(filepath.Join(gitCredDir, filepath.Base(cd.Filename)))
		if err := c.fs.WriteFile(p, data, 0600); err != nil {
			return nil, errors.Wrap(err, errWriteGitCreds)
		}
	}

	switch cr.Spec.ForProvider.Source {
	case v1beta1.ModuleSourceRemote:
		// Workaround of https://github.com/hashicorp/go-getter/issues/114
		if err := c.fs.RemoveAll(dir); err != nil {
			return nil, errors.Wrap(err, errRemoteModule)
		}

		gc := getter.Client{
			Src: cr.Spec.ForProvider.Module,
			Dst: dir,
			Pwd: dir,

			Mode: getter.ClientModeDir,
		}
		err := gc.Get()
		if err != nil {
			return nil, errors.Wrap(err, errRemoteModule)
		}

	case v1beta1.ModuleSourceInline:
		if err := c.fs.WriteFile(filepath.Join(dir, tfMain), []byte(cr.Spec.ForProvider.Module), 0600); err != nil {
			return nil, errors.Wrap(err, errWriteMain)
		}
	}

	if len(cr.Spec.ForProvider.Entrypoint) > 0 {
		entrypoint := strings.ReplaceAll(cr.Spec.ForProvider.Entrypoint, "../", "")
		dir = filepath.Join(dir, entrypoint)
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

	// NOTE(ytsarev): user tf provider cache mechanism to speed up
	// reconciliation, see https://developer.hashicorp.com/terraform/cli/config/config-file#provider-plugin-cache
	if pc.Spec.PluginCache == nil {
		pc.Spec.PluginCache = new(bool)
		*pc.Spec.PluginCache = true
	}

	envs := make([]string, len(cr.Spec.ForProvider.Env))
	for idx, env := range cr.Spec.ForProvider.Env {
		runtimeVal := env.Value
		if runtimeVal == "" {
			switch {
			case env.ConfigMapKeyReference != nil:
				cm := &corev1.ConfigMap{}
				r := env.ConfigMapKeyReference
				nn := types.NamespacedName{Namespace: r.Namespace, Name: r.Name}
				if err := c.kube.Get(ctx, nn, cm); err != nil {
					return nil, errors.Wrap(err, errVarResolution)
				}
				runtimeVal, ok = cm.Data[r.Key]
				if !ok {
					return nil, errors.Wrap(fmt.Errorf("couldn't find key %v in ConfigMap %v/%v", r.Key, r.Namespace, r.Name), errVarResolution)
				}
			case env.SecretKeyReference != nil:
				s := &corev1.Secret{}
				r := env.SecretKeyReference
				nn := types.NamespacedName{Namespace: r.Namespace, Name: r.Name}
				if err := c.kube.Get(ctx, nn, s); err != nil {
					return nil, errors.Wrap(err, errVarResolution)
				}
				secretBytes, ok := s.Data[r.Key]
				if !ok {
					return nil, errors.Wrap(fmt.Errorf("couldn't find key %v in Secret %v/%v", r.Key, r.Namespace, r.Name), errVarResolution)
				}
				runtimeVal = string(secretBytes)
			}
		}
		envs[idx] = strings.Join([]string{env.Name, runtimeVal}, "=")
	}

	tf := c.terraform(dir, envs...)
	if cr.Status.AtProvider.Checksum != "" {
		checksum, err := tf.GenerateChecksum(ctx)
		if err != nil {
			return nil, errors.Wrap(err, errChecksum)
		}
		if cr.Status.AtProvider.Checksum == checksum {
			l.Debug("Checksums match - skip running terraform init")
			return &external{tf: tf, kube: c.kube, logger: c.logger}, errors.Wrap(tf.Workspace(ctx, meta.GetExternalName(cr)), errWorkspace)
		}
		l.Debug("Checksums don't match so run terraform init:", "old", cr.Status.AtProvider.Checksum, "new", checksum)
	}

	o := make([]terraform.InitOption, 0, len(cr.Spec.ForProvider.InitArgs))
	o = append(o, terraform.WithInitArgs(cr.Spec.ForProvider.InitArgs))
	if err := tf.Init(ctx, *pc.Spec.PluginCache, o...); err != nil {
		return nil, errors.Wrap(err, errInit)
	}
	return &external{tf: tf, kube: c.kube}, errors.Wrap(tf.Workspace(ctx, meta.GetExternalName(cr)), errWorkspace)
}

type external struct {
	tf     tfclient
	kube   client.Client
	logger logging.Logger
}

func (c *external) checkDiff(ctx context.Context, cr *v1beta1.Workspace) (bool, error) {
	o, err := c.options(ctx, cr.Spec.ForProvider)
	if err != nil {
		return false, errors.Wrap(err, errOptions)
	}

	o = append(o, terraform.WithArgs(cr.Spec.ForProvider.PlanArgs))
	differs, err := c.tf.Diff(ctx, o...)
	if err != nil {
		if !meta.WasDeleted(cr) {
			return false, errors.Wrap(err, errDiff)
		}
		// terraform plan can fail on deleted resources, so let the reconciliation loop
		// call Delete() if there are still resources in the tfstate file
		differs = false
	}
	return differs, nil
}

func (c *external) Observe(ctx context.Context, mg resource.Managed) (managed.ExternalObservation, error) {
	cr, ok := mg.(*v1beta1.Workspace)
	if !ok {
		return managed.ExternalObservation{}, errors.New(errNotWorkspace)
	}

	differs, err := c.checkDiff(ctx, cr)
	if err != nil {
		return managed.ExternalObservation{}, err
	}
	r, err := c.tf.Resources(ctx)
	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, errResources)
	}
	if meta.WasDeleted(cr) && len(r) == 0 {
		// The CR was deleted and there are no more terraform resources so the workspace can be deleted
		if err = c.tf.DeleteCurrentWorkspace(ctx); err != nil {
			return managed.ExternalObservation{}, errors.Wrap(err, errDeleteWorkspace)
		}
	}
	// Include any non-sensitive outputs in our status
	op, err := c.tf.Outputs(ctx)

	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, errOutputs)
	}
	cr.Status.AtProvider = generateWorkspaceObservation(op)

	checksum, err := c.tf.GenerateChecksum(ctx)
	if err != nil {
		return managed.ExternalObservation{}, errors.Wrap(err, errChecksum)
	}
	cr.Status.AtProvider.Checksum = checksum

	if !differs {
		// TODO(negz): Allow Workspaces to optionally derive their readiness from an
		// output - similar to the logic XRs use to derive readiness from a field of
		// a composed resource.
		cr.Status.SetConditions(xpv1.Available())
	}

	return managed.ExternalObservation{
		ResourceExists:          len(r)+len(op) > 0,
		ResourceUpToDate:        !differs,
		ResourceLateInitialized: false,
		ConnectionDetails:       op2cd(op),
	}, nil
}

func (c *external) Create(ctx context.Context, mg resource.Managed) (managed.ExternalCreation, error) {
	// Terraform does not have distinct 'create' and 'update' operations.
	u, err := c.Update(ctx, mg)
	return managed.ExternalCreation(u), err
}

func (c *external) Update(ctx context.Context, mg resource.Managed) (managed.ExternalUpdate, error) {
	cr, ok := mg.(*v1beta1.Workspace)
	if !ok {
		return managed.ExternalUpdate{}, errors.New(errNotWorkspace)
	}

	o, err := c.options(ctx, cr.Spec.ForProvider)
	if err != nil {
		return managed.ExternalUpdate{}, errors.Wrap(err, errOptions)
	}

	o = append(o, terraform.WithArgs(cr.Spec.ForProvider.ApplyArgs))
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
	// Note that since Create() calls this function the Reconciler will overwrite this Available condition with Creating
	// on the first pass and it will get reset to Available() by Observe() on the next pass if there are no differences.
	// Leave this call for the Update() case.
	cr.Status.SetConditions(xpv1.Available())
	return managed.ExternalUpdate{ConnectionDetails: op2cd(op)}, nil
}

func (c *external) Delete(ctx context.Context, mg resource.Managed) error {
	cr, ok := mg.(*v1beta1.Workspace)
	if !ok {
		return errors.New(errNotWorkspace)
	}

	o, err := c.options(ctx, cr.Spec.ForProvider)
	if err != nil {
		return errors.Wrap(err, errOptions)
	}

	o = append(o, terraform.WithArgs(cr.Spec.ForProvider.DestroyArgs))
	return errors.Wrap(c.tf.Destroy(ctx, o...), errDestroy)
}

//nolint:gocyclo
func (c *external) options(ctx context.Context, p v1beta1.WorkspaceParameters) ([]terraform.Option, error) {
	o := make([]terraform.Option, 0, len(p.Vars)+len(p.VarFiles)+len(p.DestroyArgs)+len(p.ApplyArgs)+len(p.PlanArgs))

	for _, v := range p.Vars {
		o = append(o, terraform.WithVar(v.Key, v.Value))
	}

	for _, vf := range p.VarFiles {
		fmt := terraform.HCL
		if vf.Format != nil && *vf.Format == v1beta1.VarFileFormatJSON {
			fmt = terraform.JSON
		}

		switch vf.Source {
		case v1beta1.VarFileSourceConfigMapKey:
			cm := &corev1.ConfigMap{}
			r := vf.ConfigMapKeyReference
			nn := types.NamespacedName{Namespace: r.Namespace, Name: r.Name}
			if err := c.kube.Get(ctx, nn, cm); err != nil {
				return nil, errors.Wrap(err, errVarFile)
			}
			o = append(o, terraform.WithVarFile([]byte(cm.Data[r.Key]), fmt))

		case v1beta1.VarFileSourceSecretKey:
			s := &corev1.Secret{}
			r := vf.SecretKeyReference
			nn := types.NamespacedName{Namespace: r.Namespace, Name: r.Name}
			if err := c.kube.Get(ctx, nn, s); err != nil {
				return nil, errors.Wrap(err, errVarFile)
			}
			o = append(o, terraform.WithVarFile(s.Data[r.Key], fmt))
		}
	}

	if p.VarMap != nil {
		jsonBytes, err := json.Marshal(p.VarMap)
		if err != nil {
			return nil, errors.Wrap(err, errVarMap)
		}
		o = append(o, terraform.WithVarFile(jsonBytes, terraform.JSON))
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

// generateWorkspaceObservation is used to produce v1beta1.WorkspaceObservation from
// workspace_type.Workspace.
func generateWorkspaceObservation(op []terraform.Output) v1beta1.WorkspaceObservation {
	wo := v1beta1.WorkspaceObservation{
		Outputs: make(map[string]extensionsV1.JSON, len(op)),
	}
	for _, o := range op {
		if !o.Sensitive {
			if j, err := o.JSONValue(); err == nil {
				wo.Outputs[o.Name] = extensionsV1.JSON{Raw: j}
			}
		}
	}
	return wo
}
