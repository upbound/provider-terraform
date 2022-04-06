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
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
	"github.com/spf13/afero"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/crossplane/crossplane-runtime/pkg/test"

	"github.com/crossplane-contrib/provider-terraform/apis/v1alpha1"
	"github.com/crossplane-contrib/provider-terraform/internal/terraform"
)

type ErrFs struct {
	afero.Fs

	errs map[string]error
}

func (e *ErrFs) MkdirAll(path string, perm os.FileMode) error {
	if err := e.errs[path]; err != nil {
		return err
	}
	return e.Fs.MkdirAll(path, perm)
}

// Called by afero.WriteFile
func (e *ErrFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if err := e.errs[name]; err != nil {
		return nil, err
	}
	return e.Fs.OpenFile(name, flag, perm)
}

type MockTf struct {
	MockInit      func(ctx context.Context, o ...terraform.InitOption) error
	MockWorkspace func(ctx context.Context, name string) error
	MockOutputs   func(ctx context.Context) ([]terraform.Output, error)
	MockResources func(ctx context.Context) ([]string, error)
	MockDiff      func(ctx context.Context, o ...terraform.Option) (bool, error)
	MockApply     func(ctx context.Context, o ...terraform.Option) error
	MockDestroy   func(ctx context.Context, o ...terraform.Option) error
}

func (tf *MockTf) Init(ctx context.Context, o ...terraform.InitOption) error {
	return tf.MockInit(ctx, o...)
}

func (tf *MockTf) Workspace(ctx context.Context, name string) error {
	return tf.MockWorkspace(ctx, name)
}

func (tf *MockTf) Outputs(ctx context.Context) ([]terraform.Output, error) {
	return tf.MockOutputs(ctx)
}

func (tf *MockTf) Resources(ctx context.Context) ([]string, error) {
	return tf.MockResources(ctx)
}

func (tf *MockTf) Diff(ctx context.Context, o ...terraform.Option) (bool, error) {
	return tf.MockDiff(ctx, o...)
}

func (tf *MockTf) Apply(ctx context.Context, o ...terraform.Option) error {
	return tf.MockApply(ctx, o...)
}

func (tf *MockTf) Destroy(ctx context.Context, o ...terraform.Option) error {
	return tf.MockDestroy(ctx, o...)
}

func TestConnect(t *testing.T) {
	errBoom := errors.New("boom")
	uid := types.UID("no-you-id")
	tfCreds := "credentials"

	type fields struct {
		kube      client.Client
		usage     resource.Tracker
		fs        afero.Afero
		terraform func(dir string) tfclient
	}

	type args struct {
		ctx context.Context
		mg  resource.Managed
	}

	cases := map[string]struct {
		reason string
		fields fields
		args   args
		want   error
	}{
		"NotWorkSpaceError": {
			reason: "We should return an error if the supplied managed resource is not a Workspace",
			fields: fields{},
			args: args{
				mg: nil,
			},
			want: errors.New(errNotWorkspace),
		},
		"MakeDirError": {
			reason: "We should return any error encountered while making a directory for our configuration",
			fields: fields{
				fs: afero.Afero{
					Fs: &ErrFs{
						Fs:   afero.NewMemMapFs(),
						errs: map[string]error{filepath.Join(tfDir, string(uid)): errBoom},
					},
				},
			},
			args: args{
				mg: &v1alpha1.Workspace{
					ObjectMeta: metav1.ObjectMeta{UID: uid},
				},
			},
			want: errors.Wrap(errBoom, errMkdir),
		},
		"TrackUsageError": {
			reason: "We should return any error encountered while tracking ProviderConfig usage",
			fields: fields{
				usage: resource.TrackerFn(func(_ context.Context, _ resource.Managed) error { return errBoom }),
				fs:    afero.Afero{Fs: afero.NewMemMapFs()},
			},
			args: args{
				mg: &v1alpha1.Workspace{
					ObjectMeta: metav1.ObjectMeta{UID: uid},
				},
			},
			want: errors.Wrap(errBoom, errTrackPCUsage),
		},
		"GetProviderConfigError": {
			reason: "We should return any error encountered while getting our ProviderConfig",
			fields: fields{
				kube: &test.MockClient{
					MockGet: test.NewMockGetFn(errBoom),
				},
				usage: resource.TrackerFn(func(_ context.Context, _ resource.Managed) error { return nil }),
				fs:    afero.Afero{Fs: afero.NewMemMapFs()},
			},
			args: args{
				mg: &v1alpha1.Workspace{
					ObjectMeta: metav1.ObjectMeta{UID: uid},
					Spec: v1alpha1.WorkspaceSpec{
						ResourceSpec: xpv1.ResourceSpec{
							ProviderConfigReference: &xpv1.Reference{},
						},
					},
				},
			},
			want: errors.Wrap(errBoom, errGetPC),
		},
		"GetProviderConfigCredentialsError": {
			reason: "We should return any error encountered while getting our ProviderConfig credentials",
			fields: fields{
				kube: &test.MockClient{
					MockGet: test.NewMockGetFn(nil, func(obj client.Object) error {
						if pc, ok := obj.(*v1alpha1.ProviderConfig); ok {
							// We're testing through CommonCredentialsExtractor
							// here. We cause an error to be returned by asking
							// for credentials from the environment, but not
							// specifying an environment variable.
							pc.Spec.Credentials = []v1alpha1.ProviderCredentials{{
								Source: xpv1.CredentialsSourceEnvironment,
							}}
						}
						return nil
					}),
				},
				usage: resource.TrackerFn(func(_ context.Context, _ resource.Managed) error { return nil }),
				fs:    afero.Afero{Fs: afero.NewMemMapFs()},
				terraform: func(_ string) tfclient {
					return &MockTf{
						MockInit: func(ctx context.Context, o ...terraform.InitOption) error { return nil },
					}
				},
			},
			args: args{
				mg: &v1alpha1.Workspace{
					ObjectMeta: metav1.ObjectMeta{UID: uid},
					Spec: v1alpha1.WorkspaceSpec{
						ResourceSpec: xpv1.ResourceSpec{
							ProviderConfigReference: &xpv1.Reference{},
						},
					},
				},
			},
			want: errors.Wrap(errors.New("cannot extract from environment variable when none specified"), errGetCreds),
		},
		"WriteProviderConfigCredentialsError": {
			reason: "We should return any error encountered while writing our ProviderConfig credentials to a file",
			fields: fields{
				kube: &test.MockClient{
					MockGet: test.NewMockGetFn(nil, func(obj client.Object) error {
						if pc, ok := obj.(*v1alpha1.ProviderConfig); ok {
							pc.Spec.Credentials = []v1alpha1.ProviderCredentials{{
								Filename: tfCreds,
								Source:   xpv1.CredentialsSourceNone,
							}}
						}
						return nil
					}),
				},
				usage: resource.TrackerFn(func(_ context.Context, _ resource.Managed) error { return nil }),
				fs: afero.Afero{
					Fs: &ErrFs{
						Fs:   afero.NewMemMapFs(),
						errs: map[string]error{filepath.Join(tfDir, string(uid), tfCreds): errBoom},
					},
				},
				terraform: func(_ string) tfclient {
					return &MockTf{
						MockInit: func(ctx context.Context, o ...terraform.InitOption) error { return nil },
					}
				},
			},
			args: args{
				mg: &v1alpha1.Workspace{
					ObjectMeta: metav1.ObjectMeta{UID: uid},
					Spec: v1alpha1.WorkspaceSpec{
						ResourceSpec: xpv1.ResourceSpec{
							ProviderConfigReference: &xpv1.Reference{},
						},
					},
				},
			},
			want: errors.Wrap(errBoom, errWriteCreds),
		},
		"WriteProviderGitCredentialsError": {
			reason: "We should return any error encountered while writing our git credentials to a file",
			fields: fields{
				kube: &test.MockClient{
					MockGet: test.NewMockGetFn(nil, func(obj client.Object) error {
						if pc, ok := obj.(*v1alpha1.ProviderConfig); ok {
							pc.Spec.Credentials = []v1alpha1.ProviderCredentials{{
								Filename: ".git-credentials",
								Source:   xpv1.CredentialsSourceNone,
							}}
						}
						return nil
					}),
				},
				usage: resource.TrackerFn(func(_ context.Context, _ resource.Managed) error { return nil }),
				fs: afero.Afero{
					Fs: &ErrFs{
						Fs:   afero.NewMemMapFs(),
						errs: map[string]error{filepath.Join("/tmp", tfDir, string(uid), ".git-credentials"): errBoom},
					},
				},
				terraform: func(_ string) tfclient {
					return &MockTf{
						MockInit: func(ctx context.Context, o ...terraform.InitOption) error { return nil },
					}
				},
			},
			args: args{
				mg: &v1alpha1.Workspace{
					ObjectMeta: metav1.ObjectMeta{UID: uid},
					Spec: v1alpha1.WorkspaceSpec{
						ResourceSpec: xpv1.ResourceSpec{
							ProviderConfigReference: &xpv1.Reference{},
						},
						ForProvider: v1alpha1.WorkspaceParameters{
							Module: "github.com/crossplane/rocks",
							Source: v1alpha1.ModuleSourceRemote,
						},
					},
				},
			},
			want: errors.Wrap(errBoom, errWriteGitCreds),
		},
		"WriteConfigError": {
			reason: "We should return any error encountered while writing our crossplane-provider-config.tf file",
			fields: fields{
				kube: &test.MockClient{
					MockGet: test.NewMockGetFn(nil, func(obj client.Object) error {
						if pc, ok := obj.(*v1alpha1.ProviderConfig); ok {
							cfg := "I'm HCL!"
							pc.Spec.Configuration = &cfg
						}
						return nil
					}),
				},
				usage: resource.TrackerFn(func(_ context.Context, _ resource.Managed) error { return nil }),
				fs: afero.Afero{
					Fs: &ErrFs{
						Fs:   afero.NewMemMapFs(),
						errs: map[string]error{filepath.Join(tfDir, string(uid), tfConfig): errBoom},
					},
				},
				terraform: func(_ string) tfclient {
					return &MockTf{
						MockInit: func(ctx context.Context, o ...terraform.InitOption) error { return nil },
					}
				},
			},
			args: args{
				mg: &v1alpha1.Workspace{
					ObjectMeta: metav1.ObjectMeta{UID: uid},
					Spec: v1alpha1.WorkspaceSpec{
						ResourceSpec: xpv1.ResourceSpec{
							ProviderConfigReference: &xpv1.Reference{},
						},
						ForProvider: v1alpha1.WorkspaceParameters{
							Module: "I'm HCL!",
							Source: v1alpha1.ModuleSourceInline,
						},
					},
				},
			},
			want: errors.Wrap(errBoom, errWriteConfig),
		},
		"WriteMainError": {
			reason: "We should return any error encountered while writing our main.tf file",
			fields: fields{
				kube: &test.MockClient{
					MockGet: test.NewMockGetFn(nil),
				},
				usage: resource.TrackerFn(func(_ context.Context, _ resource.Managed) error { return nil }),
				fs: afero.Afero{
					Fs: &ErrFs{
						Fs:   afero.NewMemMapFs(),
						errs: map[string]error{filepath.Join(tfDir, string(uid), tfMain): errBoom},
					},
				},
				terraform: func(_ string) tfclient {
					return &MockTf{
						MockInit: func(ctx context.Context, o ...terraform.InitOption) error { return nil },
					}
				},
			},
			args: args{
				mg: &v1alpha1.Workspace{
					ObjectMeta: metav1.ObjectMeta{UID: uid},
					Spec: v1alpha1.WorkspaceSpec{
						ResourceSpec: xpv1.ResourceSpec{
							ProviderConfigReference: &xpv1.Reference{},
						},
						ForProvider: v1alpha1.WorkspaceParameters{
							Module: "I'm HCL!",
							Source: v1alpha1.ModuleSourceInline,
						},
					},
				},
			},
			want: errors.Wrap(errBoom, errWriteMain),
		},
		"TerraformInitError": {
			reason: "We should return any error encountered while initializing Terraform",
			fields: fields{
				kube: &test.MockClient{
					MockGet: test.NewMockGetFn(nil),
				},
				usage: resource.TrackerFn(func(_ context.Context, _ resource.Managed) error { return nil }),
				fs:    afero.Afero{Fs: afero.NewMemMapFs()},
				terraform: func(_ string) tfclient {
					return &MockTf{MockInit: func(_ context.Context, _ ...terraform.InitOption) error { return errBoom }}
				},
			},
			args: args{
				mg: &v1alpha1.Workspace{
					ObjectMeta: metav1.ObjectMeta{UID: uid},
					Spec: v1alpha1.WorkspaceSpec{
						ResourceSpec: xpv1.ResourceSpec{
							ProviderConfigReference: &xpv1.Reference{},
						},
					},
				},
			},
			want: errors.Wrap(errBoom, errInit),
		},
		"TerraformWorkspaceError": {
			reason: "We should return any error encountered while selecting a Terraform workspace",
			fields: fields{
				kube: &test.MockClient{
					MockGet: test.NewMockGetFn(nil),
				},
				usage: resource.TrackerFn(func(_ context.Context, _ resource.Managed) error { return nil }),
				fs:    afero.Afero{Fs: afero.NewMemMapFs()},
				terraform: func(_ string) tfclient {
					return &MockTf{
						MockInit:      func(ctx context.Context, o ...terraform.InitOption) error { return nil },
						MockWorkspace: func(_ context.Context, _ string) error { return errBoom },
					}
				},
			},
			args: args{
				mg: &v1alpha1.Workspace{
					ObjectMeta: metav1.ObjectMeta{UID: uid},
					Spec: v1alpha1.WorkspaceSpec{
						ResourceSpec: xpv1.ResourceSpec{
							ProviderConfigReference: &xpv1.Reference{},
						},
					},
				},
			},
			want: errors.Wrap(errBoom, errWorkspace),
		},
		"Success": {
			reason: "We should not return an error when we successfully 'connect' to Terraform",
			fields: fields{
				kube: &test.MockClient{
					MockGet: test.NewMockGetFn(nil),
				},
				usage: resource.TrackerFn(func(_ context.Context, _ resource.Managed) error { return nil }),
				fs:    afero.Afero{Fs: afero.NewMemMapFs()},
				terraform: func(_ string) tfclient {
					return &MockTf{
						MockInit:      func(ctx context.Context, o ...terraform.InitOption) error { return nil },
						MockWorkspace: func(_ context.Context, _ string) error { return nil },
					}
				},
			},
			args: args{
				mg: &v1alpha1.Workspace{
					ObjectMeta: metav1.ObjectMeta{UID: uid},
					Spec: v1alpha1.WorkspaceSpec{
						ResourceSpec: xpv1.ResourceSpec{
							ProviderConfigReference: &xpv1.Reference{},
						},
					},
				},
			},
			want: nil,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := connector{
				kube:      tc.fields.kube,
				usage:     tc.fields.usage,
				fs:        tc.fields.fs,
				terraform: tc.fields.terraform,
			}
			_, err := c.Connect(tc.args.ctx, tc.args.mg)
			if diff := cmp.Diff(tc.want, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\ne.Connect(...): -want error, +got error:\n%s\n", tc.reason, diff)
			}
		})
	}
}

func TestObserve(t *testing.T) {
	errBoom := errors.New("boom")

	type fields struct {
		tf   tfclient
		kube client.Reader
	}

	type args struct {
		ctx context.Context
		mg  resource.Managed
	}

	type want struct {
		o   managed.ExternalObservation
		wo  v1alpha1.WorkspaceObservation
		err error
	}

	cases := map[string]struct {
		reason string
		fields fields
		args   args
		want   want
	}{
		"NotAWorkspaceError": {
			reason: "We should return an error if the supplied managed resource is not a Workspace",
			args: args{
				mg: nil,
			},
			want: want{
				err: errors.New(errNotWorkspace),
			},
		},
		"GetConfigMapError": {
			reason: "We should return any error we encounter getting tfvars from a ConfigMap",
			fields: fields{
				kube: &test.MockClient{
					MockGet: test.NewMockGetFn(nil, func(obj client.Object) error {
						if _, ok := obj.(*corev1.ConfigMap); ok {
							return errBoom
						}
						return nil
					}),
				},
			},
			args: args{
				mg: &v1alpha1.Workspace{
					Spec: v1alpha1.WorkspaceSpec{
						ForProvider: v1alpha1.WorkspaceParameters{
							VarFiles: []v1alpha1.VarFile{
								{
									Source:                v1alpha1.VarFileSourceConfigMapKey,
									ConfigMapKeyReference: &v1alpha1.KeyReference{},
								},
							},
						},
					},
				},
			},
			want: want{
				err: errors.Wrap(errors.Wrap(errBoom, errVarFile), errOptions),
			},
		},
		"GetSecretError": {
			reason: "We should return any error we encounter getting tfvars from a Secret",
			fields: fields{
				kube: &test.MockClient{
					MockGet: test.NewMockGetFn(nil, func(obj client.Object) error {
						if _, ok := obj.(*corev1.Secret); ok {
							return errBoom
						}
						return nil
					}),
				},
			},
			args: args{
				mg: &v1alpha1.Workspace{
					Spec: v1alpha1.WorkspaceSpec{
						ForProvider: v1alpha1.WorkspaceParameters{
							VarFiles: []v1alpha1.VarFile{
								{
									Source:             v1alpha1.VarFileSourceSecretKey,
									SecretKeyReference: &v1alpha1.KeyReference{},
								},
							},
						},
					},
				},
			},
			want: want{
				err: errors.Wrap(errors.Wrap(errBoom, errVarFile), errOptions),
			},
		},
		"DiffError": {
			reason: "We should return any error encountered while diffing the Terraform configuration",
			fields: fields{
				tf: &MockTf{
					MockDiff: func(ctx context.Context, o ...terraform.Option) (bool, error) { return false, errBoom },
				},
			},
			args: args{
				mg: &v1alpha1.Workspace{},
			},
			want: want{
				err: errors.Wrap(errBoom, errDiff),
			},
		},
		"ResourcesError": {
			reason: "We should return any error encountered while listing extant Terraform resources",
			fields: fields{
				tf: &MockTf{
					MockDiff:      func(ctx context.Context, o ...terraform.Option) (bool, error) { return false, nil },
					MockResources: func(ctx context.Context) ([]string, error) { return nil, errBoom },
				},
			},
			args: args{
				mg: &v1alpha1.Workspace{},
			},
			want: want{
				err: errors.Wrap(errBoom, errResources),
			},
		},
		"OutputsError": {
			reason: "We should return any error encountered while listing Terraform outputs",
			fields: fields{
				tf: &MockTf{
					MockDiff:      func(ctx context.Context, o ...terraform.Option) (bool, error) { return false, nil },
					MockResources: func(ctx context.Context) ([]string, error) { return nil, nil },
					MockOutputs:   func(ctx context.Context) ([]terraform.Output, error) { return nil, errBoom },
				},
			},
			args: args{
				mg: &v1alpha1.Workspace{},
			},
			want: want{
				err: errors.Wrap(errBoom, errOutputs),
			},
		},
		"WorkspaceDoesNotExist": {
			reason: "A workspace with zero resources should be considered to be non-existent",
			fields: fields{
				tf: &MockTf{
					MockDiff:      func(ctx context.Context, o ...terraform.Option) (bool, error) { return false, nil },
					MockResources: func(ctx context.Context) ([]string, error) { return []string{}, nil },
					MockOutputs:   func(ctx context.Context) ([]terraform.Output, error) { return nil, nil },
				},
			},
			args: args{
				mg: &v1alpha1.Workspace{},
			},
			want: want{
				o: managed.ExternalObservation{
					ResourceExists:    false,
					ResourceUpToDate:  true,
					ConnectionDetails: managed.ConnectionDetails{},
				},
				wo: v1alpha1.WorkspaceObservation{
					Outputs: map[string]string{},
				},
			},
		},
		"WorkspaceExists": {
			reason: "A workspace with resources should return its outputs as connection details",
			fields: fields{
				tf: &MockTf{
					MockDiff: func(ctx context.Context, o ...terraform.Option) (bool, error) { return false, nil },
					MockResources: func(ctx context.Context) ([]string, error) {
						return []string{"cool_resource.very"}, nil
					},
					MockOutputs: func(ctx context.Context) ([]terraform.Output, error) {
						return []terraform.Output{
							{Name: "string", Type: terraform.OutputTypeString, Sensitive: false},
							{Name: "object", Type: terraform.OutputTypeObject, Sensitive: true},
						}, nil
					},
				},
			},
			args: args{
				mg: &v1alpha1.Workspace{},
			},
			want: want{
				o: managed.ExternalObservation{
					ResourceExists:   true,
					ResourceUpToDate: true,
					ConnectionDetails: managed.ConnectionDetails{
						"string": {},
						"object": []byte("null"), // Because we JSON decode the the value, which is interface{}{}
					},
				},
				wo: v1alpha1.WorkspaceObservation{
					Outputs: map[string]string{
						"string": "",
					},
				},
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := external{tf: tc.fields.tf, kube: tc.fields.kube}
			got, err := e.Observe(tc.args.ctx, tc.args.mg)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\ne.Observe(...): -want error, +got error:\n%s\n", tc.reason, diff)
			}
			if diff := cmp.Diff(tc.want.o, got); diff != "" {
				t.Errorf("\n%s\ne.Observe(...): -want, +got:\n%s\n", tc.reason, diff)
			}
			if tc.args.mg != nil {
				if diff := cmp.Diff(tc.want.wo, tc.args.mg.(*v1alpha1.Workspace).Status.AtProvider); diff != "" {
					t.Errorf("\n%s\ne.Observe(...): -want, +got:\n%s\n", tc.reason, diff)
				}
			}
		})
	}
}

func TestCreate(t *testing.T) {
	errBoom := errors.New("boom")

	type fields struct {
		tf   tfclient
		kube client.Reader
	}

	type args struct {
		ctx context.Context
		mg  resource.Managed
	}

	type want struct {
		c   managed.ExternalCreation
		wo  v1alpha1.WorkspaceObservation
		err error
	}

	cases := map[string]struct {
		reason string
		fields fields
		args   args
		want   want
	}{
		"NotAWorkspaceError": {
			reason: "We should return an error if the supplied managed resource is not a Workspace",
			args: args{
				mg: nil,
			},
			want: want{
				err: errors.New(errNotWorkspace),
			},
		},
		"GetConfigMapError": {
			reason: "We should return any error we encounter getting tfvars from a ConfigMap",
			fields: fields{
				kube: &test.MockClient{
					MockGet: test.NewMockGetFn(nil, func(obj client.Object) error {
						if _, ok := obj.(*corev1.ConfigMap); ok {
							return errBoom
						}
						return nil
					}),
				},
			},
			args: args{
				mg: &v1alpha1.Workspace{
					Spec: v1alpha1.WorkspaceSpec{
						ForProvider: v1alpha1.WorkspaceParameters{
							VarFiles: []v1alpha1.VarFile{
								{
									Source:                v1alpha1.VarFileSourceConfigMapKey,
									ConfigMapKeyReference: &v1alpha1.KeyReference{},
								},
							},
						},
					},
				},
			},
			want: want{
				err: errors.Wrap(errors.Wrap(errBoom, errVarFile), errOptions),
			},
		},
		"GetSecretError": {
			reason: "We should return any error we encounter getting tfvars from a Secret",
			fields: fields{
				kube: &test.MockClient{
					MockGet: test.NewMockGetFn(nil, func(obj client.Object) error {
						if _, ok := obj.(*corev1.Secret); ok {
							return errBoom
						}
						return nil
					}),
				},
			},
			args: args{
				mg: &v1alpha1.Workspace{
					Spec: v1alpha1.WorkspaceSpec{
						ForProvider: v1alpha1.WorkspaceParameters{
							VarFiles: []v1alpha1.VarFile{
								{
									Source:             v1alpha1.VarFileSourceSecretKey,
									SecretKeyReference: &v1alpha1.KeyReference{},
								},
							},
						},
					},
				},
			},
			want: want{
				err: errors.Wrap(errors.Wrap(errBoom, errVarFile), errOptions),
			},
		},
		"ApplyError": {
			reason: "We should return any error we encounter applying our Terraform configuration",
			fields: fields{
				tf: &MockTf{
					MockApply: func(_ context.Context, _ ...terraform.Option) error { return errBoom },
				},
			},
			args: args{
				mg: &v1alpha1.Workspace{},
			},
			want: want{
				err: errors.Wrap(errBoom, errApply),
			},
		},
		"OutputsError": {
			reason: "We should return any error we encounter getting our Terraform outputs",
			fields: fields{
				tf: &MockTf{
					MockApply:   func(_ context.Context, _ ...terraform.Option) error { return nil },
					MockOutputs: func(ctx context.Context) ([]terraform.Output, error) { return nil, errBoom },
				},
			},
			args: args{
				mg: &v1alpha1.Workspace{},
			},
			want: want{
				err: errors.Wrap(errBoom, errOutputs),
			},
		},
		"Success": {
			reason: "We should refresh our connection details with any updated outputs after successfully applying the Terraform configuration",
			fields: fields{
				tf: &MockTf{
					MockApply: func(_ context.Context, _ ...terraform.Option) error { return nil },
					MockOutputs: func(ctx context.Context) ([]terraform.Output, error) {
						return []terraform.Output{
							{Name: "string", Type: terraform.OutputTypeString, Sensitive: true},
							{Name: "object", Type: terraform.OutputTypeObject, Sensitive: false},
						}, nil
					},
				},
				kube: &test.MockClient{
					MockGet: test.NewMockGetFn(nil),
				},
			},
			args: args{
				mg: &v1alpha1.Workspace{
					Spec: v1alpha1.WorkspaceSpec{
						ForProvider: v1alpha1.WorkspaceParameters{
							Vars: []v1alpha1.Var{{Key: "super", Value: "cool"}},
							VarFiles: []v1alpha1.VarFile{
								{
									Source:                v1alpha1.VarFileSourceConfigMapKey,
									ConfigMapKeyReference: &v1alpha1.KeyReference{},
								},
								{
									Source:             v1alpha1.VarFileSourceSecretKey,
									SecretKeyReference: &v1alpha1.KeyReference{},
									Format:             &v1alpha1.VarFileFormatJSON,
								},
							},
						},
					},
				},
			},
			want: want{
				c: managed.ExternalCreation{
					ConnectionDetails: managed.ConnectionDetails{
						"string": {},
						"object": []byte("null"), // Because we JSON decode the value, which is interface{}{}
					},
				},
				wo: v1alpha1.WorkspaceObservation{
					Outputs: map[string]string{
						"object": "null",
					},
				},
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := external{tf: tc.fields.tf, kube: tc.fields.kube}
			got, err := e.Create(tc.args.ctx, tc.args.mg)
			if diff := cmp.Diff(tc.want.err, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\ne.Create(...): -want error, +got error:\n%s\n", tc.reason, diff)
			}
			if diff := cmp.Diff(tc.want.c, got); diff != "" {
				t.Errorf("\n%s\ne.Create(...): -want, +got:\n%s\n", tc.reason, diff)
			}
			if tc.args.mg != nil {
				if diff := cmp.Diff(tc.want.wo, tc.args.mg.(*v1alpha1.Workspace).Status.AtProvider); diff != "" {
					t.Errorf("\n%s\ne.Observe(...): -want, +got:\n%s\n", tc.reason, diff)
				}
			}
		})
	}
}

func TestDelete(t *testing.T) {
	errBoom := errors.New("boom")

	type fields struct {
		tf   tfclient
		kube client.Reader
	}

	type args struct {
		ctx context.Context
		mg  resource.Managed
	}

	cases := map[string]struct {
		reason string
		fields fields
		args   args
		want   error
	}{
		"NotAWorkspaceError": {
			reason: "We should return an error if the supplied managed resource is not a Workspace",
			args: args{
				mg: nil,
			},
			want: errors.New(errNotWorkspace),
		},
		"GetConfigMapError": {
			reason: "We should return any error we encounter getting tfvars from a ConfigMap",
			fields: fields{
				kube: &test.MockClient{
					MockGet: test.NewMockGetFn(nil, func(obj client.Object) error {
						if _, ok := obj.(*corev1.ConfigMap); ok {
							return errBoom
						}
						return nil
					}),
				},
			},
			args: args{
				mg: &v1alpha1.Workspace{
					Spec: v1alpha1.WorkspaceSpec{
						ForProvider: v1alpha1.WorkspaceParameters{
							VarFiles: []v1alpha1.VarFile{
								{
									Source:                v1alpha1.VarFileSourceConfigMapKey,
									ConfigMapKeyReference: &v1alpha1.KeyReference{},
								},
							},
						},
					},
				},
			},
			want: errors.Wrap(errors.Wrap(errBoom, errVarFile), errOptions),
		},
		"GetSecretError": {
			reason: "We should return any error we encounter getting tfvars from a Secret",
			fields: fields{
				kube: &test.MockClient{
					MockGet: test.NewMockGetFn(nil, func(obj client.Object) error {
						if _, ok := obj.(*corev1.Secret); ok {
							return errBoom
						}
						return nil
					}),
				},
			},
			args: args{
				mg: &v1alpha1.Workspace{
					Spec: v1alpha1.WorkspaceSpec{
						ForProvider: v1alpha1.WorkspaceParameters{
							VarFiles: []v1alpha1.VarFile{
								{
									Source:             v1alpha1.VarFileSourceSecretKey,
									SecretKeyReference: &v1alpha1.KeyReference{},
								},
							},
						},
					},
				},
			},
			want: errors.Wrap(errors.Wrap(errBoom, errVarFile), errOptions),
		},
		"DestroyError": {
			reason: "We should return any error we encounter destroying our Terraform configuration",
			fields: fields{
				tf: &MockTf{
					MockDestroy: func(_ context.Context, _ ...terraform.Option) error { return errBoom },
				},
			},
			args: args{
				mg: &v1alpha1.Workspace{},
			},
			want: errors.Wrap(errBoom, errApply),
		},
		"Success": {
			reason: "We should not return an error if we successfully destroy the Terraform configuration",
			fields: fields{
				tf: &MockTf{
					MockDestroy: func(_ context.Context, _ ...terraform.Option) error { return nil },
				},
				kube: &test.MockClient{
					MockGet: test.NewMockGetFn(nil),
				},
			},
			args: args{
				mg: &v1alpha1.Workspace{
					Spec: v1alpha1.WorkspaceSpec{
						ForProvider: v1alpha1.WorkspaceParameters{
							Vars: []v1alpha1.Var{{Key: "super", Value: "cool"}},
							VarFiles: []v1alpha1.VarFile{
								{
									Source:                v1alpha1.VarFileSourceConfigMapKey,
									ConfigMapKeyReference: &v1alpha1.KeyReference{},
								},
								{
									Source:             v1alpha1.VarFileSourceSecretKey,
									SecretKeyReference: &v1alpha1.KeyReference{},
									Format:             &v1alpha1.VarFileFormatJSON,
								},
							},
						},
					},
				},
			},
			want: nil,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := external{tf: tc.fields.tf, kube: tc.fields.kube}
			err := e.Delete(tc.args.ctx, tc.args.mg)
			if diff := cmp.Diff(tc.want, err, test.EquateErrors()); diff != "" {
				t.Errorf("\n%s\ne.Delete(...): -want error, +got error:\n%s\n", tc.reason, diff)
			}
		})
	}
}
